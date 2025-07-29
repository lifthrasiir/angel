package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var (
	oauthStates       = make(map[string]string) // Stores randomState -> originalQueryString
	GlobalGeminiState GeminiState
)

func init() {
	InitDB()
	GlobalGeminiState.OAuthState = "random"
	GlobalGeminiState.LoadToken()
	InitAuth(&GlobalGeminiState)
	GlobalGeminiState.InitGeminiClient()
}

func main() {
	router := mux.NewRouter()

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle {
		router.HandleFunc("/login", makeAuthHandler(&GlobalGeminiState, HandleGoogleLogin)).Methods("GET")
		router.HandleFunc("/oauth2callback", makeAuthHandler(&GlobalGeminiState, HandleGoogleCallback)).Methods("GET")
	}

	// API handlers
	router.HandleFunc("/new", newChatSession).Methods("GET")
	router.HandleFunc("/api/chat/message", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/load", loadChatSession).Methods("GET")                        // New endpoint to load chat session
	router.HandleFunc("/api/chat/sessions", listChatSessions).Methods("GET")                   // New endpoint to list all chat sessions
	router.HandleFunc("/api/chat/countTokens", countTokensHandler).Methods("POST")             // Add countTokens handler
	router.HandleFunc("/api/chat/newSessionAndMessage", newSessionAndMessage).Methods("POST")  // New endpoint to create a new session and send the first message
	router.HandleFunc("/api/default-system-prompt", getDefaultSystemPrompt).Methods("GET")     // New endpoint to get the default system prompt
	router.HandleFunc("/api/chat/updateSessionName", updateSessionNameHandler).Methods("POST") // New endpoint to update session name

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

func makeAuthHandler(gs *GeminiState, handler func(gs *GeminiState, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(gs, w, r)
	}
}

// New chat session start handler
func newChatSession(w http.ResponseWriter, r *http.Request) {
	frontendPath := filepath.Join(".", "frontend", "dist")
	http.ServeFile(w, r, filepath.Join(frontendPath, "index.html"))
}

var thoughtPattern = regexp.MustCompile(`^\*\*(.*?)\*\*\s*(.*)`) // Corrected: `^\*\*(.*?)\*\*\s*(.*)`

// New session and message handler
func newSessionAndMessage(w http.ResponseWriter, r *http.Request) {
	if GlobalGeminiState.GeminiClient == nil {
		log.Println("newSessionAndMessage: GeminiClient not initialized.")
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Add ProjectID validation for OAuth method
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.ProjectID == "" {
		log.Println("newSessionAndMessage: Project ID is not set. Please log in again.")
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		Message      string           `json:"message"`
		SystemPrompt string           `json:"systemPrompt"`
		Attachments  []FileAttachment `json:"attachments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		log.Printf("newSessionAndMessage: Invalid request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sessionId := GenerateSessionID() // Generate new session ID

	// Use provided system prompt or default
	systemPrompt := requestBody.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = GetDefaultSystemPrompt()
	}

	if err := CreateSession(sessionId, systemPrompt); err != nil {
		log.Printf("newSessionAndMessage: Failed to create new session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create new session: %v", err), http.StatusInternalServerError)
		return
	}

	userMessage := requestBody.Message

	// Add user message to current chat history in DB
	if err := AddMessageToSession(sessionId, "user", userMessage, "text", requestBody.Attachments); err != nil {
		log.Printf("newSessionAndMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Update last_updated_at for the new session
	if err := UpdateSessionLastUpdated(sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for new session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("newSessionAndMessage: Streaming unsupported!")
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Send the new session ID as the first event
	sendServerEvent(w, flusher, fmt.Sprintf("S\n%s", sessionId))

	// Retrieve session history from DB for Gemini API
	frontendHistory, err := GetSessionHistory(sessionId, true)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert FrontendMessage to Content for Gemini API
	historyContents := convertFrontendMessagesToContent(frontendHistory)

	// Goroutine to infer session name
	go func(sID string, uMsg string, sPrompt string, initialAgentResponse string) {
		// Check if session name is already set (user might have set it manually)
		session, err := GetSession(sID)
		if err != nil {
			log.Printf("Failed to get session %s for name inference: %v", sID, err)
			return
		}
		if session.Name != "" { // If name is not empty, user has set it, do not infer
			return
		}

		// Construct the prompt for name inference
		nameSystemPrompt, nameInputPrompt := GetSessionNameInferencePrompts(uMsg, initialAgentResponse)

		// Call LLM to infer name
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // 10 second timeout
		defer cancel()

		inferredName, err := GlobalGeminiState.GeminiClient.GenerateContentOneShot(ctx, []Content{{
			Role:  "user",
			Parts: []Part{{Text: nameInputPrompt}},
		}}, DefaultGeminiModel, nameSystemPrompt, &ThinkingConfig{IncludeThoughts: false})
		if err != nil {
			log.Printf("Failed to infer session name for %s: %v", sID, err)
			return
		}

		// Validate inferred name
		inferredName = strings.TrimSpace(inferredName)
		if len(inferredName) > 100 || strings.Contains(inferredName, "\n") {
			log.Printf("Inferred name for session %s is invalid (too long or multi-line): %s", sID, inferredName)
			return
		}

		// Update session name in DB
		if err := UpdateSessionName(sID, inferredName); err != nil {
			log.Printf("Failed to update session name for %s: %v", sID, err)
			return
		}

		// Notify frontend about name update
		sendServerEvent(w, flusher, fmt.Sprintf("N\n%s\n%s", sID, inferredName))
	}(sessionId, userMessage, systemPrompt, "") // Pass empty string for initialAgentResponse, it will be filled by streamGeminiResponse

	// Handle streaming response from Gemini
	if err := streamGeminiResponse(w, r, sessionId, systemPrompt, historyContents); err != nil {
		log.Printf("newSessionAndMessage: Error streaming Gemini response: %v", err)
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
		return
	}
}

// Chat message handler
func chatMessage(w http.ResponseWriter, r *http.Request) {
	if GlobalGeminiState.GeminiClient == nil {
		log.Println("chatMessage: GeminiClient not initialized.")
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Add ProjectID validation for OAuth method
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.ProjectID == "" {
		log.Println("chatMessage: Project ID is not set. Please log in again.")
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		SessionID   string           `json:"sessionId"`
		Message     string           `json:"message"`
		Attachments []FileAttachment `json:"attachments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		log.Printf("chatMessage: Invalid request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sessionId := requestBody.SessionID

	session, err := GetSession(sessionId)
	if err != nil {
		log.Printf("chatMessage: Failed to load session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}
	systemPrompt := session.SystemPrompt

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	_, ok := w.(http.Flusher)
	if !ok {
		log.Println("chatMessage: Streaming unsupported!")
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	userMessage := requestBody.Message

	// Add user message to current chat history in DB
	if err := AddMessageToSession(sessionId, "user", userMessage, "text", requestBody.Attachments); err != nil {
		log.Printf("chatMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve session history from DB for Gemini API
	frontendHistory, err := GetSessionHistory(sessionId, true)
	if err != nil {
		log.Printf("chatMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert FrontendMessage to Content for Gemini API
	historyContents := convertFrontendMessagesToContent(frontendHistory)

	// Handle streaming response from Gemini
	if err := streamGeminiResponse(w, r, sessionId, systemPrompt, historyContents); err != nil {
		log.Printf("chatMessage: Error streaming Gemini response: %v", err)
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
		return
	}
}

// New endpoint to load chat session history
func loadChatSession(w http.ResponseWriter, r *http.Request) {
	if GlobalGeminiState.GeminiClient == nil {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Add ProjectID validation for OAuth method
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.ProjectID == "" {
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

	session, err := GetSession(sessionId)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}

	history, err := GetSessionHistory(sessionId, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session history: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Ensure history is an empty slice if no messages are found, not nil
	if history == nil {
		history = []FrontendMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessionId": sessionId, "history": history, "systemPrompt": session.SystemPrompt})
}

// New endpoint to list all chat sessions
func listChatSessions(w http.ResponseWriter, r *http.Request) {
	if GlobalGeminiState.GeminiClient == nil {
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

	// Add a recover block to catch panics during Flush()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic during Flush: %v", r)
		}
	}()

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// New endpoint to get the default system prompt
func getDefaultSystemPrompt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, GetDefaultSystemPrompt())
}

// Add countTokens handler

func countTokensHandler(w http.ResponseWriter, r *http.Request) {
	if GlobalGeminiState.GeminiClient == nil {
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

	modelName := DefaultGeminiModel

	contents := []Content{
		{
			Role:  "user",
			Parts: []Part{{Text: requestBody.Text}},
		},
	}

	resp, err := GlobalGeminiState.GeminiClient.CountTokens(context.Background(), contents, modelName)
	if err != nil {
		log.Printf("CountTokens API call failed: %v", err)
		http.Error(w, fmt.Sprintf("CountTokens API call failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"totalTokens": resp.TotalTokens})
}

func callFunction(fc FunctionCall) (map[string]interface{}, error) {
	return CallToolFunction(fc)
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

// Helper function to convert FrontendMessage to Content for Gemini API
func convertFrontendMessagesToContent(frontendMessages []FrontendMessage) []Content {
	var contents []Content
	for _, fm := range frontendMessages {
		var parts []Part
		// Add text part if present
		if fm.Parts != nil && len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			parts = append(parts, Part{Text: fm.Parts[0].Text})
		}

		// Add attachments as InlineData
		for _, att := range fm.Attachments {
			parts = append(parts, Part{
				InlineData: &InlineData{
					MimeType: att.MimeType,
					Data:     att.Data,
				},
			})
		}

		// Handle function calls and responses (these should override text/attachments for their specific message types)
		if fm.Type == "function_call" && fm.Parts != nil && len(fm.Parts) > 0 && fm.Parts[0].FunctionCall != nil {
			parts = []Part{{FunctionCall: fm.Parts[0].FunctionCall}}
		} else if fm.Type == "function_response" && fm.Parts != nil && len(fm.Parts) > 0 && fm.Parts[0].FunctionResponse != nil {
			parts = []Part{{FunctionResponse: fm.Parts[0].FunctionResponse}}
		}

		contents = append(contents, Content{
			Role:  fm.Role,
			Parts: parts,
		})
	}
	return contents
}

// New handler to update session name
func updateSessionNameHandler(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		SessionID string `json:"sessionId"`
		Name      string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := UpdateSessionName(requestBody.SessionID, requestBody.Name); err != nil {
		log.Printf("Failed to update session name for %s: %v", requestBody.SessionID, err)
		http.Error(w, fmt.Sprintf("Failed to update session name: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Session name updated successfully")
}

// Helper function to stream Gemini API response
func streamGeminiResponse(w http.ResponseWriter, r *http.Request, sessionId string, systemPrompt string, initialHistory []Content) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported")
	}

	var agentResponseText string
	var currentHistory = initialHistory

	for {
		seq, closer, err := GlobalGeminiState.GeminiClient.SendMessageStream(context.Background(), currentHistory, DefaultGeminiModel, systemPrompt, &ThinkingConfig{IncludeThoughts: true})
		if err != nil {
			return fmt.Errorf("CodeAssist API call failed: %w", err)
		}
		defer closer.Close()

		var functionCalls []FunctionCall
		var modelResponseParts []Part

		for caResp := range seq {
			if len(caResp.Response.Candidates) == 0 {
				continue
			}
			if len(caResp.Response.Candidates[0].Content.Parts) == 0 {
				continue
			}
			for _, part := range caResp.Response.Candidates[0].Content.Parts {
				if part.FunctionCall != nil {
					functionCalls = append(functionCalls, *part.FunctionCall)
					argsJson, _ := json.Marshal(part.FunctionCall.Args)
					sendServerEvent(w, flusher, fmt.Sprintf("F\n%s\n%s", part.FunctionCall.Name, string(argsJson)))
					continue
				}

				if part.Thought {
					var thoughtText string
					matches := thoughtPattern.FindStringSubmatch(part.Text)
					if len(matches) > 2 {
						thoughtText = fmt.Sprintf("%s\n%s", matches[1], matches[2])
					} else {
						thoughtText = fmt.Sprintf("Thinking...\n%s", part.Text)
					}

					sendServerEvent(w, flusher, fmt.Sprintf("T\n%s", thoughtText))
					if err := AddMessageToSession(sessionId, "thought", thoughtText, "thought", nil); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue
				}

				if part.Text != "" {
					sendServerEvent(w, flusher, fmt.Sprintf("M\n%s", part.Text))
					agentResponseText += part.Text
					modelResponseParts = append(modelResponseParts, part)
				}
			}
			if len(functionCalls) > 0 {
				break
			}
		}

		if len(functionCalls) > 0 {
			for _, fc := range functionCalls {
				functionResponseValue, err := callFunction(fc)
				if err != nil {
					log.Printf("Error executing function %s: %v", fc.Name, err)
					functionResponseValue = map[string]interface{}{"error": err.Error()}
				}

				responseJson, err := json.Marshal(functionResponseValue)
				if err != nil {
					log.Printf("Failed to marshal function response for frontend: %v", err)
					responseJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
				}
				sendServerEvent(w, flusher, fmt.Sprintf("R\n%s", string(responseJson)))

				fcJson, _ := json.Marshal(fc)
				if err := AddMessageToSession(sessionId, "model", string(fcJson), "function_call", nil); err != nil {
					log.Printf("Failed to save function call: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "model", Parts: []Part{{FunctionCall: &fc}}})

				fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
				frJson, _ := json.Marshal(fr)
				if err := AddMessageToSession(sessionId, "user", string(frJson), "function_response", nil); err != nil {
					log.Printf("Failed to save function response: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "user", Parts: []Part{{FunctionResponse: &fr}}})
			}
		} else {
			break
		}
	}

	sendServerEvent(w, flusher, "Q")

	if err := AddMessageToSession(sessionId, "model", agentResponseText, "text", nil); err != nil {
		return fmt.Errorf("failed to save agent response: %w", err)
	}
	return nil
}
