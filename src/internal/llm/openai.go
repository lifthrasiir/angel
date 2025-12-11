package llm

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// Ensure OpenAIClient implements LLMProvider
var _ LLMProvider = (*OpenAIClient)(nil)

// OpenAIClient implements the LLMProvider interface for OpenAI-compatible APIs
type OpenAIClient struct {
	config           *OpenAIConfig
	httpClient       *http.Client
	modelsCache      map[string][]OpenAIModel
	cacheTime        time.Time
	contextLengths   map[string]int
	contextLengthsMu sync.RWMutex
}

// OpenAIModel represents a model from OpenAI-compatible API
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIModelsResponse represents the response from /v1/models endpoint
type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// OpenAIChatMessage represents a message in chat completion
type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIChatRequest represents the request to /v1/chat/completions endpoint
type OpenAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []OpenAIChatMessage `json:"messages"`
	Temperature *float32            `json:"temperature,omitempty"`
	MaxTokens   *int                `json:"max_tokens,omitempty"`
	TopP        *float32            `json:"top_p,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	Tools       []OpenAITool        `json:"tools,omitempty"`
	ToolChoice  interface{}         `json:"tool_choice,omitempty"`
}

// OpenAIChatResponse represents the response from /v1/chat/completions endpoint
type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// OpenAIChatStreamChunk represents a chunk from streaming chat completion
type OpenAIChatStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role         string              `json:"role,omitempty"`
			Content      string              `json:"content,omitempty"`
			ToolCalls    []OpenAIToolCall    `json:"tool_calls,omitempty"`
			FunctionCall *OpenAIFunctionCall `json:"function_call,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// OpenAIContentPart represents content parts in OpenAI format
type OpenAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL `json:"image_url,omitempty"`
}

// OpenAIImageURL represents image URL in OpenAI format
type OpenAIImageURL struct {
	URL string `json:"url"`
}

// OpenAIFunctionCall represents a function call in streaming
type OpenAIFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// OpenAITool represents a tool definition for OpenAI API
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction represents a function definition for OpenAI API
type OpenAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// OpenAIToolCall represents a tool call in streaming
type OpenAIToolCall struct {
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function *OpenAIFunctionCall `json:"function,omitempty"`
}

// Ollama API types for probing model information
type OllamaShowRequest struct {
	Model string `json:"model"`
}

type OllamaModelInfo struct {
	Modelfile  string                 `json:"modelfile"`
	Parameters interface{}            `json:"parameters"` // Can be string or object
	Template   string                 `json:"template"`
	Details    map[string]interface{} `json:"details"`
	ModelInfo  map[string]interface{} `json:"model_info"`
}

// convertGeminiToOpenAIContent converts Gemini Content to OpenAI message format
func convertGeminiToOpenAIContent(contents []Content) []OpenAIChatMessage {
	var messages []OpenAIChatMessage

	for _, content := range contents {
		message := OpenAIChatMessage{
			Role: content.Role,
		}

		// Handle different part types
		if len(content.Parts) == 1 && content.Parts[0].Text != "" {
			// Simple text content
			message.Content = content.Parts[0].Text
		} else {
			// Complex content with multiple parts
			var parts []OpenAIContentPart
			for _, part := range content.Parts {
				if part.Text != "" {
					parts = append(parts, OpenAIContentPart{
						Type: "text",
						Text: part.Text,
					})
				} else if part.InlineData != nil {
					// Convert InlineData to base64 data URL
					dataURL := fmt.Sprintf("data:%s;base64,%s",
						part.InlineData.MimeType,
						base64.StdEncoding.EncodeToString([]byte(part.InlineData.Data)))
					parts = append(parts, OpenAIContentPart{
						Type:     "image_url",
						ImageURL: &OpenAIImageURL{URL: dataURL},
					})
				} else if part.FunctionCall != nil {
					// Handle function calls (will be processed separately)
					parts = append(parts, OpenAIContentPart{
						Type: "text",
						Text: fmt.Sprintf("Function call: %s", part.FunctionCall.Name),
					})
				}
			}

			// Convert parts array to JSON string for content
			if len(parts) > 0 {
				partsJSON, _ := json.Marshal(parts)
				message.Content = string(partsJSON)
			}
		}

		messages = append(messages, message)
	}

	return messages
}

