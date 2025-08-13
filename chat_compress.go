package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp" // Added import
	"strconv"
	"strings"
)

// Constants for compression logic (from gemini-cli/packages/core/src/core/client.ts)
const (
	COMPRESSION_TOKEN_THRESHOLD    = 0.7
	COMPRESSION_PRESERVE_THRESHOLD = 0.3
)

type CompressResult struct {
	OriginalTokenCount int
	NewTokenCount      int
	CompressionMsgID   int
}

var stateSnapshotPattern = regexp.MustCompile(`(?s)<state_snapshot>(.*?)</state_snapshot>`)

// extractStateSnapshotContent extracts the content within <state_snapshot> tags.
func extractStateSnapshotContent(xmlContent string) string {
	matches := stateSnapshotPattern.FindStringSubmatch(xmlContent)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return xmlContent // Fallback to full XML if tags not found
}

func CompressSession(ctx context.Context, db *sql.DB, sessionID string, modelName string) (result CompressResult, err error) {
	// 1. Load session and all messages from the database for the given sessionID.
	session, err := GetSession(db, sessionID)
	if err != nil {
		err = fmt.Errorf("Failed to get session %s: %w", sessionID, err)
		return
	}

	// Get all messages for the session, including thoughts, to accurately represent history for compression.
	// We'll filter thoughts later if needed for the context sent to the LLM for summarization.
	allMessages, err := GetSessionHistory(db, sessionID, session.PrimaryBranchID)
	if err != nil {
		err = fmt.Errorf("Failed to get session history for %s: %w", sessionID, err)
		return
	}

	if len(allMessages) == 0 {
		return // No messages to compress
	}

	// Convert FrontendMessage to Content for LLM interaction
	var curatedHistory []Content
	for _, msg := range allMessages {
		// Only include user and model messages for compression context, similar to gemini-cli's behavior
		// where it curates history before compression.
		// We might need to refine this based on how gemini-cli's `getHistory(true)` works.
		// For now, assuming simple text content.
		if msg.Role == "user" || msg.Role == "model" {
			curatedHistory = append(curatedHistory, Content{
				Role:  msg.Role,
				Parts: msg.Parts,
			})
		}
	}

	if len(curatedHistory) == 0 {
		return // No compressible messages found
	}

	// Determine the model to use for token counting and generation
	// For now, use the default model. In a real scenario, this might come from session config.
	provider, ok := CurrentProviders[modelName]
	if !ok {
		err = fmt.Errorf("Unsupported model: %s", modelName)
		return
	}

	// 2. Calculate originalTokenCount.
	originalTokenResp, err := provider.CountTokens(context.Background(), curatedHistory, modelName)
	if err != nil {
		err = fmt.Errorf("CountTokens API call failed: %w", err)
		return
	}
	originalTokenCount := originalTokenResp.TotalTokens

	// 3. Apply COMPRESSION_TOKEN_THRESHOLD.
	// Forcing compression for now, similar to gemini-cli's `tryCompressChat(..., true)`
	// In a real scenario, we might check if originalTokenCount exceeds a threshold.
	// modelMaxTokens := provider.MaxTokens() // Need to get max tokens for the model
	// if float64(originalTokenCount) < COMPRESSION_TOKEN_THRESHOLD * float64(modelMaxTokens) {
	// 	sendJSONResponse(w, map[string]string{"status": "success", "message": "History is below compression threshold"})
	// 	return
	// }

	// 4. Implement findIndexAfterFraction and split history.
	compressBeforeIndex := findIndexAfterFraction(curatedHistory, 1-COMPRESSION_PRESERVE_THRESHOLD)

	// Ensure compressBeforeIndex is not out of bounds and points to a valid message to start compression from.
	// Adjust to ensure we don't split a user/model turn in half.
	// This part might need more sophisticated logic to match gemini-cli's `checkNextSpeaker` and `isFunctionResponse`
	// For simplicity, we'll just ensure it's not the very first message.
	if compressBeforeIndex == 0 && len(curatedHistory) > 1 {
		compressBeforeIndex = 1 // Always keep at least the first message if history is long enough
	}

	historyToCompress := curatedHistory[:compressBeforeIndex]

	var compressedUpToMessageID *int
	if len(historyToCompress) > 0 {
		var parsedID int
		parsedID, err = strconv.Atoi(allMessages[compressBeforeIndex-1].ID) // Get the last FrontendMessage that was compressed
		if err != nil {
			err = fmt.Errorf("Failed to parse last message ID in historyToMessage: %w", err)
			return
		}
		compressedUpToMessageID = &parsedID
	}

	var compressionMsgParentID *int
	if len(allMessages) > 0 {
		var parsedID int
		parsedID, err = strconv.Atoi(allMessages[len(allMessages)-1].ID) // Get the ID of the last message in allMessages
		if err != nil {
			err = fmt.Errorf("Failed to parse parent message ID for compression message: %w", err)
			return
		}
		compressionMsgParentID = &parsedID
	}

	// 5. Construct LLM request with historyToCompress and getCompressionPrompt().
	systemPrompt, triggerPrompt := GetCompressionPrompt()
	llmRequestContents := historyToCompress // Start with the history to compress
	llmRequestContents = append(llmRequestContents, Content{
		Role: "user",
		Parts: []Part{
			{Text: triggerPrompt},
		},
	})

	// 6. Call LLM to get summary (XML format) using GenerateContentOneShot.
	// Specific generation parameters for compression
	compressionGenParams := SessionGenerationParams{
		Temperature: 0.0,
		TopK:        -1,
		TopP:        1.0,
	}

	oneShotResult, err := provider.GenerateContentOneShot(ctx, SessionParams{
		Contents:         llmRequestContents,
		SystemPrompt:     systemPrompt,
		ModelName:        modelName,
		IncludeThoughts:  false,
		GenerationParams: &compressionGenParams,
	})
	if err != nil {
		err = fmt.Errorf("GenerateContentOneShot API call failed for compression: %w", err)
		return
	}
	if oneShotResult.Text == "" {
		err = fmt.Errorf("LLM returned empty summary")
		return
	}

	// Define extractedSummary by extracting content from <state_snapshot>
	extractedSummary := extractStateSnapshotContent(oneShotResult.Text)

	// 7. Parse the XML summary (optional, but good for validation/extraction if needed).
	// For now, we'll store the raw XML in the message text.

	// 8. Database Update:
	//    a. Create a new "compression" type message.
	//    b. Store summary XML in the Text field of the new message.
	//    c. Update parent_message_id and chosen_next_id to link messages correctly.

	// Start a transaction for atomicity
	tx, err := db.Begin()
	if err != nil {
		err = fmt.Errorf("Failed to begin transaction: %w", err)
		return
	}
	defer tx.Rollback() // Rollback on error

	// Find the last message in the current branch to link the new compression message
	if err != nil {
		err = fmt.Errorf("Failed to get last message in branch: %w", err)
		return
	}

	// Create the new compression message
	compressionMsg := Message{
		SessionID:       sessionID,
		BranchID:        session.PrimaryBranchID,
		Role:            "user",
		Type:            "compression",
		Text:            fmt.Sprintf("%d\n%s", *compressedUpToMessageID, extractedSummary),
		ParentMessageID: compressionMsgParentID,
		Model:           modelName,
		// CumulTokenCount will be updated after adding the message
	}

	newCompressionMsgID, err := AddMessageToSession(ctx, tx, compressionMsg)
	if err != nil {
		err = fmt.Errorf("Failed to add compression message to session: %w", err)
		return
	}

	// Update the chosen_next_id of the message *before* the compressed block
	if compressionMsgParentID != nil {
		err = UpdateMessageChosenNextID(tx, *compressionMsgParentID, &newCompressionMsgID)
		if err != nil {
			err = fmt.Errorf("Failed to update chosen_next_id for message %d: %w", *compressionMsgParentID, err)
			return
		}
	}

	// Combine compression message content and subsequent messages for a single token count
	var combinedContentForTokenCount []Content

	// Add compression message content
	combinedContentForTokenCount = append(combinedContentForTokenCount, Content{
		Role:  "user", // Compression message role is "user"
		Parts: []Part{{Text: extractedSummary}},
	})

	// Find the index of the newCompressionMsgID in allMessages
	startIndex := -1
	for i, msg := range allMessages {
		if strconv.Itoa(newCompressionMsgID) == msg.ID {
			startIndex = i + 1 // Start from the message after the compression message
			break
		}
		// If the message is the compression message itself, and it's the first message in allMessages,
		// then startIndex should be 0, and we should include all messages from allMessages.
		if i == 0 && msg.Type == "compression" && strconv.Itoa(newCompressionMsgID) == msg.ID {
			startIndex = 0
			break
		}
	}

	if startIndex != -1 {
		for i := startIndex; i < len(allMessages); i++ {
			msg := allMessages[i]

			var contentParts []Part
			var contentRole string = msg.Role // Default role

			switch msg.Type {
			case "function_call":
				var fc FunctionCall
				// Assuming FunctionCall is stored as JSON in msg.Parts[0].Text
				if len(msg.Parts) > 0 && msg.Parts[0].Text != "" {
					if err := json.Unmarshal([]byte(msg.Parts[0].Text), &fc); err != nil {
						log.Printf("Failed to unmarshal FunctionCall for message %s: %v", msg.ID, err)
						continue // Skip if malformed
					}
					contentParts = append(contentParts, Part{FunctionCall: &fc})
				} else {
					log.Printf("Warning: Malformed function_call message %s: no text part", msg.ID)
					continue
				}
			case "function_response":
				var fr FunctionResponse
				// Assuming FunctionResponse is stored as JSON in msg.Parts[0].Text
				if len(msg.Parts) > 0 && msg.Parts[0].Text != "" {
					if err := json.Unmarshal([]byte(msg.Parts[0].Text), &fr); err != nil {
						log.Printf("Failed to unmarshal FunctionResponse for message %s: %v", msg.ID, err)
						continue // Skip if malformed
					}
					contentParts = append(contentParts, Part{FunctionResponse: &fr})
				} else {
					log.Printf("Warning: Malformed function_response message %s: no text part", msg.ID)
					continue
				}
			case "compression":
				// For compression messages, the text is in the format "<CompressedUpToMessageId>\n<XMLSummary>"
				if len(msg.Parts) > 0 && msg.Parts[0].Text != "" {
					_, textAfter, found := strings.Cut(msg.Parts[0].Text, "\n")
					if found {
						contentParts = []Part{{Text: textAfter}} // XMLSummary is the second part
						contentRole = "user"                     // Role for compression message is "user"
					} else {
						log.Printf("Warning: Malformed compression message text for message %s: %s", msg.ID, msg.Parts[0].Text)
						continue // Skip if malformed
					}
				} else {
					log.Printf("Warning: Malformed compression message %s: no text part", msg.ID)
					continue
				}
			case "thought", "model_error":
				continue // Ignore these types
			default: // Handles "text" and other types
				// Handle text parts from msg.Parts
				if len(msg.Parts) > 0 {
					for _, part := range msg.Parts {
						if part.Text != "" {
							contentParts = append(contentParts, Part{Text: part.Text})
						}
					}
				}
				// Handle attachments from msg.Attachments
				for _, attachment := range msg.Attachments {
					if attachment.Hash != "" {
						blobData, err := GetBlob(db, attachment.Hash) // Use the db connection
						if err != nil {
							log.Printf("Warning: Failed to retrieve blob for hash %s: %v", attachment.Hash, err)
							continue // Skip if blob cannot be retrieved
						}
						encodedData := base64.StdEncoding.EncodeToString(blobData)
						contentParts = append(contentParts, Part{InlineData: &InlineData{MimeType: attachment.MimeType, Data: encodedData}})
					}
				}
			}

			if len(contentParts) > 0 {
				combinedContentForTokenCount = append(combinedContentForTokenCount, Content{
					Role:  contentRole,
					Parts: contentParts,
				})
			}
		}
	}

	// Perform a single CountTokens call for the combined content
	newTokenResp, err := provider.CountTokens(ctx, combinedContentForTokenCount, modelName)
	if err != nil {
		err = fmt.Errorf("CountTokens API call failed for combined history: %w", err)
		return
	}
	newTotalTokenCount := newTokenResp.TotalTokens

	// Update the cumul_token_count for the new compression message
	err = UpdateMessageTokens(tx, newCompressionMsgID, newTotalTokenCount)
	if err != nil {
		err = fmt.Errorf("Failed to update token count for compression message: %w", err)
		return
	}

	result.NewTokenCount = newTotalTokenCount

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("Failed to commit transaction: %w", err)
		return
	}

	result.OriginalTokenCount = originalTokenCount
	result.NewTokenCount = newTotalTokenCount
	result.CompressionMsgID = newCompressionMsgID
	return
}

// findIndexAfterFraction returns the index of the content after the fraction of the total characters in the history.
// (Adapted from gemini-cli/packages/core/src/core/client.ts)
func findIndexAfterFraction(history []Content, fraction float64) int {
	if fraction <= 0 || fraction >= 1 {
		// This should ideally be an error, but for now, return 0 or len(history)
		// to avoid panicking, similar to how the JS version might behave.
		if fraction <= 0 {
			return 0
		}
		return len(history)
	}

	contentLengths := make([]int, len(history))
	totalCharacters := 0
	for i, content := range history {
		// Simple approximation of content length by JSON stringifying.
		// A more accurate token count would be better, but this matches the JS logic.
		jsonBytes, _ := json.Marshal(content)
		contentLengths[i] = len(jsonBytes)
		totalCharacters += contentLengths[i]
	}

	targetCharacters := float64(totalCharacters) * fraction

	charactersSoFar := 0
	for i := 0; i < len(contentLengths); i++ {
		charactersSoFar += contentLengths[i]
		if float64(charactersSoFar) >= targetCharacters {
			return i
		}
	}
	return len(history)
}
