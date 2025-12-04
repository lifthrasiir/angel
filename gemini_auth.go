package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	. "github.com/lifthrasiir/angel/gemini"
)

// GeminiAuth encapsulates the global state related to authentication providers.
type GeminiAuth struct {
	initMutex            sync.Mutex
	db                   *sql.DB
	oauthStates          map[string]string // Stores randomState -> originalQueryString
	providersInitialized bool
}

// NewGeminiAuth creates a new instance of GeminiAuth.
func NewGeminiAuth(db *sql.DB) *GeminiAuth {
	return &GeminiAuth{
		db:          db,
		oauthStates: make(map[string]string),
	}
}

// SaveToken saves the OAuth token to the database.
func (ga *GeminiAuth) SaveToken(db *sql.DB, t *oauth2.Token, userEmail string, projectID string) {
	ga.SaveTokenWithKind(db, t, userEmail, projectID, "geminicli")
}

// SaveTokenWithKind saves the OAuth token to the database with a specific kind.
func (ga *GeminiAuth) SaveTokenWithKind(db *sql.DB, t *oauth2.Token, userEmail string, projectID string, kind string) {
	tokenJSON, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal token: %v", err)
		return
	}

	if err := SaveOAuthTokenWithKind(db, string(tokenJSON), userEmail, projectID, kind); err != nil {
		log.Printf("Failed to save OAuth token to DB: %v", err)
		return
	}
	log.Printf("OAuth token saved to DB with kind %s for user %s.", kind, userEmail)
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

// Init initializes the authentication state.
func (ga *GeminiAuth) Init() {
	ga.initMutex.Lock()
	defer ga.initMutex.Unlock()

	// Initialize all authentication providers
	ga.EnsureProvidersInitialized()
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
		config = geminiCliConfig // default to geminicli
	}
	config.RedirectURL = "http://localhost:8080/oauth2callback"
	return &config
}

// HasAuthenticatedProviders checks if any authentication providers are configured.
func (ga *GeminiAuth) HasAuthenticatedProviders() bool {
	// Check if there are any OAuth tokens
	tokens, err := GetOAuthTokens(ga.db)
	if err != nil {
		log.Printf("HasAuthenticatedProviders: Failed to check OAuth tokens: %v", err)
		return false
	}

	for _, token := range tokens {
		if token.Kind == "geminicli" || token.Kind == "antigravity" {
			return true
		}
	}

	// Check if there are any API key configurations
	// This would need to be implemented based on your API key storage
	// For now, just check if there are any Gemini API configs
	var geminiConfigsCount int
	err = ga.db.QueryRow("SELECT COUNT(*) FROM gemini_api_configs WHERE enabled = 1").Scan(&geminiConfigsCount)
	if err != nil {
		log.Printf("HasAuthenticatedProviders: Failed to check Gemini API configs: %v", err)
		return false
	}

	if geminiConfigsCount > 0 {
		return true
	}

	// Check OpenAI configs too
	var openaiConfigsCount int
	err = ga.db.QueryRow("SELECT COUNT(*) FROM openai_configs WHERE enabled = 1").Scan(&openaiConfigsCount)
	if err != nil {
		log.Printf("HasAuthenticatedProviders: Failed to check OpenAI configs: %v", err)
		return false
	}

	return openaiConfigsCount > 0
}

