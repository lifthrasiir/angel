package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

// Branch struct to hold branch data
type Branch struct {
	ID                  string  `json:"id"`
	SessionID           string  `json:"session_id"`
	ParentBranchID      *string `json:"parent_branch_id"`       // Pointer for nullable
	BranchFromMessageID *int    `json:"branch_from_message_id"` // Pointer for nullable
	CreatedAt           string  `json:"created_at"`
	PendingConfirmation *string `json:"pending_confirmation"`
}

// FileAttachment struct to hold file attachment data
type FileAttachment struct {
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Hash     string `json:"hash"`           // SHA-512/256 hash of the data
	Data     []byte `json:"data,omitempty"` // Raw binary data, used temporarily for upload/download
}

// Message struct to hold message data for database interaction
type Message struct {
	ID                      int              `json:"id"`
	SessionID               string           `json:"session_id"`
	BranchID                string           `json:"branch_id"`
	ParentMessageID         *int             `json:"parent_message_id"`
	ChosenNextID            *int             `json:"chosen_next_id"`
	Text                    string           `json:"text"`
	Type                    MessageType      `json:"type"`
	Attachments             []FileAttachment `json:"attachments,omitempty"`
	CumulTokenCount         *int             `json:"cumul_token_count,omitempty"`
	CreatedAt               string           `json:"created_at"`
	Model                   string           `json:"model,omitempty"`
	CompressedUpToMessageID *int             `json:"compressed_up_to_message_id,omitempty"`
	Generation              int              `json:"generation"`
	State                   string           `json:"state,omitempty"`
	Aux                     string           `json:"aux,omitempty"`
}

// DbOrTx interface defines the common methods used from *sql.DB and *sql.Tx.
type DbOrTx interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// PossibleNextMessage struct to hold possible next message data for the frontend
type PossibleNextMessage struct {
	MessageID string `json:"messageId"`
	BranchID  string `json:"branchId"`
	UserText  string `json:"userText,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// FrontendMessage struct to match the frontend's ChatMessage interface
type FrontendMessage struct {
	ID               string                `json:"id"`
	Parts            []Part                `json:"parts"`
	Type             MessageType           `json:"type"`
	Attachments      []FileAttachment      `json:"attachments,omitempty"`
	CumulTokenCount  *int                  `json:"cumul_token_count,omitempty"`
	SessionID        string                `json:"sessionId,omitempty"`
	BranchID         string                `json:"branchId,omitempty"`
	ParentMessageID  *string               `json:"parentMessageId,omitempty"`
	ChosenNextID     *string               `json:"chosenNextId,omitempty"`
	PossibleBranches []PossibleNextMessage `json:"possibleBranches,omitempty"`
	Model            string                `json:"model,omitempty"`
}

// CreateBranch creates a new branch in the database.
func CreateBranch(db *sql.DB, branchID string, sessionID string, parentBranchID *string, branchFromMessageID *int) (string, error) {
	_, err := db.Exec("INSERT INTO branches (id, session_id, parent_branch_id, branch_from_message_id) VALUES (?, ?, ?, ?)", branchID, sessionID, parentBranchID, branchFromMessageID)
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}
	return branchID, nil
}

// AddMessageToSession now accepts a message type, attachments, and numTokens, and branch_id, parent_message_id, chosen_next_id
func AddMessageToSession(ctx context.Context, db DbOrTx, msg Message) (int, error) {
	// Process attachments: save blob data and store only hashes
	for i := range msg.Attachments {
		if msg.Attachments[i].Data != nil {
			hash, err := SaveBlob(ctx, db, msg.Attachments[i].Data)
			if err != nil {
				return 0, fmt.Errorf("failed to save attachment blob: %w", err)
			}
			msg.Attachments[i].Hash = hash
			msg.Attachments[i].Data = nil // Clear the data after saving
		}
	}

	attachmentsJSON, err := json.Marshal(msg.Attachments)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal attachments: %w", err)
	}

	result, err := db.Exec(`
		INSERT INTO messages (
			session_id, branch_id, parent_message_id, chosen_next_id, text,
			type, attachments, cumul_token_count, model, generation, state, aux)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.SessionID, msg.BranchID, msg.ParentMessageID, msg.ChosenNextID, msg.Text,
		msg.Type, string(attachmentsJSON), msg.CumulTokenCount, msg.Model, msg.Generation, msg.State, msg.Aux)
	if err != nil {
		log.Printf("AddMessageToSession: Failed to add message to session: %v", err)
		return 0, fmt.Errorf("failed to add message to session: %w", err)
	}

	lastInsertID, err := result.LastInsertId()
	if err != nil {
		log.Printf("AddMessageToSession: Failed to get last insert ID: %v", err)
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	messageID := int(lastInsertID)

	// Add message to FTS tables only for user and model messages
	// FTS tables are contentless and will automatically read from messages_searchable view
	// which only includes messages with type IN ('user', 'model')
	if msg.Type == TypeUserText || msg.Type == TypeModelText {
		_, err = db.Exec("INSERT INTO message_stems(rowid) VALUES (?)", messageID)
		if err != nil {
			log.Printf("AddMessageToSession: Failed to insert into message_stems: %v", err)
			// Don't fail the operation, but log the error
		}

		_, err = db.Exec("INSERT INTO message_trigrams(rowid) VALUES (?)", messageID)
		if err != nil {
			log.Printf("AddMessageToSession: Failed to insert into message_trigrams: %v", err)
			// Don't fail the operation, but log the error
		}
	}

	return messageID, nil
}

