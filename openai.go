package main

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
	"strings"
	"time"
)

// Ensure OpenAIClient implements LLMProvider
var _ LLMProvider = (*OpenAIClient)(nil)

// OpenAIClient implements the LLMProvider interface for OpenAI-compatible APIs
type OpenAIClient struct {
	config      *OpenAIConfig
	modelName   string
	httpClient  *http.Client
	modelsCache map[string][]OpenAIModel
	cacheTime   time.Time
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

// OpenAIToolCall represents a tool call in streaming
type OpenAIToolCall struct {
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function *OpenAIFunctionCall `json:"function,omitempty"`
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

// NewOpenAIClient creates a new OpenAI-compatible client
func NewOpenAIClient(config *OpenAIConfig, modelName string) *OpenAIClient {
	return &OpenAIClient{
		config:      config,
		modelName:   modelName,
		httpClient:  &http.Client{Timeout: 60 * time.Second},
		modelsCache: make(map[string][]OpenAIModel),
		cacheTime:   time.Time{},
	}
}

// ModelName implements the LLMProvider interface
func (c *OpenAIClient) ModelName() string {
	return c.modelName
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
func (c *OpenAIClient) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	// Convert contents to OpenAI chat messages
	messages := convertGeminiToOpenAIContent(params.Contents)

	// Get default generation parameters
	genParams := c.DefaultGenerationParams()

	// Apply session-specific parameters if provided
	if params.GenerationParams != nil {
		if params.GenerationParams.Temperature >= 0 {
			genParams.Temperature = params.GenerationParams.Temperature
		}
		if params.GenerationParams.TopP >= 0 {
			genParams.TopP = params.GenerationParams.TopP
		}
		if params.GenerationParams.TopK >= 0 {
			// OpenAI doesn't support TopK directly, ignore
		}
	}

	// Prepare request
	req := OpenAIChatRequest{
		Model:       c.modelName,
		Messages:    messages,
		Temperature: &genParams.Temperature,
		TopP:        &genParams.TopP,
		Stream:      true,
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
	seq := func(yield func(CaGenerateContentResponse) bool) {
		scanner := bufio.NewScanner(resp.Body)
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
						parts = append(parts, Part{
							FunctionCall: &FunctionCall{
								Name: choice.Delta.FunctionCall.Name,
							},
						})
					}
					if choice.Delta.FunctionCall.Arguments != "" {
						parts = append(parts, Part{
							FunctionResponse: &FunctionResponse{
								Name:     choice.Delta.FunctionCall.Name,
								Response: choice.Delta.FunctionCall.Arguments,
							},
						})
					}
				}

				// Handle tool calls (current format)
				for _, toolCall := range choice.Delta.ToolCalls {
					if toolCall.Function != nil {
						if toolCall.Function.Name != "" {
							parts = append(parts, Part{
								FunctionCall: &FunctionCall{
									Name: toolCall.Function.Name,
								},
							})
						}
						if toolCall.Function.Arguments != "" {
							parts = append(parts, Part{
								FunctionResponse: &FunctionResponse{
									Name:     toolCall.Function.Name,
									Response: toolCall.Function.Arguments,
								},
							})
						}
					}
				}

				// Only yield if we have parts to send
				if len(parts) > 0 {
					caResp := CaGenerateContentResponse{
						Response: VertexGenerateContentResponse{
							Candidates: []Candidate{
								{
									Content: Content{
										Parts: parts,
										Role:  "model",
									},
								},
							},
						},
					}
					if !yield(caResp) {
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
func (c *OpenAIClient) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	seq, closer, err := c.SendMessageStream(ctx, params)
	if err != nil {
		return OneShotResult{}, err
	}
	defer closer.Close()

	var fullResponse strings.Builder
	var hasContent bool
	for caResp := range seq {
		if len(caResp.Response.Candidates) > 0 {
			candidate := caResp.Response.Candidates[0]
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
func (c *OpenAIClient) CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error) {
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
func (c *OpenAIClient) MaxTokens() int {
	// Default max tokens for most OpenAI-compatible models
	return 4096
}

// RelativeDisplayOrder implements the LLMProvider interface
func (c *OpenAIClient) RelativeDisplayOrder() int {
	return 5 // Lower priority than Gemini models
}

// DefaultGenerationParams implements the LLMProvider interface
func (c *OpenAIClient) DefaultGenerationParams() SessionGenerationParams {
	return SessionGenerationParams{
		Temperature: 0.7,
		TopK:        -1, // Not supported by OpenAI
		TopP:        1.0,
	}
}

// SubagentProviderAndParams implements the LLMProvider interface
func (c *OpenAIClient) SubagentProviderAndParams(task string) (LLMProvider, SessionGenerationParams) {
	params := SessionGenerationParams{
		Temperature: 0.0,
		TopK:        -1,
		TopP:        1.0,
	}
	return c, params
}

// InitOpenAIProviders initializes OpenAI providers from database configurations
func InitOpenAIProviders(db *sql.DB) {
	configs, err := GetOpenAIConfigs(db)
	if err != nil {
		log.Printf("Failed to load OpenAI configs: %v", err)
		return
	}

	for _, config := range configs {
		if !config.Enabled {
			continue // Skip disabled configurations
		}

		// Create client and get models to register as providers
		client := NewOpenAIClient(&config, "")
		models, err := client.GetModels(context.Background())
		if err != nil {
			log.Printf("Failed to fetch models for OpenAI config %s: %v", config.Name, err)
			continue
		}

		// Register each model as a provider
		for _, model := range models {
			modelClient := NewOpenAIClient(&config, model.ID)
			CurrentProviders[model.ID] = modelClient
			log.Printf("Registered OpenAI model: %s (from config: %s)", model.ID, config.Name)
		}
	}
}

// ReloadOpenAIProviders reloads OpenAI providers from database configurations
func ReloadOpenAIProviders(db *sql.DB) {
	// Get current OpenAI configs to know which models to keep
	configs, err := GetOpenAIConfigs(db)
	if err != nil {
		log.Printf("Failed to load OpenAI configs for reload: %v", err)
		return
	}

	// Create set of valid model IDs from current enabled configs
	validModels := make(map[string]bool)
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		client := NewOpenAIClient(&config, "")
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
	for modelName, provider := range CurrentProviders {
		if _, ok := provider.(*OpenAIClient); ok {
			if !validModels[modelName] {
				delete(CurrentProviders, modelName)
				log.Printf("Removed OpenAI model provider: %s", modelName)
			}
		}
	}

	// Add or update providers for valid models
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		client := NewOpenAIClient(&config, "")
		models, err := client.GetModels(context.Background())
		if err != nil {
			log.Printf("Failed to fetch models for OpenAI config %s during reload: %v", config.Name, err)
			continue
		}

		for _, model := range models {
			if validModels[model.ID] {
				modelClient := NewOpenAIClient(&config, model.ID)
				CurrentProviders[model.ID] = modelClient
				log.Printf("Registered/updated OpenAI model: %s (from config: %s)", model.ID, config.Name)
			}
		}
	}
}
