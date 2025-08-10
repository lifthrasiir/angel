package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Ensure CodeAssistClient implements LLMProvider
var _ LLMProvider = (*CodeAssistClient)(nil)

// HTTPClientProvider defines an interface for providing an *http.Client.
type HTTPClientProvider interface {
	Client(ctx context.Context) *http.Client
}

// defaultHTTPClientProvider implements HTTPClientProvider for non-OAuth cases.
type defaultHTTPClientProvider struct{}

func (d *defaultHTTPClientProvider) Client(ctx context.Context) *http.Client {
	return &http.Client{}
}

// tokenSaverSource wraps an oauth2.TokenSource and saves the token to the database
// whenever a new token is obtained (e.g., after a refresh).
type tokenSaverSource struct {
	oauth2.TokenSource
	ga *GeminiAuth
}

func (ts *tokenSaverSource) Token() (*oauth2.Token, error) {
	log.Println("tokenSaverSource: Attempting to get/refresh token...")
	token, err := ts.TokenSource.Token()
	if err != nil {
		log.Printf("tokenSaverSource: Failed to get/refresh token: %v", err)
		return nil, err
	}
	// Save the token to the database after it's obtained/refreshed
	ts.ga.SaveToken(ts.ga.db, token)
	return token, nil
}

func (ts *tokenSaverSource) Client(ctx context.Context) *http.Client {
	log.Println("tokenSaverSource: Creating new HTTP client with oauth2.TokenSource.")
	return oauth2.NewClient(ctx, ts.TokenSource)
}

type APIError struct {
	StatusCode int
	Message    string
	Response   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API response error: %d %s, response: %s", e.StatusCode, e.Message, e.Response)
}

// Define CodeAssistClient struct
type CodeAssistClient struct {
	clientProvider HTTPClientProvider // Changed from *http.Client
	projectID      string
}

// NewCodeAssistClient creates a new instance of CodeAssistClient.
func NewCodeAssistClient(provider HTTPClientProvider, projectID string) *CodeAssistClient {
	return &CodeAssistClient{
		clientProvider: provider,
		projectID:      projectID,
	}
}

// makeAPIRequest creates and executes an HTTP request with common error handling
func (c *CodeAssistClient) makeAPIRequest(ctx context.Context, url string, reqBody interface{}, headers map[string]string) (*http.Response, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("makeAPIRequest: Failed to marshal request body: %v", err)
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("makeAPIRequest: Failed to create HTTP request: %v", err)
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.clientProvider.Client(ctx).Do(httpReq)
	if err != nil {
		log.Printf("makeAPIRequest: API request failed: %v", err)
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("makeAPIRequest: API response error: Status %d %s, Response: %s", resp.StatusCode, resp.Status, string(bodyBytes))
		return nil, &APIError{StatusCode: resp.StatusCode, Message: resp.Status, Response: string(bodyBytes)}
	}

	return resp, nil
}

// streamGenerateContent calls the streamGenerateContent of Code Assist API.
func (c *CodeAssistClient) streamGenerateContent(ctx context.Context, params SessionParams) (io.ReadCloser, error) {
	reqBody := CAGenerateContentRequest{
		Model:   params.ModelName,
		Project: c.projectID,
		Request: VertexGenerateContentRequest{
			Contents: params.Contents,
			SystemInstruction: func() *Content {
				if params.SystemPrompt == "" {
					return nil
				}
				return &Content{
					Parts: []Part{
						{Text: params.SystemPrompt},
					},
				}
			}(),
			Tools: GetToolsForGemini(),
			GenerationConfig: &GenerationConfig{
				ThinkingConfig: params.ThinkingConfig,
			},
		},
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent"
	headers := map[string]string{"Accept": "text/event-stream"}

	resp, err := c.makeAPIRequest(ctx, url, reqBody, headers)
	if err != nil {
		log.Printf("streamGenerateContent: makeAPIRequest failed: %v", err)
		return nil, err
	}

	return resp.Body, nil
}

// SendMessageStream calls the streamGenerateContent of Code Assist API and returns an iter.Seq of responses.
func (c *CodeAssistClient) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	respBody, err := c.streamGenerateContent(ctx, params)
	if err != nil {
		log.Printf("SendMessageStream: streamGenerateContent failed: %v", err)
		return nil, nil, err
	}

	dec := json.NewDecoder(respBody)

	// NOTE: This function is intentionally designed to parse a specific JSON stream format, not standard SSE. Do not modify without understanding its purpose.
	// Read the opening bracket of the JSON array
	_, err = dec.Token()
	if err != nil {
		respBody.Close()
		log.Printf("SendMessageStream: Expected opening bracket '[', but got %v", err)
		return nil, nil, fmt.Errorf("expected opening bracket '[', but got %w", err)
	}

	// Create an iter.Seq that yields CaGenerateContentResponse
	seq := func(yield func(CaGenerateContentResponse) bool) {
		for dec.More() {
			var caResp CaGenerateContentResponse
			if err := dec.Decode(&caResp); err != nil {
				log.Printf("SendMessageStream: Failed to decode JSON object from stream: %v", err)
				return // Or handle error more robustly
			}
			if !yield(caResp) {
				return
			}
		}
	}

	return seq, respBody, nil
}

