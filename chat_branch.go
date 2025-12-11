package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// createBranchHandler creates a new branch from a given parent message.
func createBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
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

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	var err error
	if isRetry && requestBody.NewMessageText == "" {
		err = RetryBranch(r.Context(), db, models, ga, tools, ew, sessionId, requestBody.UpdatedMessageID)
	} else {
		err = CreateBranch(r.Context(), db, models, ga, tools, ew, sessionId, requestBody.UpdatedMessageID, requestBody.NewMessageText)
	}
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create branch")
	}
}

func RetryBranch(
	ctx context.Context, db *sql.DB, models *llm.Models, ga *llm.GeminiAuth, tools *tool.Tools,
	ew EventWriter, sessionId string, updatedMessageId int,
) error {
	// For retry, get the original message text and attachments if not provided
	originalMessage, err := database.GetMessageByID(db, updatedMessageId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("message not found")
		} else {
			return fmt.Errorf("failed to get original message: %w", err)
		}
	}

	return CreateBranch(ctx, db, models, ga, tools, ew, sessionId, updatedMessageId, originalMessage.Text)
}

func CreateBranch(
	ctx context.Context, db *sql.DB, models *llm.Models, ga *llm.GeminiAuth, tools *tool.Tools,
	ew EventWriter, sessionId string, updatedMessageID int, newMessageText string,
) error {
	// Get the updated message's role, type, parent_message_id, and branch_id to validate branching and create new branch
	updatedType, updatedParentMessageID, updatedBranchID, err := database.GetMessageDetails(db, updatedMessageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("updated message not found")
		} else {
			return fmt.Errorf("failed to get updated message details: %w", err)
		}
	}

	// Validate that the updated message is a user message of type 'text'
	if updatedType != TypeUserText {
		return badRequestError("branching is only allowed from user messages of type 'text'.")
	}

	// Get session details first (common for both paths)
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get session %s: %w", sessionId, err)
	}

	// Check if this is an attempt to edit the first message
	currentChosenFirstID, err := database.GetSessionChosenFirstID(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get session chosen_first_id for session %s: %w", sessionId, err)
	}

	// Handle first message editing if the message being updated is the current first message
	isFirstMessageEdit := currentChosenFirstID != nil && *currentChosenFirstID == updatedMessageID

	// Common variables that will be used by both paths
	var newBranchID string
	var newMessageID int
	var frontendHistoryForInitialState []FrontendMessage

	if isFirstMessageEdit {
		// First message editing logic

		// Create a new branch for the edited first message
		newBranchID = database.GenerateID()
		if _, err := database.CreateBranch(db, newBranchID, sessionId, nil, nil); err != nil {
			return fmt.Errorf("failed to create new branch for first message edit: %w", err)
		}

		// Set the new branch as the primary branch for the session
		if err := database.UpdateSessionPrimaryBranchID(db, sessionId, newBranchID); err != nil {
			return fmt.Errorf("failed to set new branch as primary for session %s: %w", sessionId, err)
		}

		// Validate that we're editing the current first message
		if currentChosenFirstID == nil || *currentChosenFirstID != updatedMessageID {
			return fmt.Errorf("can only edit the current first message of the session")
		}

		// Get the original message to preserve its properties
		originalMessage, err := database.GetMessageByID(db, updatedMessageID)
		if err != nil {
			return fmt.Errorf("failed to get original message %d: %w", updatedMessageID, err)
		}

		// Create the new first message in the new branch
		newMessageID, err = database.AddMessageToSession(ctx, db, Message{
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
			return fmt.Errorf("failed to create new first message: %w", err)
		}

		// Update the session's chosen_first_id to point to the new message
		if err := database.UpdateSessionChosenFirstID(db, sessionId, &newMessageID); err != nil {
			return fmt.Errorf("failed to update session chosen_first_id: %w", err)
		}

		// Retrieve session history for frontend InitialState
		frontendHistoryForInitialState, err = database.GetSessionHistoryPaginated(db, sessionId, newBranchID, 0, 20)
		if err != nil {
			return fmt.Errorf("failed to retrieve paginated session history for frontend: %w", err)
		}
	} else {
		// Normal branching logic for non-first messages

		// For any other message that has no parent (old first messages that are no longer active), reject
		if !updatedParentMessageID.Valid {
			return badRequestError("cannot branch from a message that has no parent message")
		}

		newBranchID = database.GenerateID()
		branchFromMessageID := int(updatedParentMessageID.Int64)

		// Create the new branch in the branches table
		if _, err := database.CreateBranch(db, newBranchID, sessionId, &updatedBranchID, &branchFromMessageID); err != nil {
			return fmt.Errorf("failed to create new branch in branches table: %w", err)
		}

		// Get current generation and original message
		_, currentGeneration, err := database.GetLatestSessionEnv(db, sessionId)
		if err != nil {
			return fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
		}

		originalMessage, err := database.GetMessageByID(db, updatedMessageID)
		if err != nil {
			return fmt.Errorf("failed to get original message %d: %w", updatedMessageID, err)
		}

		// Create the new message in the new branch
		newMessageID, err = database.AddMessageToSession(ctx, db, Message{
			SessionID:       sessionId,
			BranchID:        newBranchID,
			ParentMessageID: &branchFromMessageID,
			ChosenNextID:    nil,
			Text:            newMessageText,
			Type:            TypeUserText,
			Attachments:     originalMessage.Attachments,
			CumulTokenCount: nil,
			Model:           originalMessage.Model,
			Generation:      currentGeneration,
		})
		if err != nil {
			return fmt.Errorf("failed to add new message to new branch: %w", err)
		}

		// Update the chosen_next_id of the branch_from_message_id to point to the new message
		if err := database.UpdateMessageChosenNextID(db, branchFromMessageID, &newMessageID); err != nil {
			log.Printf("createBranchHandler: Failed to update chosen_next_id for branch_from_message_id %d: %v", branchFromMessageID, err)
			// Non-fatal, but log it
		}

		// Set the new branch as the primary branch for the session
		if err := database.UpdateSessionPrimaryBranchID(db, sessionId, newBranchID); err != nil {
			return fmt.Errorf("failed to set new branch as primary for session %s: %w", sessionId, err)
		}

		// Create frontend history with the new message
		newMessageAsFrontendMessage, err := database.GetMessageByID(db, newMessageID)
		if err != nil {
			return fmt.Errorf("failed to get new message %d for initial state: %w", newMessageID, err)
		}

		fm, _, err := database.CreateFrontendMessage(*newMessageAsFrontendMessage, sql.NullString{}, nil, false, false, false)
		if err != nil {
			return fmt.Errorf("failed to convert new message %d to frontend message: %w", newMessageID, err)
		}
		frontendHistoryForInitialState = []FrontendMessage{fm}
	}

	// Update last_updated_at for the session (common for both paths)
	if err := database.UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for session %s after branch operation: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	// Common streaming logic for both paths
	ew.Acquire()
	defer ew.Release()

	// Retrieve session history for LLM (full context) - common for both paths
	fullFrontendHistoryForLLM, err := database.GetSessionHistoryContext(db, sessionId, newBranchID)
	if err != nil {
		return fmt.Errorf("failed to retrieve full session history for LLM: %w", err)
	}

	// Get latest session environment - common for both paths
	roots, _, err := database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
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
	ew.Send(EventWorkspaceHint, session.WorkspaceID)

	// Send initial state - common for both paths
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("failed to marshal initial state: %w", err)
	}
	ew.Send(EventInitialState, string(initialStateJSON))

	// Send acknowledgement for the new message - common for both paths
	ew.Send(EventAcknowledge, fmt.Sprintf("%d", newMessageID))

	// Create a new message chain for streaming - common for both paths
	mc, err := database.NewMessageChain(ctx, db, sessionId, newBranchID)
	if err != nil {
		return fmt.Errorf("failed to create message chain for branch operation: %w", err)
	}

	// Stream LLM response - common for both paths
	if err := streamLLMResponse(db, models, ga, tools, initialState, ew, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		return fmt.Errorf("error streaming LLM response after branch operation: %w", err)
	}
	return nil
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

	if err := SwitchBranch(db, sessionId, requestBody.NewPrimaryBranchID); err != nil {
		sendInternalServerError(w, r, err, "Failed to switch primary branch")
		return
	}

	sendJSONResponse(w, map[string]string{
		"status":          "success",
		"primaryBranchId": requestBody.NewPrimaryBranchID,
	})
}

