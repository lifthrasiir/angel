package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// Helper function to set up the test environment
func setupTest(t *testing.T) (*mux.Router, *sql.DB, *GeminiAuth) {

	// Initialize an in-memory database for testing with unique name
	dbName := fmt.Sprintf(":memory:?cache=shared&_txlock=immediate&_foreign_keys=1&_journal_mode=WAL&test=%s", t.Name())
	testDB, err := InitDB(dbName)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Verify that all required tables exist
	requiredTables := []string{"sessions", "messages", "oauth_tokens", "workspaces", "mcp_configs", "branches"}
	for _, tableName := range requiredTables {
		_, err = testDB.Exec(fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", tableName))
		if err != nil {
			t.Fatalf("%s table does not exist after InitDB: %v", tableName, err)
		}
	}

	InitMCPManager(testDB)

	// Reset GlobalGeminiAuth for each test
	ga := &GeminiAuth{}

	// Set up a mock LLMProvider
	originalProvider := CurrentProvider
	CurrentProvider = &MockLLMProvider{
		SendMessageStreamFunc: func(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
			// Return a single response to complete the streaming properly
			return iter.Seq[CaGenerateContentResponse](func(yield func(CaGenerateContentResponse) bool) {
				yield(CaGenerateContentResponse{
					Response: VertexGenerateContentResponse{
						Candidates: []Candidate{
							{Content: Content{Parts: []Part{{Text: "Test response"}}}},
						},
						UsageMetadata: &UsageMetadata{
							TotalTokenCount: 10,
						},
					},
				})
			}), io.NopCloser(nil), nil
		},
		GenerateContentOneShotFunc: func(ctx context.Context, params SessionParams) (string, error) {
			return "Mocked one-shot response", nil
		},
	}

	// Create a new router for testing
	router := mux.NewRouter()
	router.Use(makeContextMiddleware(testDB, ga))
	InitRouter(router)

	// Ensure the database connection is closed after the test
	t.Cleanup(func() {
		if testDB != nil {
			testDB.Close()
		}
		// Restore original provider
		CurrentProvider = originalProvider
	})

	return router, testDB, ga
}

// TestCreateWorkspaceHandler tests the createWorkspaceHandler function
func TestCreateWorkspaceHandler(t *testing.T) {
	router, _, _ := setupTest(t)

	// Test case 1: Successful workspace creation
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "Test Workspace"}`)
		req, _ := http.NewRequest("POST", "/api/workspaces", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

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
		req, _ := http.NewRequest("POST", "/api/workspaces", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})

	// Test case 3: Empty name (assuming name is required)
	t.Run("Empty Name", func(t *testing.T) {
		payload := []byte(`{"name": ""}`)
		req, _ := http.NewRequest("POST", "/api/workspaces", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		// Depending on validation logic, this might be BadRequest or OK with empty name
		// For now, assuming it's OK as per current implementation (empty string is valid)
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code for empty name: got %v want %v",
				status, http.StatusOK)
		}
	})
}

// TestListWorkspacesHandler tests the listWorkspacesHandler function
func TestListWorkspacesHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare some workspaces in the DB
	CreateWorkspace(testDB, "ws1", "Workspace One", "")
	CreateWorkspace(testDB, "ws2", "Workspace Two", "")

	req, _ := http.NewRequest("GET", "/api/workspaces", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

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
	CreateWorkspace(testDB, workspaceID, "Workspace to Delete", "")
	var err error // Declare err here
	sessionID := generateID()
	primaryBranchID, err := CreateSession(testDB, sessionID, "System prompt", workspaceID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	_, err = AddMessageToSession(testDB, sessionID, primaryBranchID, nil, nil, "user", "Hello", "text", nil, nil)
	if err != nil {
		t.Fatalf("Failed to add message to session: %v", err)
	}

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/workspaces/"+workspaceID, nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

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
		testDB.QueryRow("SELECT COUNT(*) FROM workspaces WHERE id = ?", workspaceID).Scan(&count)
		if count != 0 {
			t.Errorf("workspace not deleted from DB")
		}
		testDB.QueryRow("SELECT COUNT(*) FROM sessions WHERE workspace_id = ?", workspaceID).Scan(&count)
		if count != 0 {
			t.Errorf("sessions not deleted from DB")
		}
		testDB.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&count)
		if count != 0 {
			t.Errorf("messages not deleted from DB")
		}
		testDB.QueryRow("SELECT COUNT(*) FROM branches WHERE session_id = ?", sessionID).Scan(&count)
		if count != 0 {
			t.Errorf("branches not deleted from DB")
		}
	})

	// Test case 2: Workspace not found
	t.Run("Not Found", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/workspaces/non-existent-id", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code for non-existent workspace: got %v want %v",
				status, http.StatusOK)
		}
	})

	// Test case 3: Missing workspace ID
	t.Run("Missing ID", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/workspaces/", nil) // Missing ID
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusNotFound {
			t.Errorf("handler returned wrong status code for missing ID: got %v want %v",
				status, http.StatusNotFound)
		}
	})
}

// TestNewSessionAndMessage tests the newSessionAndMessage function
func TestNewSessionAndMessage(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Test case 1: Successful creation of new session and message
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"message": "Hello, world!", "workspaceId": "testWsNewSession"}`)
		req, _ := http.NewRequest("POST", "/api/chat", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Create a dummy workspace first
		CreateWorkspace(testDB, "testWsNewSession", "New Session Workspace", "")

		// Verify workspace was created in testDB
		var count int
		testDB.QueryRow("SELECT COUNT(*) FROM workspaces WHERE id = ?", "testWsNewSession").Scan(&count)
		if count != 1 {
			t.Fatalf("Workspace not created in testDB")
		}

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		// Verify in DB - both workspace and session/message should be created
		var sessionIDFromDB string
		var text string
		var err error

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
		err = testDB.QueryRow("SELECT id FROM sessions WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 1", "testWsNewSession").Scan(&actualSessionID)
		if err != nil {
			t.Fatalf("failed to query latest session from DB: %v", err)
		}

		err = testDB.QueryRow("SELECT id FROM sessions WHERE id = ?", actualSessionID).Scan(&sessionIDFromDB)
		if err != nil {
			t.Fatalf("failed to query session from DB: %v", err)
		}
		err = testDB.QueryRow("SELECT text FROM messages WHERE session_id = ? AND role = 'user' ORDER BY id ASC LIMIT 1", actualSessionID).Scan(&text)
		if err != nil {
			t.Fatalf("failed to query message from DB: %v", err)
		}
		if text != "Hello, world!" {
			t.Errorf("message text in DB mismatch: got %v want %v", text, "Hello, world!")
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"message": {"parts": [{"text": "Hello, world!"}]`) // Malformed JSON
		req, _ := http.NewRequest("POST", "/api/chat", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})
}

// TestChatMessage tests the chatMessage function
func TestChatMessage(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session
	var err error // Declare err here
	sessionId := "testChatSession"
	_, err = CreateSession(testDB, sessionId, "Initial system prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test case 1: Successful addition of a new message
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"message": "Another message"}`)
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionId, bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		// Verify in DB
		var text string
		var err error
		err = testDB.QueryRow("SELECT text FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 1", sessionId).Scan(&text)
		if err != nil {
			t.Fatalf("failed to query message from DB: %v", err)
		}
		if text != "Another message" {
			t.Errorf("message text in DB mismatch: got %v want %v", text, "Another message")
		}
	})

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		payload := []byte(`{"message": "Message for non-existent session"}`)
		req, _ := http.NewRequest("POST", "/api/chat/NonExistentSession", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusNotFound {
			t.Errorf("handler returned wrong status code for non-existent session: got %v want %v",
				status, http.StatusNotFound)
		}
	})

	// Test case 3: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"message": {"parts": [{"text": "Another message"}]`) // Malformed JSON
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionId, bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})
}

