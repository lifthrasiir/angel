package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	html2text "github.com/k3a/html2text"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/llm"
)

const (
	URL_FETCH_TIMEOUT_MS = 10000
	MAX_CONTENT_LENGTH   = 100000
	USER_AGENT           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36 Angel/0.0"
)

var httpUrlPattern = regexp.MustCompile(`(https?://[^\n]+)`)

// extractURLs extracts URLs from a given text.
func extractURLs(text string) []string {
	return httpUrlPattern.FindAllString(text, -1)
}

// isPrivateIp checks if a given URL points to a private IP address.
func isPrivateIp(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() { // IsPrivate() checks for RFC1918 addresses
			return true
		}
	}
	return false
}

// fetchWithTimeout fetches content from a URL with a given timeout.
func fetchWithTimeout(ctx context.Context, targetURL string, timeout time.Duration) (string, error) {
	client := http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Add("User-Agent", USER_AGENT)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request failed with status code %d %s", resp.StatusCode, resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(bodyBytes), nil
}

// executeWebFetchFallback handles web fetching using a direct HTTP request and returns the fetched content processed by LLM.
func executeWebFetchFallback(ctx context.Context, prompt string, modelProvider llm.ModelProvider) (ToolHandlerResults, error) {
	urls := extractURLs(prompt)
	if len(urls) == 0 {
		return ToolHandlerResults{}, fmt.Errorf("no URL found in the prompt for fallback")
	}
	urlToFetch := urls[0]

	// Convert GitHub blob URL to raw URL
	if strings.Contains(urlToFetch, "github.com") && strings.Contains(urlToFetch, "/blob/") {
		urlToFetch = strings.Replace(urlToFetch, "github.com", "raw.githubusercontent.com", 1)
		urlToFetch = strings.Replace(urlToFetch, "/blob/", "/", 1)
	}

	htmlContent, err := fetchWithTimeout(ctx, urlToFetch, URL_FETCH_TIMEOUT_MS*time.Millisecond)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("Error during fallback fetch for %s: %v", urlToFetch, err)
	}

	textContent := html2text.HTML2Text(htmlContent)
	if len(textContent) > MAX_CONTENT_LENGTH {
		textContent = textContent[:MAX_CONTENT_LENGTH]
	}

	fallbackPrompt := executePromptTemplate("web-fetch-fallback.md", map[string]any{
		"Prompt":      prompt,
		"TextContent": textContent,
	})

	sessionParams := llm.SessionParams{
		Contents: []Content{{Role: "user", Parts: []Part{{Text: fallbackPrompt}}}},
	}

	oneShotResult, err := modelProvider.GenerateContentOneShot(ctx, sessionParams)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("Error during LLM processing of fallback content: %v", err)
	}

	llmContent := oneShotResult.Text

	return ToolHandlerResults{Value: map[string]interface{}{
		"llmContent":    llmContent,
		"returnDisplay": fmt.Sprintf("Content for %s processed using fallback fetch.", urlToFetch),
	}}, nil
}

