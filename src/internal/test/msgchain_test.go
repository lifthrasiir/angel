package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/chat"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/llm"
	. "github.com/lifthrasiir/angel/internal/types"
)

// MockGeminiProvider for testing streamGeminiResponse
type MockGeminiProvider struct {
	Responses        []GenerateContentResponse
	Err              error
	Delay            time.Duration
	ExtraDelayIndex  int           // Index at which to apply extra delay
	ExtraDelayAmount time.Duration // Amount of extra delay to apply
}

// ModelName implements the LLMProvider interface for MockGeminiProvider.
func (m *MockGeminiProvider) SendMessageStream(ctx context.Context, modelName string, params llm.SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	if m.Err != nil {
		return nil, nil, m.Err
	}

	ch := make(chan GenerateContentResponse)
	closer := func() { close(ch) }

	seq := func(yield func(GenerateContentResponse) bool) {
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

func (m *MockGeminiProvider) GenerateContentOneShot(ctx context.Context, modelName string, params llm.SessionParams) (llm.OneShotResult, error) {
	return llm.OneShotResult{Text: "inferred name"}, nil
}

func (m *MockGeminiProvider) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
	return &CaCountTokenResponse{TotalTokens: 10}, nil
}

// MaxTokens is a placeholder for angel-eval.
func (m *MockGeminiProvider) MaxTokens(modelName string) int {
	return 1024 // A reasonable default for a simple eval model
}

// mockCloser implements io.Closer for testing purposes
type mockCloser struct{}

func (mc *mockCloser) Close() error {
	return nil
}

func nilIntStr(i *int) string {
	if i == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *i)
}