// TestLoadChatSession tests the loadChatSession function
func TestLoadChatSession(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session and some messages
	sessionId := "testLoadSession"
	primaryBranchID, err := CreateSession(testDB, sessionId, "System prompt for loading", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	msg1ID, err := AddMessageToSession(testDB, sessionId, primaryBranchID, nil, nil, "user", "User message 1", "text", nil, nil)
	if err != nil {
		t.Fatalf("Failed to add message 1: %v", err)
	}
	msg2ID, err := AddMessageToSession(testDB, sessionId, primaryBranchID, nil, nil, "model", "Model response 1", "text", nil, nil)
	if err != nil {
		t.Fatalf("Failed to add message 2: %v", err)
	}
	if err := UpdateMessageChosenNextID(testDB, msg1ID, &msg2ID); err != nil {
		t.Fatalf("Failed to update chosen_next_id for message 1: %v", err)
	}

	// Test case 1: Successful session load
	t.Run("Success", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/chat/"+sessionId, nil)
		req.Header.Set("Accept", "text/event-stream")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		// Parse SSE events mirroring client-side logic
		// Use a channel to signal when the initial state is found
		initialStateChan := make(chan InitialState, 1)
		errorChan := make(chan error, 1)

		go func() {
			scanner := bufio.NewScanner(rr.Body)
			var buffer string
			for scanner.Scan() {
				line := scanner.Text()
				buffer += line + "\n" // Add newline back as scanner consumes it

				// Check for end of event (double newline)
				if strings.HasSuffix(buffer, "\n\n") {
					// Extract the event string, removing "data: " prefix and replacing internal "data: "
					eventString := buffer[6 : len(buffer)-2] // Remove "data: " and trailing "\n\n"
					eventString = strings.ReplaceAll(eventString, "\ndata: ", "\n")

					// Split into type and data
					parts := strings.SplitN(eventString, "\n", 2)
					if len(parts) < 2 {
						errorChan <- fmt.Errorf("malformed SSE event: %s", eventString)
						return
					}
					eventType := parts[0]
					payloadData := parts[1]

					if eventType == string(EventInitialState) || eventType == string(EventInitialStateNoCall) {
						var initialState InitialState
						err := json.Unmarshal([]byte(payloadData), &initialState)
						if err != nil {
							errorChan <- fmt.Errorf("failed to unmarshal initialState payload: %v", err)
							return
						}
						initialStateChan <- initialState
						return // Found initial state, no need to read further
					}
					buffer = "" // Reset buffer for the next event
				}
			}
			if err := scanner.Err(); err != nil {
				errorChan <- fmt.Errorf("scanner error: %v", err)
			} else {
				errorChan <- fmt.Errorf("InitialState event not found in SSE response (stream ended)")
			}
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
		req, _ := http.NewRequest("GET", "/api/chat/NonExistentLoadSession", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusNotFound {
			t.Errorf("handler returned wrong status code for non-existent session: got %v want %v",
				status, http.StatusNotFound)
		}
	})
}

// TestUpdateSessionNameHandler tests the updateSessionNameHandler function
func TestUpdateSessionNameHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session
	sessionId := "testUpdateNameSession"
	_, err := CreateSession(testDB, sessionId, "System prompt", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test case 1: Successful name update
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "Updated Session Name"}`)
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionId+"/name", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		// Verify in DB
		var name string
		var err error
		err = testDB.QueryRow("SELECT name FROM sessions WHERE id = ?", sessionId).Scan(&name)
		if err != nil {
			t.Fatalf("failed to query session from DB: %v", err)
		}
		if name != "Updated Session Name" {
			t.Errorf("session name in DB mismatch: got %v want %v", name, "Updated Session Name")
		}
	})

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		payload := []byte(`{"name": "Non-existent Session Name"}`)
		req, _ := http.NewRequest("POST", "/api/chat/NonExistentSession/name", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code for non-existent session: got %v want %v",
				status, http.StatusOK)
		}
	})

	// Test case 3: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"name": "Updated Session Name"`) // Malformed JSON
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionId+"/name", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})
}

