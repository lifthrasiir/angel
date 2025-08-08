package main

import (
	"context"
	"net/http"
)

type authContextKeyType string

const authContextKey authContextKeyType = "authContext"

// Auth is an interface that abstracts authentication-related functionalities.
type Auth interface {
	// GetUserEmail returns the email of the currently logged-in user.
	GetUserEmail(r *http.Request) (string, error)
	// IsAuthenticated checks if the current request is authenticated.
	IsAuthenticated(r *http.Request) bool
	// GetCurrentProvider returns the currently used authentication provider.
	GetCurrentProvider() string
	// GetAuthHandler returns the HTTP handler for authentication.
	GetAuthHandler() http.Handler
	// GetAuthCallbackHandler returns the HTTP handler for authentication callbacks.
	GetAuthCallbackHandler() http.Handler
	// GetLogoutHandler returns the HTTP handler for logout.
	GetLogoutHandler() http.Handler
	// GetAuthContext retrieves the Auth implementation from the request context.
	GetAuthContext(ctx context.Context) Auth
	// SetAuthContext sets the Auth implementation into the request context.
	SetAuthContext(ctx context.Context, auth Auth) context.Context
	// Validate performs common authentication and project validation for handlers.
	Validate(handlerName string, w http.ResponseWriter, r *http.Request) bool
}
