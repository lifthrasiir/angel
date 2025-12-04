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

// GeminiAuth encapsulates the global state related to Gemini client and authentication.
type GeminiAuth struct {
	GoogleOauthConfig *oauth2.Config
	Token             *oauth2.Token
	TokenSource       HTTPClientProvider
	ProjectID         string
	UserEmail         string
	initMutex         sync.Mutex
	db                *sql.DB
	oauthStates       map[string]string // Stores randomState -> originalQueryString
}

// NewGeminiAuth creates a new instance of GeminiAuth.
func NewGeminiAuth(db *sql.DB) *GeminiAuth {
	return &GeminiAuth{
		db:          db,
		oauthStates: make(map[string]string),
	}
}

// saveToken saves the OAuth token to the database.
func (ga *GeminiAuth) SaveToken(db *sql.DB, t *oauth2.Token) {
	ga.SaveTokenWithKind(db, t, "geminicli")
}

// saveTokenWithKind saves the OAuth token to the database with a specific kind.
func (ga *GeminiAuth) SaveTokenWithKind(db *sql.DB, t *oauth2.Token, kind string) {
	tokenJSON, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal token: %v", err)
		return
	}

	if err := SaveOAuthTokenWithKind(db, string(tokenJSON), ga.UserEmail, ga.ProjectID, kind); err != nil {
		log.Printf("Failed to save OAuth token to DB: %v", err)
		return
	}
	log.Printf("OAuth token saved to DB with kind %s.", kind)
	ga.Token = t // Update global token variable
}

// loadToken loads the OAuth token from the database.
func (ga *GeminiAuth) LoadToken(db *sql.DB) {
	tokenJSON, userEmail, projectID, err := LoadOAuthToken(db)
	if err != nil {
		log.Printf("LoadToken: Failed to load OAuth token from DB: %v", err)
		return
	}

	if tokenJSON == "" {
		log.Println("LoadToken: No existing token in DB.")
		return
	}

	if err := json.Unmarshal([]byte(tokenJSON), &ga.Token); err != nil {
		log.Printf("LoadToken: Failed to decode token from DB: %v", err)
		return
	}
	ga.UserEmail = userEmail
	ga.ProjectID = projectID

}

// Init initializes the authentication state.
func (ga *GeminiAuth) Init() {
	ga.initMutex.Lock()
	defer ga.initMutex.Unlock()

	ga.LoadToken(ga.db)

	// THIS IS INTENTIONALLY HARD-CODED TO MATCH GEMINI-CLI!
	ga.GoogleOauthConfig = &oauth2.Config{
		ClientID:     "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com",
		ClientSecret: "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",
		RedirectURL:  "http://localhost:8080/oauth2callback", // Web app redirect URI
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	// Attempt to load existing token on startup
	ga.LoadToken(ga.db)

	// Initialize GenAI service
	ga.InitCurrentProvider()
}

// InitCurrentProvider initializes the TokenSource and ProjectID based on GeminiAuth state.
func (ga *GeminiAuth) InitCurrentProvider() {
	ctx := context.Background()
	var clientProvider HTTPClientProvider

	// Clear only Gemini-related providers, not all providers
	GlobalModelsRegistry.ClearGeminiProviders()

	if ga.Token == nil {
		log.Println("InitCurrentProvider: OAuth token is nil. Cannot initialize client.")
		ga.TokenSource = nil // Ensure TokenSource is nil if token is not available
		ga.ProjectID = ""
		return
	}
	// Always create a new token source to ensure it's up-to-date with the current token
	ga.TokenSource = &tokenSaverSource{
		TokenSource: ga.GoogleOauthConfig.TokenSource(ctx, ga.Token),
		ga:          ga,
	}
	clientProvider = ga.TokenSource

	// Proactively refresh token on startup if expired
	// This will also save the new token via tokenSaverSource.Token()
	_, err := ga.TokenSource.(*tokenSaverSource).Token() // This call will trigger refresh if needed
	if err != nil {
		log.Printf("InitCurrentProvider: Failed to proactively refresh token on startup: %v. Client not initialized.", err)
		ga.TokenSource = nil // Ensure TokenSource is nil on error
		ga.ProjectID = ""
		return
	}

	// If ProjectID is not set, try to get it and/or perform onboarding
	if ga.ProjectID == "" {
		var projectID string
		if projectID, err = LoginWithGoogle(ctx, clientProvider, ga.ProjectID); err != nil {
			log.Printf("InitCurrentProvider: initWithGoogle failed: %v. ProjectID not set.", err)
			ga.ProjectID = ""
			return
		} else {
			ga.ProjectID = projectID
		}
	}

	// Ensure the final ProjectID is saved to the database
	ga.SaveToken(ga.db, ga.Token)

	// Centralized GlobalModelsRegistry population
	if ga.TokenSource != nil {
		client := NewCodeAssistClient(ga.TokenSource, ga.ProjectID)

		// Register Gemini models in GlobalModelsRegistry
		GlobalModelsRegistry.SetGeminiCodeAssistClient(client)
	} else {
		log.Println("InitCurrentProvider: No valid TokenSource available. LLM clients will not be initialized.")
	}
}

// GetUserEmail returns the email of the currently logged-in user.
func (ga *GeminiAuth) GetUserEmail(r *http.Request) (string, error) {
	if ga.UserEmail == "" {
		return "", fmt.Errorf("user not authenticated")
	}
	return ga.UserEmail, nil
}

// IsAuthenticated checks if the current request is authenticated.
func (ga *GeminiAuth) IsAuthenticated(r *http.Request) bool {
	return ga.Token != nil && ga.Token.Valid() && ga.UserEmail != ""
}

// GetAuthHandler returns the HTTP handler for authentication.
func (ga *GeminiAuth) GetAuthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the entire raw query string from the /login request.
		// This will contain both 'redirect_to' and 'draft_message'.
		redirectToQueryString := r.URL.RawQuery
		if redirectToQueryString == "" {
			// If no query parameters, default to redirecting to the root.
			// This case might not be hit if frontend always sends redirect_to.
			redirectToQueryString = "redirect_to=/" // Ensure a default redirect_to
		}

		// Generate a secure random state string
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			log.Printf("Error generating random state: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		randomState := base64.URLEncoding.EncodeToString(b)
		ga.oauthStates[randomState] = redirectToQueryString // Store the original query string with the random state

		url := ga.GoogleOauthConfig.AuthCodeURL(randomState)

		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})
}