// TestDeleteSession tests the deleteSession function
func TestDeleteSession(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare a session and some messages
	sessionId := "TestDeleteSession"
	primaryBranchID, err := CreateSession(testDB, sessionId, "System prompt for deletion", "default")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	_, err = AddMessageToSession(testDB, sessionId, primaryBranchID, nil, nil, "user", "Message to be deleted", "text", nil, nil)
	if err != nil {
		t.Fatalf("Failed to add message to session: %v", err)
	}

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/chat/"+sessionId, nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var response map[string]string
		var err error
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}

		// Verify deletion in DB
		var count int
		testDB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", sessionId).Scan(&count)
		if count != 0 {
			t.Errorf("session not deleted from DB")
		}
		testDB.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionId).Scan(&count)
		if count != 0 {
			t.Errorf("messages not deleted from DB")
		}
		testDB.QueryRow("SELECT COUNT(*) FROM branches WHERE session_id = ?", sessionId).Scan(&count)
		if count != 0 {
			t.Errorf("branches not deleted from DB")
		}
	})

	// Test case 2: Session not found
	t.Run("Session Not Found", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/chat/NonExistentDeleteSession", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code for non-existent session: got %v want %v",
				status, http.StatusOK)
		}
	})
}

// TestCountTokensHandler tests the countTokensHandler function
func TestCountTokensHandler(t *testing.T) {
	router, _, ga := setupTest(t)

	// Mock the CountTokens method of CurrentProvider
	mockLLMProvider := CurrentProvider.(*MockLLMProvider)
	mockLLMProvider.CountTokensFunc = func(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
		// Simulate token counting based on input text length
		totalTokens := len(contents[0].Parts[0].Text) / 2 // Example: 2 chars per token
		return &CaCountTokenResponse{TotalTokens: totalTokens}, nil
	}

	// Test case 1: Successful token count
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"text": "This is a test string for token counting."}`)
		req, _ := http.NewRequest("POST", "/api/countTokens", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Temporarily set GlobalGeminiAuth to a valid state for this test
		ga.SelectedAuthType = AuthTypeUseGemini
		ga.ProjectID = "test-project"

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var response map[string]int
		var err error
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}

		expectedTokens := len("This is a test string for token counting.") / 2
		if response["totalTokens"] != expectedTokens {
			t.Errorf("expected %d tokens, got %d", expectedTokens, response["totalTokens"])
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"text": "This is a test string for token counting."`) // Malformed JSON
		req, _ := http.NewRequest("POST", "/api/countTokens", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		ga.SelectedAuthType = AuthTypeUseGemini
		ga.ProjectID = "test-project"

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})

	// Test case 3: Authentication failure
	t.Run("Authentication Failure", func(t *testing.T) {
		payload := []byte(`{"text": "Some text"}`)
		req, _ := http.NewRequest("POST", "/api/countTokens", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Simulate no authentication
		ga.SelectedAuthType = AuthTypeUseGemini // Use AuthTypeUseGemini
		ga.ProjectID = ""                       // Empty ProjectID to trigger auth failure

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusUnauthorized {
			t.Errorf("handler returned wrong status code for auth failure: got %v want %v",
				status, http.StatusUnauthorized)
		}
	})
}

