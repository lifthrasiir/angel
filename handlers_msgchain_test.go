package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// MockGeminiProvider for testing streamGeminiResponse
type MockGeminiProvider struct {
	Responses        []CaGenerateContentResponse
	Err              error
	Delay            time.Duration
	ExtraDelayIndex  int           // Index at which to apply extra delay
	ExtraDelayAmount time.Duration // Amount of extra delay to apply
}

func (m *MockGeminiProvider) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	if m.Err != nil {
		return nil, nil, m.Err
	}

	ch := make(chan CaGenerateContentResponse)
	closer := func() { close(ch) }

	seq := func(yield func(CaGenerateContentResponse) bool) {
		defer closer()
		for i, resp := range m.Responses {
			// Simulate streaming delay before sending each response (except the first)
			if i > 0 {
				if m.Delay > 0 {
					time.Sleep(m.Delay)
				} else {
					time.Sleep(10 * time.Millisecond)
				}
			}

			// Apply extra delay at specified index
			if i == m.ExtraDelayIndex && m.ExtraDelayAmount > 0 {
				log.Printf("MockGeminiProvider: Applying extra delay of %v at index %d", m.ExtraDelayAmount, i)
				time.Sleep(m.ExtraDelayAmount)
			}

			select {
			case <-ctx.Done():
				log.Printf("MockGeminiProvider: Context cancelled at index %d", i)
				return
			default:
				log.Printf("MockGeminiProvider: Sending response at index %d: %+v", i, resp)
				if !yield(resp) {
					log.Printf("MockGeminiProvider: yield returned false at index %d", i)
					return
				}
			}
		}
	}
	return seq, &mockCloser{}, nil // Return a mock io.Closer
}

func (m *MockGeminiProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	return OneShotResult{Text: "inferred name"}, nil
}

func (m *MockGeminiProvider) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	return &CaCountTokenResponse{TotalTokens: 10}, nil
}

// MaxTokens is a placeholder for angel-eval.
func (m *MockGeminiProvider) MaxTokens() int {
	return 1024 // A reasonable default for a simple eval model
}

// RelativeDisplayOrder implements the LLMProvider interface for MockGeminiProvider.
func (m *MockGeminiProvider) RelativeDisplayOrder() int {
	return 0
}

// DefaultGenerationParams implements the LLMProvider interface for MockGeminiProvider.
func (m *MockGeminiProvider) DefaultGenerationParams() SessionGenerationParams {
	return SessionGenerationParams{}
}

// mockCloser implements io.Closer for testing purposes
type mockCloser struct{}

func (mc *mockCloser) Close() error {
	return nil
}

