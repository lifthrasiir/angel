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

	// Use the primary_branch_id from the session to load history with pagination
	history, err := GetSessionHistoryPaginated(db, sessionId, session.PrimaryBranchID, beforeMessageID, fetchLimit)
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
		PrimaryBranchID: session.PrimaryBranchID,
		Roots:           currentRoots,
		EnvChanged:      initialStateEnvChanged,
	}

	branch, err := GetBranch(db, session.PrimaryBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get branch %s", session.PrimaryBranchID))
		return
	}

	if branch.PendingConfirmation != nil {
		initialState.PendingConfirmation = *branch.PendingConfirmation
	}

	// Check if it's an SSE request
	if r.Header.Get("Accept") == "text/event-stream" {
		sseW := newSseWriter(sessionId, w, r)
		if sseW == nil {
			return
		}

		addSseWriter(sessionId, sseW)
		defer removeSseWriter(sessionId, sseW)

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

// createBranchHandler creates a new branch from a given parent message.
func createBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("createBranchHandler", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		UpdatedMessageID int    `json:"updatedMessageId"`
		NewMessageText   string `json:"newMessageText"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "createBranchHandler") {
		return
	}

	// Get the updated message's role, type, parent_message_id, and branch_id to validate branching and create new branch
	updatedType, updatedParentMessageID, updatedBranchID, err := GetMessageDetails(db, requestBody.UpdatedMessageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendNotFoundError(w, r, "Updated message not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to get updated message details")
		}
		return
	}

	// Validate that the updated message is a user message of type 'text'
	if updatedType != TypeUserText {
		sendBadRequestError(w, r, "Branching is only allowed from user messages of type 'text'.")
		return
	}

	// Validate that the updated message is not the first message of the session
	if !updatedParentMessageID.Valid {
		sendBadRequestError(w, r, "Branching is not allowed from the first message of the session.")
		return
	}

	newBranchID := generateID()

	// The branch_from_message_id for the new branch is the parent of the updatedMessageID
	branchFromMessageID := int(updatedParentMessageID.Int64)

	// Create the new branch in the branches table
	// Pass the updatedBranchID as a pointer, and branchFromMessageID as a pointer
	if _, err := CreateBranch(db, newBranchID, sessionId, &updatedBranchID, &branchFromMessageID); err != nil {
		sendInternalServerError(w, r, err, "Failed to create new branch in branches table")
		return
	}

	// Create the new message in the new branch, with updatedMessageID as its parent
	_, currentGeneration, err := GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Create the new message in the new branch, with updatedMessageID as its parent
	newMessageID, err := AddMessageToSession(r.Context(), db, Message{
		SessionID:       sessionId,
		BranchID:        newBranchID,
		ParentMessageID: &branchFromMessageID,
		ChosenNextID:    nil,
		Text:            requestBody.NewMessageText,
		Type:            TypeUserText,
		Attachments:     nil,
		CumulTokenCount: nil,
		Model:           "", // Model will be inferred or set later
		Generation:      currentGeneration,
	})
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to add new message to new branch")
		return
	}

	// Update the chosen_next_id of the branch_from_message_id to point to the new message
	if err := UpdateMessageChosenNextID(db, branchFromMessageID, &newMessageID); err != nil {
		log.Printf("createBranchHandler: Failed to update chosen_next_id for branch_from_message_id %d: %v", branchFromMessageID, err)
		// Non-fatal, but log it
	}

	// Set the new branch as the primary branch for the session
	if err := UpdateSessionPrimaryBranchID(db, sessionId, newBranchID); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to set new branch as primary for session %s", sessionId))
		return
	}

	sendJSONResponse(w, map[string]string{
		"status":       "success",
		"newBranchId":  newBranchID,
		"newMessageId": fmt.Sprintf("%d", newMessageID),
	})
}

// switchBranchHandler switches the primary branch of a session.
func switchBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("switchBranchHandler", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		NewPrimaryBranchID string `json:"newPrimaryBranchId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "switchBranchHandler") {
		return
	}

	// Get current session to retrieve old primary branch ID
	session, err := GetSession(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get session %s", sessionId))
		return
	}
	oldPrimaryBranchID := session.PrimaryBranchID

	// Update the session's primary branch ID
	if err := UpdateSessionPrimaryBranchID(db, sessionId, requestBody.NewPrimaryBranchID); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to switch primary branch for session %s to %s", sessionId, requestBody.NewPrimaryBranchID))
		return
	}

	handleOldPrimaryBranchChosenNextID(db, sessionId, oldPrimaryBranchID, requestBody.NewPrimaryBranchID)
	handleNewPrimaryBranchChosenNextID(db, requestBody.NewPrimaryBranchID)

	sendJSONResponse(w, map[string]string{
		"status":          "success",
		"primaryBranchId": requestBody.NewPrimaryBranchID,
	})
}

