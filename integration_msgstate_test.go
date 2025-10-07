package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mockCloser is already defined in integration_msgchain_test.go

// testProviderWrapper wraps a provider to capture params
type testProviderWrapper struct {
	original      LLMProvider
	captureParams *SessionParams
}

func (w *testProviderWrapper) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	*w.captureParams = params // Capture the SessionParams
	return w.original.SendMessageStream(ctx, params)
}

func (w *testProviderWrapper) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	return w.original.GenerateContentOneShot(ctx, params)
}

func (w *testProviderWrapper) CountTokens(ctx context.Context, contents []Content) (*CaCountTokenResponse, error) {
	return w.original.CountTokens(ctx, contents)
}

func (w *testProviderWrapper) MaxTokens() int {
	return w.original.MaxTokens()
}

func (w *testProviderWrapper) RelativeDisplayOrder() int {
	return w.original.RelativeDisplayOrder()
}

func (w *testProviderWrapper) DefaultGenerationParams() SessionGenerationParams {
	return w.original.DefaultGenerationParams()
}

func (w *testProviderWrapper) SubagentProviderAndParams(task string) (LLMProvider, SessionGenerationParams) {
	return w.original.SubagentProviderAndParams(task)
}

func TestThoughtSignatureHandling(t *testing.T) {
	// Setup test environment
	router, db, _ := setupTest(t)

	// Create responses slice like the working integration tests
	responses := []CaGenerateContentResponse{
		// Thought 1
		{
			Response: VertexGenerateContentResponse{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{Thought: true, Text: "Thinking about a good story..."},
							},
						},
					},
				},
			},
		},
		// Thought 2 + Model Message with ThoughtSignature
		{
			Response: VertexGenerateContentResponse{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{Thought: true, Text: "Almost there..."},
								{Text: "Once upon a time, in a land far, far away...", ThoughtSignature: "test_thought_signature_123"},
							},
						},
					},
				},
			},
		},
		// Continued Model Message
		{
			Response: VertexGenerateContentResponse{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{Text: "lived a brave knight."},
							},
						},
					},
				},
			},
		},
	}

	// Create a mock LLM provider like the working tests
	mockLLM := &MockGeminiProvider{
		Responses: responses,
	}
	CurrentProviders["gemini-2.5-flash"] = mockLLM

	// Simulate user sending the first message
	reqBody := strings.NewReader(`{"message": "Hello LLM, tell me a story."}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", reqBody)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status OK, got %d: %s", rr.Code, rr.Body.String())
	}

	// Parse initial SSE stream to get session ID
	var sessionID string
	var primaryBranchID string
	var firstModelMessageID int
	var secondModelMessageID int

	sseStream := parseSseStream(t, rr.Result())
	for event := range sseStream {
		switch event.Type {
		case EventInitialState:
			var initialState InitialState
			if err := json.Unmarshal([]byte(event.Payload), &initialState); err != nil {
				t.Fatalf("Failed to unmarshal initial state: %v", err)
			}
			sessionID = initialState.SessionId
			primaryBranchID = initialState.PrimaryBranchID
		case EventModelMessage:
			// Parse SSE payload format: "ID\ntext"
			messageIdPart, messageText, _ := strings.Cut(event.Payload, "\n")
			messageID, _ := strconv.Atoi(messageIdPart)

			t.Logf("First stream - EventModelMessage payload: [%q], messageID: %d, messageText: [%q]", event.Payload, messageID, messageText)

			if strings.Contains(messageText, "Once upon a time") {
				firstModelMessageID = messageID
				t.Logf("Set firstModelMessageID to %d", firstModelMessageID)
			} else if strings.Contains(messageText, "lived a brave knight") {
				secondModelMessageID = messageID
				t.Logf("Set secondModelMessageID to %d", secondModelMessageID)
			}
		}
	}

	if sessionID == "" {
		t.Fatal("Session ID not found in initial state")
	}

	testThoughtSignature := "test_thought_signature_123"

	// Verification 1: Check if ThoughtSignature is stored in the database (using GetSessionHistoryContext for LLM context)
	// This is what would be sent to the LLM, so it should include ThoughtSignature
	messagesForLLM, err := GetSessionHistoryContext(db, sessionID, primaryBranchID)
	if err != nil {
		t.Fatalf("Failed to get messages for LLM context: %v", err)
	}

	// Debug: check raw database state column to confirm it's stored
	rows, err := db.Query("SELECT id, type, state FROM messages WHERE session_id = ? ORDER BY id", sessionID)
	if err != nil {
		t.Fatalf("Failed to query raw database: %v", err)
	}
	defer rows.Close()

	t.Logf("Raw database state column:")
	for rows.Next() {
		var id int
		var msgType, state string
		if err := rows.Scan(&id, &msgType, &state); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		t.Logf("  ID: %d, Type: %s, Raw State: '%s'", id, msgType, state)
	}

	// Debug: print LLM context messages
	t.Logf("Messages for LLM context:")
	for _, msg := range messagesForLLM {
		t.Logf("  ID: %s, Type: %s, Parts: %v", msg.ID, msg.Type, msg.Parts)
		if len(msg.Parts) > 0 {
			t.Logf("    First part ThoughtSignature: '%s'", msg.Parts[0].ThoughtSignature)
		}
	}

	foundModelMessageWithSignature := false
	for _, msg := range messagesForLLM {
		if msg.ID == fmt.Sprintf("%d", firstModelMessageID) && msg.Type == TypeModelText {
			foundModelMessageWithSignature = true
			t.Logf("Found model message in LLM context: ID=%s, Parts=%v", msg.ID, msg.Parts)
			if len(msg.Parts) > 0 && msg.Parts[0].ThoughtSignature != testThoughtSignature {
				t.Errorf("Expected model message (ID %s) to have ThoughtSignature '%s', got '%s'", msg.ID, testThoughtSignature, msg.Parts[0].ThoughtSignature)
			}
			break // Since both messages have the same ID, we only need to check once
		}
	}

	if !foundModelMessageWithSignature {
		t.Errorf("Model message (ID %d) not found in LLM context", firstModelMessageID)
	}

	// Scenario 2: User sends a follow-up message, verify ThoughtSignature is sent back to LLM

	// Create a new mock for the second request
	var capturedSessionParams SessionParams
	secondResponses := []CaGenerateContentResponse{
		{
			Response: VertexGenerateContentResponse{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{{Text: "Dummy response."}},
						},
					},
				},
			},
		},
	}

	// Create a custom mock that captures the params
	customMockLLM := &struct {
		responses []CaGenerateContentResponse
	}{
		responses: secondResponses,
	}

	// Create a wrapper provider that captures parameters
	mockLLM2 := &MockGeminiProvider{
		Responses: customMockLLM.responses,
	}

	// Create wrapper provider that captures parameters
	wrapperProvider := &testProviderWrapper{
		original:      mockLLM2,
		captureParams: &capturedSessionParams,
	}

	CurrentProviders["gemini-2.5-flash"] = wrapperProvider

	// Simulate user sending a follow-up message
	reqBody = strings.NewReader(`{"message": "What happened next?"}`)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/chat/%s", sessionID), reqBody)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status OK, got %d: %s", rr.Code, rr.Body.String())
	}

	// Wait for the mock LLM call to complete (optional, but good for ensuring capture)
	time.Sleep(100 * time.Millisecond)

	// Verification 2: Check if ThoughtSignature was sent back to LLM
	foundThoughtSignatureInRequest := false
	for _, content := range capturedSessionParams.Contents {
		for _, part := range content.Parts {
			if part.ThoughtSignature == testThoughtSignature {
				foundThoughtSignatureInRequest = true
				break
			}
		}
		if foundThoughtSignatureInRequest {
			break
		}
	}

	if !foundThoughtSignatureInRequest {
		t.Errorf("Expected ThoughtSignature '%s' to be sent back to LLM, but not found in SessionParams.Contents", testThoughtSignature)
	}
}