// TestHandleEvaluatePrompt tests the handleEvaluatePrompt function
func TestHandleEvaluatePrompt(t *testing.T) {
	router, _, _ := setupTest(t)

	// Test case 1: Successful template evaluation
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"template": "{{.Builtin.SystemPrompt}}"}`)
		req, _ := http.NewRequest("POST", "/api/evaluatePrompt", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var response map[string]string
		var err error
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}

		if response["evaluatedPrompt"] != GetDefaultSystemPrompt() {
			t.Errorf("expected %q, got %q", GetDefaultSystemPrompt(), response["evaluatedPrompt"])
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"template": "Hello, {{.Name}}!"`) // Malformed JSON
		req, _ := http.NewRequest("POST", "/api/evaluatePrompt", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})
}

// TestGetMCPConfigsHandler tests the getMCPConfigsHandler function
func TestGetMCPConfigsHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare some MCP configs in the DB
	SaveMCPServerConfig(testDB, MCPServerConfig{Name: "mcp1", ConfigJSON: json.RawMessage(`{}`), Enabled: true})
	SaveMCPServerConfig(testDB, MCPServerConfig{Name: "mcp2", ConfigJSON: json.RawMessage(`{}`), Enabled: false})

	req, _ := http.NewRequest("GET", "/api/mcp/configs", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var configs []FrontendMCPConfig
	err := json.Unmarshal(rr.Body.Bytes(), &configs)
	if err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}

	if len(configs) != 2 {
		t.Errorf("expected 2 configs, got %d", len(configs))
	}

	// Check if the configs are present
	foundMcp1 := false
	foundMcp2 := false
	for _, cfg := range configs {
		if cfg.Name == "mcp1" {
			foundMcp1 = true
			if !cfg.Enabled {
				t.Errorf("mcp1 should be enabled")
			}
		}
		if cfg.Name == "mcp2" {
			foundMcp2 = true
			if cfg.Enabled {
				t.Errorf("mcp2 should be disabled")
			}
		}
	}

	if !foundMcp1 || !foundMcp2 {
		t.Errorf("expected MCP configs not found in response")
	}
}

