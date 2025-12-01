package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// HTTPClientProvider defines an interface for providing an *http.Client.
type HTTPClientProvider interface {
	Client(ctx context.Context) *http.Client
}

// Define CodeAssistClient struct
type CodeAssistClient struct {
	baseClient
	ProjectID string
}

// NewCodeAssistClient creates a new CodeAssist client
func NewCodeAssistClient(clientProvider HTTPClientProvider, projectID string) *CodeAssistClient {
	return &CodeAssistClient{
		baseClient: baseClient{
			clientProvider: clientProvider,
			clientName:     "CodeAssistClient",
		},
		ProjectID: projectID,
	}
}

// makeAPIRequest creates and executes an HTTP request with common error handling
func (c *CodeAssistClient) makeAPIRequest(ctx context.Context, url string, reqBody interface{}, headers map[string]string) (*http.Response, error) {
	return c.MakeAPIRequest(ctx, url, reqBody, headers)
}

// StreamGenerateContent calls the StreamGenerateContent of Code Assist API.
func (c *CodeAssistClient) StreamGenerateContent(ctx context.Context, modelName string, request GenerateContentRequest) (io.ReadCloser, error) {
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
