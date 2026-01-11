package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	. "github.com/lifthrasiir/angel/internal/types"
)

// GetMessagePossibleNextIDs retrieves all possible next message IDs and their branch IDs for a given message ID.
func GetMessagePossibleNextIDs(db SessionDbOrTx, messageID int) ([]PossibleNextMessage, error) {
	rows, err := db.Query("SELECT id, branch_id FROM S.messages WHERE parent_message_id = ?", messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to query possible next message IDs: %w", err)
	}
	defer rows.Close()

	var nextIDs []PossibleNextMessage
	for rows.Next() {
		var next PossibleNextMessage
		if err := rows.Scan(&next.MessageID, &next.BranchID); err != nil {
			return nil, fmt.Errorf("failed to scan next message ID and branch ID: %w", err)
		}
		nextIDs = append(nextIDs, next)
	}
	return nextIDs, nil
}

// getSessionHistoryInternal retrieves the chat history for a given session and its primary branch,
// recursively fetching messages from parent branches. It allows for discarding thoughts
// and history alteration through compression/clear/clearblobs commands.
//
// If fetchLimit is > 0, cursor-based pagination using beforeMessageID and fetchLimit is done.
// If beforeMessageID is 0, it fetches the latest messages.
func getSessionHistoryInternal(
	db SessionDbOrTx,
	primaryBranchID string,
	discardThoughts bool,
	canAlterHistory bool,
	beforeMessageID int, // Cursor: fetch messages with ID less than this
	fetchLimit int, // Number of messages to fetch
) ([]FrontendMessage, error) {
	// Determine if this is a paginated call that might have over-fetched
	isPaginatedCall := (beforeMessageID != 0 || fetchLimit > 0)
	branchID := primaryBranchID
	keepGoing := true

	// History alteration flags
	var compressUpToID int = 0
	var clearSeen bool = false
	var clearblobsSeen bool = false

	// Determine the initial message ID limit for the query
	messageIdLimit := math.MaxInt
	if beforeMessageID != 0 {
		messageIdLimit = beforeMessageID - 1
	}

	// For first message editing support, find the starting point using chosen_first_id
	var startingMessageID int
	var chosenFirstID sql.NullInt64
	err := db.QueryRow("SELECT chosen_first_id FROM S.sessions WHERE id = ?", db.SessionId()).Scan(&chosenFirstID)
	if err != nil || !chosenFirstID.Valid {
		// If no chosen_first_id is set, fall back to the original behavior
		startingMessageID = 1
	} else {
		startingMessageID = int(chosenFirstID.Int64)
	}

	// The starting message ID should not be greater than the upper limit
	if startingMessageID > messageIdLimit {
		// This means the cursor is before the chosen first message, return empty
		return nil, nil
	}

	isFullHistoryFetch := (fetchLimit <= 0)

	var history [][]FrontendMessage
	var currentMessageCount int
	for keepGoing && (isFullHistoryFetch || currentMessageCount < fetchLimit) {
		err := func() error {
			rows, err := db.Query(`
				SELECT
					m.id, m.session_id, m.branch_id, m.parent_message_id, m.chosen_next_id,
					m.text, m.type, m.attachments, m.cumul_token_count, m.created_at, m.model,
					m.state, m.aux, coalesce(
						json_group_array(
							json_object(
								'id', mm.id,
								'branchId', mm.branch_id,
								'text', COALESCE(mm.text, ''),
								'createdAt', COALESCE(mm.created_at, '')
							)
						),
						'[]'
					)
				FROM S.messages AS m LEFT OUTER JOIN S.messages AS mm ON m.id = mm.parent_message_id
				GROUP BY m.id
				HAVING m.branch_id = ? AND m.id >= ? AND m.id <= ?
				ORDER BY m.id ASC
			`, branchID, startingMessageID, messageIdLimit)
			if err != nil {
				return fmt.Errorf("failed to query branch messages: %w", err)
			}
			defer rows.Close()

			var messages []FrontendMessage
			var possibleBranches []PossibleNextMessage
			parentBranchMessageID := -1

			for rows.Next() {
				var m Message
				var attachmentsJSON sql.NullString
				var auxJSON sql.NullString
				var possibleNextIDsAndBranchesStr string
				if err := rows.Scan(
					&m.ID, &m.LocalSessionID, &m.BranchID, &m.ParentMessageID, &m.ChosenNextID,
					&m.Text, &m.Type, &attachmentsJSON, &m.CumulTokenCount, &m.CreatedAt, &m.Model,
					&m.State, &auxJSON, &possibleNextIDsAndBranchesStr,
				); err != nil {
					return fmt.Errorf("failed to scan message: %w", err)
				}

				// Store aux JSON string in m.Aux for later processing
				if auxJSON.Valid {
					m.Aux = auxJSON.String
				}

				if discardThoughts && m.Type == TypeThought {
					continue // Skip thought messages
				}

				// Check for compression message and parse ID before processing
				if m.Type == TypeCompression {
					before, _, found := strings.Cut(m.Text, "\n")
					if found {
						parsedID, err := strconv.Atoi(before)
						if err == nil {
							compressUpToID = parsedID
						} else {
							log.Printf("Warning: Failed to parse CompressedUpToMessageId from compression message %d: %v", m.ID, err)
						}
					} else {
						log.Printf("Warning: Malformed compression message text for message %d: %s", m.ID, m.Text)
					}
				}

				// Create frontend message with possibleBranches from previous iteration
				fm, _, err := createFrontendMessage(m, attachmentsJSON, possibleBranches, canAlterHistory, discardThoughts, clearblobsSeen)
				if err != nil {
					return fmt.Errorf("failed to create frontend message: %w", err)
				}

				if len(messages) == 0 && fm.ParentMessageID != nil {
					parentBranchMessageID = *m.ParentMessageID
				}
				messages = append(messages, fm)

				// Parse possibleNextIDs from current message to be used as possibleBranches for next message
				possibleBranches = parsePossibleNextIDs(possibleNextIDsAndBranchesStr)
			}

			if len(messages) == 0 {
				keepGoing = false
				return nil
			}

			// Set possibleBranches for the first message in this batch from previous iteration
			if len(history) > 0 {
				lastBatch := history[len(history)-1]
				if len(lastBatch) > 0 {
					lastBatch[0].PossibleBranches = possibleBranches
				}
			}

			// Apply history alteration filters if enabled
			if canAlterHistory {
				var filteredMessages []FrontendMessage

				// Process messages in reverse chronological order, setting flags and filtering in one pass
				for i := len(messages) - 1; i >= 0; i-- {
					msg := messages[i]
					msgID, err := strconv.Atoi(msg.ID)
					if err != nil {
						log.Printf("Warning: Failed to parse message ID %s: %v", msg.ID, err)
						continue
					}

					// Check for command messages and set flags
					if msg.Type == TypeCommand && len(msg.Parts) > 0 {
						switch msg.Parts[0].Text {
						case "clear":
							clearSeen = true
						case "clearblobs":
							clearblobsSeen = true
						}
					}

					// Apply filtering based on current flags (processed in reverse order)
					// Skip messages before compression (except the compression message itself)
					if msgID <= compressUpToID && msg.Type != TypeCompression {
						continue
					}

					// Skip messages before clear command
					if clearSeen {
						continue
					}

					// Process clearblobs (mark attachments as omitted)
					if clearblobsSeen {
						for i := range msg.Attachments {
							msg.Attachments[i].Omitted = true
						}
					}

					// Add to filtered list (will be in reverse order)
					filteredMessages = append(filteredMessages, msg)
				}

				// Reverse filteredMessages to restore chronological order
				for i, j := 0, len(filteredMessages)-1; i < j; i, j = i+1, j-1 {
					filteredMessages[i], filteredMessages[j] = filteredMessages[j], filteredMessages[i]
				}

				messages = filteredMessages

				// If clear was seen, no need to process parent branches
				if clearSeen {
					keepGoing = false
				}
			}

			history = append(history, messages)
			currentMessageCount += len(messages) // Update counter
			if parentBranchMessageID < 0 {
				// Reached the top level, use chosen_first_id as the final limit
				if startingMessageID > 1 {
					messageIdLimit = startingMessageID - 1
				} else {
					keepGoing = false
				}
			} else {
				messageIdLimit = parentBranchMessageID
				err := db.QueryRow("SELECT branch_id FROM S.messages WHERE id = ?", parentBranchMessageID).Scan(&branchID)
				if err != nil {
					return fmt.Errorf("failed to query parent branch ID: %w", err)
				}
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	// Calculate the actual number of messages to return
	numMessagesToReturn := currentMessageCount
	if !isFullHistoryFetch && currentMessageCount > fetchLimit {
		numMessagesToReturn = fetchLimit
	}

	// Iterate from the end of history backwards to get the latest messages
	// until numMessagesToReturn is reached.
	var combinedHistory []FrontendMessage
	for i := 0; i < len(history) && len(combinedHistory) < numMessagesToReturn; i++ {
		group := history[i]
		// Messages are in the chronological order within group.
		for j := len(group) - 1; j >= 0 && len(combinedHistory) < numMessagesToReturn; j-- {
			combinedHistory = append(combinedHistory, group[j])
		}
	}

	// Return in the chronological order.
	for i, j := 0, len(combinedHistory)-1; i < j; i, j = i+1, j-1 {
		combinedHistory[i], combinedHistory[j] = combinedHistory[j], combinedHistory[i]
	}

	// Handle paginated calls that might have over-fetched for possibleBranches
	if isPaginatedCall && !isFullHistoryFetch && fetchLimit > 0 && len(combinedHistory) > fetchLimit {
		// We have extra messages, so the first message in combinedHistory has its possibleNextIds
		// which should become possibleBranches for the second message (which becomes the first)
		if len(combinedHistory) >= 2 {
			// Get possibleNextIds from the first message (combinedHistory[0])
			firstMessageID, err := strconv.Atoi(combinedHistory[0].ID)
			if err != nil {
				log.Printf("getSessionHistoryInternal: Failed to parse message ID %s: %v", combinedHistory[0].ID, err)
			} else {
				nextIDs, err := GetMessagePossibleNextIDs(db, firstMessageID)
				if err != nil {
					log.Printf("getSessionHistoryInternal: Failed to get possible next IDs for message %s: %v", combinedHistory[0].ID, err)
				} else {
					combinedHistory[1].PossibleBranches = nextIDs
				}
			}
		}
		// Remove the first message (the extra one we read) to respect the original fetchLimit
		combinedHistory = combinedHistory[1:]
	} else if len(combinedHistory) > 0 && combinedHistory[0].ParentMessageID == nil {
		// Full history fetch or we didn't exceed fetchLimit, handle first message case
		firstMessages, err := GetSessionFirstMessages(db)
		if err != nil {
			log.Printf("getSessionHistoryInternal: Failed to get first messages for session %s: %v", db.SessionId(), err)
			// Non-fatal, continue without possible first message IDs
		} else if len(firstMessages) > 1 {
			var possibleFirstIds []PossibleNextMessage
			for _, msg := range firstMessages {
				// Parse created_at string to time.Time (handle both ISO 8601 and SQL format)
				var createdAt time.Time
				var err error

				// Try ISO 8601 format first (e.g., "2025-10-06T19:55:07Z")
				createdAt, err = time.Parse(time.RFC3339, msg.CreatedAt)
				if err != nil {
					// If ISO 8601 fails, try SQL format (e.g., "2025-10-06 19:55:07")
					createdAt, err = time.Parse("2006-01-02 15:04:05", msg.CreatedAt)
					if err != nil {
						log.Printf("Failed to parse created_at for message %d: %v", msg.ID, err)
						continue
					}
				}

				possibleFirstIds = append(possibleFirstIds, PossibleNextMessage{
					MessageID: strconv.Itoa(msg.ID),
					BranchID:  msg.BranchID,
					UserText:  msg.Text,
					Timestamp: createdAt.Unix(),
				})
			}

			// Filter out the current message from possibleFirstIds and set as possibleBranches
			var filteredPossibleBranches []PossibleNextMessage
			for _, possibleMsg := range possibleFirstIds {
				if possibleMsg.MessageID != combinedHistory[0].ID {
					filteredPossibleBranches = append(filteredPossibleBranches, possibleMsg)
				}
			}
			combinedHistory[0].PossibleBranches = filteredPossibleBranches
		}
	}

	// For history context (canAlterHistory=true), move compression summary to first position
	if canAlterHistory {
		for i, msg := range combinedHistory {
			if msg.Type == TypeCompression {
				if i != 0 {
					// Move compression message to first position
					compressionMsg := combinedHistory[i]
					// Remove from current position
					combinedHistory = append(combinedHistory[:i], combinedHistory[i+1:]...)
					// Insert at beginning
					combinedHistory = append([]FrontendMessage{compressionMsg}, combinedHistory...)
				}
				break // Only process first compression message
			}
		}
	}

	return combinedHistory, nil
}

func CreateFrontendMessage(
	m Message,
	attachmentsJSON sql.NullString,
	possibleBranches []PossibleNextMessage,
	ignoreBeforeLastCompression bool,
	includeState bool,
	clearblobsSeen bool,
) (FrontendMessage, *int, error) {
	return createFrontendMessage(m, attachmentsJSON, possibleBranches, ignoreBeforeLastCompression, includeState, clearblobsSeen)
}

// createFrontendMessage converts a Message DB struct into a FrontendMessage
// using either provided possibleBranches or parsing from JSON string
func createFrontendMessage(
	m Message,
	attachmentsJSON sql.NullString,
	possibleBranches []PossibleNextMessage,
	ignoreBeforeLastCompression bool,
	includeState bool,
	clearblobsSeen bool,
) (FrontendMessage, *int, error) {
	if attachmentsJSON.Valid {
		if err := json.Unmarshal([]byte(attachmentsJSON.String), &m.Attachments); err != nil {
			log.Printf("Failed to unmarshal attachments for message %d: %v", m.ID, err)
			// Continue even if unmarshaling fails, as the message itself is valid
		}
	}

	var parts []Part
	var tokens *int = nil

	if m.CumulTokenCount != nil {
		tokens = m.CumulTokenCount
	}

	var compressedUpToMessageID *int

	var fmParentMessageID *string = nil
	var fmChosenNextID *string = nil

	if m.ParentMessageID != nil {
		s := fmt.Sprintf("%d", *m.ParentMessageID)
		fmParentMessageID = &s
	}

	if m.ChosenNextID != nil {
		s := fmt.Sprintf("%d", *m.ChosenNextID)
		fmChosenNextID = &s
	}

	// Filter out self-references from possibleBranches
	var filteredPossibleBranches []PossibleNextMessage
	currentMessageID := fmt.Sprintf("%d", m.ID)
	for _, branch := range possibleBranches {
		if branch.MessageID != currentMessageID {
			filteredPossibleBranches = append(filteredPossibleBranches, branch)
		}
	}

	// Process attachments for clearblobs
	var processedAttachments []FileAttachment
	if clearblobsSeen {
		// Mark all attachments as omitted when clearblobs is active
		for _, att := range m.Attachments {
			omittedAtt := att
			omittedAtt.Omitted = true
			processedAttachments = append(processedAttachments, omittedAtt)
		}
	} else {
		processedAttachments = m.Attachments
	}

	// Parse aux JSON if present
	var auxMap map[string]any
	if m.Aux != "" {
		if err := json.Unmarshal([]byte(m.Aux), &auxMap); err != nil {
			log.Printf("Failed to unmarshal aux for message %d: %v", m.ID, err)
			// Continue even if unmarshaling fails
			auxMap = nil
		}
	}

	// Define fm here, before the switch statement
	fm := FrontendMessage{
		ID:               fmt.Sprintf("%d", m.ID),
		Parts:            parts,
		Type:             m.Type,
		Attachments:      processedAttachments,
		CumulTokenCount:  tokens,
		SessionID:        m.LocalSessionID,
		BranchID:         m.BranchID,
		ParentMessageID:  fmParentMessageID,
		ChosenNextID:     fmChosenNextID,
		PossibleBranches: filteredPossibleBranches,
		Model:            m.Model,
		Aux:              auxMap,
	}

	thoughtSignature := m.State
	if !includeState {
		thoughtSignature = ""
	}

	switch m.Type {
	case TypeFunctionCall:
		var fc FunctionCall
		if err := json.Unmarshal([]byte(m.Text), &fc); err != nil {
			log.Printf("Failed to unmarshal FunctionCall for message %d: %v", m.ID, err)
		} else {
			fm.Parts = []Part{{FunctionCall: &fc, ThoughtSignature: thoughtSignature}}
		}
	case TypeFunctionResponse:
		var fr FunctionResponse
		if err := json.Unmarshal([]byte(m.Text), &fr); err != nil {
			log.Printf("Failed to unmarshal FunctionResponse for message %d: %v", m.ID, err)
		} else {
			fm.Parts = []Part{{FunctionResponse: &fr, ThoughtSignature: thoughtSignature}}
		}
	case TypeCompression:
		// For compression messages, the text is in the format "ID\nSummary"
		before, textAfter, found := strings.Cut(m.Text, "\n")
		if found {
			// Parse ID for compressedUpToMessageID
			if found {
				parsedID, err := strconv.Atoi(before)
				if err != nil {
					log.Printf("Failed to parse CompressedUpToMessageId for message %d: %v", m.ID, err)
				} else {
					compressedUpToMessageID = &parsedID
				}
			}
			// If ignoreBeforeLastCompression is true, only show the summary part.
			// Otherwise, show the full text (ID\nSummary).
			if ignoreBeforeLastCompression {
				fm.Parts = []Part{{Text: textAfter, ThoughtSignature: thoughtSignature}}
			} else {
				fm.Parts = []Part{{Text: m.Text, ThoughtSignature: thoughtSignature}}
			}
		} else {
			log.Printf("Warning: Malformed compression message text for message %d: %s", m.ID, m.Text)
			fm.Parts = []Part{{Text: m.Text, ThoughtSignature: thoughtSignature}} // Fallback to raw text
		}
	case TypeCommand:
		// For command messages, keep the original command text
		fm.Parts = []Part{{Text: m.Text, ThoughtSignature: thoughtSignature}}
	default:
		if m.Text != "" {
			// Recover length from `<length>,<ThoughtSignature>` if possible.
			if first, rest, found := strings.Cut(thoughtSignature, ","); found {
				length, err := strconv.Atoi(first)
				if err != nil {
					log.Panicf("Failed to parse length from ThoughtSignature %q: %v", thoughtSignature, err)
				}
				parts = append(parts, Part{Text: m.Text[:length], ThoughtSignature: rest})
				if len(m.Text) > length {
					parts = append(parts, Part{Text: m.Text[length:]})
				}
			} else {
				parts = append(parts, Part{Text: m.Text, ThoughtSignature: thoughtSignature})
			}
		}
		fm.Parts = parts // Assign the accumulated parts to fm.Parts
	}
	return fm, compressedUpToMessageID, nil
}

// parsePossibleNextIDs parses the JSON string containing possible next messages
// and returns a slice of PossibleNextMessage
func parsePossibleNextIDs(possibleNextIDsAndBranchesStr string) []PossibleNextMessage {
	var possibleBranches []PossibleNextMessage

	// Parse JSON array of possible next messages with user text and timestamp
	if possibleNextIDsAndBranchesStr != "" && possibleNextIDsAndBranchesStr != "[]" {
		var possibleNextMessages []struct {
			ID        int    `json:"id"`
			BranchID  string `json:"branchId"`
			Text      string `json:"text"`
			CreatedAt string `json:"createdAt"`
		}

		if err := json.Unmarshal([]byte(possibleNextIDsAndBranchesStr), &possibleNextMessages); err != nil {
			log.Printf("Warning: Failed to parse possibleNextMessages JSON: %v", err)
			return nil
		}

		for _, msg := range possibleNextMessages {
			if msg.ID == 0 {
				continue // Skip empty entries from LEFT JOIN
			}

			// Parse created_at string to time.Time (handle both ISO 8601 and SQL format)
			var timestamp int64
			parsedTime, err := time.Parse(time.RFC3339, msg.CreatedAt)
			if err != nil {
				parsedTime, err = time.Parse("2006-01-02 15:04:05", msg.CreatedAt)
				if err != nil {
					log.Printf("Warning: Failed to parse created_at for message %d: %v", msg.ID, err)
					// Still include the entry without timestamp
					timestamp = 0
				} else {
					timestamp = parsedTime.Unix()
				}
			} else {
				timestamp = parsedTime.Unix()
			}

			possibleBranches = append(possibleBranches, PossibleNextMessage{
				MessageID: strconv.Itoa(msg.ID),
				BranchID:  msg.BranchID,
				UserText:  msg.Text,
				Timestamp: timestamp,
			})
		}
	}

	return possibleBranches
}
