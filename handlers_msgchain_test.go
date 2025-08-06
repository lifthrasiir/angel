package main

import (
	"context"
	"database/sql"
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

// MockGeminiProvider for testing streamGeminiResponse
type MockGeminiProvider struct {
	Responses []CaGenerateContentResponse
	Err       error
}

func (m *MockGeminiProvider) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	if m.Err != nil {
		return nil, nil, m.Err
	}

	ch := make(chan CaGenerateContentResponse)
	closer := func() { close(ch) }

	seq := func(yield func(CaGenerateContentResponse) bool) {
		defer closer()
		for _, resp := range m.Responses {
			select {
			case <-ctx.Done():
				return
			default:
				if !yield(resp) {
					return
				}
				// Simulate streaming delay
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	return seq, &mockCloser{}, nil // Return a mock io.Closer
}

func (m *MockGeminiProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (string, error) {
	return "inferred name", nil
}

func (m *MockGeminiProvider) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	return &CaCountTokenResponse{TotalTokens: 10}, nil
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
	// Setup DB
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Setup router and context middleware
	router, db, _ := setupTest(t)

	// Setup Mock Gemini Provider
	provider := replaceProvider(&MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
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
		"workspaceId":  "test-workspace",
	}
	body, _ := json.Marshal(reqBody)
	rr := testRequest(t, router, "POST", "/api/chat", body, http.StatusOK)

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
	rr = testRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s", sessionId), body, http.StatusOK)

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
	// Setup DB
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Setup router and context middleware
	router, db, _ := setupTest(t)

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
		"workspaceId":  "test-workspace",
	}
	bodyA1, _ := json.Marshal(reqBodyA1)
	rrA1 := testRequest(t, router, "POST", "/api/chat", bodyA1, http.StatusOK)

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
	rrB1 := testRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s", sessionId), bodyB1, http.StatusOK)

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
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA1ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgA2ID {
		t.Errorf("A1's chosen_next_id is not A2 (thought). Got %v, want %d", msg.ChosenNextID, msgA2ID)
	}
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA2ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgA3ID {
		t.Errorf("A2's chosen_next_id is not A3 (model). Got %v, want %d", msg.ChosenNextID, msgA3ID)
	}
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgA3ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB1ID {
		t.Errorf("A3's chosen_next_id is not B1 (user). Got %v, want %d", msg.ChosenNextID, msgB1ID)
	}
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgB1ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB2ID {
		t.Errorf("B1's chosen_next_id is not B2 (thought). Got %v, want %d", msg.ChosenNextID, msgB2ID)
	}
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgB2ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB3ID {
		t.Errorf("B2's chosen_next_id is not B3 (model). Got %v, want %d", msg.ChosenNextID, msgB3ID)
	}
	querySingleRow(t, db, "SELECT chosen_next_id FROM messages WHERE id = ?", []interface{}{msgB3ID}, &msg.ChosenNextID)
	if msg.ChosenNextID != nil {
		t.Errorf("B3's chosen_next_id is not nil. Got %v", msg.ChosenNextID)
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
			responseFromPart(Part{Text: "C's thought", Thought: true}),
			responseFromPart(Part{Text: "C's response"}),
		},
	})

	// Prepare initial state for streaming for the new branch
	initialStateCStream := InitialState{
		SessionId:       sessionId,
		History:         []FrontendMessage{},
		SystemPrompt:    "You are a helpful assistant.",
		WorkspaceID:     "test-workspace",
		PrimaryBranchID: newBranchCID,
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
	if err := streamGeminiResponse(db, initialStateCStream, dummySseW, msgC1ID); err != nil {
		t.Fatalf("Error streaming Gemini response for C: %v", err)
	}

	printMessages(t, db, "After A1-A3-C1-C3 branch creation")

	// iii) Verify that the history correctly includes C1-C2-C3 and that A3 has both B1 and C1 as next messages.
	// Load history for C1-C2-C3 branch
	rrLoadC := testRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)

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

	// History should be C (user), C (thought), C (model)
	if len(initialStateC.History) != 3 {
		t.Fatalf("Expected C1-C2-C3 history length 3, got %d", len(initialStateC.History))
	}
	if initialStateC.History[0].ID != fmt.Sprintf("%d", msgC1ID) ||
		initialStateC.History[1].ID != fmt.Sprintf("%d", msgC2ID) ||
		initialStateC.History[2].ID != fmt.Sprintf("%d", msgC3ID) {
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
	rrLoadA1B3 := testRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)

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