// GenerateContentOneShot calls the streamGenerateContent of Code Assist API and returns a single response.
func (c *CodeAssistClient) GenerateContentOneShot(ctx context.Context, params SessionParams) (string, error) {
	seq, closer, err := c.SendMessageStream(ctx, params)
	if err != nil {
		log.Printf("GenerateContentOneShot: SendMessageStream failed: %v", err)
		return "", err
	}
	defer closer.Close()

	for caResp := range seq {
		if len(caResp.Response.Candidates) > 0 && len(caResp.Response.Candidates[0].Content.Parts) > 0 {
			textPart := caResp.Response.Candidates[0].Content.Parts[0].Text
			if textPart != "" {
				return textPart, nil
			}
		}
	}

	return "", fmt.Errorf("no text content found in LLM response")
}

// CountTokens calls the countTokens of Code Assist API.
func (c *CodeAssistClient) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	reqBody := CaCountTokenRequest{
		Request: VertexCountTokenRequest{
			Model:    modelName,
			Contents: contents,
		},
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:countTokens"
	resp, err := c.makeAPIRequest(ctx, url, reqBody, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var caResp CaCountTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&caResp); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &caResp, nil
}

// LoadCodeAssist calls the loadCodeAssist of Code Assist API.
func (c *CodeAssistClient) LoadCodeAssist(ctx context.Context, req LoadCodeAssistRequest) (*LoadCodeAssistResponse, error) {
	url := "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	resp, err := c.makeAPIRequest(ctx, url, req, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	resp.Body.Close() // Close the body after reading

	var loadRes LoadCodeAssistResponse
	if err := json.Unmarshal(bodyBytes, &loadRes); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	// If cloudaicompanionProject is not set, generate an accurate error indicating that the free tier cannot be used based on the contents of ineligibleTiers.
	if loadRes.CloudaicompanionProject == "" {
		if len(loadRes.IneligibleTiers) > 0 {
			var errorMessages []string
			for _, tier := range loadRes.IneligibleTiers {
				if tier.TierID == "free-tier" {
					errorMessages = append(errorMessages, fmt.Sprintf("Free tier (%s) is not available: %s (Reason Code: %s)", tier.TierName, tier.ReasonMessage, tier.ReasonCode))
				}
			}
			if len(errorMessages) > 0 {
				return nil, fmt.Errorf("failed to load code assist: %s", strings.Join(errorMessages, "; "))
			}
		}
		// This means that the initial CloudAICompanion project will be assigned soon. (TODO: restart required for some reason)
		return nil, fmt.Errorf("failed to load code assist: cloudaicompanionProject is not set and no specific ineligible tiers found. This still means that you are going to get assigned for the project, try to restart the application to take the effect.")
	}

	// If cloudaicompanionProject is set and showNotice is true, print the privacy notice to the console and a message indicating that continuing to use it automatically accepts the notice.
	if loadRes.CurrentTier != nil && loadRes.CurrentTier.PrivacyNotice != nil && loadRes.CurrentTier.PrivacyNotice.ShowNotice {
		log.Println("--- Privacy Notice ---")
		log.Println(loadRes.CurrentTier.PrivacyNotice.NoticeText)
		log.Println("----------------------")
		log.Println("By continuing to use Gemini Code Assist, you automatically accept the privacy notice.")
	}

	return &loadRes, nil
}

// OnboardUser calls the onboardUser of Code Assist API.
func (c *CodeAssistClient) OnboardUser(ctx context.Context, req OnboardUserRequest) (*LongRunningOperationResponse, error) {
	url := "https://cloudcode-pa.googleapis.com/v1internal:onboardUser"
	resp, err := c.makeAPIRequest(ctx, url, req, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	resp.Body.Close() // Close the body after reading

	var lroRes LongRunningOperationResponse
	if err := json.Unmarshal(bodyBytes, &lroRes); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &lroRes, nil
}

// MaxTokens implements the LLMProvider interface for CodeAssistClient.
func (c *CodeAssistClient) MaxTokens() int {
	// Both gemini-2.5-flash and gemini-2.5-pro have a token limit of 1048576
	return 1048576
}

// RelativeDisplayOrder implements the LLMProvider interface for CodeAssistClient.
func (c *CodeAssistClient) RelativeDisplayOrder() int {
	return 0
}
