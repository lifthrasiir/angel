package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/llm/spec"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// Ensure providers implement LLMProvider
var _ LLMProvider = (*GeminiAPIProvider)(nil)
var _ LLMProvider = (*CodeAssistProvider)(nil)

// Reserved names for Gemini's internal tools.
const (
	GeminiUrlContextToolName    = ".url_context"
	GeminiCodeExecutionToolName = ".code_execution"
)

// GeminiAPIProvider handles the "api" provider type
type GeminiAPIProvider struct {
	models *Models
}

// NewGeminiAPIProvider creates a new GeminiAPIProvider
func NewGeminiAPIProvider(models *Models) *GeminiAPIProvider {
	return &GeminiAPIProvider{models: models}
}

// SendMessageStream calls the Gemini API and returns an iter.Seq of responses
// The modelName parameter is already the internal model name (e.g., "gemini-3-flash-preview")
// as resolved by the Models registry from the provider-model pair in models.json
func (p *GeminiAPIProvider) SendMessageStream(ctx context.Context, apiModelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	if apiModelName == "" {
		return nil, nil, fmt.Errorf("model name cannot be empty")
	}

	db, err := database.FromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	tools, err := tool.FromContext(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Get model configuration from models registry
	model := p.models.GetModel(apiModelName)

	configs, err := database.GetGeminiAPIConfigs(db)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get Gemini API configs: %w", err)
	}

	// Sort configs by last_used for this model (oldest first)
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].LastUsedByModel[apiModelName].Before(configs[j].LastUsedByModel[apiModelName])
	})

	var lastErr error
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		// Create client
		clientProvider := NewGeminiAPIHTTPClientProvider(config.APIKey)
		client := NewGeminiAPIClient(clientProvider, config.APIKey)

		// Update last_used for this model
		database.UpdateModelLastUsed(db, config.ID, apiModelName)

		// Convert SessionParams to GenerateContentRequest
		request := convertSessionParamsToGenerateRequest(tools, model, params)

		respBody, err := client.StreamGenerateContent(ctx, apiModelName, request)
		if err != nil {
			lastErr = err

			// Check rate limit
			if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
				// Handle rate limit
				go func() {
					retryAfter := parseRetryAfter("")
					database.HandleModelRateLimit(db, config.ID, apiModelName, retryAfter)
					log.Printf("Rate limit detected for Gemini API config %s, model %s", config.ID, apiModelName)
				}()
			}
			continue
		}

		dec := json.NewDecoder(respBody)

		// Gemini API returns a JSON array of responses
		// Read the opening bracket of the JSON array
		_, err = dec.Token()
		if err != nil {
			respBody.Close()
			log.Printf("GeminiAPIProvider.SendMessageStream: Expected opening bracket '[', but got %v", err)
			lastErr = fmt.Errorf("expected opening bracket '[', but got %w", err)
			continue
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
	}

	return nil, nil, lastErr
}

// GenerateContentOneShot calls the Gemini API and returns a single response
func (p *GeminiAPIProvider) GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error) {
	seq, closer, err := p.SendMessageStream(ctx, modelName, params)
	if err != nil {
		log.Printf("GenerateContentOneShot: SendMessageStream failed: %v", err)
		return OneShotResult{}, err
	}
	defer closer.Close()

	var fullResponse strings.Builder
	var urlContextMeta *URLContextMetadata
	var groundingMetadata *GroundingMetadata

	for resp := range seq {
		if len(resp.Candidates) > 0 {
			candidate := resp.Candidates[0]
			if len(candidate.Content.Parts) > 0 {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						fullResponse.WriteString(part.Text)
					}
				}
			}
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

func (p *GeminiAPIProvider) CountTokens(ctx context.Context, apiModelName string, contents []Content) (*CaCountTokenResponse, error) {
	if apiModelName == "" {
		return nil, fmt.Errorf("model name cannot be empty")
	}

	db, err := database.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	configs, err := database.GetGeminiAPIConfigs(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get Gemini API configs: %w", err)
	}

	// Sort configs by last_used for this model (oldest first)
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].LastUsedByModel[apiModelName].Before(configs[j].LastUsedByModel[apiModelName])
	})

	var lastErr error
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		clientProvider := NewGeminiAPIHTTPClientProvider(config.APIKey)
		client := NewGeminiAPIClient(clientProvider, config.APIKey)

		database.UpdateModelLastUsed(db, config.ID, apiModelName)

		result, err := client.CountTokens(ctx, apiModelName, contents)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// Check rate limit
		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
			go func() {
				retryAfter := parseRetryAfter("")
				database.HandleModelRateLimit(db, config.ID, apiModelName, retryAfter)
				log.Printf("Rate limit detected for Gemini API config %s, model %s", config.ID, apiModelName)
			}()
		}
	}

	return nil, lastErr
}

func (p *GeminiAPIProvider) MaxTokens(apiModelName string) int {
	// Return a default max tokens value
	// The actual max tokens is managed by the Models registry
	return 1048576
}

// CodeAssistProvider handles the "geminicli" and "antigravity" provider types
type CodeAssistProvider struct {
	providerKind string // "geminicli" or "antigravity"
	models       *Models
}