func SwitchBranch(db *sql.DB, sessionId string, newPrimaryBranchID string) error {
	// Get current session to retrieve old primary branch ID
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get session %s: %w", sessionId, err)
	}
	oldPrimaryBranchID := session.PrimaryBranchID

	// Update the session's primary branch ID
	if err := database.UpdateSessionPrimaryBranchID(db, sessionId, newPrimaryBranchID); err != nil {
		return fmt.Errorf("failed to switch primary branch for session %s to %s: %w", sessionId, newPrimaryBranchID, err)
	}

	handleOldPrimaryBranchChosenNextID(db, sessionId, oldPrimaryBranchID, newPrimaryBranchID)
	handleNewPrimaryBranchChosenNextID(db, newPrimaryBranchID)
	return nil
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
	models := getModels(w, r)
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

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	if err := ConfirmBranch(
		r.Context(), db, models, ga, tools,
		ew, sessionId, branchId, requestBody.Approved, requestBody.ModifiedData,
	); err != nil {
		sendInternalServerError(w, r, err, "Failed to confirm branch")
		return
	}
}

func ConfirmBranch(
	ctx context.Context, db *sql.DB, models *llm.Models, ga *llm.GeminiAuth, tools *tool.Tools,
	ew EventWriter, sessionId string, branchId string, approved bool, modifiedData map[string]interface{},
) error {
	// Clear pending_confirmation for the branch regardless of approval/denial
	if err := database.UpdateBranchPendingConfirmation(db, branchId, ""); err != nil { // Set to empty string to clear
		return fmt.Errorf("failed to clear confirmation status for branch %s: %w", branchId, err)
	}

	// Get session and branch details
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get session %s: %w", sessionId, err)
	}

	// If the confirmed branch is not the primary branch, switch to it
	if session.PrimaryBranchID != branchId {
		if err := database.UpdateSessionPrimaryBranchID(db, sessionId, branchId); err != nil {
			return fmt.Errorf("failed to switch primary branch for session %s to %s: %w", sessionId, branchId, err)
		}
		handleOldPrimaryBranchChosenNextID(db, sessionId, session.PrimaryBranchID, branchId)
		handleNewPrimaryBranchChosenNextID(db, branchId)
	}

	// Create a new message chain
	mc, err := database.NewMessageChain(ctx, db, sessionId, branchId)
	if err != nil {
		return fmt.Errorf("failed to create message chain for session %s and branch %s: %w", sessionId, branchId, err)
	}

	// Get the full message details for the last message (the function call)
	lastMessage, err := database.GetMessageByID(db, mc.LastMessageID)
	if err != nil {
		return fmt.Errorf("failed to get last message details for ID %d: %w", mc.LastMessageID, err)
	}

	if !approved {
		// User denied the confirmation
		log.Printf("confirmBranchHandler: User denied confirmation for session %s, branch %s", sessionId, branchId)

		// Construct the function response for denial
		functionName, _, _ := strings.Cut(lastMessage.Text, "\n")
		denialResponseMap := map[string]interface{}{"error": "User denied tool execution"}
		fr := FunctionResponse{Name: functionName, Response: denialResponseMap}
		frJson, err := json.Marshal(fr)
		if err != nil {
			return fmt.Errorf("failed to marshal denial function response: %w", err)
		}

		// Add the function response message to the session
		denialResponseMsg, err := mc.Add(ctx, db, Message{
			Text:            string(frJson),
			Type:            TypeFunctionResponse,
			Attachments:     nil,
			CumulTokenCount: nil,
		})
		if err != nil {
			return fmt.Errorf("failed to save denial function response message: %w", err)
		}

		// Send EventFunctionResponse to frontend
		ew.Acquire()
		defer ew.Release()

		denialResponseMapJson, err := json.Marshal(FunctionResponsePayload{Response: denialResponseMap})
		if err != nil {
			log.Printf("confirmBranchHandler: Failed to marshal denial response map for SSE: %v", err)
			denialResponseMapJson = fmt.Appendf(nil, `{"response": {"error": "%v"}}`, err)
		}
		formattedData := fmt.Sprintf("%d\n%s\n%s", denialResponseMsg.ID, functionName, string(denialResponseMapJson))
		ew.Send(EventFunctionResponse, formattedData)

		// Send EventComplete to signal the end of the pending state
		ew.Broadcast(EventComplete, "") // Signal completion
		return nil
	}

	// User approved the confirmation
	// Extract the original function call from the last message
	var fc FunctionCall
	if lastMessage.Type == TypeFunctionCall {
		if err := json.Unmarshal([]byte(lastMessage.Text), &fc); err != nil {
			return fmt.Errorf("failed to unmarshal function call from message %d: %w", lastMessage.ID, err)
		}
	} else {
		return badRequestError("last message %d is not a function call (type: %s)", lastMessage.ID, lastMessage.Type)
	}

	// If modifiedData is provided, update the function call arguments
	maps.Copy(fc.Args, modifiedData)

	// Re-execute the tool function with confirmationReceived = true
	toolResults, err := tools.Call(ctx, fc, tool.HandlerParams{
		ModelName:            lastMessage.Model,
		SessionId:            sessionId,
		BranchId:             branchId,
		ConfirmationReceived: true,
	})
	if err != nil {
		log.Printf("confirmBranchHandler: Error re-executing function %s after confirmation: %v", fc.Name, err)
		// If re-execution fails, send an error event and stop streaming
		ew.Acquire()
		defer ew.Release()
		ew.Broadcast(EventError, fmt.Sprintf("Tool re-execution failed: %v", err))
		return nil
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
	functionResponseMsg, err := mc.Add(ctx, db, Message{
		Text:        string(frJson),
		Type:        TypeFunctionResponse,
		Attachments: toolResults.Attachments,
	})
	if err != nil {
		return fmt.Errorf("failed to save function response message after confirmation: %w", err)
	}

	// Send EventFunctionResponse to frontend
	ew.Acquire()
	defer ew.Release()

	functionResponseValueJson, err := json.Marshal(FunctionResponsePayload{
		Response:    toolResults.Value,
		Attachments: toolResults.Attachments,
	})
	if err != nil {
		log.Printf("confirmBranchHandler: Failed to marshal function response value for SSE: %v", err)
		functionResponseValueJson = fmt.Appendf(nil, `{"response": {"error": "%v"}}`, err)
	}
	formattedData := fmt.Sprintf("%d\n%s\n%s", functionResponseMsg.ID, fc.Name, string(functionResponseValueJson))
	ew.Send(EventFunctionResponse, formattedData)

	// Send WorkspaceID hint to frontend
	ew.Send(EventWorkspaceHint, session.WorkspaceID)

	// Retrieve session history from DB for LLM (full context)
	fullFrontendHistoryForLLM, err := database.GetSessionHistoryContext(db, sessionId, branchId)
	if err != nil {
		return fmt.Errorf("failed to retrieve full session history for LLM after confirmation: %w", err)
	}

	var roots []string
	roots, mc.LastMessageGeneration, err = database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
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
	if err := streamLLMResponse(db, models, ga, tools, initialState, ew, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		return fmt.Errorf("error streaming LLM response after confirmation: %w", err)
	}
	return nil
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
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	branchId := vars["branchId"]

	if sessionId == "" || branchId == "" {
		sendBadRequestError(w, r, "Session ID and Branch ID are required")
		return
	}

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	if err := RetryErrorBranch(r.Context(), db, models, ga, tools, ew, sessionId, branchId); err != nil {
		sendInternalServerError(w, r, err, "Failed to retry error branch")
		return
	}
}

