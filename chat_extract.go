package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

// extractSessionHandler extracts messages from a specific branch up to a given message and creates a new session.
func extractSessionHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		MessageID string `json:"messageId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "extractSessionHandler") {
		return
	}

	if requestBody.MessageID == "" {
		sendBadRequestError(w, r, "Message ID is required")
		return
	}

	// Parse message ID
	targetMessageID, err := strconv.Atoi(requestBody.MessageID)
	if err != nil {
		sendBadRequestError(w, r, "Invalid message ID")
		return
	}

	// Get the original session to validate existence
	originalSession, err := database.GetSession(db, sessionId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendNotFoundError(w, r, "Session not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to get session")
		}
		return
	}

	// Get the target message to find its branch
	targetMessage, err := database.GetMessageByID(db, targetMessageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendNotFoundError(w, r, "Target message not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to get target message")
		}
		return
	}

	// Validate that the target message belongs to the session
	if targetMessage.SessionID != sessionId {
		sendBadRequestError(w, r, "Target message does not belong to the specified session")
		return
	}

	// Get all messages from the session up to the target message, following branch history
	// Use GetSessionHistory to get the complete message chain without alterations
	completeMessages, err := database.GetSessionHistory(db, sessionId, targetMessage.BranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to get messages from branch")
		return
	}

	// Filter messages to only include those up to the target message
	var frontendMessages []FrontendMessage
	for _, msg := range completeMessages {
		msgID, err := strconv.Atoi(msg.ID)
		if err != nil {
			log.Printf("extractSessionHandler: Failed to parse message ID %s: %v", msg.ID, err)
			continue
		}

		if msgID <= targetMessageID {
			frontendMessages = append(frontendMessages, msg)
		} else {
			break // Stop when we reach a message beyond our target
		}
	}

	// Process compression remapping
	processedMessages, err := processCompressionRemapping(db, frontendMessages)
	if err != nil {
		log.Printf("extractSessionHandler: Failed to process compression remapping: %v", err)
		// Continue with original messages if remapping fails
		processedMessages = frontendMessages
	}

	// Create a new session
	newSessionId := database.GenerateID()

	// Collect subsession IDs from subagent FunctionResponse messages
	subsessionIDs := collectSubsessionIDs(processedMessages)

	// Copy subsessions if any exist
	if len(subsessionIDs) > 0 {
		err := copySubsessionsToNewSession(db, sessionId, newSessionId, subsessionIDs)
		if err != nil {
			log.Printf("extractSessionHandler: Failed to copy subsessions: %v", err)
			// Non-fatal error, continue without subsessions
		}
	}

	// Generate a name for the new session
	newSessionName := generateCopySessionName(originalSession.Name)

	// Use the already evaluated system prompt from original session
	// originalSession.SystemPrompt is already evaluated, so use it directly

	// Create the new session
	newPrimaryBranchID, err := database.CreateSession(db, newSessionId, originalSession.SystemPrompt, originalSession.WorkspaceID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create new session")
		return
	}

	// Set the name for the new session
	if err := database.UpdateSessionName(db, newSessionId, newSessionName); err != nil {
		log.Printf("Failed to set name for new session %s: %v", newSessionId, err)
		// Non-fatal error, continue
	}

	// Create a new message chain for the new session
	mc, err := database.NewMessageChain(r.Context(), db, newSessionId, newPrimaryBranchID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to create new message chain")
		return
	}

	// Add processed messages to the new session by finding original messages and copying all fields
	for _, processedMsg := range processedMessages {
		// Convert FrontendMessage ID back to integer to find original message
		originalMsgID, err := strconv.Atoi(processedMsg.ID)
		if err != nil {
			log.Printf("extractSessionHandler: Failed to parse message ID %s: %v", processedMsg.ID, err)
			continue
		}

		// Get the original message to preserve all fields
		originalMsg, err := database.GetMessageByID(db, originalMsgID)
		if err != nil {
			log.Printf("extractSessionHandler: Failed to get original message %d: %v", originalMsgID, err)
			continue
		}

		// Skip system_prompt messages as they're handled differently
		if originalMsg.Type == TypeSystemPrompt {
			continue
		}

		// Update text content from processed message (in case compression remapping occurred)
		// Convert parts back to proper Message.Text and State format
		if len(processedMsg.Parts) > 0 {
			text, state, err := frontendMessageToText(originalMsg.Type, processedMsg.Parts)
			if err != nil {
				log.Printf("extractSessionHandler: Failed to convert parts to text for message %d: %v", originalMsgID, err)
				// Fall back to original text if conversion fails
			} else if text != "" {
				originalMsg.Text = text
				originalMsg.State = state
			}
		}

		// Reset IDs and session references for the new session
		originalMsg.ID = 0 // Will be assigned by database
		originalMsg.SessionID = newSessionId
		originalMsg.BranchID = newPrimaryBranchID
		originalMsg.ParentMessageID = nil // Will be set by MessageChain.Add()
		originalMsg.ChosenNextID = nil    // Will be set by MessageChain.Add()
		originalMsg.Indexed = 0           // Reset indexed to 0 to trigger reindex when needed

		// Add the message to the new session with all original fields preserved
		_, err = mc.Add(r.Context(), db, *originalMsg)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to add message %d to new session", originalMsgID))
			return
		}
	}

	// Update last_updated_at for the new session
	if err := database.UpdateSessionLastUpdated(db, newSessionId); err != nil {
		log.Printf("Failed to update last_updated_at for new session %s: %v", newSessionId, err)
		// Non-fatal error, continue with response
	}

	// Return the new session link
	response := map[string]string{
		"status":      "success",
		"sessionId":   newSessionId,
		"sessionName": newSessionName,
		"link":        fmt.Sprintf("/%s", newSessionId),
		"message":     "Session extracted successfully",
	}

	sendJSONResponse(w, response)
}

// processCompressionRemapping processes compression messages and remaps their content
func processCompressionRemapping(db *sql.DB, messages []FrontendMessage) ([]FrontendMessage, error) {
	var processed []FrontendMessage

	for _, msg := range messages {
		if msg.Type == TypeCompression {
			// Handle compression message remapping
			if len(msg.Parts) > 0 && msg.Parts[0].Text != "" {
				// Parse compression data and remap if needed
				var compressionData map[string]interface{}
				if err := json.Unmarshal([]byte(msg.Parts[0].Text), &compressionData); err != nil {
					log.Printf("Failed to parse compression data: %v", err)
					processed = append(processed, msg)
					continue
				}

				// Remap compression content as needed
				remappedText, err := remapCompressionContent(db, compressionData)
				if err != nil {
					log.Printf("Failed to remap compression content: %v", err)
					processed = append(processed, msg)
					continue
				}

				// Create a new message with remapped content
				remappedMsg := msg
				if len(remappedMsg.Parts) > 0 {
					remappedMsg.Parts[0].Text = remappedText
				}
				processed = append(processed, remappedMsg)
			} else {
				processed = append(processed, msg)
			}
		} else {
			processed = append(processed, msg)
		}
	}

	return processed, nil
}

// remapCompressionContent remaps the content of a compression message based on the actual messages
func remapCompressionContent(db *sql.DB, compressionData map[string]interface{}) (string, error) {
	// Extract the original message IDs from compression data
	messageIDs, ok := compressionData["messageIds"].([]interface{})
	if !ok {
		return "", fmt.Errorf("invalid compression data format")
	}

	var remappedContent []string
	for _, msgID := range messageIDs {
		id, ok := msgID.(float64)
		if !ok {
			continue
		}

		// Get the actual message content
		msg, err := database.GetMessageByID(db, int(id))
		if err != nil {
			log.Printf("Failed to get message %d for compression remapping: %v", int(id), err)
			continue
		}

		remappedContent = append(remappedContent, fmt.Sprintf("Message %d: %s", msg.ID, msg.Text))
	}

	return strings.Join(remappedContent, "\n\n"), nil
}

var copiedSessionNamePattern = regexp.MustCompile(`^(.*?)(?:\s*\(Copy(?:\s+(\d+))?\))?$`)

// collectSubsessionIDs collects all subsession IDs from subagent FunctionResponse messages
func collectSubsessionIDs(messages []FrontendMessage) []string {
	var subsessionIDs []string
	seenIDs := make(map[string]bool) // Avoid duplicates

	for _, msg := range messages {
		if msg.Type == TypeFunctionResponse {
			// Check if this is a subagent response
			for _, part := range msg.Parts {
				if part.FunctionResponse != nil && part.FunctionResponse.Name == "subagent" {
					// Extract subagent ID from the response
					if response, ok := part.FunctionResponse.Response.(map[string]interface{}); ok {
						if subagentID, exists := response["subagent_id"].(string); exists {
							if !seenIDs[subagentID] {
								subsessionIDs = append(subsessionIDs, subagentID)
								seenIDs[subagentID] = true
							}
						}
					}
				}
			}
		}
	}

	return subsessionIDs
}

// copySubsessionsToNewSession copies all subsessions to the new session
func copySubsessionsToNewSession(db *sql.DB, sessionId string, newSessionID string, subsessionIDs []string) error {
	for _, subsessionID := range subsessionIDs {
		// Create full session IDs
		// fullSessionId = sessionId "." subsessionId
		// newFullSessionId = newSessionId "." subsessionId
		fullSessionID := fmt.Sprintf("%s.%s", sessionId, subsessionID)
		newFullSessionID := fmt.Sprintf("%s.%s", newSessionID, subsessionID)

		// Get the original subsession
		originalSubsession, err := database.GetSession(db, fullSessionID)
		if err != nil {
			log.Printf("Failed to get subsession %s: %v", fullSessionID, err)
			continue
		}

		// Create new subsession with new full session ID and new session ID as parent
		_, err = database.CreateSession(db, newFullSessionID, originalSubsession.SystemPrompt, newSessionID)
		if err != nil {
			log.Printf("Failed to create new subsession %s: %v", newFullSessionID, err)
			continue
		}

		// Copy all messages from original subsession to new subsession
		err = copyMessagesBetweenSessions(db, fullSessionID, newFullSessionID)
		if err != nil {
			log.Printf("Failed to copy messages from subsession %s to %s: %v", fullSessionID, newFullSessionID, err)
			continue
		}
	}

	return nil
}

// copyMessagesBetweenSessions copies all messages from one session to another
func copyMessagesBetweenSessions(db *sql.DB, sourceSessionID, targetSessionID string) error {
	// Get all messages from source session
	sourceMessages, err := database.GetSessionHistory(db, sourceSessionID, "")
	if err != nil {
		return fmt.Errorf("failed to get messages from source session: %w", err)
	}

	// Create message chain for target session
	targetBranchID, err := database.GetSessionPrimaryBranchID(db, targetSessionID)
	if err != nil {
		return fmt.Errorf("failed to get primary branch ID for target session: %w", err)
	}

	mc, err := database.NewMessageChain(context.Background(), db, targetSessionID, targetBranchID)
	if err != nil {
		return fmt.Errorf("failed to create message chain for target session: %w", err)
	}

	// Copy each message
	for _, msg := range sourceMessages {
		// Get the original message to preserve all fields
		originalMsgID, err := strconv.Atoi(msg.ID)
		if err != nil {
			log.Printf("Failed to parse message ID %s: %v", msg.ID, err)
			continue
		}

		originalMsg, err := database.GetMessageByID(db, originalMsgID)
		if err != nil {
			log.Printf("Failed to get original message %d: %v", originalMsgID, err)
			continue
		}

		// Reset IDs and session references for the new session
		originalMsg.ID = 0
		originalMsg.SessionID = targetSessionID
		originalMsg.BranchID = targetBranchID
		originalMsg.ParentMessageID = nil // Will be set by MessageChain.Add()
		originalMsg.ChosenNextID = nil
		originalMsg.Indexed = 0 // Reset indexed to 0

		// Add the message to the target session
		_, err = mc.Add(context.Background(), db, *originalMsg)
		if err != nil {
			log.Printf("Failed to add message %d to target session: %v", originalMsgID, err)
			continue
		}
	}

	return nil
}

// frontendMessageToText converts FrontendMessage.Parts back to Message.Text and State format
func frontendMessageToText(msgType MessageType, parts []Part) (string, string, error) {
	switch msgType {
	case TypeFunctionCall:
		if len(parts) > 0 && parts[0].FunctionCall != nil {
			jsonBytes, err := json.Marshal(parts[0].FunctionCall)
			if err != nil {
				return "", "", err
			}
			// For function calls, return the JSON and the thoughtSignature (state)
			state := ""
			if len(parts) > 0 {
				state = parts[0].ThoughtSignature
			}
			return string(jsonBytes), state, nil
		}
		return "", "", nil

	case TypeFunctionResponse:
		if len(parts) > 0 && parts[0].FunctionResponse != nil {
			jsonBytes, err := json.Marshal(parts[0].FunctionResponse)
			if err != nil {
				return "", "", err
			}
			// For function responses, return the JSON and the thoughtSignature (state)
			state := ""
			if len(parts) > 0 {
				state = parts[0].ThoughtSignature
			}
			return string(jsonBytes), state, nil
		}
		return "", "", nil

	case TypeCompression:
		// For compression messages, reconstruct the "ID\nSummary" format
		if len(parts) > 0 && parts[0].Text != "" {
			state := ""
			if len(parts) > 0 {
				state = parts[0].ThoughtSignature
			}
			return parts[0].Text, state, nil
		}
		return "", "", nil

	default:
		// For regular text messages (TypeUserText, TypeModelText, etc.)
		if len(parts) == 0 {
			return "", "", nil
		}

		// Combine all text parts and determine thoughtSignature
		var textBuilder strings.Builder
		var thoughtSignature string

		for _, part := range parts {
			if part.Text != "" {
				textBuilder.WriteString(part.Text)
			}
			// Use the thoughtSignature from the first part that has one
			if thoughtSignature == "" && part.ThoughtSignature != "" {
				thoughtSignature = part.ThoughtSignature
			}
		}

		text := textBuilder.String()
		if text == "" {
			return "", "", nil
		}

		// Return separate text and state (thoughtSignature)
		return text, thoughtSignature, nil
	}
}

// generateCopySessionName generates a copy name for a session, incrementing the copy number if needed
func generateCopySessionName(originalName string) string {
	if originalName == "" {
		return "New Chat (Copy)"
	}

	// Check if the name already ends with "(Copy)" or "(Copy N)"
	baseName := originalName
	copyNum := 1

	// Use regex to extract copy number if it exists
	matches := copiedSessionNamePattern.FindStringSubmatch(originalName)
	if matches != nil && matches[1] != "" {
		baseName = matches[1]
		if matches[2] != "" {
			// Parse existing copy number
			if num, err := strconv.Atoi(matches[2]); err == nil {
				copyNum = num + 1
			} else {
				copyNum = math.MaxInt
			}
		} else {
			copyNum = 2 // Original was just "(Copy)", so next is 2
		}
	}

	// Generate new name
	if copyNum == 1 {
		return fmt.Sprintf("%s (Copy)", baseName)
	}
	return fmt.Sprintf("%s (Copy %d)", baseName, copyNum)
}
