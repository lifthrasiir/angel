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
	router.HandleFunc("/api/chat/load", loadChatSession).Methods("GET")                       // New endpoint to load chat session
	router.HandleFunc("/api/chat/sessions", listChatSessions).Methods("GET")                  // New endpoint to list all chat sessions
	router.HandleFunc("/api/chat/countTokens", countTokensHandler).Methods("POST")            // Add countTokens handler
	router.HandleFunc("/api/chat/newSessionAndMessage", newSessionAndMessage).Methods("POST") // New endpoint to create a new session and send the first message
	router.HandleFunc("/api/default-system-prompt", getDefaultSystemPrompt).Methods("GET")    // New endpoint to get the default system prompt

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
	frontendPath := filepath.Join(".", "frontend", "dist")
	http.ServeFile(w, r, filepath.Join(frontendPath, "index.html"))
}

var thoughtPattern = regexp.MustCompile(`^\*\*(.*?)\*\*\s*(.*)`) // Corrected: `^\*\*(.*?)\*\*\s*(.*)`

// New session and message handler
func newSessionAndMessage(w http.ResponseWriter, r *http.Request) {
	if GeminiClient == nil {
		log.Println("newSessionAndMessage: GeminiClient not initialized.")
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}
	// Add ProjectID validation for OAuth method
	if SelectedAuthType == AuthTypeLoginWithGoogle && ProjectID == "" {
		log.Println("newSessionAndMessage: Project ID is not set. Please log in again.")
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		Message      string `json:"message"`
		SystemPrompt string `json:"systemPrompt"`
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
	if err := AddMessageToSession(sessionId, "user", userMessage, "text"); err != nil {
		log.Printf("newSessionAndMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Update last_updated_at for the new session
	if err := UpdateSessionLastUpdated(sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for new session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	modelName := "gemini-2.5-flash"

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
	historyContents, err := GetSessionHistory(sessionId, true)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	var agentResponseText string
	var currentHistory = historyContents

	for {
		seq, closer, err := GeminiClient.SendMessageStream(context.Background(), currentHistory, modelName, systemPrompt)
		if err != nil {
			log.Printf("newSessionAndMessage: CodeAssist API call failed: %v", err)
			http.Error(w, fmt.Sprintf("CodeAssist API call failed: %v", err), http.StatusInternalServerError)
			return
		}
		defer closer.Close()

		var functionCalls []FunctionCall // Changed to slice
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
					functionCalls = append(functionCalls, *part.FunctionCall) // Append to slice
					// Send FunctionCall to frontend
					argsJson, _ := json.Marshal(part.FunctionCall.Args)
					sendServerEvent(w, flusher, fmt.Sprintf("F\n%s\n%s", part.FunctionCall.Name, string(argsJson)))
					continue
				}

				// Check if it's a thought part
				if part.Thought { // If it's a thought
					// Parse subject and description from part.Text
					var thoughtText string
					matches := thoughtPattern.FindStringSubmatch(part.Text)
					if len(matches) > 2 {
						thoughtText = fmt.Sprintf("%s\n%s", matches[1], matches[2])
					} else {
						thoughtText = fmt.Sprintf("Thinking...\n%s", part.Text) // Placeholder subject
					}

					sendServerEvent(w, flusher, fmt.Sprintf("T\n%s", thoughtText))
					// Add thought message to chat history in DB
					if err := AddMessageToSession(sessionId, "thought", thoughtText, "thought"); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue // Skip further processing for thought parts
				}

				if part.Text != "" {
					sendServerEvent(w, flusher, fmt.Sprintf("M\n%s", part.Text))
					agentResponseText += part.Text
					modelResponseParts = append(modelResponseParts, part)
				}
			}
			// If any function calls were found in this caResp, break the seq loop
			if len(functionCalls) > 0 {
				break
			}
		}

		if len(functionCalls) > 0 {
			// Process all collected function calls
			for _, fc := range functionCalls {
				functionResponseValue, err := callFunction(fc)
				if err != nil {
					log.Printf("Error executing function %s: %v", fc.Name, err)
					functionResponseValue = map[string]interface{}{"error": err.Error()}
				}

				// Send FunctionResponse to frontend
				responseJson, err := json.Marshal(functionResponseValue)
				if err != nil {
					log.Printf("Failed to marshal function response for frontend: %v", err)
					responseJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
				}
				sendServerEvent(w, flusher, fmt.Sprintf("R\n%s", string(responseJson)))

				// Add FunctionCall to history
				fcJson, _ := json.Marshal(fc)
				if err := AddMessageToSession(sessionId, "model", string(fcJson), "function_call"); err != nil {
					log.Printf("Failed to save function call: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "model", Parts: []Part{{FunctionCall: &fc}}})

				// Add FunctionResponse to history
				fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
				frJson, _ := json.Marshal(fr)
				if err := AddMessageToSession(sessionId, "user", string(frJson), "function_response"); err != nil {
					log.Printf("Failed to save function response: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "user", Parts: []Part{{FunctionResponse: &fr}}})
			}
			// Continue the outer loop to send the function response back to the model
		} else {
			// No function calls, break the outer loop
			break
		}
	}

	// Send 'Q' to signal end of content
	sendServerEvent(w, flusher, "Q")

	// Add agent response to chat history in DB
	if err := AddMessageToSession(sessionId, "model", agentResponseText, "text"); err != nil {
		log.Printf("newSessionAndMessage: Failed to save agent response: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save agent response: %v", err), http.StatusInternalServerError)
		return
	}
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

	session, err := GetSession(sessionId)
	if err != nil {
		log.Printf("chatMessage: Failed to load session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}
	systemPrompt := session.SystemPrompt

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

	userMessage := requestBody.Message

	// Add user message to current chat history in DB
	if err := AddMessageToSession(sessionId, "user", userMessage, "text"); err != nil {
		log.Printf("chatMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve session history from DB for Gemini API
	historyContents, err := GetSessionHistory(sessionId, true)
	if err != nil {
		log.Printf("chatMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	var agentResponseText string
	var currentHistory = historyContents

	for {
		seq, closer, err := GeminiClient.SendMessageStream(context.Background(), currentHistory, modelName, systemPrompt)
		if err != nil {
			log.Printf("chatMessage: CodeAssist API call failed: %v", err)
			http.Error(w, fmt.Sprintf("CodeAssist API call failed: %v", err), http.StatusInternalServerError)
			return
		}
		defer closer.Close()

		var functionCalls []FunctionCall // Changed to slice
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
					functionCalls = append(functionCalls, *part.FunctionCall) // Append to slice
					// Send FunctionCall to frontend
					argsJson, _ := json.Marshal(part.FunctionCall.Args)
					sendServerEvent(w, flusher, fmt.Sprintf("F\n%s\n%s", part.FunctionCall.Name, string(argsJson)))
					continue
				}

				// Check if it's a thought part
				if part.Thought { // If it's a thought
					// Parse subject and description from part.Text
					var thoughtText string
					matches := thoughtPattern.FindStringSubmatch(part.Text)
					if len(matches) > 2 {
						thoughtText = fmt.Sprintf("%s\n%s", matches[1], matches[2])
					} else {
						thoughtText = fmt.Sprintf("Thinking...\n%s", part.Text) // Placeholder subject
					}

					sendServerEvent(w, flusher, fmt.Sprintf("T\n%s", thoughtText))
					// Add thought message to chat history in DB
					if err := AddMessageToSession(sessionId, "thought", thoughtText, "thought"); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue // Skip further processing for thought parts
				}

				if part.Text != "" {
					sendServerEvent(w, flusher, fmt.Sprintf("M\n%s", part.Text))
					agentResponseText += part.Text
					modelResponseParts = append(modelResponseParts, part)
				}
			}
			// If any function calls were found in this caResp, break the seq loop
			if len(functionCalls) > 0 {
				break
			}
		}

		if len(functionCalls) > 0 {
			// Process all collected function calls
			for _, fc := range functionCalls {
				functionResponseValue, err := callFunction(fc)
				if err != nil {
					log.Printf("Error executing function %s: %v", fc.Name, err)
					functionResponseValue = map[string]interface{}{"error": err.Error()}
				}

				// Send FunctionResponse to frontend
				responseJson, err := json.Marshal(functionResponseValue)
				if err != nil {
					log.Printf("Failed to marshal function response for frontend: %v", err)
					responseJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
				}
				sendServerEvent(w, flusher, fmt.Sprintf("R\n%s", string(responseJson)))

				// Add FunctionCall to history
				fcJson, _ := json.Marshal(fc)
				if err := AddMessageToSession(sessionId, "model", string(fcJson), "function_call"); err != nil {
					log.Printf("Failed to save function call: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "model", Parts: []Part{{FunctionCall: &fc}}})

				// Add FunctionResponse to history
				fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
				frJson, _ := json.Marshal(fr)
				if err := AddMessageToSession(sessionId, "user", string(frJson), "function_response"); err != nil {
					log.Printf("Failed to save function response: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "user", Parts: []Part{{FunctionResponse: &fr}}})
			}
			// Continue the outer loop to send the function response back to the model
		} else {
			// No function calls, break the outer loop
			break
		}
	}

	// Send 'Q' to signal end of content
	sendServerEvent(w, flusher, "Q")

	// Add agent response to chat history in DB
	if err := AddMessageToSession(sessionId, "model", agentResponseText, "text"); err != nil {
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
		history = []Content{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessionId": sessionId, "history": history, "systemPrompt": session.SystemPrompt})
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

// New endpoint to get the default system prompt
func getDefaultSystemPrompt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, GetDefaultSystemPrompt())
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

func callFunction(fc FunctionCall) (map[string]interface{}, error) {
	switch fc.Name {
	case "list_directory":
		// Assuming args are map[string]interface{}
		_, ok := fc.Args["path"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid path argument for list_directory")
		}
		// TODO: Call the actual list_directory function from sandbox
		// For now, let's mock it
		// result, err := sandbox.ListDirectory(path) // Assuming sandbox.ListDirectory exists
		result := []string{"file1.txt", "file2.txt", "subdir/"} // Mock result
		return map[string]interface{}{"files": result}, nil
	case "read_file":
		_, ok := fc.Args["absolute_path"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid absolute_path argument for read_file")
		}
		// TODO: Call the actual read_file function from sandbox
		// For now, let's mock it
		// content, err := sandbox.ReadFile(path) // Assuming sandbox.ReadFile exists
		content := "Mock file content for " + fc.Args["absolute_path"].(string) // Mock content
		return map[string]interface{}{"content": content}, nil
	default:
		return nil, fmt.Errorf("unknown function: %s", fc.Name)
	}
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
