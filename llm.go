package main

import (
	"context"
	"fmt"
	"io"
	"iter"
)

const DefaultGeminiModel = "gemini-2.5-flash"

var CurrentProviders = make(map[string]LLMProvider)

// SessionParams holds the parameters for a chat session.
type SessionParams struct {
	Contents         []Content
	ModelName        string
	SystemPrompt     string
	IncludeThoughts  bool
	GenerationParams *SessionGenerationParams
	ToolConfig       map[string]interface{}
}

// SessionGenerationParams holds common generation parameters for a session.
type SessionGenerationParams struct {
	Temperature float32
	TopK        int32
	TopP        float32
}

// OneShotResult holds the result of a single-shot content generation,
// including text and any associated metadata.
type OneShotResult struct {
	Text               string
	URLContextMetadata *URLContextMetadata // Assuming this struct will be defined in gemini_types.go or similar
	GroundingMetadata  *GroundingMetadata  // Assuming this struct will be defined in gemini_types.go or similar
}

// LLMProvider defines the interface for interacting with an LLM.
type LLMProvider interface {
	SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error)
	GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error)
	CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error)
	MaxTokens() int
	RelativeDisplayOrder() int
	DefaultGenerationParams() SessionGenerationParams
}

// MockLLMProvider is a mock implementation of the LLMProvider interface for testing.
type MockLLMProvider struct {
	SendMessageStreamFunc        func(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error)
	GenerateContentOneShotFunc   func(ctx context.Context, params SessionParams) (OneShotResult, error)
	CountTokensFunc              func(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error)
	MaxTokensValue               int
	RelativeDisplayOrderValue    int
	DefaultGenerationParamsValue SessionGenerationParams
}

// SendMessageStream implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	if m.SendMessageStreamFunc != nil {
		return m.SendMessageStreamFunc(ctx, params)
	}
	return nil, nil, fmt.Errorf("SendMessageStream not implemented in mock")
}

// GenerateContentOneShot implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	if m.GenerateContentOneShotFunc != nil {
		return m.GenerateContentOneShotFunc(ctx, params)
	}
	return OneShotResult{}, fmt.Errorf("GenerateContentOneShot not implemented in mock")
}

// CountTokens implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	if m.CountTokensFunc != nil {
		return m.CountTokensFunc(ctx, contents, modelName)
	}
	return nil, fmt.Errorf("CountTokens not implemented in mock")
}

// MaxTokens implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) MaxTokens() int {
	return m.MaxTokensValue
}

// RelativeDisplayOrder implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) RelativeDisplayOrder() int {
	return m.RelativeDisplayOrderValue
}

// DefaultGenerationParams implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) DefaultGenerationParams() SessionGenerationParams {
	return m.DefaultGenerationParamsValue
}
