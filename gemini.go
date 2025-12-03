package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	. "github.com/lifthrasiir/angel/gemini"
)

// Ensure CodeAssistProvider implements LLMProvider
var _ LLMProvider = (*CodeAssistProvider)(nil)

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
	return oauth2.NewClient(ctx, ts.TokenSource)
}

type CodeAssistProvider struct {
	client *CodeAssistClient
	models map[string]*Model
}

// NewCodeAssistProvider creates a new CodeAssistProvider with the given models and client
func NewCodeAssistProvider(models map[string]*Model, client *CodeAssistClient) *CodeAssistProvider {
	return &CodeAssistProvider{
		client: client,
		models: models,
	}
}

// APIType defines which API to use
type APIType string

const (
	APITypeCodeAssist APIType = "codeassist"
	APITypeGemini     APIType = "gemini"
)

// Reserved names for Gemini's internal tools.
const (
	GeminiUrlContextToolName    = ".url_context"
	GeminiCodeExecutionToolName = ".code_execution"
)

// resolveModelName resolves the model key to the actual API model name
func (cap *CodeAssistProvider) resolveModel(modelName string) (*Model, string) {
	if model, exists := cap.models[modelName]; exists {
		apiModelName := model.ModelName
		if apiModelName != "" {
			return model, apiModelName
		}
	}
	return nil, "" // Return nil to indicate model not found
}

// SendMessageStream calls the streamGenerateContent of Code Assist API and returns an iter.Seq of responses.
func (cap *CodeAssistProvider) SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	// Resolve the model to get both model info and API model name
	model, apiModelName := cap.resolveModel(modelName)
	if model == nil {
		return nil, nil, fmt.Errorf("unsupported model: %s", modelName)
	}

	// Determine which API to use
	apiType, err := cap.selectAPIType(ctx)
	if err != nil {
		log.Printf("SendMessageStream: Failed to select API type: %v", err)
		// Fall back to Code Assist
		apiType = APITypeCodeAssist
	}

	switch apiType {
	case APITypeGemini:
		// Use Gemini API
		geminiClient, config, err := cap.createGeminiAPIClient(ctx, apiModelName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create Gemini API client: %w", err)
		}

		// Convert SessionParams to GenerateContentRequest
		request := convertSessionParamsToGenerateRequest(model, params)

		respBody, err := geminiClient.StreamGenerateContent(ctx, apiModelName, request)
		if err != nil {
			// Check rate limit
			if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
				// Handle rate limit
				go func() {
					retryAfter := parseRetryAfter("")
					db, _ := getDbFromContext(ctx)
					HandleModelRateLimit(db, config.ID, apiModelName, retryAfter)
					log.Printf("Rate limit detected for Gemini API config %s, model %s", config.ID, apiModelName)
				}()
				return nil, nil, fmt.Errorf("rate limited for model %s", apiModelName)
			}
			return nil, nil, fmt.Errorf("failed to call Gemini API: %w", err)
		}

		dec := json.NewDecoder(respBody)

		// Gemini API returns a JSON array of responses
		// Read the opening bracket of the JSON array
		_, err = dec.Token()
		if err != nil {
			respBody.Close()
			log.Printf("GeminiAPIProvider.SendMessageStream: Expected opening bracket '[', but got %v", err)
			return nil, nil, fmt.Errorf("expected opening bracket '[', but got %w", err)
		}

		// Create an iter.Seq that yields GenerateContentResponse
		seq := func(yield func(GenerateContentResponse) bool) {
			for dec.More() {
				var geminiResp GenerateContentResponse
				if err := dec.Decode(&geminiResp); err != nil {
					log.Printf("GeminiAPIProvider.SendMessageStream: Failed to decode JSON object from stream: %v", err)
					return
				}

				if !yield(geminiResp) {
					return
				}
			}
		}

		return seq, respBody, nil

	case APITypeCodeAssist:
		// Use Code Assist API (existing logic)
		request := convertSessionParamsToGenerateRequest(model, params)
		respBody, err := cap.client.StreamGenerateContent(ctx, apiModelName, request)
		if err != nil {
			log.Printf("SendMessageStream: streamGenerateContent failed: %v", err)
			return nil, nil, err
		}

		dec := json.NewDecoder(respBody)

		// Code Assist returns a JSON array of responses
		_, err = dec.Token()
		if err != nil {
			respBody.Close()
			log.Printf("SendMessageStream: Expected opening bracket '[', but got %v", err)
			return nil, nil, fmt.Errorf("expected opening bracket '[', but got %w", err)
		}

		// Create an iter.Seq that yields GenerateContentResponse
		seq := func(yield func(GenerateContentResponse) bool) {
			for dec.More() {
				var caResp CaGenerateContentResponse
				if err := dec.Decode(&caResp); err != nil {
					log.Printf("SendMessageStream: Failed to decode JSON object from stream: %v", err)
					return
				}

				if !yield(caResp.Response) {
					return
				}
			}
		}

		return seq, respBody, nil

	default:
		return nil, nil, fmt.Errorf("unsupported API type: %s", apiType)
	}
}