// EnsureProvidersInitialized ensures that authentication providers are properly initialized.
func (ga *GeminiAuth) EnsureProvidersInitialized() error {
	ctx := context.Background()

	// Clear only Gemini-related providers, not all providers
	GlobalModelsRegistry.ClearGeminiProviders()

	// Initialize Google OAuth providers if tokens exist
	tokens, err := GetOAuthTokens(ga.db)
	if err != nil {
		return fmt.Errorf("failed to get OAuth tokens: %w", err)
	}

	hasGeminiTokens := false
	hasAntigravityTokens := false
	for _, token := range tokens {
		switch token.Kind {
		case "geminicli":
			hasGeminiTokens = true
		case "antigravity":
			hasAntigravityTokens = true
		}
	}

	// Initialize Gemini CLI providers
	if hasGeminiTokens {
		// Get the least recently used token for initialization
		selectedToken, err := GetNextOAuthToken(ga.db, "geminicli", "")
		if err != nil {
			return fmt.Errorf("failed to get next OAuth token: %w", err)
		}

		if selectedToken != nil {
			var oauthToken oauth2.Token
			if err := json.Unmarshal([]byte(selectedToken.TokenData), &oauthToken); err != nil {
				return fmt.Errorf("failed to unmarshal OAuth token: %w", err)
			}

			// Create token source
			oauthConfig := ga.getOAuthConfig("geminicli")
			tokenSource := oauthConfig.TokenSource(ctx, &oauthToken)

			// Set up CodeAssist client
			client := NewCodeAssistClient(&tokenSourceProvider{TokenSource: tokenSource}, selectedToken.ProjectID, "geminicli")
			GlobalModelsRegistry.SetGeminiCodeAssistClient(client)

			log.Printf("Initialized Gemini provider with token for user %s", selectedToken.UserEmail)
		}
	}

	// Initialize Antigravity providers
	if hasAntigravityTokens {
		// Get the least recently used token for initialization
		selectedToken, err := GetNextOAuthToken(ga.db, "antigravity", "")
		if err != nil {
			return fmt.Errorf("failed to get next OAuth token: %w", err)
		}

		if selectedToken != nil {
			var oauthToken oauth2.Token
			if err := json.Unmarshal([]byte(selectedToken.TokenData), &oauthToken); err != nil {
				return fmt.Errorf("failed to unmarshal OAuth token: %w", err)
			}

			// Create token source
			oauthConfig := ga.getOAuthConfig("antigravity")
			tokenSource := oauthConfig.TokenSource(ctx, &oauthToken)

			// Set up CodeAssist client
			client := NewCodeAssistClient(&tokenSourceProvider{TokenSource: tokenSource}, selectedToken.ProjectID, "antigravity")
			GlobalModelsRegistry.SetGeminiCodeAssistClient(client)

			log.Printf("Initialized Antigravity provider with token for user %s", selectedToken.UserEmail)
		}
	}

	ga.providersInitialized = true
	return nil
}

// tokenSourceProvider implements HTTPClientProvider for oauth2.TokenSource
type tokenSourceProvider struct {
	TokenSource oauth2.TokenSource
}

func (tsp *tokenSourceProvider) Client(ctx context.Context) *http.Client {
	return oauth2.NewClient(ctx, tsp.TokenSource)
}

// IsAuthenticated checks if the current request is authenticated.
func (ga *GeminiAuth) IsAuthenticated(r *http.Request) bool {
	return ga.HasAuthenticatedProviders()
}

