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

// GeminiState encapsulates the global state related to Gemini client and authentication.
type GeminiState struct {
	GoogleOauthConfig *oauth2.Config
	OAuthState        string
	Token             *oauth2.Token
	GeminiClient      *CodeAssistClient
	ProjectID         string
	SelectedAuthType  AuthType
	UserEmail         string
}

// Define CodeAssistClient struct
type CodeAssistClient struct {
	client    *http.Client
	projectID string
}

// NewCodeAssistClient creates a new instance of CodeAssistClient.
func NewCodeAssistClient(httpClient *http.Client, projectID string) *CodeAssistClient {
	return &CodeAssistClient{
		client:    httpClient,
		projectID: projectID,
	}
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

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream") // Indicate that we expect a stream

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close() // Close the body after reading for error logging
		return nil, fmt.Errorf("API response error: %s, response: %s", resp.Status, string(bodyBytes))
	}

	return resp.Body, nil // Return the response body directly
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

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:countTokens"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API response error: %s, response: %s", resp.Status, string(bodyBytes))
	}

	var caResp CaCountTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&caResp); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &caResp, nil
}

// LoadCodeAssist calls the loadCodeAssist of Code Assist API.
func (c *CodeAssistClient) LoadCodeAssist(ctx context.Context, req LoadCodeAssistRequest) (*LoadCodeAssistResponse, error) {
	jsonBody, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API response error: %s, response: %s", resp.Status, string(bodyBytes))
	}

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
	jsonBody, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:onboardUser"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API response error: %s, response: %s", resp.Status, string(bodyBytes))
	}

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
	var httpClient *http.Client

	switch gs.SelectedAuthType {
	case AuthTypeLoginWithGoogle:
		if gs.Token == nil || !gs.Token.Valid() {
			log.Println("OAuth token is invalid. Login required.")
			return
		}
		httpClient = gs.GoogleOauthConfig.Client(ctx, gs.Token)
		caClient := NewCodeAssistClient(httpClient, "") // Initialize with empty projectID for now

		// LoadCodeAssist
		loadReq := LoadCodeAssistRequest{
			CloudaicompanionProject: gs.ProjectID,
			Metadata: &ClientMetadata{
				IdeType:     "IDE_UNSPECIFIED",
				Platform:    "PLATFORM_UNSPECIFIED",
				PluginType:  "GEMINI",
				DuetProject: gs.ProjectID,
			},
		}
		loadRes, loadErr := caClient.LoadCodeAssist(ctx, loadReq)
		if loadErr != nil {
			log.Printf("InitGeminiClient: Failed to load code assist: %v", loadErr)
			gs.GeminiClient = nil
			return
		}

		// Update ProjectID after LoadCodeAssist call
		if loadRes.CloudaicompanionProject != "" {
			gs.ProjectID = loadRes.CloudaicompanionProject
		}

		var userTierID UserTierID
		if loadRes.CurrentTier != nil {
			userTierID = loadRes.CurrentTier.ID
		} else {
			// Find default tier if currentTier is not set
			for _, tier := range loadRes.AllowedTiers {
				if tier.IsDefault != nil && *tier.IsDefault {
					userTierID = tier.ID
					break
				}
			}
		}

		// OnboardUser
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
			log.Printf("InitGeminiClient: Failed to onboard user: %v", onboardErr)
			gs.GeminiClient = nil
			return
		}

		if lroRes.Response != nil && lroRes.Response.CloudaicompanionProject != nil {
			gs.ProjectID = lroRes.Response.CloudaicompanionProject.ID
		} else {
			log.Println("InitGeminiClient: No project ID returned from onboardUser, Gemini client not initialized.")
			gs.GeminiClient = nil
			return
		}
	case AuthTypeUseGemini:
		log.Println("Gemini API Key method does not use Code Assist API.")
		return
	case AuthTypeUseVertexAI:
		log.Println("Vertex AI method does not use Code Assist API.")
		return
	case AuthTypeCloudShell:
		httpClient = &http.Client{}
	default:
		log.Fatalf("Unsupported authentication type: %s", gs.SelectedAuthType)
	}

	gs.GeminiClient = NewCodeAssistClient(httpClient, gs.ProjectID)

	log.Println("CodeAssist client initialized.")
}

// saveToken saves the OAuth token to the database.
func (gs *GeminiState) SaveToken(t *oauth2.Token) {
	tokenJSON, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal token: %v", err)
		return
	}

	if err := SaveOAuthToken(string(tokenJSON)); err != nil {
		log.Printf("Failed to save OAuth token to DB: %v", err)
		return
	}
	log.Println("OAuth token saved to DB.")
	gs.Token = t // Update global token variable
}

// loadToken loads the OAuth token from the database.
func (gs *GeminiState) LoadToken() {
	tokenJSON, err := LoadOAuthToken()
	if err != nil {
		log.Printf("Failed to load OAuth token from DB: %v", err)
		return
	}

	if tokenJSON == "" {
		log.Println("No existing token in DB.")
		return
	}

	if err := json.Unmarshal([]byte(tokenJSON), &gs.Token); err != nil {
		log.Printf("Failed to decode token from DB: %v", err)
		return
	}
	log.Println("OAuth token loaded from DB.")
}
