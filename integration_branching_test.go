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

		mc, err := NewMessageChain(context.Background(), testDB, sessionId, primaryBranchID)
		if err != nil {
			t.Fatalf("Failed to create message chain: %v", err)
		}

		// Message 1
		msg1, err := mc.Add(context.Background(), testDB, Message{Text: "Message 1", Type: "user"})
		if err != nil {
			t.Fatalf("Failed to add message 1: %v", err)
		}
		msg1ID := msg1.ID

		// Message 2
		msg2, err := mc.Add(context.Background(), testDB, Message{Text: "Message 2", Type: "model"})
		if err != nil {
			t.Fatalf("Failed to add message 2: %v", err)
		}
		msg2ID := msg2.ID

		// Message 3
		msg3, err := mc.Add(context.Background(), testDB, Message{Text: "Message 3", Type: "user"})
		if err != nil {
			t.Fatalf("Failed to add message 3: %v", err)
		}
		msg3ID := msg3.ID

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

		mcOriginal, err := NewMessageChain(context.Background(), testDB, originalSessionId, originalPrimaryBranchID)
		if err != nil {
			t.Fatalf("Failed to create message chain for original session: %v", err)
		}

		// Add messages to the original session
		firstMsg, err := mcOriginal.Add(context.Background(), testDB, Message{Text: "First message", Type: "user"})
		if err != nil {
			t.Fatalf("Failed to add first message: %v", err)
		}
		firstMsgID := firstMsg.ID

		msgToBranchFrom, err := mcOriginal.Add(context.Background(), testDB, Message{Text: "Message to branch from", Type: "user"})
		if err != nil {
			t.Fatalf("Failed to add message to branch from: %v", err)
		}
		msgIDToBranchFrom := msgToBranchFrom.ID

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
		msgToBranchFrom := Message{SessionID: sessionId, BranchID: originalPrimaryBranchID, Text: "Message to branch from for primary branch test", Type: "user"}
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

	t.Run("TestGetSessionHistory_And_Context_InBranching", func(t *testing.T) {
		sessionId := generateID()
		primaryBranchID, err := CreateSession(testDB, sessionId, "System prompt for history test", "default")
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		mcHistory, err := NewMessageChain(context.Background(), testDB, sessionId, primaryBranchID)
		if err != nil {
			t.Fatalf("Failed to create message chain for history test: %v", err)
		}

		// 1. Create linear message flow
		// Message 1 (user)
		if _, err = mcHistory.Add(context.Background(), testDB, Message{Text: "User message 1", Type: "user"}); err != nil {
			t.Fatalf("Failed to add msg1: %v", err)
		}

		// Message 2 (model)
		if _, err = mcHistory.Add(context.Background(), testDB, Message{Text: "Model message 2", Type: "model"}); err != nil {
			t.Fatalf("Failed to add msg2: %v", err)
		}

		// Message 3 (thought) - should be discarded by GetSessionHistoryContext
		if _, err = mcHistory.Add(context.Background(), testDB, Message{Text: "Thought message 3", Type: "thought"}); err != nil {
			t.Fatalf("Failed to add msg3: %v", err)
		}

		// Message 4 (user)
		msg4, err := mcHistory.Add(context.Background(), testDB, Message{Text: "User message 4", Type: "user"})
		if err != nil {
			t.Fatalf("Failed to add msg4: %v", err)
		}

		// Message 5 (compression) - should affect GetSessionHistoryContext
		compressionText := fmt.Sprintf("%d\nSummary of messages up to msg4", msg4.ID)
		if _, err = mcHistory.Add(context.Background(), testDB, Message{Text: compressionText, Type: "compression"}); err != nil {
			t.Fatalf("Failed to add msg5: %v", err)
		}

		// Message 6 (model) - after compression
		if _, err = mcHistory.Add(context.Background(), testDB, Message{Text: "Model message 6 (after compression)", Type: "model"}); err != nil {
			t.Fatalf("Failed to add msg6: %v", err)
		}

		// Message 7 (user)
		if _, err = mcHistory.Add(context.Background(), testDB, Message{Text: "User message 7", Type: "user"}); err != nil {
			t.Fatalf("Failed to add msg7: %v", err)
		}

		// 2. Create a branch (branch from msg4)
		newBranchID, err := CreateBranch(testDB, generateID(), sessionId, &primaryBranchID, &msg4.ID)
		if err != nil {
			t.Fatalf("Failed to create new branch: %v", err)
		}

		mcNewBranch, err := NewMessageChain(context.Background(), testDB, sessionId, newBranchID)
		if err != nil {
			t.Fatalf("Failed to create message chain for new branch: %v", err)
		}
		mcNewBranch.LastMessageID = msg4.ID // Set parent for the first message in new branch

		// Message A (new branch, user) - parent is msg4
		if _, err := mcNewBranch.Add(context.Background(), testDB, Message{Text: "User message A (new branch)", Type: "user"}); err != nil {
			t.Fatalf("Failed to add msgA: %v", err)
		}

		// Message B (new branch, model)
		if _, err := mcNewBranch.Add(context.Background(), testDB, Message{Text: "Model message B (new branch)", Type: "model"}); err != nil {
			t.Fatalf("Failed to add msgB: %v", err)
		}

		// Message C (new branch, thought) - should be discarded by GetSessionHistoryContext
		if _, err := mcNewBranch.Add(context.Background(), testDB, Message{Text: "Thought message C (new branch)", Type: "thought"}); err != nil {
			t.Fatalf("Failed to add msgC: %v", err)
		}

		// Message D (new branch, user)
		if _, err := mcNewBranch.Add(context.Background(), testDB, Message{Text: "User message D (new branch)", Type: "user"}); err != nil {
			t.Fatalf("Failed to add msgD: %v", err)
		}

		// 3. GetSessionHistory 테스트 (primaryBranchID)
		t.Run("GetSessionHistory_PrimaryBranch", func(t *testing.T) {
			history, err := GetSessionHistory(testDB, sessionId, primaryBranchID)
			if err != nil {
				t.Fatalf("GetSessionHistory failed: %v", err)
			}

			expectedTexts := []string{
				"User message 1",
				"Model message 2",
				"Thought message 3",
				"User message 4",
				compressionText,
				"Model message 6 (after compression)",
				"User message 7",
			}

			if len(history) != len(expectedTexts) {
				t.Errorf("Expected %d messages, got %d", len(expectedTexts), len(history))
			}

			for i, msg := range history {
				if i >= len(expectedTexts) {
					break
				}
				if msg.Parts[0].Text != expectedTexts[i] {
					t.Errorf("Message %d: Expected '%s', got '%s'", i, expectedTexts[i], msg.Parts[0].Text)
				}
			}
		})

		// 4. GetSessionHistoryContext 테스트 (primaryBranchID)
		t.Run("GetSessionHistoryContext_PrimaryBranch", func(t *testing.T) {
			history, err := GetSessionHistoryContext(testDB, sessionId, primaryBranchID)
			if err != nil {
				t.Fatalf("GetSessionHistoryContext failed: %v", err)
			}

			// Expected: msg5 (compression), msg6, msg7
			expectedTexts := []string{
				"Summary of messages up to msg4", // Only the summary part should be in FrontendMessage.Parts[0].Text
				"Model message 6 (after compression)",
				"User message 7",
			}

			if len(history) != len(expectedTexts) {
				t.Errorf("Expected %d messages, got %d", len(expectedTexts), len(history))
			}

			for i, msg := range history {
				if i >= len(expectedTexts) {
					break
				}
				if msg.Parts[0].Text != expectedTexts[i] {
					t.Errorf("Message %d: Expected '%s', got '%s'", i, expectedTexts[i], msg.Parts[0].Text)
				}
			}
		})

		// 5. Test GetSessionHistoryContext (newBranchID)
		t.Run("GetSessionHistoryContext_NewBranch", func(t *testing.T) {
			history, err := GetSessionHistoryContext(testDB, sessionId, newBranchID)
			if err != nil {
				t.Fatalf("GetSessionHistoryContext failed: %v", err)
			}

			// Expected: msg1, msg2, msg4 (branch point), msgA, msgB, msgD
			// msg3 (thought) and msgC (thought) should be discarded.
			// No compression message in this branch, so it should go from the beginning of the branch.
			expectedTexts := []string{
				"User message 1",
				"Model message 2",
				"User message 4", // Branch point
				"User message A (new branch)",
				"Model message B (new branch)",
				"User message D (new branch)",
			}

			if len(history) != len(expectedTexts) {
				t.Errorf("Expected %d messages, got %d", len(expectedTexts), len(history))
			}

			for i, msg := range history {
				if i >= len(expectedTexts) {
					break
				}
				if msg.Parts[0].Text != expectedTexts[i] {
					t.Errorf("Message %d: Expected '%s', got '%s'", i, expectedTexts[i], msg.Parts[0].Text)
				}
			}
		})
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
		firstMessage := Message{SessionID: sessionId, BranchID: primaryBranchID, Text: "First message for branching test", Type: "user"}
		firstMessageID, err := AddMessageToSession(context.Background(), testDB, firstMessage)
		if err != nil {
			t.Fatalf("Failed to add first message: %v", err)
		}
		parentMessage := Message{SessionID: sessionId, BranchID: primaryBranchID, ParentMessageID: &firstMessageID, Text: "Message to branch from", Type: "user"}
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