// convertOpenAIToGeminiContent converts OpenAI response to Gemini format
func convertOpenAIToGeminiPart(text string) []Part {
	var parts []Part

	// Try to parse as JSON array of content parts
	var contentParts []OpenAIContentPart
	if err := json.Unmarshal([]byte(text), &contentParts); err == nil {
		// Successfully parsed as content parts array
		for _, contentPart := range contentParts {
			if contentPart.Type == "text" && contentPart.Text != "" {
				parts = append(parts, Part{Text: contentPart.Text})
			} else if contentPart.Type == "image_url" && contentPart.ImageURL != nil {
				// Parse data URL: data:<mime type>;base64,<base64 blob>
				url := contentPart.ImageURL.URL
				if strings.HasPrefix(url, "data:") && strings.Contains(url, ";base64,") {
					urlParts := strings.SplitN(url, ",", 2)
					if len(urlParts) == 2 {
						mimeType := strings.TrimPrefix(strings.TrimSuffix(urlParts[0], ";base64"), "data:")
						base64Data := urlParts[1]

						data, err := base64.StdEncoding.DecodeString(base64Data)
						if err == nil {
							parts = append(parts, Part{
								InlineData: &InlineData{
									MimeType: mimeType,
									Data:     string(data),
								},
							})
						}
					}
				}
			}
		}
	} else {
		// Treat as plain text
		parts = append(parts, Part{Text: text})
	}

	return parts
}

// convertGeminiToolsToOpenAI converts Gemini tools to OpenAI function format
func convertGeminiToolsToOpenAI(tools []Tool) []OpenAITool {
	var openAITools []OpenAITool

	if tools == nil {
		return openAITools
	}

	for _, tool := range tools {
		if len(tool.FunctionDeclarations) > 0 {
			for _, funcDecl := range tool.FunctionDeclarations {
				openAIFunc := OpenAIFunction{
					Name:        funcDecl.Name,
					Description: funcDecl.Description,
				}

				// Convert parameters from Gemini schema to JSON schema format
				if funcDecl.Parameters != nil {
					openAIFunc.Parameters = convertGeminiSchemaToJSONSchema(funcDecl.Parameters)
				}

				openAITools = append(openAITools, OpenAITool{
					Type:     "function",
					Function: openAIFunc,
				})
			}
		}
	}

	return openAITools
}

// convertGeminiSchemaToJSONSchema converts Gemini Schema to JSON Schema format for OpenAI
func convertGeminiSchemaToJSONSchema(geminiSchema *Schema) map[string]interface{} {
	if geminiSchema == nil {
		return nil
	}

	jsonSchema := make(map[string]interface{})

	// Set type
	switch geminiSchema.Type {
	case TypeString:
		jsonSchema["type"] = "string"
	case TypeNumber:
		jsonSchema["type"] = "number"
	case TypeInteger:
		jsonSchema["type"] = "integer"
	case TypeBoolean:
		jsonSchema["type"] = "boolean"
	case TypeArray:
		jsonSchema["type"] = "array"
	case TypeObject:
		jsonSchema["type"] = "object"
	default:
		jsonSchema["type"] = "object"
	}

	// Set properties for object type
	if geminiSchema.Type == TypeObject && len(geminiSchema.Properties) > 0 {
		properties := make(map[string]interface{})
		for key, propSchema := range geminiSchema.Properties {
			properties[key] = convertGeminiSchemaToJSONSchema(propSchema)
		}
		jsonSchema["properties"] = properties

		// Set required fields
		if len(geminiSchema.Required) > 0 {
			jsonSchema["required"] = geminiSchema.Required
		}
	}

	// Set items for array type
	if geminiSchema.Type == TypeArray {
		// For arrays, we'd need item schema, but Gemini doesn't specify it in the same way
		// For now, we'll leave it empty or set a generic object type
		jsonSchema["items"] = map[string]interface{}{"type": "object"}
	}

	return jsonSchema
}

// NewOpenAIClient creates a new OpenAI-compatible client
func NewOpenAIClient(config *OpenAIConfig) *OpenAIClient {
	// Create new client
	client := &OpenAIClient{
		config:         config,
		httpClient:     &http.Client{Timeout: 60 * time.Second},
		modelsCache:    make(map[string][]OpenAIModel),
		cacheTime:      time.Time{},
		contextLengths: make(map[string]int),
	}

	return client
}