func printMessages(t *testing.T, db *sql.DB, testName string) {
	t.Logf("--- Messages in DB after %s ---", testName)
	rows, err := db.Query("SELECT id, role, parent_message_id, chosen_next_id, text FROM messages ORDER BY id ASC")
	if err != nil {
		t.Fatalf("Failed to query messages: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var role string
		var parentMessageID sql.NullInt64
		var chosenNextID sql.NullInt64
		var text string
		if err := rows.Scan(&id, &role, &parentMessageID, &chosenNextID, &text); err != nil {
			t.Fatalf("Failed to scan message: %v", err)
		}
		parent := "nil"
		if parentMessageID.Valid {
			parent = fmt.Sprintf("%d", parentMessageID.Int64)
		}
		chosen := "nil"
		if chosenNextID.Valid {
			chosen = fmt.Sprintf("%d", chosenNextID.Int64)
		}
		t.Logf("ID: %d, Role: %s, Parent: %s, ChosenNext: %s, Text: %s", id, role, parent, chosen, text)
	}
	t.Log("------------------------------------")
}

func responseFromPart(part Part) CaGenerateContentResponse {
	return CaGenerateContentResponse{
		Response: VertexGenerateContentResponse{
			Candidates: []Candidate{
				{
					Content: Content{
						Parts: []Part{part},
					},
				},
			},
		},
	}
}

func TestMessageChainWithThoughtAndModel(t *testing.T) {
	// Setup router and context middleware
	router, db, _ := setupTest(t)

	// Create the workspace for this test
	err := CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Setup Mock Gemini Provider
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for B (thought, model)
			responseFromPart(Part{Text: "**Thinking**\nThis is a thought.", Thought: true}),
			responseFromPart(Part{Text: "This is the model's response."}),
		},
	})
	defer replaceProvider(provider)

	// 1. Start new session with initial user message
	initialUserMessage := "Hello, Gemini!"
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	body, _ := json.Marshal(reqBody)
	rr := testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
	defer rr.Body.Close()

	printMessages(t, db, "After first message chain") // ADDED

	// Parse SSE events to get session ID and message IDs
	var sessionId string
	var firstUserMessageID int
	var thoughtMessageID int
	var modelMessageID int

	for event := range parseSseStream(t, rr) {
		switch event.Type {
		case EventAcknowledge:
			firstUserMessageID, _ = strconv.Atoi(event.Payload)
		case EventInitialState:
			var initialState InitialState
			err := json.Unmarshal([]byte(event.Payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
		case EventThought:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			thoughtMessageID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			modelMessageID, _ = strconv.Atoi(messageIdPart)
		}
	}

	if sessionId == "" {
		t.Fatal("Session ID not found in SSE events")
	}
	if firstUserMessageID == 0 {
		t.Fatal("First user message ID not found in SSE events")
	}
	if thoughtMessageID == 0 {
		t.Fatal("Thought message ID not found in SSE events")
	}
	if modelMessageID == 0 {
		t.Fatal("Model message ID not found in SSE events")
	}

	// Verify initial message chain
	// User (firstUserMessageID) -> Thought (thoughtMessageID) -> Model (modelMessageID)
	var msg Message
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{firstUserMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID != nil {
		t.Errorf("Expected first user message parent_message_id to be nil, got %d", *msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != thoughtMessageID {
		t.Errorf("Expected first user message chosen_next_id to be %d, got %v", thoughtMessageID, msg.ChosenNextID)
	}

	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{thoughtMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != firstUserMessageID {
		t.Errorf("Expected thought message parent_message_id to be %d, got %v", firstUserMessageID, msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != modelMessageID {
		t.Errorf("Expected thought message chosen_next_id to be %d, got %v", modelMessageID, msg.ChosenNextID)
	}

	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{modelMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != thoughtMessageID {
		t.Errorf("Expected model message parent_message_id to be %d, got %v", thoughtMessageID, msg.ParentMessageID)
	}
	if msg.ChosenNextID != nil { // Should be nil as it's the end of the stream
		t.Errorf("Expected model message chosen_next_id to be nil, got %d", *msg.ChosenNextID)
	}

	// 2. Send another user message
	printMessages(t, db, "Before second user message") // ADDED
	secondUserMessage := "How are you?"
	reqBody = map[string]interface{}{
		"message": secondUserMessage,
	}
	body, _ = json.Marshal(reqBody)
	rr = testStreamingRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s", sessionId), body, http.StatusOK)
	defer rr.Body.Close()

	// Parse SSE events for the second user message ID and second thought message ID
	var secondUserMessageID int
	var secondThoughtMessageID int
	var secondModelMessageID int
	for event := range parseSseStream(t, rr) {
		switch event.Type {
		case EventAcknowledge:
			secondUserMessageID, _ = strconv.Atoi(event.Payload)
		case EventThought:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			secondThoughtMessageID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			secondModelMessageID, _ = strconv.Atoi(messageIdPart)
		}
	}

	if secondUserMessageID == 0 {
		t.Fatal("Second user message ID not found in SSE events")
	}
	if secondThoughtMessageID == 0 {
		t.Fatal("Second thought message ID not found in SSE events")
	}

	// Verify chain after second user message
	// Model (modelMessageID) -> User (secondUserMessageID) -> Thought (secondThoughtMessageID)
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{modelMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondUserMessageID {
		t.Errorf("Expected model message chosen_next_id to be %d, got %v", secondUserMessageID, msg.ChosenNextID)
	}

	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{secondUserMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != modelMessageID {
		t.Errorf("Expected second user message parent_message_id to be %d, got %v", modelMessageID, msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondThoughtMessageID {
		t.Errorf("Expected second user message chosen_next_id to be %d, got %v", secondThoughtMessageID, msg.ChosenNextID)
	}

	// Verify the second thought message
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{secondThoughtMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != secondUserMessageID {
		t.Errorf("Expected second thought message parent_message_id to be %d, got %v", secondUserMessageID, msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondModelMessageID {
		t.Errorf("Expected second thought message chosen_next_id to be %d, got %v", secondModelMessageID, msg.ChosenNextID)
	}
}

func TestBranchingMessageChain(t *testing.T) {
	// Setup router and context middleware
	router, db, _ := setupTest(t)

	// Create the workspace for this test
	err := CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// i) Create three messages: A-B-C
	// 1. Send initial user message (A)
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for B (thought, model)
			responseFromPart(Part{Text: "B's thought", Thought: true}),
			responseFromPart(Part{Text: "B's response"}),
		},
	})
	defer replaceProvider(provider)
	msgA1Text := "Message A"
	reqBodyA1 := map[string]interface{}{
		"message":      msgA1Text,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	bodyA1, _ := json.Marshal(reqBodyA1)
	rrA1 := testStreamingRequest(t, router, "POST", "/api/chat", bodyA1, http.StatusOK)
	defer rrA1.Body.Close()

	var sessionId string
	var msgA1ID int // A1: User message
	var msgA2ID int // A2: Thought message
	var msgA3ID int // A3: Model message
	var originalPrimaryBranchID string

	for event := range parseSseStream(t, rrA1) {
		switch event.Type {
		case EventAcknowledge:
			msgA1ID, _ = strconv.Atoi(event.Payload)
		case EventInitialState:
			var initialState InitialState
			err := json.Unmarshal([]byte(event.Payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
			originalPrimaryBranchID = initialState.PrimaryBranchID
		case EventThought:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			msgA2ID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			msgA3ID, _ = strconv.Atoi(messageIdPart)
		}
	}

	if sessionId == "" || msgA1ID == 0 || msgA2ID == 0 || msgA3ID == 0 || originalPrimaryBranchID == "" {
		t.Fatalf("Failed to get all IDs for A1-A3 chain. SessionID: %s, MsgA1ID: %d, MsgA2ID: %d, MsgA3ID: %d, OriginalPrimaryBranchID: %s", sessionId, msgA1ID, msgA2ID, msgA3ID, originalPrimaryBranchID)
	}
	printMessages(t, db, "After A1-A3 chain")

	// 2. Send second user message (C)
	replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for B (thought, model)
			responseFromPart(Part{Text: "B's thought", Thought: true}),
			responseFromPart(Part{Text: "B's response"}),
		},
	})
	userMessageB1 := "Message B"
	reqBodyB1 := map[string]interface{}{
		"message": userMessageB1,
	}
	bodyB1, _ := json.Marshal(reqBodyB1)
	rrB1 := testStreamingRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s", sessionId), bodyB1, http.StatusOK)
	defer rrB1.Body.Close()

	var msgB1ID int // B1: User message
	var msgB2ID int // B2: Thought message
	var msgB3ID int // B3: Model message

	for event := range parseSseStream(t, rrB1) {
		switch event.Type {
		case EventAcknowledge:
			msgB1ID, _ = strconv.Atoi(event.Payload)
		case EventThought:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			msgB2ID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
			msgB3ID, _ = strconv.Atoi(messageIdPart)
		}
	}
	if msgB1ID == 0 || msgB2ID == 0 || msgB3ID == 0 {
		t.Fatalf("Failed to get all IDs for B chain. MsgB1ID: %d, MsgB2ID: %d, MsgB3ID: %d", msgB1ID, msgB2ID, msgB3ID)
	}
	printMessages(t, db, "After A1-A3-B1-B3 chain")

	// Verify A1-A3-B1-B3 chain in DB
	var msg Message
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA1ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID != nil {
		t.Errorf("Expected first user message parent_message_id to be nil, got %d", *msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgA2ID {
		t.Errorf("Expected first user message chosen_next_id to be %d, got %v", msg.ChosenNextID, msgA2ID)
	}
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA2ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgA3ID {
		t.Errorf("Expected A2's chosen_next_id to be A3 (model). Got %v, want %d", msg.ChosenNextID, msgA3ID)
	}
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA3ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB1ID {
		t.Errorf("Expected A3's chosen_next_id to be B1 (user). Got %v, want %d", msg.ChosenNextID, msgB1ID)
	}
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{msgB1ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB2ID {
		t.Errorf("Expected B1's chosen_next_id to be B2 (thought). Got %v, want %d", msg.ChosenNextID, msgB2ID)
	}
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{msgB2ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB3ID {
		t.Errorf("Expected B2's chosen_next_id to be B3 (model). Got %v, want %d", msg.ChosenNextID, msgB3ID)
	}
	querySingleRow(t, db, "SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", []interface{}{msgB3ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID != nil {
		t.Errorf("Expected B3's chosen_next_id to be nil. Got %v", msg.ChosenNextID)
	}

	// ii) Create a new branch C1-C2-C3 after A3 (Model A)
	// Create a new branch from message A3 (Model A)
	replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for C (thought, model)
			responseFromPart(Part{Text: "C's thought", Thought: true}),
			responseFromPart(Part{Text: "C's response"}),
		},
	})
	msgC1Text := "Message C"
	reqBodyC1 := map[string]interface{}{
		"updatedMessageId": msgB1ID, // Branching from A3, updating B1
		"newMessageText":   msgC1Text,
	}
	bodyC1, _ := json.Marshal(reqBodyC1)
	rrC1 := testRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s/branch", sessionId), bodyC1, http.StatusOK)

	var branchResponseC map[string]string
	err = json.Unmarshal(rrC1.Body.Bytes(), &branchResponseC)
	if err != nil {
		t.Fatalf("Failed to unmarshal branch response: %v", err)
	}
	newBranchCID := branchResponseC["newBranchId"]
	msgC1ID, _ := strconv.Atoi(branchResponseC["newMessageId"]) // C1: User message
	if newBranchCID == "" || msgC1ID == 0 {
		t.Fatalf("Failed to get newBranchCID or msgC1ID. NewBranchCID: %s, MsgC1ID: %d", newBranchCID, msgC1ID)
	}
	printMessages(t, db, "After A1-A3-C1 branch creation")

	// Verify that the new branch is now the primary branch
	var currentPrimaryBranchID string
	querySingleRow(t, db, "SELECT primary_branch_id FROM sessions WHERE id = ?", []interface{}{sessionId}, &currentPrimaryBranchID)
	if currentPrimaryBranchID != newBranchCID {
		t.Errorf("Primary branch not updated to new branch. Got %s, want %s", currentPrimaryBranchID, newBranchCID)
	}

	// Simulate streaming response for Message C (thought and model)
	replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for C (thought, model)
			responseFromPart(Part{Text: "C's thought", Thought: true}),
			responseFromPart(Part{Text: "C's response"}),
		},
	})

	// Prepare initial state for streaming for the new branch
	// Prepare initial state for streaming for the new branch
	initialStateCStream := InitialState{
		SessionId:       sessionId,
		History:         []FrontendMessage{},
		SystemPrompt:    "You are a helpful assistant.",
		WorkspaceID:     "testWorkspace",
		PrimaryBranchID: newBranchCID,
		Roots:           []string{},
	}

	// Create a dummy SSE writer for streamGeminiResponse
	rrDummy := httptest.NewRecorder()
	reqDummy := httptest.NewRequest("GET", "/", nil)
	dummySseW := newSseWriter(sessionId, rrDummy, reqDummy)
	if dummySseW == nil {
		t.Fatalf("Failed to create dummy SSE writer")
	}
	defer dummySseW.Close()

	// Call streamGeminiResponse to add thought and model messages for C
	if err := streamGeminiResponse(db, initialStateCStream, dummySseW, msgC1ID, DefaultGeminiModel, false, false, time.Now(), []FrontendMessage{}); err != nil {
		t.Fatalf("Error streaming Gemini response for C: %v", err)
	}

	// Verify that EventInitialState was NOT sent
	for event := range parseSseStream(t, rrDummy.Result()) {
		if event.Type == EventInitialState || event.Type == EventInitialStateNoCall {
			t.Errorf("Expected NO EventInitialState or EventInitialStateNoCall, but received %c", event.Type)
		}
	}

	printMessages(t, db, "After A1-A3-C1-C3 branch creation")

	// iii) Verify that the history correctly includes C1-C2-C3 and that A3 has both B1 and C1 as next messages.
	// Load history for C1-C2-C3 branch
	rrLoadC := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer rrLoadC.Body.Close()

	var initialStateC InitialState
	for event := range parseSseStream(t, rrLoadC) {
		if event.Type == EventInitialState || event.Type == EventInitialStateNoCall {
			err = json.Unmarshal([]byte(event.Payload), &initialStateC)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState for C1-C2-C3: %v", err)
			}
			break // Only need the initial state
		}
	}

	// Get msgC2ID and msgC3ID from DB after streaming
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgC1ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil {
		t.Fatalf("Failed to get chosen_next_id for msgC1ID %d: %v", msgC1ID, err)
	}
	msgC2ID := int(*msg.ChosenNextID)

	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgC2ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil {
		t.Fatalf("Failed to get chosen_next_id for msgC2ID %d: %v", msgC2ID, err)
	}
	msgC3ID := int(*msg.ChosenNextID)

	// History should be A1 (user), A2 (thought), A3 (model), C1 (user), C2 (thought), C3 (model)
	if len(initialStateC.History) != 6 { // Expected 6 messages (A1-A3 + C1-C3)
		t.Fatalf("Expected C1-C2-C3 history length 6, got %d", len(initialStateC.History))
	}
	if initialStateC.History[0].ID != fmt.Sprintf("%d", msgA1ID) ||
		initialStateC.History[1].ID != fmt.Sprintf("%d", msgA2ID) ||
		initialStateC.History[2].ID != fmt.Sprintf("%d", msgA3ID) ||
		initialStateC.History[3].ID != fmt.Sprintf("%d", msgC1ID) ||
		initialStateC.History[4].ID != fmt.Sprintf("%d", msgC2ID) ||
		initialStateC.History[5].ID != fmt.Sprintf("%d", msgC3ID) {
		t.Errorf("C1-C2-C3 history mismatch. Got %v", initialStateC.History)
	}

	// Verify A3's chosen_next_id is C1 (user C)
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA3ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgC1ID {
		t.Errorf("A3's chosen_next_id is not C1 (user C). Got %v, want %d", msg.ChosenNextID, msgC1ID)
	}

	// Verify A3's parent (A3) has both B1 and C1 as possible next messages (by checking branches table)
	var count int
	querySingleRow(t, db, "SELECT COUNT(*) FROM branches WHERE session_id = ? AND branch_from_message_id = ?", []interface{}{sessionId, msgA3ID}, &count)
	if count != 1 { // Only the new C branch should have A3 as parent
		t.Errorf("Expected 1 branch from A3, got %d", count)
	}

	// iv) Switch back to the A1-A3-B1-B3 branch
	// Switch back to A1-A3-B1-B3 branch
	reqBodySwitch := map[string]interface{}{
		"newPrimaryBranchId": originalPrimaryBranchID,
	}
	bodySwitch, _ := json.Marshal(reqBodySwitch)
	testRequest(t, router, "PUT", fmt.Sprintf("/api/chat/%s/branch", sessionId), bodySwitch, http.StatusOK)
	printMessages(t, db, "After switching back to A1-A3-B1-B3 branch")

	// v) Verify that the history has changed to A1-A3-B1-B3
	// Load history for A1-A3-B1-B3 branch
	rrLoadA1B3 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer rrLoadA1B3.Body.Close()

	var initialStateA1B3 InitialState
	for event := range parseSseStream(t, rrLoadA1B3) {
		if event.Type == EventInitialState || event.Type == EventInitialStateNoCall {
			err := json.Unmarshal([]byte(event.Payload), &initialStateA1B3)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState for A1-A3-B1-B3: %v", err)
			}
			break
		}
	}

	// History should be A1 (user), A2 (thought), A3 (model), B1 (user), B2 (thought), B3 (model)
	if len(initialStateA1B3.History) != 6 {
		t.Errorf("Expected A1-A3-B1-B3 history length 6, got %d", len(initialStateA1B3.History))
	}
	if initialStateA1B3.History[0].ID != fmt.Sprintf("%d", msgA1ID) ||
		initialStateA1B3.History[1].ID != fmt.Sprintf("%d", msgA2ID) ||
		initialStateA1B3.History[2].ID != fmt.Sprintf("%d", msgA3ID) ||
		initialStateA1B3.History[3].ID != fmt.Sprintf("%d", msgB1ID) ||
		initialStateA1B3.History[4].ID != fmt.Sprintf("%d", msgB2ID) ||
		initialStateA1B3.History[5].ID != fmt.Sprintf("%d", msgB3ID) {
		t.Errorf("A1-A3-B1-B3 history mismatch. Got %v", initialStateA1B3.History)
	}
}

func TestStreamingMessageConsolidation(t *testing.T) {
	// Setup router and context middleware
	router, db, _ := setupTest(t)

	// Create the workspace for this test
	err := CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Setup Mock Gemini Provider to stream "A", "B", "C" and then complete
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B"}),
			responseFromPart(Part{Text: "C"}),
		},
	})
	defer replaceProvider(provider)

	// 1. Start new session with initial user message
	initialUserMessage := "Test streaming consolidation."
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	body, _ := json.Marshal(reqBody)
	rr := testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
	defer rr.Body.Close()

	var sessionId string
	var userMessageID int
	var modelMessageID int
	var receivedModelMessage string

	// Parse SSE events
	for event := range parseSseStream(t, rr) {
		switch event.Type {
		case EventAcknowledge:
			userMessageID, _ = strconv.Atoi(event.Payload)
		case EventInitialState:
			var initialState InitialState
			err := json.Unmarshal([]byte(event.Payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
		case EventModelMessage:
			messageIdPart, messageText, _ := strings.Cut(event.Payload, "\n")
			currentModelMessageID, _ := strconv.Atoi(messageIdPart)
			if modelMessageID == 0 { // First model message
				modelMessageID = currentModelMessageID
			} else if modelMessageID != currentModelMessageID { // Subsequent model messages must have the same ID
				t.Errorf("Expected model message ID to be %d, got %d", modelMessageID, currentModelMessageID)
			}
			receivedModelMessage += messageText
		case EventComplete:
			// Stream complete, check final message content and ID
			if receivedModelMessage != "ABC" {
				t.Errorf("Expected consolidated message 'ABC', got '%s'", receivedModelMessage)
			}

			// Verify the message in the database
			var msg Message
			querySingleRow(t, db, "SELECT text FROM messages WHERE id = ?", []interface{}{modelMessageID}, &msg.Text)
			if msg.Text != "ABC" {
				t.Errorf("Expected message in DB to be 'ABC', got '%s'", msg.Text)
			}
		}
	}

	if sessionId == "" {
		t.Fatal("Session ID not found in SSE events")
	}
	if userMessageID == 0 {
		t.Fatal("User message ID not found in SSE events")
	}
	if modelMessageID == 0 {
		t.Fatal("Model message ID not found in SSE events")
	}
}

func TestSyncDuringThought(t *testing.T) {
	router, db, _ := setupTest(t) // Get db from setupTest

	// Create the workspace for this test
	err := CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider: A, B (thought), C (thought), D, E, F
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B", Thought: true}),
			responseFromPart(Part{Text: "C", Thought: true}),
			responseFromPart(Part{Text: "D"}),
			responseFromPart(Part{Text: "E"}),
			responseFromPart(Part{Text: "F"}),
		},
		Delay: 50 * time.Millisecond, // Faster for robust testing
	})
	defer replaceProvider(provider)

	// 1. Start new session with initial user message
	initialUserMessage := "Hello, Gemini!"
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	body, _ := json.Marshal(reqBody)

	// Use a channel to signal when the first stream has received 'B'
	stream1Ready := make(chan bool)

	var sessionId string

	// First client connection (simulated)
	go func() {
		rr1 := testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
		defer rr1.Body.Close()

		receivedB := false
		receivedComplete := false
		for event := range parseSseStream(t, rr1) {
			switch event.Type {
			case EventAcknowledge:
				// Expected but do nothing
			case EventInitialState:
				var initialState InitialState
				err := json.Unmarshal([]byte(event.Payload), &initialState)
				if err != nil {
					t.Errorf("Failed to unmarshal initialState: %v", err)
					stream1Ready <- false
					return
				}
				sessionId = initialState.SessionId
			case EventThought:
				_, messageText, _ := strings.Cut(event.Payload, "\n")
				if messageText == "Thinking...\nB" {
					receivedB = true
				}
			case EventModelMessage:
				// Expected but do nothing
			case EventComplete:
				receivedComplete = true
			case EventSessionName:
				// Expected but do nothing
			default:
				t.Errorf("Unexpected event type: %c", event.Type)
				stream1Ready <- false
				return
			}
		}
		if !receivedB {
			t.Errorf("First client did not receive B")
		}
		if !receivedComplete {
			t.Errorf("First client did not receive EventComplete")
		}
		stream1Ready <- receivedB && receivedComplete
	}()

	// Wait for the first stream to receive 'B'
	select {
	case ok := <-stream1Ready:
		if !ok {
			return
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for first stream to receive 'B'")
	}

	// 2. Second client connects while streaming is ongoing (after B)
	rr2 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)

	var initialState2 InitialState
	receivedModelMessage2 := ""
	receivedThoughtMessage2 := ""

	// Verify initial state for second client
	for event := range parseSseStream(t, rr2) {
		defer rr2.Body.Close()
		switch event.Type {
		case EventInitialState:
			err := json.Unmarshal([]byte(event.Payload), &initialState2)
			if err != nil {
				t.Errorf("Failed to unmarshal initialState: %v", err)
				return
			}
			// Check if A and B are in the initial state
			if len(initialState2.History) < 4 ||
				initialState2.History[0].Parts[0].Text != initialUserMessage ||
				initialState2.History[1].Parts[0].Text != "A" ||
				initialState2.History[2].Parts[0].Text != "Thinking...\nB" {
				t.Errorf("Second client initial state missing A, B, or C. History: %+v", initialState2.History)
			}
			for _, message := range initialState2.History[1:] {
				if message.Parts[0].Thought {
					receivedThoughtMessage2 += message.Parts[0].Text
				} else {
					receivedModelMessage2 += message.Parts[0].Text
				}
			}
		case EventInitialStateNoCall:
			err := json.Unmarshal([]byte(event.Payload), &initialState2)
			if err != nil {
				t.Errorf("Failed to unmarshal initialState: %v", err)
				return
			}
			// Check if all messages are in the initial state
			if len(initialState2.History) < 5 ||
				initialState2.History[0].Parts[0].Text != initialUserMessage ||
				initialState2.History[1].Parts[0].Text != "A" ||
				initialState2.History[2].Parts[0].Text != "Thinking...\nB" ||
				initialState2.History[3].Parts[0].Text != "Thinking...\nC" ||
				initialState2.History[4].Parts[0].Text != "DEF" {
				t.Errorf("Second client initial state missing A, B, or C. History: %+v", initialState2.History)
			}
			return
		case EventThought:
			_, messageText, _ := strings.Cut(event.Payload, "\n")
			receivedThoughtMessage2 += messageText
		case EventModelMessage:
			_, messageText, _ := strings.Cut(event.Payload, "\n")
			receivedModelMessage2 += messageText
		case EventComplete:
			// Check if D, E, F are streamed to the second client
			if receivedThoughtMessage2 != "Thinking...\nBThinking\nC" {
				t.Errorf("Second client received unexpected thought. Got: %s", receivedThoughtMessage2)
			}
			if receivedModelMessage2 != "DEF" {
				t.Errorf("Second client did not receive model DEF. Got: %s", receivedModelMessage2)
			}
			return
		default:
			t.Errorf("Unexpected event type: %c", event.Type)
			return
		}
		t.Errorf("Second client did not receive EventComplete")
	}
}

func TestSyncDuringResponse(t *testing.T) {
	router, db, _ := setupTest(t) // Get db from setupTest

	// Create the workspace for this test
	err := CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider: A, B (thought), C (thought), D, E, F
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B", Thought: true}),
			responseFromPart(Part{Text: "C", Thought: true}),
			responseFromPart(Part{Text: "D"}),
			responseFromPart(Part{Text: "E"}),
			responseFromPart(Part{Text: "F"}),
		},
		Delay: 50 * time.Millisecond, // Faster for robust testing
	})
	defer replaceProvider(provider)

	// 1. Start new session with initial user message
	initialUserMessage := "Hello, Gemini!"
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	body, _ := json.Marshal(reqBody)

	// Use a channel to signal when the first stream has received 'E'
	stream1Ready := make(chan struct{})
	stream1Finished := make(chan struct{})

	var sessionId string

	receivedModelMessage1 := ""

	// First client connection (simulated)
	go func() {
		rr1 := testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
		defer rr1.Body.Close()

		receivedE := false
		receivedComplete := false
		for event := range parseSseStream(t, rr1) {
			switch event.Type {
			case EventAcknowledge:
				// Expected but do nothing
			case EventInitialState:
				var initialState InitialState
				err := json.Unmarshal([]byte(event.Payload), &initialState)
				if err != nil {
					t.Errorf("Failed to unmarshal initialState: %v", err)
					close(stream1Finished)
					return
				}
				sessionId = initialState.SessionId
			case EventThought:
				// Expected but do nothing
			case EventModelMessage:
				_, messageText, _ := strings.Cut(event.Payload, "\n")
				receivedModelMessage1 += messageText
				if strings.Contains(messageText, "E") {
					receivedE = true
					close(stream1Ready)
				}
			case EventComplete:
				receivedComplete = true
			case EventSessionName:
				// Expected but do nothing
			default:
				t.Errorf("Unexpected event type: %c", event.Type)
				close(stream1Finished)
				return
			}
		}
		if !receivedE {
			t.Errorf("First client did not receive 'E'")
		}
		if !receivedComplete {
			t.Errorf("First client did not receive EventComplete")
		}
		close(stream1Finished)
	}()

	// Wait for the first stream to receive 'E'
	select {
	case <-stream1Ready:
		// Continue
	case <-stream1Finished:
		return
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for first stream to receive 'E'")
	}

	// 2. Second client connects while streaming is ongoing (after E)
	rr2 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer rr2.Body.Close()

	var initialState2 InitialState
	receivedModelMessage2 := ""

	// Verify initial state for second client
	for event := range parseSseStream(t, rr2) {
		switch event.Type {
		case EventInitialState, EventInitialStateNoCall:
			err := json.Unmarshal([]byte(event.Payload), &initialState2)
			if err != nil {
				t.Errorf("Failed to unmarshal initialState for second client: %v", err)
				return
			}
			expectedModelMessage := "DE"
			if event.Type == EventInitialStateNoCall {
				// If we've got EventInitialStateNoCall (possible due to the timing),
				// Every model message should have been merged into one.
				expectedModelMessage += "F"
			}
			// Check if A, B, C, D+E+(F) are in the initial state (including thought messages)
			if len(initialState2.History) < 5 ||
				initialState2.History[0].Parts[0].Text != initialUserMessage ||
				initialState2.History[1].Parts[0].Text != "A" ||
				initialState2.History[2].Parts[0].Text != "Thinking...\nB" ||
				initialState2.History[3].Parts[0].Text != "Thinking...\nC" ||
				initialState2.History[4].Parts[0].Text != expectedModelMessage {
				t.Errorf("Second client initial state missing A, B, C, D, E or (possibly) F. History: %+v", initialState2.History)
			}
		case EventModelMessage:
			_, messageText, _ := strings.Cut(event.Payload, "\n")
			receivedModelMessage2 += messageText
		case EventComplete:
			// Check if F is streamed to the second client
			if receivedModelMessage2 == "F" {
				select {
				case <-stream1Finished:
					// Continue
				case <-time.After(time.Second):
					t.Fatal("Timeout waiting for first stream to receive EventComplete")
				}
			} else {
				t.Errorf("Second client did not receive model F. Got: %s", receivedModelMessage2)
			}
			return
		case EventSessionName:
			// Expected but do nothing
		default:
			t.Errorf("Unexpected event type: %c", event.Type)
			return
		}
	}
	t.Errorf("Second client did not receive EventComplete")
}

func TestCancelDuringSync(t *testing.T) {
	router, db, _ := setupTest(t) // Get db from setupTest

	// Create the workspace for this test
	err := CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider: A, B (thought), C (thought), D, E, F
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B", Thought: true}),
			responseFromPart(Part{Text: "C", Thought: true}),
			responseFromPart(Part{Text: "D"}),
			responseFromPart(Part{Text: "E"}),
			responseFromPart(Part{Text: "F"}),
		},
		Delay:            200 * time.Millisecond, // Simulate streaming delay
		ExtraDelayIndex:  5,
		ExtraDelayAmount: 500 * time.Millisecond, // Long enough for cancellation
	})
	defer replaceProvider(provider)

	// 1. Start new session with initial user message
	initialUserMessage := "Hello, Gemini!"
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "testWorkspace",
	}
	body, _ := json.Marshal(reqBody)

	// Use a channel to signal when the first stream has received 'B'
	stream1Ready := make(chan struct{})
	stream1Finished := make(chan struct{})

	var sessionId string

	// First client connection (simulated)
	go func() {
		rr1 := testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
		defer rr1.Body.Close()

		receivedB := false
		receivedCompleteOrError := false
		for event := range parseSseStream(t, rr1) {
			switch event.Type {
			case EventAcknowledge:
				// Expected but do nothing
			case EventInitialState:
				var initialState InitialState
				err := json.Unmarshal([]byte(event.Payload), &initialState)
				if err != nil {
					t.Errorf("Failed to unmarshal initialState: %v", err)
					close(stream1Finished)
					return
				}
				sessionId = initialState.SessionId
			case EventThought:
				_, messageText, _ := strings.Cut(event.Payload, "\n")
				if messageText == "Thinking...\nB" {
					receivedB = true
					close(stream1Ready)
				}
			case EventModelMessage:
				// Expected but do nothing
			case EventComplete, EventError:
				receivedCompleteOrError = true
			case EventSessionName:
				// Expected but do nothing
			default:
				t.Errorf("Unexpected event type: %c", event.Type)
				close(stream1Finished)
				return
			}
		}
		if !receivedB {
			t.Errorf("First client did not receive B")
		}
		if !receivedCompleteOrError {
			// Both are possible depending on timing
			t.Errorf("First client did not receive EventComplete or EventError")
		}
		close(stream1Finished)
	}()

	// Wait for the first stream to receive 'B'
	select {
	case <-stream1Ready:
		// Continue
	case <-stream1Finished:
		return
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for first stream to receive 'B'")
	}

	// 2. Second client connects while streaming is ongoing (after B)
	stream2Ready := make(chan struct{})
	stream2Finished := make(chan struct{})
	rr2 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)

	var initialState2 InitialState
	receivedModelMessage2 := ""
	receivedThoughtMessage2 := ""
	secondClientErrorReceived := false

	go func() {
		defer rr2.Body.Close()
		for event := range parseSseStream(t, rr2) {
			switch event.Type {
			case EventInitialState:
				err := json.Unmarshal([]byte(event.Payload), &initialState2)
				if err != nil {
					t.Errorf("Failed to unmarshal initialState: %v", err)
					close(stream2Finished)
					return
				}
				// Check if A and B are in the initial state (including thought message)
				if len(initialState2.History) < 3 ||
					initialState2.History[0].Parts[0].Text != initialUserMessage ||
					initialState2.History[1].Parts[0].Text != "A" ||
					initialState2.History[2].Parts[0].Text != "Thinking...\nB" {
					t.Errorf("Second client initial state missing A or B. History: %+v", initialState2.History)
				}
			case EventThought:
				_, messageText, _ := strings.Cut(event.Payload, "\n")
				receivedThoughtMessage2 += messageText
			case EventModelMessage:
				_, messageText, _ := strings.Cut(event.Payload, "\n")
				receivedModelMessage2 += messageText
				if strings.Contains(messageText, "E") {
					close(stream2Ready)
				}
			case EventError:
				if strings.Contains(event.Payload, "user canceled request") {
					secondClientErrorReceived = true
				}
				close(stream2Finished)
				return // Exit the goroutine after processing the error
			case EventComplete:
				close(stream2Finished)
				return // Exit the goroutine
			case EventSessionName:
				// Expected but do nothing
			default:
				t.Errorf("Unexpected event type: %c", event.Type)
				close(stream2Finished)
				return
			}
		}
		t.Errorf("Second client did not receive EventError or EventComplete")
		close(stream2Finished)
	}()

	// Wait for the second stream to receive 'E'
	select {
	case <-stream2Ready:
		// Continue
	case <-stream2Finished:
		return
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for second stream to receive 'E'")
	}

	// 3. Cancel the ongoing call
	cancelRR := testRequest(t, router, "DELETE", fmt.Sprintf("/api/chat/%s/call", sessionId), nil, http.StatusOK)
	if cancelRR.Code != http.StatusOK {
		t.Fatalf("Failed to cancel call: %v", cancelRR.Body.String())
	}

	// Give some time for the error event to propagate
	select {
	case <-stream2Finished:
		// Continue
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for second stream to receive EventError or EventComplete")
	}

	// Verify that both clients received the error message
	if !secondClientErrorReceived {
		t.Errorf("Second client did not receive 'user canceled request' error")
	}

	// 4. After cancellation, request initial state again
	rr3 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer rr3.Body.Close()

	var initialState3 InitialState
	initialStateNoCallReceived := false
	errorInInitialState := false

	for event := range parseSseStream(t, rr3) {
		switch event.Type {
		case EventInitialStateNoCall:
			initialStateNoCallReceived = true
			err := json.Unmarshal([]byte(event.Payload), &initialState3)
			if err != nil {
				t.Errorf("Failed to unmarshal initialStateNoCall: %v", err)
				return
			}
			// Check if A, B, C, DE and the error message are in the initial state
			// (D and E should merge into one message)
			if len(initialState3.History) < 6 ||
				initialState3.History[0].Parts[0].Text != initialUserMessage ||
				initialState3.History[1].Parts[0].Text != "A" ||
				initialState3.History[2].Parts[0].Text != "Thinking...\nB" ||
				initialState3.History[3].Parts[0].Text != "Thinking...\nC" ||
				initialState3.History[4].Parts[0].Text != "DE" ||
				!strings.Contains(initialState3.History[5].Parts[0].Text, "user canceled request") {
				t.Errorf("Initial state after cancellation missing expected messages or error. History: %+v", initialState3.History)
			}
			// Ensure the last message is an error message
			if len(initialState3.History) >= 6 && initialState3.History[5].Type != "model_error" {
				errorInInitialState = true
			}
		default:
			t.Errorf("Unexpected event type: %c", event.Type)
			return
		}
	}

	if !initialStateNoCallReceived {
		t.Errorf("Did not receive EventInitialStateNoCall after cancellation")
	}
	if errorInInitialState {
		t.Errorf("Last message in initial state after cancellation is not an error message")
	}
}