// NewCodeAssistProvider creates a new CodeAssistProvider for a specific provider kind
func NewCodeAssistProvider(providerKind string, models *Models) *CodeAssistProvider {
	return &CodeAssistProvider{
		providerKind: providerKind,
		models:       models,
	}
}

// SendMessageStream calls the Code Assist API and returns an iter.Seq of responses
// The modelName parameter is already the internal model name as resolved by Models registry
func (p *CodeAssistProvider) SendMessageStream(ctx context.Context, apiModelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	if apiModelName == "" {
		return nil, nil, fmt.Errorf("model name cannot be empty")
	}

	db, err := database.FromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	tools, err := tool.FromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	ga, err := GeminiAuthFromContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get GeminiAuth from context: %w", err)
	}

	tokens, err := database.GetOAuthTokensWithValidProjectID(db)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get OAuth tokens: %w", err)
	}

	// Filter tokens for the current provider kind and sort by last_used for this model (oldest first)
	var providerTokens []OAuthToken
	for _, token := range tokens {
		if token.Kind == p.providerKind {
			providerTokens = append(providerTokens, token)
		}
	}

	sort.Slice(providerTokens, func(i, j int) bool {
		return providerTokens[i].LastUsedByModel[apiModelName].Before(providerTokens[j].LastUsedByModel[apiModelName])
	})

	var lastErr error
	for _, token := range providerTokens {
		// Parse OAuth token
		var oauthToken oauth2.Token
		if err := json.Unmarshal([]byte(token.TokenData), &oauthToken); err != nil {
			log.Printf("Failed to unmarshal OAuth token %d: %v", token.ID, err)
			continue
		}

		// Create token source with database refresh hook
		oauthConfig := ga.OAuthConfig(p.providerKind)
		tokenSource := &databaseTokenSource{
			db:          db,
			tokenID:     token.ID,
			kind:        p.providerKind,
			userEmail:   token.UserEmail,
			projectID:   token.ProjectID,
			baseToken:   &oauthToken,
			tokenSource: oauthConfig.TokenSource(context.Background(), &oauthToken),
		}
		client := NewCodeAssistClient(TokenSourceClientProvider(tokenSource), token.ProjectID, p.providerKind)

		// Update last_used for this model
		database.UpdateOAuthTokenModelLastUsed(db, token.ID, apiModelName)

		// Get model configuration from models registry
		model := p.models.GetModel(apiModelName)

		// Convert SessionParams to GenerateContentRequest
		request := convertSessionParamsToGenerateRequest(tools, model, params)

		respBody, err := client.StreamGenerateContent(ctx, apiModelName, request)
		if err != nil {
			lastErr = err

			// Check rate limit
			if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
				go func() {
					retryAfter := parseRetryAfter("")
					database.HandleOAuthRateLimit(db, token.ID, apiModelName, retryAfter)
					log.Printf("Rate limit detected for OAuth token %d, model %s", token.ID, apiModelName)
				}()
			}
			continue
		}

		dec := json.NewDecoder(respBody)

		// Code Assist returns a JSON array of responses
		_, err = dec.Token()
		if err != nil {
			respBody.Close()
			log.Printf("CodeAssistProvider.SendMessageStream: Expected opening bracket '[', but got %v", err)
			lastErr = fmt.Errorf("expected opening bracket '[', but got %w", err)
			continue
		}

		// Create an iter.Seq that yields GenerateContentResponse
		seq := func(yield func(GenerateContentResponse) bool) {
			for dec.More() {
				var caResp CaGenerateContentResponse
				if err := dec.Decode(&caResp); err != nil {
					log.Printf("CodeAssistProvider.SendMessageStream: Failed to decode JSON object from stream: %v", err)
					return
				}

				if !yield(caResp.Response) {
					return
				}
			}
		}

		return seq, respBody, nil
	}

	return nil, nil, lastErr
}

