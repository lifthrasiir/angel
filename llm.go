package main

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

// ModelProvider wraps LLMProvider and automatically manages model name
type ModelProvider struct {
	LLMProvider
	Name string
}

// SendMessageStream calls the underlying provider with the stored model name
func (mp ModelProvider) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	return mp.LLMProvider.SendMessageStream(ctx, mp.Name, params)
}

// GenerateContentOneShot calls the underlying provider with the stored model name
func (mp ModelProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	return mp.LLMProvider.GenerateContentOneShot(ctx, mp.Name, params)
}

// CountTokens calls the underlying provider with the stored model name
func (mp ModelProvider) CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error) {
	return mp.LLMProvider.CountTokens(ctx, mp.Name, contents)
}

// MaxTokens calls the underlying provider with the stored model name
func (mp ModelProvider) MaxTokens() int {
	return mp.LLMProvider.MaxTokens(mp.Name)
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