func printMessages(t *testing.T, db *database.SessionDatabase, testName string) {
	t.Helper()

	t.Logf("--- Messages in DB after %s ---", testName)

	rows, err := db.Query("SELECT id, type, parent_message_id, chosen_next_id, text FROM S.messages ORDER BY id ASC")
	if err != nil {
		t.Fatalf("Failed to query messages: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var msgType string
		var parentMessageID sql.NullInt64
		var chosenNextID sql.NullInt64
		var text string
		if err := rows.Scan(&id, &msgType, &parentMessageID, &chosenNextID, &text); err != nil {
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
		t.Logf("ID: %d, Type: %s, Parent: %s, ChosenNext: %s, Text: %s", id, msgType, parent, chosen, text)
	}
	t.Log("------------------------------------")
}

func responseFromPart(part Part) GenerateContentResponse {
	return GenerateContentResponse{
		Candidates: []Candidate{
			{
				Content: Content{
					Parts: []Part{part},
				},
			},
		},
	}
}

func TestMessageChainWithThoughtAndModel(t *testing.T) {
	// Setup router and context middleware
	router, db, models := setupTest(t)

	// Create the workspace for this test
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Setup Mock Gemini Provider
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			// Responses for B (thought, model)
			responseFromPart(Part{Text: "**Thinking**\nThis is a thought.", Thought: true}),
			responseFromPart(Part{Text: "This is the model's response."}),
		},
	})

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
			var initialState chat.InitialState
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

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		t.Fatalf("Failed to get session DB: %v", err)
	}
	defer sdb.Close()

	printMessages(t, sdb, "After first message chain")

	// Verify initial message chain
	// User (firstUserMessageID) -> Thought (thoughtMessageID) -> Model (modelMessageID)
	var msg Message
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{firstUserMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID != nil {
		t.Errorf("Expected first user message parent_message_id to be nil, got %d", *msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != thoughtMessageID {
		t.Errorf("Expected first user message chosen_next_id to be %d, got %v", thoughtMessageID, nilIntStr(msg.ChosenNextID))
	}

	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{thoughtMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != firstUserMessageID {
		t.Errorf("Expected thought message parent_message_id to be %d, got %v", firstUserMessageID, nilIntStr(msg.ParentMessageID))
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != modelMessageID {
		t.Errorf("Expected thought message chosen_next_id to be %d, got %v", modelMessageID, nilIntStr(msg.ChosenNextID))
	}

	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{modelMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != thoughtMessageID {
		t.Errorf("Expected model message parent_message_id to be %d, got %v", thoughtMessageID, nilIntStr(msg.ParentMessageID))
	}
	if msg.ChosenNextID != nil { // Should be nil as it's the end of the stream
		t.Errorf("Expected model message chosen_next_id to be nil, got %d", *msg.ChosenNextID)
	}

	// 2. Send another user message
	printMessages(t, sdb, "Before second user message")
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
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{modelMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondUserMessageID {
		t.Errorf("Expected model message chosen_next_id to be %d, got %v", secondUserMessageID, nilIntStr(msg.ChosenNextID))
	}

	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{secondUserMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != modelMessageID {
		t.Errorf("Expected second user message parent_message_id to be %d, got %v", modelMessageID, nilIntStr(msg.ParentMessageID))
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondThoughtMessageID {
		t.Errorf("Expected second user message chosen_next_id to be %d, got %v", secondThoughtMessageID, nilIntStr(msg.ChosenNextID))
	}

	// Verify the second thought message
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{secondThoughtMessageID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != secondUserMessageID {
		t.Errorf("Expected second thought message parent_message_id to be %d, got %v", secondUserMessageID, nilIntStr(msg.ParentMessageID))
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondModelMessageID {
		t.Errorf("Expected second thought message chosen_next_id to be %d, got %v", secondModelMessageID, nilIntStr(msg.ChosenNextID))
	}
}

func TestBranchingMessageChain(t *testing.T) {
	// Setup router and context middleware
	router, db, models := setupTest(t)

	// Create the workspace for this test
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// i) Create three messages: A-B-C
	// 1. Send initial user message (A)
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			// Responses for B (thought, model)
			responseFromPart(Part{Text: "A's thought", Thought: true}),
			responseFromPart(Part{Text: "A's response"}),
		},
	})
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
			var initialState chat.InitialState
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

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		t.Fatalf("Failed to get session DB: %v", err)
	}
	defer sdb.Close()

	printMessages(t, sdb, "After A1-A3 chain")

	// 2. Send second user message (C)
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
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
	printMessages(t, sdb, "After A1-A3-B1-B3 chain")

	// Verify A1-A3-B1-B3 chain in DB
	var msg Message
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgA1ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ParentMessageID != nil {
		t.Errorf("Expected first user message parent_message_id to be nil, got %d", *msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgA2ID {
		t.Errorf("Expected first user message chosen_next_id to be %v, got %d", nilIntStr(msg.ChosenNextID), msgA2ID)
	}
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgA2ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgA3ID {
		t.Errorf("Expected A2's chosen_next_id to be A3 (model). Got %v, want %d", nilIntStr(msg.ChosenNextID), msgA3ID)
	}
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgA3ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB1ID {
		t.Errorf("Expected A3's chosen_next_id to be B1 (user). Got %v, want %d", nilIntStr(msg.ChosenNextID), msgB1ID)
	}
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgB1ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB2ID {
		t.Errorf("Expected B1's chosen_next_id to be B2 (thought). Got %v, want %d", nilIntStr(msg.ChosenNextID), msgB2ID)
	}
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgB2ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgB3ID {
		t.Errorf("Expected B2's chosen_next_id to be B3 (model). Got %v, want %d", nilIntStr(msg.ChosenNextID), msgB3ID)
	}
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgB3ID}, &msg.ParentMessageID, &msg.ChosenNextID)
	if msg.ChosenNextID != nil {
		t.Errorf("Expected B3's chosen_next_id to be nil. Got %v", nilIntStr(msg.ChosenNextID))
	}

	// ii) Create a new branch C1-C2-C3 after A3 (Model A)
	// Create a new branch from message A3 (Model A)
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
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
	respC1 := testStreamingRequest(t, router, "POST", fmt.Sprintf("/api/chat/%s/branch", sessionId), bodyC1, http.StatusOK)
	defer respC1.Body.Close()

	// Parse the SSE response to get the initial state
	var newBranchCID string
	var msgC1ID int
	var initialStateBranchC chat.InitialState
	foundInitialState := false

	for event := range parseSseStream(t, respC1) {
		if event.Type == EventInitialState || event.Type == EventInitialStateNoCall {
			err = json.Unmarshal([]byte(event.Payload), &initialStateBranchC)
			if err != nil {
				t.Fatalf("Failed to unmarshal initial state for branch C: %v", err)
			}
			newBranchCID = initialStateBranchC.PrimaryBranchID
			// Find the newest message in the history (C1 - the new user message)
			if len(initialStateBranchC.History) > 0 {
				// The new message should be the last one in history
				newestMsg := initialStateBranchC.History[len(initialStateBranchC.History)-1]
				if newestMsg.Type == "user" && len(newestMsg.Parts) > 0 && newestMsg.Parts[0].Text == msgC1Text {
					msgC1ID, _ = strconv.Atoi(newestMsg.ID)
				}
			}
			foundInitialState = true
			break
		}
	}

	if !foundInitialState {
		t.Fatalf("Did not receive EventInitialState in branch creation response")
	}
	if newBranchCID == "" || msgC1ID == 0 {
		t.Fatalf("Failed to get newBranchCID or msgC1ID. NewBranchCID: %s, MsgC1ID: %d", newBranchCID, msgC1ID)
	}
	printMessages(t, sdb, "After A1-A3-C1 branch creation")

	// Verify that the new branch is now the primary branch
	var currentPrimaryBranchID string
	querySingleRow(t, sdb, "SELECT primary_branch_id FROM S.sessions WHERE id = ?", []interface{}{sdb.LocalSessionId()}, &currentPrimaryBranchID)
	if currentPrimaryBranchID != newBranchCID {
		t.Errorf("Primary branch not updated to new branch. Got %s, want %s", currentPrimaryBranchID, newBranchCID)
	}

	// Use the normal chat endpoint to complete C1-C3 chain (similar to B1-B3)
	respC2 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer respC2.Body.Close()

	// Wait for the response to complete
	for event := range parseSseStream(t, respC2) {
		// Process events but don't need to store them
		if event.Type == EventComplete {
			break
		}
	}

	printMessages(t, sdb, "After A1-A3-C1-C3 branch creation")

	// iii) Verify that the history correctly includes C1-C2-C3 and that A3 has both B1 and C1 as next messages.
	// Load history for C1-C2-C3 branch
	rrLoadC := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer rrLoadC.Body.Close()

	var initialStateC chat.InitialState
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
	querySingleRow(t, sdb, "SELECT chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgC1ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil {
		t.Fatalf("Failed to get chosen_next_id for msgC1ID %d: %v", msgC1ID, err)
	}
	msgC2ID := int(*msg.ChosenNextID)

	querySingleRow(t, sdb, "SELECT chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgC2ID}, &msg.ChosenNextID)
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
	querySingleRow(t, sdb, "SELECT chosen_next_id FROM S.messages WHERE id = ?", []interface{}{msgA3ID}, &msg.ChosenNextID)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != msgC1ID {
		t.Errorf("A3's chosen_next_id is not C1 (user C). Got %v, want %d", nilIntStr(msg.ChosenNextID), msgC1ID)
	}

	// Verify A3's parent (A3) has both B1 and C1 as possible next messages (by checking branches table)
	var count int
	querySingleRow(t, sdb, "SELECT COUNT(*) FROM S.branches WHERE session_id = ? AND branch_from_message_id = ?", []interface{}{sdb.LocalSessionId(), msgA3ID}, &count)
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
	printMessages(t, sdb, "After switching back to A1-A3-B1-B3 branch")

	// v) Verify that the history has changed to A1-A3-B1-B3
	// Load history for A1-A3-B1-B3 branch
	rrLoadA1B3 := testStreamingRequest(t, router, "GET", fmt.Sprintf("/api/chat/%s", sessionId), nil, http.StatusOK)
	defer rrLoadA1B3.Body.Close()

	var initialStateA1B3 chat.InitialState
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
	router, db, models := setupTest(t)

	// Create the workspace for this test
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Setup Mock Gemini Provider to stream "A", "B", "C" and then complete
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B"}),
			responseFromPart(Part{Text: "C"}),
		},
	})

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
			var initialState chat.InitialState
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

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		t.Fatalf("Failed to get session DB: %v", err)
	}
	defer sdb.Close()

	// Verify the message in the database
	var msg Message
	querySingleRow(t, sdb, "SELECT text FROM S.messages WHERE id = ?", []interface{}{modelMessageID}, &msg.Text)
	if msg.Text != "ABC" {
		t.Errorf("Expected message in DB to be 'ABC', got '%s'", msg.Text)
	}
}

func TestSyncDuringThought(t *testing.T) {
	router, db, models := setupTest(t) // Get db from setupTest

	// Create the workspace for this test
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider: A, B (thought), C (thought), D, E, F
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B", Thought: true}),
			responseFromPart(Part{Text: "C", Thought: true}),
			responseFromPart(Part{Text: "D"}),
			responseFromPart(Part{Text: "E"}),
			responseFromPart(Part{Text: "F"}),
		},
		Delay: 50 * time.Millisecond, // Faster for robust testing
	})

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
				var initialState chat.InitialState
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

	var initialState2 chat.InitialState
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
		case EventWorkspaceHint:
			// Expected but do nothing
			continue
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
	router, db, models := setupTest(t) // Get db from setupTest

	// Create the workspace for this test
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider: A, B (thought), C (thought), D, E, F
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{Text: "A"}),
			responseFromPart(Part{Text: "B", Thought: true}),
			responseFromPart(Part{Text: "C", Thought: true}),
			responseFromPart(Part{Text: "D"}),
			responseFromPart(Part{Text: "E"}),
			responseFromPart(Part{Text: "F"}),
		},
		Delay: 50 * time.Millisecond, // Faster for robust testing
	})

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
				var initialState chat.InitialState
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

	var initialState2 chat.InitialState
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
		case EventSessionName, EventWorkspaceHint:
			// Expected but do nothing
		default:
			t.Errorf("Unexpected event type: %c", event.Type)
			return
		}
	}
	t.Errorf("Second client did not receive EventComplete")
}