// probeContextLength tries to detect context length for known OpenAI-compatible APIs
func (c *OpenAIClient) probeContextLength(modelName string) int {
	// Check cache first
	c.contextLengthsMu.RLock()
	if length, exists := c.contextLengths[modelName]; exists {
		c.contextLengthsMu.RUnlock()
		return length
	}
	c.contextLengthsMu.RUnlock()

	// Try Ollama API first
	if length := c.probeOllamaContextLength(modelName); length > 0 {
		// Cache the result
		c.contextLengthsMu.Lock()
		c.contextLengths[modelName] = length
		c.contextLengthsMu.Unlock()
		return length
	}

	// Could add more providers here later
	return 0
}

// probeOllamaContextLength uses ../api/show endpoint to get model details
func (c *OpenAIClient) probeOllamaContextLength(modelName string) int {
	apiURL, _ := url.JoinPath(c.config.Endpoint, "../api/show")

	reqBody := OllamaShowRequest{Model: modelName}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("Failed to marshal Ollama show request: %v", err)
		return 0
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("Failed to create Ollama show request: %v", err)
		return 0
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to call %s: %v", apiURL, err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("API %s returned status %d", apiURL, resp.StatusCode)
		return 0
	}

	var modelInfo OllamaModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&modelInfo); err != nil {
		log.Printf("Failed to decode model info from %s: %v", apiURL, err)
		return 0
	}

	if modelInfo.ModelInfo != nil {
		for key, value := range modelInfo.ModelInfo {
			if strings.HasSuffix(key, ".context_length") {
				if contextLength, ok := value.(float64); ok && contextLength > 0 {
					log.Printf("Detected context length for %s: %d", modelName, int(contextLength))
					return int(contextLength)
				}
			}
		}
	}

	return 0
}

// GetModels retrieves available models from the API with 24-hour caching
func (c *OpenAIClient) GetModels(ctx context.Context) ([]OpenAIModel, error) {
	// Check if cache is still valid (24 hours)
	if time.Since(c.cacheTime) < 24*time.Hour {
		if models, exists := c.modelsCache[c.config.Endpoint]; exists {
			return models, nil
		}
	}

	// Fetch models from API
	url := strings.TrimSuffix(c.config.Endpoint, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %d %s, response: %s", resp.StatusCode, resp.Status, string(body))
	}

	var modelsResp OpenAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Update cache
	c.modelsCache[c.config.Endpoint] = modelsResp.Data
	c.cacheTime = time.Now()

	return modelsResp.Data, nil
}

// RefreshModels forces a refresh of the models cache
func (c *OpenAIClient) RefreshModels(ctx context.Context) ([]OpenAIModel, error) {
	// Clear cache
	c.cacheTime = time.Time{}
	delete(c.modelsCache, c.config.Endpoint)

	// Fetch fresh models
	return c.GetModels(ctx)
}

