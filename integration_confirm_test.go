package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/llm"
	. "github.com/lifthrasiir/angel/internal/types"
)

// Helper to create a session with a pending confirmation
func setupSessionWithPendingConfirmation(
	t *testing.T,
	router *mux.Router,
	db *sql.DB,
	models *llm.Models,
	toolName string,
	toolArgs map[string]interface{},
) (sessionId string, branchId string, pendingConfirmationData string, resp *http.Response) {
	// Setup Mock Gemini Provider to return a function call for the specified tool
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{FunctionCall: &FunctionCall{Name: toolName, Args: toolArgs}}),
		},
	})

	initialUserMessage := fmt.Sprintf("Please execute %s", toolName)
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	body, _ := json.Marshal(reqBody)
	resp = testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
	// No defer resp.Body.Close() here, as the caller will close it.

	// Parse SSE events to get session ID, message IDs, and pending confirmation data
	for event := range parseSseStream(t, resp) {
		switch event.Type {
		case EventAcknowledge:
			// We don't need userMessageID for this test, so we ignore it.
		case EventInitialState:
			var initialState InitialState
			err := json.Unmarshal([]byte(event.Payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
			branchId = initialState.PrimaryBranchID
		case EventFunctionCall:
			// We don't need functionCallMessageID for this test, so we ignore it.
		case EventPendingConfirmation:
			pendingConfirmationData = event.Payload
		case EventComplete:
			t.Fatalf("Expected streaming to stop for confirmation, but received EventComplete")
		case EventError:
			t.Fatalf("Expected streaming to stop for confirmation, but received EventError: %s", event.Payload)
		}
	}

	if sessionId == "" || branchId == "" || pendingConfirmationData == "" {
		t.Fatalf("Failed to get all required data for confirmation test. SessionID: %s, BranchID: %s, PendingConfirmationData: %s", sessionId, branchId, pendingConfirmationData)
	}

	// Verify that the branch's pending_confirmation column is set
	var branch Branch
	querySingleRow(t, db, "SELECT pending_confirmation FROM branches WHERE id = ?", []interface{}{branchId}, &branch.PendingConfirmation)
	if branch.PendingConfirmation == nil || *branch.PendingConfirmation == "" {
		t.Fatalf("Expected pending_confirmation to be set in DB, got %v", branch.PendingConfirmation)
	}

	return
}

func TestConfirmationDenial(t *testing.T) {
	router, db, models := setupTest(t)

	// Create a test workspace
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Create a temporary directory for testing file writes
	tempDir, err := os.MkdirTemp("", "angel_test_denial_")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temporary directory

	// Setup session with pending confirmation
	sessionId, branchId, _, resp1 := setupSessionWithPendingConfirmation(t, router, db, models, "write_file", map[string]interface{}{"file_path": filepath.Join(tempDir, "denied.txt"), "content": "Denied Content"})
	defer resp1.Body.Close()

	// Update session roots to include the temporary directory
	_, err = database.AddSessionEnv(db, sessionId, []string{tempDir})
	if err != nil {
		t.Fatalf("Failed to update session roots: %v", err)
	}

	// Send a denial confirmation
	confirmReqBody := map[string]interface{}{
		"approved": false,
	}
	confirmBody, _ := json.Marshal(confirmReqBody)
	resp2 := testStreamingRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s/branch/%s/confirm", sessionId, branchId), confirmBody, http.StatusOK)
	defer resp2.Body.Close()

	// Expect EventFunctionResponse and EventComplete
	var receivedFunctionResponse bool
	var receivedComplete bool
	for event := range parseSseStream(t, resp2) {
		switch event.Type {
		case EventFunctionResponse:
			receivedFunctionResponse = true
		case EventComplete:
			receivedComplete = true
		case EventError:
			t.Fatalf("Received EventError during denial: %s", event.Payload)
		default:
			t.Errorf("Unexpected event type during denial: %c", event.Type)
		}
	}
	if !receivedFunctionResponse {
		t.Fatalf("Expected EventFunctionResponse after denial, but not received")
	}
	if !receivedComplete {
		t.Fatalf("Expected EventComplete after denial, but not received")
	}

	// Verify pending_confirmation is cleared in DB
	var branch Branch
	querySingleRow(t, db, "SELECT pending_confirmation FROM branches WHERE id = ?", []interface{}{branchId}, &branch.PendingConfirmation)
	if branch.PendingConfirmation != nil && *branch.PendingConfirmation != "" {
		t.Fatalf("Expected pending_confirmation to be cleared in DB after denial, got %v", branch.PendingConfirmation)
	}

	// Verify a new message (function response) was added after denial
	var messageCount int
	querySingleRow(t, db, "SELECT COUNT(*) FROM messages WHERE session_id = ?", []interface{}{sessionId}, &messageCount)
	// Initial user message + function call message + function response message
	if messageCount != 3 {
		t.Fatalf("Expected 3 messages after denial (user + function call + function response), got %d", messageCount)
	}
}

func TestConfirmationApproval(t *testing.T) {
	router, db, models := setupTest(t)

	// Create a test workspace
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Create a temporary directory for testing file writes
	tempDir, err := os.MkdirTemp("", "angel_test_approval_")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temporary directory

	// Setup session with pending confirmation
	sessionId, branchId, _, resp1 := setupSessionWithPendingConfirmation(t, router, db, models, "write_file", map[string]interface{}{"file_path": filepath.Join(tempDir, "approved.txt"), "content": "Approved Content"})
	defer resp1.Body.Close()

	// Update session roots to include the temporary directory
	_, err = database.AddSessionEnv(db, sessionId, []string{tempDir})
	if err != nil {
		t.Fatalf("Failed to update session roots: %v", err)
	}

	// Setup Mock Gemini Provider to return a function response and a model response
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{FunctionResponse: &FunctionResponse{Name: "write_file", Response: map[string]interface{}{"status": "success"}}}),
			responseFromPart(Part{Text: "File written successfully."}),
		},
	})

	// Send an approval confirmation
	confirmReqBody := map[string]interface{}{
		"approved": true,
		"modifiedData": map[string]interface{}{
			"file_path": filepath.Join(tempDir, "modified_approved.txt"), // Test modified data
			"content":   "Modified Approved Content",
		},
	}
	confirmBody, _ := json.Marshal(confirmReqBody)
	resp2 := testStreamingRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s/branch/%s/confirm", sessionId, branchId), confirmBody, http.StatusOK)
	defer resp2.Body.Close()

	// Expect EventFunctionResponse and EventComplete
	var receivedFunctionResponse bool
	var receivedCompleteAfterApproval bool
	var functionResponseContent string
	var modelMessageContent string

	for event := range parseSseStream(t, resp2) {
		switch event.Type {
		case EventFunctionResponse:
			receivedFunctionResponse = true
			parts := strings.SplitN(event.Payload, "\n", 3) // Split into 3 parts: messageID, functionName, functionResponseJSON
			if len(parts) < 3 {
				t.Fatalf("Invalid EventFunctionResponse payload: %s", event.Payload)
			}
			functionResponseContent = parts[2] // The third part is the JSON string
		case EventModelMessage:
			_, content, _ := strings.Cut(event.Payload, "\n")
			modelMessageContent = content
		case EventComplete:
			receivedCompleteAfterApproval = true
		case EventError:
			t.Fatalf("Received EventError during approval: %s", event.Payload)
		case EventWorkspaceHint:
			// Expected but do nothing
		default:
			t.Errorf("Unexpected event type during approval: %c", event.Type)
		}
	}

	if !receivedFunctionResponse {
		t.Fatalf("Expected EventFunctionResponse after approval, but not received")
	}
	if !receivedCompleteAfterApproval {
		t.Fatalf("Expected EventComplete after approval, but not received")
	}

	// Verify the content of the function response (should reflect the modified data)
	var response FunctionResponsePayload
	err = json.Unmarshal([]byte(functionResponseContent), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal function response content: %v", err)
	}

	if status, ok := response.Response["status"]; !ok || status != "success" {
		t.Errorf("Expected function response status 'success', got %v", status)
	}

	// Verify pending_confirmation is cleared in DB
	var pendingConfirmationAfterApproval sql.NullString
	querySingleRow(t, db, "SELECT pending_confirmation FROM branches WHERE id = ?", []interface{}{branchId}, &pendingConfirmationAfterApproval)
	if pendingConfirmationAfterApproval.Valid && pendingConfirmationAfterApproval.String != "" {
		t.Fatalf("Expected pending_confirmation to be cleared in DB after approval, got %v", pendingConfirmationAfterApproval.String)
	}

	// Verify new messages were added after approval
	var messageCount int
	querySingleRow(t, db, "SELECT COUNT(*) FROM messages WHERE session_id = ?", []interface{}{sessionId}, &messageCount)
	// Initial user message + function call message + function response message + model message
	if messageCount != 4 {
		t.Fatalf("Expected 4 messages after approval (user + function call + function response + model), got %d", messageCount)
	}

	// Verify the model message content
	if modelMessageContent != "File written successfully." {
		t.Errorf("Expected model message 'File written successfully.', got '%s'", modelMessageContent)
	}

	// Verify the file was actually written with the modified content
	writtenContent, err := os.ReadFile(filepath.Join(tempDir, "modified_approved.txt"))
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}
	if string(writtenContent) != "Modified Approved Content" {
		t.Errorf("Expected written content 'Modified Approved Content', got '%s'", string(writtenContent))
	}
}
