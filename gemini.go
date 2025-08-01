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

const DefaultGeminiModel = "gemini-2.5-flash"

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
	gs *GeminiState
}

func (ts *tokenSaverSource) Token() (*oauth2.Token, error) {
	token, err := ts.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	// Save the token to the database after it's obtained/refreshed
	ts.gs.SaveToken(token)
	return token, nil
}

func (ts *tokenSaverSource) Client(ctx context.Context) *http.Client {
	return oauth2.NewClient(ctx, ts.TokenSource)
}

// GeminiState encapsulates the global state related to Gemini client and authentication.
type GeminiState struct {
	GoogleOauthConfig *oauth2.Config
	OAuthState        string
	Token             *oauth2.Token
	TokenSource       HTTPClientProvider // Changed to HTTPClientProvider
	GeminiClient      *CodeAssistClient
	ProjectID         string
	SelectedAuthType  AuthType
	UserEmail         string
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
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.clientProvider.Client(ctx).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API response error: %s, response: %s", resp.Status, string(bodyBytes))
	}

	return resp, nil
}

// streamGenerateContent calls the streamGenerateContent of Code Assist API.
func (c *CodeAssistClient) streamGenerateContent(ctx context.Context, contents []Content, modelName string, systemPrompt string, thinkingConfig *ThinkingConfig) (io.ReadCloser, error) {
	reqBody := CAGenerateContentRequest{
		Model:   modelName,
		Project: c.projectID,
		Request: VertexGenerateContentRequest{
			Contents: contents,
			SystemInstruction: func() *Content {
				if systemPrompt == "" {
					return nil
				}
				return &Content{
					Parts: []Part{
						{Text: systemPrompt},
					},
				}
			}(),
			Tools: GetToolsForGemini(),
			GenerationConfig: &GenerationConfig{
				ThinkingConfig: thinkingConfig,
			},
		},
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent"
	headers := map[string]string{"Accept": "text/event-stream"}

	resp, err := c.makeAPIRequest(ctx, url, reqBody, headers)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// SendMessageStream calls the streamGenerateContent of Code Assist API and returns an iter.Seq of responses.
func (c *CodeAssistClient) SendMessageStream(ctx context.Context, contents []Content, modelName string, systemPrompt string, thinkingConfig *ThinkingConfig) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	respBody, err := c.streamGenerateContent(ctx, contents, modelName, systemPrompt, thinkingConfig)
	if err != nil {
		return nil, nil, err
	}

	dec := json.NewDecoder(respBody)

	// NOTE: This function is intentionally designed to parse a specific JSON stream format, not standard SSE. Do not modify without understanding its purpose.
	// Read the opening bracket of the JSON array
	_, err = dec.Token()
	if err != nil {
		respBody.Close()
		return nil, nil, fmt.Errorf("expected opening bracket '[', but got %w", err)
	}

	// Create an iter.Seq that yields CaGenerateContentResponse
	seq := func(yield func(CaGenerateContentResponse) bool) {
		for dec.More() {
			var caResp CaGenerateContentResponse
			if err := dec.Decode(&caResp); err != nil {
				log.Printf("Failed to decode JSON object from stream: %v", err)
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
func (c *CodeAssistClient) GenerateContentOneShot(ctx context.Context, contents []Content, modelName string, systemPrompt string, thinkingConfig *ThinkingConfig) (string, error) {
	seq, closer, err := c.SendMessageStream(ctx, contents, modelName, systemPrompt, thinkingConfig)
	if err != nil {
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

// InitGeminiClient initializes the CodeAssistClient.
func (gs *GeminiState) InitGeminiClient() {
	ctx := context.Background()
	var clientProvider HTTPClientProvider

	switch gs.SelectedAuthType {
	case AuthTypeLoginWithGoogle:
		if gs.Token == nil {
			log.Println("InitGeminiClient: OAuth token is nil. Cannot initialize client.")
			return
		}
		// Always create a new token source to ensure it's up-to-date with the current token
		gs.TokenSource = &tokenSaverSource{
			TokenSource: gs.GoogleOauthConfig.TokenSource(ctx, gs.Token),
			gs:          gs,
		}
		clientProvider = gs.TokenSource

		// Proactively refresh token on startup if expired
		// This will also save the new token via tokenSaverSource.Token()
		_, err := gs.TokenSource.(*tokenSaverSource).Token() // This call will trigger refresh if needed
		if err != nil {
			log.Printf("InitGeminiClient: Failed to proactively refresh token on startup: %v. Client not initialized.", err)
			gs.GeminiClient = nil
			return
		}

		caClient := NewCodeAssistClient(clientProvider, gs.ProjectID) // Initialize with current ProjectID

		// Only call LoadCodeAssist and OnboardUser if ProjectID is not set or needs re-validation
		if gs.ProjectID == "" {
			loadReq := LoadCodeAssistRequest{
				CloudaicompanionProject: gs.ProjectID, // Will be empty
				Metadata: &ClientMetadata{
					IdeType:     "IDE_UNSPECIFIED",
					Platform:    "PLATFORM_UNSPECIFIED",
					PluginType:  "GEMINI",
					DuetProject: gs.ProjectID,
				},
			}
			loadRes, loadErr := caClient.LoadCodeAssist(ctx, loadReq)
			if loadErr != nil {
				log.Printf("InitGeminiClient: LoadCodeAssist failed: %v. Client not initialized.", loadErr)
				gs.GeminiClient = nil
				return
			}

			if loadRes.CloudaicompanionProject != "" {
				gs.ProjectID = loadRes.CloudaicompanionProject

				var userTierID UserTierID
				if loadRes.CurrentTier != nil {
					userTierID = loadRes.CurrentTier.ID
				} else {
					for _, tier := range loadRes.AllowedTiers {
						if tier.IsDefault != nil && *tier.IsDefault {
							userTierID = tier.ID
							break
						}
					}
				}

				onboardReq := OnboardUserRequest{
					CloudaicompanionProject: gs.ProjectID,
					Metadata: &ClientMetadata{
						IdeType:     "IDE_UNSPECIFIED",
						Platform:    "PLATFORM_UNSPECIFIED",
						PluginType:  "GEMINI",
						DuetProject: gs.ProjectID,
					},
				}
				if userTierID != "" {
					onboardReq.TierID = &userTierID
				}
				lroRes, onboardErr := caClient.OnboardUser(ctx, onboardReq)
				if onboardErr != nil {
					log.Printf("InitGeminiClient: OnboardUser failed: %v. Client not initialized.", onboardErr)
					gs.GeminiClient = nil
					return
				}

				if lroRes.Response != nil && lroRes.Response.CloudaicompanionProject != nil {
					gs.ProjectID = lroRes.Response.CloudaicompanionProject.ID
				} else {
					log.Println("InitGeminiClient: No project ID from OnboardUser. Client not initialized.")
					gs.GeminiClient = nil
					return
				}
			} else {
				log.Println("InitGeminiClient: LoadCodeAssist did not return a Project ID. Client not initialized.")
				gs.GeminiClient = nil
				return
			}
		} else {
			// If ProjectID is already set, skip LoadCodeAssist and OnboardUser
		}

		// Ensure the final ProjectID is saved to the database
		gs.SaveToken(gs.Token)

	case AuthTypeUseGemini:
		clientProvider = &defaultHTTPClientProvider{}
	case AuthTypeUseVertexAI:
		clientProvider = &defaultHTTPClientProvider{}
	case AuthTypeCloudShell:
		clientProvider = &defaultHTTPClientProvider{}
	default:
		log.Fatalf("InitGeminiClient: Unsupported authentication type: %s", gs.SelectedAuthType)
	}

	gs.GeminiClient = NewCodeAssistClient(clientProvider, gs.ProjectID)
}

// saveToken saves the OAuth token to the database.
func (gs *GeminiState) SaveToken(t *oauth2.Token) {
	tokenJSON, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal token: %v", err)
		return
	}

	if err := SaveOAuthToken(string(tokenJSON), gs.UserEmail, gs.ProjectID); err != nil {
		log.Printf("Failed to save OAuth token to DB: %v", err)
		return
	}
	log.Println("OAuth token saved to DB.")
	gs.Token = t // Update global token variable
}

// loadToken loads the OAuth token from the database.
func (gs *GeminiState) LoadToken() {
	tokenJSON, userEmail, projectID, err := LoadOAuthToken()
	if err != nil {
		log.Printf("LoadToken: Failed to load OAuth token from DB: %v", err)
		return
	}

	if tokenJSON == "" {
		log.Println("LoadToken: No existing token in DB.")
		return
	}

	if err := json.Unmarshal([]byte(tokenJSON), &gs.Token); err != nil {
		log.Printf("LoadToken: Failed to decode token from DB: %v", err)
		return
	}
	gs.UserEmail = userEmail
	gs.ProjectID = projectID

}
