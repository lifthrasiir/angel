package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"strings"

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
}

// geminiDefaultGenerationParams holds the default generation parameters for Gemini models
var geminiDefaultGenerationParams = SessionGenerationParams{
	Temperature: 1.0,
	TopK:        64,
	TopP:        0.95,
}

// Reserved names for Gemini's internal tools.
const (
	GeminiUrlContextToolName    = ".url_context"
	GeminiCodeExecutionToolName = ".code_execution"
)

// sessionParamsToVertexRequest converts SessionParams to VertexGenerateContentRequest (private)
func (cap *CodeAssistProvider) sessionParamsToVertexRequest(modelInfo *GeminiModel, params SessionParams) GenerateContentRequest {
	var systemInstruction *Content
	if params.SystemPrompt != "" {
		systemInstruction = &Content{
			Parts: []Part{
				{Text: params.SystemPrompt},
			},
		}
	}

	var tools []Tool
	if modelInfo.ToolSupported {
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

	includeThoughts := params.IncludeThoughts && modelInfo.ThoughtEnabled

	// Determine final generation parameters, prioritizing SessionParams
	genParams := geminiDefaultGenerationParams
	if params.GenerationParams != nil {
		if params.GenerationParams.Temperature >= 0 { // Assuming >=0 means set
			genParams.Temperature = params.GenerationParams.Temperature
		}
		if params.GenerationParams.TopP >= 0 { // Assuming >=0 means set
			genParams.TopP = params.GenerationParams.TopP
		}
		if params.GenerationParams.TopK >= 0 { // Assuming >=0 means set
			genParams.TopK = params.GenerationParams.TopK
		}
	}

	return GenerateContentRequest{
		Contents:          params.Contents,
		SystemInstruction: systemInstruction,
		Tools:             tools,
		GenerationConfig: &GenerationConfig{
			ThinkingConfig: &ThinkingConfig{
				IncludeThoughts: includeThoughts,
			},
			Temperature: &genParams.Temperature,
			TopP:        &genParams.TopP,
			TopK:        &genParams.TopK,
		},
	}
}

// SendMessageStream calls the streamGenerateContent of Code Assist API and returns an iter.Seq of responses.
func (cap *CodeAssistProvider) SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	modelInfo := GeminiModelInfo(modelName)
	if modelInfo == nil {
		return nil, nil, fmt.Errorf("unsupported model: %s", modelName)
	}

	request := cap.sessionParamsToVertexRequest(modelInfo, params)
	respBody, err := cap.client.StreamGenerateContent(ctx, modelName, request)
	if err != nil {
		log.Printf("SendMessageStream: streamGenerateContent failed: %v", err)
		return nil, nil, err
	}

	dec := json.NewDecoder(respBody)

	// NOTE: This function is intentionally designed to parse a specific JSON stream format,
	// not standard SSE. Do not modify without understanding its purpose.

	// Read the opening bracket of the JSON array
	_, err = dec.Token()
	if err != nil {
		respBody.Close()
		log.Printf("SendMessageStream: Expected opening bracket '[', but got %v", err)
		return nil, nil, fmt.Errorf("expected opening bracket '[', but got %w", err)
	}

	// Create an iter.Seq that yields CaGenerateContentResponse
	seq := func(yield func(CaGenerateContentResponse) bool) {
		for dec.More() {
			var caResp CaGenerateContentResponse
			if err := dec.Decode(&caResp); err != nil {
				log.Printf("SendMessageStream: Failed to decode JSON object from stream: %v", err)
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

	for caResp := range seq {
		if len(caResp.Response.Candidates) > 0 {
			candidate := caResp.Response.Candidates[0]
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
	return cap.client.CountTokens(ctx, modelName, contents)
}

// MaxTokens implements the LLMProvider interface for CodeAssistProvider.
func (cap *CodeAssistProvider) MaxTokens(modelName string) int {
	switch modelName {
	case "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.5-pro":
		return 1048576
	case "gemini-2.5-flash-image-preview":
		return 32768
	default:
		return 1048576
	}
}

// RelativeDisplayOrder implements the LLMProvider interface for CodeAssistProvider.
func (cap *CodeAssistProvider) RelativeDisplayOrder(modelName string) int {
	switch modelName {
	case "gemini-2.5-flash":
		return 10
	case "gemini-2.5-pro":
		return 9
	case "gemini-2.5-flash-image-preview":
		return 8
	case "gemini-2.5-flash-lite":
		return 7
	default:
		return 0
	}
}

// DefaultGenerationParams implements the LLMProvider interface for CodeAssistProvider.
func (cap *CodeAssistProvider) DefaultGenerationParams(modelName string) SessionGenerationParams {
	return geminiDefaultGenerationParams
}

// SubagentProviderAndParams implements the LLMProvider interface for CodeAssistProvider.
func (cap *CodeAssistProvider) SubagentProviderAndParams(modelName string, task string) (provider LLMProvider, returnModelName string, params SessionGenerationParams) {
	params = SessionGenerationParams{
		Temperature: 0.0,
		TopK:        -1,
		TopP:        1.0,
	}

	provider = cap // Return self as provider

	if task == SubagentImageGenerationTask {
		returnModelName = "gemini-2.5-flash-image-preview"
	} else if modelName == "gemini-2.5-pro" {
		returnModelName = "gemini-2.5-flash"
	} else {
		returnModelName = "gemini-2.5-flash-lite"
	}

	return
}
