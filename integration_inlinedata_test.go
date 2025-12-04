package main

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"testing"

	. "github.com/lifthrasiir/angel/gemini"
)

// TestInlineDataStreaming tests inlineData streaming functionality with proper SSE parsing
func TestInlineDataStreaming(t *testing.T) {
	router, _ := setupTest(t)

	// Create a simple 1x1 PNG image (base64 encoded)
	pngData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

	// Mock the SendMessageStream method to simulate inlineData response
	provider := GlobalModelsRegistry.GetProvider(DefaultGeminiModel)
	mockLLMProvider := provider.(*MockLLMProvider)
	mockLLMProvider.SendMessageStreamFunc = func(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
		// Create a mock response with text and inlineData
		responses := []GenerateContentResponse{
			{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{Text: "Here's your first image:"},
							},
						},
					},
				},
			},
			{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{
									InlineData: &InlineData{
										MimeType: "image/png",
										Data:     pngData,
									},
								},
							},
						},
					},
				},
			},
			{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{Text: "And here's a second image:"},
							},
						},
					},
				},
			},
			{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{
									InlineData: &InlineData{
										MimeType: "image/png",
										Data:     pngData,
									},
								},
							},
						},
					},
				},
			},
		}

		seq := func(yield func(GenerateContentResponse) bool) {
			for _, resp := range responses {
				if !yield(resp) {
					return
				}
			}
		}

		return seq, &mockCloser{}, nil
	}

	t.Run("Success", func(t *testing.T) {
		payload := map[string]interface{}{
			"message": "Generate some images for me",
		}
		body, _ := json.Marshal(payload)

		// Use testStreamingRequest to get a real HTTP response for SSE parsing
		resp := testStreamingRequest(t, router, "POST", "/api/chat", body, http.StatusOK)
		defer resp.Body.Close()

		// Parse SSE events
		var sessionId string
		var modelMessageIDs []int
		var inlineDataMessageCount int

		for event := range parseSseStream(t, resp) {
			switch event.Type {
			case EventInitialState:
				var initialState InitialState
				err := json.Unmarshal([]byte(event.Payload), &initialState)
				if err != nil {
					t.Fatalf("Failed to unmarshal initialState: %v", err)
				}
				sessionId = initialState.SessionId

			case EventModelMessage:
				// Parse model message ID for text messages
				messageIdPart, _, _ := strings.Cut(event.Payload, "\n")
				if messageIdPart != "" {
					messageID, err := strconv.Atoi(messageIdPart)
					if err == nil {
						modelMessageIDs = append(modelMessageIDs, messageID)
					}
				}

			case EventInlineData:
				// Handle inline data events (for images)
				var payload InlineDataPayload
				err := json.Unmarshal([]byte(event.Payload), &payload)
				if err != nil {
					t.Fatalf("Failed to unmarshal inline data payload: %v", err)
				}

				// Parse message ID from payload
				messageID, err := strconv.Atoi(payload.MessageId)
				if err == nil {
					modelMessageIDs = append(modelMessageIDs, messageID)
					inlineDataMessageCount++
				}

			case EventComplete:
				// Streaming completed
			}
		}

		// Verify results
		if sessionId == "" {
			t.Fatal("Session ID not found in SSE events")
		}

		// Should have multiple model messages (text + images)
		if len(modelMessageIDs) < 2 {
			t.Errorf("Expected at least 2 model messages, got %d", len(modelMessageIDs))
		}

		// Verify we got inline data messages (images)
		if inlineDataMessageCount < 2 {
			t.Errorf("Expected at least 2 inline data messages, got %d", inlineDataMessageCount)
		}
	})
}

