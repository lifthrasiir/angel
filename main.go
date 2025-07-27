package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func init() {
	// Select authentication method from environment variables
	if os.Getenv("GEMINI_API_KEY") != "" {
		SelectedAuthType = AuthTypeUseGemini
		log.Println("Authentication method: Using Gemini API Key")
	} else if os.Getenv("GOOGLE_API_KEY") != "" || (os.Getenv("GOOGLE_CLOUD_PROJECT") != "" && os.Getenv("GOOGLE_CLOUD_LOCATION") != "") {
		SelectedAuthType = AuthTypeUseVertexAI
		log.Println("Authentication method: Using Vertex AI")
	} else if os.Getenv("CLOUD_SHELL") == "true" { // Check Cloud Shell environment variable
		SelectedAuthType = AuthTypeCloudShell
		log.Println("Authentication method: Using Cloud Shell")
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
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST") // Add countTokens handler

	// Serve frontend static files
	frontendPath := filepath.Join(".", "frontend", "dist")
	router.PathPrefix("/").Handler(http.FileServer(http.Dir(frontendPath)))

	fmt.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	url := GoogleOauthConfig.AuthCodeURL(OAuthState)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("state") != OAuthState {
		log.Printf("Invalid OAuth state: %s", r.FormValue("state"))
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	code := r.FormValue("code")
	var err error
	Token, err = GoogleOauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Failed to exchange token: %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// Retrieve Project ID using the token
	if err := FetchProjectID(Token); err != nil {
		log.Printf("Failed to retrieve Project ID: %v", err)
		http.Error(w, "Could not retrieve Project ID.", http.StatusInternalServerError)
		return
	}

	// Save token to file
	SaveToken(Token)

	// Initialize GenAI service
	InitGeminiClient()

	// Redirect to frontend root after successful authentication
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
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

	sessionID := generateSessionID() // Generate session ID

	cs := &ChatSession{}      // Create a new ChatSession instance
	cs.History = []*Content{} // Explicitly initialize chat history
	ChatSessions[sessionID] = cs

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sessionID, "message": "New chat session started."})
}

// Chat message handler
func chatMessage(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		SessionID string `json:"sessionId"`
		Message   string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sessionId := requestBody.SessionID
	userMessage := requestBody.Message

	// Retrieve session history
	cs, ok := ChatSessions[sessionId]
	if !ok {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Add user message to current chat history
	cs.History = append(cs.History, &Content{
		Parts: []Part{{Text: userMessage}},
		Role:  "user",
	})

	var historyContents []Content
	for _, h := range cs.History {
		historyContents = append(historyContents, *h)
	}

	modelName := "gemini-2.5-flash"

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	respBody, err := GeminiClient.SendMessageStream(context.Background(), historyContents, modelName)
	if err != nil {
		log.Printf("CodeAssist API call failed: %v", err)
		http.Error(w, fmt.Sprintf("CodeAssist API call failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer respBody.Close()

	var agentResponseText string
	if err := processStreamingJsonResponse(respBody, w, flusher, &agentResponseText); err != nil {
		log.Printf("Error processing streaming response: %v", err)
	}

	cs.History = append(cs.History, &Content{
		Parts: []Part{{Text: agentResponseText}},
		Role:  "model",
	})

	/*
		finishedEvent := ServerGeminiFinishedEvent{
			Type: GeminiTypeFinished,
		}
		eventBytes, _ := json.Marshal(finishedEvent)
		fmt.Fprintf(w, "data: %s\n\n", eventBytes)
		flusher.Flush()
	*/
}

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
					for line := range strings.Lines(part.Text) {
						fmt.Fprintf(w, "data: %s\n", line)
					}
					fmt.Fprintf(w, "\n")
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
	return fmt.Sprintf("session-%d", len(ChatSessions)+1)
}