// TestSaveMCPConfigHandler tests the saveMCPConfigHandler function
func TestSaveMCPConfigHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Test case 1: Successful creation/update
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "new-mcp", "config_json": "{}", "enabled": true}`)
		req, _ := http.NewRequest("POST", "/api/mcp/configs", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var response MCPServerConfig
		var err error
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response.Name != "new-mcp" || !response.Enabled {
			t.Errorf("unexpected response: %+v", response)
		}

		// Verify in DB
		var name string
		var enabled bool
		err = testDB.QueryRow("SELECT name, enabled FROM mcp_configs WHERE name = ? ", "new-mcp").Scan(&name, &enabled)
		if err != nil {
			t.Fatalf("failed to query MCP config from DB: %v", err)
		}
		if name != "new-mcp" || !enabled {
			t.Errorf("MCP config in DB mismatch: name=%v, enabled=%v", name, enabled)
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"name": "new-mcp", "config_json": "{}", "enabled": true`) // Malformed JSON
		req, _ := http.NewRequest("POST", "/api/mcp/configs", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("handler returned wrong status code for invalid JSON: got %v want %v",
				status, http.StatusBadRequest)
		}
	})
}

// TestDeleteMCPConfigHandler tests the deleteMCPConfigHandler function
func TestDeleteMCPConfigHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare an MCP config in the DB
	SaveMCPServerConfig(testDB, MCPServerConfig{Name: "mcp-to-delete", ConfigJSON: json.RawMessage(`{}`), Enabled: true})

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/mcp/configs/mcp-to-delete", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var response map[string]string
		var err error
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}

		// Verify in DB
		var count int
		testDB.QueryRow("SELECT COUNT(*) FROM mcp_configs WHERE name = ?", "mcp-to-delete").Scan(&count)
		if count != 0 {
			t.Errorf("MCP config not deleted from DB")
		}
	})

	// Test case 2: Config not found
	t.Run("Not Found", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/mcp/configs/non-existent-mcp", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusInternalServerError { // Or 404 if handled specifically
			t.Errorf("handler returned wrong status code for non-existent config: got %v want %v",
				status, http.StatusInternalServerError)
		}
	})

}