func TestApplyCurationRules(t *testing.T) {
	tests := []struct {
		name     string
		input    []FrontendMessage
		expected []FrontendMessage
	}{
		{
			name: "Basic scenario: User -> Model -> User -> Model",
			input: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 1"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 2"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 2"}}},
			},
			expected: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 1"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 2"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 2"}}},
			},
		},
		{
			name: "Consecutive user input: User -> User -> Model (first user removed)",
			input: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 1 (to be removed)"}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 2"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 2"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Function call without response: Model(FC) -> Model (FC removed)",
			input: []FrontendMessage{
				{Role: "model", Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Function call with response: Model(FC) -> User(FR) -> Model (all kept)",
			input: []FrontendMessage{
				{Role: "model", Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Role: "user", Type: TypeFunctionResponse, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: "tool"}}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Role: "model", Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Role: "user", Type: TypeFunctionResponse, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: "tool"}}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Consecutive user input with thought in between: User -> Thought -> User -> Model (first user removed)",
			input: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 1 (to be removed)"}}},
				{Role: "thought", Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 2"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Role: "thought", Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User 2"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Function call without response with thought in between: Model(FC) -> Thought -> Model (FC removed)",
			input: []FrontendMessage{
				{Role: "model", Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Role: "thought", Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Role: "thought", Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Mixed scenario: User -> Model -> User(removed) -> User -> Model(FC) -> Thought -> Model(removed) -> Model",
			input: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User A"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model A"}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User B (removed)"}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User C"}}},
				{Role: "model", Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Role: "thought", Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model D"}}}, // This is not a function response
			},
			expected: []FrontendMessage{
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User A"}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model A"}}},
				{Role: "user", Type: TypeText, Parts: []Part{{Text: "User C"}}},
				{Role: "thought", Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Role: "model", Type: TypeText, Parts: []Part{{Text: "Model D"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			curated := applyCurationRules(tt.input)

			if len(curated) != len(tt.expected) {
				t.Fatalf("Expected length %d, got %d. Curated: %+v, Expected: %+v", len(tt.expected), len(curated), curated, tt.expected)
			}

			for i := range curated {
				// Compare relevant fields for FrontendMessage
				if curated[i].Role != tt.expected[i].Role ||
					curated[i].Type != tt.expected[i].Type ||
					(len(curated[i].Parts) > 0 && len(tt.expected[i].Parts) > 0 && curated[i].Parts[0].Text != tt.expected[i].Parts[0].Text) ||
					(len(curated[i].Parts) > 0 && len(tt.expected[i].Parts) > 0 && curated[i].Parts[0].FunctionCall != nil && tt.expected[i].Parts[0].FunctionCall != nil && curated[i].Parts[0].FunctionCall.Name != tt.expected[i].Parts[0].FunctionCall.Name) ||
					(len(curated[i].Parts) > 0 && len(tt.expected[i].Parts) > 0 && curated[i].Parts[0].FunctionResponse != nil && tt.expected[i].Parts[0].FunctionResponse != nil && curated[i].Parts[0].FunctionResponse.Name != tt.expected[i].Parts[0].FunctionResponse.Name) {
					t.Errorf("Mismatch at index %d.\nExpected: %+v\nGot:      %+v", i, tt.expected[i], curated[i])
				}
			}
		})
	}
}