func RetryErrorBranch(
	ctx context.Context, db *sql.DB, models *llm.Models, ga *llm.GeminiAuth, tools *tool.Tools,
	ew EventWriter, sessionId string, branchId string,
) error {
	// Verify that the session exists
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("session not found")
		} else {
			return fmt.Errorf("failed to get session: %w", err)
		}
	}

	// Verify that the branch exists and belongs to the session
	branch, err := database.GetBranch(db, branchId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("branch not found")
		} else {
			return fmt.Errorf("failed to get branch: %w", err)
		}
	}

	if branch.SessionID != sessionId {
		return badRequestError("branch does not belong to the specified session")
	}

	// Set up SSE streaming first
	ew.Acquire()
	defer ew.Release()

	// First, retrieve the current session history before deleting error messages
	// Retrieve session history for LLM (full context)
	historyContext, err := database.GetSessionHistoryContext(db, sessionId, branchId)
	if err != nil {
		return fmt.Errorf("failed to retrieve session history for LLM: %w", err)
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistory, err := database.GetSessionHistoryPaginated(db, sessionId, branchId, 0, 20)
	if err != nil {
		return fmt.Errorf("failed to retrieve paginated session history for frontend: %w", err)
	}

	// If history context is empty, try to get context from parent branch
	if len(historyContext) == 0 {
		log.Printf("Branch %s is empty, trying parent branch", branchId)

		// Get the current branch to find its parent
		currentBranch, err := database.GetBranch(db, branchId)
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}

		// If parent branch exists, get history from parent
		if currentBranch.ParentBranchID != nil && *currentBranch.ParentBranchID != "" {
			log.Printf("Getting history context from parent branch %s", *currentBranch.ParentBranchID)
			historyContext, err = database.GetSessionHistoryContext(db, sessionId, *currentBranch.ParentBranchID)
			if err != nil {
				return fmt.Errorf("failed to retrieve session history from parent branch: %w", err)
			}

			// Also get frontend history from parent branch
			frontendHistory, err = database.GetSessionHistoryPaginated(db, sessionId, *currentBranch.ParentBranchID, 0, 20)
			if err != nil {
				return fmt.Errorf("failed to retrieve paginated session history from parent branch: %w", err)
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
			return fmt.Errorf("failed to delete error messages: %w", err)
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
		return fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
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
	ew.Send(EventWorkspaceHint, session.WorkspaceID)

	// Send initial state as a single SSE event
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("failed to marshal initial state: %w", err)
	}
	ew.Send(EventInitialState, string(initialStateJSON))

	// Create a new message chain for the branch
	mc, err := database.NewMessageChain(ctx, db, sessionId, branchId)
	if err != nil {
		return fmt.Errorf("failed to create message chain for retry: %w", err)
	}

	// Resume streaming from the cleaned up state
	if err := streamLLMResponse(db, models, ga, tools, initialState, ew, mc, false, time.Now(), filteredHistoryContext); err != nil {
		return fmt.Errorf("error streaming LLM response during retry: %w", err)
	}
	return nil
}
