package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestBranchingLogic tests the branching functionality in sessions and messages.
func TestBranchingLogic(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Scenario 1: Linear message flow - chosen_next_id updates correctly
	t.Run("LinearMessageFlow", func(t *testing.T) {
		sessionId := generateID()                                                            // Generate session ID
		primaryBranchID, err := CreateSession(testDB, sessionId, "System prompt", "default") // Updated
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// Message 1
		msg1 := Message{SessionID: sessionId, BranchID: primaryBranchID, Role: "user", Text: "Message 1", Type: "text"}
		msg1ID, err := AddMessageToSession(context.Background(), testDB, msg1)
		if err != nil {
			t.Fatalf("Failed to add message 1: %v", err)
		}

		// Message 2
		msg2 := Message{SessionID: sessionId, BranchID: primaryBranchID, ParentMessageID: &msg1ID, Role: "model", Text: "Message 2", Type: "text"}
		msg2ID, err := AddMessageToSession(context.Background(), testDB, msg2)
		if err != nil {
			t.Fatalf("Failed to add message 2: %v", err)
		}
		// Update chosen_next_id for Message 1
		if err = UpdateMessageChosenNextID(testDB, msg1ID, &msg2ID); err != nil {
			t.Fatalf("Failed to update chosen_next_id for message 1: %v", err)
		}

		// Message 3
		msg3 := Message{SessionID: sessionId, BranchID: primaryBranchID, ParentMessageID: &msg2ID, Role: "user", Text: "Message 3", Type: "text"}
		msg3ID, err := AddMessageToSession(context.Background(), testDB, msg3)
		if err != nil {
			t.Fatalf("Failed to add message 3: %v", err)
		}
		// Update chosen_next_id for Message 2
		if err = UpdateMessageChosenNextID(testDB, msg2ID, &msg3ID); err != nil {
			t.Fatalf("Failed to update chosen_next_id for message 2: %v", err)
		}

		// Verify chosen_next_id for Message 1
		var chosenNextID1 sql.NullInt64
		querySingleRow(t, testDB, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msg1ID}, &chosenNextID1)
		if !chosenNextID1.Valid || chosenNextID1.Int64 != int64(msg2ID) {
			t.Errorf("Expected chosen_next_id for message 1 to be %d, got %v", msg2ID, chosenNextID1)
		}

		// Verify chosen_next_id for Message 2
		var chosenNextID2 sql.NullInt64
		querySingleRow(t, testDB, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msg2ID}, &chosenNextID2)
		if !chosenNextID2.Valid || chosenNextID2.Int64 != int64(msg3ID) {
			t.Errorf("Expected chosen_next_id for message 2 to be %d, got %v", msg3ID, chosenNextID2)
		}

		// Verify chosen_next_id for Message 3 (should be empty)
		var chosenNextID3 sql.NullInt64
		querySingleRow(t, testDB, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msg3ID}, &chosenNextID3)
		if chosenNextID3.Valid {
			t.Errorf("Expected chosen_next_id for message 3 to be empty, got %v", chosenNextID3.Int64)
		}
	})

	// Scenario 2: Branching - new session has correct parent_id and branch_id
	t.Run("BranchingNewSession", func(t *testing.T) {
		originalSessionId := generateID()
		originalPrimaryBranchID, err := CreateSession(testDB, originalSessionId, "Original system prompt", "default")
		if err != nil {
			t.Fatalf("Failed to create original session: %v", err)
		}

		// Add messages to the original session
		firstMsg := Message{SessionID: originalSessionId, BranchID: originalPrimaryBranchID, Role: "user", Text: "First message", Type: "text"}
		firstMsgID, err := AddMessageToSession(context.Background(), testDB, firstMsg)
		if err != nil {
			t.Fatalf("Failed to add first message: %v", err)
		}
		msgToBranchFrom := Message{SessionID: originalSessionId, BranchID: originalPrimaryBranchID, ParentMessageID: &firstMsgID, Role: "user", Text: "Message to branch from", Type: "text"}
		msgIDToBranchFrom, err := AddMessageToSession(context.Background(), testDB, msgToBranchFrom)
		if err != nil {
			t.Fatalf("Failed to add message to branch from: %v", err)
		}
		// Update chosen_next_id for the first message
		if err = UpdateMessageChosenNextID(testDB, firstMsgID, &msgIDToBranchFrom); err != nil {
			t.Fatalf("Failed to update chosen_next_id for first message: %v", err)
		}

		// Create a new branch using the handler
		payload := []byte(fmt.Sprintf(`{"updatedMessageId": %d, "newMessageText": "First message in new branch"}`, msgIDToBranchFrom))
		rr := testRequest(t, router, "POST", "/api/chat/"+originalSessionId+"/branch", payload, http.StatusOK)

		var response map[string]string
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		newBranchID := response["newBranchId"]
		if newBranchID == "" {
			t.Fatalf("newBranchId not found in response")
		}

		// Verify the new branch in the branches table
		var b Branch
		querySingleRow(t, testDB, "SELECT id, session_id, parent_branch_id, branch_from_message_id FROM branches WHERE id = ?", []interface{}{newBranchID}, &b.ID, &b.SessionID, &b.ParentBranchID, &b.BranchFromMessageID)
		if b.ID != newBranchID {
			t.Errorf("Expected branch ID %s, got %s", newBranchID, b.ID)
		}
		if b.SessionID != originalSessionId {
			t.Errorf("Expected session ID %s, got %s", originalSessionId, b.SessionID)
		}
		if *b.ParentBranchID != originalPrimaryBranchID {
			t.Errorf("Expected parent branch ID %s, got %s", originalPrimaryBranchID, *b.ParentBranchID)
		}
		if *b.BranchFromMessageID != firstMsgID {
			t.Errorf("Expected branch from message ID %d, got %d", firstMsgID, *b.BranchFromMessageID)
		}

		// Verify the first message in the new branch
		var firstMsgBranchID string
		var firstMsgParentID sql.NullInt64
		querySingleRow(t, testDB, "SELECT branch_id, parent_message_id FROM messages WHERE session_id = ? AND branch_id = ? ORDER BY created_at ASC LIMIT 1", []interface{}{originalSessionId, newBranchID}, &firstMsgBranchID, &firstMsgParentID)
		if firstMsgBranchID != newBranchID {
			t.Errorf("Expected first message branch ID %s, got %s", newBranchID, firstMsgBranchID)
		}
		if !firstMsgParentID.Valid || firstMsgParentID.Int64 != int64(firstMsgID) {
			t.Errorf("Expected first message parent ID %d, got %v", firstMsgID, firstMsgParentID)
		}

		// Verify that the original session's primary branch is updated to the new branch
		var updatedPrimaryBranchID string
		querySingleRow(t, testDB, "SELECT primary_branch_id FROM sessions WHERE id = ?", []interface{}{originalSessionId}, &updatedPrimaryBranchID)
		if updatedPrimaryBranchID != newBranchID {
			t.Errorf("Expected original session's primary branch ID to be %s, got %s", newBranchID, updatedPrimaryBranchID)
		}

		// Verify chosen_next_id for the message branched from
		var chosenNextID sql.NullInt64
		querySingleRow(t, testDB, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{firstMsgID}, &chosenNextID)
		if !chosenNextID.Valid {
			t.Errorf("Expected chosen_next_id for branched message to be valid, got NULL")
		}
		// Get the ID of the first message in the new branch to compare
		var firstMessageInNewBranchID int
		querySingleRow(t, testDB, "SELECT id FROM messages WHERE branch_id = ? ORDER BY created_at ASC LIMIT 1", []interface{}{newBranchID}, &firstMessageInNewBranchID)
		if chosenNextID.Int64 != int64(firstMessageInNewBranchID) {
			t.Errorf("Expected chosen_next_id for branched message to be %d, got %d", firstMessageInNewBranchID, chosenNextID.Int64)
		}
	})

	// Scenario 3: SetPrimaryBranchHandler
	t.Run("SetPrimaryBranchHandler", func(t *testing.T) {
		sessionId := generateID()
		originalPrimaryBranchID, err := CreateSession(testDB, sessionId, "Session for primary branch test", "default")
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// Create a new branch
		msgToBranchFrom := Message{SessionID: sessionId, BranchID: originalPrimaryBranchID, Role: "user", Text: "Message to branch from for primary branch test", Type: "text"}
		msgIDToBranchFrom, err := AddMessageToSession(context.Background(), testDB, msgToBranchFrom)
		if err != nil {
			t.Fatalf("Failed to add message to branch from for primary branch test: %v", err)
		}
		newBranchID := generateID()
		_, err = CreateBranch(testDB, newBranchID, sessionId, &originalPrimaryBranchID, &msgIDToBranchFrom) // Pass generated ID
		if err != nil {
			t.Fatalf("Failed to create new branch for primary branch test: %v", err)
		}

		// Set the new branch as primary
		payload := []byte(fmt.Sprintf(`{"newPrimaryBranchId": "%s"}`, newBranchID))
		rr := testRequest(t, router, "PUT", "/api/chat/"+sessionId+"/branch", payload, http.StatusOK)

		var response map[string]string
		rawResponse := rr.Body.Bytes()
		t.Logf("Raw response body: %s", rawResponse)
		err = json.Unmarshal(rawResponse, &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}
		if response["primaryBranchId"] != newBranchID {
			t.Errorf("expected primaryBranchId in response %s, got %s", newBranchID, response["primaryBranchId"])
		}

		// Verify in DB that the primary branch ID is updated
		var currentPrimaryBranchID string
		querySingleRow(t, testDB, "SELECT primary_branch_id FROM sessions WHERE id = ?", []interface{}{sessionId}, &currentPrimaryBranchID)
		if currentPrimaryBranchID != newBranchID {
			t.Errorf("Expected primary branch ID %s, got %s", newBranchID, currentPrimaryBranchID)
		}
	})

}