// TestInlineDataFilenameGeneration tests the filename generation function
func TestInlineDataFilenameGeneration(t *testing.T) {
	testCases := []struct {
		mimeType     string
		counter      int
		expectedFile string
	}{
		{"image/png", 1, "generated_image_001.png"},
		{"image/jpeg", 10, "generated_image_010.jpg"},
		{"image/gif", 999, "generated_image_999.gif"},
		{"application/pdf", 42, "generated_document_042.pdf"},
		{"text/plain", 5, "generated_text_005.txt"},
		{"application/json", 100, "generated_data_100.json"},
		{"unknown/type", 7, "generated_file_007"},
	}

	for _, tc := range testCases {
		t.Run(tc.mimeType, func(t *testing.T) {
			result := generateFilenameFromMimeType(tc.mimeType, tc.counter)
			if result != tc.expectedFile {
				t.Errorf("generateFilenameFromMimeType(%s, %d) = %s, expected %s",
					tc.mimeType, tc.counter, result, tc.expectedFile)
			}
		})
	}
}

// TestInlineDataCounterReset tests that the inlineData counter resets for each streaming session
func TestInlineDataCounterReset(t *testing.T) {
	router, db := setupTest(t)

	// Create a simple 1x1 PNG image (base64 encoded)
	pngData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

	// Mock the SendMessageStream method to simulate inlineData response
	provider := GlobalModelsRegistry.GetProvider(DefaultGeminiModel)
	mockLLMProvider := provider.(*MockLLMProvider)

	var callCount int = 0
	mockLLMProvider.SendMessageStreamFunc = func(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
		callCount++

		responses := []GenerateContentResponse{
			{
				Candidates: []Candidate{
					{
						Content: Content{
							Parts: []Part{
								{
									InlineData: &InlineData{
										MimeType: "image/png",
										Data:     pngData,
									},
								},
							},
						},
					},
				},
			},
		}

		seq := func(yield func(GenerateContentResponse) bool) {
			for _, resp := range responses {
				if !yield(resp) {
					return
				}
			}
		}

		return seq, &mockCloser{}, nil
	}

	t.Run("CounterReset", func(t *testing.T) {
		// First request
		payload1 := map[string]interface{}{
			"message": "Generate first image",
		}
		body1, _ := json.Marshal(payload1)
		resp1 := testStreamingRequest(t, router, "POST", "/api/chat", body1, http.StatusOK)
		defer resp1.Body.Close()

		var firstSessionId string
		for event := range parseSseStream(t, resp1) {
			if event.Type == EventInitialState {
				var initialState InitialState
				err := json.Unmarshal([]byte(event.Payload), &initialState)
				if err != nil {
					t.Fatalf("Failed to unmarshal initialState: %v", err)
				}
				firstSessionId = initialState.SessionId
				break
			}
		}

		// Second request (should reset counter)
		payload2 := map[string]interface{}{
			"message": "Generate second image",
		}
		body2, _ := json.Marshal(payload2)
		resp2 := testStreamingRequest(t, router, "POST", "/api/chat", body2, http.StatusOK)
		defer resp2.Body.Close()

		var secondSessionId string
		for event := range parseSseStream(t, resp2) {
			if event.Type == EventInitialState {
				var initialState InitialState
				err := json.Unmarshal([]byte(event.Payload), &initialState)
				if err != nil {
					t.Fatalf("Failed to unmarshal initialState: %v", err)
				}
				secondSessionId = initialState.SessionId
				break
			}
		}

		// Verify we have different session IDs (new sessions)
		if firstSessionId == secondSessionId {
			t.Errorf("Expected different session IDs, got same: %s", firstSessionId)
		}

		// First, let's just check if any messages exist in the sessions
		var firstMessageCount int
		err1 := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", firstSessionId).Scan(&firstMessageCount)
		if err1 != nil {
			t.Fatalf("Failed to count messages in first session: %v", err1)
		}
		t.Logf("First session has %d messages", firstMessageCount)

		var secondMessageCount int
		err2 := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", secondSessionId).Scan(&secondMessageCount)
		if err2 != nil {
			t.Fatalf("Failed to count messages in second session: %v", err2)
		}
		t.Logf("Second session has %d messages", secondMessageCount)

		// Check for messages with attachments
		var firstAttachmentCount int
		err3 := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND attachments IS NOT NULL AND attachments != '[]'", firstSessionId).Scan(&firstAttachmentCount)
		if err3 != nil {
			t.Fatalf("Failed to count attachment messages in first session: %v", err3)
		}
		t.Logf("First session has %d messages with attachments", firstAttachmentCount)

		var secondAttachmentCount int
		err4 := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND attachments IS NOT NULL AND attachments != '[]'", secondSessionId).Scan(&secondAttachmentCount)
		if err4 != nil {
			t.Fatalf("Failed to count attachment messages in second session: %v", err4)
		}
		t.Logf("Second session has %d messages with attachments", secondAttachmentCount)

		// For now, just expect that attachment messages exist (don't check exact filenames)
		if firstAttachmentCount == 0 {
			t.Errorf("Expected at least 1 attachment message in first session, got 0")
		}
		if secondAttachmentCount == 0 {
			t.Errorf("Expected at least 1 attachment message in second session, got 0")
		}

		// Now let's check the actual filenames to verify counter reset functionality
		// Query the first session's attachment messages
		rows, err := db.Query(`
			SELECT id, attachments FROM messages
			WHERE session_id = ? AND attachments IS NOT NULL AND attachments != '[]'
			ORDER BY created_at ASC
		`, firstSessionId)
		if err != nil {
			t.Fatalf("Failed to query messages with attachments: %v", err)
		}
		defer rows.Close()

		var firstFilenames []string
		for rows.Next() {
			var messageID int
			var attachmentsJSON string
			if err := rows.Scan(&messageID, &attachmentsJSON); err != nil {
				t.Fatalf("Failed to scan message: %v", err)
			}

			// Parse attachments JSON
			var attachments []FileAttachment
			if err := json.Unmarshal([]byte(attachmentsJSON), &attachments); err != nil {
				t.Fatalf("Failed to unmarshal attachments: %v", err)
			}

			// Collect all filenames from this message
			for _, attachment := range attachments {
				if attachment.FileName != "" {
					firstFilenames = append(firstFilenames, attachment.FileName)
				}
			}
		}

		// Query the second session's attachment messages
		rows2, err := db.Query(`
			SELECT id, attachments FROM messages
			WHERE session_id = ? AND attachments IS NOT NULL AND attachments != '[]'
			ORDER BY created_at ASC
		`, secondSessionId)
		if err != nil {
			t.Fatalf("Failed to query messages with attachments for second session: %v", err)
		}
		defer rows2.Close()

		var secondFilenames []string
		for rows2.Next() {
			var messageID int
			var attachmentsJSON string
			if err := rows2.Scan(&messageID, &attachmentsJSON); err != nil {
				t.Fatalf("Failed to scan message for second session: %v", err)
			}

			// Parse attachments JSON
			var attachments []FileAttachment
			if err := json.Unmarshal([]byte(attachmentsJSON), &attachments); err != nil {
				t.Fatalf("Failed to unmarshal attachments for second session: %v", err)
			}

			// Collect all filenames from this message
			for _, attachment := range attachments {
				if attachment.FileName != "" {
					secondFilenames = append(secondFilenames, attachment.FileName)
				}
			}
		}

		t.Logf("First session filenames: %v", firstFilenames)
		t.Logf("Second session filenames: %v", secondFilenames)

		// Find the first image filename in each session (should be generated_image_001.png)
		var firstImageFilename string
		for _, filename := range firstFilenames {
			if strings.Contains(filename, "generated_image_") && strings.HasSuffix(filename, ".png") {
				firstImageFilename = filename
				break
			}
		}

		var secondImageFilename string
		for _, filename := range secondFilenames {
			if strings.Contains(filename, "generated_image_") && strings.HasSuffix(filename, ".png") {
				secondImageFilename = filename
				break
			}
		}

		// Both should start with 001 since counter resets for each streaming session
		if firstImageFilename != "generated_image_001.png" {
			t.Errorf("Expected first filename to be 'generated_image_001.png', got '%s'", firstImageFilename)
		}

		if secondImageFilename != "generated_image_001.png" {
			t.Errorf("Expected second filename to be 'generated_image_001.png', got '%s'", secondImageFilename)
		}
	})
}

// Use existing mockCloser from integration_msgchain_test.go