// handleOldPrimaryBranchChosenNextID handles the chosen_next_id logic for the old primary branch.
func handleOldPrimaryBranchChosenNextID(db *sql.DB, sessionId, oldPrimaryBranchID, newPrimaryBranchID string) {
	if oldPrimaryBranchID != "" && oldPrimaryBranchID != newPrimaryBranchID {
		oldBranch, err := GetBranch(db, oldPrimaryBranchID)
		if err != nil {
			log.Printf("switchBranchHandler: Failed to get old branch %s: %v", oldPrimaryBranchID, err)
			// Non-fatal, continue
		}

		if oldBranch.BranchFromMessageID != nil {
			// This was a branched branch. Its parent's chosen_next_id needs to revert to its original next message.
			parentMsgID := *oldBranch.BranchFromMessageID

			// Find the message that originally followed parentMsgID in its own branch
			originalNextMessageID, err := GetOriginalNextMessageID(db, parentMsgID, oldPrimaryBranchID)
			if err != nil && err != sql.ErrNoRows {
				log.Printf("switchBranchHandler: Failed to find original next message for %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}

			var chosenNextID *int
			if originalNextMessageID.Valid {
				val := int(originalNextMessageID.Int64)
				chosenNextID = &val
			}

			if err := UpdateMessageChosenNextID(db, parentMsgID, chosenNextID); err != nil {
				log.Printf("switchBranchHandler: Failed to update chosen_next_id for message %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}
		} else {
			// This was the initial branch. Its last message's chosen_next_id needs to revert to its original next message.
			lastMessageID, _, _, err := GetLastMessageInBranch(db, sessionId, oldPrimaryBranchID)
			if err != nil && err != sql.ErrNoRows {
				log.Printf("switchBranchHandler: Failed to get last message ID for old primary branch %s: %v", oldPrimaryBranchID, err)
				// Non-fatal, continue
			}

			if lastMessageID != 0 {
				// Find the message that originally followed lastMessageID in its own branch
				var originalNextMessageID sql.NullInt64
				err := db.QueryRow(`
					SELECT id FROM messages
					WHERE parent_message_id = ? AND branch_id = ?
					ORDER BY created_at ASC LIMIT 1
				`, lastMessageID, oldPrimaryBranchID).Scan(&originalNextMessageID)

				if err != nil && err != sql.ErrNoRows {
					log.Printf("switchBranchHandler: Failed to find original next message for %d: %v", lastMessageID, err)
					// Non-fatal, continue
				}

				var chosenNextID *int
				if originalNextMessageID.Valid {
					val := int(originalNextMessageID.Int64)
					chosenNextID = &val
				}

				if err := UpdateMessageChosenNextID(db, lastMessageID, chosenNextID); err != nil {
					log.Printf("switchBranchHandler: Failed to update chosen_next_id for message %d: %v", lastMessageID, err)
					// Non-fatal, continue
				}
			}
		}
	}
}

// handleNewPrimaryBranchChosenNextID handles the chosen_next_id logic for the new primary branch.
func handleNewPrimaryBranchChosenNextID(db *sql.DB, newPrimaryBranchID string) {
	// If the new primary branch is a branched branch, update its branch_from_message_id's chosen_next_id
	// to point to the first message of this new primary branch.
	newBranch, err := GetBranch(db, newPrimaryBranchID)
	if err != nil {
		log.Printf("switchBranchHandler: Failed to get new branch %s: %v", newPrimaryBranchID, err)
		// Non-fatal, continue
	} else if newBranch.BranchFromMessageID != nil {
		parentMsgID := *newBranch.BranchFromMessageID

		// Find the first message of the new primary branch that has parentMsgID as its parent
		var firstMessageOfNewBranchID int
		firstMessageOfNewBranchID, err := GetFirstMessageOfBranch(db, parentMsgID, newPrimaryBranchID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				log.Printf("switchBranchHandler: No first message found for new branch %s. Skipping chosen_next_id update.", newPrimaryBranchID)
			} else {
				log.Printf("switchBranchHandler: Failed to find first message of new branch %s: %v", newPrimaryBranchID, err)
			}
			// Non-fatal, continue
		} else {
			if err := UpdateMessageChosenNextID(db, parentMsgID, &firstMessageOfNewBranchID); err != nil {
				log.Printf("switchBranchHandler: Failed to update chosen_next_id for message %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}
		}
	}
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
		// Add text part if present
		if len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			parts = append(parts, Part{
				Text:             fm.Parts[0].Text,
				ThoughtSignature: fm.Parts[0].ThoughtSignature,
			})
		}

		// Add attachments as InlineData
		for _, att := range fm.Attachments {
			if att.Hash != "" { // Only process if hash exists
				blobData, err := GetBlob(db, att.Hash)
				if err != nil {
					log.Printf("Error retrieving blob data for hash %s: %v", att.Hash, err)
					// Decide how to handle this error: skip attachment, return error, etc.
					// For now, we'll skip this attachment to avoid breaking the whole message.
					continue
				}
				parts = append(parts, Part{
					InlineData: &InlineData{
						MimeType: att.MimeType,
						Data:     base64.StdEncoding.EncodeToString(blobData),
					},
				})
			}
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

// confirmBranchHandler handles the confirmation of a pending action on a branch.
func confirmBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("confirmBranchHandler", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	branchId := vars["branchId"]
	if sessionId == "" || branchId == "" {
		sendBadRequestError(w, r, "Session ID and Branch ID are required")
		return
	}

	var requestBody struct {
		Approved     bool                   `json:"approved"`
		ModifiedData map[string]interface{} `json:"modifiedData"` // Optional: tool arguments if modified
	}

	if !decodeJSONRequest(r, w, &requestBody, "confirmBranchHandler") {
		return
	}

	// Clear pending_confirmation for the branch regardless of approval/denial
	if err := UpdateBranchPendingConfirmation(db, branchId, ""); err != nil { // Set to empty string to clear
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to clear confirmation status for branch %s", branchId))
		return
	}

	// Get session and branch details
	session, err := GetSession(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get session %s", sessionId))
		return
	}

	// If the confirmed branch is not the primary branch, switch to it
	if session.PrimaryBranchID != branchId {
		if err := UpdateSessionPrimaryBranchID(db, sessionId, branchId); err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to switch primary branch to %s", branchId))
			return
		}
		handleOldPrimaryBranchChosenNextID(db, sessionId, session.PrimaryBranchID, branchId)
		handleNewPrimaryBranchChosenNextID(db, branchId)
	}

	// Create a new message chain
	mc, err := NewMessageChain(r.Context(), db, sessionId, branchId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to create message chain for session %s and branch %s", sessionId, branchId))
		return
	}

	// Get the full message details for the last message (the function call)
	lastMessage, err := GetMessageByID(db, mc.LastMessageID)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get last message details for ID %d", mc.LastMessageID))
		return
	}

	if !requestBody.Approved {
		// User denied the confirmation
		log.Printf("confirmBranchHandler: User denied confirmation for session %s, branch %s", sessionId, branchId)

		// Construct the function response for denial
		functionName, _, _ := strings.Cut(lastMessage.Text, "\n")
		denialResponseMap := map[string]interface{}{"error": "User denied tool execution"}
		fr := FunctionResponse{Name: functionName, Response: denialResponseMap}
		frJson, err := json.Marshal(fr)
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to marshal denial function response")
			return
		}

		// Add the function response message to the session
		denialResponseMsg, err := mc.Add(r.Context(), db, Message{
			Text:            string(frJson),
			Type:            TypeFunctionResponse,
			Attachments:     nil,
			CumulTokenCount: nil,
		})
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to save denial function response message")
			return
		}

		// Send EventFunctionResponse to frontend
		sseW := newSseWriter(sessionId, w, r)
		if sseW == nil {
			return
		}
		addSseWriter(sessionId, sseW)
		defer removeSseWriter(sessionId, sseW)

		denialResponseMapJson, err := json.Marshal(FunctionResponsePayload{Response: denialResponseMap})
		if err != nil {
			log.Printf("confirmBranchHandler: Failed to marshal denial response map for SSE: %v", err)
			denialResponseMapJson = fmt.Appendf(nil, `{"response": {"error": "%v"}}`, err)
		}
		formattedData := fmt.Sprintf("%d\n%s\n%s", denialResponseMsg.ID, functionName, string(denialResponseMapJson))
		sseW.sendServerEvent(EventFunctionResponse, formattedData)

		// Send EventComplete to signal the end of the pending state
		broadcastToSession(sessionId, EventComplete, "") // Signal completion
		sendJSONResponse(w, map[string]string{"status": "denied", "message": "Confirmation denied by user"})
		return
	}

	// User approved the confirmation
	// Extract the original function call from the last message
	var fc FunctionCall
	if lastMessage.Type == TypeFunctionCall {
		if err := json.Unmarshal([]byte(lastMessage.Text), &fc); err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to unmarshal function call from message %d", lastMessage.ID))
			return
		}
	} else {
		sendBadRequestError(w, r, fmt.Sprintf("Last message %d is not a function call (type: %s)", lastMessage.ID, lastMessage.Type))
		return
	}

	// If modifiedData is provided, update the function call arguments
	if requestBody.ModifiedData != nil {
		for k, v := range requestBody.ModifiedData {
			fc.Args[k] = v
		}
	}

	// Re-execute the tool function with confirmationReceived = true
	toolResults, err := CallToolFunction(r.Context(), fc, ToolHandlerParams{
		ModelName:            lastMessage.Model,
		SessionId:            sessionId,
		BranchId:             branchId,
		ConfirmationReceived: true,
	})
	if err != nil {
		log.Printf("confirmBranchHandler: Error re-executing function %s after confirmation: %v", fc.Name, err)
		// If re-execution fails, send an error event and stop streaming
		sseW := newSseWriter(sessionId, w, r)
		if sseW == nil {
			return
		}
		addSseWriter(sessionId, sseW)
		defer removeSseWriter(sessionId, sseW)
		broadcastToSession(sessionId, EventError, fmt.Sprintf("Tool re-execution failed: %v", err))
		sendJSONResponse(w, map[string]string{"status": "error", "message": fmt.Sprintf("Tool re-execution failed: %v", err)})
		return
	}

	// Save the function response message
	fr := FunctionResponse{Name: fc.Name, Response: toolResults.Value}
	frJson, err := json.Marshal(fr)
	if err != nil { // Check error from json.Marshal(fr)
		log.Printf("confirmBranchHandler: Failed to marshal function response for frontend: %v", err)
		// If marshaling fails, create a basic error JSON
		frJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
	}

	// Add the function response message to the session
	// Note: cumulTokenCount is not updated here, as it's handled by streamLLMResponse
	functionResponseMsg, err := mc.Add(r.Context(), db, Message{
		Text:        string(frJson),
		Type:        TypeFunctionResponse,
		Attachments: toolResults.Attachments,
	})
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to save function response message after confirmation")
		return
	}

	// Send EventFunctionResponse to frontend
	sseW := newSseWriter(sessionId, w, r)
	if sseW == nil {
		return
	}
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	functionResponseValueJson, err := json.Marshal(FunctionResponsePayload{
		Response:    toolResults.Value,
		Attachments: toolResults.Attachments,
	})
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to marshal function response value for SSE: %v", err)
		functionResponseValueJson = fmt.Appendf(nil, `{"response": {"error": "%v"}}`, err)
	}
	formattedData := fmt.Sprintf("%d\n%s\n%s", functionResponseMsg.ID, fc.Name, string(functionResponseValueJson))
	sseW.sendServerEvent(EventFunctionResponse, formattedData)

	// Retrieve session history from DB for LLM (full context)
	fullFrontendHistoryForLLM, err := GetSessionHistoryContext(db, sessionId, branchId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve full session history for LLM after confirmation")
		return
	}

	var roots []string
	roots, mc.LastMessageGeneration, err = GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Prepare initial state for streaming (only for passing session details, not for sending EventInitialState)
	initialState := InitialState{
		SessionId:           sessionId,
		History:             []FrontendMessage{}, // History will be streamed
		SystemPrompt:        session.SystemPrompt,
		WorkspaceID:         session.WorkspaceID,
		PrimaryBranchID:     branchId,
		Roots:               roots,
		PendingConfirmation: "", // Clear pending confirmation in initial state
	}

	// Resume streaming from the point after the function response
	if err := streamLLMResponse(db, initialState, sseW, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		sendInternalServerError(w, r, err, "Error streaming LLM response after confirmation")
		return
	}

	sendJSONResponse(w, map[string]string{"status": "approved", "message": "Confirmation approved and streaming resumed"})
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
