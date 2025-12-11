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

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// createBranchHandler creates a new branch from a given parent message.
func createBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	registry := getModelsRegistry(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

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

	// Check if this is a retry request
	isRetry := r.URL.Query().Get("retry") == "1"

	// For retry, get the original message text and attachments if not provided
	if isRetry && requestBody.NewMessageText == "" {
		originalMessage, err := database.GetMessageByID(db, requestBody.UpdatedMessageID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				sendNotFoundError(w, r, "Message not found")
			} else {
				sendInternalServerError(w, r, err, "Failed to get original message")
			}
			return
		}
		requestBody.NewMessageText = originalMessage.Text
	}

	// Get the updated message's role, type, parent_message_id, and branch_id to validate branching and create new branch
	updatedType, updatedParentMessageID, updatedBranchID, err := database.GetMessageDetails(db, requestBody.UpdatedMessageID)
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

	// Get session details first (common for both paths)
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get session %s", sessionId))
		return
	}

	// Check if this is an attempt to edit the first message
	currentChosenFirstID, err := database.GetSessionChosenFirstID(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to get current chosen_first_id")
		return
	}

	// Handle first message editing if the message being updated is the current first message
	isFirstMessageEdit := currentChosenFirstID != nil && *currentChosenFirstID == requestBody.UpdatedMessageID

	// Common variables that will be used by both paths
	var newBranchID string
	var newMessageID int
	var frontendHistoryForInitialState []FrontendMessage

	if isFirstMessageEdit {
		// First message editing logic
		updatedMessageID := requestBody.UpdatedMessageID
		newMessageText := requestBody.NewMessageText

		// Create a new branch for the edited first message
		newBranchID = database.GenerateID()
		if _, err := database.CreateBranch(db, newBranchID, sessionId, nil, nil); err != nil {
			sendInternalServerError(w, r, err, "Failed to create new branch for first message edit")
			return
		}

		// Set the new branch as the primary branch for the session
		if err := database.UpdateSessionPrimaryBranchID(db, sessionId, newBranchID); err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to set new branch as primary for session %s", sessionId))
			return
		}

		// Validate that we're editing the current first message
		if currentChosenFirstID == nil || *currentChosenFirstID != updatedMessageID {
			sendBadRequestError(w, r, "Can only edit the current first message of the session")
			return
		}

		// Get the original message to preserve its properties
		originalMessage, err := database.GetMessageByID(db, updatedMessageID)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get original message %d", updatedMessageID))
			return
		}

		// Create the new first message in the new branch
		newMessageID, err = database.AddMessageToSession(r.Context(), db, Message{
			SessionID:       sessionId,
			BranchID:        newBranchID,
			ParentMessageID: nil,
			ChosenNextID:    nil,
			Text:            newMessageText,
			Type:            TypeUserText,
			Attachments:     originalMessage.Attachments,
			Model:           originalMessage.Model,
			Generation:      originalMessage.Generation,
		})
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to create new first message")
			return
		}

		// Update the session's chosen_first_id to point to the new message
		if err := database.UpdateSessionChosenFirstID(db, sessionId, &newMessageID); err != nil {
			sendInternalServerError(w, r, err, "Failed to update session chosen_first_id")
			return
		}

		// Retrieve session history for frontend InitialState
		frontendHistoryForInitialState, err = database.GetSessionHistoryPaginated(db, sessionId, newBranchID, 0, 20)
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to retrieve paginated session history for frontend")
			return
		}

	} else {
		// Normal branching logic for non-first messages

		// For any other message that has no parent (old first messages that are no longer active), reject
		if !updatedParentMessageID.Valid {
			sendBadRequestError(w, r, "Cannot edit a message that is not the current first message")
			return
		}

		newBranchID = database.GenerateID()
		branchFromMessageID := int(updatedParentMessageID.Int64)

		// Create the new branch in the branches table
		if _, err := database.CreateBranch(db, newBranchID, sessionId, &updatedBranchID, &branchFromMessageID); err != nil {
			sendInternalServerError(w, r, err, "Failed to create new branch in branches table")
			return
		}

		// Get current generation and original message
		_, currentGeneration, err := database.GetLatestSessionEnv(db, sessionId)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
			return
		}

		originalMessage, err := database.GetMessageByID(db, requestBody.UpdatedMessageID)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get original message %d: %v", requestBody.UpdatedMessageID, err))
			return
		}

		// Create the new message in the new branch
		newMessageID, err = database.AddMessageToSession(r.Context(), db, Message{
			SessionID:       sessionId,
			BranchID:        newBranchID,
			ParentMessageID: &branchFromMessageID,
			ChosenNextID:    nil,
			Text:            requestBody.NewMessageText,
			Type:            TypeUserText,
			Attachments:     originalMessage.Attachments,
			CumulTokenCount: nil,
			Model:           originalMessage.Model,
			Generation:      currentGeneration,
		})
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to add new message to new branch")
			return
		}

		// Update the chosen_next_id of the branch_from_message_id to point to the new message
		if err := database.UpdateMessageChosenNextID(db, branchFromMessageID, &newMessageID); err != nil {
			log.Printf("createBranchHandler: Failed to update chosen_next_id for branch_from_message_id %d: %v", branchFromMessageID, err)
			// Non-fatal, but log it
		}

		// Set the new branch as the primary branch for the session
		if err := database.UpdateSessionPrimaryBranchID(db, sessionId, newBranchID); err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to set new branch as primary for session %s", sessionId))
			return
		}

		// Create frontend history with the new message
		newMessageAsFrontendMessage, err := database.GetMessageByID(db, newMessageID)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get new message %d for initial state", newMessageID))
			return
		}

		fm, _, err := database.CreateFrontendMessage(*newMessageAsFrontendMessage, sql.NullString{}, nil, false, false, false)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to convert new message %d to frontend message: %v", newMessageID, err))
			return
		}
		frontendHistoryForInitialState = []FrontendMessage{fm}
	}

	// Update last_updated_at for the session (common for both paths)
	if err := database.UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for session %s after branch operation: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	// Common streaming logic for both paths
	sseW := newSseWriter(sessionId, w, r)
	if sseW == nil {
		return
	}
	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	// Retrieve session history for LLM (full context) - common for both paths
	fullFrontendHistoryForLLM, err := database.GetSessionHistoryContext(db, sessionId, newBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve full session history for LLM")
		return
	}

	// Get latest session environment - common for both paths
	roots, _, err := database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Prepare initial state for streaming - common for both paths
	var primaryBranchID string
	if isFirstMessageEdit {
		primaryBranchID = session.PrimaryBranchID // Use the original primary branch ID
	} else {
		primaryBranchID = newBranchID // Use the new branch ID
	}

	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistoryForInitialState,
		SystemPrompt:    session.SystemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: primaryBranchID,
		Roots:           roots,
	}

	// Send WorkspaceID hint to frontend
	sseW.sendServerEvent(EventWorkspaceHint, session.WorkspaceID)

	// Send initial state - common for both paths
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to marshal initial state")
		return
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	// Send acknowledgement for the new message - common for both paths
	sseW.sendServerEvent(EventAcknowledge, fmt.Sprintf("%d", newMessageID))

	// Create a new message chain for streaming - common for both paths
	mc, err := database.NewMessageChain(r.Context(), db, sessionId, newBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create message chain for branch operation")
		return
	}

	// Stream LLM response - common for both paths
	if err := streamLLMResponse(db, registry, ga, tools, initialState, sseW, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		sendInternalServerError(w, r, err, "Error streaming LLM response after branch operation")
		return
	}
}