// GenerateContentOneShot calls the Code Assist API and returns a single response
func (p *CodeAssistProvider) GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error) {
	seq, closer, err := p.SendMessageStream(ctx, modelName, params)
	if err != nil {
		log.Printf("GenerateContentOneShot: SendMessageStream failed: %v", err)
		return OneShotResult{}, err
	}
	defer closer.Close()

	var fullResponse strings.Builder
	var urlContextMeta *URLContextMetadata
	var groundingMetadata *GroundingMetadata

	for resp := range seq {
		if len(resp.Candidates) > 0 {
			candidate := resp.Candidates[0]
			if len(candidate.Content.Parts) > 0 {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						fullResponse.WriteString(part.Text)
					}
				}
			}
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

func (p *CodeAssistProvider) CountTokens(ctx context.Context, apiModelName string, contents []Content) (*CaCountTokenResponse, error) {
	if apiModelName == "" {
		return nil, fmt.Errorf("model name cannot be empty")
	}

	db, err := database.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	ga, err := GeminiAuthFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get GeminiAuth from context: %w", err)
	}

	tokens, err := database.GetOAuthTokensWithValidProjectID(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth tokens: %w", err)
	}

	// Filter tokens for the current provider kind
	var providerTokens []OAuthToken
	for _, token := range tokens {
		if token.Kind == p.providerKind {
			providerTokens = append(providerTokens, token)
		}
	}

	sort.Slice(providerTokens, func(i, j int) bool {
		return providerTokens[i].LastUsedByModel[apiModelName].Before(providerTokens[j].LastUsedByModel[apiModelName])
	})

	var lastErr error
	for _, token := range providerTokens {
		var oauthToken oauth2.Token
		if err := json.Unmarshal([]byte(token.TokenData), &oauthToken); err != nil {
			log.Printf("Failed to unmarshal OAuth token %d: %v", token.ID, err)
			continue
		}

		oauthConfig := ga.OAuthConfig(p.providerKind)
		tokenSource := &databaseTokenSource{
			db:          db,
			tokenID:     token.ID,
			kind:        p.providerKind,
			userEmail:   token.UserEmail,
			projectID:   token.ProjectID,
			baseToken:   &oauthToken,
			tokenSource: oauthConfig.TokenSource(context.Background(), &oauthToken),
		}
		client := NewCodeAssistClient(TokenSourceClientProvider(tokenSource), token.ProjectID, p.providerKind)

		database.UpdateOAuthTokenModelLastUsed(db, token.ID, apiModelName)

		result, err := client.CountTokens(ctx, apiModelName, contents)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 429 {
			go func() {
				retryAfter := parseRetryAfter("")
				database.HandleOAuthRateLimit(db, token.ID, apiModelName, retryAfter)
				log.Printf("Rate limit detected for OAuth token %d, model %s", token.ID, apiModelName)
			}()
		}
	}

	return nil, lastErr
}

func (p *CodeAssistProvider) MaxTokens(apiModelName string) int {
	// Return a default max tokens value
	// The actual max tokens is managed by the Models registry
	return 1048576
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
func convertSessionParamsToGenerateRequest(toolRegistry *tool.Tools, model *Model, params SessionParams) GenerateContentRequest {
	// Determine generation params source
	// Priority: model.GenParams > defaults
	var ignoreSystemPrompt bool
	var toolSupported bool
	var thoughtEnabled bool
	var responseModalities []string
	var genParams spec.GenerationParams

	if model != nil {
		// Use model config
		ignoreSystemPrompt = model.IgnoreSystemPrompt
		toolSupported = model.ToolSupported
		thoughtEnabled = model.ThoughtEnabled
		responseModalities = model.ResponseModalities
		genParams = model.GenParams
	} else {
		// Use defaults
		ignoreSystemPrompt = false
		toolSupported = true
		thoughtEnabled = true
		responseModalities = []string{"TEXT"}
		genParams = spec.GenerationParams{}
	}

	var systemInstruction *Content
	if params.SystemPrompt != "" && !ignoreSystemPrompt {
		systemInstruction = &Content{
			Parts: []Part{
				{Text: params.SystemPrompt},
			},
		}
	}

	var tools []Tool
	if toolSupported {
		tools = toolRegistry.ForGemini()
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
	if params.IncludeThoughts && thoughtEnabled {
		thinkingConfig = &ThinkingConfig{
			IncludeThoughts: true,
		}
	}

	var temperature, topP *float32
	var topK *int32
	if genParams.Temperature >= 0 {
		temperature = &genParams.Temperature
	}
	if genParams.TopP >= 0 {
		topP = &genParams.TopP
	}
	if genParams.TopK >= 0 {
		topK = &genParams.TopK
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
			ResponseModalities: responseModalities,
		},
	}
}

// databaseTokenSource wraps oauth2.TokenSource to save refreshed tokens to database
type databaseTokenSource struct {
	db          *database.Database
	tokenID     int
	kind        string
	userEmail   string
	projectID   string
	baseToken   *oauth2.Token
	tokenSource oauth2.TokenSource
	mu          sync.Mutex
}

// Token implements the oauth2.TokenSource interface
// It returns a token, refreshing it if necessary, and saves the refreshed token to the database
func (dts *databaseTokenSource) Token() (*oauth2.Token, error) {
	dts.mu.Lock()
	defer dts.mu.Unlock()

	// Get token from the underlying source
	token, err := dts.tokenSource.Token()
	if err != nil {
		return nil, err
	}

	// Check if token was refreshed by comparing access tokens
	if token.AccessToken != dts.baseToken.AccessToken {
		// Token was refreshed, save to database
		tokenJSON, err := json.MarshalIndent(token, "", "  ")
		if err != nil {
			log.Printf("Failed to marshal refreshed OAuth token %d: %v", dts.tokenID, err)
		} else {
			if err := database.UpdateOAuthTokenData(dts.db, dts.tokenID, string(tokenJSON)); err != nil {
				log.Printf("Failed to save refreshed OAuth token %d to DB: %v", dts.tokenID, err)
			} else {
				log.Printf("OAuth token %d refreshed and saved to DB for user %s", dts.tokenID, dts.userEmail)
			}
		}
		// Update the stored base token
		dts.baseToken = token
	}

	return token, nil
}