// TestNewSessionAndMessage_BranchIDConsistency tests that branch IDs are consistent when a new session is created.
func TestNewSessionAndMessage_BranchIDConsistency(t *testing.T) {
	router, testDB, _ := setupTest(t)

	t.Run("BranchIDConsistency", func(t *testing.T) {
		// Create the workspace before making the request
		CreateWorkspace(testDB, "testWsConsistency", "Consistency Workspace", "")

		payload := []byte(`{"message": "Initial message for consistency test", "workspaceId": "testWsConsistency"}`)
		_ = testRequest(t, router, "POST", "/api/chat", payload, http.StatusOK)

		var sessionID string
		var primaryBranchIDFromSession string
		querySingleRow(t, testDB, "SELECT id, primary_branch_id FROM sessions WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 1", []interface{}{"testWsConsistency"}, &sessionID, &primaryBranchIDFromSession)

		// Verify branch in branches table
		var branchIDFromBranches string
		var branchSessionIDFromBranches string
		querySingleRow(t, testDB, "SELECT id, session_id FROM branches WHERE id = ?", []interface{}{primaryBranchIDFromSession}, &branchIDFromBranches, &branchSessionIDFromBranches)
		if branchIDFromBranches != primaryBranchIDFromSession {
			t.Errorf("Expected branch ID from branches table %s, got %s", primaryBranchIDFromSession, branchIDFromBranches)
		}
		if branchSessionIDFromBranches != sessionID {
			t.Errorf("Expected branch session ID from branches table %s, got %s", sessionID, branchSessionIDFromBranches)
		}

		// Verify branch_id of the first message
		var messageBranchID string
		querySingleRow(t, testDB, "SELECT branch_id FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 1", []interface{}{sessionID}, &messageBranchID)
		if messageBranchID != primaryBranchIDFromSession {
			t.Errorf("Expected first message branch ID %s, got %s", primaryBranchIDFromSession, messageBranchID)
		}
	})
}

