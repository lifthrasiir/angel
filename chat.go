package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// SSE Event Types
	EventSessionID          = "S" // Initial session ID
	EventInitialState       = "0" // Initial state with history (for active call)
	EventInitialStateNoCall = "1" // Initial state with history (for load session when no active call)
	EventFunctionCall       = "F" // Function call
	EventThought            = "T" // Thought process
	EventModelMessage       = "M" // Model message (text)
	EventFunctionReply      = "R" // Function response
	EventComplete           = "Q" // Query complete
	EventSessionName        = "N" // Session name inferred/updated
	EventError              = "E" // Error message
)

type InitialState struct {
	SessionId    string            `json:"sessionId"`
	History      []FrontendMessage `json:"history"`
	SystemPrompt string            `json:"systemPrompt"`
}

var thoughtPattern = regexp.MustCompile(`^\*\*(.*?)\*\*\n+(.*)\n*$`)

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

	// Use provided system prompt as-is (including empty string if intentionally set)
	systemPrompt := requestBody.SystemPrompt

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

	// Send the new session ID as the first event
	sseW.sendServerEvent(EventSessionID, sessionId)

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

	var requestBody struct {
		SessionID   string           `json:"sessionId"`
		Message     string           `json:"message"`
		Attachments []FileAttachment `json:"attachments"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "chatMessage") {
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

// sseWriter wraps http.ResponseWriter and http.Flusher to handle client disconnections gracefully.
//
// Connection cleanup sequence analysis from Go stdlib net/http/server.go:
// 1. defer cancelCtx() executes first -> context.Done() signal sent
// 2. c.finalFlush() called -> c.bufw.Flush() executed
// 3. putBufioWriter(c.bufw) called -> bufio.Writer.Reset(nil) -> panic if Flush() called after this
//
// Solution: Monitor request context to detect disconnection before Reset(nil) happens
type sseWriter struct {
	http.ResponseWriter
	http.Flusher
	mu           sync.Mutex
	disconnected bool
	ctx          context.Context
	sessionId    string // Add sessionId to sseWriter
}

// Close marks the sseWriter as disconnected and removes it from the active writers.
func (s *sseWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.disconnected {
		s.disconnected = true
		removeSseWriter(s.sessionId, s)
		log.Printf("sseWriter for session %s explicitly closed.", s.sessionId)
	}
}

// prepareSSEEventData prepares the SSE event data string.
// Note: `eventType` is for the logical message kind, not the browser event type!
func prepareSSEEventData(eventType, data string) []byte {
	escapedData := strings.ReplaceAll(data, "\n", "\ndata: ")
	return []byte(fmt.Sprintf("data: %s\ndata: %s\n\n", eventType, escapedData))
}

var (
	activeSseWriters = make(map[string][]*sseWriter) // sessionId -> list of sseWriters
	sseWritersMutex  sync.Mutex
)

// addSseWriter adds an sseWriter to the activeSseWriters map.
func addSseWriter(sessionId string, sseW *sseWriter) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()
	activeSseWriters[sessionId] = append(activeSseWriters[sessionId], sseW)
	log.Printf("Added sseWriter for session %s. Total writers: %d", sessionId, len(activeSseWriters[sessionId]))
}

// removeSseWriter removes an sseWriter from the activeSseWriters map.
func removeSseWriter(sessionId string, sseW *sseWriter) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()
	writers := activeSseWriters[sessionId]
	for i, w := range writers {
		if w == sseW {
			activeSseWriters[sessionId] = append(writers[:i], writers[i+1:]...)
			log.Printf("Removed sseWriter for session %s. Remaining writers: %d", sessionId, len(activeSseWriters[sessionId]))
			return
		}
	}
}

// broadcastToSession sends an event to all active sseWriters for a given session.
func broadcastToSession(sessionId string, eventType string, data string) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	writers, ok := activeSseWriters[sessionId]
	if !ok || len(writers) == 0 {
		return
	}

	// Prepare the event data once
	eventData := prepareSSEEventData(eventType, data)

	for _, sseW := range writers {
		sseW.mu.Lock()
		if !sseW.disconnected {
			_, err := sseW.writeUnsafe(eventData)
			if err == nil {
				sseW.flushUnsafe()
			}
		} else {
			log.Printf("Skipping broadcast to disconnected sseWriter for session %s", sessionId)
		}
		sseW.mu.Unlock()
	}
}

// Write implements the io.Writer interface for sseWriter.
// It checks for write errors and marks the connection as disconnected if an error occurs.
func (s *sseWriter) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeUnsafe(p)
}

// writeUnsafe performs the actual write without locking (must be called with mutex held)
func (s *sseWriter) writeUnsafe(p []byte) (n int, err error) {
	if s.disconnected {
		return len(p), nil // Return nil error and assume all bytes are "written" to avoid stopping execution
	}
	n, err = s.ResponseWriter.Write(p)
	if err != nil {
		log.Printf("sseWriter: Write error: %v", err)
		s.disconnected = true
		return n, nil // Do not return error to caller, just log and mark disconnected
	}
	return n, nil // Return nil error on success
}

// Flush implements the http.Flusher interface for sseWriter.
// It only flushes if the connection is not marked as disconnected.
func (s *sseWriter) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushUnsafe()
}

// flushUnsafe performs the actual flush without locking (must be called with mutex held)
func (s *sseWriter) flushUnsafe() {
	if s.disconnected {
		return
	}

	// Check if context is cancelled (connection cleanup started)
	// This happens before bufio.Writer.Reset(nil) which would cause panic
	select {
	case <-s.ctx.Done():

		s.disconnected = true
		return
	default:
		// Context not cancelled, safe to flush
	}

	s.Flusher.Flush()
}

func (sseW *sseWriter) sendServerEvent(eventType, data string) {
	sseW.mu.Lock()
	defer sseW.mu.Unlock()

	if sseW.disconnected {
		return
	}

	eventData := prepareSSEEventData(eventType, data)
	_, err := sseW.writeUnsafe(eventData)
	if err == nil {
		sseW.flushUnsafe()
	}
}

// New endpoint to get the default system prompt
func getDefaultSystemPrompt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, GetDefaultSystemPrompt())
}

// New endpoint to delete a chat session
func deleteSession(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("deleteSession", w) {
		return
	}

	sessionId := strings.TrimPrefix(r.URL.Path, "/api/chat/deleteSession/")
	if sessionId == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

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

// Helper function to stream Gemini API response
func streamGeminiResponse(initialState InitialState, sseW *sseWriter) error {
	var agentResponseText string
	currentHistory := convertFrontendMessagesToContent(initialState.History)

	// Create a cancellable context for the Gemini API call
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context is cancelled when function exits

	// Register the call with the call manager
	if err := startCall(initialState.SessionId, cancel); err != nil {
		log.Printf("streamGeminiResponse: Failed to start call for session %s: %v", initialState.SessionId, err)
		broadcastToSession(initialState.SessionId, EventError, err.Error())
		return err
	}
	defer removeCall(initialState.SessionId) // Ensure call is removed from manager when function exits

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState) // initialState struct를 JSON으로 마샬링
	if err != nil {
		log.Printf("streamGeminiResponse: Failed to marshal initial state: %v", err)
		return err
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))
	log.Printf("streamGeminiResponse: Sent initial state for session %s.", initialState.SessionId)

	// Goroutine to monitor client disconnection
	go func() {
		select {
		case <-ctx.Done():
			// Gemini API call context was cancelled (e.g., by explicit cancel request)
			// No need to do anything here, the main goroutine will handle it.
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// API call was cancelled (either by client disconnect or explicit cancel)
			// Mark the call as cancelled in the manager
			failCall(initialState.SessionId, ctx.Err())
			return ctx.Err() // Return the context error
		default:
			// Continue with the Gemini API call
		}

		seq, closer, err := GlobalGeminiState.GeminiClient.SendMessageStream(ctx, currentHistory, DefaultGeminiModel, initialState.SystemPrompt, &ThinkingConfig{IncludeThoughts: true})
		if err != nil {
			failCall(initialState.SessionId, err) // Mark the call as failed
			// Save a model_error message to the database
			errorMessage := fmt.Sprintf("Gemini API call failed: %v", err)
			if errors.Is(err, context.Canceled) {
				errorMessage = "Request canceled by user"
			}
			if err := AddMessageToSession(initialState.SessionId, "model", errorMessage, "model_error", nil); err != nil {
				log.Printf("Failed to save model_error message for API call failure: %v", err)
			}
			return fmt.Errorf("CodeAssist API call failed: %w", err)
		}
		defer closer.Close()

		var functionCalls []FunctionCall
		var modelResponseParts []Part

		for caResp := range seq {
			select {
			case <-ctx.Done():
				// Context was canceled, send a message to the frontend
				broadcastToSession(initialState.SessionId, EventError, "Request canceled by user")
				return ctx.Err() // Return the context error
			default:
				// Continue processing the response
			}
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
					broadcastToSession(initialState.SessionId, EventFunctionCall, fmt.Sprintf("%s\n%s", part.FunctionCall.Name, string(argsJson)))
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

					broadcastToSession(initialState.SessionId, EventThought, thoughtText)
					if err := AddMessageToSession(initialState.SessionId, "thought", thoughtText, "thought", nil); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue
				}

				if part.Text != "" {
					broadcastToSession(initialState.SessionId, EventModelMessage, part.Text)
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
				broadcastToSession(initialState.SessionId, EventFunctionReply, string(responseJson))

				fcJson, _ := json.Marshal(fc)
				if err := AddMessageToSession(initialState.SessionId, "model", string(fcJson), "function_call", nil); err != nil {
					log.Printf("Failed to save function call: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "model", Parts: []Part{{FunctionCall: &fc}}})

				fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
				frJson, _ := json.Marshal(fr)
				if err := AddMessageToSession(initialState.SessionId, "user", string(frJson), "function_response", nil); err != nil {
					log.Printf("Failed to save function response: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "user", Parts: []Part{{FunctionResponse: &fr}}})
			}
		} else {
			break
		}
	}

	broadcastToSession(initialState.SessionId, EventComplete, "")

	// Before saving the final agent response, delete any empty model messages
	if err := DeleteLastEmptyModelMessage(initialState.SessionId); err != nil {
		log.Printf("Failed to save last empty model message: %v", err)
	}

	// Infer session name after streaming is complete
	go inferAndSetSessionName(initialState.SessionId, initialState.History[0].Parts[0].Text, sseW)

	if err := AddMessageToSession(initialState.SessionId, "model", agentResponseText, "text", nil); err != nil {
		failCall(initialState.SessionId, err) // Mark the call as failed
		return fmt.Errorf("failed to save agent response: %w", err)
	}

	completeCall(initialState.SessionId) // Mark the call as completed
	return nil
}

// inferAndSetSessionName infers the session name using LLM and updates it in the DB.
func inferAndSetSessionName(sessionId string, userMessage string, sseW *sseWriter) {
	var inferredName string // Initialize to empty string

	defer func() {
		// This defer will execute at the end of the function, ensuring 'N' is sent.
		// If inferredName is still empty, it means inference failed or was skipped.
		sseW.sendServerEvent(EventSessionName, fmt.Sprintf("%s\n%s", sessionId, inferredName))
	}()

	session, err := GetSession(sessionId)
	if err != nil {
		log.Printf("Failed to get session %s for name inference: %v", sessionId, err)
		return // inferredName remains empty
	}
	if session.Name != "" { // If name is not empty, user has set it, do not infer
		inferredName = session.Name // Use existing name if already set
		return
	}

	nameSystemPrompt, nameInputPrompt := GetSessionNameInferencePrompts(userMessage, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	llmInferredName, err := GlobalGeminiState.GeminiClient.GenerateContentOneShot(ctx, []Content{{
		Role:  "user",
		Parts: []Part{{Text: nameInputPrompt}},
	}}, DefaultGeminiModel, nameSystemPrompt, &ThinkingConfig{IncludeThoughts: false})
	if err != nil {
		log.Printf("Failed to infer session name for %s: %v", sessionId, err)
		return // inferredName remains empty
	}

	llmInferredName = strings.TrimSpace(llmInferredName)
	if len(llmInferredName) > 100 || strings.Contains(llmInferredName, "\n") {
		log.Printf("Inferred name for session %s is invalid (too long or multi-line): %s", sessionId, llmInferredName)
		return // inferredName remains empty
	}

	inferredName = llmInferredName // Set inferredName only if successful

	if err := UpdateSessionName(sessionId, inferredName); err != nil {
		log.Printf("Failed to update session name for %s: %v", sessionId, err)
		// If DB update fails, inferredName is still the valid one, but DB might not reflect it.
		// We still send the inferredName to frontend, as it's the best we have.
		return
	}
}
