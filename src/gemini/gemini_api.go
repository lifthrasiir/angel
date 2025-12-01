package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// GeminiAPIClient holds the client for direct Gemini API
type GeminiAPIClient struct {
	baseClient
	APIKey string
}

// NewGeminiAPIClient creates a new Gemini API client
func NewGeminiAPIClient(clientProvider HTTPClientProvider, apiKey string) *GeminiAPIClient {
	return &GeminiAPIClient{
		baseClient: baseClient{
			clientProvider: clientProvider,
			clientName:     "GeminiAPIClient",
		},
		APIKey: apiKey,
	}
}

// makeAPIRequest creates and executes an HTTP request with common error handling
func (c *GeminiAPIClient) makeAPIRequest(ctx context.Context, url string, reqBody interface{}, headers map[string]string) (*http.Response, error) {
	return c.MakeAPIRequest(ctx, url, reqBody, headers)
}

// StreamGenerateContent calls the StreamGenerateContent of Gemini API.
func (c *GeminiAPIClient) StreamGenerateContent(ctx context.Context, modelName string, request GenerateContentRequest) (io.ReadCloser, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?key=%s", modelName, c.APIKey)
	headers := map[string]string{"Accept": "text/event-stream"}

	resp, err := c.makeAPIRequest(ctx, url, request, headers)
	if err != nil {
		log.Printf("GeminiAPI.StreamGenerateContent: makeAPIRequest failed: %v", err)
		return nil, err
	}

	return resp.Body, nil
}

// CountTokens calls the countTokens of Gemini API.
func (c *GeminiAPIClient) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
	reqBody := CountTokenRequest{
		Model:    modelName,
		Contents: contents,
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:countTokens?key=%s", modelName, c.APIKey)
	resp, err := c.makeAPIRequest(ctx, url, reqBody, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response struct {
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &CaCountTokenResponse{
		TotalTokens: response.TotalTokens,
	}, nil
}

// GenerateContent calls the non-streaming generateContent endpoint
func (c *GeminiAPIClient) GenerateContent(ctx context.Context, modelName string, request GenerateContentRequest) (*GenerateContentResponse, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, c.APIKey)
	resp, err := c.makeAPIRequest(ctx, url, request, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response GenerateContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &response, nil
}
