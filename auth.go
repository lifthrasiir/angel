package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func InitAuth() {
	// Select authentication method from environment variables
	if os.Getenv("GEMINI_API_KEY") != "" {
		SelectedAuthType = AuthTypeUseGemini
		log.Println("Authentication method: Using Gemini API Key")
	} else if os.Getenv("GOOGLE_API_KEY") != "" || (os.Getenv("GOOGLE_CLOUD_PROJECT") != "" && os.Getenv("GOOGLE_CLOUD_LOCATION") != "") {
		SelectedAuthType = AuthTypeUseVertexAI
		log.Println("Authentication method: Using Vertex AI")
	} else {
		SelectedAuthType = AuthTypeLoginWithGoogle
		log.Println("Authentication method: Using Google Login (OAuth)")
	}

	// Branch initialization logic based on authentication method
	switch SelectedAuthType {
	case AuthTypeLoginWithGoogle:
		// THIS IS INTENTIONALLY HARD-CODED TO MATCH GEMINI-CLI!
		GoogleOauthConfig = &oauth2.Config{
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
		LoadToken()

		// Initialize GenAI service if token is loaded
		if Token != nil && Token.Valid() {
			InitGeminiClient()
		}
	case AuthTypeUseGemini:
		InitGeminiClient() // GEMINI_API_KEY is handled inside InitGeminiClient
	case AuthTypeUseVertexAI:
		InitGeminiClient() // GOOGLE_API_KEY or GCP environment variables are handled inside InitGeminiClient
	case AuthTypeCloudShell:
		InitGeminiClient() // Cloud Shell authentication is handled inside InitGeminiClient
	}
}

func HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
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
	oauthStates[randomState] = redirectToQueryString // Store the original query string with the random state

	url := GoogleOauthConfig.AuthCodeURL(randomState)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateParam := r.FormValue("state")

	// Validate the random part of the state against the stored value
	originalQueryString, ok := oauthStates[stateParam]
	if !ok {
		log.Printf("Invalid or expired OAuth state: %s", stateParam)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	// Remove the state after use to prevent replay attacks
	delete(oauthStates, stateParam)

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
	Token, err := GoogleOauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Failed to exchange token: %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// Retrieve Project ID using the token
	if err = FetchProjectID(Token); err != nil {
		log.Printf("Failed to retrieve Project ID: %v", err)
		http.Error(w, "Could not retrieve Project ID.", http.StatusInternalServerError)
		return
	}

	// Save token to file
	SaveToken(Token)

	// Initialize GenAI service
	InitGeminiClient()

	// Redirect to the original path after successful authentication
	http.Redirect(w, r, finalRedirectURL, http.StatusTemporaryRedirect)
}
