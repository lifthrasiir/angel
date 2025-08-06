package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type InitialState struct {
	SessionId       string            `json:"sessionId"`
	History         []FrontendMessage `json:"history"`
	SystemPrompt    string            `json:"systemPrompt"`
	WorkspaceID     string            `json:"workspaceId"`
	PrimaryBranchID string            `json:"primaryBranchId"`
}

// New session and message handler
func newSessionAndMessage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("newSessionAndMessage", w, r) {
		return
	}

	var requestBody struct {
		Message      string           `json:"message"`
		SystemPrompt string           `json:"systemPrompt"`
		Attachments  []FileAttachment `json:"attachments"`
		WorkspaceID  string           `json:"workspaceId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "newSessionAndMessage") {
		return
	}

	sessionId := generateID()

	// Evaluate system prompt
	systemPrompt, err := GetEvaluatedSystemPrompt(requestBody.SystemPrompt)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to evaluate system prompt: %v", err)
		http.Error(w, fmt.Sprintf("Failed to evaluate system prompt: %v", err), http.StatusInternalServerError)
		return
	}

	// Create session with primary_branch_id
	primaryBranchID, err := CreateSession(db, sessionId, systemPrompt, requestBody.WorkspaceID)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to create new session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create new session: %v", err), http.StatusInternalServerError)
		return
	}

	userMessage := requestBody.Message

	log.Printf("newSessionAndMessage: Attempting to add user message to session %s with branch %s", sessionId, primaryBranchID)
	// Add message with branch_id, no parent_message_id, no chosen_next_id initially
	userMessageID, err := AddMessageToSession(db, sessionId, primaryBranchID, nil, nil, "user", userMessage, "text", requestBody.Attachments, nil)
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("newSessionAndMessage: Successfully added user message with ID %d to session %s", userMessageID, sessionId)

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

	// Retrieve session history from DB for Gemini API
	frontendHistory, err := GetSessionHistory(db, sessionId, primaryBranchID, true) // Pass primaryBranchID
	if err != nil {
		log.Printf("newSessionAndMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistory,
		SystemPrompt:    systemPrompt,
		WorkspaceID:     requestBody.WorkspaceID,
		PrimaryBranchID: primaryBranchID, // Include primary branch ID
	}

	// Handle streaming response from Gemini
	if err := streamGeminiResponse(db, initialState, sseW, userMessageID); err != nil {
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
		return
	}

}

// Chat message handler
func chatMessage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("chatMessage", w, r) {
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
	// Modified query to find the last message in the chain (chosen_next_id IS NULL)
	err = db.QueryRow("SELECT id FROM messages WHERE session_id = ? AND branch_id = ? AND chosen_next_id IS NULL ORDER BY created_at DESC LIMIT 1", sessionId, primaryBranchID).Scan(&lastMessageID)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("chatMessage: Failed to get last message ID for session %s and branch %s: %v", sessionId, primaryBranchID, err)
		http.Error(w, fmt.Sprintf("Failed to get last message: %v", err), http.StatusInternalServerError)
		return
	}

	userMessage := requestBody.Message

	// Add new message to the primary branch
	userMessageID, err := AddMessageToSession(db, sessionId, primaryBranchID, &lastMessageID, nil, "user", userMessage, "text", requestBody.Attachments, nil)
	if err != nil {
		log.Printf("chatMessage: Failed to save user message: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user message: %v", err), http.StatusInternalServerError)
		return
	}

	// Update the chosen_next_id of the last message in the primary branch
	if lastMessageID != 0 {
		if err := UpdateMessageChosenNextID(db, lastMessageID, &userMessageID); err != nil { // Changed userMessageID to &userMessageID
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

	// Retrieve session history from DB for Gemini API
	frontendHistory, err := GetSessionHistory(db, sessionId, primaryBranchID, true) // Pass primaryBranchID
	if err != nil {
		log.Printf("chatMessage: Failed to retrieve session history: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve session history: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistory,
		SystemPrompt:    systemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: primaryBranchID, // Include primary branch ID
	}

	if err := streamGeminiResponse(db, initialState, sseW, userMessageID); err != nil {
		log.Printf("chatMessage: Error streaming Gemini response: %v", err)
		http.Error(w, fmt.Sprintf("Error streaming Gemini response: %v", err), http.StatusInternalServerError)
		return
	}
}

// New endpoint to load chat session history
func loadChatSession(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("loadChatSession", w, r) {
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

	// Use the primary_branch_id from the session to load history
	history, err := GetSessionHistory(db, sessionId, session.PrimaryBranchID, false)
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
		PrimaryBranchID: session.PrimaryBranchID, // Include primary branch ID
	}

	// Check if it's an SSE request
	if r.Header.Get("Accept") == "text/event-stream" {
		sseW := newSseWriter(sessionId, w, r)
		if sseW == nil {
			return
		}

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
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("listSessionsByWorkspaceHandler", w, r) {
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
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("createBranchHandler", w, r) {
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
	var updatedRole, updatedType, updatedBranchID string
	var updatedParentMessageID sql.NullInt64
	err := db.QueryRow("SELECT role, type, parent_message_id, branch_id FROM messages WHERE id = ?", requestBody.UpdatedMessageID).Scan(&updatedRole, &updatedType, &updatedParentMessageID, &updatedBranchID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Updated message not found", http.StatusNotFound)
		} else {
			log.Printf("createBranchHandler: Failed to get updated message details: %v", err)
			http.Error(w, fmt.Sprintf("Failed to get updated message details: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Validate that the updated message is a user message of type 'text'
	if updatedRole != "user" || updatedType != "text" {
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
	newMessageID, err := AddMessageToSession(db, sessionId, newBranchID, &requestBody.UpdatedMessageID, nil, "user", requestBody.NewMessageText, "text", nil, nil)
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
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("switchBranchHandler", w, r) {
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

	// --- Handle chosen_next_id for the OLD primary branch ---
	if oldPrimaryBranchID != "" && oldPrimaryBranchID != requestBody.NewPrimaryBranchID {
		oldBranch, err := GetBranch(db, oldPrimaryBranchID)
		if err != nil {
			log.Printf("switchBranchHandler: Failed to get old branch %s: %v", oldPrimaryBranchID, err)
			// Non-fatal, continue
		}

		if oldBranch.BranchFromMessageID != nil {
			// This was a branched branch. Its parent's chosen_next_id needs to revert to its original next message.
			parentMsgID := *oldBranch.BranchFromMessageID

			// Find the message that originally followed parentMsgID in its own branch
			var originalNextMessageID sql.NullInt64
			err := db.QueryRow(`
				SELECT id FROM messages
				WHERE parent_message_id = ? AND branch_id = (SELECT branch_id FROM messages WHERE id = ?)
				ORDER BY created_at ASC LIMIT 1
			`, parentMsgID, parentMsgID).Scan(&originalNextMessageID)

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
			// Find the last message of the old primary branch
			var lastMessageID int
			err := db.QueryRow("SELECT id FROM messages WHERE session_id = ? AND branch_id = ? AND chosen_next_id IS NULL ORDER BY created_at DESC LIMIT 1", sessionId, oldPrimaryBranchID).Scan(&lastMessageID)
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

	// --- Handle chosen_next_id for the NEW primary branch ---
	// If the new primary branch is a branched branch, update its branch_from_message_id's chosen_next_id
	// to point to the first message of this new primary branch.
	newBranch, err := GetBranch(db, requestBody.NewPrimaryBranchID)
	if err != nil {
		log.Printf("switchBranchHandler: Failed to get new branch %s: %v", requestBody.NewPrimaryBranchID, err)
		// Non-fatal, continue
	} else if newBranch.BranchFromMessageID != nil {
		parentMsgID := *newBranch.BranchFromMessageID

		// Find the first message of the new primary branch that has parentMsgID as its parent
		var firstMessageOfNewBranchID int
		err := db.QueryRow(`
			SELECT id FROM messages
			WHERE parent_message_id = ? AND branch_id = ?
			ORDER BY created_at ASC LIMIT 1
		`, parentMsgID, requestBody.NewPrimaryBranchID).Scan(&firstMessageOfNewBranchID)

		if err != nil {
			log.Printf("switchBranchHandler: Failed to find first message of new branch %s: %v", requestBody.NewPrimaryBranchID, err)
			// Non-fatal, continue
		} else {
			if err := UpdateMessageChosenNextID(db, parentMsgID, &firstMessageOfNewBranchID); err != nil {
				log.Printf("switchBranchHandler: Failed to update chosen_next_id for message %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}
		}
	}

	sendJSONResponse(w, map[string]string{
		"status":          "success",
		"primaryBranchId": requestBody.NewPrimaryBranchID,
	})
}

// New endpoint to delete a chat session
func deleteSession(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("deleteSession", w, r) {
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
