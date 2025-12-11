package llm

import (
	"context"
	"fmt"
)

// modelsKey is the private context key for storing the Models registry
type modelsKey struct{}

// ModelsFromContext retrieves the *Models instance from the given context.Context.
// Returns an error if no Models registry is found in the context.
func ModelsFromContext(ctx context.Context) (*Models, error) {
	registry, ok := ctx.Value(modelsKey{}).(*Models)
	if !ok {
		return nil, fmt.Errorf("models registry not found in context")
	}
	return registry, nil
}

// ContextWithModels returns a new context.Context that contains the given *Models instance.
func ContextWithModels(ctx context.Context, registry *Models) context.Context {
	return context.WithValue(ctx, modelsKey{}, registry)
}

// gaKey is the private context key for storing the GeminiAuth
type gaKey struct{}

// GeminiAuthFromContext retrieves the *GeminiAuth instance from the given context.Context.
// Returns an error if no GeminiAuth is found in the context.
func GeminiAuthFromContext(ctx context.Context) (*GeminiAuth, error) {
	ga, ok := ctx.Value(gaKey{}).(*GeminiAuth)
	if !ok {
		return nil, fmt.Errorf("gemini auth not found in context")
	}
	return ga, nil
}

// ContextWithGeminiAuth returns a new context.Context that contains the given *GeminiAuth instance.
func ContextWithGeminiAuth(ctx context.Context, ga *GeminiAuth) context.Context {
	return context.WithValue(ctx, gaKey{}, ga)
}
