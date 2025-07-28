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
func (c *CodeAssistClient) streamGenerateContent(ctx context.Context, contents []Content, modelName string) (io.ReadCloser, error) {
	reqBody := CAGenerateContentRequest{
		Model:   modelName,
		Project: c.projectID,
		Request: VertexGenerateContentRequest{
			Contents: contents,
			SystemInstruction: &Content{
				Parts: []Part{
					{Text: GetCoreSystemPrompt()},
				},
			},
			Tools: []Tool{
				{
					FunctionDeclarations: []FunctionDeclaration{
						{
							Name:        "list_directory",
							Description: "Lists a directory.",
							Parameters: &Schema{
								Type: TypeObject,
								Properties: map[string]*Schema{
									"path": {
										Type:        TypeString,
										Description: "The absolute path to the directory to list.",
									},
								},
								Required: []string{"path"},
							},
						},
					},
				},
			},
			GenerationConfig: &GenerationConfig{
				ThinkingConfig: &ThinkingConfig{
					IncludeThoughts: true,
				},
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
func (c *CodeAssistClient) SendMessageStream(ctx context.Context, contents []Content, modelName string) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	respBody, err := c.streamGenerateContent(ctx, contents, modelName)
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

// Declare global variables (accessible from main.go)
var (
	GoogleOauthConfig *oauth2.Config
	OAuthState        = "random"
	Token             *oauth2.Token
	GeminiClient      *CodeAssistClient // Changed from CodeAssistClient to GeminiClient

	ProjectID        string
	SelectedAuthType AuthType
)

// InitGeminiClient initializes the CodeAssistClient.
func InitGeminiClient() {
	ctx := context.Background()
	var httpClient *http.Client
	var err error // Declare err here

	switch SelectedAuthType {
	case AuthTypeLoginWithGoogle:
		if Token == nil || !Token.Valid() {
			log.Println("OAuth token is invalid. Login required.")
			return
		}
		// If ProjectID is not set, try to fetch it using the existing token
		if ProjectID == "" {
			log.Println("InitGeminiClient: ProjectID is empty, attempting to fetch using existing token.")
			if err = FetchProjectID(Token); err != nil {
				log.Printf("InitGeminiClient: Failed to retrieve Project ID using existing token: %v", err)
				// Do not return, allow GeminiClient to be nil if ProjectID cannot be fetched
			} else {
				log.Printf("InitGeminiClient: Successfully fetched Project ID: %s", ProjectID)
			}
		}
		if ProjectID != "" { // Only proceed if ProjectID is now available
			httpClient = GoogleOauthConfig.Client(ctx, Token)
		} else {
			log.Println("InitGeminiClient: ProjectID still empty, Gemini client not initialized.")
			GeminiClient = nil
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
		log.Fatalf("Unsupported authentication type: %s", SelectedAuthType)
	}

	GeminiClient = NewCodeAssistClient(httpClient, ProjectID)

	log.Println("CodeAssist client initialized.")
}

// saveToken saves the OAuth token to the database.
func SaveToken(t *oauth2.Token) {
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
	Token = t // Update global token variable
}

// loadToken loads the OAuth token from the database.
func LoadToken() {
	tokenJSON, err := LoadOAuthToken()
	if err != nil {
		log.Printf("Failed to load OAuth token from DB: %v", err)
		return
	}

	if tokenJSON == "" {
		log.Println("No existing token in DB.")
		return
	}

	if err := json.Unmarshal([]byte(tokenJSON), &Token); err != nil {
		log.Printf("Failed to decode token from DB: %v", err)
		return
	}
	log.Println("OAuth token loaded from DB.")
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