func TestCancelDuringSync(t *testing.T) {
	router, db, models := setupTest(t) // Get db from setupTest

	// Create the workspace for this test
	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider: A, B (thought), C (thought), D, E, F
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
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
				var initialState chat.InitialState
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

	var initialState2 chat.InitialState
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
			case EventSessionName, EventWorkspaceHint:
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

	var initialState3 chat.InitialState
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
		case EventWorkspaceHint:
			// Expected but do nothing
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

func TestCodeExecutionMessageHandling(t *testing.T) {
	router, db, models := setupTest(t)

	err := database.CreateWorkspace(db, "testWorkspace", "Test Workspace", "")
	if err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Mock Gemini Provider to return ExecutableCode and CodeExecutionResult
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{ExecutableCode: &ExecutableCode{Language: "python", Code: "print('hello')"}}),
			responseFromPart(Part{CodeExecutionResult: &CodeExecutionResult{Outcome: "OUTCOME_OK", Output: "hello"}}),
		},
	})

	initialUserMessage := "Run some code."
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
	var codeCallMessageID int
	var codeResponseMessageID int

	for event := range parseSseStream(t, rr) {
		switch event.Type {
		case EventAcknowledge:
			userMessageID, _ = strconv.Atoi(event.Payload)
		case EventInitialState:
			var initialState chat.InitialState
			err := json.Unmarshal([]byte(event.Payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
		case EventFunctionCall:
			messageIdPart, rest, _ := strings.Cut(event.Payload, "\n")
			functionName, argsJson, _ := strings.Cut(rest, "\n")
			codeCallMessageID, _ = strconv.Atoi(messageIdPart)
			if functionName != llm.GeminiCodeExecutionToolName {
				t.Errorf("Expected function name %s, got %s", llm.GeminiCodeExecutionToolName, functionName)
			}
			var ec ExecutableCode
			if err := json.Unmarshal([]byte(argsJson), &ec); err != nil {
				t.Errorf("Failed to unmarshal ExecutableCode from args: %v", err)
			}
			if ec.Language != "python" || ec.Code != "print('hello')" {
				t.Errorf("ExecutableCode mismatch. Expected {python, print('hello')}, got {%s, %s}", ec.Language, ec.Code)
			}
		case EventFunctionResponse:
			messageIdPart, rest, _ := strings.Cut(event.Payload, "\n")
			functionName, responseJson, _ := strings.Cut(rest, "\n")
			codeResponseMessageID, _ = strconv.Atoi(messageIdPart)
			if functionName != llm.GeminiCodeExecutionToolName {
				t.Errorf("Expected function name %s, got %s", llm.GeminiCodeExecutionToolName, functionName)
			}
			var payload FunctionResponsePayload
			if err := json.Unmarshal([]byte(responseJson), &payload); err != nil {
				t.Errorf("Failed to unmarshal FunctionResponsePayload: %v", err)
			}
			var cer CodeExecutionResult
			// Payload.Response is interface{}, need to marshal then unmarshal
			responseBytes, err := json.Marshal(payload.Response)
			if err != nil {
				t.Errorf("Failed to marshal payload.Response to JSON: %v", err)
			} else if err := json.Unmarshal(responseBytes, &cer); err != nil {
				t.Errorf("Failed to unmarshal CodeExecutionResult from payload.Response: %v", err)
			}
			if cer.Outcome != "OUTCOME_OK" || cer.Output != "hello" {
				t.Errorf("CodeExecutionResult mismatch. Expected {OUTCOME_OK, hello}, got {%s, %s}", cer.Outcome, cer.Output)
			}
		case EventComplete:
			// Test complete
		}
	}

	if sessionId == "" {
		t.Fatal("Session ID not found in SSE events")
	}
	if userMessageID == 0 {
		t.Fatal("User message ID not found in SSE events")
	}
	if codeCallMessageID == 0 {
		t.Fatal("Code call message ID not found in SSE events")
	}
	if codeResponseMessageID == 0 {
		t.Fatal("Code response message ID not found in SSE events")
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		t.Fatalf("Failed to get session DB: %v", err)
	}
	defer sdb.Close()

	// Verify message chain in DB
	// User -> CodeCall -> CodeResponse
	var msg Message
	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id, type, text FROM S.messages WHERE id = ?", []interface{}{userMessageID}, &msg.ParentMessageID, &msg.ChosenNextID, &msg.Type, &msg.Text)
	if msg.ChosenNextID == nil || *msg.ChosenNextID != codeCallMessageID {
		t.Errorf("Expected user message chosen_next_id to be %d, got %v", codeCallMessageID, nilIntStr(msg.ChosenNextID))
	}

	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id, type, text FROM S.messages WHERE id = ?", []interface{}{codeCallMessageID}, &msg.ParentMessageID, &msg.ChosenNextID, &msg.Type, &msg.Text)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != userMessageID {
		t.Errorf("Expected code call parent_message_id to be %d, got %v", userMessageID, nilIntStr(msg.ParentMessageID))
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != codeResponseMessageID {
		t.Errorf("Expected code call chosen_next_id to be %d, got %v", codeResponseMessageID, nilIntStr(msg.ChosenNextID))
	}
	if msg.Type != TypeFunctionCall {
		t.Errorf("Expected code call message type to be %s, got %s", TypeFunctionCall, msg.Type)
	}
	var fc FunctionCall
	if err := json.Unmarshal([]byte(msg.Text), &fc); err != nil {
		t.Errorf("Failed to unmarshal FunctionCall from DB text: %v", err)
	}
	if fc.Name != llm.GeminiCodeExecutionToolName {
		t.Errorf("Expected FunctionCall name in DB to be %s, got %s", llm.GeminiCodeExecutionToolName, fc.Name)
	}
	var ec ExecutableCode
	jsonBytes, err := json.Marshal(fc.Args)
	if err != nil {
		t.Errorf("Failed to marshal FunctionCall args to JSON: %v", err)
	}
	if err := json.Unmarshal(jsonBytes, &ec); err != nil {
		t.Errorf("Failed to unmarshal ExecutableCode from FunctionCall args in DB: %v", err)
	}
	if ec.Language != "python" || ec.Code != "print('hello')" {
		t.Errorf("ExecutableCode in DB mismatch. Expected {python, print('hello')}, got {%s, %s}", ec.Language, ec.Code)
	}

	querySingleRow(t, sdb, "SELECT parent_message_id, chosen_next_id, type, text FROM S.messages WHERE id = ?", []interface{}{codeResponseMessageID}, &msg.ParentMessageID, &msg.ChosenNextID, &msg.Type, &msg.Text)
	if msg.ParentMessageID == nil || *msg.ParentMessageID != codeCallMessageID {
		t.Errorf("Expected code response parent_message_id to be %d, got %v", codeCallMessageID, nilIntStr(msg.ParentMessageID))
	}
	if msg.ChosenNextID != nil {
		t.Errorf("Expected code response chosen_next_id to be nil, got %v", nilIntStr(msg.ChosenNextID))
	}
	if msg.Type != TypeFunctionResponse {
		t.Errorf("Expected code response message type to be %s, got %s", TypeFunctionResponse, msg.Type)
	}
	var fr FunctionResponse
	if err := json.Unmarshal([]byte(msg.Text), &fr); err != nil {
		t.Errorf("Failed to unmarshal FunctionResponse from DB text: %v", err)
	}
	if fr.Name != llm.GeminiCodeExecutionToolName {
		t.Errorf("Expected FunctionResponse name in DB to be %s, got %s", llm.GeminiCodeExecutionToolName, fr.Name)
	}
	var cer CodeExecutionResult
	responseBytes, err := json.Marshal(fr.Response)
	if err != nil {
		t.Errorf("Failed to marshal FunctionResponse.Response to JSON in DB: %v", err)
	} else if err := json.Unmarshal(responseBytes, &cer); err != nil {
		t.Errorf("Failed to unmarshal CodeExecutionResult from FunctionResponse.Response in DB: %v", err)
	}
	if cer.Outcome != "OUTCOME_OK" || cer.Output != "hello" {
		t.Errorf("CodeExecutionResult in DB mismatch. Expected {OUTCOME_OK, hello}, got {%s, %s}", cer.Outcome, cer.Output)
	}
}

func TestRetryErrorBranchHandler(t *testing.T) {
	router, db, models := setupTest(t)

	// Step 1: Create session and branch
	sessionId := database.GenerateID()
	primaryBranchId := database.GenerateID()

	sdb, _, err := database.CreateSession(db, sessionId, "You are a helpful assistant", "")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sdb.Close()

	_, err = database.CreateBranch(sdb, primaryBranchId, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create branch: %v", err)
	}

	// Step 2: Add messages using MessageChain for proper relationships
	mc, err := database.NewMessageChain(context.Background(), sdb, primaryBranchId)
	if err != nil {
		t.Fatalf("Failed to create message chain: %v", err)
	}

	// Add user message
	_, err = mc.Add(Message{
		Text:  "Hello",
		Type:  TypeUserText,
		Model: "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("Failed to add user message: %v", err)
	}

	// Add error message
	errorMsg, err := mc.Add(Message{
		Text: "Error occurred",
		Type: TypeError,
	})
	if err != nil {
		t.Fatalf("Failed to add error message: %v", err)
	}

	errorMsgID := errorMsg.ID

	// Verify error message is the last one
	lastID, _, _, err := database.GetLastMessageInBranch(sdb, primaryBranchId)
	if err != nil {
		t.Fatalf("Failed to get last message: %v", err)
	}
	if lastID != errorMsgID {
		t.Fatalf("Expected last message ID %d, got %d", errorMsgID, lastID)
	}

	// Step 3: Test retry with error message
	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{Text: "Retry response"}),
		},
		Delay: 5 * time.Millisecond,
	})

	retryRR := testStreamingRequest(t, router, "POST",
		fmt.Sprintf("/api/chat/%s/branch/%s/retry-error", sessionId, primaryBranchId),
		nil, http.StatusOK)

	// Collect events
	var events []string
	for event := range parseSseStream(t, retryRR) {
		defer retryRR.Body.Close()
		events = append(events, string(event.Type)+":"+event.Payload)
		if event.Type == EventComplete {
			break
		}
	}

	// Verify we got expected events
	if len(events) == 0 {
		t.Fatal("No events received from retry")
	}

	// Check for successful streaming
	foundInitialState := false
	foundModelMessage := false
	for _, event := range events {
		if strings.HasPrefix(event, "0:") { // EventInitialState
			foundInitialState = true
		}
		// Model message can be various event types, check for the content
		if strings.Contains(event, "Retry response") {
			foundModelMessage = true
		}
	}

	if !foundInitialState {
		t.Error("Missing initial state event")
	}
	if !foundModelMessage {
		t.Error("Missing model message event")
	}

	// Step 4: Verify error message was deleted
	_, err = database.GetMessageByID(sdb, errorMsgID)
	if err == nil {
		t.Error("Error message still exists in database")
	} else if err != sql.ErrNoRows {
		// err should be "message not found" which is expected
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Unexpected error checking message: %v", err)
		}
	}

	// Step 5: Test retry without errors should succeed (behavior changed after allowing retry without errors)
	// Wait a bit to ensure the first retry call is completely finished
	time.Sleep(100 * time.Millisecond)

	models.SetGeminiProvider(&MockGeminiProvider{
		Responses: []GenerateContentResponse{
			responseFromPart(Part{Text: "Retry without errors"}),
		},
		Delay: 5 * time.Millisecond,
	})

	retryRR2 := testStreamingRequest(t, router, "POST",
		fmt.Sprintf("/api/chat/%s/branch/%s/retry-error", sessionId, primaryBranchId),
		nil, http.StatusOK)
	defer retryRR2.Body.Close()

	// Collect events to verify successful retry
	var retry2Events []string
	for event := range parseSseStream(t, retryRR2) {
		retry2Events = append(retry2Events, string(event.Type)+":"+event.Payload)
		if event.Type == EventComplete {
			break
		}
	}

	// Verify we got expected events from retry without errors
	if len(retry2Events) == 0 {
		t.Error("No events received from retry without errors")
	}

	// Check for successful streaming
	foundRetry2InitialState := false
	foundRetry2ModelMessage := false
	for _, event := range retry2Events {
		if strings.HasPrefix(event, "0:") { // EventInitialState
			foundRetry2InitialState = true
		}
		// Model message can be various event types, check for the content
		if strings.Contains(event, "Retry without errors") {
			foundRetry2ModelMessage = true
		}
	}

	if !foundRetry2InitialState {
		t.Error("Missing initial state event in retry without errors")
	}
	if !foundRetry2ModelMessage {
		t.Error("Missing model message event in retry without errors")
	}
}
