package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	. "github.com/lifthrasiir/angel/gemini"
)

// Ensure CodeAssistProvider implements LLMProvider
var _ LLMProvider = (*CodeAssistProvider)(nil)

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

	db, err := getDbFromContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get database from context: %w", err)
	}

	type StreamResult struct {
		iter.Seq[GenerateContentResponse]
		io.Closer
	}

	apiCallback := func(geminiClient *GeminiAPIClient, config GeminiAPIConfig) (out StreamResult, err error) {
		// Convert SessionParams to GenerateContentRequest
		request := convertSessionParamsToGenerateRequest(model, params)

		respBody, err := geminiClient.StreamGenerateContent(ctx, apiModelName, request)
		if err != nil {
			err = fmt.Errorf("failed to call Gemini API: %w", err)
			return
		}

		dec := json.NewDecoder(respBody)

		// Gemini API returns a JSON array of responses
		// Read the opening bracket of the JSON array
		_, err = dec.Token()
		if err != nil {
			respBody.Close()
			log.Printf("GeminiAPIProvider.SendMessageStream: Expected opening bracket '[', but got %v", err)
			err = fmt.Errorf("expected opening bracket '[', but got %w", err)
			return
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

		out.Seq = seq
		out.Closer = respBody
		return
	}

	codeAssistCallback := func(client *CodeAssistClient) (out StreamResult, err error) {
		// Use Code Assist API with the provided client
		request := convertSessionParamsToGenerateRequest(model, params)
		respBody, err := client.StreamGenerateContent(ctx, apiModelName, request)
		if err != nil {
			log.Printf("SendMessageStream: streamGenerateContent failed: %v", err)
			return
		}

		dec := json.NewDecoder(respBody)

		// Code Assist returns a JSON array of responses
		_, err = dec.Token()
		if err != nil {
			respBody.Close()
			log.Printf("SendMessageStream: Expected opening bracket '[', but got %v", err)
			err = fmt.Errorf("expected opening bracket '[', but got %w", err)
			return
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

		out.Seq = seq
		out.Closer = respBody
		return
	}

	out, err := tryAllProviders(db, model, apiCallback, codeAssistCallback)
	if err != nil {
		return nil, nil, err
	}
	return out.Seq, out.Closer, nil
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

	apiCallback := func(client *GeminiAPIClient, config GeminiAPIConfig) (*CaCountTokenResponse, error) {
		return client.CountTokens(ctx, apiModelName, contents)
	}
	codeAssistCallback := func(client *CodeAssistClient) (*CaCountTokenResponse, error) {
		return client.CountTokens(ctx, apiModelName, contents)
	}
	return tryAllProviders(db, model, apiCallback, codeAssistCallback)
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

func tryAllProviders[T any](
	db *sql.DB,
	model *Model,
	apiCallback func(*GeminiAPIClient, GeminiAPIConfig) (T, error),
	codeAssistCallback func(*CodeAssistClient) (T, error),
) (out T, err error) {
	for _, providerType := range model.Providers {
		switch providerType {
		case "api":
			var configs []GeminiAPIConfig
			configs, err = GetGeminiAPIConfigs(db)
			if err != nil {
				err = fmt.Errorf("failed to get Gemini API configs: %w", err)
				return
			}

			apiModelName := model.ModelName

			// Sort configs by last_used for this model (oldest first)
			sort.Slice(configs, func(i, j int) bool {
				return configs[i].LastUsedByModel[apiModelName].Before(configs[j].LastUsedByModel[apiModelName])
			})

			for _, config := range configs {
				if !config.Enabled {
					continue
				}

				// Create client
				clientProvider := NewGeminiAPIHTTPClientProvider(config.APIKey)
				client := NewGeminiAPIClient(clientProvider, config.APIKey)

				// Update last_used for this model
				UpdateModelLastUsed(db, config.ID, apiModelName)

				out, err = apiCallback(client, config)
				if err == nil {
					return
				}

				// Check rate limit
				if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
					// Handle rate limit
					go func() {
						retryAfter := parseRetryAfter("")
						HandleModelRateLimit(db, config.ID, apiModelName, retryAfter)
						log.Printf("Rate limit detected for Gemini API config %s, model %s", config.ID, apiModelName)
					}()
					err = fmt.Errorf("rate limited for model %s", apiModelName)
				}
			}

		case "geminicli", "antigravity":
			// Handle OAuth-based providers
			var tokens []OAuthToken
			tokens, err = GetOAuthTokens(db)
			if err != nil {
				err = fmt.Errorf("failed to get OAuth tokens: %w", err)
				return
			}

			// Filter tokens for the current provider kind and sort by last_used for this model (oldest first)
			var providerTokens []OAuthToken
			for _, token := range tokens {
				if token.Kind == providerType {
					providerTokens = append(providerTokens, token)
				}
			}

			apiModelName := model.ModelName

			sort.Slice(providerTokens, func(i, j int) bool {
				return providerTokens[i].LastUsedByModel[apiModelName].Before(providerTokens[j].LastUsedByModel[apiModelName])
			})

			for _, token := range providerTokens {
				// Parse OAuth token
				var oauthToken oauth2.Token
				if err := json.Unmarshal([]byte(token.TokenData), &oauthToken); err != nil {
					log.Printf("Failed to unmarshal OAuth token %d: %v", token.ID, err)
					continue
				}

				// Create token source and client (oauth2.TokenSource will handle refresh automatically)
				oauthConfig := GlobalGeminiAuth.getOAuthConfig(providerType)
				tokenSource := oauthConfig.TokenSource(context.Background(), &oauthToken)
				client := NewCodeAssistClient(&tokenSourceProvider{TokenSource: tokenSource}, token.ProjectID, providerType)

				// Update last_used for this model
				UpdateOAuthTokenModelLastUsed(db, token.ID, apiModelName)

				out, err = codeAssistCallback(client)
				if err == nil {
					return
				}

				// Check rate limit
				if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
					// Handle rate limit
					go func() {
						retryAfter := parseRetryAfter("")
						HandleOAuthRateLimit(db, token.ID, apiModelName, retryAfter)
						log.Printf("Rate limit detected for OAuth token %d, model %s", token.ID, apiModelName)
					}()
					err = fmt.Errorf("rate limited for model %s", apiModelName)
				}
			}

		default:
			log.Panicf("Unknown provider type: %s", providerType)
		}
	}

	// If we reach here, all providers failed. Return the last error if available, otherwise create a generic error.
	if err == nil {
		err = fmt.Errorf("all providers failed for model %s", model.ModelName)
	}
	return
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
