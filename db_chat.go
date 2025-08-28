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
}

// FrontendMessage struct to match the frontend's ChatMessage interface
type FrontendMessage struct {
	ID              string                `json:"id"`
	Parts           []Part                `json:"parts"`
	Type            MessageType           `json:"type"`
	Attachments     []FileAttachment      `json:"attachments,omitempty"`
	CumulTokenCount *int                  `json:"cumul_token_count,omitempty"`
	SessionID       string                `json:"sessionId,omitempty"`
	BranchID        string                `json:"branchId,omitempty"`
	ParentMessageID *string               `json:"parentMessageId,omitempty"`
	ChosenNextID    *string               `json:"chosenNextId,omitempty"`
	PossibleNextIDs []PossibleNextMessage `json:"possibleNextIds,omitempty"`
	Model           string                `json:"model,omitempty"`
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

	return int(lastInsertID), nil
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
func GetMessagePossibleNextIDs(db *sql.DB, messageID int) ([]PossibleNextMessage, error) {
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

// createFrontendMessage converts a Message DB struct and related data into a FrontendMessage.
func createFrontendMessage(
	m Message,
	attachmentsJSON sql.NullString,
	possibleNextIDsAndBranchesStr string,
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
	var fmPossibleNextIDs []PossibleNextMessage = nil

	if m.ParentMessageID != nil {
		s := fmt.Sprintf("%d", *m.ParentMessageID)
		fmParentMessageID = &s
	}

	if m.ChosenNextID != nil {
		s := fmt.Sprintf("%d", *m.ChosenNextID)
		fmChosenNextID = &s
	}

	if possibleNextIDsAndBranchesStr != "" {
		possibleNextIDsAndBranches := strings.Split(possibleNextIDsAndBranchesStr, ",")
		for i := 0; i < len(possibleNextIDsAndBranches); i += 2 {
			if i+1 < len(possibleNextIDsAndBranches) { // Ensure there's a branch ID for the message ID
				fmPossibleNextIDs = append(fmPossibleNextIDs, PossibleNextMessage{
					MessageID: possibleNextIDsAndBranches[i],
					BranchID:  possibleNextIDsAndBranches[i+1],
				})
			} else {
				log.Printf(
					"Warning: Malformed possibleNextIDsAndBranchesStr for message %d: %s",
					m.ID, possibleNextIDsAndBranchesStr)
			}
		}
	}

	// Define fm here, before the switch statement
	fm := FrontendMessage{
		ID:              fmt.Sprintf("%d", m.ID),
		Parts:           parts,
		Type:            m.Type,
		Attachments:     m.Attachments,
		CumulTokenCount: tokens,
		SessionID:       m.SessionID,
		BranchID:        m.BranchID,
		ParentMessageID: fmParentMessageID,
		ChosenNextID:    fmChosenNextID,
		PossibleNextIDs: fmPossibleNextIDs,
		Model:           m.Model,
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
		textBefore, textAfter, found := strings.Cut(m.Text, "\n")
		if found {
			parsedID, err := strconv.Atoi(textBefore)
			if err != nil {
				log.Printf("Failed to parse CompressedUpToMessageId for message %d: %v", m.ID, err)
			} else {
				compressedUpToMessageID = &parsedID
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

	isFullHistoryFetch := (fetchLimit <= 0)

	var history [][]FrontendMessage
	var currentMessageCount int
	for keepGoing && (isFullHistoryFetch || currentMessageCount < fetchLimit) { // Modified condition
		err := func() error {
			rows, err := db.Query(`
				SELECT
					m.id, m.session_id, m.branch_id, m.parent_message_id, m.chosen_next_id,
					m.text, m.type, m.attachments, m.cumul_token_count, m.created_at, m.model,
					m.state, coalesce(group_concat(mm.id || ',' || mm.branch_id), '')
				FROM messages AS m LEFT OUTER JOIN messages AS mm ON m.id = mm.parent_message_id
				GROUP BY m.id
				HAVING m.branch_id = ? AND m.id <= ?
				ORDER BY m.id ASC
			`, branchID, messageIdLimit)
			if err != nil {
				return fmt.Errorf("failed to query branch messages: %w", err)
			}
			defer rows.Close()

			var messages []FrontendMessage
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

				// discardThought implies includeState because state summarizes prior thoughts.
				fm, _, err := createFrontendMessage(m, attachmentsJSON, possibleNextIDsAndBranchesStr, ignoreBeforeLastCompression, discardThoughts)
				if err != nil {
					return fmt.Errorf("failed to create frontend message: %w", err)
				}

				if len(messages) == 0 && fm.ParentMessageID != nil {
					parentBranchMessageID = *m.ParentMessageID
				}
				messages = append(messages, fm)
			}

			if len(messages) == 0 {
				keepGoing = false
				return nil
			}

			history = append(history, messages)
			currentMessageCount += len(messages) // Update counter
			if parentBranchMessageID < 0 {
				keepGoing = false
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
	return getSessionHistoryInternal(db, sessionID, primaryBranchID, false, false, beforeMessageID, fetchLimit)
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
func UpdateMessageContent(db *sql.DB, messageID int, content string) error {
	stmt, err := db.Prepare("UPDATE messages SET text = ? WHERE id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare update message content statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(content, messageID)
	if err != nil {
		return fmt.Errorf("failed to execute update message content statement: %w", err)
	}
	return nil
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
func GetLastMessageInBranch(db *sql.DB, sessionID string, branchID string) (lastMessageID int, lastMessageModel string, lastMessageGeneration int, err error) {
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