// GenerateContentOneShot calls the streamGenerateContent of Code Assist API and returns a single response.
func (cap *CodeAssistProvider) GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error) {
	seq, closer, err := cap.SendMessageStream(ctx, modelName, params)
	if err != nil {
		log.Printf("GenerateContentOneShot: SendMessageStream failed: %v", err)
		return OneShotResult{}, err
	}
	defer closer.Close()

	var fullResponse strings.Builder // Use a strings.Builder for efficient concatenation
	var urlContextMeta *URLContextMetadata
	var groundingMetadata *GroundingMetadata

	for resp := range seq {
		if len(resp.Candidates) > 0 {
			candidate := resp.Candidates[0]
			if len(candidate.Content.Parts) > 0 {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" { // Check if it's a text part
						fullResponse.WriteString(part.Text)
					}
					// TODO: Handle other part types if necessary (e.g., function_call, function_response)
				}
			}
			// Extract metadata from the last candidate (assuming it's cumulative or final)
			if candidate.URLContextMetadata != nil {
				urlContextMeta = candidate.URLContextMetadata
			}
			if candidate.GroundingMetadata != nil {
				groundingMetadata = candidate.GroundingMetadata
			}
		}
	}

	if fullResponse.Len() > 0 {
		return OneShotResult{
			Text:               fullResponse.String(),
			URLContextMetadata: urlContextMeta,
			GroundingMetadata:  groundingMetadata,
		}, nil
	}

	return OneShotResult{}, fmt.Errorf("no text content found in LLM response")
}

func (cap *CodeAssistProvider) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
	// Resolve the model name to the actual API model name
	model, apiModelName := cap.resolveModel(modelName)
	if model == nil {
		return nil, fmt.Errorf("unsupported model: %s", modelName)
	}

	// Get database from context
	db, err := getDbFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database from context: %w", err)
	}

	// Check if Gemini API is available
	configs, err := GetGeminiAPIConfigs(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get Gemini API configs: %w", err)
	}

	if len(configs) == 0 {
		// Use Code Assist API
		return cap.client.CountTokens(ctx, apiModelName, contents)
	}

	// Use Gemini API
	// Get next available API key for the model
	config, err := GetNextGeminiAPIConfig(db, apiModelName)
	if err != nil {
		return nil, fmt.Errorf("failed to get Gemini API config: %w", err)
	}
	if config == nil {
		// No available configs, fallback to Code Assist API
		log.Printf("No available Gemini API configs, falling back to Code Assist API for model %s", apiModelName)
		return cap.client.CountTokens(ctx, apiModelName, contents)
	}

	// Update last_used timestamp
	defer UpdateModelLastUsed(db, config.ID, apiModelName)

	// Create Gemini API client
	clientProvider := NewGeminiAPIHTTPClientProvider(config.APIKey)
	client := NewGeminiAPIClient(clientProvider, config.APIKey)

	// Call Gemini API
	response, err := client.CountTokens(ctx, apiModelName, contents)
	if err != nil {
		// Check rate limit
		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
			// Handle rate limit
			go func() {
				retryAfter := parseRetryAfter("")
				HandleModelRateLimit(db, config.ID, apiModelName, retryAfter)
				log.Printf("Rate limit detected for Gemini API config %s, model %s", config.ID, apiModelName)
			}()
		}
		// Fallback to Code Assist API
		log.Printf("Gemini API CountTokens failed for model %s, falling back to Code Assist API: %v", apiModelName, err)
		return cap.client.CountTokens(ctx, apiModelName, contents)
	}

	return response, nil
}

// MaxTokens implements the LLMProvider interface for CodeAssistProvider.
func (cap *CodeAssistProvider) MaxTokens(modelName string) int {
	// Use model information from ModelRegistry models map
	if model, exists := cap.models[modelName]; exists {
		return model.MaxTokens
	}
	return 0
}

