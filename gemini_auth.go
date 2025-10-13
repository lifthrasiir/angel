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
	"time"

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

// InitCurrentProvider initializes the TokenSource and ProjectID based on GeminiAuth state.
func (ga *GeminiAuth) InitCurrentProvider() {
	ctx := context.Background()
	var clientProvider HTTPClientProvider

	// Clear CurrentProviders at the beginning of InitCurrentProvider
	// to ensure a clean state before re-populating.
	CurrentProviders = make(map[string]LLMProvider)

	switch ga.SelectedAuthType {
	case AuthTypeLoginWithGoogle:
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
			if err := ga.initWithGoogle(ctx, clientProvider); err != nil {
				log.Printf("InitCurrentProvider: initWithGoogle failed: %v. ProjectID not set.", err)
				ga.ProjectID = ""
				return
			}
		}

		// Ensure the final ProjectID is saved to the database
		ga.SaveToken(ga.db, ga.Token)

	case AuthTypeUseGemini:
		clientProvider = &defaultHTTPClientProvider{}
		ga.TokenSource = clientProvider
		// ProjectID is not needed for Gemini API Key
		ga.ProjectID = ""
	case AuthTypeUseVertexAI:
		clientProvider = &defaultHTTPClientProvider{}
		ga.TokenSource = clientProvider
		// ProjectID should be set from env for Vertex AI
		ga.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	case AuthTypeCloudShell:
		clientProvider = &defaultHTTPClientProvider{}
		ga.TokenSource = clientProvider
		// ProjectID should be set from env for Cloud Shell
		ga.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	default:
		log.Fatalf("InitCurrentProvider: Unsupported authentication type: %s", ga.SelectedAuthType)
	}

	// Centralized CurrentProviders population
	if ga.TokenSource != nil {
		geminiFlashClient = NewCodeAssistClient(ga.TokenSource, ga.ProjectID, "gemini-2.5-flash", true, true)
		CurrentProviders["gemini-2.5-flash"] = geminiFlashClient

		geminiProClient = NewCodeAssistClient(ga.TokenSource, ga.ProjectID, "gemini-2.5-pro", true, true)
		CurrentProviders["gemini-2.5-pro"] = geminiProClient

		geminiFlashLiteClient = NewCodeAssistClient(ga.TokenSource, ga.ProjectID, "gemini-2.5-flash-lite", false, true)
		CurrentProviders["gemini-2.5-flash-lite"] = geminiFlashLiteClient

		geminiFlashImageClient = NewCodeAssistClient(ga.TokenSource, ga.ProjectID, "gemini-2.5-flash-image-preview", false, false)
		CurrentProviders["gemini-2.5-flash-image-preview"] = geminiFlashImageClient
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

		// Re-initialize CurrentProviders after successful authentication and user info fetch
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
		CurrentProviders = make(map[string]LLMProvider) // Clear all providers on logout

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

// initWithGoogle handles Google OAuth authentication flow including onboarding
func (ga *GeminiAuth) initWithGoogle(ctx context.Context, clientProvider HTTPClientProvider) error {
	// Create a temporary client to get ProjectID
	tempCaClient := NewCodeAssistClient(clientProvider, "", "", false, false)
	loadReq := LoadCodeAssistRequest{
		CloudaicompanionProject: ga.ProjectID, // Will be empty initially
		Metadata: &ClientMetadata{
			IdeType:     "IDE_UNSPECIFIED",
			Platform:    "PLATFORM_UNSPECIFIED",
			PluginType:  "GEMINI",
			DuetProject: ga.ProjectID,
		},
	}

	loadRes, loadErr := tempCaClient.LoadCodeAssist(ctx, loadReq)
	if loadErr != nil {
		log.Printf("initWithGoogle: LoadCodeAssist failed: %v", loadErr)
		return loadErr
	}

	// If currentTier exists, user is already onboarded
	if loadRes.CurrentTier != nil {
		log.Printf("initWithGoogle: User already onboarded with tier %s", loadRes.CurrentTier.ID)

		if loadRes.CloudaicompanionProject != "" {
			ga.ProjectID = loadRes.CloudaicompanionProject
		} else if ga.ProjectID != "" {
			// Use existing project ID
		} else {
			return fmt.Errorf("no project ID available for onboarded user")
		}

		// Set freeTierDataCollectionOptin to false for free tier users
		if loadRes.CurrentTier.ID == UserTierIDFree {
			if err := ga.setFreeTierDataCollectionOptin(ctx, tempCaClient, ga.ProjectID); err != nil {
				log.Printf("initWithGoogle: Failed to set freeTierDataCollectionOptin: %v", err)
				// Don't fail the entire process for this
			}
		}

		return nil
	}

	// User needs onboarding - proceed with onboarding flow
	return ga.performOnboarding(ctx, tempCaClient, loadRes)
}

// performOnboarding handles the onboarding process for new users
func (ga *GeminiAuth) performOnboarding(ctx context.Context, tempCaClient *CodeAssistClient, loadRes *LoadCodeAssistResponse) error {
	// Determine user tier for onboarding
	userTierID := ga.determineUserTier(loadRes)

	onboardReq := OnboardUserRequest{
		TierID: &userTierID,
		Metadata: &ClientMetadata{
			IdeType:    "IDE_UNSPECIFIED",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}
	if userTierID != UserTierIDFree {
		// The free tier uses a managed google cloud project.
		// Setting a project in the `onboardUser` request causes a `Precondition Failed` error.
		onboardReq.CloudaicompanionProject = ga.ProjectID
		onboardReq.Metadata.DuetProject = ga.ProjectID
	}

	// Perform onboarding with LRO polling
	lroRes, err := ga.performOnboardUserWithPolling(ctx, tempCaClient, onboardReq)
	if err != nil {
		log.Printf("performOnboarding: OnboardUser failed: %v", err)
		return err
	}

	if lroRes.Response != nil && lroRes.Response.CloudaicompanionProject != nil && lroRes.Response.CloudaicompanionProject.ID != "" {
		ga.ProjectID = lroRes.Response.CloudaicompanionProject.ID
		log.Printf("performOnboarding: Successfully onboarded with project ID: %s", ga.ProjectID)

		// Set freeTierDataCollectionOptin to false for free tier users
		if userTierID == UserTierIDFree {
			if err := ga.setFreeTierDataCollectionOptin(ctx, tempCaClient, ga.ProjectID); err != nil {
				log.Printf("performOnboarding: Failed to set freeTierDataCollectionOptin: %v", err)
				// Don't fail the entire process for this
			}
		}

		return nil
	} else {
		return fmt.Errorf("onboardUser succeeded but returned empty or invalid project ID - user may be ineligible for service")
	}
}

// determineUserTier determines the user tier from LoadCodeAssist response
func (ga *GeminiAuth) determineUserTier(loadRes *LoadCodeAssistResponse) UserTierID {
	for _, tier := range loadRes.AllowedTiers {
		if tier.IsDefault != nil && *tier.IsDefault {
			return tier.ID
		}
	}
	return UserTierIDLegacy
}

// performOnboardUserWithPolling handles the LRO polling for onboardUser
func (ga *GeminiAuth) performOnboardUserWithPolling(ctx context.Context, tempCaClient *CodeAssistClient, onboardReq OnboardUserRequest) (*LongRunningOperationResponse, error) {
	lroRes, err := tempCaClient.OnboardUser(ctx, onboardReq)
	if err != nil {
		return nil, fmt.Errorf("initial onboardUser call failed: %w", err)
	}

	// Poll until LRO is complete
	for lroRes.Done == nil || !*lroRes.Done {
		log.Printf("performOnboardUserWithPolling: OnboardUser in progress, waiting 5 seconds...")

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			// Continue polling
		}

		lroRes, err = tempCaClient.OnboardUser(ctx, onboardReq)
		if err != nil {
			return nil, fmt.Errorf("polling onboardUser failed: %w", err)
		}
	}

	log.Printf("performOnboardUserWithPolling: OnboardUser completed successfully")
	return lroRes, nil
}

// setFreeTierDataCollectionOptin sets freeTierDataCollectionOptin to false for free tier users
func (ga *GeminiAuth) setFreeTierDataCollectionOptin(ctx context.Context, tempCaClient *CodeAssistClient, projectID string) error {
	settingReq := SetCodeAssistGlobalUserSettingRequest{
		CloudaicompanionProject:     projectID,
		FreeTierDataCollectionOptin: false,
	}

	_, err := tempCaClient.SetCodeAssistGlobalUserSetting(ctx, settingReq)
	if err != nil {
		return fmt.Errorf("failed to set freeTierDataCollectionOptin to false: %w", err)
	}

	log.Printf("setFreeTierDataCollectionOptin: Successfully set freeTierDataCollectionOptin to false for project %s", projectID)
	return nil
}

// Validate performs common auth and project validation for handlers
func (ga *GeminiAuth) Validate(handlerName string, w http.ResponseWriter, r *http.Request) bool {
	ga.initMutex.Lock()
	defer ga.initMutex.Unlock()

	// Detailed logging at the start of ValidateAuthAndProject
	// Check if any provider is initialized and attempt to re-initialize if needed
	if len(CurrentProviders) == 0 {
		log.Printf("%s: No GeminiClient initialized, attempting to re-initialize...", handlerName)
		if ga.SelectedAuthType == AuthTypeLoginWithGoogle && ga.Token != nil {
			// Attempt to re-initialize, which includes token refresh if needed
			ga.InitCurrentProvider()
			log.Printf("%s: CurrentProviders state after InitCurrentProvider: %t, ProjectID: %s", handlerName, len(CurrentProviders) == 0, ga.ProjectID)
		}
		// After attempting re-initialization, check CurrentProviders again
		if len(CurrentProviders) == 0 {
			log.Printf("%s: GeminiClient still not initialized after re-attempt.", handlerName)
			// If CurrentProviders is still empty, it means token refresh failed or no token exists.
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
