package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var thoughtPattern = regexp.MustCompile(`^\*\*(.*?)\*\*\s*(.*)`)

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
	sseW := &sseWriter{ResponseWriter: w, Flusher: flusher, ctx: r.Context()}

	// Send the new session ID as the first event
	sseW.sendServerEvent(fmt.Sprintf("S\n%s", sessionId))

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
		sseW.sendServerEvent(fmt.Sprintf("N\n%s\n%s", sID, inferredName))
	}(sessionId, userMessage, systemPrompt, "") // Pass empty string for initialAgentResponse, it will be filled by streamGeminiResponse

	// Handle streaming response from Gemini
	if err := streamGeminiResponse(sseW, r, sessionId, systemPrompt, historyContents); err != nil {
		log.Printf("newSessionAndMessage: Error streaming Gemini response: %v", err)
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
	sseW := &sseWriter{ResponseWriter: w, Flusher: flusher, ctx: r.Context()}

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
	if err := streamGeminiResponse(sseW, r, sessionId, systemPrompt, historyContents); err != nil {
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

	sendJSONResponse(w, map[string]interface{}{"sessionId": sessionId, "history": history, "systemPrompt": session.SystemPrompt})
}

// New endpoint to list all chat sessions
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

func (sseW *sseWriter) sendServerEvent(data string) {
	sseW.mu.Lock()
	defer sseW.mu.Unlock()

	if sseW.disconnected {
		return
	}

	escapedData := strings.ReplaceAll(data, "\n", "\ndata: ")
	eventData := fmt.Sprintf("data: %s\n\n", escapedData)
	_, err := sseW.writeUnsafe([]byte(eventData))
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
func streamGeminiResponse(sseW *sseWriter, r *http.Request, sessionId string, systemPrompt string, initialHistory []Content) error {
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
					sseW.sendServerEvent(fmt.Sprintf("F\n%s\n%s", part.FunctionCall.Name, string(argsJson)))
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

					sseW.sendServerEvent(fmt.Sprintf("T\n%s", thoughtText))
					if err := AddMessageToSession(sessionId, "thought", thoughtText, "thought", nil); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue
				}

				if part.Text != "" {
					sseW.sendServerEvent(fmt.Sprintf("M\n%s", part.Text))
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
				sseW.sendServerEvent(fmt.Sprintf("R\n%s", string(responseJson)))

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

	sseW.sendServerEvent("Q")

	if err := AddMessageToSession(sessionId, "model", agentResponseText, "text", nil); err != nil {
		return fmt.Errorf("failed to save agent response: %w", err)
	}
	return nil
}
