package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	. "github.com/lifthrasiir/angel/gemini"
	. "github.com/lifthrasiir/angel/internal/types"
)

// GeminiAuth encapsulates the global state related to authentication providers.
type GeminiAuth struct {
	callbackUrl string
	oauthStates map[string]string // Stores randomState -> originalQueryString
}

// NewGeminiAuth creates a new instance of GeminiAuth.
func NewGeminiAuth(callbackUrl string) *GeminiAuth {
	return &GeminiAuth{
		callbackUrl: callbackUrl,
		oauthStates: make(map[string]string),
	}
}

// THIS IS INTENTIONALLY HARD-CODED TO MATCH GEMINI-CLI!
var geminiCliConfig = oauth2.Config{
	ClientID:     "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com",
	ClientSecret: "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",
	Scopes: []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	},
	Endpoint: google.Endpoint,
}

// THIS IS INTENTIONALLY HARD-CODED TO MATCH ANTIGRAVITY!
var antigravityConfig = oauth2.Config{
	ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
	ClientSecret: "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",
	Scopes: []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	},
	Endpoint: google.Endpoint,
}

// getOAuthConfig returns the OAuth config for the specified provider
func (ga *GeminiAuth) getOAuthConfig(provider string) *oauth2.Config {
	var config oauth2.Config
	switch provider {
	case "geminicli":
		config = geminiCliConfig
	case "antigravity":
		config = antigravityConfig
	default:
		return nil
	}
	config.RedirectURL = ga.callbackUrl
	return &config
}

// TokenSource creates a TokenSource for the given provider and token.
func (ga *GeminiAuth) TokenSource(provider string, token *oauth2.Token) oauth2.TokenSource {
	oauthConfig := ga.getOAuthConfig(provider)
	return oauth2.ReuseTokenSource(token, oauthConfig.TokenSource(context.Background(), token))
}

// GenerateAuthURL generates an OAuth authentication URL and stores the state.
// Returns the auth URL and any error encountered.
func (ga *GeminiAuth) GenerateAuthURL(provider, redirectToQueryString string) (string, error) {
	// Generate a secure random state string
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("error generating random state: %w", err)
	}
	randomState := base64.URLEncoding.EncodeToString(b)

	// Store provider and original query string with the random state
	stateData := map[string]string{
		"provider": provider,
		"query":    redirectToQueryString,
	}
	stateDataJSON, _ := json.Marshal(stateData)
	ga.oauthStates[randomState] = string(stateDataJSON)

	oauthConfig := ga.getOAuthConfig(provider)
	authURL := oauthConfig.AuthCodeURL(randomState)

	return authURL, nil
}

// HandleCallback processes the OAuth callback with the given state and code.
// Returns the redirect URL and any error encountered.
func (ga *GeminiAuth) HandleCallback(ctx context.Context, state, code string) (string, OAuthToken, error) {
	// Validate the random part of the state against the stored value
	stateDataJSON, ok := ga.oauthStates[state]
	if !ok {
		return "", OAuthToken{}, fmt.Errorf("invalid or expired OAuth state: %s", state)
	}
	// Remove the state after use to prevent replay attacks
	delete(ga.oauthStates, state)

	// Parse the stored state data
	var stateData struct {
		Provider string `json:"provider"`
		Query    string `json:"query"`
	}
	if err := json.Unmarshal([]byte(stateDataJSON), &stateData); err != nil {
		return "", OAuthToken{}, fmt.Errorf("error parsing state data: %w", err)
	}

	// Parse the original query string to extract redirect_to
	parsedQuery, err := url.ParseQuery(stateData.Query)
	if err != nil {
		return "", OAuthToken{}, fmt.Errorf("error parsing original query string from state: %w", err)
	}

	frontendPath := parsedQuery.Get("redirect_to")
	if frontendPath == "" {
		frontendPath = "/" // Default to root if not specified
	}

	// Exchange code for token
	oauthConfig := ga.getOAuthConfig(stateData.Provider)
	token, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		return "", OAuthToken{}, fmt.Errorf("failed to exchange token: %w", err)
	}

	// Fetch user info (email) first
	var userEmail string
	userInfoClient := oauthConfig.Client(ctx, token)
	userInfoResp, err := userInfoClient.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		log.Printf("HandleCallback: Failed to fetch user info: %v", err)
		// Non-fatal, continue without email
	} else {
		defer userInfoResp.Body.Close()
		var userInfo struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(userInfoResp.Body).Decode(&userInfo); err != nil {
			log.Printf("HandleCallback: Failed to decode user info JSON: %v", err)
			// Non-fatal, continue without email
		} else {
			userEmail = userInfo.Email
		}
	}

	// Get project ID via Google login flow
	tokenSource := oauth2.ReuseTokenSource(token, oauthConfig.TokenSource(context.Background(), token))
	projectID, err := LoginWithGoogle(ctx, TokenSourceClientProvider(tokenSource))
	if err != nil {
		log.Printf("HandleCallback: Failed to get project ID: %v", err)
		// Continue with empty project ID - this will be handled gracefully
		projectID = ""
	}

	// Save token to database with project ID and provider kind
	tokenJSON, _ := json.MarshalIndent(token, "", "  ")
	oauthToken := OAuthToken{
		TokenData: string(tokenJSON),
		UserEmail: userEmail,
		ProjectID: projectID,
		Kind:      stateData.Provider,
	}

	return frontendPath, oauthToken, nil
}
