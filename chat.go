package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

type InitialState struct {
	SessionId    string            `json:"sessionId"`
	History      []FrontendMessage `json:"history"`
	SystemPrompt string            `json:"systemPrompt"`
}

// New session and message handler
func newSessionAndMessage(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("newSessionAndMessage", w) {
		return
	}

	var requestBody struct {
		Message      string           `json:"message"`
		SystemPrompt string           `json:"systemPrompt"`
		Attachments  []FileAttachment `json:"attachments"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "newSessionAndMessage") {
		return
	}

	sessionId := GenerateSessionID() // Generate new session ID

	// Always evaluate the system prompt as a Go template
	evaluatedSystemPrompt, err := GetEvaluatedSystemPrompt(requestBody.SystemPrompt)
	if err != nil {
		log.Printf("newSessionAndMessage: Error evaluating system prompt: %v", err)
		http.Error(w, fmt.Sprintf("Error evaluating system prompt: %v", err), http.StatusBadRequest)
		return
	}
	systemPrompt := evaluatedSystemPrompt

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

	setupSSEHeaders(w)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("newSessionAndMessage: Streaming unsupported!")
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}
	// Create a sseWriter for this client connection
	sseW := &sseWriter{ResponseWriter: w, Flusher: flusher, ctx: r.Context(), sessionId: sessionId}

	// Prepare initial state for streaming with only SessionId and SystemPrompt
	initialStateForClient := InitialState{
		SessionId:    sessionId,
		History:      []FrontendMessage{}, // Empty history for initial client state
		SystemPrompt: systemPrompt,
	}

	initialStateJSON, err := json.Marshal(initialStateForClient)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to marshal initial state for client: %v", err)
		http.Error(w, "Failed to prepare initial state for client", http.StatusInternalServerError)
		return
	}

	// Send the initial state (SessionId and SystemPrompt) as the first event
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	// Add this sseWriter to the active list for broadcasting subsequent events
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	// Retrieve session history from DB for Gemini API
	frontendHistory, err := GetSessionHistory(sessionId, true)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert FrontendMessage to Content for Gemini API

	// Prepare initial state for streaming
	initialState := InitialState{
		SessionId:    sessionId,
		History:      frontendHistory,
		SystemPrompt: systemPrompt,
	}

	// Handle streaming response from Gemini
	if err := streamGeminiResponse(initialState, sseW); err != nil {
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
		return
	}
}

// Chat message handler
func chatMessage(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("chatMessage", w) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

	var requestBody struct {
		Message     string           `json:"message"`
		Attachments []FileAttachment `json:"attachments"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "chatMessage") {
		return
	}

	session, err := GetSession(sessionId)
	if err != nil {
		log.Printf("chatMessage: Failed to load session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}
	systemPrompt := session.SystemPrompt

	setupSSEHeaders(w)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("chatMessage: Streaming unsupported!")
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}
	// Add this sseWriter to the active list for broadcasting subsequent events
	sseW := &sseWriter{ResponseWriter: w, Flusher: flusher, ctx: r.Context(), sessionId: sessionId}
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

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

	// Handle streaming response from Gemini
	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:    sessionId,
		History:      frontendHistory,
		SystemPrompt: systemPrompt,
	}

	if err := streamGeminiResponse(initialState, sseW); err != nil {
		log.Printf("chatMessage: Error streaming Gemini response: %v", err)
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
		return
	}
}

// New endpoint to load chat session history
func loadChatSession(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("loadChatSession", w) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

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

	// Ensure history is an empty slice if no messages are found, not nil
	if history == nil {
		history = []FrontendMessage{}
	}

	// Prepare initial state as a single JSON object
	initialState := InitialState{
		SessionId:    sessionId,
		History:      history,
		SystemPrompt: session.SystemPrompt,
	}

	// Check if it's an SSE request
	if r.Header.Get("Accept") == "text/event-stream" {
		setupSSEHeaders(w)
		flusher, ok := w.(http.Flusher)
		if !ok {
			log.Println("loadChatSession: Streaming unsupported!")
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		sseW := &sseWriter{ResponseWriter: w, Flusher: flusher, ctx: r.Context(), sessionId: sessionId}
		addSseWriter(sessionId, sseW)
		defer removeSseWriter(sessionId, sseW)

		initialStateJSON, err := json.Marshal(initialState)
		if err != nil {
			log.Printf("loadChatSession: Failed to marshal initial state: %v", err)
			http.Error(w, "Failed to prepare initial state", http.StatusInternalServerError)
			return
		}

		if hasActiveCall(sessionId) {
			log.Printf("loadChatSession: Active call found for session %s, streaming response.", sessionId)
			sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

			// Keep the connection open until client disconnects.
			// sseW will get any broadcasted messages over the course.
			<-r.Context().Done()
			log.Printf("loadChatSession: Client disconnected from SSE for session %s.", sessionId)
		} else {
			// If no active call, close the SSE connection after sending initial state
			log.Printf("loadChatSession: No active call for session %s, closing SSE connection.", sessionId)
			sseW.sendServerEvent(EventInitialStateNoCall, string(initialStateJSON))
			sseW.Close()
		}
	} else {
		// Original JSON response for non-SSE requests
		sendJSONResponse(w, initialState)
	}
}

func listChatSessions(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("listChatSessions", w) {
		return
	}

	sessions, err := GetAllSessions()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve sessions: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, sessions)
}

// New endpoint to delete a chat session
func deleteSession(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("deleteSession", w) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

	if err := DeleteSession(sessionId); err != nil {
		log.Printf("deleteSession: Failed to delete session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Session deleted successfully"})
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
