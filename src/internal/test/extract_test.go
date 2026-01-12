package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/lifthrasiir/angel/internal/chat"
	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

// TestExtractSession_Basic tests basic session extraction functionality.
func TestExtractSession_Basic(t *testing.T) {
	_, testDB, _ := setupTest(t)

	// Create a session with some messages
	sessionId := "testExtractBasic"
	sdb, primaryBranchID, err := database.CreateSession(testDB, sessionId, "Test system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	// Create a message chain and add messages
	ctx := context.Background()
	mc, err := database.NewMessageChain(ctx, sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create message chain: %v", err)
	}

	_, err = mc.Add(Message{Text: "User message 1", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add message 1: %v", err)
	}

	msg2, err := mc.Add(Message{Text: "Model response 1", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add message 2: %v", err)
	}

	_, err = mc.Add(Message{Text: "User message 2", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add message 3: %v", err)
	}

	// Extract session up to msg2 (excluding msg3)
	newSessionId, newSessionName, err := chat.ExtractSession(ctx, sdb, msg2.ID)
	if err != nil {
		t.Fatalf("ExtractSession failed: %v", err)
	}

	// Verify the new session name
	if newSessionName == "" {
		t.Error("New session name is empty")
	}
	if newSessionId == "" {
		t.Error("New session ID is empty")
	}

	// Open the new session
	newSdb, err := testDB.WithSession(newSessionId)
	if err != nil {
		t.Fatalf("Failed to open new session: %v", err)
	}
	defer newSdb.Close()

	// Get the new session's primary branch
	newSession, err := database.GetSession(newSdb)
	if err != nil {
		t.Fatalf("Failed to get new session: %v", err)
	}

	// Get messages from new session
	newHistory, err := database.GetSessionHistory(newSdb, newSession.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get new session history: %v", err)
	}

	// Should have 2 messages (up to msg2)
	if len(newHistory) != 2 {
		t.Errorf("Expected 2 messages in new session, got %d", len(newHistory))
	}

	// Verify message content
	if len(newHistory) > 0 && newHistory[0].Parts[0].Text != "User message 1" {
		t.Errorf("Expected first message to be 'User message 1', got '%s'", newHistory[0].Parts[0].Text)
	}
	if len(newHistory) > 1 && newHistory[1].Parts[0].Text != "Model response 1" {
		t.Errorf("Expected second message to be 'Model response 1', got '%s'", newHistory[1].Parts[0].Text)
	}

	// Verify original session is unchanged
	originalHistory, err := database.GetSessionHistory(sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get original session history: %v", err)
	}
	if len(originalHistory) != 3 {
		t.Errorf("Expected 3 messages in original session, got %d", len(originalHistory))
	}
}

// TestExtractSession_WithSubsessions tests session extraction with subsessions.
// Uses filesystem-based session DBs because in-memory DBs are shared and cause issues with subsession isolation.
func TestExtractSession_WithSubsessions(t *testing.T) {
	_, testDB, _ := setupTestWithFilesystem(t)

	// Create a session with a subagent call (subsession)
	sessionId := "testExtractSubsession"
	sdb, primaryBranchID, err := database.CreateSession(testDB, sessionId, "Test system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	ctx := context.Background()
	mc, err := database.NewMessageChain(ctx, sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create message chain: %v", err)
	}

	// Add user message
	_, err = mc.Add(Message{Text: "Use subagent", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add user message: %v", err)
	}

	// Add function call to create subagent
	subagentID := "subagent123"
	functionCallData := map[string]interface{}{
		"name": "subagent",
		"args": map[string]interface{}{
			"prompt":    "Subagent task",
			"parent_id": sessionId,
		},
	}
	fcJSON, _ := json.Marshal(functionCallData)
	_, err = mc.Add(Message{Text: string(fcJSON), Type: TypeFunctionCall})
	if err != nil {
		t.Fatalf("Failed to add function call: %v", err)
	}

	// Add function response with subagent_id
	functionResponseData := map[string]interface{}{
		"name":     "subagent",
		"response": map[string]interface{}{"subagent_id": subagentID},
	}
	frJSON, _ := json.Marshal(functionResponseData)
	frMsg, err := mc.Add(Message{Text: string(frJSON), Type: TypeFunctionResponse})
	if err != nil {
		t.Fatalf("Failed to add function response: %v", err)
	}

	// Create the subsession
	subsessionID := fmt.Sprintf("%s.%s", sessionId, subagentID)
	subSdb, subPrimaryBranchID, err := database.CreateSession(testDB, subsessionID, "Subsession prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create subsession: %v", err)
	}
	defer subSdb.Close()

	// Add a message to the subsession
	subMc, err := database.NewMessageChain(ctx, subSdb, subPrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create subsession message chain: %v", err)
	}
	_, err = subMc.Add(Message{Text: "Subsession message", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add subsession message: %v", err)
	}

	// Extract session up to the function response
	newSessionId, _, err := chat.ExtractSession(ctx, sdb, frMsg.ID)
	if err != nil {
		t.Fatalf("ExtractSession failed: %v", err)
	}

	// Open the new subsession
	newSubsessionID := fmt.Sprintf("%s.%s", newSessionId, subagentID)
	newSubSdb, err := testDB.WithSession(newSubsessionID)
	if err != nil {
		t.Fatalf("Failed to open new subsession: %v", err)
	}
	defer newSubSdb.Close()

	// Get the new subsession's primary branch ID
	newSubSession, err := database.GetSession(newSubSdb)
	if err != nil {
		t.Fatalf("Failed to get new subsession: %v", err)
	}

	// Verify the subsession was copied
	newSubHistory, err := database.GetSessionHistory(newSubSdb, newSubSession.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get new subsession history: %v", err)
	}

	// Should have exactly 1 message in the new subsession
	if len(newSubHistory) != 1 {
		t.Errorf("Expected 1 message in new subsession, got %d", len(newSubHistory))
	}

	// Verify the content
	if len(newSubHistory) > 0 && newSubHistory[0].Parts[0].Text != "Subsession message" {
		t.Errorf("Expected subsession message to be 'Subsession message', got '%s'", newSubHistory[0].Parts[0].Text)
	}
}

// TestExtractSession_WithAttachments tests session extraction with file attachments.
func TestExtractSession_WithAttachments(t *testing.T) {
	_, testDB, _ := setupTest(t)

	// Create a session with file attachments
	sessionId := "testExtractAttachments"
	sdb, primaryBranchID, err := database.CreateSession(testDB, sessionId, "Test system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	ctx := context.Background()
	mc, err := database.NewMessageChain(ctx, sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create message chain: %v", err)
	}

	// Create a blob
	blobData := []byte("test file content")
	blobHash, err := database.SaveBlob(ctx, sdb, blobData)
	if err != nil {
		t.Fatalf("Failed to save blob: %v", err)
	}

	// Add message with attachment
	_, err = mc.Add(Message{
		Text: "User message with file",
		Type: TypeUserText,
		Attachments: []FileAttachment{
			{
				Hash:     blobHash,
				FileName: "test.txt",
				MimeType: "text/plain",
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to add message with attachment: %v", err)
	}

	// Add another message without attachment
	msg2, err := mc.Add(Message{Text: "Model response", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add model message: %v", err)
	}

	// Extract session
	newSessionId, _, err := chat.ExtractSession(ctx, sdb, msg2.ID)
	if err != nil {
		t.Fatalf("ExtractSession failed: %v", err)
	}

	// Open the new session
	newSdb, err := testDB.WithSession(newSessionId)
	if err != nil {
		t.Fatalf("Failed to open new session: %v", err)
	}
	defer newSdb.Close()

	// Get messages from new session
	newSession, err := database.GetSession(newSdb)
	if err != nil {
		t.Fatalf("Failed to get new session: %v", err)
	}

	newHistory, err := database.GetSessionHistory(newSdb, newSession.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get new session history: %v", err)
	}

	// Verify blob was copied
	newBlobData, err := database.GetBlob(newSdb, blobHash)
	if err != nil {
		t.Fatalf("Failed to get blob from new session: %v", err)
	}
	if string(newBlobData) != string(blobData) {
		t.Errorf("Blob data mismatch: expected '%s', got '%s'", string(blobData), string(newBlobData))
	}

	// Verify attachment in the message
	if len(newHistory) > 0 && len(newHistory[0].Attachments) != 1 {
		t.Errorf("Expected 1 attachment in first message, got %d", len(newHistory[0].Attachments))
	}
	if len(newHistory) > 0 && len(newHistory[0].Attachments) > 0 {
		if newHistory[0].Attachments[0].Hash != blobHash {
			t.Errorf("Attachment hash mismatch: expected %s, got %s", blobHash, newHistory[0].Attachments[0].Hash)
		}
	}
}

// TestExtractSession_WithMultipleBranches tests session extraction when there are multiple branches.
// Only the branch containing the target message should be extracted.
func TestExtractSession_WithMultipleBranches(t *testing.T) {
	_, testDB, _ := setupTest(t)

	// Create a session with multiple branches
	sessionId := "testExtractBranches"
	sdb, primaryBranchID, err := database.CreateSession(testDB, sessionId, "Test system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	ctx := context.Background()

	// Create primary branch: msg1 -> msg2 -> msg3
	mcPrimary, err := database.NewMessageChain(ctx, sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create primary message chain: %v", err)
	}

	msg1, err := mcPrimary.Add(Message{Text: "Primary message 1", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add msg1: %v", err)
	}

	msg2, err := mcPrimary.Add(Message{Text: "Primary message 2", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add msg2: %v", err)
	}

	_, err = mcPrimary.Add(Message{Text: "Primary message 3", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add msg3: %v", err)
	}

	// Create a branch from msg1: msg1 -> branch1_msgA -> branch1_msgB
	branch1ID, err := database.CreateBranch(sdb, database.GenerateID(), &primaryBranchID, &msg1.ID)
	if err != nil {
		t.Fatalf("Failed to create branch1: %v", err)
	}

	mcBranch1, err := database.NewMessageChain(ctx, sdb, branch1ID)
	if err != nil {
		t.Fatalf("Failed to create branch1 message chain: %v", err)
	}
	mcBranch1.LastMessageID = msg1.ID

	_, err = mcBranch1.Add(Message{Text: "Branch1 message A", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add branch1_msgA: %v", err)
	}

	_, err = mcBranch1.Add(Message{Text: "Branch1 message B", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add branch1_msgB: %v", err)
	}

	// Create another branch from msg2: msg1 -> msg2 -> branch2_msgX
	branch2ID, err := database.CreateBranch(sdb, database.GenerateID(), &primaryBranchID, &msg2.ID)
	if err != nil {
		t.Fatalf("Failed to create branch2: %v", err)
	}

	mcBranch2, err := database.NewMessageChain(ctx, sdb, branch2ID)
	if err != nil {
		t.Fatalf("Failed to create branch2 message chain: %v", err)
	}
	mcBranch2.LastMessageID = msg2.ID

	branch2MsgX, err := mcBranch2.Add(Message{Text: "Branch2 message X", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add branch2_msgX: %v", err)
	}

	// Extract from primary branch (up to msg2)
	// This should only copy the primary branch, not branch1 or branch2
	newSessionId, _, err := chat.ExtractSession(ctx, sdb, msg2.ID)
	if err != nil {
		t.Fatalf("ExtractSession failed: %v", err)
	}

	// Open the new session
	newSdb, err := testDB.WithSession(newSessionId)
	if err != nil {
		t.Fatalf("Failed to open new session: %v", err)
	}
	defer newSdb.Close()

	// Verify only the primary branch messages were copied
	newSession, err := database.GetSession(newSdb)
	if err != nil {
		t.Fatalf("Failed to get new session: %v", err)
	}

	newHistory, err := database.GetSessionHistory(newSdb, newSession.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get new session history: %v", err)
	}

	// Should have only 2 messages from primary branch (msg1, msg2)
	if len(newHistory) != 2 {
		t.Errorf("Expected 2 messages in new session (primary branch only), got %d", len(newHistory))
	}

	// Verify the messages are from primary branch
	if len(newHistory) > 0 && newHistory[0].Parts[0].Text != "Primary message 1" {
		t.Errorf("Expected first message to be 'Primary message 1', got '%s'", newHistory[0].Parts[0].Text)
	}
	if len(newHistory) > 1 && newHistory[1].Parts[0].Text != "Primary message 2" {
		t.Errorf("Expected second message to be 'Primary message 2', got '%s'", newHistory[1].Parts[0].Text)
	}

	// Note: In in-memory DB mode, other branches from other tests may exist.
	// We only verify that the primary branch has the correct messages.

	// Extract from branch2 (up to branch2_msgX)
	// This should copy primary branch up to msg2, then branch2_msgX
	newSessionId2, _, err := chat.ExtractSession(ctx, sdb, branch2MsgX.ID)
	if err != nil {
		t.Fatalf("ExtractSession from branch2 failed: %v", err)
	}

	// Open the second new session
	newSdb2, err := testDB.WithSession(newSessionId2)
	if err != nil {
		t.Fatalf("Failed to open second new session: %v", err)
	}
	defer newSdb2.Close()

	newSession2, err := database.GetSession(newSdb2)
	if err != nil {
		t.Fatalf("Failed to get second new session: %v", err)
	}

	newHistory2, err := database.GetSessionHistory(newSdb2, newSession2.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get second new session history: %v", err)
	}

	// Should have 3 messages: msg1, msg2 (from primary), branch2_msgX
	if len(newHistory2) != 3 {
		t.Errorf("Expected 3 messages in second new session, got %d", len(newHistory2))
	}

	// Verify the branch2 message is included
	if len(newHistory2) > 2 && newHistory2[2].Parts[0].Text != "Branch2 message X" {
		t.Errorf("Expected third message to be 'Branch2 message X', got '%s'", newHistory2[2].Parts[0].Text)
	}

	// Verify branch1 messages are NOT included
	for _, msg := range newHistory2 {
		if msg.Parts[0].Text == "Branch1 message A" || msg.Parts[0].Text == "Branch1 message B" {
			t.Error("Branch1 messages should not be in the extracted session")
		}
	}
}

// TestExtractSession_Comprehensive tests session extraction with subsessions, attachments, and branches all together.
// Uses filesystem-based session DBs because in-memory DBs are shared and cause issues with subsession isolation.
func TestExtractSession_Comprehensive(t *testing.T) {
	_, testDB, _ := setupTestWithFilesystem(t)

	// Create a session with multiple branches, subsessions, and attachments
	sessionId := "testExtractComprehensive"
	sdb, primaryBranchID, err := database.CreateSession(testDB, sessionId, "Test system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	ctx := context.Background()

	// Create primary branch: msg1 -> msg2 -> msg3
	mcPrimary, err := database.NewMessageChain(ctx, sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create primary message chain: %v", err)
	}

	// msg1: User message with file attachment
	blobData := []byte("test file content for comprehensive test")
	blobHash, err := database.SaveBlob(ctx, sdb, blobData)
	if err != nil {
		t.Fatalf("Failed to save blob: %v", err)
	}

	msg1, err := mcPrimary.Add(Message{
		Text: "User message with file",
		Type: TypeUserText,
		Attachments: []FileAttachment{
			{
				Hash:     blobHash,
				FileName: "test.txt",
				MimeType: "text/plain",
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to add msg1: %v", err)
	}

	// msg2: Model response
	_, err = mcPrimary.Add(Message{Text: "Model response", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add msg2: %v", err)
	}

	// msg3: User message calling subagent
	subagentID := "subagent_comprehensive"
	functionCallData := map[string]interface{}{
		"name": "subagent",
		"args": map[string]interface{}{
			"prompt":    "Subagent task",
			"parent_id": sessionId,
		},
	}
	fcJSON, _ := json.Marshal(functionCallData)
	_, err = mcPrimary.Add(Message{Text: string(fcJSON), Type: TypeFunctionCall})
	if err != nil {
		t.Fatalf("Failed to add function call: %v", err)
	}

	// msg4: Function response with subagent_id
	functionResponseData := map[string]interface{}{
		"name":     "subagent",
		"response": map[string]interface{}{"subagent_id": subagentID},
	}
	frJSON, _ := json.Marshal(functionResponseData)
	frMsg, err := mcPrimary.Add(Message{Text: string(frJSON), Type: TypeFunctionResponse})
	if err != nil {
		t.Fatalf("Failed to add function response: %v", err)
	}

	// Create the subsession
	subsessionID := fmt.Sprintf("%s.%s", sessionId, subagentID)
	subSdb, subPrimaryBranchID, err := database.CreateSession(testDB, subsessionID, "Subsession prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create subsession: %v", err)
	}
	defer subSdb.Close()

	// Add a message to the subsession
	subMc, err := database.NewMessageChain(ctx, subSdb, subPrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create subsession message chain: %v", err)
	}
	_, err = subMc.Add(Message{Text: "Subsession message", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add subsession message: %v", err)
	}

	// Verify the subsession message was added
	subHistory, err := database.GetSessionHistory(subSdb, subPrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get subsession history: %v", err)
	}
	if len(subHistory) != 1 {
		t.Fatalf("Expected 1 message in subsession before extraction, got %d", len(subHistory))
	}

	// Create a branch from msg1
	branch1ID, err := database.CreateBranch(sdb, database.GenerateID(), &primaryBranchID, &msg1.ID)
	if err != nil {
		t.Fatalf("Failed to create branch1: %v", err)
	}

	mcBranch1, err := database.NewMessageChain(ctx, sdb, branch1ID)
	if err != nil {
		t.Fatalf("Failed to create branch1 message chain: %v", err)
	}
	mcBranch1.LastMessageID = msg1.ID

	_, err = mcBranch1.Add(Message{Text: "Branch1 message A", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add branch1_msgA: %v", err)
	}

	// Extract session up to the function response (includes attachment and subsession)
	newSessionId, _, err := chat.ExtractSession(ctx, sdb, frMsg.ID)
	if err != nil {
		t.Fatalf("ExtractSession failed: %v", err)
	}

	// Open the new session
	newSdb, err := testDB.WithSession(newSessionId)
	if err != nil {
		t.Fatalf("Failed to open new session: %v", err)
	}
	defer newSdb.Close()

	// Verify the attachment was copied
	newBlobData, err := database.GetBlob(newSdb, blobHash)
	if err != nil {
		t.Fatalf("Failed to get blob from new session: %v", err)
	}
	if string(newBlobData) != string(blobData) {
		t.Errorf("Blob data mismatch: expected '%s', got '%s'", string(blobData), string(newBlobData))
	}

	// Verify the subsession was copied
	newSubsessionID := fmt.Sprintf("%s.%s", newSessionId, subagentID)
	newSubSdb, err := testDB.WithSession(newSubsessionID)
	if err != nil {
		t.Fatalf("Failed to open new subsession: %v", err)
	}
	defer newSubSdb.Close()

	// Get the new subsession's primary branch ID
	newSubSession, err := database.GetSession(newSubSdb)
	if err != nil {
		t.Fatalf("Failed to get new subsession: %v", err)
	}

	// Verify the subsession was copied
	newSubHistory, err := database.GetSessionHistory(newSubSdb, newSubSession.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get new subsession history: %v", err)
	}

	// Should have exactly 1 message in the new subsession
	if len(newSubHistory) != 1 {
		t.Errorf("Expected 1 message in new subsession, got %d", len(newSubHistory))
	}

	// Verify the content
	if len(newSubHistory) > 0 && newSubHistory[0].Parts[0].Text != "Subsession message" {
		t.Errorf("Expected subsession message to be 'Subsession message', got '%s'", newSubHistory[0].Parts[0].Text)
	}

	// Verify the new session history
	newSession, err := database.GetSession(newSdb)
	if err != nil {
		t.Fatalf("Failed to get new session: %v", err)
	}

	// Note: In in-memory DB mode, other branches from other tests may exist.
	// We only verify that the primary branch has the correct messages.

	newHistory, err := database.GetSessionHistory(newSdb, newSession.PrimaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get new session history: %v", err)
	}

	// Should have 4 messages (msg1, msg2, function call, function response)
	if len(newHistory) != 4 {
		t.Errorf("Expected 4 messages in new session, got %d", len(newHistory))
	}

	// Verify attachment in the first message
	if len(newHistory) > 0 && len(newHistory[0].Attachments) != 1 {
		t.Errorf("Expected 1 attachment in first message, got %d", len(newHistory[0].Attachments))
	}
}

// TestExtractSession_Handler tests the extract session HTTP handler.
func TestExtractSession_Handler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Create a session
	sessionId := "testExtractHandler"
	sdb, primaryBranchID, err := database.CreateSession(testDB, sessionId, "Test system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	ctx := context.Background()
	mc, err := database.NewMessageChain(ctx, sdb, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create message chain: %v", err)
	}

	_, err = mc.Add(Message{Text: "User message", Type: TypeUserText})
	if err != nil {
		t.Fatalf("Failed to add message: %v", err)
	}

	msg2, err := mc.Add(Message{Text: "Model response", Type: TypeModelText})
	if err != nil {
		t.Fatalf("Failed to add message: %v", err)
	}

	// Call the extract handler
	payload := []byte(fmt.Sprintf(`{"messageId": "%d"}`, msg2.ID))
	rr := testRequest(t, router, "POST", "/api/chat/"+sessionId+"/extract", payload, http.StatusOK)

	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}

	if response["sessionId"] == "" {
		t.Error("Expected sessionId in response")
	}
	if response["sessionName"] == "" {
		t.Error("Expected sessionName in response")
	}

	// Verify the new session exists
	newSdb, err := testDB.WithSession(response["sessionId"])
	if err != nil {
		t.Fatalf("Failed to open new session: %v", err)
	}
	defer newSdb.Close()

	newSession, err := database.GetSession(newSdb)
	if err != nil {
		t.Fatalf("Failed to get new session: %v", err)
	}

	if newSession.Name != response["sessionName"] {
		t.Errorf("Session name mismatch: expected '%s', got '%s'", response["sessionName"], newSession.Name)
	}
}
