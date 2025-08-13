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
)

const (
	URL_FETCH_TIMEOUT_MS = 10000
	MAX_CONTENT_LENGTH   = 100000
)

// WebFetchToolParams represents the parameters for the WebFetch tool.
type WebFetchToolParams struct {
	Prompt    string `json:"prompt"`
	ModelName string `json:"modelName"`
}

// WebFetchTool implements the ToolDefinition handler for web_fetch.
type WebFetchTool struct {
	// Config and LLMProvider will need to be passed or accessed globally.
	// For now, we'll assume access via GlobalGeminiState or similar.
}

var httpUrlPattern = regexp.MustCompile(`(https?://[^\s]+)`)

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

// executeFallback handles web fetching using a direct HTTP request and returns the fetched content processed by LLM.
func (t *WebFetchTool) executeFallback(ctx context.Context, params WebFetchToolParams, llmProvider LLMProvider) (map[string]interface{}, error) {
	urls := extractURLs(params.Prompt)
	if len(urls) == 0 {
		return nil, fmt.Errorf("Error: No URL found in the prompt for fallback.")
	}
	urlToFetch := urls[0]

	// Convert GitHub blob URL to raw URL
	if strings.Contains(urlToFetch, "github.com") && strings.Contains(urlToFetch, "/blob/") {
		urlToFetch = strings.Replace(urlToFetch, "github.com", "raw.githubusercontent.com", 1)
		urlToFetch = strings.Replace(urlToFetch, "/blob/", "/", 1)
	}

	htmlContent, err := fetchWithTimeout(ctx, urlToFetch, URL_FETCH_TIMEOUT_MS*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("Error during fallback fetch for %s: %v", urlToFetch, err)
	}

	textContent := html2text.HTML2Text(htmlContent)
	if len(textContent) > MAX_CONTENT_LENGTH {
		textContent = textContent[:MAX_CONTENT_LENGTH]
	}

	fallbackPrompt := fmt.Sprintf(`The user requested the following: "%s".

I was unable to access the URL directly. Instead, I have fetched the raw content of the page. Please use the following content to answer the user's request. Do not attempt to access the URL again.

---
%s
---`, params.Prompt, textContent)

	sessionParams := SessionParams{
		ModelName: params.ModelName,
		Contents:  []Content{{Role: "user", Parts: []Part{{Text: fallbackPrompt}}}},
	}

	oneShotResult, err := llmProvider.GenerateContentOneShot(ctx, sessionParams)
	if err != nil {
		return nil, fmt.Errorf("Error during LLM processing of fallback content: %v", err)
	}

	llmContent := oneShotResult.Text

	return map[string]interface{}{
		"llmContent":    llmContent,
		"returnDisplay": fmt.Sprintf("Content for %s processed using fallback fetch.", urlToFetch),
	}, nil
}

// CallToolFunction implements the handler for the web_fetch tool.
func (t *WebFetchTool) CallToolFunction(ctx context.Context, args map[string]interface{}, modelName string) (map[string]interface{}, error) {
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("invalid or empty 'prompt' argument for web_fetch")
	}

	params := WebFetchToolParams{Prompt: prompt, ModelName: modelName}

	urls := extractURLs(params.Prompt)
	if len(urls) == 0 {
		return nil, fmt.Errorf("the 'prompt' must contain at least one valid URL (starting with http:// or https://).")
	}

	llmProvider := CurrentProviders[modelName]
	if llmProvider == nil {
		return nil, fmt.Errorf("LLM provider not initialized for web_fetch (model: %s)", modelName)
	}

	// Check for private IP before calling LLM
	urlToProcess := urls[0] // Assuming we only process the first URL for private IP check
	if isPrivateIp(urlToProcess) {
		log.Printf("WebFetchTool: Detected private IP for %s. Bypassing LLM and performing manual fetch.", urlToProcess)
		return t.executeFallback(ctx, params, llmProvider)
	}

	// --- Primary Gemini API call with urlContext ---
	sessionParams := SessionParams{
		ModelName: modelName,
		Contents:  []Content{{Role: "user", Parts: []Part{{Text: params.Prompt}}}},
		ToolConfig: map[string]interface{}{
			"urlContext": map[string]interface{}{}, // Empty object as per Gemini API
		},
	}

	oneShotResult, err := llmProvider.GenerateContentOneShot(ctx, sessionParams)

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
					log.Printf("WebFetchTool: Processing error detected (%s returned %s). Attempting fallback.", meta.URL, meta.URLRetrievalStatus)
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
		return t.executeFallback(ctx, params, llmProvider)
	}

	// If no processing error, return the LLM's response
	return map[string]interface{}{
		"llmContent":    oneShotResult.Text,
		"returnDisplay": fmt.Sprintf("Content processed from prompt."),
	}, nil
}

// Helper for min function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
