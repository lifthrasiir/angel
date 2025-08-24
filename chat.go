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
	}

	if !decodeJSONRequest(r, w, &requestBody, "newSessionAndMessage") {
		return
	}

	sessionId := generateID()

	var workspaceName string
	if requestBody.WorkspaceID != "" {
		workspace, err := GetWorkspace(db, requestBody.WorkspaceID)
		if err != nil {
			log.Printf("newSessionAndMessage: Failed to get workspace %s: %v", requestBody.WorkspaceID, err)
			http.Error(w, fmt.Sprintf("Failed to get workspace: %v", err), http.StatusInternalServerError)
			return
		}
		workspaceName = workspace.Name
	}

	// Evaluate system prompt
	data := PromptData{workspaceName: workspaceName}
	systemPrompt, err := data.EvaluatePrompt(requestBody.SystemPrompt)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to evaluate system prompt: %v", err)
		http.Error(w, fmt.Sprintf("Failed to evaluate system prompt: %v", err), http.StatusInternalServerError)
		return
	}

	// Determine the model to use
	modelToUse := requestBody.Model
	if modelToUse == "" {
		modelToUse = DefaultGeminiModel // Default model for new sessions
	}

	// Create session with primary_branch_id
	primaryBranchID, err := CreateSession(db, sessionId, systemPrompt, requestBody.WorkspaceID)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to create new session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create new session: %v", err), http.StatusInternalServerError)
		return
	}

	userMessage := requestBody.Message

	// Add message with branch_id, no parent_message_id, no chosen_next_id initially
	userMessageID, err := AddMessageToSession(r.Context(), db, Message{
		SessionID:       sessionId,
		BranchID:        primaryBranchID,
		ParentMessageID: nil,
		ChosenNextID:    nil,
		Role:            RoleUser,
		Text:            userMessage,
		Type:            TypeText,
		Attachments:     requestBody.Attachments,
		CumulTokenCount: nil,
		Model:           modelToUse,
	})
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
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
	sseW.sendServerEvent(EventAcknowledge, fmt.Sprintf("%d", userMessageID))

	// Add this sseWriter to the active list for broadcasting subsequent events
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	// Retrieve session history from DB for Gemini API (full context)
	historyContext, err := GetSessionHistoryContext(db, sessionId, primaryBranchID)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to retrieve full session history for Gemini: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve full session history for Gemini: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistory, err := GetSessionHistoryPaginated(db, sessionId, primaryBranchID, 0, requestBody.FetchLimit)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to retrieve paginated session history for frontend: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve paginated session history for frontend: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistory,
		SystemPrompt:    systemPrompt,
		WorkspaceID:     requestBody.WorkspaceID,
		PrimaryBranchID: primaryBranchID,
		Roots:           []string{},
	}

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to marshal initial state: %v", err)
		http.Error(w, fmt.Sprintf("Failed to prepare initial state: %v", err), http.StatusInternalServerError)
		return
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	// Handle streaming response from LLM
	// Pass full history to streamLLMResponse for LLM
	if err := streamLLMResponse(db, initialState, sseW, userMessageID, modelToUse, true, time.Now(), historyContext); err != nil {
		http.Error(w, fmt.Sprintf("Error streaming LLM response: %v", err), http.StatusInternalServerError)
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
		http.Error(w, "Session ID is required", http.StatusBadRequest)
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
			http.Error(w, "Session not found", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		}
		return
	}
	systemPrompt := session.SystemPrompt
	primaryBranchID := session.PrimaryBranchID // Get primary branch ID from session

	// Find the last message in the current primary branch to update its chosen_next_id
	var lastMessageID int
	var parentMessageID *int // Declare as pointer to int
	var modelToUse string    // Declare modelToUse here

	lastMessageIDFromDB, lastMessageModelFromDB, err := GetLastMessageInBranch(db, sessionId, primaryBranchID)
	if err != nil {
		if err == sql.ErrNoRows {
			parentMessageID = nil
			lastMessageID = 0
			modelToUse = requestBody.Model // Use request body model for new branch
			if modelToUse == "" {
				modelToUse = DefaultGeminiModel // Fallback to default
			}
		} else {
			log.Printf("chatMessage: Failed to get last message ID for session %s and branch %s: %v", sessionId, primaryBranchID, err)
			http.Error(w, fmt.Sprintf("Failed to get last message: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		lastMessageID = lastMessageIDFromDB
		parentMessageID = &lastMessageID

		modelToUse = requestBody.Model
		if modelToUse == "" {
			modelToUse = lastMessageModelFromDB // Use the model of the last message in the branch
			if modelToUse == "" {
				modelToUse = DefaultGeminiModel // Fallback to default
			}
		}
	}

	userMessage := requestBody.Message

	// Add new message to the primary branch
	userMessageID, err := AddMessageToSession(r.Context(), db, Message{
		SessionID:       sessionId,
		BranchID:        primaryBranchID,
		ParentMessageID: parentMessageID,
		ChosenNextID:    nil,
		Role:            RoleUser,
		Text:            userMessage,
		Type:            TypeText,
		Attachments:     requestBody.Attachments,
		CumulTokenCount: nil,
		Model:           modelToUse,
	})
	if err != nil {
		log.Printf("chatMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Update the chosen_next_id of the last message in the primary branch
	if lastMessageID != 0 {
		if err := UpdateMessageChosenNextID(db, lastMessageID, &userMessageID); err != nil {
			log.Printf("chatMessage: Failed to update chosen_next_id for message %d: %v", lastMessageID, err)
			// Non-fatal error, continue with response
		}
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

	// Send acknowledgement for user message ID to frontend
	sseW.sendServerEvent(EventAcknowledge, fmt.Sprintf("%d", userMessageID))

	// Retrieve session history from DB for Gemini API (full context)
	fullFrontendHistoryForGemini, err := GetSessionHistoryContext(db, sessionId, primaryBranchID)
	if err != nil {
		log.Printf("chatMessage: Failed to retrieve full session history for Gemini: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve full session history for Gemini: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistoryForInitialState, err := GetSessionHistoryPaginated(db, sessionId, primaryBranchID, 0, requestBody.FetchLimit)
	if err != nil {
		log.Printf("chatMessage: Failed to retrieve paginated session history for frontend: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve paginated session history for frontend: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistoryForInitialState, // Use paginated history for frontend
		SystemPrompt:    systemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: primaryBranchID,
		Roots:           session.Roots,
	}

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		log.Printf("chatMessage: Failed to marshal initial state: %v", err)
		http.Error(w, fmt.Sprintf("Failed to prepare initial state: %v", err), http.StatusInternalServerError)
		return
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	if err := streamLLMResponse(db, initialState, sseW, userMessageID, modelToUse, false, time.Now(), fullFrontendHistoryForGemini); err != nil {
		log.Printf("chatMessage: Error streaming Gemini response: %v", err)
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
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
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Check if session exists
	exists, err := SessionExists(db, sessionId)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check session existence: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session, err := GetSession(db, sessionId)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse pagination parameters
	beforeMessageIDStr := r.URL.Query().Get("beforeMessageId")
	fetchLimitStr := r.URL.Query().Get("fetchLimit")

	beforeMessageID := 0 // Default to 0, meaning fetch from the latest
	if beforeMessageIDStr != "" {
		parsedID, err := strconv.Atoi(beforeMessageIDStr)
		if err != nil {
			log.Printf("loadChatSession: Invalid beforeMessageId: %v", err)
			http.Error(w, "Invalid beforeMessageId parameter", http.StatusBadRequest)
			return
		}
		beforeMessageID = parsedID
	}

	fetchLimit := math.MaxInt // Default fetch limit
	if fetchLimitStr != "" {
		parsedLimit, err := strconv.Atoi(fetchLimitStr)
		if err != nil {
			log.Printf("loadChatSession: Invalid fetchLimit: %v", err)
			http.Error(w, "Invalid fetchLimit parameter", http.StatusBadRequest)
			return
		}
		fetchLimit = parsedLimit
	}

	// Use the primary_branch_id from the session to load history with pagination
	history, err := GetSessionHistoryPaginated(db, sessionId, session.PrimaryBranchID, beforeMessageID, fetchLimit)
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
		SessionId:       sessionId,
		History:         history,
		SystemPrompt:    session.SystemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: session.PrimaryBranchID,
		Roots:           session.Roots,
	}

	branch, err := GetBranch(db, session.PrimaryBranchID)
	if err != nil {
		log.Printf("loadChatSession: Failed to get branch %s: %v", session.PrimaryBranchID, err)
		http.Error(w, fmt.Sprintf("Failed to get branch: %v", err), http.StatusInternalServerError)
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
				log.Printf("loadChatSession: Failed to marshal initial state with elapsed time: %v", err)
				http.Error(w, "Failed to prepare initial state", http.StatusInternalServerError)
				return
			}
			sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

			// Keep the connection open until client disconnects.
			// sseW will get any broadcasted messages over the course.
			<-r.Context().Done()
		} else {
			initialStateJSON, err := json.Marshal(initialState)
			if err != nil {
				log.Printf("loadChatSession: Failed to marshal initial state: %v", err)
				http.Error(w, "Failed to prepare initial state", http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("Failed to retrieve sessions: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, wsWithSessions)
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
		http.Error(w, "Session ID is required", http.StatusBadRequest)
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
	updatedRole, updatedType, updatedParentMessageID, updatedBranchID, err := GetMessageDetails(db, requestBody.UpdatedMessageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Updated message not found", http.StatusNotFound)
		} else {
			log.Printf("createBranchHandler: Failed to get updated message details: %v", err)
			http.Error(w, fmt.Sprintf("Failed to get updated message details: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Validate that the updated message is a user message of type 'text'
	if updatedRole != RoleUser || updatedType != TypeText {
		http.Error(w, "Branching is only allowed from user messages of type 'text'.", http.StatusBadRequest)
		return
	}

	// Validate that the updated message is not the first message of the session
	if !updatedParentMessageID.Valid {
		http.Error(w, "Branching is not allowed from the first message of the session.", http.StatusBadRequest)
		return
	}

	newBranchID := generateID()

	// The branch_from_message_id for the new branch is the parent of the updatedMessageID
	branchFromMessageID := int(updatedParentMessageID.Int64)

	// Create the new branch in the branches table
	// Pass the updatedBranchID as a pointer, and branchFromMessageID as a pointer
	if _, err := CreateBranch(db, newBranchID, sessionId, &updatedBranchID, &branchFromMessageID); err != nil {
		log.Printf("createBranchHandler: Failed to create new branch in branches table: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create new branch: %v", err), http.StatusInternalServerError)
		return
	}

	// Create the new message in the new branch, with updatedMessageID as its parent
	newMessageID, err := AddMessageToSession(r.Context(), db, Message{
		SessionID:       sessionId,
		BranchID:        newBranchID,
		ParentMessageID: &branchFromMessageID,
		ChosenNextID:    nil,
		Role:            RoleUser,
		Text:            requestBody.NewMessageText,
		Type:            TypeText,
		Attachments:     nil,
		CumulTokenCount: nil,
		Model:           "", // Model will be inferred or set later
	})
	if err != nil {
		log.Printf("createBranchHandler: Failed to add new message to new branch: %v", err)
		http.Error(w, fmt.Sprintf("Failed to add new message: %v", err), http.StatusInternalServerError)
		return
	}

	// Update the chosen_next_id of the branch_from_message_id to point to the new message
	if err := UpdateMessageChosenNextID(db, branchFromMessageID, &newMessageID); err != nil {
		log.Printf("createBranchHandler: Failed to update chosen_next_id for branch_from_message_id %d: %v", branchFromMessageID, err)
		// Non-fatal, but log it
	}

	// Set the new branch as the primary branch for the session
	if err := UpdateSessionPrimaryBranchID(db, sessionId, newBranchID); err != nil {
		log.Printf("createBranchHandler: Failed to set new branch as primary for session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to set primary branch: %v", err), http.StatusInternalServerError)
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
		http.Error(w, "Session ID is required", http.StatusBadRequest)
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
		log.Printf("switchBranchHandler: Failed to get session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}
	oldPrimaryBranchID := session.PrimaryBranchID

	// Update the session's primary branch ID
	if err := UpdateSessionPrimaryBranchID(db, sessionId, requestBody.NewPrimaryBranchID); err != nil {
		log.Printf("switchBranchHandler: Failed to switch primary branch for session %s to %s: %v", sessionId, requestBody.NewPrimaryBranchID, err)
		http.Error(w, fmt.Sprintf("Failed to set primary branch: %v", err), http.StatusInternalServerError)
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
			lastMessageID, _, err := GetLastMessageInBranch(db, sessionId, oldPrimaryBranchID)
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
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	if err := DeleteSession(db, sessionId); err != nil {
		log.Printf("deleteSession: Failed to delete session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Session deleted successfully"})
}

// Helper function to convert FrontendMessage to Content for Gemini API
func convertFrontendMessagesToContent(db *sql.DB, frontendMessages []FrontendMessage) []Content {
	var contents []Content
	// Apply curation rules before converting to Content
	curatedMessages := applyCurationRules(frontendMessages)

	for _, fm := range curatedMessages {
		var parts []Part
		// Add text part if present
		if len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			parts = append(parts, Part{Text: fm.Parts[0].Text})
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
			parts = append(parts, Part{FunctionCall: fm.Parts[0].FunctionCall})
		} else if fm.Type == TypeFunctionResponse && len(fm.Parts) > 0 && fm.Parts[0].FunctionResponse != nil {
			parts = append(parts, Part{FunctionResponse: fm.Parts[0].FunctionResponse})
		} else if fm.Type == TypeSystemPrompt && len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			// System_prompt should expand to *two* `Content`s
			contents = append(contents,
				Content{
					Role: RoleModel,
					Parts: []Part{
						{FunctionCall: &FunctionCall{Name: "new_system_prompt", Args: map[string]interface{}{}}},
					},
				},
				Content{
					Role: RoleUser,
					Parts: []Part{
						{FunctionResponse: &FunctionResponse{
							Name:     "new_system_prompt",
							Response: map[string]interface{}{"prompt": fm.Parts[0].Text},
						}},
					},
				},
			)
			continue
		}

		// If parts is still empty, add an empty text part to satisfy Gemini API requirements
		if len(parts) == 0 {
			parts = append(parts, Part{Text: ""})
		}

		contents = append(contents, Content{
			Role:  fm.Role,
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
		http.Error(w, "Session ID and Branch ID are required", http.StatusBadRequest)
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
		log.Printf("confirmBranchHandler: Failed to clear pending_confirmation for branch %s: %v", branchId, err)
		http.Error(w, fmt.Sprintf("Failed to clear confirmation status: %v", err), http.StatusInternalServerError)
		return
	}

	// Get session and branch details
	session, err := GetSession(db, sessionId)
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to get session %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}

	// If the confirmed branch is not the primary branch, switch to it
	if session.PrimaryBranchID != branchId {
		if err := UpdateSessionPrimaryBranchID(db, sessionId, branchId); err != nil {
			log.Printf("confirmBranchHandler: Failed to switch primary branch to %s: %v", branchId, err)
			http.Error(w, fmt.Sprintf("Failed to switch primary branch: %v", err), http.StatusInternalServerError)
			return
		}
		handleOldPrimaryBranchChosenNextID(db, sessionId, session.PrimaryBranchID, branchId)
		handleNewPrimaryBranchChosenNextID(db, branchId)
	}

	// Find the last message in the current primary branch (which should be the function_call that triggered confirmation)
	lastMessageIDFromDB, lastMessageModelFromDB, err := GetLastMessageInBranch(db, sessionId, branchId)
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to get last message ID for session %s and branch %s: %v", sessionId, branchId, err)
		http.Error(w, fmt.Sprintf("Failed to get last message: %v", err), http.StatusInternalServerError)
		return
	}

	// Get the full message details for the last message (the function call)
	lastMessage, err := GetMessageByID(db, lastMessageIDFromDB)
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to get last message details for ID %d: %v", lastMessageIDFromDB, err)
		http.Error(w, fmt.Sprintf("Failed to get last message details: %v", err), http.StatusInternalServerError)
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
			log.Printf("confirmBranchHandler: Failed to marshal denial function response: %v", err)
			http.Error(w, fmt.Sprintf("Failed to process denial: %v", err), http.StatusInternalServerError)
			return
		}

		// Add the function response message to the session
		denialResponseID, err := AddMessageToSession(r.Context(), db, Message{
			SessionID:       sessionId,
			BranchID:        branchId,
			ParentMessageID: &lastMessageIDFromDB,
			ChosenNextID:    nil,
			Role:            RoleUser, // Function responses are from the user's perspective
			Text:            string(frJson),
			Type:            TypeFunctionResponse,
			Attachments:     nil,
			CumulTokenCount: nil,
			Model:           lastMessageModelFromDB,
		})
		if err != nil {
			log.Printf("confirmBranchHandler: Failed to save denial function response message: %v", err)
			http.Error(w, fmt.Sprintf("Failed to save denial response: %v", err), http.StatusInternalServerError)
			return
		}

		// Update the chosen_next_id of the function call message to point to the denial response
		if err := UpdateMessageChosenNextID(db, lastMessageIDFromDB, &denialResponseID); err != nil {
			log.Printf("confirmBranchHandler: Failed to update chosen_next_id for function call message %d after denial: %v", lastMessageIDFromDB, err)
			// Non-fatal, but log it
		}

		// Send EventFunctionReply to frontend
		sseW := newSseWriter(sessionId, w, r)
		if sseW == nil {
			return
		}
		addSseWriter(sessionId, sseW)
		defer removeSseWriter(sessionId, sseW)

		denialResponseMapJson, err := json.Marshal(denialResponseMap)
		if err != nil {
			log.Printf("confirmBranchHandler: Failed to marshal denial response map for SSE: %v", err)
			denialResponseMapJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
		}
		formattedData := fmt.Sprintf("%d\n%s\n%s", denialResponseID, functionName, string(denialResponseMapJson))
		sseW.sendServerEvent(EventFunctionReply, formattedData)

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
			log.Printf("confirmBranchHandler: Failed to unmarshal function call from message %d: %v", lastMessageIDFromDB, err)
			http.Error(w, fmt.Sprintf("Failed to parse function call: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		log.Printf("confirmBranchHandler: Last message %d is not a function call (type: %s)", lastMessageIDFromDB, lastMessage.Type)
		http.Error(w, "Last message is not a function call, cannot confirm", http.StatusBadRequest)
		return
	}

	// If modifiedData is provided, update the function call arguments
	if requestBody.ModifiedData != nil {
		for k, v := range requestBody.ModifiedData {
			fc.Args[k] = v
		}
	}

	// Re-execute the tool function with confirmationReceived = true
	functionResponseValue, err := CallToolFunction(r.Context(), fc, ToolHandlerParams{ModelName: lastMessageModelFromDB, SessionId: sessionId, ConfirmationReceived: true})
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
	fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
	frJson, err := json.Marshal(fr)
	if err != nil { // Check error from json.Marshal(fr)
		log.Printf("confirmBranchHandler: Failed to marshal function response for frontend: %v", err)
		// If marshaling fails, create a basic error JSON
		frJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
	}

	// Add the function response message to the session
	// Note: cumulTokenCount is not updated here, as it's handled by streamLLMResponse
	functionResponseID, err := AddMessageToSession(r.Context(), db, Message{
		SessionID:       sessionId,
		BranchID:        branchId,
		ParentMessageID: &lastMessageIDFromDB,
		ChosenNextID:    nil,
		Role:            RoleUser, // Function responses are from the user's perspective
		Text:            string(frJson),
		Type:            TypeFunctionResponse,
		Attachments:     nil,
		CumulTokenCount: nil,
		Model:           lastMessageModelFromDB,
	})
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to save function response message after confirmation: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save function response: %v", err), http.StatusInternalServerError)
		return
	}

	// Update the chosen_next_id of the function call message to point to the response
	if err := UpdateMessageChosenNextID(db, lastMessageIDFromDB, &functionResponseID); err != nil {
		log.Printf("confirmBranchHandler: Failed to update chosen_next_id for function call message %d: %v", lastMessageIDFromDB, err)
		// Non-fatal, but log it
	}

	// Send EventFunctionReply to frontend
	sseW := newSseWriter(sessionId, w, r)
	if sseW == nil {
		return
	}
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	functionResponseValueJson, err := json.Marshal(functionResponseValue)
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to marshal function response value for SSE: %v", err)
		functionResponseValueJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
	}
	formattedData := fmt.Sprintf("%d\n%s\n%s", functionResponseID, fc.Name, string(functionResponseValueJson))
	sseW.sendServerEvent(EventFunctionReply, formattedData)

	// Retrieve session history from DB for Gemini API (full context)
	fullFrontendHistoryForGemini, err := GetSessionHistoryContext(db, sessionId, branchId)
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to retrieve full session history for Gemini after confirmation: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve full session history for Gemini: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare initial state for streaming (only for passing session details, not for sending EventInitialState)
	initialState := InitialState{
		SessionId:           sessionId,
		History:             []FrontendMessage{}, // History will be streamed
		SystemPrompt:        session.SystemPrompt,
		WorkspaceID:         session.WorkspaceID,
		PrimaryBranchID:     branchId,
		Roots:               session.Roots,
		PendingConfirmation: "", // Clear pending confirmation in initial state
	}

	// Resume streaming from the point after the function response
	if err := streamLLMResponse(db, initialState, sseW, functionResponseID, lastMessageModelFromDB, false, time.Now(), fullFrontendHistoryForGemini); err != nil {
		log.Printf("confirmBranchHandler: Error streaming Gemini response after confirmation: %v", err)
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
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
		// If current is user text and next is user text (ignoring thoughts/errors in between)
		if currentMsg.Role == RoleUser && currentMsg.Type == TypeText {
			nextUserTextIndex := -1
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Type == TypeThought {
					continue // Ignore thoughts and errors for continuity
				}
				if messages[j].Role == RoleUser && messages[j].Type == TypeText {
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
		if currentMsg.Role == RoleModel && currentMsg.Type == TypeFunctionCall {
			foundResponse := false
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Type == TypeThought {
					continue // Ignore thoughts and errors for continuity
				}
				if messages[j].Role == RoleUser && messages[j].Type == TypeFunctionResponse {
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
