package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

// TestCreateWorkspaceHandler tests the createWorkspaceHandler function
func TestCreateWorkspaceHandler(t *testing.T) {
	router, _, _ := setupTest(t)

	// Test case 1: Successful workspace creation
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "Test Workspace"}`)
		rr := testRequest(t, router, "POST", "/api/workspaces", payload, http.StatusOK)

		var response map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}

		if response["name"] != "Test Workspace" {
			t.Errorf("handler returned unexpected name: got %v want %v",
				response["name"], "Test Workspace")
		}
		if response["id"] == "" {
			t.Errorf("handler returned empty ID")
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"name": "Test Workspace"`) // Malformed JSON
		testRequest(t, router, "POST", "/api/workspaces", payload, http.StatusBadRequest)
	})

	// Test case 3: Empty name (assuming name is required)
	t.Run("Empty Name", func(t *testing.T) {
		payload := []byte(`{"name": ""}`)
		// Depending on validation logic, this might be BadRequest or OK with empty name
		// For now, assuming it's OK as per current implementation (empty string is valid)
		testRequest(t, router, "POST", "/api/workspaces", payload, http.StatusOK)
	})
}

// TestListWorkspacesHandler tests the listWorkspacesHandler function
func TestListWorkspacesHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare some workspaces in the DB
	database.CreateWorkspace(testDB, "ws1", "Workspace One", "")
	database.CreateWorkspace(testDB, "ws2", "Workspace Two", "")

	rr := testRequest(t, router, "GET", "/api/workspaces", nil, http.StatusOK)

	var workspaces []Workspace
	err := json.Unmarshal(rr.Body.Bytes(), &workspaces)
	if err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}

	if len(workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(workspaces))
	}

	// Check if the workspaces are present (order might vary, so check by name)
	foundWs1 := false
	foundWs2 := false
	for _, ws := range workspaces {
		if ws.Name == "Workspace One" {
			foundWs1 = true
		}
		if ws.Name == "Workspace Two" {
			foundWs2 = true
		}
	}

	if !foundWs1 || !foundWs2 {
		t.Errorf("expected workspaces not found in response")
	}
}

// TestDeleteWorkspaceHandler tests the deleteWorkspaceHandler function
func TestDeleteWorkspaceHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Create a workspace and a session/message within it
	workspaceID := "testWsDelete"
	database.CreateWorkspace(testDB, workspaceID, "Workspace to Delete", "")
	var err error // Declare err here
	sessionID := database.GenerateID()
	primaryBranchID, err := database.CreateSession(testDB, sessionID, "System prompt", workspaceID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	msg := Message{SessionID: sessionID, BranchID: primaryBranchID, Text: "Hello", Type: "user"}
	_, err = database.AddMessageToSession(context.Background(), testDB, msg)
	if err != nil {
		t.Fatalf("Failed to add message to session: %v", err)
	}

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		rr := testRequest(t, router, "DELETE", "/api/workspaces/"+workspaceID, nil, http.StatusOK)

		var response map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}

		// Verify deletion in DB
		var count int
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM workspaces WHERE id = ?", []interface{}{workspaceID}, &count)
		if count != 0 {
			t.Errorf("workspace not deleted from DB")
		}
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM sessions WHERE workspace_id = ?", []interface{}{workspaceID}, &count)
		if count != 0 {
			t.Errorf("sessions not deleted from DB")
		}
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM messages WHERE session_id = ?", []interface{}{sessionID}, &count)
		if count != 0 {
			t.Errorf("messages not deleted from DB")
		}
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM branches WHERE session_id = ?", []interface{}{sessionID}, &count)
		if count != 0 {
			t.Errorf("branches not deleted from DB")
		}
	})

	// Test case 2: Workspace not found
	t.Run("Not Found", func(t *testing.T) {
		testRequest(t, router, "DELETE", "/api/workspaces/non-existent-id", nil, http.StatusOK)
	})

	// Test case 3: Missing workspace ID
	t.Run("Missing ID", func(t *testing.T) {
		testRequest(t, router, "DELETE", "/api/workspaces/", nil, http.StatusNotFound)
	})
}

