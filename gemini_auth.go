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
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GeminiAuth encapsulates the global state related to Gemini client and authentication.
type GeminiAuth struct {
	GoogleOauthConfig *oauth2.Config
	Token             *oauth2.Token
	TokenSource       HTTPClientProvider
	ProjectID         string
	SelectedAuthType  AuthType
	UserEmail         string
	initMutex         sync.Mutex
	db                *sql.DB           // Add db field
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
	tokenJSON, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal token: %v", err)
		return
	}

	if err := SaveOAuthToken(db, string(tokenJSON), ga.UserEmail, ga.ProjectID); err != nil {
		log.Printf("Failed to save OAuth token to DB: %v", err)
		return
	}
	log.Println("OAuth token saved to DB.")
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
	// Select authentication method from environment variables
	if os.Getenv("GEMINI_API_KEY") != "" {
		ga.SelectedAuthType = AuthTypeUseGemini
		log.Println("Authentication method: Using Gemini API Key")
	} else if os.Getenv("GOOGLE_API_KEY") != "" || (os.Getenv("GOOGLE_CLOUD_PROJECT") != "" && os.Getenv("GOOGLE_CLOUD_LOCATION") != "") {
		ga.SelectedAuthType = AuthTypeUseVertexAI
		log.Println("Authentication method: Using Vertex AI")
	} else {
		ga.SelectedAuthType = AuthTypeLoginWithGoogle
		log.Println("Authentication method: Using Google Login (OAuth)")
	}

	// Branch initialization logic based on authentication method
	switch ga.SelectedAuthType {
	case AuthTypeLoginWithGoogle:
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
	case AuthTypeUseGemini:
		ga.InitCurrentProvider()
	case AuthTypeUseVertexAI:
		ga.InitCurrentProvider()
	case AuthTypeCloudShell:
		ga.InitCurrentProvider()
	}
}

// InitCurrentProvider initializes the CurrentProvider based on GeminiAuth state.
func (ga *GeminiAuth) InitCurrentProvider() {
	ctx := context.Background()
	var clientProvider HTTPClientProvider

	switch ga.SelectedAuthType {
	case AuthTypeLoginWithGoogle:
		if ga.Token == nil {
			log.Println("InitCurrentProvider: OAuth token is nil. Cannot initialize client.")
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
			CurrentProvider = nil
			return
		}

		caClient := NewCodeAssistClient(clientProvider, ga.ProjectID) // Initialize with current ProjectID

		// Only call LoadCodeAssist and OnboardUser if ProjectID is not set or needs re-validation
		if ga.ProjectID == "" {
			loadReq := LoadCodeAssistRequest{
				CloudaicompanionProject: ga.ProjectID, // Will be empty
				Metadata: &ClientMetadata{
					IdeType:     "IDE_UNSPECIFIED",
					Platform:    "PLATFORM_UNSPECIFIED",
					PluginType:  "GEMINI",
					DuetProject: ga.ProjectID,
				},
			}
			loadRes, loadErr := caClient.LoadCodeAssist(ctx, loadReq)
			if loadErr != nil {
				log.Printf("InitCurrentProvider: LoadCodeAssist failed: %v. Client not initialized.", loadErr)
				CurrentProvider = nil
				return
			}

			if loadRes.CloudaicompanionProject != "" {
				ga.ProjectID = loadRes.CloudaicompanionProject

				var userTierID UserTierID
				if loadRes.CurrentTier != nil {
					userTierID = loadRes.CurrentTier.ID
				} else {
					for _, tier := range loadRes.AllowedTiers {
						if tier.IsDefault != nil && *tier.IsDefault {
							userTierID = tier.ID
							break
						}
					}
				}

				onboardReq := OnboardUserRequest{
					CloudaicompanionProject: ga.ProjectID,
					Metadata: &ClientMetadata{
						IdeType:     "IDE_UNSPECIFIED",
						Platform:    "PLATFORM_UNSPECIFIED",
						PluginType:  "GEMINI",
						DuetProject: ga.ProjectID,
					},
				}
				if userTierID != "" {
					onboardReq.TierID = &userTierID
				}
				lroRes, onboardErr := caClient.OnboardUser(ctx, onboardReq)
				if onboardErr != nil {
					log.Printf("InitCurrentProvider: OnboardUser failed: %v. Client not initialized.", onboardErr)
					CurrentProvider = nil
					return
				}

				if lroRes.Response != nil && lroRes.Response.CloudaicompanionProject != nil {
					ga.ProjectID = lroRes.Response.CloudaicompanionProject.ID
				} else {
					log.Println("InitCurrentProvider: No project ID from OnboardUser. Client not initialized.")
					CurrentProvider = nil
					return
				}
			} else {
				log.Println("InitCurrentProvider: LoadCodeAssist did not return a Project ID. Client not initialized.")
				CurrentProvider = nil
				return
			}
		} else {
			// If ProjectID is already set, skip LoadCodeAssist and OnboardUser
		}

		// Ensure the final ProjectID is saved to the database
		ga.SaveToken(ga.db, ga.Token)

	case AuthTypeUseGemini:
		clientProvider = &defaultHTTPClientProvider{}
	case AuthTypeUseVertexAI:
		clientProvider = &defaultHTTPClientProvider{}
	case AuthTypeCloudShell:
		clientProvider = &defaultHTTPClientProvider{}
	default:
		log.Fatalf("InitCurrentProvider: Unsupported authentication type: %s", ga.SelectedAuthType)
	}

	CurrentProvider = NewCodeAssistClient(clientProvider, ga.ProjectID)
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

// GetCurrentProvider returns the currently used authentication provider.
func (ga *GeminiAuth) GetCurrentProvider() string {
	return string(ga.SelectedAuthType)
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

		// Parse the original query string to extract redirect_to and draft_message
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
		draftMessage := parsedQuery.Get("draft_message")

		// Construct the final URL for the frontend
		finalRedirectURL := frontendPath
		if draftMessage != "" {
			// Check if frontendPath already has query parameters
			if strings.Contains(frontendPath, "?") {
				finalRedirectURL = fmt.Sprintf("%s&draft_message=%s", frontendPath, url.QueryEscape(draftMessage))
			} else {
				finalRedirectURL = fmt.Sprintf("%s?draft_message=%s", frontendPath, url.QueryEscape(draftMessage))
			}
		}

		code := r.FormValue("code")
		Token, err := ga.GoogleOauthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Printf("Failed to exchange token: %v", err)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}

		// Save token to file
		ga.SaveToken(ga.db, Token)

		// Fetch user info (email)
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

		// Re-initialize CurrentProvider after successful authentication and user info fetch
		ga.InitCurrentProvider()

		// Redirect to the original path after successful authentication
		http.Redirect(w, r, finalRedirectURL, http.StatusTemporaryRedirect)
	})
}

