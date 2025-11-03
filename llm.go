package main

import (
	"context"
	"fmt"
	"io"
	"iter"

	. "github.com/lifthrasiir/angel/gemini"
)

const DefaultGeminiModel = "gemini-2.5-flash"

var CurrentProviders = make(map[string]LLMProvider)

// SessionParams holds the parameters for a chat session.
type SessionParams struct {
	Contents         []Content
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
	SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error)
	GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error)
	CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error)
	MaxTokens(modelName string) int
	RelativeDisplayOrder(modelName string) int
	DefaultGenerationParams(modelName string) SessionGenerationParams
	SubagentProviderAndParams(modelName string, task string) (LLMProvider, string, SessionGenerationParams)
}

// Well-known tasks for SubagentProviderAndParams.
const (
	SubagentCompressionTask      = "compression"
	SubagentSessionNameTask      = "session_name"
	SubagentWebFetchTask         = "web_fetch"
	SubagentWebFetchFallbackTask = "web_fetch_fallback"
	SubagentImageGenerationTask  = "image_generation"
)

// MockLLMProvider is a mock implementation of the LLMProvider interface for testing.
type MockLLMProvider struct {
	SendMessageStreamFunc         func(ctx context.Context, modelName string, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error)
	GenerateContentOneShotFunc    func(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error)
	CountTokensFunc               func(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error)
	MaxTokensFunc                 func(modelName string) int
	RelativeDisplayOrderFunc      func(modelName string) int
	DefaultGenerationParamsFunc   func(modelName string) SessionGenerationParams
	SubagentProviderAndParamsFunc func(modelName string, task string) (LLMProvider, string, SessionGenerationParams)
}

// SendMessageStream implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	if m.SendMessageStreamFunc != nil {
		return m.SendMessageStreamFunc(ctx, modelName, params)
	}
	return nil, nil, fmt.Errorf("SendMessageStream not implemented in mock")
}

// GenerateContentOneShot implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error) {
	if m.GenerateContentOneShotFunc != nil {
		return m.GenerateContentOneShotFunc(ctx, modelName, params)
	}
	return OneShotResult{}, fmt.Errorf("GenerateContentOneShot not implemented in mock")
}

// CountTokens implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
	if m.CountTokensFunc != nil {
		return m.CountTokensFunc(ctx, modelName, contents)
	}
	return nil, fmt.Errorf("CountTokens not implemented in mock")
}

// MaxTokens implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) MaxTokens(modelName string) int {
	if m.MaxTokensFunc != nil {
		return m.MaxTokensFunc(modelName)
	}
	return 1048576 // Default fallback
}

// RelativeDisplayOrder implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) RelativeDisplayOrder(modelName string) int {
	if m.RelativeDisplayOrderFunc != nil {
		return m.RelativeDisplayOrderFunc(modelName)
	}
	return 0 // Default fallback
}

// DefaultGenerationParams implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) DefaultGenerationParams(modelName string) SessionGenerationParams {
	if m.DefaultGenerationParamsFunc != nil {
		return m.DefaultGenerationParamsFunc(modelName)
	}
	return SessionGenerationParams{
		Temperature: 1.0,
		TopK:        64,
		TopP:        0.95,
	} // Default fallback
}

// SubagentProviderAndParams implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) SubagentProviderAndParams(modelName string, task string) (LLMProvider, string, SessionGenerationParams) {
	if m.SubagentProviderAndParamsFunc != nil {
		return m.SubagentProviderAndParamsFunc(modelName, task)
	}
	// Default mock behavior for subagent provider
	return m, modelName, SessionGenerationParams{
		Temperature: 0.0,
		TopK:        -1,
		TopP:        1.0,
	}
}

var _ LLMProvider = (*MockLLMProvider)(nil)