// SendMessageStream implements the LLMProvider interface for streaming chat completions
func (c *OpenAIClient) SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	// Convert contents to OpenAI chat messages
	messages := convertGeminiToOpenAIContent(params.Contents)

	// Convert Gemini tools to OpenAI format
	var openAITools []OpenAITool
	if params.ToolConfig == nil {
		// If no specific tool config, include all available tools
		tools, err := tool.FromContext(ctx)
		if err != nil {
			return nil, nil, err
		}
		geminiTools := tools.ForGemini()
		openAITools = convertGeminiToolsToOpenAI(geminiTools)
	}

	// Prepare request
	req := OpenAIChatRequest{
		Model:    modelName,
		Messages: messages,
		Stream:   true,
		Tools:    openAITools,
	}

	// Set tool choice if specified
	if len(openAITools) > 0 {
		req.ToolChoice = "auto" // Let the model decide when to use tools
	}

	// Convert request to JSON
	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := strings.TrimSuffix(c.config.Endpoint, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, fmt.Errorf("API error: %d %s, response: %s", resp.StatusCode, resp.Status, string(body))
	}

	// Create streaming response
	seq := func(yield func(GenerateContentResponse) bool) {
		scanner := bufio.NewScanner(resp.Body)

		// Track ongoing function calls to accumulate arguments
		type ongoingCall struct {
			name       string
			argsBuffer string
		}
		currentCalls := make(map[string]*ongoingCall)

		for scanner.Scan() {
			line := scanner.Text()

			// Skip empty lines and "data: [DONE]" messages
			if line == "" || strings.HasPrefix(line, "data: [DONE]") {
				continue
			}

			// Remove "data: " prefix
			line = strings.TrimPrefix(line, "data: ")

			// Parse JSON
			var chunk OpenAIChatStreamChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				log.Printf("Failed to parse chunk: %v, line: %s", err, line)
				continue
			}

			// Convert to CaGenerateContentResponse
			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]
				var parts []Part

				// Handle text content and inline images
				if choice.Delta.Content != "" {
					// Convert OpenAI content to Gemini parts
					openAIParts := convertOpenAIToGeminiPart(choice.Delta.Content)
					parts = append(parts, openAIParts...)
				}

				// Handle function calls (legacy format)
				if choice.Delta.FunctionCall != nil {
					if choice.Delta.FunctionCall.Name != "" {
						// Start a new function call
						if _, exists := currentCalls["legacy"]; !exists {
							currentCalls["legacy"] = &ongoingCall{name: choice.Delta.FunctionCall.Name}
						}
					}
					if choice.Delta.FunctionCall.Arguments != "" {
						// Legacy format usually comes as complete JSON
						if call, exists := currentCalls["legacy"]; exists {
							call.argsBuffer = choice.Delta.FunctionCall.Arguments
						}
					}
				}

				// Handle tool calls (current format)
				for _, toolCall := range choice.Delta.ToolCalls {
					if toolCall.Function != nil {
						callKey := toolCall.ID
						if callKey == "" {
							callKey = fmt.Sprintf("call_%d", len(currentCalls))
						}

						// Get or create ongoing call
						var call *ongoingCall
						if existing, exists := currentCalls[callKey]; exists {
							call = existing
						} else {
							call = &ongoingCall{}
							currentCalls[callKey] = call
						}

						// Handle name (usually comes first)
						if toolCall.Function.Name != "" {
							call.name = toolCall.Function.Name
						}

						// Handle arguments (comes in chunks - just accumulate)
						if toolCall.Function.Arguments != "" {
							call.argsBuffer += toolCall.Function.Arguments
						}
					}
				}

				// When stream is ending, try to parse all accumulated function calls
				if choice.FinishReason != nil && (*choice.FinishReason == "tool_calls" || *choice.FinishReason == "function_call" || *choice.FinishReason == "stop") {
					for callKey, call := range currentCalls {
						if call.name != "" && call.argsBuffer != "" {
							funcCall := &FunctionCall{
								Name: call.name,
							}

							// Try to parse accumulated arguments as JSON
							var args map[string]interface{}
							if err := json.Unmarshal([]byte(call.argsBuffer), &args); err != nil {
								// If JSON parsing fails, pass the raw string as a single argument
								log.Printf("Failed to parse function arguments for %s as JSON: %v, treating as raw string", call.name, err)
								funcCall.Args = map[string]interface{}{
									"raw_arguments": call.argsBuffer,
								}
							} else {
								funcCall.Args = args
							}

							parts = append(parts, Part{FunctionCall: funcCall})
						}
						// Clear processed call
						delete(currentCalls, callKey)
					}
				}

				// Only yield if we have parts to send
				if len(parts) > 0 {
					resp := GenerateContentResponse{
						Candidates: []Candidate{
							{
								Content: Content{
									Parts: parts,
									Role:  "model",
								},
							},
						},
					}
					if !yield(resp) {
						return
					}
				}

				// Check for finish reason and end stream if present
				if choice.FinishReason != nil {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Scanner error: %v", err)
		}
	}

	return seq, resp.Body, nil
}

// GenerateContentOneShot implements the LLMProvider interface for non-streaming completion
func (c *OpenAIClient) GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error) {
	seq, closer, err := c.SendMessageStream(ctx, modelName, params)
	if err != nil {
		return OneShotResult{}, err
	}
	defer closer.Close()

	var fullResponse strings.Builder
	var hasContent bool
	for caResp := range seq {
		if len(caResp.Candidates) > 0 {
			candidate := caResp.Candidates[0]
			if len(candidate.Content.Parts) > 0 {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						fullResponse.WriteString(part.Text)
						hasContent = true
					} else if part.InlineData != nil {
						// For inline data, we can't easily serialize it to text
						// So we'll just indicate that there was non-text content
						fullResponse.WriteString("[Inline Data: " + part.InlineData.MimeType + "]")
						hasContent = true
					}
				}
			}
		}
	}

	if hasContent {
		return OneShotResult{
			Text: fullResponse.String(),
		}, nil
	}

	return OneShotResult{}, fmt.Errorf("no content found in response")
}

