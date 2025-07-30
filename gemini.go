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
			SystemInstruction: &Content{
				Parts: []Part{
					{Text: systemPrompt},
				},
			},
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

// InitGeminiClient initializes the CodeAssistClient.
func (gs *GeminiState) InitGeminiClient() {
	ctx := context.Background()
	var httpClient *http.Client
	var err error // Declare err here

	switch gs.SelectedAuthType {
	case AuthTypeLoginWithGoogle:
		if gs.Token == nil || !gs.Token.Valid() {
			log.Println("OAuth token is invalid. Login required.")
			return
		}
		// If ProjectID is not set, try to fetch it using the existing token
		if gs.ProjectID == "" {
			log.Println("InitGeminiClient: ProjectID is empty, attempting to fetch using existing token.")
			if err = gs.FetchProjectID(gs.Token); err != nil {
				log.Printf("InitGeminiClient: Failed to retrieve Project ID using existing token: %v", err)
				// Do not return, allow GeminiClient to be nil if ProjectID cannot be fetched
			} else {
				log.Printf("InitGeminiClient: Successfully fetched Project ID: %s", gs.ProjectID)
			}
		}
		if gs.ProjectID != "" { // Only proceed if ProjectID is now available
			httpClient = gs.GoogleOauthConfig.Client(ctx, gs.Token)
		} else {
			log.Println("InitGeminiClient: ProjectID still empty, Gemini client not initialized.")
			gs.GeminiClient = nil
			return // Return early if ProjectID is not available
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

// fetchProjectID retrieves the Google Cloud Project ID with the OAuth token.
func (gs *GeminiState) FetchProjectID(t *oauth2.Token) error {
	client := gs.GoogleOauthConfig.Client(context.Background(), t)
	resp, err := client.Get("https://cloudresourcemanager.googleapis.com/v1/projects?filter=lifecycleState:ACTIVE")
	if err != nil {
		return fmt.Errorf("failed to fetch project list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("project list API error: %s, response: %s", resp.Status, string(bodyBytes))
	}

	var projectsResponse struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&projectsResponse); err != nil {
		return fmt.Errorf("failed to parse project response: %w", err)
	}

	if len(projectsResponse.Projects) > 0 {
		gs.ProjectID = projectsResponse.Projects[0].ProjectID
		log.Printf("Project ID set: %s", gs.ProjectID)
		return nil
	}

	return fmt.Errorf("no active project found.")
}