// TestBranchingLogic tests the branching functionality in sessions and messages.
func TestBranchingLogic(t *testing.T) {
	router, testDB, _ := setupTest(t)

	var msgIDToBranchFrom int
	var newBranchID string // Declared here

	// Scenario 1: Linear message flow - chosen_next_id updates correctly
	t.Run("LinearMessageFlow", func(t *testing.T) {
		sessionId := generateID()                                                            // Generate session ID
		primaryBranchID, err := CreateSession(testDB, sessionId, "System prompt", "default") // Updated
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// Message 1
		var msg1ID int
		msg1ID, err = AddMessageToSession(testDB, sessionId, primaryBranchID, nil, nil, "user", "Message 1", "text", nil, nil) // Updated
		if err != nil {
			t.Fatalf("Failed to add message 1: %v", err)
		}

		// Message 2
		var msg2ID int
		msg2ID, err = AddMessageToSession(testDB, sessionId, primaryBranchID, &msg1ID, nil, "model", "Message 2", "text", nil, nil) // Updated
		if err != nil {
			t.Fatalf("Failed to add message 2: %v", err)
		}
		// Update chosen_next_id for Message 1
		if err := UpdateMessageChosenNextID(testDB, msg1ID, &msg2ID); err != nil {
			t.Fatalf("Failed to update chosen_next_id for message 1: %v", err)
		}

		// Message 3
		var msg3ID int
		msg3ID, err = AddMessageToSession(testDB, sessionId, primaryBranchID, &msg2ID, nil, "user", "Message 3", "text", nil, nil) // Updated
		if err != nil {
			t.Fatalf("Failed to add message 3: %v", err)
		}
		// Update chosen_next_id for Message 2
		if err := UpdateMessageChosenNextID(testDB, msg2ID, &msg3ID); err != nil {
			t.Fatalf("Failed to update chosen_next_id for message 2: %v", err)
		}

		// Verify chosen_next_id for Message 1
		var chosenNextID1 sql.NullInt64
		err = testDB.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msg1ID).Scan(&chosenNextID1)
		if err != nil {
			t.Fatalf("Failed to query chosen_next_id for message 1: %v", err)
		}
		if !chosenNextID1.Valid || chosenNextID1.Int64 != int64(msg2ID) {
			t.Errorf("Expected chosen_next_id for message 1 to be %d, got %v", msg2ID, chosenNextID1)
		}

		// Verify chosen_next_id for Message 2
		var chosenNextID2 sql.NullInt64
		err = testDB.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msg2ID).Scan(&chosenNextID2)
		if err != nil {
			t.Fatalf("Failed to query chosen_next_id for message 2: %v", err)
		}
		if !chosenNextID2.Valid || chosenNextID2.Int64 != int64(msg3ID) {
			t.Errorf("Expected chosen_next_id for message 2 to be %d, got %v", msg3ID, chosenNextID2)
		}

		// Verify chosen_next_id for Message 3 (should be empty)
		var chosenNextID3 sql.NullInt64
		err = testDB.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msg3ID).Scan(&chosenNextID3)
		if err != nil {
			t.Fatalf("Failed to query chosen_next_id for message 3: %v", err)
		}
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
		firstMsgID, err := AddMessageToSession(testDB, originalSessionId, originalPrimaryBranchID, nil, nil, "user", "First message", "text", nil, nil)
		if err != nil {
			t.Fatalf("Failed to add first message: %v", err)
		}
		msgIDToBranchFrom, err = AddMessageToSession(testDB, originalSessionId, originalPrimaryBranchID, &firstMsgID, nil, "user", "Message to branch from", "text", nil, nil)
		if err != nil {
			t.Fatalf("Failed to add message to branch from: %v", err)
		}
		// Update chosen_next_id for the first message
		if err := UpdateMessageChosenNextID(testDB, firstMsgID, &msgIDToBranchFrom); err != nil {
			t.Fatalf("Failed to update chosen_next_id for first message: %v", err)
		}

		// Create a new branch using the handler
		payload := []byte(fmt.Sprintf(`{"updatedMessageId": %d, "newMessageText": "First message in new branch"}`, msgIDToBranchFrom))
		req, _ := http.NewRequest("POST", "/api/chat/"+originalSessionId+"/branch", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var response map[string]string
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		newBranchID = response["newBranchId"]
		if newBranchID == "" {
			t.Fatalf("newBranchId not found in response")
		}

		// Verify the new branch in the branches table
		var b Branch
		err = testDB.QueryRow("SELECT id, session_id, parent_branch_id, branch_from_message_id FROM branches WHERE id = ?", newBranchID).Scan(&b.ID, &b.SessionID, &b.ParentBranchID, &b.BranchFromMessageID)
		if err != nil {
			t.Fatalf("Failed to query new branch from branches table: %v", err)
		}
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
		err = testDB.QueryRow("SELECT branch_id, parent_message_id FROM messages WHERE session_id = ? AND branch_id = ? ORDER BY created_at ASC LIMIT 1", originalSessionId, newBranchID).Scan(&firstMsgBranchID, &firstMsgParentID)
		if err != nil {
			t.Fatalf("Failed to query first message in new branch: %v", err)
		}
		if firstMsgBranchID != newBranchID {
			t.Errorf("Expected first message branch ID %s, got %s", newBranchID, firstMsgBranchID)
		}
		if !firstMsgParentID.Valid || firstMsgParentID.Int64 != int64(msgIDToBranchFrom) {
			t.Errorf("Expected first message parent ID %d, got %v", msgIDToBranchFrom, firstMsgParentID)
		}

		// Verify that the original session's primary branch is updated to the new branch
		var updatedPrimaryBranchID string
		err = testDB.QueryRow("SELECT primary_branch_id FROM sessions WHERE id = ?", originalSessionId).Scan(&updatedPrimaryBranchID)
		if err != nil {
			t.Fatalf("Failed to query updated primary branch ID for original session: %v", err)
		}
		if updatedPrimaryBranchID != newBranchID {
			t.Errorf("Expected original session's primary branch ID to be %s, got %s", newBranchID, updatedPrimaryBranchID)
		}

		// Verify chosen_next_id for the message branched from
		var chosenNextID sql.NullInt64
		err = testDB.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", firstMsgID).Scan(&chosenNextID)
		if err != nil {
			t.Fatalf("Failed to query chosen_next_id for branched message: %v", err)
		}
		if !chosenNextID.Valid {
			t.Errorf("Expected chosen_next_id for branched message to be valid, got NULL")
		}
		// Get the ID of the first message in the new branch to compare
		var firstMessageInNewBranchID int
		err = testDB.QueryRow("SELECT id FROM messages WHERE branch_id = ? ORDER BY created_at ASC LIMIT 1", newBranchID).Scan(&firstMessageInNewBranchID)
		if err != nil {
			t.Fatalf("Failed to get ID of first message in new branch: %v", err)
		}
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
		msgIDToBranchFrom, err = AddMessageToSession(testDB, sessionId, originalPrimaryBranchID, nil, nil, "user", "Message to branch from for primary branch test", "text", nil, nil)
		if err != nil {
			t.Fatalf("Failed to add message to branch from for primary branch test: %v", err)
		}
		newBranchID = generateID()
		_, err = CreateBranch(testDB, newBranchID, sessionId, &originalPrimaryBranchID, &msgIDToBranchFrom) // Pass generated ID
		if err != nil {
			t.Fatalf("Failed to create new branch for primary branch test: %v", err)
		}

		// Set the new branch as primary
		payload := []byte(fmt.Sprintf(`{"newPrimaryBranchId": "%s"}`, newBranchID))
		req, _ := http.NewRequest("PUT", "/api/chat/"+sessionId+"/branch", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		// Verify the response body
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
		err = testDB.QueryRow("SELECT primary_branch_id FROM sessions WHERE id = ?", sessionId).Scan(&currentPrimaryBranchID)
		if err != nil {
			t.Fatalf("Failed to query primary branch ID: %v", err)
		}
		if currentPrimaryBranchID != newBranchID {
			t.Errorf("Expected primary branch ID %s, got %s", newBranchID, currentPrimaryBranchID)
		}
	})

}