// parseRetryAfter parses Retry-After header
func parseRetryAfter(retryAfter string) time.Duration {
	if retryAfter == "" {
		return 60 * time.Second // default 60 seconds
	}

	// Try to parse as seconds
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try to parse as HTTP date
	if t, err := http.ParseTime(retryAfter); err == nil {
		return time.Until(t)
	}

	// Fallback to 60 seconds
	return 60 * time.Second
}

// selectAPIType determines which API to use based on available configurations
func (cap *CodeAssistProvider) selectAPIType(ctx context.Context) (APIType, error) {
	// Get database from context
	db, err := getDbFromContext(ctx)
	if err != nil {
		return APITypeCodeAssist, fmt.Errorf("failed to get database from context: %w", err)
	}

	// Check if Gemini API configs are available
	configs, err := GetGeminiAPIConfigs(db)
	if err != nil {
		log.Printf("Failed to load Gemini API configs, falling back to Code Assist: %v", err)
		return APITypeCodeAssist, nil
	}

	// Check if there are enabled Gemini API configs
	for _, config := range configs {
		if config.Enabled {
			return APITypeGemini, nil
		}
	}

	// No Gemini API configs available, use Code Assist
	return APITypeCodeAssist, nil
}

// createGeminiAPIClient creates a Gemini API client with appropriate API key selection
func (cap *CodeAssistProvider) createGeminiAPIClient(ctx context.Context, modelName string) (*GeminiAPIClient, *GeminiAPIConfig, error) {
	// Get database from context
	db, err := getDbFromContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get database from context: %w", err)
	}

	// Select API key for this model
	config, err := GetNextGeminiAPIConfig(db, modelName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get Gemini API config: %w", err)
	}
	if config == nil {
		return nil, nil, fmt.Errorf("no enabled Gemini API config found")
	}

	// Create client
	clientProvider := NewGeminiAPIHTTPClientProvider(config.APIKey)
	client := NewGeminiAPIClient(clientProvider, config.APIKey)

	// Update last_used for this model
	UpdateModelLastUsed(db, config.ID, modelName)

	return client, config, nil
}

// NewGeminiAPIHTTPClientProvider creates a simple HTTP client provider for Gemini API
func NewGeminiAPIHTTPClientProvider(apiKey string) HTTPClientProvider {
	return &geminiAPIHTTPClientProvider{apiKey: apiKey}
}

type geminiAPIHTTPClientProvider struct {
	apiKey string
}

func (p *geminiAPIHTTPClientProvider) Client(ctx context.Context) *http.Client {
	// For Gemini API, we just need a basic HTTP client
	// The API key is added to the URL
	return &http.Client{}
}

// convertSessionParamsToGenerateRequest converts SessionParams to GenerateContentRequest
func convertSessionParamsToGenerateRequest(model *Model, params SessionParams) GenerateContentRequest {
	var systemInstruction *Content
	if params.SystemPrompt != "" && !model.IgnoreSystemPrompt {
		systemInstruction = &Content{
			Parts: []Part{
				{Text: params.SystemPrompt},
			},
		}
	}

	var tools []Tool
	if model.ToolSupported {
		tools = GetToolsForGemini()
		if params.ToolConfig != nil {
			if _, ok := params.ToolConfig[""]; ok {
				// Remove the default tool list
				tools = nil
			}
			if _, ok := params.ToolConfig[GeminiUrlContextToolName]; ok {
				// Add a new Tool with URLContext
				tools = append(tools, Tool{URLContext: &URLContext{}})
			}
			if _, ok := params.ToolConfig[GeminiCodeExecutionToolName]; ok {
				// Add a new Tool with CodeExecution
				tools = append(tools, Tool{CodeExecution: &CodeExecution{}})
			}
		}
	}

	var thinkingConfig *ThinkingConfig
	if params.IncludeThoughts && model.ThoughtEnabled {
		thinkingConfig = &ThinkingConfig{
			IncludeThoughts: true,
		}
	}

	var temperature, topP *float32
	var topK *int32
	if model.GenParams.Temperature >= 0 {
		temperature = &model.GenParams.Temperature
	}
	if model.GenParams.TopP >= 0 {
		topP = &model.GenParams.TopP
	}
	if model.GenParams.TopK >= 0 {
		topK = &model.GenParams.TopK
	}

	return GenerateContentRequest{
		Contents:          params.Contents,
		SystemInstruction: systemInstruction,
		Tools:             tools,
		GenerationConfig: &GenerationConfig{
			ThinkingConfig:     thinkingConfig,
			Temperature:        temperature,
			TopP:               topP,
			TopK:               topK,
			ResponseModalities: model.ResponseModalities,
		},
	}
}