// UpdateMessageChosenNextID updates the chosen_next_id for a specific message.
func UpdateMessageChosenNextID(db DbOrTx, messageID int, chosenNextID *int) error {
	_, err := db.Exec("UPDATE messages SET chosen_next_id = ? WHERE id = ?", chosenNextID, messageID)
	if err != nil {
		return fmt.Errorf("failed to update message chosen_next_id: %w", err)
	}
	return nil
}

// UpdateSessionPrimaryBranchID updates the primary_branch_id for a session.
func UpdateSessionPrimaryBranchID(db *sql.DB, sessionID string, branchID string) error {
	_, err := db.Exec("UPDATE sessions SET primary_branch_id = ? WHERE id = ?", branchID, sessionID)
	if err != nil {
		log.Printf("UpdateSessionPrimaryBranchID: Failed to update session primary_branch_id: %v", err)
		return fmt.Errorf("failed to update session primary_branch_id: %w", err)
	}
	return nil
}

// GetMessagePossibleNextIDs retrieves all possible next message IDs and their branch IDs for a given message ID.
func GetMessagePossibleNextIDs(db DbOrTx, messageID int) ([]PossibleNextMessage, error) {
	rows, err := db.Query("SELECT id, branch_id FROM messages WHERE parent_message_id = ?", messageID)
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

func GetBranch(db *sql.DB, branchID string) (Branch, error) {
	var b Branch
	row := db.QueryRow("SELECT id, session_id, parent_branch_id, branch_from_message_id, created_at, pending_confirmation FROM branches WHERE id = ?", branchID)
	err := row.Scan(&b.ID, &b.SessionID, &b.ParentBranchID, &b.BranchFromMessageID, &b.CreatedAt, &b.PendingConfirmation)
	if err != nil {
		return b, fmt.Errorf("failed to get branch: %w", err)
	}
	return b, nil
}

// UpdateBranchPendingConfirmation updates the pending_confirmation for a branch.
func UpdateBranchPendingConfirmation(db *sql.DB, branchID string, confirmationData string) error {
	_, err := db.Exec("UPDATE branches SET pending_confirmation = ? WHERE id = ?", confirmationData, branchID)
	if err != nil {
		return fmt.Errorf("failed to update branch pending_confirmation: %w", err)
	}
	return nil
}

// createFrontendMessage converts a Message DB struct into a FrontendMessage
// using either provided possibleBranches or parsing from JSON string
func createFrontendMessage(
	m Message,
	attachmentsJSON sql.NullString,
	possibleBranches []PossibleNextMessage,
	ignoreBeforeLastCompression bool,
	includeState bool,
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

	// Define fm here, before the switch statement
	fm := FrontendMessage{
		ID:               fmt.Sprintf("%d", m.ID),
		Parts:            parts,
		Type:             m.Type,
		Attachments:      m.Attachments,
		CumulTokenCount:  tokens,
		SessionID:        m.SessionID,
		BranchID:         m.BranchID,
		ParentMessageID:  fmParentMessageID,
		ChosenNextID:     fmChosenNextID,
		PossibleBranches: filteredPossibleBranches,
		Model:            m.Model,
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
		_, textAfter, found := strings.Cut(m.Text, "\n")
		if found {
			// Parse ID for compressedUpToMessageID
			before, _, found := strings.Cut(m.Text, "\n")
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

// getSessionHistoryInternal retrieves the chat history for a given session and its primary branch,
// recursively fetching messages from parent branches. It allows for discarding thoughts
// and ignoring messages before the last compression message.
//
// If fetchLimit is > 0, cursor-based pagination using beforeMessageID and fetchLimit is done.
// If beforeMessageID is 0, it fetches the latest messages.
func getSessionHistoryInternal(
	db DbOrTx,
	sessionID string,
	primaryBranchID string,
	discardThoughts bool,
	ignoreBeforeLastCompression bool,
	beforeMessageID int, // Cursor: fetch messages with ID less than this
	fetchLimit int, // Number of messages to fetch
) ([]FrontendMessage, error) {
	// Determine if this is a paginated call that might have over-fetched
	isPaginatedCall := (beforeMessageID != 0 || fetchLimit > 0)
	branchID := primaryBranchID
	keepGoing := true

	var lastCompressionMessageID int = -1
	var lastCompressedUpToMessageID *int

	if ignoreBeforeLastCompression {
		// Find the ID of the last compression message in the current branch
		var compressionText string
		row := db.QueryRow(
			"SELECT id, text FROM messages WHERE session_id = ? AND branch_id = ? AND type = ? ORDER BY id DESC LIMIT 1",
			sessionID, primaryBranchID, TypeCompression)
		err := row.Scan(&lastCompressionMessageID, &compressionText)
		if err == nil && lastCompressionMessageID != -1 {
			before, _, found := strings.Cut(compressionText, "\n")
			if found {
				parsedID, parseErr := strconv.Atoi(before)
				if parseErr == nil {
					lastCompressedUpToMessageID = &parsedID
				} else {
					log.Printf("Warning: Failed to parse CompressedUpToMessageId from compression message text %q: %v", before, parseErr)
				}
			} else {
				log.Printf("Warning: Malformed compression message text: %q", compressionText)
			}
		}
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to find last compression message: %w", err)
		}
		// If a compression message is found, parse its text to get the compressedUpToMessageID
		if lastCompressionMessageID != -1 {
			var textContent string
			err := db.QueryRow("SELECT text FROM messages WHERE id = ?", lastCompressionMessageID).Scan(&textContent)
			if err != nil {
				log.Printf("Warning: Failed to retrieve text for last compression message %d: %v", lastCompressionMessageID, err)
				lastCompressedUpToMessageID = nil
			} else {
				parts := strings.SplitN(textContent, "\n", 2)
				if len(parts) == 2 {
					parsedID, err := strconv.Atoi(parts[0])
					if err == nil {
						lastCompressedUpToMessageID = &parsedID
					} else {
						log.Printf("Warning: Failed to parse CompressedUpToMessageId from last compression message %d: %v",
							lastCompressionMessageID, err)
						lastCompressedUpToMessageID = nil // Treat as if no valid ID was found
					}
				} else {
					log.Printf("Warning: Malformed text in last compression message %d: %s", lastCompressionMessageID, textContent)
					lastCompressedUpToMessageID = nil // Treat as if no valid ID was found
				}
			}
		}
	}

	// Determine the initial message ID limit for the query
	messageIdLimit := math.MaxInt
	if beforeMessageID != 0 {
		messageIdLimit = beforeMessageID - 1
	}

	// For first message editing support, find the starting point using chosen_first_id
	var startingMessageID int
	var chosenFirstID sql.NullInt64
	err := db.QueryRow("SELECT chosen_first_id FROM sessions WHERE id = ?", sessionID).Scan(&chosenFirstID)
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
					m.state, coalesce(
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
				FROM messages AS m LEFT OUTER JOIN messages AS mm ON m.id = mm.parent_message_id
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
				var possibleNextIDsAndBranchesStr string
				if err := rows.Scan(
					&m.ID, &m.SessionID, &m.BranchID, &m.ParentMessageID, &m.ChosenNextID,
					&m.Text, &m.Type, &attachmentsJSON, &m.CumulTokenCount, &m.CreatedAt, &m.Model,
					&m.State, &possibleNextIDsAndBranchesStr,
				); err != nil {
					return fmt.Errorf("failed to scan message: %w", err)
				}

				// If ignoring before last compression, and current message is older than or equal to the compressed ID
				if ignoreBeforeLastCompression &&
					lastCompressedUpToMessageID != nil &&
					m.ID <= *lastCompressedUpToMessageID &&
					m.ID != lastCompressionMessageID {
					continue // Skip this message, unless it's the compression message itself
				}

				if discardThoughts && m.Type == TypeThought {
					continue // Skip thought messages
				}

				// Create frontend message with possibleBranches from previous iteration
				fm, _, err := createFrontendMessage(m, attachmentsJSON, possibleBranches, ignoreBeforeLastCompression, discardThoughts)
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
				err := db.QueryRow("SELECT branch_id FROM messages WHERE id = ?", parentBranchMessageID).Scan(&branchID)
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
		firstMessages, err := GetSessionFirstMessages(db, sessionID)
		if err != nil {
			log.Printf("getSessionHistoryInternal: Failed to get first messages for session %s: %v", sessionID, err)
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

	return combinedHistory, nil
}

// GetSessionHistory retrieves the chat history for a given session and its primary branch.
// It includes all messages, including thoughts.
func GetSessionHistory(db DbOrTx, sessionID string, primaryBranchID string) ([]FrontendMessage, error) {
	return getSessionHistoryInternal(db, sessionID, primaryBranchID, false, false, 0, 0)
}

// GetSessionHistoryContext retrieves the chat history for a given session and its primary branch,
// discarding thoughts and ignoring messages before the last compression message.
func GetSessionHistoryContext(db DbOrTx, sessionID string, primaryBranchID string) ([]FrontendMessage, error) {
	return getSessionHistoryInternal(db, sessionID, primaryBranchID, true, true, 0, 0)
}

// GetSessionHistoryPaginated retrieves a paginated chat history for a given session and branch.
// It fetches messages with IDs less than beforeMessageID, up to fetchLimit.
func GetSessionHistoryPaginated(db DbOrTx, sessionID string, primaryBranchID string, beforeMessageID int, fetchLimit int) ([]FrontendMessage, error) {
	// For paginated calls, we need to fetch one more message to get proper possibleBranches for the first message
	if fetchLimit > 0 {
		return getSessionHistoryInternal(db, sessionID, primaryBranchID, false, false, beforeMessageID, fetchLimit+1)
	}
	return getSessionHistoryInternal(db, sessionID, primaryBranchID, false, false, beforeMessageID, fetchLimit)
}

// GetSessionHistoryPaginatedWithAutoBranch retrieves paginated chat history with automatic branch detection.
// If beforeMessageID is specified, it automatically uses the branch containing that message.
// Otherwise, it falls back to the session's primary branch.
func GetSessionHistoryPaginatedWithAutoBranch(db *sql.DB, sessionID string, beforeMessageID int, fetchLimit int) ([]FrontendMessage, string, error) {
	var targetBranchID string

	// Get the session's primary branch ID as default
	err := db.QueryRow("SELECT primary_branch_id FROM sessions WHERE id = ?", sessionID).Scan(&targetBranchID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get primary branch ID for session %s: %w", sessionID, err)
	}

	// If beforeMessageID is specified, find which branch contains this message
	if beforeMessageID > 0 {
		var messageBranchID string
		var parentMessageID sql.NullInt64
		err := db.QueryRow("SELECT branch_id, parent_message_id FROM messages WHERE id = ? AND session_id = ?", beforeMessageID, sessionID).Scan(&messageBranchID, &parentMessageID)
		if err == nil && messageBranchID != "" {
			// Default to the message's branch
			targetBranchID = messageBranchID

			// If the message has a parent in a different branch, use the parent's branch instead
			if parentMessageID.Valid {
				var parentBranchID string
				err := db.QueryRow("SELECT branch_id FROM messages WHERE id = ?", parentMessageID.Int64).Scan(&parentBranchID)
				if err == nil && parentBranchID != messageBranchID {
					targetBranchID = parentBranchID
				}
			}
		} else if err != sql.ErrNoRows {
			// Error occurred (not just "no rows")
			return nil, "", fmt.Errorf("failed to find branch for message %d: %w", beforeMessageID, err)
		}
	}

	// Get the paginated history using the determined branch
	history, err := GetSessionHistoryPaginated(db, sessionID, targetBranchID, beforeMessageID, fetchLimit)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get session history: %w", err)
	}

	return history, targetBranchID, nil
}

// UpdateMessageTokens updates the cumul_token_count for a specific message.
func UpdateMessageTokens(db DbOrTx, messageID int, cumulTokenCount int) error {
	_, err := db.Exec("UPDATE messages SET cumul_token_count = ? WHERE id = ?", cumulTokenCount, messageID)
	if err != nil {
		return fmt.Errorf("failed to update message tokens: %w", err)
	}
	return nil
}

// UpdateMessageContent updates the content of a message in the database.
func UpdateMessageContent(db *sql.DB, messageID int, content string, syncFTS bool) error {
	// Start transaction for atomic update
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update message content
	stmt, err := tx.Prepare("UPDATE messages SET text = ? WHERE id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare update message content statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(content, messageID)
	if err != nil {
		return fmt.Errorf("failed to execute update message content statement: %w", err)
	}

	// Sync FTS if requested (for final message updates)
	if syncFTS {
		// FTS tables are contentless and will automatically read from messages_searchable view
		// Use INSERT OR REPLACE to force re-indexing with new content
		_, err = tx.Exec("INSERT OR REPLACE INTO message_stems(rowid) VALUES (?)", messageID)
		if err != nil {
			return fmt.Errorf("failed to update message_stems: %w", err)
		}

		_, err = tx.Exec("INSERT OR REPLACE INTO message_trigrams(rowid) VALUES (?)", messageID)
		if err != nil {
			return fmt.Errorf("failed to update message_trigrams: %w", err)
		}
	}

	return tx.Commit()
}

// GetMessageBranchID retrieves the branch_id for a given message ID.
func GetMessageBranchID(db *sql.DB, messageID int) (string, error) {
	var branchID string
	err := db.QueryRow("SELECT branch_id FROM messages WHERE id = ?", messageID).Scan(&branchID)
	if err != nil {
		return "", fmt.Errorf("failed to get branch_id for message %d: %w", messageID, err)
	}
	return branchID, nil
}

// GetLastMessageInBranch retrieves the ID and model of the last message in a given session and branch.
func GetLastMessageInBranch(db DbOrTx, sessionID string, branchID string) (lastMessageID int, lastMessageModel string, lastMessageGeneration int, err error) {
	row := db.QueryRow("SELECT id, model, generation FROM messages WHERE session_id = ? AND branch_id = ? AND chosen_next_id IS NULL ORDER BY created_at DESC LIMIT 1", sessionID, branchID)
	err = row.Scan(&lastMessageID, &lastMessageModel, &lastMessageGeneration)
	if err != nil {
		err = fmt.Errorf("failed to get last message in branch: %w", err)
		return
	}
	return
}

// GetMessageDetails retrieves the type, parent_message_id, and branch_id for a given message ID.
func GetMessageDetails(db *sql.DB, messageID int) (MessageType, sql.NullInt64, string, error) {
	var msgType, branchID string
	var parentMessageID sql.NullInt64
	row := db.QueryRow("SELECT type, parent_message_id, branch_id FROM messages WHERE id = ?", messageID)
	err := row.Scan(&msgType, &parentMessageID, &branchID)
	if err != nil {
		return MessageType(""), sql.NullInt64{}, "", fmt.Errorf("failed to get message details: %w", err)
	}
	return MessageType(msgType), parentMessageID, branchID, nil
}

// GetOriginalNextMessageID retrieves the ID of the message that originally followed a given message in its branch.
func GetOriginalNextMessageID(db *sql.DB, parentMessageID int, branchID string) (sql.NullInt64, error) {
	var originalNextMessageID sql.NullInt64
	err := db.QueryRow(`
		SELECT id FROM messages
		WHERE parent_message_id = ? AND branch_id = ?
		ORDER BY created_at ASC LIMIT 1
	`, parentMessageID, branchID).Scan(&originalNextMessageID)
	if err != nil && err != sql.ErrNoRows {
		return sql.NullInt64{}, fmt.Errorf("failed to find original next message: %w", err)
	}
	return originalNextMessageID, nil
}

// GetFirstMessageOfBranch retrieves the ID of the first message in a given branch that has a specific parent message.
func GetFirstMessageOfBranch(db *sql.DB, parentMessageID int, branchID string) (int, error) {
	var firstMessageID int
	err := db.QueryRow(`
		SELECT id FROM messages
		WHERE parent_message_id = ? AND branch_id = ?
		ORDER BY created_at ASC LIMIT 1
	`, parentMessageID, branchID).Scan(&firstMessageID)
	if err != nil {
		return 0, fmt.Errorf("failed to find first message of branch: %w", err)
	}
	return firstMessageID, nil
}

// GetMessageByID retrieves a single message by its ID.
func GetMessageByID(db *sql.DB, messageID int) (*Message, error) {
	var m Message
	var attachmentsJSON sql.NullString // Use sql.NullString to handle NULL attachments

	err := db.QueryRow(`
		SELECT
			id, session_id, branch_id, parent_message_id, chosen_next_id,
			text, type, attachments, cumul_token_count, created_at, model, generation
		FROM messages
		WHERE id = ?
	`, messageID).Scan(
		&m.ID, &m.SessionID, &m.BranchID, &m.ParentMessageID, &m.ChosenNextID,
		&m.Text, &m.Type, &attachmentsJSON, &m.CumulTokenCount, &m.CreatedAt, &m.Model, &m.Generation,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("message not found")
		}
		return nil, fmt.Errorf("failed to get message by ID: %w", err)
	}

	// Unmarshal attachments JSON if it's not NULL
	if attachmentsJSON.Valid {
		if err := json.Unmarshal([]byte(attachmentsJSON.String), &m.Attachments); err != nil {
			log.Printf("Failed to unmarshal attachments for message %d: %v", m.ID, err)
			// Continue even if unmarshaling fails, as the message itself is valid
		}
	}

	return &m, nil
}

// UpdateSessionChosenFirstID updates the chosen_first_id for a specific session.
func UpdateSessionChosenFirstID(db *sql.DB, sessionID string, chosenFirstID *int) error {
	_, err := db.Exec("UPDATE sessions SET chosen_first_id = ? WHERE id = ?", chosenFirstID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session chosen_first_id: %w", err)
	}
	return nil
}

// GetSessionChosenFirstID retrieves the chosen_first_id for a specific session.
func GetSessionChosenFirstID(db *sql.DB, sessionID string) (*int, error) {
	var chosenFirstID *int
	err := db.QueryRow("SELECT chosen_first_id FROM sessions WHERE id = ?", sessionID).Scan(&chosenFirstID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chosen_first_id for session %s: %w", sessionID, err)
	}
	return chosenFirstID, nil
}

// GetSessionFirstMessage retrieves the first message for a session using chosen_first_id.
func GetSessionFirstMessage(db *sql.DB, sessionID string) (*Message, error) {
	chosenFirstID, err := GetSessionChosenFirstID(db, sessionID)
	if err != nil {
		return nil, err
	}
	if chosenFirstID == nil {
		return nil, fmt.Errorf("no first message set for session %s", sessionID)
	}
	return GetMessageByID(db, *chosenFirstID)
}

// GetSessionFirstMessages retrieves all first messages (parent_message_id IS NULL) for a session.
func GetSessionFirstMessages(db DbOrTx, sessionID string) ([]Message, error) {
	query := `
		SELECT id, session_id, branch_id, parent_message_id, chosen_next_id,
		       text, type, attachments, cumul_token_count, created_at,
		       model, generation, state, aux
		FROM messages
		WHERE session_id = ? AND parent_message_id IS NULL
		ORDER BY created_at ASC
	`

	rows, err := db.Query(query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query first messages for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var attachments sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.SessionID, &msg.BranchID, &msg.ParentMessageID, &msg.ChosenNextID,
			&msg.Text, &msg.Type, &attachments, &msg.CumulTokenCount, &msg.CreatedAt,
			&msg.Model, &msg.Generation, &msg.State, &msg.Aux,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan first message: %w", err)
		}

		if attachments.Valid {
			if err := json.Unmarshal([]byte(attachments.String), &msg.Attachments); err != nil {
				log.Printf("Failed to unmarshal attachments for message %d: %v", msg.ID, err)
			}
		}

		messages = append(messages, msg)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating first messages: %w", err)
	}

	return messages, nil
}