// GetLogoutHandler returns the HTTP handler for logout.
func (ga *GeminiAuth) GetLogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ga.Token = nil
		ga.UserEmail = ""
		CurrentProvider = nil

		// Delete token from DB
		if err := DeleteOAuthToken(ga.db); err != nil {
			log.Printf("Failed to delete OAuth token from DB: %v", err)
			http.Error(w, "Failed to logout", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Logged out successfully")
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
	if ga.Token != nil {
		// If token is invalid but CurrentProvider is not nil, force CurrentProvider to nil
		if !ga.Token.Valid() && CurrentProvider != nil {
			log.Println("Validate: Invalid token detected, forcing CurrentProvider to nil for re-initialization.")
			CurrentProvider = nil
		}
	} else {
		// If token is nil but CurrentProvider is not nil, force CurrentProvider to nil
		if CurrentProvider != nil {
			log.Println("Validate: Token is nil, forcing CurrentProvider to nil for re-initialization.")
			CurrentProvider = nil
		}
	}

	// Check if GeminiClient is initialized and attempt to re-initialize if needed
	if CurrentProvider == nil {
		log.Printf("%s: GeminiClient not initialized, attempting to re-initialize...", handlerName)
		if ga.SelectedAuthType == AuthTypeLoginWithGoogle && ga.Token != nil {
			// Attempt to re-initialize, which includes token refresh if needed
			ga.InitCurrentProvider()
			log.Printf("%s: CurrentProvider state after InitCurrentProvider: %t, ProjectID: %s", handlerName, CurrentProvider == nil, ga.ProjectID)
		}
		// After attempting re-initialization, check CurrentProvider again
		if CurrentProvider == nil {
			log.Printf("%s: GeminiClient still not initialized after re-attempt.", handlerName)
			// If CurrentProvider is still nil, it means token refresh failed or no token exists.
			// Provide a more user-friendly message.
			if ga.SelectedAuthType == AuthTypeLoginWithGoogle && ga.Token != nil && ga.Token.RefreshToken == "" {
				http.Error(w, "Session expired. Please log in again (no refresh token).", http.StatusUnauthorized)
			} else if ga.SelectedAuthType == AuthTypeLoginWithGoogle && ga.Token != nil {
				http.Error(w, "Session expired. Please log in again (token refresh failed).", http.StatusUnauthorized)
			} else {
				http.Error(w, "CodeAssist client not initialized. Check authentication method or log in.", http.StatusUnauthorized)
			}
			return false
		}
	}

	if (ga.SelectedAuthType == AuthTypeLoginWithGoogle || ga.SelectedAuthType == AuthTypeUseGemini || ga.SelectedAuthType == AuthTypeUseVertexAI) && ga.ProjectID == "" {
		log.Printf("%s: Project ID is not set. Please log in again or set GOOGLE_CLOUD_PROJECT.", handlerName)
		http.Error(w, "Project ID is not set. Please log in again or set GOOGLE_CLOUD_PROJECT.", http.StatusUnauthorized)
		return false
	}
	return true
}
