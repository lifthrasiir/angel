package llm

import (
	"context"
	"fmt"
	"io"
	"iter"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/llm/spec"
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
	llm                LLMProvider
	modelName          string
	maxTokens          int
	genParams          spec.GenerationParams
	toolSupported      bool
	thoughtEnabled     bool
	ignoreSystemPrompt bool
	responseModalities []string
}

// SendMessageStream calls the underlying provider with the stored model name
func (mp *modelProviderImpl) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	return mp.llm.SendMessageStream(ctx, mp.modelName, params)
}

// GenerateContentOneShot calls the underlying provider with the stored model name
func (mp *modelProviderImpl) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	return mp.llm.GenerateContentOneShot(ctx, mp.modelName, params)
}

// CountTokens calls the underlying provider with the stored model name
func (mp *modelProviderImpl) CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error) {
	return mp.llm.CountTokens(ctx, mp.modelName, contents)
}

// MaxTokens returns the max tokens for this model
func (mp *modelProviderImpl) MaxTokens() int {
	return mp.maxTokens
}

// Name returns the model name
func (mp *modelProviderImpl) Name() string {
	return mp.modelName
}

// GetModelConfig returns the model configuration for providers that need it
func (mp *modelProviderImpl) GetModelConfig() *Model {
	return &Model{
		Name:               mp.modelName,
		ModelName:          mp.modelName,
		GenParams:          mp.genParams,
		MaxTokens:          mp.maxTokens,
		ToolSupported:      mp.toolSupported,
		ThoughtEnabled:     mp.thoughtEnabled,
		IgnoreSystemPrompt: mp.ignoreSystemPrompt,
		ResponseModalities: mp.responseModalities,
	}
}

// newModelProvider creates a new ModelProvider from an LLMProvider, model name, and model spec
func newModelProviderWithSpec(provider LLMProvider, modelName string, modelSpec *spec.ModelSpec) ModelProvider {
	var genParams spec.GenerationParams
	var maxTokens int
	var toolSupported, thoughtEnabled, ignoreSystemPrompt bool
	var responseModalities []string

	if modelSpec != nil {
		genParams = modelSpec.GenParams
		maxTokens = modelSpec.MaxTokens
		toolSupported = modelSpec.ToolSupported
		thoughtEnabled = modelSpec.ThoughtEnabled
		ignoreSystemPrompt = modelSpec.IgnoreSystemPrompt
		responseModalities = modelSpec.ResponseModalities
	} else {
		// Default values
		genParams = spec.GenerationParams{}
		maxTokens = 1048576
		toolSupported = true
		thoughtEnabled = true
		ignoreSystemPrompt = false
		responseModalities = []string{"TEXT"}
	}

	return &modelProviderImpl{
		llm:                provider,
		modelName:          modelName,
		maxTokens:          maxTokens,
		genParams:          genParams,
		toolSupported:      toolSupported,
		thoughtEnabled:     thoughtEnabled,
		ignoreSystemPrompt: ignoreSystemPrompt,
		responseModalities: responseModalities,
	}
}

// newModelProvider creates a new ModelProvider from an LLMProvider and model name
// Uses default model configuration
func newModelProvider(provider LLMProvider, modelName string) ModelProvider {
	return newModelProviderWithSpec(provider, modelName, nil)
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
