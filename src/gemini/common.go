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

// baseClient provides common functionality for Gemini API clients
type baseClient struct {
	clientProvider HTTPClientProvider
	clientName     string // Used for logging
}

// MakeAPIRequest creates and executes an HTTP request with common error handling
func (c *baseClient) MakeAPIRequest(ctx context.Context, url string, reqBody interface{}, headers map[string]string) (*http.Response, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("%s.makeAPIRequest: Failed to marshal request body: %v", c.clientName, err)
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("%s.makeAPIRequest: Failed to create HTTP request: %v", c.clientName, err)
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.clientProvider.Client(ctx).Do(httpReq)
	if err != nil {
		// Skip logging for cancellation errors
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("API request failed: %w", err)
		}
		// Log both request and response (error) when request fails
		filteredReq := filterLargeJSON(reqBody)
		log.Printf("%s.makeAPIRequest: API request failed - Request: %s, Error: %v", c.clientName, filteredReq, err)
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
		log.Printf("%s.makeAPIRequest: API response error - Request: %s, Status %d %s, Response: %s", c.clientName, filteredReq, resp.StatusCode, resp.Status, filteredResp)
		return nil, &APIError{StatusCode: resp.StatusCode, Message: resp.Status, Response: string(bodyBytes)}
	}

	return resp, nil
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