// TestNewSessionAndMessage tests the newSessionAndMessage function
func TestNewSessionAndMessage(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Test case 1: Successful creation of new session and message
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"message": "Hello, world!", "workspaceId": "testWsNewSession"}`)

		// Create a dummy workspace first
		database.CreateWorkspace(testDB, "testWsNewSession", "New Session Workspace", "")

		// Verify workspace was created in testDB
		var count int
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM workspaces WHERE id = ?", []interface{}{"testWsNewSession"}, &count)
		if count != 1 {
			t.Fatalf("Workspace not created in testDB")
		}

		rr := testStreamingRequest(t, router, "POST", "/api/chat", payload, http.StatusOK)
		defer rr.Body.Close()

		// Verify in DB - both workspace and session/message should be created
		var sessionIDFromDB string
		var text string

		// Check if testDB is still valid
		if testDB == nil {
			t.Fatalf("testDB is nil before querying latest session")
		}
		if err := testDB.Ping(); err != nil {
			t.Fatalf("testDB connection is invalid before querying latest session: %v", err)
		}

		// Since newSessionAndMessage streams, we can't get session ID from Location header.
		// Instead, we need to query the DB for the latest session created in the test workspace.
		var actualSessionID string
		querySingleRow(t, testDB, "SELECT id FROM sessions WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 1", []interface{}{"testWsNewSession"}, &actualSessionID)

		querySingleRow(t, testDB, "SELECT id FROM sessions WHERE id = ?", []interface{}{actualSessionID}, &sessionIDFromDB)
		querySingleRow(t, testDB, "SELECT text FROM messages WHERE session_id = ? AND type = 'user' ORDER BY id ASC LIMIT 1", []interface{}{actualSessionID}, &text)
		if text != "Hello, world!" {
			t.Errorf("message text in DB mismatch: got %v want %v", text, "Hello, world!")
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"message": {"parts": [{"text": "Hello, world!"}]`) // Malformed JSON
		testRequest(t, router, "POST", "/api/chat", payload, http.StatusBadRequest)
	})
}

// TestChatMessage tests the chatMessage function
func TestChatMessage(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session
	var err error // Declare err here
	sessionId := "testChatSession"
	_, err = database.CreateSession(testDB, sessionId, "Initial system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test case 1: Successful addition of a new message
	t.Run("Success", func(t *testing.T) {
		sessionData, err := database.GetSession(testDB, sessionId)
		if err != nil {
			t.Fatalf("Failed to get session: %v", err)
		}
		// Add an initial message to the session
		msg := Message{SessionID: sessionId, BranchID: sessionData.PrimaryBranchID, Text: "Initial message", Type: "user", Model: DefaultGeminiModel}
		_, err = database.AddMessageToSession(context.Background(), testDB, msg)
		if err != nil {
			t.Fatalf("Failed to add initial message: %v", err)
		}

		payload := []byte(fmt.Sprintf(`{"message": "Another message", "model": "%s"}`, DefaultGeminiModel))
		testRequest(t, router, "POST", "/api/chat/"+sessionId, payload, http.StatusOK)

		// Verify in DB
		var text string
		querySingleRow(t, testDB, "SELECT text FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 1", []interface{}{sessionId}, &text)
		if text != "Another message" {
			t.Errorf("message text in DB mismatch: got %v want %v", text, "Another message")
		}
	})

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		payload := []byte(`{"message": "Message for non-existent session"}`)
		testRequest(t, router, "POST", "/api/chat/NonExistentSession", payload, http.StatusNotFound)
	})

	// Test case 3: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"message": {"parts": [{"text": "Another message"}]`) // Malformed JSON
		testRequest(t, router, "POST", "/api/chat/"+sessionId, payload, http.StatusBadRequest)
	})
}