// TestNewSessionAndMessage_BranchIDConsistency tests that branch IDs are consistent when a new session is created.
func TestNewSessionAndMessage_BranchIDConsistency(t *testing.T) {
	router, testDB, _ := setupTest(t)

	t.Run("BranchIDConsistency", func(t *testing.T) {
		payload := []byte(`{"message": "Initial message for consistency test", "workspaceId": "testWsConsistency"}`)
		req, _ := http.NewRequest("POST", "/api/chat", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		CreateWorkspace(testDB, "testWsConsistency", "Consistency Workspace", "")

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var sessionID string
		var primaryBranchIDFromSession string
		err := testDB.QueryRow("SELECT id, primary_branch_id FROM sessions WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 1", "testWsConsistency").Scan(&sessionID, &primaryBranchIDFromSession)
		if err != nil {
			t.Fatalf("Failed to query session from DB: %v", err)
		}

		// Verify branch in branches table
		var branchIDFromBranches string
		var branchSessionIDFromBranches string
		err = testDB.QueryRow("SELECT id, session_id FROM branches WHERE id = ?", primaryBranchIDFromSession).Scan(&branchIDFromBranches, &branchSessionIDFromBranches)
		if err != nil {
			t.Fatalf("Failed to query branch from branches table: %v", err)
		}
		if branchIDFromBranches != primaryBranchIDFromSession {
			t.Errorf("Expected branch ID from branches table %s, got %s", primaryBranchIDFromSession, branchIDFromBranches)
		}
		if branchSessionIDFromBranches != sessionID {
			t.Errorf("Expected branch session ID from branches table %s, got %s", sessionID, branchSessionIDFromBranches)
		}

		// Verify branch_id of the first message
		var messageBranchID string
		err = testDB.QueryRow("SELECT branch_id FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 1", sessionID).Scan(&messageBranchID)
		if err != nil {
			t.Fatalf("Failed to query first message branch ID from DB: %v", err)
		}
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
		firstMessageID, err := AddMessageToSession(testDB, sessionId, primaryBranchID, nil, nil, "user", "First message for branching test", "text", nil, nil)
		if err != nil {
			t.Fatalf("Failed to add first message: %v", err)
		}
		parentMessageID, err := AddMessageToSession(testDB, sessionId, primaryBranchID, &firstMessageID, nil, "user", "Message to branch from", "text", nil, nil)
		if err != nil {
			t.Fatalf("Failed to add parent message: %v", err)
		}
		// Update chosen_next_id for the first message
		if err := UpdateMessageChosenNextID(testDB, firstMessageID, &parentMessageID); err != nil {
			t.Fatalf("Failed to update chosen_next_id for first message: %v", err)
		}

		// 2. Call createBranchHandler to create a new branch
		payload := []byte(fmt.Sprintf(`{"updatedMessageId": %d, "newMessageText": "First message in new branch"}`, parentMessageID))
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionId+"/branch", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}

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
		err = testDB.QueryRow("SELECT id, session_id, parent_branch_id, branch_from_message_id FROM branches WHERE id = ?", newBranchID).Scan(&branchFromBranches.ID, &branchFromBranches.SessionID, &branchFromBranches.ParentBranchID, &branchFromBranches.BranchFromMessageID)
		if err != nil {
			t.Fatalf("Failed to query new branch from branches table: %v", err)
		}
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
		err = testDB.QueryRow("SELECT id, branch_id, parent_message_id FROM messages WHERE session_id = ? AND branch_id = ? ORDER BY created_at ASC LIMIT 1", sessionId, newBranchID).Scan(&firstMessageInNewBranchID, &firstMessageBranchID, &firstMessageParentID)
		if err != nil {
			t.Fatalf("Failed to query first message in new branch: %v", err)
		}
		if firstMessageBranchID != newBranchID {
			t.Errorf("Expected first message branch ID %s, got %s", newBranchID, firstMessageBranchID)
		}
		if !firstMessageParentID.Valid || firstMessageParentID.Int64 != int64(parentMessageID) {
			t.Errorf("Expected first message parent ID %d, got %v", parentMessageID, firstMessageParentID)
		}

		// 5. Verify chosen_next_id of the parent message
		var chosenNextID sql.NullInt64
		err = testDB.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", firstMessageID).Scan(&chosenNextID)
		if err != nil {
			t.Fatalf("Failed to query chosen_next_id for parent message: %v", err)
		}
		if !chosenNextID.Valid || chosenNextID.Int64 != int64(firstMessageInNewBranchID) {
			t.Errorf("Expected chosen_next_id for parent message to be %d, got %d", firstMessageInNewBranchID, chosenNextID.Int64)
		}
	})
}

// Dummy oauth2.Token for testing
type oauth2Token struct {
	AccessToken  string
	TokenType    string
	RefreshToken string
	Expiry       time.Time
}

func (t *oauth2Token) Valid() bool {
	return t != nil && t.AccessToken != "" && t.Expiry.After(time.Now())
}
