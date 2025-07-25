package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
)

// AuthType enum definition (matches AuthType in TypeScript)
type AuthType string

const (
	AuthTypeLoginWithGoogle AuthType = "oauth-personal"
	AuthTypeUseGemini       AuthType = "gemini-api-key"
	AuthTypeUseVertexAI     AuthType = "vertex-ai"
	AuthTypeCloudShell      AuthType = "cloud-shell"
)

// Define Go structs to match the structs defined in gemini-cli's converter.ts
type Part struct {
	Text string `json:"text,omitempty"`
	// TODO: Add other part types like inlineData, functionCall, functionResponse if needed
}

type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type VertexGenerateContentRequest struct {
	Contents          []Content `json:"contents"`
	SystemInstruction *Content  `json:"systemInstruction,omitempty"`
	SessionID         string    `json:"session_id,omitempty"`
	// TODO: Add other fields from VertexGenerateContentRequest if needed
}

type CAGenerateContentRequest struct {
	Model   string                       `json:"model"`
	Project string                       `json:"project,omitempty"`
	Request VertexGenerateContentRequest `json:"request"`
}

type Candidate struct {
	Content Content `json:"content"`
	// TODO: Add other fields from Candidate if needed
}

type VertexGenerateContentResponse struct {
	Candidates []Candidate `json:"candidates"`
}

type CaGenerateContentResponse struct {
	Response VertexGenerateContentResponse `json:"response"`
}

type VertexCountTokenRequest struct {
	Model    string    `json:"model"`
	Contents []Content `json:"contents"`
}

type CaCountTokenRequest struct {
	Request VertexCountTokenRequest `json:"request"`
}

type CaCountTokenResponse struct {
	TotalTokens int `json:"totalTokens"`
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

// SendMessageStream calls the streamGenerateContent of Code Assist API.
func (c *CodeAssistClient) SendMessageStream(ctx context.Context, contents []Content, modelName string) (io.ReadCloser, error) {
	reqBody := CAGenerateContentRequest{
		Model:   modelName,
		Project: c.projectID,
		Request: VertexGenerateContentRequest{
			Contents: contents,
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

// Define ChatSession struct (used instead of genai.ChatSession)
type ChatSession struct {
	History []*Content
}

// GeminiEventType enum definition (matches GeminiEventType in TypeScript)
type GeminiEventType string

const (
	GeminiEventTypeContent              GeminiEventType = "content"
	GeminiEventTypeToolCode             GeminiEventType = "tool_code"
	GeminiEventTypeToolCallConfirmation GeminiEventType = "tool_call_confirmation"
	GeminiEventTypeToolCallResponse     GeminiEventType = "tool_call_response"
	GeminiTypeError                     GeminiEventType = "error"
	GeminiTypeFinished                  GeminiEventType = "finished"
)

// ServerGeminiContentEvent matches ServerGeminiContentEvent in TypeScript
type ServerGeminiContentEvent struct {
	Type  GeminiEventType `json:"type"`
	Value Content         `json:"value"`
}

// ServerGeminiFinishedEvent matches ServerGeminiFinishedEvent in TypeScript
type ServerGeminiFinishedEvent struct {
	Type GeminiEventType `json:"type"`
}

// ServerGeminiErrorEvent matches ServerGeminiErrorEvent in TypeScript
type ServerGeminiErrorEvent struct {
	Type  GeminiEventType `json:"type"`
	Value struct {
		Message string `json:"message"`
	} `json:"value"`
}

// Declare global variables (accessible from main.go)
var (
	GoogleOauthConfig *oauth2.Config
	OAuthState        = "random"
	Token             *oauth2.Token
	GeminiClient      *CodeAssistClient // Changed from CodeAssistClient to GeminiClient
	ChatSessions      = make(map[string]*ChatSession)
	ProjectID         string
	SelectedAuthType  AuthType
)

// InitGeminiClient initializes the CodeAssistClient.
func InitGeminiClient() {
	ctx := context.Background()
	var httpClient *http.Client

	switch SelectedAuthType {
	case AuthTypeLoginWithGoogle:

		if Token == nil || !Token.Valid() {
			log.Println("OAuth token is invalid. Login required.")
			return
		}
		httpClient = GoogleOauthConfig.Client(ctx, Token)
	case AuthTypeUseGemini:
		log.Println("Gemini API Key method does not use Code Assist API.")
		return
	case AuthTypeUseVertexAI:
		log.Println("Vertex AI method does not use Code Assist API.")
		return
	case AuthTypeCloudShell:
		httpClient = &http.Client{}
	default:
		log.Fatalf("Unsupported authentication type: %s", SelectedAuthType)
	}

	GeminiClient = NewCodeAssistClient(httpClient, ProjectID) // Changed from CodeAssistClient to GeminiClient

	log.Println("CodeAssist client initialized.")
}

// saveToken saves the OAuth token to a file.
func SaveToken(t *oauth2.Token) {
	file, err := os.Create("oauth_token.json")
	if err != nil {
		log.Printf("Failed to create token file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(t); err != nil {
		log.Printf("Failed to save token: %v", err)
	}
	log.Println("OAuth token saved to oauth_token.json.")
	Token = t // Update global token variable
}

// loadToken loads the OAuth token from a file.
func LoadToken() {
	file, err := os.Open("oauth_token.json")
	if err != nil {
		log.Printf("No existing token file: %v", err)
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&Token); err != nil {
		log.Printf("Failed to decode token: %v", err)
		return
	}
	log.Println("OAuth token loaded.")
}

// fetchProjectID retrieves the Google Cloud Project ID with the OAuth token.
func FetchProjectID(t *oauth2.Token) error {
	client := GoogleOauthConfig.Client(context.Background(), t)
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
		ProjectID = projectsResponse.Projects[0].ProjectID
		log.Printf("Project ID set: %s", ProjectID)
		return nil
	}

	return fmt.Errorf("no active project found.")
}