// TestCreateBranchHandler_BranchIDConsistency tests that branch IDs are consistent when a new branch is created.
func TestCreateBranchHandler_BranchIDConsistency(t *testing.T) {
	router, testDB, _ := setupTest(t)

	var newBranchID string // Declared here

	t.Run("BranchIDConsistency", func(t *testing.T) {
		// 1. Create an initial session and messages to branch from
		sessionId := generateID()
		primaryBranchID, err := CreateSession(testDB, sessionId, "Initial system prompt for branching test", "default")
		if err != nil {
			t.Fatalf("Failed to create initial session: %v", err)
		}
		firstMessage := Message{SessionID: sessionId, BranchID: primaryBranchID, Role: "user", Text: "First message for branching test", Type: "text"}
		firstMessageID, err := AddMessageToSession(context.Background(), testDB, firstMessage)
		if err != nil {
			t.Fatalf("Failed to add first message: %v", err)
		}
		parentMessage := Message{SessionID: sessionId, BranchID: primaryBranchID, ParentMessageID: &firstMessageID, Role: "user", Text: "Message to branch from", Type: "text"}
		parentMessageID, err := AddMessageToSession(context.Background(), testDB, parentMessage)
		if err != nil {
			t.Fatalf("Failed to add parent message: %v", err)
		}
		// Update chosen_next_id for the first message
		if err = UpdateMessageChosenNextID(testDB, firstMessageID, &parentMessageID); err != nil {
			t.Fatalf("Failed to update chosen_next_id for first message: %v", err)
		}

		// 2. Call createBranchHandler to create a new branch
		payload := []byte(fmt.Sprintf(`{"updatedMessageId": %d, "newMessageText": "First message in new branch"}`, parentMessageID))
		rr := testRequest(t, router, "POST", "/api/chat/"+sessionId+"/branch", payload, http.StatusOK)

		var response map[string]string
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		newBranchID = response["newBranchId"]
		if newBranchID == "" {
			t.Fatalf("newBranchId not found in response")
		}

		// 3. Verify the new branch in the branches table
		var branchFromBranches Branch
		querySingleRow(t, testDB, "SELECT id, session_id, parent_branch_id, branch_from_message_id FROM branches WHERE id = ?", []interface{}{newBranchID}, &branchFromBranches.ID, &branchFromBranches.SessionID, &branchFromBranches.ParentBranchID, &branchFromBranches.BranchFromMessageID)
		if branchFromBranches.ID != newBranchID {
			t.Errorf("Expected branch ID from branches table %s, got %s", newBranchID, branchFromBranches.ID)
		}
		if branchFromBranches.SessionID != sessionId {
			t.Errorf("Expected branch session ID from branches table %s, got %s", sessionId, branchFromBranches.SessionID)
		}
		if *branchFromBranches.ParentBranchID != primaryBranchID {
			t.Errorf("Expected parent branch ID %s, got %s", primaryBranchID, *branchFromBranches.ParentBranchID)
		}
		if *branchFromBranches.BranchFromMessageID != firstMessageID {
			t.Errorf("Expected branch from message ID %d, got %d", firstMessageID, *branchFromBranches.BranchFromMessageID)
		}

		// 4. Verify the first message in the new branch
		var firstMessageInNewBranchID int
		var firstMessageBranchID string
		var firstMessageParentID sql.NullInt64
		querySingleRow(t, testDB, "SELECT id, branch_id, parent_message_id FROM messages WHERE session_id = ? AND branch_id = ? ORDER BY created_at ASC LIMIT 1", []interface{}{sessionId, newBranchID}, &firstMessageInNewBranchID, &firstMessageBranchID, &firstMessageParentID)
		if firstMessageBranchID != newBranchID {
			t.Errorf("Expected first message branch ID %s, got %s", newBranchID, firstMessageBranchID)
		}
		if !firstMessageParentID.Valid || firstMessageParentID.Int64 != int64(firstMessageID) {
			t.Errorf("Expected first message parent ID %d, got %v", firstMessageID, firstMessageParentID)
		}

		// 5. Verify chosen_next_id of the parent message
		var chosenNextID sql.NullInt64
		querySingleRow(t, testDB, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{firstMessageID}, &chosenNextID)
		if !chosenNextID.Valid || chosenNextID.Int64 != int64(firstMessageInNewBranchID) {
			t.Errorf("Expected chosen_next_id for parent message to be %d, got %d", firstMessageInNewBranchID, chosenNextID.Int64)
		}
	})
}
