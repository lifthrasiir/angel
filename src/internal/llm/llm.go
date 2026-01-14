package llm

import (
	"context"
	"fmt"
	"io"
	"iter"

	. "github.com/lifthrasiir/angel/gemini"
)

// SessionParams holds parameters for a chat session.
type SessionParams struct {
	Contents        []Content
	SystemPrompt    string
	IncludeThoughts bool
	ToolConfig      map[string]interface{}
}

// OneShotResult holds result of a single-shot content generation,
// including text and any associated metadata.
type OneShotResult struct {
	Text               string
	URLContextMetadata *URLContextMetadata // Assuming this struct will be defined in gemini_types.go or similar
	GroundingMetadata  *GroundingMetadata  // Assuming this struct will be defined in gemini_types.go or similar
}

// LLMProvider defines the interface for interacting with an LLM.
type LLMProvider interface {
	SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error)
	GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error)
	CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error)
	MaxTokens(modelName string) int
}

// ModelProvider defines the interface for a provider that automatically manages model name
type ModelProvider interface {
	SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error)
	GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error)
	CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error)
	MaxTokens() int
	Name() string
}

// modelProviderImpl is the default implementation of ModelProvider that wraps LLMProvider
type modelProviderImpl struct {
	LLMProvider
	modelName string
}

// SendMessageStream calls the underlying provider with the stored model name
func (mp *modelProviderImpl) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	return mp.LLMProvider.SendMessageStream(ctx, mp.modelName, params)
}

// GenerateContentOneShot calls the underlying provider with the stored model name
func (mp *modelProviderImpl) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	return mp.LLMProvider.GenerateContentOneShot(ctx, mp.modelName, params)
}

// CountTokens calls the underlying provider with the stored model name
func (mp *modelProviderImpl) CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error) {
	return mp.LLMProvider.CountTokens(ctx, mp.modelName, contents)
}

// MaxTokens calls the underlying provider with the stored model name
func (mp *modelProviderImpl) MaxTokens() int {
	return mp.LLMProvider.MaxTokens(mp.modelName)
}

// Name returns the model name
func (mp *modelProviderImpl) Name() string {
	return mp.modelName
}

// newModelProvider creates a new ModelProvider from an LLMProvider and model name
func newModelProvider(provider LLMProvider, modelName string) ModelProvider {
	return &modelProviderImpl{
		LLMProvider: provider,
		modelName:   modelName,
	}
}

// ModelProviderChain chains multiple ModelProviders with ordered fallback
type ModelProviderChain struct {
	providers []ModelProvider
	modelName string
}

// NewModelProviderChain creates a new chain from a list of ModelProviders
func NewModelProviderChain(providers []ModelProvider, modelName string) ModelProvider {
	return &ModelProviderChain{
		providers: providers,
		modelName: modelName,
	}
}

// SendMessageStream tries each provider in order until one succeeds
func (c *ModelProviderChain) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	var lastErr error
	for _, provider := range c.providers {
		seq, closer, err := provider.SendMessageStream(ctx, params)
		if err == nil {
			return seq, closer, nil
		}
		lastErr = err
	}
	return nil, nil, lastErr
}

// GenerateContentOneShot tries each provider in order until one succeeds
func (c *ModelProviderChain) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	var lastErr error
	for _, provider := range c.providers {
		result, err := provider.GenerateContentOneShot(ctx, params)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return OneShotResult{}, lastErr
}

// CountTokens tries each provider in order until one succeeds
func (c *ModelProviderChain) CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error) {
	var lastErr error
	for _, provider := range c.providers {
		result, err := provider.CountTokens(ctx, contents)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// MaxTokens returns the max tokens from the first provider
func (c *ModelProviderChain) MaxTokens() int {
	if len(c.providers) > 0 {
		return c.providers[0].MaxTokens()
	}
	return 0
}

// Name returns the model name
func (c *ModelProviderChain) Name() string {
	return c.modelName
}

// Well-known tasks for subagents.
const (
	SubagentCompressionTask      = "compression"
	SubagentSessionNameTask      = "session_name"
	SubagentWebFetchTask         = "web_fetch"
	SubagentWebFetchFallbackTask = "web_fetch_fallback"
	SubagentImageGenerationTask  = "image_generation"
)

// MockLLMProvider is a mock implementation of LLMProvider interface for testing.
type MockLLMProvider struct {
	SendMessageStreamFunc      func(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error)
	GenerateContentOneShotFunc func(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error)
	CountTokensFunc            func(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error)
	MaxTokensFunc              func(modelName string) int
}

// SendMessageStream implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
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
	return 1000000 // Default fallback
}

var _ LLMProvider = (*MockLLMProvider)(nil)
