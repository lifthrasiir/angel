package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
)

type InitialState struct {
	SessionId              string            `json:"sessionId"`
	History                []FrontendMessage `json:"history"` // May or may not include thoughts
	SystemPrompt           string            `json:"systemPrompt"`
	WorkspaceID            string            `json:"workspaceId"`
	PrimaryBranchID        string            `json:"primaryBranchId"`
	Roots                  []string          `json:"roots"`
	CallElapsedTimeSeconds float64           `json:"callElapsedTimeSeconds,omitempty"`
	PendingConfirmation    string            `json:"pendingConfirmation,omitempty"`
	EnvChanged             *EnvChanged       `json:"envChanged,omitempty"` // Added EnvChanged field
}

// New session and message handler
func newSessionAndMessage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("newSessionAndMessage", w, r) {
		return
	}

	var requestBody struct {
		Message      string           `json:"message"`
		SystemPrompt string           `json:"systemPrompt"`
		Attachments  []FileAttachment `json:"attachments"`
		WorkspaceID  string           `json:"workspaceId"`
		Model        string           `json:"model"`
		FetchLimit   int              `json:"fetchLimit"`
		InitialRoots []string         `json:"initialRoots"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "newSessionAndMessage") {
		return
	}

	sessionId := generateID()

	var workspaceName string
	if requestBody.WorkspaceID != "" {
		workspace, err := GetWorkspace(db, requestBody.WorkspaceID)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get workspace %s", requestBody.WorkspaceID))
			return
		}
		workspaceName = workspace.Name
	}

	// Evaluate system prompt
	data := PromptData{workspaceName: workspaceName}
	systemPrompt, err := data.EvaluatePrompt(requestBody.SystemPrompt)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to evaluate system prompt")
		return
	}

	// Determine the model to use
	modelToUse := requestBody.Model
	if modelToUse == "" {
		modelToUse = DefaultGeminiModel // Default model for new sessions
	}

	// Handle InitialRoots if provided
	if len(requestBody.InitialRoots) > 0 {
		// Set initial roots as generation 0 environment
		err := SetInitialSessionEnv(db, sessionId, requestBody.InitialRoots)
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to set initial session environment")
			return
		}

		// Calculate EnvChanged from empty to initial roots
		rootsChanged, err := calculateRootsChanged([]string{}, requestBody.InitialRoots)
		if err != nil {
			log.Printf("newSessionAndMessage: Failed to calculate roots changed for initial roots: %v", err)
			// Non-fatal, continue without adding env change to prompt
		} else {
			envChanged := EnvChanged{Roots: &rootsChanged}
			envChangeContext := GetEnvChangeContext(envChanged)
			systemPrompt = systemPrompt + "\n" + envChangeContext // Append to system prompt
		}
	}

	// Create session with primary_branch_id (moved after InitialRoots handling)
	primaryBranchID, err := CreateSession(db, sessionId, systemPrompt, requestBody.WorkspaceID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create new session")
		return
	}

	userMessage := requestBody.Message

	// Create a new message chain
	mc, err := NewMessageChain(r.Context(), db, sessionId, primaryBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create new message chain")
		return
	}

	// Add user message to the chain
	mc.LastMessageModel = modelToUse
	mc.LastMessageGeneration = 0 // New session starts with generation 0
	userMsg, err := mc.Add(r.Context(), db, Message{Text: userMessage, Type: TypeUserText, Attachments: requestBody.Attachments})
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to save user message")
		return
	}

	// Update last_updated_at for the new session
	if err := UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for new session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	sseW := newSseWriter(sessionId, w, r)
	if sseW == nil {
		return
	}

	// Send acknowledgement for user message ID to frontend
	sseW.sendServerEvent(EventAcknowledge, fmt.Sprintf("%d", userMsg.ID))

	// Add this sseWriter to the active list for broadcasting subsequent events
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	// Retrieve session history from DB for LLM (full context)
	historyContext, err := GetSessionHistoryContext(db, sessionId, primaryBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve full session history for LLM")
		return
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistory, err := GetSessionHistoryPaginated(db, sessionId, primaryBranchID, 0, requestBody.FetchLimit)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve paginated session history for frontend")
		return
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	currentRoots, _, err := GetLatestSessionEnv(db, sessionId) // Generation is guaranteed to be 0
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to get latest session environment")
		return
	}

	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistory,
		SystemPrompt:    systemPrompt,
		WorkspaceID:     requestBody.WorkspaceID,
		PrimaryBranchID: primaryBranchID,
		Roots:           currentRoots,
	}

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to marshal initial state")
		return
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	// Handle streaming response from LLM
	// Pass full history to streamLLMResponse for LLM
	if err := streamLLMResponse(db, initialState, sseW, mc, true, time.Now(), historyContext); err != nil {
		sendInternalServerError(w, r, err, "Error streaming LLM response")
		return
	}
}

// Chat message handler
func chatMessage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("chatMessage", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		Message     string           `json:"message"`
		Attachments []FileAttachment `json:"attachments"`
		Model       string           `json:"model"`
		FetchLimit  int              `json:"fetchLimit"` // Add FetchLimit
	}

	if !decodeJSONRequest(r, w, &requestBody, "chatMessage") {
		return
	}

	session, err := GetSession(db, sessionId)
	if err != nil {
		log.Printf("chatMessage: Failed to load session %s: %v", sessionId, err)
		if errors.Is(err, sql.ErrNoRows) ||
			err.Error() == "sql: no rows in result set" ||
			strings.Contains(err.Error(), "no such table") {
			sendNotFoundError(w, r, "Session not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to load session")
		}
		return
	}
	systemPrompt := session.SystemPrompt
	primaryBranchID := session.PrimaryBranchID // Get primary branch ID from session

	var envChangedEventPayload string

	// Create a new message chain
	mc, err := NewMessageChain(r.Context(), db, sessionId, primaryBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create message chain")
		return
	}

	// Override the model to use the one specified in the request
	if requestBody.Model != "" {
		mc.LastMessageModel = requestBody.Model
	} else if mc.LastMessageModel == "" {
		mc.LastMessageModel = DefaultGeminiModel
	}

	_, currentGeneration, err := GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Check for environment changes and add system message to chain if needed
	if currentGeneration > mc.LastMessageGeneration {
		// Get old roots from the previous generation
		oldRoots, err := GetSessionEnv(db, sessionId, mc.LastMessageGeneration)
		if err != nil {
			log.Printf("chatMessage: Failed to get old session environment for session %s, generation %d: %v", sessionId, mc.LastMessageGeneration, err)
			// Non-fatal, continue with user message
		}

		// Get new roots from the current generation
		newRoots, err := GetSessionEnv(db, sessionId, currentGeneration)
		if err != nil {
			log.Printf("chatMessage: Failed to get new session environment for session %s, generation %d: %v", sessionId, currentGeneration, err)
			// Non-fatal, continue with user message
		}

		rootsChanged, err := calculateRootsChanged(oldRoots, newRoots)
		if err != nil {
			log.Printf("chatMessage: Failed to calculate roots changed: %v", err)
			// Non-fatal, continue with user message
		}

		envChanged := EnvChanged{Roots: &rootsChanged}

		// Marshal envChanged into JSON
		envChangedJSON, err := json.Marshal(envChanged) // Use = instead of :=
		if err != nil {
			log.Printf("chatMessage: Failed to marshal envChanged for system message: %v", err)
			// Non-fatal, continue with user message
		}

		systemMsg, err := mc.Add(r.Context(), db, Message{
			Text:            string(envChangedJSON),
			Type:            TypeEnvChanged,
			Attachments:     nil,
			CumulTokenCount: nil,
		})
		if err != nil {
			log.Printf("chatMessage: Failed to add envChanged message to chain: %v", err)
			// Non-fatal, continue with user message
		}

		envChangedEventPayload = fmt.Sprintf("%d\n%s", systemMsg.ID, string(envChangedJSON))
	}

	// Add user message to the chain
	userMsg, err := mc.Add(r.Context(), db, Message{
		Text:            requestBody.Message,
		Type:            TypeUserText,
		Attachments:     requestBody.Attachments,
		CumulTokenCount: nil,
	})
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to save user message")
		return
	}

	// Update last_updated_at for the session
	if err := UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	sseW := newSseWriter(sessionId, w, r)
	if sseW == nil {
		return
	}

	// Add this sseWriter to the active list for broadcasting subsequent events
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	if envChangedEventPayload != "" {
		sseW.sendServerEvent(EventGenerationChanged, envChangedEventPayload)
	}

	// Send acknowledgement for user message ID to frontend
	sseW.sendServerEvent(EventAcknowledge, fmt.Sprintf("%d", userMsg.ID))

	// Retrieve session history from DB for LLM (full context)
	fullFrontendHistoryForLLM, err := GetSessionHistoryContext(db, sessionId, primaryBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve full session history for LLM")
		return
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistoryForInitialState, err := GetSessionHistoryPaginated(db, sessionId, primaryBranchID, 0, requestBody.FetchLimit)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve paginated session history for frontend")
		return
	}

	roots, _, err := GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistoryForInitialState, // Use paginated history for frontend
		SystemPrompt:    systemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: primaryBranchID,
		Roots:           roots,
	}

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to prepare initial state")
		return
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	if err := streamLLMResponse(db, initialState, sseW, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		sendInternalServerError(w, r, err, "Error streaming LLM response")
		return
	}
}

// New endpoint to load chat session history
func loadChatSession(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("loadChatSession", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	// Check if session exists
	exists, err := SessionExists(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to check session existence")
		return
	}
	if !exists {
		sendNotFoundError(w, r, "Session not found")
		return
	}

	session, err := GetSession(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to load session")
		return
	}

	// Parse pagination parameters
	beforeMessageIDStr := r.URL.Query().Get("beforeMessageId")
	fetchLimitStr := r.URL.Query().Get("fetchLimit")

	beforeMessageID := 0 // Default to 0, meaning fetch from the latest
	if beforeMessageIDStr != "" {
		parsedID, err := strconv.Atoi(beforeMessageIDStr)
		if err != nil {
			sendBadRequestError(w, r, "Invalid beforeMessageId parameter")
			return
		}
		beforeMessageID = parsedID
	}

	fetchLimit := math.MaxInt // Default fetch limit
	if fetchLimitStr != "" {
		parsedLimit, err := strconv.Atoi(fetchLimitStr)
		if err != nil {
			sendBadRequestError(w, r, "Invalid fetchLimit parameter")
			return
		}
		fetchLimit = parsedLimit
	}

	// Check if it's an SSE request and initialize sseW early
	var sseW *sseWriter
	if r.Header.Get("Accept") == "text/event-stream" {
		sseW = newSseWriter(sessionId, w, r)
		if sseW == nil {
			return
		}

		// Send WorkspaceID hint to frontend as early as possible
		sseW.sendServerEvent(EventWorkspaceHint, session.WorkspaceID)

		addSseWriter(sessionId, sseW)
		defer removeSseWriter(sessionId, sseW)
	}

	// Use automatic branch detection to load history with pagination
	history, actualBranchID, err := GetSessionHistoryPaginatedWithAutoBranch(db, sessionId, beforeMessageID, fetchLimit)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to load session history")
		return
	}

	// Ensure history is an empty slice if no messages are found, not nil
	if history == nil {
		history = []FrontendMessage{}
	}

	currentRoots, currentGeneration, err := GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Get the generation of the last message in the history being loaded
	// If history is empty, assume generation 0.
	lastMessageGenerationInHistory := 0
	if len(history) > 0 {
		// Get the actual Message from DB using the ID of the last FrontendMessage
		lastFrontendMessageID, err := strconv.Atoi(history[len(history)-1].ID)
		if err != nil {
			log.Printf("loadChatSession: Failed to parse last message ID: %v", err)
			// Non-fatal, continue with generation 0
		} else {
			lastMessage, err := GetMessageByID(db, lastFrontendMessageID)
			if err != nil {
				log.Printf("loadChatSession: Failed to get last message by ID %d: %v", lastFrontendMessageID, err)
				// Non-fatal, continue with generation 0
			} else {
				lastMessageGenerationInHistory = lastMessage.Generation
			}
		}
	}

	var initialStateEnvChanged *EnvChanged
	if currentGeneration > lastMessageGenerationInHistory {
		oldRoots, err := GetSessionEnv(db, sessionId, lastMessageGenerationInHistory)
		if err != nil {
			log.Printf("loadChatSession: Failed to get old session environment for generation %d: %v", lastMessageGenerationInHistory, err)
			// Non-fatal, continue
		}
		rootsChanged, err := calculateRootsChanged(oldRoots, currentRoots)
		if err != nil {
			log.Printf("loadChatSession: Failed to calculate roots changed for initial state: %v", err)
			// Non-fatal, continue
		}
		initialStateEnvChanged = &EnvChanged{Roots: &rootsChanged}
	}

	// Prepare initial state as a single JSON object
	initialState := InitialState{
		SessionId:       sessionId,
		History:         history,
		SystemPrompt:    session.SystemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: actualBranchID,
		Roots:           currentRoots,
		EnvChanged:      initialStateEnvChanged,
	}

	branch, err := GetBranch(db, actualBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get branch %s", actualBranchID))
		return
	}

	if branch.PendingConfirmation != nil {
		initialState.PendingConfirmation = *branch.PendingConfirmation
	}

	// If it's an SSE request, handle streaming. Otherwise, send regular JSON response.
	if sseW != nil {
		if hasActiveCall(sessionId) {
			callStartTime, ok := GetCallStartTime(sessionId)
			if ok {
				initialState.CallElapsedTimeSeconds = time.Since(callStartTime).Seconds()
			}
			initialStateJSON, err := json.Marshal(initialState)
			if err != nil {
				sendInternalServerError(w, r, err, "Failed to marshal initial state with elapsed time")
				return
			}
			sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

			// Keep the connection open until client disconnects.
			// sseW will get any broadcasted messages over the course.
			<-r.Context().Done()
		} else {
			initialStateJSON, err := json.Marshal(initialState)
			if err != nil {
				sendInternalServerError(w, r, err, "Failed to marshal initial state")
				return
			}

			// If no active call, close the SSE connection after sending initial state
			sseW.sendServerEvent(EventInitialStateNoCall, string(initialStateJSON))
			time.Sleep(10 * time.Millisecond) // Give some time for the event to be processed
			sseW.Close()
		}
	} else {
		// Original JSON response for non-SSE requests
		sendJSONResponse(w, initialState)
	}
}

func listSessionsByWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("listSessionsByWorkspaceHandler", w, r) {
		return
	}

	workspaceID := r.URL.Query().Get("workspaceId")

	wsWithSessions, err := GetWorkspaceAndSessions(db, workspaceID)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to retrieve sessions for workspace %s", workspaceID))
		return
	}

	sendJSONResponse(w, wsWithSessions)
}

// calculateNewSessionEnvChangedHandler calculates EnvChanged for a new session.
// It expects newRoots as a JSON string in the query parameter.
func calculateNewSessionEnvChangedHandler(w http.ResponseWriter, r *http.Request) {
	// No authentication needed for this endpoint as it's for pre-session calculation
	// and doesn't modify any session state.

	newRootsJSON := r.URL.Query().Get("newRoots")
	if newRootsJSON == "" {
		sendBadRequestError(w, r, "newRoots query parameter is required")
		return
	}

	var newRoots []string
	if err := json.Unmarshal([]byte(newRootsJSON), &newRoots); err != nil {
		sendBadRequestError(w, r, "Invalid newRoots JSON")
		return
	}

	// oldRoots is always empty for a new session's initial environment calculation
	oldRoots := []string{}

	rootsChanged, err := calculateRootsChanged(oldRoots, newRoots)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to calculate environment changes")
		return
	}

	envChanged := EnvChanged{Roots: &rootsChanged}
	sendJSONResponse(w, envChanged)
}

// New endpoint to delete a chat session
func deleteSession(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("deleteSession", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	if err := DeleteSession(db, sessionId); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete session %s", sessionId))
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Session deleted successfully"})
}

// Helper function to convert FrontendMessage to Content for LLM
func convertFrontendMessagesToContent(db *sql.DB, frontendMessages []FrontendMessage) []Content {
	var contents []Content
	// Apply curation rules before converting to Content
	curatedMessages := applyCurationRules(frontendMessages)

	for _, fm := range curatedMessages {
		var parts []Part

		if fm.Type == TypeCommand {
			continue // Command messages are only visible to users
		}

		// Add text part if present
		if len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			parts = append(parts, Part{
				Text:             fm.Parts[0].Text,
				ThoughtSignature: fm.Parts[0].ThoughtSignature,
			})
		}

		// Add attachments as InlineData with preceding hash information
		hasBinaryAttachments := false
		for _, att := range fm.Attachments {
			if att.Hash != "" { // Only process if hash exists
				if att.Omitted {
					// Attachment was omitted due to clearblobs command
					parts = append(parts,
						Part{Text: fmt.Sprintf("[Binary with hash %s is currently **UNPROCESSED**. You **MUST** use recall(query='%[1]s') to gain access to its content for internal analysis. **Until recalled, you have NO information about this binary's content, and any attempt to describe or act upon it will be pure guesswork.**]", att.Hash)},
					)
				} else {
					// Normal blob processing
					blobData, err := GetBlob(db, att.Hash)
					if err != nil {
						log.Printf("Error retrieving blob data for hash %s: %v", att.Hash, err)
						// Decide how to handle this error: skip attachment, return error, etc.
						// For now, we'll skip this attachment to avoid breaking the whole message.
						continue
					}
					hasBinaryAttachments = true
					parts = append(parts,
						Part{Text: fmt.Sprintf("[Binary with hash %s follows:]", att.Hash)},
						Part{
							InlineData: &InlineData{
								MimeType: att.MimeType,
								Data:     base64.StdEncoding.EncodeToString(blobData),
							},
						},
					)
				}
			}
		}

		// Add warning message after all binary attachments have been displayed
		if hasBinaryAttachments {
			parts = append(parts, Part{Text: "[IMPORTANT: The hashes shown above are explicitly for SHA-512/256 hash-accepting tools only and must never be exposed to users without explicit request.]"})
		}

		// Handle function calls and responses (these should override text/attachments for their specific message types)
		if fm.Type == TypeFunctionCall && len(fm.Parts) > 0 && fm.Parts[0].FunctionCall != nil {
			fc := fm.Parts[0].FunctionCall
			if fc.Name == GeminiCodeExecutionToolName {
				var ec ExecutableCode
				// fc.Args is map[string]interface{}, need to marshal then unmarshal
				argsBytes, err := json.Marshal(fc.Args)
				if err != nil {
					log.Printf("Error marshaling FunctionCall args to JSON for ExecutableCode: %v", err)
					parts = append(parts, Part{FunctionCall: fc, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else if err := json.Unmarshal(argsBytes, &ec); err != nil {
					log.Printf("Error unmarshaling ExecutableCode from FunctionCall args: %v", err)
					parts = append(parts, Part{FunctionCall: fc, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else {
					parts = append(parts, Part{ExecutableCode: &ec, ThoughtSignature: fm.Parts[0].ThoughtSignature})
				}
			} else {
				parts = append(parts, Part{FunctionCall: fc, ThoughtSignature: fm.Parts[0].ThoughtSignature})
			}
		} else if fm.Type == TypeFunctionResponse && len(fm.Parts) > 0 && fm.Parts[0].FunctionResponse != nil {
			fr := fm.Parts[0].FunctionResponse
			if fr.Name == GeminiCodeExecutionToolName {
				var cer CodeExecutionResult
				// fr.Response is interface{}, need to marshal then unmarshal
				responseBytes, err := json.Marshal(fr.Response)
				if err != nil {
					log.Printf("Error marshaling FunctionResponse.Response to JSON for CodeExecutionResult: %v", err)
					parts = append(parts, Part{FunctionResponse: fr, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else if err := json.Unmarshal(responseBytes, &cer); err != nil {
					log.Printf("Error unmarshaling CodeExecutionResult from FunctionResponse.Response: %v", err)
					parts = append(parts, Part{FunctionResponse: fr, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else {
					parts = append(parts, Part{CodeExecutionResult: &cer, ThoughtSignature: fm.Parts[0].ThoughtSignature})
				}
			} else {
				parts = append(parts, Part{FunctionResponse: fr, ThoughtSignature: fm.Parts[0].ThoughtSignature})
			}
		} else if (fm.Type == TypeSystemPrompt || fm.Type == TypeEnvChanged) && len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			// System_prompt should expand to *two* `Content`s
			prompt := fm.Parts[0].Text
			if fm.Type == TypeEnvChanged {
				var envChanged EnvChanged
				err := json.Unmarshal([]byte(prompt), &envChanged)
				if err != nil {
					log.Printf("Error unmarshalling envChanged JSON: %v", err)
				} else {
					prompt = GetEnvChangeContext(envChanged)
				}
			}
			contents = append(contents,
				Content{
					Role: RoleModel,
					Parts: []Part{{
						FunctionCall: &FunctionCall{
							Name: "new_system_prompt",
							Args: map[string]interface{}{},
						},
						ThoughtSignature: fm.Parts[0].ThoughtSignature,
					}},
				},
				Content{
					Role: RoleUser,
					Parts: []Part{{
						FunctionResponse: &FunctionResponse{
							Name:     "new_system_prompt",
							Response: map[string]interface{}{"prompt": prompt},
						},
					}},
				},
			)
			continue
		}

		// If parts is still empty, add an empty text part to satisfy Gemini API requirements
		if len(parts) == 0 {
			parts = append(parts, Part{Text: ""})
		}

		contents = append(contents, Content{
			Role:  fm.Type.Role(),
			Parts: parts,
		})
	}
	return contents
}

// applyCurationRules applies the specified curation rules to a slice of FrontendMessage.
func applyCurationRules(messages []FrontendMessage) []FrontendMessage {
	var curated []FrontendMessage
	for i := 0; i < len(messages); i++ {
		currentMsg := messages[i]

		// Rule 1: Remove consecutive user text messages
		// If current is user text and next is user text (ignoring errors in between)
		if currentMsg.Type == TypeUserText {
			nextUserTextIndex := -1
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Type == TypeError || messages[j].Type == TypeModelError {
					continue // Ignore errors for continuity
				}
				if messages[j].Type == TypeUserText {
					nextUserTextIndex = j
					break
				}
				// If we find any other type of message, it breaks the "consecutive user text" chain
				break
			}
			if nextUserTextIndex != -1 {
				// This 'currentMsg' is followed by another user text message, so skip it.
				continue
			}
		}

		// Rule 2: Remove function_call if not followed by function_response
		// If current is model function_call
		if currentMsg.Type == TypeFunctionCall {
			foundResponse := false
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Type == TypeThought {
					continue // Ignore thoughts and errors for continuity
				}
				if messages[j].Type == TypeFunctionResponse {
					foundResponse = true
					break
				}
				// If we find any other type of message, it means no immediate function response
				break
			}
			if !foundResponse {
				// This 'currentMsg' (function_call) is not followed by a function_response, so skip it.
				continue
			}
		}

		curated = append(curated, currentMsg)
	}
	return curated
}
