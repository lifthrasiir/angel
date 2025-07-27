package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var oauthStates = make(map[string]string) // Stores randomState -> originalQueryString

func init() {
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

	InitDB() // Initialize SQLite database
}

func main() {
	router := mux.NewRouter()

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if SelectedAuthType == AuthTypeLoginWithGoogle {
		router.HandleFunc("/login", handleGoogleLogin).Methods("GET")
		router.HandleFunc("/oauth2callback", handleGoogleCallback).Methods("GET")
	}

	// API handlers
	router.HandleFunc("/api/chat/new", newChatSession).Methods("POST")
	router.HandleFunc("/api/chat/message", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/load", loadChatSession).Methods("GET")       // New endpoint to load chat session
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST") // Add countTokens handler

	// Serve frontend static files
	frontendPath := filepath.Join(".", "frontend", "dist")
	// Serve static files and fallback to index.html for client-side routing
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the requested file
		fs := http.FileServer(http.Dir(frontendPath))
		// Check if the file exists
		if _, err := os.Stat(filepath.Join(frontendPath, r.URL.Path)); os.IsNotExist(err) {
			// If not, serve index.html for client-side routing
			http.ServeFile(w, r, filepath.Join(frontendPath, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	}))

	fmt.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
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

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
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

// New chat session start handler
func newChatSession(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Skip ProjectID validation if not OAuth method.
	if SelectedAuthType == AuthTypeLoginWithGoogle && ProjectID == "" {
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return
	}

	sessionId := generateSessionID() // Generate session ID
	if err := CreateSession(sessionId); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create new session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sessionId, "message": "New chat session started."})
}

// Chat message handler
func chatMessage(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		log.Println("chatMessage: GeminiClient not initialized.")
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Add ProjectID validation for OAuth method
	if SelectedAuthType == AuthTypeLoginWithGoogle && ProjectID == "" {
		log.Println("chatMessage: Project ID is not set. Please log in again.")
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		SessionID string `json:"sessionId"`
		Message   string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		log.Printf("chatMessage: Invalid request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sessionId := requestBody.SessionID
	userMessage := requestBody.Message

	// Add user message to current chat history in DB
	if err := AddMessageToSession(sessionId, "user", userMessage); err != nil {
		log.Printf("chatMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve session history from DB
	historyContents, err := GetSessionHistory(sessionId)
	if err != nil {
		log.Printf("chatMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	modelName := "gemini-2.5-flash"

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("chatMessage: Streaming unsupported!")
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	respBody, err := GeminiClient.SendMessageStream(context.Background(), historyContents, modelName)
	if err != nil {
		log.Printf("chatMessage: CodeAssist API call failed: %v", err)
		http.Error(w, fmt.Sprintf("CodeAssist API call failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer respBody.Close()

	var agentResponseText string
	if err := processStreamingJsonResponse(respBody, w, flusher, &agentResponseText); err != nil {
		log.Printf("chatMessage: Error processing streaming response: %v", err)
		return
	}
	// Add agent response to chat history in DB
	if err := AddMessageToSession(sessionId, "model", agentResponseText); err != nil {
		log.Printf("chatMessage: Failed to save agent response: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save agent response: %v", err), http.StatusInternalServerError)
		return
	}
}

// New endpoint to load chat session history
func loadChatSession(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Add ProjectID validation for OAuth method
	if SelectedAuthType == AuthTypeLoginWithGoogle && ProjectID == "" {
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return
	}

	sessionId := r.URL.Query().Get("sessionId")
	if sessionId == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	history, err := GetSessionHistory(sessionId)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session history: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Ensure history is an empty slice if no messages are found, not nil
	if history == nil {
		history = []Content{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessionId": sessionId, "history": history})
}

// NOTE: This function is intentionally designed to parse a specific JSON stream format, not standard SSE. Do not modify without understanding its purpose.
func processStreamingJsonResponse(r io.Reader, w http.ResponseWriter, flusher http.Flusher, agentResponseText *string) (err error) {
	b := make([]byte, 1)
	n, err := r.Read(b)
	if n != 1 {
		err = fmt.Errorf("expected opening bracket '[', but got a premature EOF")
		return
	}
	if b[0] != '[' {
		err = fmt.Errorf("expected opening bracket '[', but got %v", b[0])
		return
	}

	scanner := bufio.NewScanner(r)
	var lines strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			line = line[6:]
		}
		lines.WriteString(line)
		if line != "}" {
			continue
		}

		var caResp CaGenerateContentResponse
		if err := json.Unmarshal([]byte(lines.String()), &caResp); err != nil {
			log.Printf("Failed to decode JSON object from stream: %v", err)
			continue
		}
		lines.Reset()

		if len(caResp.Response.Candidates) > 0 && len(caResp.Response.Candidates[0].Content.Parts) > 0 {
			for _, part := range caResp.Response.Candidates[0].Content.Parts {
				// Check if it's a thought part
				if part.Thought != "" { // If it's a thought, skip it
					log.Printf("Thinking: %v", part.Thought)
					continue
				}
				if part.Text != "" {
					/*
						contentEvent := ServerGeminiContentEvent{
							Type:  GeminiEventTypeContent,
							Value: Content{Role: "model", Parts: []Part{{Text: part.Text}}},
						}
						eventBytes, _ := json.Marshal(contentEvent)
					*/
					fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(part.Text, "\n", "\ndata: "))
					*agentResponseText += part.Text
					flusher.Flush()
				}
			}
		}

		if !scanner.Scan() {
			break
		}

		line = scanner.Text()
		if line == "," {
			continue
		} else if line == "]" {
			break
		} else {
			log.Printf("Unexpected streaming delimiter %v", line)
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error reading from response body: %v", err)
	}

	return
}

// Add countTokens handler
func countTokensHandler(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		Text string `json:"text"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	modelName := "gemini-2.5-flash"

	contents := []Content{
		{
			Role:  "user",
			Parts: []Part{{Text: requestBody.Text}},
		},
	}

	resp, err := GeminiClient.CountTokens(context.Background(), contents, modelName)
	if err != nil {
		log.Printf("CountTokens API call failed: %v", err)
		http.Error(w, fmt.Sprintf("CountTokens API call failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"totalTokens": resp.TotalTokens})
}

func generateSessionID() string {
	return uuid.New().String()
}
