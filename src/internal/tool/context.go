package tool

import (
	"context"
	"fmt"
)

// toolsKey is the private context key for storing the Tools instance
type toolsKey struct{}

// FromContext retrieves the *Tools instance from the given context.Context.
// Returns nil if no Tools instance is found in the context.
func FromContext(ctx context.Context) (*Tools, error) {
	tools, ok := ctx.Value(toolsKey{}).(*Tools)
	if !ok {
		return nil, fmt.Errorf("tools not found in context")
	}
	return tools, nil
}

// ContextWith returns a new context.Context that contains the given *Tools instance.
func ContextWith(ctx context.Context, tools *Tools) context.Context {
	return context.WithValue(ctx, toolsKey{}, tools)
}
