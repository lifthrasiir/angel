package main

import (
	"context"
	"fmt"
	"io"
	"iter"
)

var CurrentProvider LLMProvider

// SessionParams holds the parameters for a chat session.
type SessionParams struct {
	Contents       []Content
	ModelName      string
	SystemPrompt   string
	ThinkingConfig *ThinkingConfig
}

// LLMProvider defines the interface for interacting with an LLM.
type LLMProvider interface {
	SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error)
	GenerateContentOneShot(ctx context.Context, params SessionParams) (string, error)
	CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error)
}

// MockLLMProvider is a mock implementation of the LLMProvider interface for testing.
type MockLLMProvider struct {
	SendMessageStreamFunc      func(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error)
	GenerateContentOneShotFunc func(ctx context.Context, params SessionParams) (string, error)
	CountTokensFunc            func(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error)
}

// SendMessageStream implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	if m.SendMessageStreamFunc != nil {
		return m.SendMessageStreamFunc(ctx, params)
	}
	return nil, nil, fmt.Errorf("SendMessageStream not implemented in mock")
}

// GenerateContentOneShot implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (string, error) {
	if m.GenerateContentOneShotFunc != nil {
		return m.GenerateContentOneShotFunc(ctx, params)
	}
	return "", fmt.Errorf("GenerateContentOneShot not implemented in mock")
}

// CountTokens implements the LLMProvider interface for MockLLMProvider.
func (m *MockLLMProvider) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	if m.CountTokensFunc != nil {
		return m.CountTokensFunc(ctx, contents, modelName)
	}
	return nil, fmt.Errorf("CountTokens not implemented in mock")
}