// switchBranchHandler switches the primary branch of a session.
func switchBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

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
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get session %s", sessionId))
		return
	}
	oldPrimaryBranchID := session.PrimaryBranchID

	// Update the session's primary branch ID
	if err := database.UpdateSessionPrimaryBranchID(db, sessionId, requestBody.NewPrimaryBranchID); err != nil {
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
		oldBranch, err := database.GetBranch(db, oldPrimaryBranchID)
		if err != nil {
			log.Printf("switchBranchHandler: Failed to get old branch %s: %v", oldPrimaryBranchID, err)
			// Non-fatal, continue
		}

		if oldBranch.BranchFromMessageID != nil {
			// This was a branched branch. Its parent's chosen_next_id needs to revert to its original next message.
			parentMsgID := *oldBranch.BranchFromMessageID

			// Find the message that originally followed parentMsgID in its own branch
			originalNextMessageID, err := database.GetOriginalNextMessageID(db, parentMsgID, oldPrimaryBranchID)
			if err != nil && err != sql.ErrNoRows {
				log.Printf("switchBranchHandler: Failed to find original next message for %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}

			var chosenNextID *int
			if originalNextMessageID.Valid {
				val := int(originalNextMessageID.Int64)
				chosenNextID = &val
			}

			if err := database.UpdateMessageChosenNextID(db, parentMsgID, chosenNextID); err != nil {
				log.Printf("switchBranchHandler: Failed to update chosen_next_id for message %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}
		} else {
			// This was the initial branch. Its last message's chosen_next_id needs to revert to its original next message.
			lastMessageID, _, _, err := database.GetLastMessageInBranch(db, sessionId, oldPrimaryBranchID)
			if err != nil && err != sql.ErrNoRows {
				log.Printf("switchBranchHandler: Failed to get last message ID for old primary branch %s: %v", oldPrimaryBranchID, err)
				// Non-fatal, continue
			}

			if lastMessageID != 0 {
				// Find the message that originally followed lastMessageID in its own branch
				originalNextMessageID, err := database.GetOriginalNextMessageInBranch(db, lastMessageID, oldPrimaryBranchID)

				if err != nil {
					log.Printf("switchBranchHandler: Failed to find original next message for %d: %v", lastMessageID, err)
					// Non-fatal, continue
				}

				if err := database.UpdateMessageChosenNextID(db, lastMessageID, originalNextMessageID); err != nil {
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
	newBranch, err := database.GetBranch(db, newPrimaryBranchID)
	if err != nil {
		log.Printf("switchBranchHandler: Failed to get new branch %s: %v", newPrimaryBranchID, err)
		// Non-fatal, continue
	} else if newBranch.BranchFromMessageID != nil {
		parentMsgID := *newBranch.BranchFromMessageID

		// Find the first message of the new primary branch that has parentMsgID as its parent
		var firstMessageOfNewBranchID int
		firstMessageOfNewBranchID, err := database.GetFirstMessageOfBranch(db, parentMsgID, newPrimaryBranchID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				log.Printf("switchBranchHandler: No first message found for new branch %s. Skipping chosen_next_id update.", newPrimaryBranchID)
			} else {
				log.Printf("switchBranchHandler: Failed to find first message of new branch %s: %v", newPrimaryBranchID, err)
			}
			// Non-fatal, continue
		} else {
			if err := database.UpdateMessageChosenNextID(db, parentMsgID, &firstMessageOfNewBranchID); err != nil {
				log.Printf("switchBranchHandler: Failed to update chosen_next_id for message %d: %v", parentMsgID, err)
				// Non-fatal, continue
			}
		}
	}

	// Additionally, check if the new primary branch has a first message (parent_message_id IS NULL)
	// and update the session's chosen_first_id accordingly
	sessionID, err := database.GetBranchSessionID(db, newPrimaryBranchID)
	if err != nil {
		log.Printf("switchBranchHandler: Failed to get session ID for branch %s: %v", newPrimaryBranchID, err)
		// Non-fatal, continue
		return
	}

	// Find the first message of the new primary branch (parent_message_id IS NULL)
	firstMessageID, err := database.GetFirstMessageInBranch(db, sessionID, newPrimaryBranchID)

	if err != nil {
		log.Printf("switchBranchHandler: Failed to find first message for branch %s: %v", newPrimaryBranchID, err)
		// Non-fatal, continue
		return
	}

	// Update the session's chosen_first_id to point to this first message
	if firstMessageID != nil {
		if err := database.UpdateSessionChosenFirstID(db, sessionID, firstMessageID); err != nil {
			log.Printf("switchBranchHandler: Failed to update chosen_first_id for session %s: %v", sessionID, err)
			// Non-fatal, continue
		}
	}
}

// confirmBranchHandler handles the confirmation of a pending action on a branch.
func confirmBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	registry := getModelsRegistry(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

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
	if err := database.UpdateBranchPendingConfirmation(db, branchId, ""); err != nil { // Set to empty string to clear
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to clear confirmation status for branch %s", branchId))
		return
	}

	// Get session and branch details
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get session %s", sessionId))
		return
	}

	// If the confirmed branch is not the primary branch, switch to it
	if session.PrimaryBranchID != branchId {
		if err := database.UpdateSessionPrimaryBranchID(db, sessionId, branchId); err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to switch primary branch to %s", branchId))
			return
		}
		handleOldPrimaryBranchChosenNextID(db, sessionId, session.PrimaryBranchID, branchId)
		handleNewPrimaryBranchChosenNextID(db, branchId)
	}

	// Create a new message chain
	mc, err := database.NewMessageChain(r.Context(), db, sessionId, branchId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to create message chain for session %s and branch %s", sessionId, branchId))
		return
	}

	// Get the full message details for the last message (the function call)
	lastMessage, err := database.GetMessageByID(db, mc.LastMessageID)
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
	toolResults, err := tools.Call(r.Context(), fc, tool.HandlerParams{
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

	// Send WorkspaceID hint to frontend
	sseW.sendServerEvent(EventWorkspaceHint, session.WorkspaceID)

	// Retrieve session history from DB for LLM (full context)
	fullFrontendHistoryForLLM, err := database.GetSessionHistoryContext(db, sessionId, branchId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve full session history for LLM after confirmation")
		return
	}

	var roots []string
	roots, mc.LastMessageGeneration, err = database.GetLatestSessionEnv(db, sessionId)
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
	if err := streamLLMResponse(db, registry, ga, tools, initialState, sseW, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		sendInternalServerError(w, r, err, "Error streaming LLM response after confirmation")
		return
	}

	sendJSONResponse(w, map[string]string{"status": "approved", "message": "Confirmation approved and streaming resumed"})
}

// deleteErrorMessages deletes error messages from the end of a branch starting from the last message.
// It continues deleting messages backwards until it finds a non-error message.
func deleteErrorMessages(db *sql.DB, sessionID, branchID string) error {
	// Get the last message in the branch
	lastMessageID, _, _, err := database.GetLastMessageInBranch(db, sessionID, branchID)
	if err != nil {
		if err == sql.ErrNoRows {
			// No messages in branch, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to get last message in branch: %w", err)
	}

	if lastMessageID == 0 {
		// No messages in branch
		return nil
	}

	// Start from the last message and work backwards
	currentMessageID := lastMessageID
	deletedCount := 0

	for currentMessageID != 0 {
		// Get the current message details
		currentMessage, err := database.GetMessageByID(db, currentMessageID)
		if err != nil {
			return fmt.Errorf("failed to get message %d: %w", currentMessageID, err)
		}

		// Check if this is an error message
		if currentMessage.Type != TypeError && currentMessage.Type != TypeModelError {
			// Found a non-error message, stop deleting
			break
		}

		// This is an error message, delete it
		if err := database.DeleteMessage(db, currentMessageID); err != nil {
			return fmt.Errorf("failed to delete error message %d: %w", currentMessageID, err)
		}

		deletedCount++
		currentMessageID = *currentMessage.ParentMessageID
	}

	if deletedCount == 0 {
		return fmt.Errorf("no error messages found at the end of branch %s", branchID)
	}

	log.Printf("Deleted %d error messages from branch %s", deletedCount, branchID)
	return nil
}

// retryErrorBranchHandler handles retry-error requests for a branch by removing error messages and resuming streaming.
func retryErrorBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	registry := getModelsRegistry(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	branchId := vars["branchId"]

	if sessionId == "" || branchId == "" {
		sendBadRequestError(w, r, "Session ID and Branch ID are required")
		return
	}

	// Verify that the session exists
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendNotFoundError(w, r, "Session not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to get session")
		}
		return
	}

	// Verify that the branch exists and belongs to the session
	branch, err := database.GetBranch(db, branchId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendNotFoundError(w, r, "Branch not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to get branch")
		}
		return
	}

	if branch.SessionID != sessionId {
		sendBadRequestError(w, r, "Branch does not belong to the specified session")
		return
	}

	// Set up SSE streaming first
	sseW := newSseWriter(sessionId, w, r)
	if sseW == nil {
		return
	}

	addSseWriter(sessionId, sseW)
	defer removeSseWriter(sessionId, sseW)

	// First, retrieve the current session history before deleting error messages
	// Retrieve session history for LLM (full context)
	historyContext, err := database.GetSessionHistoryContext(db, sessionId, branchId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve session history for LLM")
		return
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistory, err := database.GetSessionHistoryPaginated(db, sessionId, branchId, 0, 20)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve paginated session history for frontend")
		return
	}

	// If history context is empty, try to get context from parent branch
	if len(historyContext) == 0 {
		log.Printf("Branch %s is empty, trying parent branch", branchId)

		// Get the current branch to find its parent
		currentBranch, err := database.GetBranch(db, branchId)
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to get current branch")
			return
		}

		// If parent branch exists, get history from parent
		if currentBranch.ParentBranchID != nil && *currentBranch.ParentBranchID != "" {
			log.Printf("Getting history context from parent branch %s", *currentBranch.ParentBranchID)
			historyContext, err = database.GetSessionHistoryContext(db, sessionId, *currentBranch.ParentBranchID)
			if err != nil {
				sendInternalServerError(w, r, err, "Failed to retrieve session history from parent branch")
				return
			}

			// Also get frontend history from parent branch
			frontendHistory, err = database.GetSessionHistoryPaginated(db, sessionId, *currentBranch.ParentBranchID, 0, 20)
			if err != nil {
				sendInternalServerError(w, r, err, "Failed to retrieve paginated session history from parent branch")
				return
			}
		}
	}

	// Now delete error messages from the end of the branch (if any)
	if err := deleteErrorMessages(db, sessionId, branchId); err != nil {
		// If no error messages were found, log but continue with retry
		// This allows retry even when there are no error messages (e.g., after user cancellation)
		if strings.Contains(err.Error(), "no error messages found") {
			log.Printf("No error messages found at end of branch %s, proceeding with retry anyway", branchId)
		} else {
			sendInternalServerError(w, r, err, "Failed to delete error messages")
			return
		}
	}

	// Remove error messages from the history context in memory (before LLM call)
	var filteredHistoryContext []FrontendMessage
	errorMessageCount := 0
	for i := len(historyContext) - 1; i >= 0; i-- {
		msg := historyContext[i]
		if msg.Type == TypeError || msg.Type == TypeModelError {
			errorMessageCount++
		} else {
			break // Stop at first non-error message
		}
	}

	// Remove the trailing error messages
	if errorMessageCount > 0 {
		filteredHistoryContext = historyContext[:len(historyContext)-errorMessageCount]
		log.Printf("Removed %d error messages from history context", errorMessageCount)
	} else {
		filteredHistoryContext = historyContext
	}

	// Update session last_updated_at
	if err := database.UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for session %s after retry: %v", sessionId, err)
		// Non-fatal error, continue
	}

	// Get latest session environment
	roots, _, err := database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get latest session environment for session %s", sessionId))
		return
	}

	// Prepare initial state for streaming
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistory,
		SystemPrompt:    session.SystemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: branchId,
		Roots:           roots,
	}

	// Send WorkspaceID hint to frontend
	sseW.sendServerEvent(EventWorkspaceHint, session.WorkspaceID)

	// Send initial state as a single SSE event
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to marshal initial state")
		return
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	// Create a new message chain for the branch
	mc, err := database.NewMessageChain(r.Context(), db, sessionId, branchId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create message chain for retry")
		return
	}

	// Resume streaming from the cleaned up state
	if err := streamLLMResponse(db, registry, ga, tools, initialState, sseW, mc, false, time.Now(), filteredHistoryContext); err != nil {
		sendInternalServerError(w, r, err, "Error streaming LLM response during retry")
		return
	}
}
