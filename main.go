package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var oauthStates = make(map[string]string) // Stores randomState -> originalQueryString

func init() {
	InitDB()
	InitAuth()
}

func main() {
	router := mux.NewRouter()

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if SelectedAuthType == AuthTypeLoginWithGoogle {
		router.HandleFunc("/login", HandleGoogleLogin).Methods("GET")
		router.HandleFunc("/oauth2callback", HandleGoogleCallback).Methods("GET")
	}

	// API handlers
	router.HandleFunc("/new", newChatSession).Methods("GET")
	router.HandleFunc("/api/chat/message", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/load", loadChatSession).Methods("GET")       // New endpoint to load chat session
	router.HandleFunc("/api/chat/sessions", listChatSessions).Methods("GET")  // New endpoint to list all chat sessions
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

	sessionId := GenerateSessionID() // Generate session ID
	if err := CreateSession(sessionId); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create new session: %v", err), http.StatusInternalServerError)
		return
	}

	// Update last_updated_at for the new session
	if err := UpdateSessionLastUpdated(sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for new session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	// Redirect to the new session URL
	http.Redirect(w, r, fmt.Sprintf("/%s", sessionId), http.StatusSeeOther)
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

	// Update last_updated_at for the current session
	if err := UpdateSessionLastUpdated(sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	// Retrieve session history from DB for Gemini API
	historyContents, err := GetSessionHistoryForGeminiAPI(sessionId)
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
	if err := processStreamingJsonResponse(respBody, w, flusher, &agentResponseText, sessionId); err != nil {
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

	// Check if session exists
	exists, err := SessionExists(sessionId)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check session existence: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	history, err := GetSessionHistoryForWebAPI(sessionId) // Use the new function for web API
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

// New endpoint to list all chat sessions
func listChatSessions(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}

	sessions, err := GetAllSessions()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve sessions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func sendServerEvent(w http.ResponseWriter, flusher http.Flusher, data string) {
	escapedData := strings.ReplaceAll(data, "\n", "\ndata: ")
	fmt.Fprintf(w, "data: %s\n\n", escapedData)
	flusher.Flush()
}

// NOTE: This function is intentionally designed to parse a specific JSON stream format, not standard SSE. Do not modify without understanding its purpose.
func processStreamingJsonResponse(r io.Reader, w http.ResponseWriter, flusher http.Flusher, agentResponseText *string, sessionId string) (err error) {
	dec := json.NewDecoder(r)

	// Read the opening bracket of the JSON array
	_, err = dec.Token()
	if err != nil {
		return fmt.Errorf("expected opening bracket '[', but got %w", err)
	}

	for dec.More() {
		var caResp CaGenerateContentResponse
		if err := dec.Decode(&caResp); err != nil {
			log.Printf("Failed to decode JSON object from stream: %v", err)
			continue
		}

		if len(caResp.Response.Candidates) > 0 && len(caResp.Response.Candidates[0].Content.Parts) > 0 {
			for _, part := range caResp.Response.Candidates[0].Content.Parts {
				// Check if it's a thought part
				if part.Thought { // If it's a thought
					// Parse subject and description from part.Text
					rawText := part.Text
					subject := ""
					description := rawText

					// Use regexp to extract subject and description
					re := regexp.MustCompile(`^\*\*(.*?)\*\*\s*(.*)`)
					matches := re.FindStringSubmatch(rawText)
					if len(matches) > 2 {
						subject = matches[1]
						description = matches[2]
					}

					sendServerEvent(w, flusher, fmt.Sprintf("T\n%s\n%s", subject, description))
					// Add thought message to chat history in DB
					if err := AddMessageToSession(sessionId, "thought", fmt.Sprintf("%s\n%s", subject, description)); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue // Skip further processing for thought parts
				}
				if part.Text != "" {
					sendServerEvent(w, flusher, fmt.Sprintf("M\n%s", part.Text))
					*agentResponseText += part.Text
				}
			}
		}
	}

	// Read the closing bracket of the JSON array
	_, err = dec.Token()
	if err != nil {
		return fmt.Errorf("expected closing bracket ']', but got %w", err)
	}

	// Send 'Q' to signal end of content
	sendServerEvent(w, flusher, "Q")

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

func GenerateSessionID() string {
	b := make([]byte, 8) // 8 bytes will result in an 11-character base64 string
	if _, err := rand.Read(b); err != nil {
		log.Printf("Error generating random session ID: %v", err)
		// Fallback to UUID or handle error appropriately
		return uuid.New().String() // Fallback to UUID if random generation fails
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