func executeWebFetch(ctx context.Context, prompt string, modelProvider, fallbackModelProvider llm.ModelProvider) (ToolHandlerResults, error) {
	urls := extractURLs(prompt)
	if len(urls) == 0 {
		// If no URLs are found, perform a DuckDuckGo search
		searchQuery := url.QueryEscape(prompt)
		duckDuckGoURL := fmt.Sprintf("https://duckduckgo.com/html/?q=%s", searchQuery)
		log.Printf("WebFetchTool: No URLs found. Performing DuckDuckGo search for: %s", duckDuckGoURL)

		// Modify the prompt to explicitly ask to exclude irrelevant content
		modifiedPrompt := fmt.Sprintf("Process the content from this URL: %s\nExtract relevant information, excluding any irrelevant content.", duckDuckGoURL)

		return executeWebFetchFallback(ctx, modifiedPrompt, fallbackModelProvider)
	}

	// Check for private IP before calling LLM
	urlToProcess := urls[0] // Assuming we only process the first URL for private IP check
	if isPrivateIp(urlToProcess) {
		log.Printf("WebFetchTool: Detected private IP for %s. Bypassing LLM and performing manual fetch.", urlToProcess)
		return executeWebFetchFallback(ctx, prompt, fallbackModelProvider)
	}

	// Primary Gemini API call with urlContext
	sessionParams := llm.SessionParams{
		Contents: []Content{{Role: "user", Parts: []Part{{Text: prompt}}}},
		ToolConfig: map[string]interface{}{
			"":                           map[string]interface{}{}, // No default tools
			llm.GeminiUrlContextToolName: map[string]interface{}{},
		},
	}

	oneShotResult, err := modelProvider.GenerateContentOneShot(ctx, sessionParams)

	processingError := false
	if err != nil {
		log.Printf("WebFetchTool: Processing error detected (%s). Attempting fallback.", err)
		processingError = true
	} else {
		// Check URL retrieval status from LLM
		if oneShotResult.URLContextMetadata != nil && len(oneShotResult.URLContextMetadata.URLMetadata) > 0 {
			allSuccessful := true
			for _, meta := range oneShotResult.URLContextMetadata.URLMetadata {
				if meta.URLRetrievalStatus != "URL_RETRIEVAL_STATUS_SUCCESS" {
					log.Printf("WebFetchTool: Processing error detected (%s returned %s). Attempting fallback.", meta.RetrievedURL, meta.URLRetrievalStatus)
					allSuccessful = false
					break
				}
			}
			if !allSuccessful {
				processingError = true
			}
		} else if oneShotResult.Text == "" && oneShotResult.GroundingMetadata == nil {
			// No URL metadata and no content/sources
			log.Printf("WebFetchTool: Processing error detected (no URL metadata nor content/sources). Attempting fallback.")
			processingError = true
		}
	}

	if processingError {
		// Perform fallback for the original prompt
		return executeWebFetchFallback(ctx, prompt, fallbackModelProvider)
	}

	// If no processing error, return the LLM's response
	return ToolHandlerResults{Value: map[string]interface{}{
		"llmContent":    oneShotResult.Text,
		"returnDisplay": "Content processed from prompt.",
	}}, nil
}

// WebFetchTool implements the handler for the web_fetch tool.
func WebFetchTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
	if err := EnsureKnownKeys("web_fetch", args, "prompt"); err != nil {
		return ToolHandlerResults{}, err
	}
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return ToolHandlerResults{}, fmt.Errorf("invalid or empty 'prompt' argument for web_fetch")
	}

	registry, err := llm.ModelsFromContext(ctx)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get models registry from context: %w", err)
	}

	// Resolve subagents for web fetch tasks
	modelProvider, err := registry.ResolveSubagent(params.ModelName, llm.SubagentWebFetchTask)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("LLM provider not initialized for web_fetch (model: %s): %w", params.ModelName, err)
	}
	fallbackModelProvider, err := registry.ResolveSubagent(params.ModelName, llm.SubagentWebFetchFallbackTask)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("LLM provider not initialized for web_fetch_fallback (model: %s): %w", params.ModelName, err)
	}

	return executeWebFetch(ctx, prompt, modelProvider, fallbackModelProvider)
}

var webFetchToolDefinition = ToolDefinition{
	Name:        "web_fetch",
	Description: "Processes content from URL(s). If no URLs are provided, it automatically performs a DuckDuckGo search using your prompt as the query. Your prompt and instructions are forwarded to an internal AI agent that fetches and interprets the content. While not a direct search engine, it can retrieve content from HTML-only search result pages (e.g., `https://html.duckduckgo.com/html?q=query`). Without explicit instructions, the agent may summarize or extract key data for efficient information retrieval. Clear directives are required to obtain the original or full content.",
	Parameters: &Schema{
		Type: TypeObject,
		Properties: map[string]*Schema{
			"prompt": {
				Type:        TypeString,
				Description: "A comprehensive prompt that includes the URL(s) (up to 20) to fetch and specific instructions on how to process their content (e.g., \"Summarize https://example.com/article and extract key points from https://another.com/data\"). If no URLs are provided, this prompt will be used as a search query for DuckDuckGo. To retrieve the full, unsummarized content, you must include explicit instructions such as 'return full content', 'do not summarize', or 'provide original text'. Must contain at least one URL starting with http:// or https://, or be a search query.",
			},
		},
		Required: []string{"prompt"},
	},
	Handler: WebFetchTool,
}
