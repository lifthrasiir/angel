package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"unicode/utf8"
)

// GeminiModel holds information about a specific Gemini model and its capabilities
type GeminiModel struct {
	ThoughtEnabled bool
	ToolSupported  bool
}

// HTTPClientProvider defines an interface for providing an *http.Client.
type HTTPClientProvider interface {
	Client(ctx context.Context) *http.Client
}

type APIError struct {
	StatusCode int
	Message    string
	Response   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API response error: %d %s, response: %s", e.StatusCode, e.Message, e.Response)
}

// geminiModels holds information about all supported Gemini models and their capabilities
var geminiModels = map[string]*GeminiModel{
	"gemini-2.5-flash": {
		ThoughtEnabled: true,
		ToolSupported:  true,
	},
	"gemini-2.5-pro": {
		ThoughtEnabled: true,
		ToolSupported:  true,
	},
	"gemini-2.5-flash-lite": {
		ThoughtEnabled: false,
		ToolSupported:  true,
	},
	"gemini-2.5-flash-image-preview": {
		ThoughtEnabled: false,
		ToolSupported:  false,
	},
}

// GeminiModelInfo returns the capabilities of a given Gemini model
func GeminiModelInfo(modelName string) *GeminiModel {
	return geminiModels[modelName]
}

// Define CodeAssistClient struct
type CodeAssistClient struct {
	ClientProvider HTTPClientProvider
	ProjectID      string
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

	resp, err := c.ClientProvider.Client(ctx).Do(httpReq)
	if err != nil {
		// Skip logging for cancellation errors
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("API request failed: %w", err)
		}
		// Log both request and response (error) when request fails
		filteredReq := filterLargeJSON(reqBody)
		log.Printf("makeAPIRequest: API request failed - Request: %s, Error: %v", filteredReq, err)
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Skip logging for cancellation errors
		if ctx.Err() == context.Canceled {
			return nil, &APIError{StatusCode: resp.StatusCode, Message: resp.Status, Response: string(bodyBytes)}
		}

		// Log both request and response when status code is not OK
		filteredReq := filterLargeJSON(reqBody)
		filteredResp := filterLargeJSON(string(bodyBytes))
		log.Printf("makeAPIRequest: API response error - Request: %s, Status %d %s, Response: %s", filteredReq, resp.StatusCode, resp.Status, filteredResp)
		return nil, &APIError{StatusCode: resp.StatusCode, Message: resp.Status, Response: string(bodyBytes)}
	}

	return resp, nil
}

// StreamGenerateContent calls the StreamGenerateContent of Code Assist API.
func (c *CodeAssistClient) StreamGenerateContent(ctx context.Context, modelName string, request GenerateContentRequest) (io.ReadCloser, error) {
	// Validate model
	if GeminiModelInfo(modelName) == nil {
		return nil, fmt.Errorf("unsupported model: %s", modelName)
	}

	reqBody := CAGenerateContentRequest{
		Model:   modelName,
		Project: c.ProjectID,
		Request: request,
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

// CountTokens calls the countTokens of Code Assist API.
func (c *CodeAssistClient) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
	// Validate model
	if GeminiModelInfo(modelName) == nil {
		return nil, fmt.Errorf("unsupported model: %s", modelName)
	}

	reqBody := CaCountTokenRequest{
		Request: CountTokenRequest{
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

// SetCodeAssistGlobalUserSetting calls the setCodeAssistGlobalUserSetting of Code Assist API.
func (c *CodeAssistClient) SetCodeAssistGlobalUserSetting(ctx context.Context, req SetCodeAssistGlobalUserSettingRequest) (*CodeAssistGlobalUserSettingResponse, error) {
	url := "https://cloudcode-pa.googleapis.com/v1internal:setCodeAssistGlobalUserSetting"
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

	var settingRes CodeAssistGlobalUserSettingResponse
	if err := json.Unmarshal(bodyBytes, &settingRes); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &settingRes, nil
}

// filterLargeJSON filters large JSON content, truncating individual string literals that exceed 100 characters
func filterLargeJSON(data interface{}) string {
	var jsonStr string

	// Convert input to JSON string
	switch v := data.(type) {
	case string:
		jsonStr = v
	case []byte:
		jsonStr = string(v)
	default:
		jsonBytes, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Sprintf("Failed to marshal JSON: %v", err)
		}
		jsonStr = string(jsonBytes)
	}

	// Parse the JSON to truncate individual string values
	var filteredData interface{}
	if err := json.Unmarshal([]byte(jsonStr), &filteredData); err != nil {
		return jsonStr // Return original if parsing fails
	}

	// Recursively truncate string values that exceed 100 characters
	truncateStrings(filteredData, 100)

	// Marshal the filtered data back to JSON
	filteredBytes, err := json.MarshalIndent(filteredData, "", "  ")
	if err != nil {
		return jsonStr // Return original if marshaling fails
	}

	return string(filteredBytes)
}

// truncateStrings recursively truncates string values in nested data structures
func truncateStrings(data interface{}, maxLen int) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if str, ok := value.(string); ok {
				if utf8.RuneCountInString(str) > maxLen {
					v[key] = truncateString(str, maxLen)
				}
			} else {
				truncateStrings(value, maxLen)
			}
		}
	case []interface{}:
		for i, item := range v {
			if str, ok := item.(string); ok {
				if utf8.RuneCountInString(str) > maxLen {
					v[i] = truncateString(str, maxLen)
				}
			} else {
				truncateStrings(item, maxLen)
			}
		}
	}
}

// truncateString truncates a string to maxLen runes, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
