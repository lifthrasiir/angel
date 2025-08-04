package main

import (
	"bytes"
	"context"
	"database/sql" // ADDED
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

	"github.com/gorilla/mux"
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

// mockCloser implements io.Closer for testing purposes
type mockCloser struct{}

func (mc *mockCloser) Close() error {
	return nil
}

func (m *MockGeminiProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (string, error) {
	return "inferred name", nil
}

func (m *MockGeminiProvider) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	return &CaCountTokenResponse{TotalTokens: 10}, nil
}

func (m *MockGeminiProvider) LoadCodeAssist(ctx context.Context) error {
	return nil
}

func (m *MockGeminiProvider) OnboardUser(ctx context.Context) error {
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

func TestMessageChainWithThoughtAndModel(t *testing.T) {
	// Setup DB
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Setup Mock Gemini Provider
	mockProvider := &MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "**Thinking**\nThis is a thought.", Thought: true},
								},
							},
						},
					},
				},
			},
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "This is the model's response."},
								},
							},
						},
					},
				},
			},
		},
	}
	CurrentProvider = mockProvider

	// Setup router and context middleware
	router := mux.NewRouter()

	ga := &GeminiAuth{}
	ga.Init(db)
	ga.ProjectID = "test-project" // Set a dummy project ID for testing

	router.Use(makeContextMiddleware(db, ga))
	InitRouter(router)

	// 1. Start new session with initial user message
	initialUserMessage := "Hello, Gemini!"
	reqBody := map[string]interface{}{
		"message":      initialUserMessage,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "test-workspace",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Manually add db, ga, wg to request context
	req = req.WithContext(contextWithGlobals(req.Context(), db, ga))

	rr := httptest.NewRecorder()

	newSessionAndMessage(rr, req)

	printMessages(t, db, "After first message chain") // ADDED

	if rr.Code != http.StatusOK {
		t.Fatalf("newSessionAndMessage failed with status %d: %s", rr.Code, rr.Body.String())
	}

	// Parse SSE events to get session ID and message IDs
	events := strings.Split(strings.TrimSpace(rr.Body.String()), "\n\n")
	var sessionId string
	var firstUserMessageID int
	var thoughtMessageID int
	var modelMessageID int

	for _, event := range events {
		event = strings.ReplaceAll(event[6:], "\ndata: ", "\n")
		eventType, payload, _ := strings.Cut(event, "\n")

		switch EventType([]rune(eventType)[0]) {
		case EventAcknowledge:
			firstUserMessageID, _ = strconv.Atoi(payload)
		case EventInitialState:
			var initialState InitialState
			err := json.Unmarshal([]byte(payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
		case EventThought:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
			thoughtMessageID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
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
	err = db.QueryRow("SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", firstUserMessageID).Scan(&msg.ParentMessageID, &msg.ChosenNextID)
	if err != nil {
		t.Fatalf("Failed to get message %d: %v", firstUserMessageID, err)
	}
	if msg.ParentMessageID != nil {
		t.Errorf("Expected first user message parent_message_id to be nil, got %d", *msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != thoughtMessageID {
		t.Errorf("Expected first user message chosen_next_id to be %d, got %v", thoughtMessageID, msg.ChosenNextID)
	}

	err = db.QueryRow("SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", thoughtMessageID).Scan(&msg.ParentMessageID, &msg.ChosenNextID)
	if err != nil {
		t.Fatalf("Failed to get message %d: %v", thoughtMessageID, err)
	}
	if msg.ParentMessageID == nil || *msg.ParentMessageID != firstUserMessageID {
		t.Errorf("Expected thought message parent_message_id to be %d, got %v", firstUserMessageID, msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != modelMessageID {
		t.Errorf("Expected thought message chosen_next_id to be %d, got %v", modelMessageID, msg.ChosenNextID)
	}

	err = db.QueryRow("SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", modelMessageID).Scan(&msg.ParentMessageID, &msg.ChosenNextID)
	if err != nil {
		t.Fatalf("Failed to get message %d: %v", modelMessageID, err)
	}
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
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/chat/%s", sessionId), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Manually add db, ga, wg to request context for the second request
	req = req.WithContext(contextWithGlobals(req.Context(), db, ga))

	rr = httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("chatMessage failed with status %d: %s", rr.Code, rr.Body.String())
	}

	// Parse SSE events for the second user message ID and second thought message ID
	events = strings.Split(strings.TrimSpace(rr.Body.String()), "\n\n")
	var secondUserMessageID int
	var secondThoughtMessageID int
	var secondModelMessageID int // ADDED
	for _, event := range events {
		event = strings.ReplaceAll(event[6:], "\ndata: ", "\n")
		eventType, payload, _ := strings.Cut(event, "\n")
		switch EventType([]rune(eventType)[0]) {
		case EventAcknowledge:
			secondUserMessageID, _ = strconv.Atoi(payload)
		case EventThought:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
			secondThoughtMessageID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage: // ADDED
			messageIdPart, _, _ := strings.Cut(payload, "\n")     // ADDED
			secondModelMessageID, _ = strconv.Atoi(messageIdPart) // ADDED
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
	err = db.QueryRow("SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", modelMessageID).Scan(&msg.ParentMessageID, &msg.ChosenNextID)
	if err != nil {
		t.Fatalf("Failed to get message %d: %v", modelMessageID, err)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondUserMessageID {
		t.Errorf("Expected model message chosen_next_id to be %d, got %v", secondUserMessageID, msg.ChosenNextID)
	}

	err = db.QueryRow("SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", secondUserMessageID).Scan(&msg.ParentMessageID, &msg.ChosenNextID)
	if err != nil {
		t.Fatalf("Failed to get message %d: %v", secondUserMessageID, err)
	}
	if msg.ParentMessageID == nil || *msg.ParentMessageID != modelMessageID {
		t.Errorf("Expected second user message parent_message_id to be %d, got %v", modelMessageID, msg.ParentMessageID)
	}
	if msg.ChosenNextID == nil || *msg.ChosenNextID != secondThoughtMessageID {
		t.Errorf("Expected second user message chosen_next_id to be %d, got %v", secondThoughtMessageID, msg.ChosenNextID)
	}

	// Verify the second thought message
	err = db.QueryRow("SELECT parent_message_id, chosen_next_id FROM messages WHERE id = ?", secondThoughtMessageID).Scan(&msg.ParentMessageID, &msg.ChosenNextID)
	if err != nil {
		t.Fatalf("Failed to get message %d: %v", secondThoughtMessageID, err)
	}
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

	// Setup Mock Gemini Provider (will be re-assigned for each request)
	var mockProvider *MockGeminiProvider

	// Setup router and context middleware
	router := mux.NewRouter()

	ga := &GeminiAuth{}
	ga.Init(db)
	ga.ProjectID = "test-project" // Set a dummy project ID for testing

	router.Use(makeContextMiddleware(db, ga))
	InitRouter(router)

	// i) A-B-C 세 메시지를 작성
	// 1. Send initial user message (A)
	mockProvider = &MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for B (thought, model)
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "B's thought", Thought: true},
								},
							},
						},
					},
				},
			},
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "B's response"},
								},
							},
						},
					},
				},
			},
		},
	}
	CurrentProvider = mockProvider
	msgA1Text := "Message A"
	reqBodyA1 := map[string]interface{}{
		"message":      msgA1Text,
		"systemPrompt": "You are a helpful assistant.",
		"workspaceId":  "test-workspace",
	}
	bodyA1, _ := json.Marshal(reqBodyA1)
	reqA1 := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(bodyA1))
	reqA1.Header.Set("Content-Type", "application/json")
	reqA1.Header.Set("Accept", "text/event-stream")
	reqA1 = reqA1.WithContext(contextWithGlobals(reqA1.Context(), db, ga))
	rrA1 := httptest.NewRecorder()
	newSessionAndMessage(rrA1, reqA1)
	if rrA1.Code != http.StatusOK {
		t.Fatalf("newSessionAndMessage for A failed with status %d: %s", rrA1.Code, rrA1.Body.String())
	}

	eventsA := strings.Split(strings.TrimSpace(rrA1.Body.String()), "\n\n")
	var sessionId string
	var msgA1ID int // A1: User message
	var msgA2ID int // A2: Thought message
	var msgA3ID int // A3: Model message
	var originalPrimaryBranchID string

	for _, event := range eventsA {
		event = strings.ReplaceAll(event[6:], "\ndata: ", "\n")
		eventType, payload, _ := strings.Cut(event, "\n")
		switch EventType([]rune(eventType)[0]) {
		case EventAcknowledge:
			msgA1ID, _ = strconv.Atoi(payload)
		case EventInitialState:
			var initialState InitialState
			err := json.Unmarshal([]byte(payload), &initialState)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState: %v", err)
			}
			sessionId = initialState.SessionId
			originalPrimaryBranchID = initialState.PrimaryBranchID
		case EventThought:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
			msgA2ID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
			msgA3ID, _ = strconv.Atoi(messageIdPart)
		}
	}

	if sessionId == "" || msgA1ID == 0 || msgA2ID == 0 || msgA3ID == 0 || originalPrimaryBranchID == "" {
		t.Fatalf("Failed to get all IDs for A1-A3 chain. SessionID: %s, MsgA1ID: %d, MsgA2ID: %d, MsgA3ID: %d, OriginalPrimaryBranchID: %s", sessionId, msgA1ID, msgA2ID, msgA3ID, originalPrimaryBranchID)
	}
	printMessages(t, db, "After A1-A3 chain")

	// 2. Send second user message (C)
	mockProvider = &MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for B (thought, model)
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "B's thought", Thought: true},
								},
							},
						},
					},
				},
			},
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "B's response"},
								},
							},
						},
					},
				},
			},
		},
	}
	CurrentProvider = mockProvider
	userMessageB1 := "Message B"
	reqBodyB1 := map[string]interface{}{
		"message": userMessageB1,
	}
	bodyB1, _ := json.Marshal(reqBodyB1)
	reqB1 := httptest.NewRequest("POST", fmt.Sprintf("/api/chat/%s", sessionId), bytes.NewReader(bodyB1))
	reqB1.Header.Set("Content-Type", "application/json")
	reqB1 = reqB1.WithContext(contextWithGlobals(reqB1.Context(), db, ga))
	rrB1 := httptest.NewRecorder()
	router.ServeHTTP(rrB1, reqB1)
	if rrB1.Code != http.StatusOK {
		t.Fatalf("chatMessage for B failed with status %d: %s", rrB1.Code, rrB1.Body.String())
	}

	eventsB := strings.Split(strings.TrimSpace(rrB1.Body.String()), "\n\n")
	var msgB1ID int // B1: User message
	var msgB2ID int // B2: Thought message
	var msgB3ID int // B3: Model message

	for _, event := range eventsB {
		event = strings.ReplaceAll(event[6:], "\ndata: ", "\n")
		eventType, payload, _ := strings.Cut(event, "\n")
		switch EventType([]rune(eventType)[0]) {
		case EventAcknowledge:
			msgB1ID, _ = strconv.Atoi(payload)
		case EventThought:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
			msgB2ID, _ = strconv.Atoi(messageIdPart)
		case EventModelMessage:
			messageIdPart, _, _ := strings.Cut(payload, "\n")
			msgB3ID, _ = strconv.Atoi(messageIdPart)
		}
	}
	if msgB1ID == 0 || msgB2ID == 0 || msgB3ID == 0 {
		t.Fatalf("Failed to get all IDs for B chain. MsgB1ID: %d, MsgB2ID: %d, MsgB3ID: %d", msgB1ID, msgB2ID, msgB3ID)
	}
	printMessages(t, db, "After A1-A3-B1-B3 chain")

	// Verify A1-A3-B1-B3 chain in DB
	var msg Message
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgA1ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil || *msg.ChosenNextID != msgA2ID {
		t.Errorf("A1's chosen_next_id is not A2 (thought). Got %v, want %d", msg.ChosenNextID, msgA2ID)
	}
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgA2ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil || *msg.ChosenNextID != msgA3ID {
		t.Errorf("A2's chosen_next_id is not A3 (model). Got %v, want %d", msg.ChosenNextID, msgA3ID)
	}
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgA3ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil || *msg.ChosenNextID != msgB1ID {
		t.Errorf("A3's chosen_next_id is not B1 (user). Got %v, want %d", msg.ChosenNextID, msgB1ID)
	}
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgB1ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil || *msg.ChosenNextID != msgB2ID {
		t.Errorf("B1's chosen_next_id is not B2 (thought). Got %v, want %d", msg.ChosenNextID, msgB2ID)
	}
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgB2ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil || *msg.ChosenNextID != msgB3ID {
		t.Errorf("B2's chosen_next_id is not B3 (model). Got %v, want %d", msg.ChosenNextID, msgB3ID)
	}
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgB3ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID != nil {
		t.Errorf("B3's chosen_next_id is not nil. Got %v", msg.ChosenNextID)
	}

	// ii) A3 (모델 A) 다음에 브랜치를 새로 만들어 C1-C2-C3를 만들고
	// Create a new branch from message A3 (Model A)
	mockProvider = &MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			// Responses for C (thought, model)
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "C's thought", Thought: true},
								},
							},
						},
					},
				},
			},
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "C's response"},
								},
							},
						},
					},
				},
			},
		},
	}
	CurrentProvider = mockProvider
	msgC1Text := "Message C"
	reqBodyC1 := map[string]interface{}{
		"updatedMessageId": msgB1ID, // Branching from A3, updating B1
		"newMessageText":   msgC1Text,
	}
	bodyC1, _ := json.Marshal(reqBodyC1)
	reqC1 := httptest.NewRequest("POST", fmt.Sprintf("/api/chat/%s/branch", sessionId), bytes.NewReader(bodyC1))
	reqC1.Header.Set("Content-Type", "application/json")
	reqC1 = reqC1.WithContext(contextWithGlobals(reqC1.Context(), db, ga))
	rrC1 := httptest.NewRecorder()
	router.ServeHTTP(rrC1, reqC1)
	if rrC1.Code != http.StatusOK {
		t.Fatalf("createBranchHandler for C failed with status %d: %s", rrC1.Code, rrC1.Body.String())
	}

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
	err = db.QueryRow("SELECT primary_branch_id FROM sessions WHERE id = ?", sessionId).Scan(&currentPrimaryBranchID)
	if err != nil || currentPrimaryBranchID != newBranchCID {
		t.Errorf("Primary branch not updated to new branch. Got %s, want %s", currentPrimaryBranchID, newBranchCID)
	}

	// Simulate streaming response for Message C (thought and model)
	mockProvider = &MockGeminiProvider{
		Responses: []CaGenerateContentResponse{
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "C's thought", Thought: true},
								},
							},
						},
					},
				},
			},
			{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{
						{
							Content: Content{
								Parts: []Part{
									{Text: "C's response"},
								},
							},
						},
					},
				},
			},
		},
	}
	CurrentProvider = mockProvider

	// Prepare initial state for streaming for the new branch
	initialStateCStream := InitialState{
		SessionId:       sessionId,
		History:         []FrontendMessage{},
		SystemPrompt:    "You are a helpful assistant.",
		WorkspaceID:     "test-workspace",
		PrimaryBranchID: newBranchCID,
	}

	// Create a dummy SSE writer for streamGeminiResponse
	dummySseW := newSseWriter(sessionId, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if dummySseW == nil {
		t.Fatalf("Failed to create dummy SSE writer")
	}
	defer dummySseW.Close()

	// Call streamGeminiResponse to add thought and model messages for C
	if err := streamGeminiResponse(db, initialStateCStream, dummySseW, msgC1ID); err != nil {
		t.Fatalf("Error streaming Gemini response for C: %v", err)
	}

	printMessages(t, db, "After A1-A3-C1-C3 branch creation")

	// iii) 그 상태에서 히스토리가 C1-C2-C3를 제대로 포함하며 A3 다음 메시지가 B1/C1 둘 다 있음을 확인
	// Load history for C1-C2-C3 branch
	reqLoadC := httptest.NewRequest("GET", fmt.Sprintf("/api/chat/%s", sessionId), nil)
	reqLoadC.Header.Set("Accept", "text/event-stream")
	reqLoadC = reqLoadC.WithContext(contextWithGlobals(reqLoadC.Context(), db, ga))
	rrLoadC := httptest.NewRecorder()
	router.ServeHTTP(rrLoadC, reqLoadC)
	if rrLoadC.Code != http.StatusOK {
		t.Fatalf("loadChatSession for C1-C2-C3 failed with status %d: %s", rrLoadC.Code, rrLoadC.Body.String())
	}

	eventsLoadC := strings.Split(strings.TrimSpace(rrLoadC.Body.String()), "\n\n")
	var initialStateC InitialState
	for _, event := range eventsLoadC {
		event = strings.ReplaceAll(event[6:], "\ndata: ", "\n")
		eventType, payload, _ := strings.Cut(event, "\n")
		if EventType([]rune(eventType)[0]) == EventInitialState || EventType([]rune(eventType)[0]) == EventInitialStateNoCall {
			err = json.Unmarshal([]byte(payload), &initialStateC)
			if err != nil {
				t.Fatalf("Failed to unmarshal initialState for C1-C2-C3: %v", err)
			}
			break // Only need the initial state
		}
	}

	// Get msgC2ID and msgC3ID from DB after streaming
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgC1ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil {
		t.Fatalf("Failed to get chosen_next_id for msgC1ID %d: %v", msgC1ID, err)
	}
	msgC2ID := int(*msg.ChosenNextID)

	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgC2ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil {
		t.Fatalf("Failed to get chosen_next_id for msgC2ID %d: %v", msgC2ID, err)
	}
	msgC3ID := int(*msg.ChosenNextID)

	// History should be C (user), C (thought), C (model)
	if len(initialStateC.History) != 3 {
		t.Errorf("Expected C1-C2-C3 history length 3, got %d", len(initialStateC.History))
	}
	if initialStateC.History[0].ID != fmt.Sprintf("%d", msgC1ID) ||
		initialStateC.History[1].ID != fmt.Sprintf("%d", msgC2ID) ||
		initialStateC.History[2].ID != fmt.Sprintf("%d", msgC3ID) {
		t.Errorf("C1-C2-C3 history mismatch. Got %v", initialStateC.History)
	}

	// Verify A3's chosen_next_id is C1 (user C)
	err = db.QueryRow("SELECT chosen_next_id FROM messages WHERE id = ?", msgA3ID).Scan(&msg.ChosenNextID)
	if err != nil || msg.ChosenNextID == nil || *msg.ChosenNextID != msgC1ID {
		t.Errorf("A3's chosen_next_id is not C1 (user C). Got %v, want %d", msg.ChosenNextID, msgC1ID)
	}

	// Verify A3's parent (A3) has both B1 and C1 as possible next messages (by checking branches table)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM branches WHERE session_id = ? AND branch_from_message_id = ?", sessionId, msgA3ID).Scan(&count)
	if err != nil || count != 1 { // Only the new C branch should have A3 as parent
		t.Errorf("Expected 1 branch from A3, got %d", count)
	}

	// iv) A1-A3-B1-B3 브랜치로 돌아가고
	// Switch back to A1-A3-B1-B3 branch
	reqBodySwitch := map[string]interface{}{
		"newPrimaryBranchId": originalPrimaryBranchID,
	}
	bodySwitch, _ := json.Marshal(reqBodySwitch)
	reqSwitch := httptest.NewRequest("PUT", fmt.Sprintf("/api/chat/%s/branch", sessionId), bytes.NewReader(bodySwitch))
	reqSwitch.Header.Set("Content-Type", "application/json")
	reqSwitch = reqSwitch.WithContext(contextWithGlobals(reqSwitch.Context(), db, ga))
	rrSwitch := httptest.NewRecorder()
	router.ServeHTTP(rrSwitch, reqSwitch)
	if rrSwitch.Code != http.StatusOK {
		t.Fatalf("switchBranchHandler failed with status %d: %s", rrSwitch.Code, rrSwitch.Body.String())
	}
	printMessages(t, db, "After switching back to A1-A3-B1-B3 branch")

	// v) 히스토리가 A1-A3-B1-B3로 바뀌었음을 확인
	// Load history for A1-A3-B1-B3 branch
	reqLoadA1B3 := httptest.NewRequest("GET", fmt.Sprintf("/api/chat/%s", sessionId), nil)
	reqLoadA1B3.Header.Set("Accept", "text/event-stream")
	reqLoadA1B3 = reqLoadA1B3.WithContext(contextWithGlobals(reqLoadA1B3.Context(), db, ga))
	rrLoadA1B3 := httptest.NewRecorder()
	router.ServeHTTP(rrLoadA1B3, reqLoadA1B3)
	if rrLoadA1B3.Code != http.StatusOK {
		t.Fatalf("loadChatSession for A1-A3-B1-B3 failed with status %d: %s", rrLoadA1B3.Code, rrLoadA1B3.Body.String())
	}

	eventsLoadA1B3 := strings.Split(strings.TrimSpace(rrLoadA1B3.Body.String()), "\n\n")
	var initialStateA1B3 InitialState
	for _, event := range eventsLoadA1B3 {
		event = strings.ReplaceAll(event[6:], "\ndata: ", "\n")
		eventType, payload, _ := strings.Cut(event, "\n")
		if EventType([]rune(eventType)[0]) == EventInitialState || EventType([]rune(eventType)[0]) == EventInitialStateNoCall {
			err := json.Unmarshal([]byte(payload), &initialStateA1B3)
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