// TestLoadChatSession tests the loadChatSession function
func TestLoadChatSession(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session and some messages
	sessionId := "testLoadSession"
	primaryBranchID, err := database.CreateSession(testDB, sessionId, "System prompt for loading", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	ctx := context.Background()
	mc, err := database.NewMessageChain(ctx, testDB, sessionId, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to create message chain: %v", err)
	}
	if _, err := mc.Add(ctx, testDB, Message{Text: "User message 1", Type: "user"}); err != nil {
		t.Fatalf("Failed to add message 1: %v", err)
	}
	if _, err := mc.Add(ctx, testDB, Message{Text: "Model response 1", Type: "model"}); err != nil {
		t.Fatalf("Failed to add message 2: %v", err)
	}

	// Test case 1: Successful session load
	t.Run("Success", func(t *testing.T) {
		rr := testStreamingRequest(t, router, "GET", "/api/chat/"+sessionId, nil, http.StatusOK)
		defer rr.Body.Close()

		// Parse SSE events mirroring client-side logic
		// Use a channel to signal when the initial state is found
		initialStateChan := make(chan InitialState, 1)
		errorChan := make(chan error, 1)

		go func() {
			for event := range parseSseStream(t, rr) {
				if event.Type == EventInitialState || event.Type == EventInitialStateNoCall {
					var initialState InitialState
					err := json.Unmarshal([]byte(event.Payload), &initialState)
					if err != nil {
						errorChan <- fmt.Errorf("failed to unmarshal initialState payload: %v", err)
						return
					}
					initialStateChan <- initialState
					return // Found initial state, no need to read further
				}
			}
			errorChan <- fmt.Errorf("InitialState event not found in SSE response (stream ended)")
		}()

		select {
		case initialState := <-initialStateChan:
			// Initial state received, continue with assertions
			if initialState.SessionId != sessionId {
				t.Errorf("expected session ID %s, got %s", sessionId, initialState.SessionId)
			}
			if len(initialState.History) != 2 {
				t.Errorf("expected 2 messages, got %d", len(initialState.History))
			}
			if len(initialState.History) >= 2 && (initialState.History[0].Parts[0].Text != "User message 1" || initialState.History[1].Parts[0].Text != "Model response 1") {
				t.Errorf("message content mismatch")
			}
			if initialState.PrimaryBranchID != primaryBranchID { // Check if PrimaryBranchID is correctly loaded
				t.Errorf("expected PrimaryBranchID %s, got %s", primaryBranchID, initialState.PrimaryBranchID)
			}
		case err := <-errorChan:
			t.Fatalf("%v", err)
		case <-time.After(5 * time.Second): // Add a timeout
			t.Fatalf("timeout waiting for InitialState event")
		}
	}) // Close the anonymous function passed to t.Run

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		testRequest(t, router, "GET", "/api/chat/NonExistentLoadSession", nil, http.StatusNotFound)
	})
}

// TestUpdateSessionNameHandler tests the updateSessionNameHandler function
func TestUpdateSessionNameHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session
	sessionId := "testUpdateNameSession"
	_, err := database.CreateSession(testDB, sessionId, "System prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test case 1: Successful name update
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "Updated Session Name"}`)
		testRequest(t, router, "POST", "/api/chat/"+sessionId+"/name", payload, http.StatusOK)

		// Verify in DB
		var name string
		querySingleRow(t, testDB, "SELECT name FROM sessions WHERE id = ?", []interface{}{sessionId}, &name)
		if name != "Updated Session Name" {
			t.Errorf("session name in DB mismatch: got %v want %v", name, "Updated Session Name")
		}
	})

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		payload := []byte(`{"name": "Non-existent Session Name"}`)
		testRequest(t, router, "POST", "/api/chat/NonExistentSession/name", payload, http.StatusOK)
	})

	// Test case 3: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"name": "Updated Session Name"`) // Malformed JSON
		testRequest(t, router, "POST", "/api/chat/"+sessionId+"/name", payload, http.StatusBadRequest)
	})
}

// TestDeleteSession tests the deleteSession function
func TestDeleteSession(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session and some messages
	sessionId := "TestDeleteSession"
	primaryBranchID, err := database.CreateSession(testDB, sessionId, "System prompt for deletion", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	msg := Message{SessionID: sessionId, BranchID: primaryBranchID, Text: "Message to be deleted", Type: "user"}
	_, err = database.AddMessageToSession(context.Background(), testDB, msg)
	if err != nil {
		t.Fatalf("Failed to add message to session: %v", err)
	}

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		rr := testRequest(t, router, "DELETE", "/api/chat/"+sessionId, nil, http.StatusOK)

		var response map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}

		// Verify deletion in DB
		var count int
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM sessions WHERE id = ?", []interface{}{sessionId}, &count)
		if count != 0 {
			t.Errorf("session not deleted from DB")
		}
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM messages WHERE session_id = ?", []interface{}{sessionId}, &count)
		if count != 0 {
			t.Errorf("messages not deleted from DB")
		}
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM branches WHERE session_id = ?", []interface{}{sessionId}, &count)
		if count != 0 {
			t.Errorf("branches not deleted from DB")
		}
	})

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		testRequest(t, router, "DELETE", "/api/chat/NonExistentDeleteSession", nil, http.StatusOK)
	})
}