// CountTokens implements the LLMProvider interface
// Note: OpenAI-compatible APIs may not support token counting, so we'll estimate
func (c *OpenAIClient) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
	// Simple token estimation (rough approximation: 1 token ≈ 4 characters)
	totalChars := 0
	for _, content := range contents {
		for _, part := range content.Parts {
			if part.Text != "" {
				totalChars += len(part.Text)
			}
		}
	}

	estimatedTokens := totalChars / 4
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}

	return &CaCountTokenResponse{
		TotalTokens: estimatedTokens,
	}, nil
}

// MaxTokens implements the LLMProvider interface
func (c *OpenAIClient) MaxTokens(modelName string) int {
	// First check known model context lengths
	switch modelName {
	// All public OpenAI models as of 2025-10-15
	case "gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5-codex", "gpt-5-pro":
		return 400000
	case "gpt-5-chat-latest":
		return 128000
	case "gpt-4.5-preview":
		return 128000
	case "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano":
		return 1047576
	case "gpt-4o", "gpt-4o-audio", "gpt-4o-mini", "gpt-4o-mini-audio", "gpt-4o-search-preview", "gpt-4o-mini-search-preview", "chatgpt-4o-latest":
		return 128000
	case "gpt-4o-realtime-preview":
		return 32000
	case "gpt-4o-mini-realtime-preview", "gpt-4o-mini-transcribe", "gpt-4o-transcribe":
		return 16000
	case "gpt-4":
		return 8192
	case "gpt-4-turbo", "gpt-4-turbo-preview":
		return 128000
	case "gpt-3.5-turbo":
		return 16385
	case "babbage-002", "davinci-002":
		return 16384
	case "codex-mini-latest":
		return 200000
	case "o4-mini", "o4-deep-research":
		return 200000
	case "o3", "o3-mini", "o3-pro", "o3-deep-research":
		return 200000
	case "o1", "o1-mini", "o1-pro":
		return 200000
	case "o1-preview":
		return 128000
	case "computer-use-preview":
		return 8192
	case "gpt-audio", "gpt-audio-mini":
		return 128000
	case "gpt-realtime", "gpt-realtime-mini":
		return 32000
	case "text-moderation", "text-moderation-stable":
		return 32768

	case "gpt-oss-120b", "gpt-oss-20b", "gpt-oss:120b", "gpt-oss:20b":
		return 131072
	}

	// Check cache for probed context length
	if length := c.probeContextLength(modelName); length > 0 {
		return length
	}

	return 4096 // Safe default
}

// ReloadOpenAIProviders reloads OpenAI providers from database configurations
func ReloadOpenAIProviders(db *sql.DB, registry *Models) {
	// Get current OpenAI configs to know which models to keep
	configs, err := database.GetOpenAIConfigs(db)
	if err != nil {
		log.Printf("Failed to load OpenAI configs for reload: %v", err)
		return
	}

	// Create set of valid model IDs from current enabled configs
	validModels := make(map[string]bool)
	// Create a mapping of config to client for sharing during reload
	configClients := make(map[string]*OpenAIClient)

	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		client := NewOpenAIClient(&config)
		configClients[config.ID] = client

		models, err := client.GetModels(context.Background())
		if err != nil {
			log.Printf("Failed to fetch models for OpenAI config %s during reload: %v", config.Name, err)
			continue
		}

		for _, model := range models {
			validModels[model.ID] = true
		}
	}

	// Remove providers that are no longer valid
	func() {
		registry.mutex.Lock()
		defer registry.mutex.Unlock()
		for modelName, provider := range registry.providers {
			if _, ok := provider.(*OpenAIClient); ok {
				if !validModels[modelName] {
					delete(registry.providers, modelName)
					log.Printf("Removed OpenAI model provider: %s", modelName)
				}
			}
		}
	}()

	// Add or update providers for valid models using shared clients
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		client := configClients[config.ID]
		models, err := client.GetModels(context.Background())
		if err != nil {
			log.Printf("Failed to fetch models for OpenAI config %s during reload: %v", config.Name, err)
			continue
		}

		for _, model := range models {
			if validModels[model.ID] {
				// Use the same client instance for all models from this config
				registry.mutex.Lock()
				registry.providers[model.ID] = client
				registry.mutex.Unlock()
				log.Printf("Registered/updated OpenAI model: %s (from config: %s)", model.ID, config.Name)

				// Pre-warm MaxTokens cache asynchronously
				go func(modelName string, provider LLMProvider) {
					_ = provider.MaxTokens(modelName)
				}(model.ID, client)
			}
		}
	}
}
