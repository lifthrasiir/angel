package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// Helper function to set up the test environment
func setupTest(t *testing.T) (*mux.Router, *sql.DB, *GeminiAuth, *sync.WaitGroup) {
	wg := &sync.WaitGroup{}

	// Initialize an in-memory database for testing with unique name
	dbName := fmt.Sprintf(":memory:?cache=shared&_txlock=immediate&_foreign_keys=1&_journal_mode=WAL&test=%s", t.Name())
	testDB, err := InitDB(dbName)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Verify that all required tables exist
	requiredTables := []string{"sessions", "messages", "oauth_tokens", "workspaces", "mcp_configs"}
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
	router.Use(makeContextMiddleware(testDB, ga, wg))

	setupRoutes(router, testDB, ga) // Assuming a function to set up all routes

	// Ensure the database connection is closed after the test
	t.Cleanup(func() {
		wg.Wait() // Wait for all goroutines to finish
		if testDB != nil {
			testDB.Close()
		}
		// Restore original provider
		CurrentProvider = originalProvider
	})

	return router, testDB, ga, wg
}

// setupRoutes is a helper to register all API routes to a given router
func setupRoutes(router *mux.Router, db *sql.DB, ga *GeminiAuth) {
	// API handlers (copy from main.go for testing purposes)
	router.HandleFunc("/api/workspaces", createWorkspaceHandler).Methods("POST")
	router.HandleFunc("/api/workspaces", listWorkspacesHandler).Methods("GET")
	router.HandleFunc("/api/workspaces/{id}", deleteWorkspaceHandler).Methods("DELETE")

	router.HandleFunc("/api/chat", listSessionsByWorkspaceHandler).Methods("GET")
	router.HandleFunc("/api/chat", newSessionAndMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", loadChatSession).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}/name", updateSessionNameHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/call", handleCall).Methods("GET", "DELETE")
	router.HandleFunc("/api/chat/{sessionId}", deleteSession).Methods("DELETE")
	router.HandleFunc("/api/userinfo", getUserInfoHandler).Methods("GET")
	router.HandleFunc("/api/logout", ga.HandleLogout).Methods("POST")
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST")
	router.HandleFunc("/api/evaluatePrompt", handleEvaluatePrompt).Methods("POST")
	router.HandleFunc("/api/mcp/configs", getMCPConfigsHandler).Methods("GET")
	router.HandleFunc("/api/mcp/configs", saveMCPConfigHandler).Methods("POST")
	router.HandleFunc("/api/mcp/configs/{name}", deleteMCPConfigHandler).Methods("DELETE")
	router.HandleFunc("/api", handleNotFound)
}

// TestCreateWorkspaceHandler tests the createWorkspaceHandler function
func TestCreateWorkspaceHandler(t *testing.T) {
	router, _, _, _ := setupTest(t)

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
	router, testDB, _, _ := setupTest(t)

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
	router, testDB, _, _ := setupTest(t)

	// Create a workspace and a session/message within it
	workspaceID := "test-ws-delete"
	sessionID := "test-session-delete"
	CreateWorkspace(testDB, workspaceID, "Workspace to Delete", "")
	CreateSession(testDB, sessionID, "System prompt", workspaceID)
	AddMessageToSession(testDB, sessionID, "user", "Hello", "text", nil, nil)

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
	router, testDB, _, _ := setupTest(t)

	// Test case 1: Successful creation of new session and message
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"message": "Hello, world!", "workspaceId": "test-ws-new-session"}`)
		req, _ := http.NewRequest("POST", "/api/chat", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Create a dummy workspace first
		CreateWorkspace(testDB, "test-ws-new-session", "New Session Workspace", "")

		// Verify workspace was created in testDB
		var count int
		testDB.QueryRow("SELECT COUNT(*) FROM workspaces WHERE id = ?", "test-ws-new-session").Scan(&count)
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
		err = testDB.QueryRow("SELECT id FROM sessions WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 1", "test-ws-new-session").Scan(&actualSessionID)
		if err != nil {
			t.Fatalf("failed to query latest session from DB: %v", err)
		}

		err = testDB.QueryRow("SELECT id FROM sessions WHERE id = ?", actualSessionID).Scan(&sessionIDFromDB)
		if err != nil {
			t.Fatalf("failed to query session from DB: %v", err)
		}
		err = testDB.QueryRow("SELECT text FROM messages WHERE session_id = ?", actualSessionID).Scan(&text)
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
	router, testDB, _, _ := setupTest(t)

	// Prepare a session
	sessionID := "test-chat-session"
	CreateSession(testDB, sessionID, "Initial system prompt", "default")

	// Test case 1: Successful addition of a new message
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"message": "Another message"}`)
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionID, bytes.NewBuffer(payload))
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
		err = testDB.QueryRow("SELECT text FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 1", sessionID).Scan(&text)
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
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionID, bytes.NewBuffer(payload))
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
	router, testDB, _, _ := setupTest(t)

	// Prepare a session and some messages
	sessionID := "test-load-session"
	CreateSession(testDB, sessionID, "System prompt for loading", "default")
	AddMessageToSession(testDB, sessionID, "user", "User message 1", "text", nil, nil)
	AddMessageToSession(testDB, sessionID, "model", "Model response 1", "text", nil, nil)

	// Test case 1: Successful session load
	t.Run("Success", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/chat/"+sessionID, nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		var initialState InitialState
		err := json.Unmarshal(rr.Body.Bytes(), &initialState)
		if err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if initialState.SessionId != sessionID {
			t.Errorf("expected session ID %s, got %s", sessionID, initialState.SessionId)
		}
		if len(initialState.History) != 2 {
			t.Errorf("expected 2 messages, got %d", len(initialState.History))
		}
		if initialState.History[0].Parts[0].Text != "User message 1" || initialState.History[1].Parts[0].Text != "Model response 1" {
			t.Errorf("message content mismatch")
		}
	})

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
	router, testDB, _, _ := setupTest(t)

	// Prepare a session
	sessionID := "test-update-name-session"
	CreateSession(testDB, sessionID, "System prompt", "default")

	// Test case 1: Successful name update
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "Updated Session Name"}`)
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionID+"/name", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		// Verify in DB
		var name string
		err := testDB.QueryRow("SELECT name FROM sessions WHERE id = ?", sessionID).Scan(&name)
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
		req, _ := http.NewRequest("POST", "/api/chat/non-existent-session/name", bytes.NewBuffer(payload))
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
		req, _ := http.NewRequest("POST", "/api/chat/"+sessionID+"/name", bytes.NewBuffer(payload))
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
	router, testDB, _, _ := setupTest(t)

	// Prepare a session and some messages
	sessionID := "TestDeleteSession"
	CreateSession(testDB, sessionID, "System prompt for deletion", "default")
	AddMessageToSession(testDB, sessionID, "user", "Message to be deleted", "text", nil, nil)

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/chat/"+sessionID, nil)
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
		testDB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&count)
		if count != 0 {
			t.Errorf("session not deleted from DB")
		}
		testDB.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&count)
		if count != 0 {
			t.Errorf("messages not deleted from DB")
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
	router, _, ga, _ := setupTest(t)

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
		err := json.Unmarshal(rr.Body.Bytes(), &response)
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
	router, _, _, _ := setupTest(t)

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
		err := json.Unmarshal(rr.Body.Bytes(), &response)
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
	router, testDB, _, _ := setupTest(t)

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
	router, testDB, _, _ := setupTest(t)

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
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response.Name != "new-mcp" || !response.Enabled {
			t.Errorf("unexpected response: %+v", response)
		}

		// Verify in DB
		var name string
		var enabled bool
		err = testDB.QueryRow("SELECT name, enabled FROM mcp_configs WHERE name = ?", "new-mcp").Scan(&name, &enabled)
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
	router, testDB, _, _ := setupTest(t)

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
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}

		// Verify deletion in DB
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