// GetAuthCallbackHandler returns the HTTP handler for authentication callbacks.
func (ga *GeminiAuth) GetAuthCallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateParam := r.FormValue("state")

		// Validate the random part of the state against the stored value
		originalQueryString, ok := ga.oauthStates[stateParam]
		if !ok {
			log.Printf("Invalid or expired OAuth state: %s", stateParam)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}
		// Remove the state after use to prevent replay attacks
		delete(ga.oauthStates, stateParam)

		// Parse the original query string to extract redirect_to
		parsedQuery, err := url.ParseQuery(originalQueryString)
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
		Token, err := ga.GoogleOauthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Printf("Failed to exchange token: %v", err)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}

		// Fetch user info (email) first
		userInfoClient := ga.GoogleOauthConfig.Client(context.Background(), Token)
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
				ga.UserEmail = userInfo.Email
			}
		}

		// Save token to file after setting userEmail
		ga.SaveToken(ga.db, Token)

		// Re-initialize CurrentProviders after successful authentication and user info fetch
		ga.InitCurrentProvider()

		// Redirect to the original path after successful authentication
		http.Redirect(w, r, finalRedirectURL, http.StatusTemporaryRedirect)
	})
}

// GetLogoutHandler returns the HTTP handler for logout.
func (ga *GeminiAuth) GetLogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			err = DeleteOAuthTokenByID(ga.db, request.ID)
			log.Printf("Logged out account with ID %d", request.ID)
		} else if request.Email != "" {
			// Delete specific account by email
			err = DeleteOAuthTokenByEmail(ga.db, request.Email, "geminicli")
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

		// Clear current auth state if it was the logged out account
		if request.Email == ga.UserEmail {
			ga.Token = nil
			ga.UserEmail = ""
			ga.ProjectID = ""
			GlobalModelsRegistry.Clear()
			// Try to initialize with next available account
			ga.InitCurrentProvider()
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Account logged out successfully")
	})
}

// GetAuthContext retrieves the Auth implementation from the request context.
func (ga *GeminiAuth) GetAuthContext(ctx context.Context) Auth {
	val := ctx.Value(authContextKey)
	if val == nil {
		return nil
	}
	return val.(Auth)
}

// SetAuthContext sets the Auth implementation into the request context.
func (ga *GeminiAuth) SetAuthContext(ctx context.Context, auth Auth) context.Context {
	return context.WithValue(ctx, authContextKey, auth)
}

// Validate performs common auth and project validation for handlers
func (ga *GeminiAuth) Validate(handlerName string, w http.ResponseWriter, r *http.Request) bool {
	ga.initMutex.Lock()
	defer ga.initMutex.Unlock()

	// Detailed logging at the start of ValidateAuthAndProject
	// Check if any provider is initialized and attempt to re-initialize if needed
	if GlobalModelsRegistry.IsEmpty() {
		log.Printf("%s: No GeminiClient initialized, attempting to re-initialize...", handlerName)
		if ga.Token != nil {
			// Attempt to re-initialize, which includes token refresh if needed
			ga.InitCurrentProvider()
			log.Printf("%s: GlobalModelsRegistry state after InitCurrentProvider: %t, ProjectID: %s", handlerName, GlobalModelsRegistry.IsEmpty(), ga.ProjectID)
		}
		// After attempting re-initialization, check GlobalModelsRegistry again
		if GlobalModelsRegistry.IsEmpty() {
			log.Printf("%s: GeminiClient still not initialized after re-attempt.", handlerName)
			// If CurrentProviders is still empty, it means token refresh failed or no token exists.
			// Provide a more user-friendly message.
			if ga.Token != nil && ga.Token.RefreshToken == "" {
				http.Error(w, "Session expired. Please log in again (no refresh token).", http.StatusUnauthorized)
			} else if ga.Token != nil {
				http.Error(w, "Session expired. Please log in again (token refresh failed).", http.StatusUnauthorized)
			} else {
				http.Error(w, "CodeAssist client not initialized. Please log in.", http.StatusUnauthorized)
			}
			return false
		}
	}

	if ga.ProjectID == "" {
		log.Printf("%s: Project ID is not set. Please log in again.", handlerName)
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return false
	}
	return true
}