// authHandler handles authentication requests.
func authHandler(w http.ResponseWriter, r *http.Request) {
	// Capture the entire raw query string from the /login request.
	// This will contain both 'redirect_to' and 'draft_message'.
	redirectToQueryString := r.URL.RawQuery
	if redirectToQueryString == "" {
		// If no query parameters, default to redirecting to the root.
		// This case might not be hit if frontend always sends redirect_to.
		redirectToQueryString = "redirect_to=/" // Ensure a default redirect_to
	}

	// Parse query parameters to get provider
	queryParams, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		log.Printf("Error parsing query parameters: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	provider := queryParams.Get("provider")
	if provider == "" {
		provider = "geminicli" // default provider
	}

	// Generate a secure random state string
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("Error generating random state: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	randomState := base64.URLEncoding.EncodeToString(b)

	// Store provider and original query string with the random state
	stateData := map[string]string{
		"provider": provider,
		"query":    redirectToQueryString,
	}
	stateDataJSON, _ := json.Marshal(stateData)
	GlobalGeminiAuth.oauthStates[randomState] = string(stateDataJSON)

	oauthConfig := GlobalGeminiAuth.getOAuthConfig(provider)
	url := oauthConfig.AuthCodeURL(randomState)

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// authCallbackHandler handles authentication callback requests.
func authCallbackHandler(w http.ResponseWriter, r *http.Request) {
	stateParam := r.FormValue("state")

	// Validate the random part of the state against the stored value
	stateDataJSON, ok := GlobalGeminiAuth.oauthStates[stateParam]
	if !ok {
		log.Printf("Invalid or expired OAuth state: %s", stateParam)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	// Remove the state after use to prevent replay attacks
	delete(GlobalGeminiAuth.oauthStates, stateParam)

	// Parse the stored state data
	var stateData struct {
		Provider string `json:"provider"`
		Query    string `json:"query"`
	}
	if err := json.Unmarshal([]byte(stateDataJSON), &stateData); err != nil {
		log.Printf("Error parsing state data: %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// Parse the original query string to extract redirect_to
	parsedQuery, err := url.ParseQuery(stateData.Query)
	if err != nil {
		log.Printf("Error parsing original query string from state: %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	frontendPath := parsedQuery.Get("redirect_to")
	if frontendPath == "" {
		frontendPath = "/" // Default to root if not specified
	}

	// Construct the final URL for the frontend
	finalRedirectURL := frontendPath

	code := r.FormValue("code")
	oauthConfig := GlobalGeminiAuth.getOAuthConfig(stateData.Provider)
	Token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Failed to exchange token: %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// Fetch user info (email) first
	var userEmail string
	userInfoClient := oauthConfig.Client(context.Background(), Token)
	userInfoResp, err := userInfoClient.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		log.Printf("HandleGoogleCallback: Failed to fetch user info: %v", err)
		// Non-fatal, continue without email
	} else {
		defer userInfoResp.Body.Close()
		var userInfo struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(userInfoResp.Body).Decode(&userInfo); err != nil {
			log.Printf("HandleGoogleCallback: Failed to decode user info JSON: %v", err)
			// Non-fatal, continue without email
		} else {
			userEmail = userInfo.Email
		}
	}

	// Get project ID via Google login flow
	tokenSourceProvider := &tokenSourceProvider{TokenSource: oauth2.ReuseTokenSource(Token, oauthConfig.TokenSource(context.Background(), Token))}
	projectID, err := LoginWithGoogle(context.Background(), tokenSourceProvider, "")
	if err != nil {
		log.Printf("HandleGoogleCallback: Failed to get project ID: %v", err)
		// Continue with empty project ID - this will be handled gracefully
		projectID = ""
	}

	// Save token to database with project ID and provider kind
	GlobalGeminiAuth.SaveTokenWithKind(GlobalGeminiAuth.db, Token, userEmail, projectID, stateData.Provider)

	// Re-initialize providers after successful authentication
	GlobalGeminiAuth.EnsureProvidersInitialized()

	// Redirect to the original path after successful authentication
	http.Redirect(w, r, finalRedirectURL, http.StatusTemporaryRedirect)
}

// logoutHandler handles logout requests.
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	// Parse request body to get which account to logout
	var request struct {
		Email string `json:"email"`
		ID    int    `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Printf("Failed to decode logout request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var err error
	if request.ID != 0 {
		// Delete specific account by ID
		err = DeleteOAuthTokenByID(GlobalGeminiAuth.db, request.ID)
		log.Printf("Logged out account with ID %d", request.ID)
	} else if request.Email != "" {
		// Delete specific account by email
		err = DeleteOAuthTokenByEmail(GlobalGeminiAuth.db, request.Email, "geminicli")
		log.Printf("Logged out account for email %s", request.Email)
	} else {
		http.Error(w, "Either email or id must be provided", http.StatusBadRequest)
		return
	}

	if err != nil {
		log.Printf("Failed to delete OAuth token from DB: %v", err)
		http.Error(w, "Failed to logout", http.StatusInternalServerError)
		return
	}

	// Re-initialize providers after account removal
	GlobalGeminiAuth.EnsureProvidersInitialized()

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Account logged out successfully")
}
