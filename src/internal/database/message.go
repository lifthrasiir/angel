package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	. "github.com/lifthrasiir/angel/gemini"
	. "github.com/lifthrasiir/angel/internal/types"
)

// DbOrTx interface defines the common methods used from *sql.DB and *sql.Tx.
type DbOrTx interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// CreateBranch creates a new branch in the
func CreateBranch(db *sql.DB, branchID string, sessionID string, parentBranchID *string, branchFromMessageID *int) (string, error) {
	_, err := db.Exec("INSERT INTO branches (id, session_id, parent_branch_id, branch_from_message_id) VALUES (?, ?, ?, ?)", branchID, sessionID, parentBranchID, branchFromMessageID)
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}
	return branchID, nil
}

// AddMessageToSession adds a message to a session in the
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

// GetBranch retrieves a branch by its ID.
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

// GetSessionHistory retrieves the chat history for a given session and its primary branch.
// It includes all messages, including thoughts.
func GetSessionHistory(db DbOrTx, sessionID string, primaryBranchID string) ([]FrontendMessage, error) {
	return getSessionHistoryInternal(db, sessionID, primaryBranchID, false, false, 0, 0)
}

// GetSessionHistoryContext retrieves the chat history for a given session and its primary branch,
// discarding thoughts and ignoring messages before the last compression or clear command.
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

// UpdateMessageContent updates the content of a message in the
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
			text, type, attachments, cumul_token_count, created_at, model, generation, state, aux, indexed
		FROM messages
		WHERE id = ?
	`, messageID).Scan(
		&m.ID, &m.SessionID, &m.BranchID, &m.ParentMessageID, &m.ChosenNextID,
		&m.Text, &m.Type, &attachmentsJSON, &m.CumulTokenCount, &m.CreatedAt, &m.Model, &m.Generation,
		&m.State, &m.Aux, &m.Indexed,
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
func UpdateSessionChosenFirstID(db DbOrTx, sessionID string, chosenFirstID *int) error {
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

// DeleteMessage deletes a message from the database and updates the parent's chosen_next_id.
// Simplified version to avoid deadlocks in tests.
func DeleteMessage(db *sql.DB, messageID int) error {
	// Get the message details first
	var msg Message
	var attachments sql.NullString
	err := db.QueryRow(`
		SELECT id, session_id, branch_id, parent_message_id, chosen_next_id,
		       text, type, attachments, cumul_token_count, created_at,
		       model, generation, state, aux
		FROM messages
		WHERE id = ?
	`, messageID).Scan(
		&msg.ID, &msg.SessionID, &msg.BranchID, &msg.ParentMessageID, &msg.ChosenNextID,
		&msg.Text, &msg.Type, &attachments, &msg.CumulTokenCount, &msg.CreatedAt,
		&msg.Model, &msg.Generation, &msg.State, &msg.Aux,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("message %d not found", messageID)
		}
		return fmt.Errorf("failed to get message %d: %w", messageID, err)
	}

	// Parse attachments if they exist
	if attachments.Valid {
		if err := json.Unmarshal([]byte(attachments.String), &msg.Attachments); err != nil {
			log.Printf("Failed to unmarshal attachments for message %d: %v", msg.ID, err)
		}
	}

	// Use a simple transaction for deletion
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update parent's chosen_next_id if parent exists
	if msg.ParentMessageID != nil {
		var nextID *int = msg.ChosenNextID
		_, err = tx.Exec("UPDATE messages SET chosen_next_id = ? WHERE id = ?", nextID, *msg.ParentMessageID)
		if err != nil {
			return fmt.Errorf("failed to update parent message: %w", err)
		}
	}

	// Delete the message
	_, err = tx.Exec("DELETE FROM messages WHERE id = ?", messageID)
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Successfully deleted message %d from session %s, branch %s", messageID, msg.SessionID, msg.BranchID)
	return nil
}

// GetSessionPrimaryBranchID retrieves the primary branch ID for a given session.
func GetSessionPrimaryBranchID(db *sql.DB, sessionID string) (string, error) {
	var primaryBranchID string
	err := db.QueryRow("SELECT primary_branch_id FROM sessions WHERE id = ?", sessionID).Scan(&primaryBranchID)
	if err != nil {
		return "", fmt.Errorf("failed to get primary branch ID for session %s: %w", sessionID, err)
	}
	return primaryBranchID, nil
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

// MessageChain represents a sequence of messages in a conversation branch.
type MessageChain struct {
	SessionID             string
	BranchID              string
	Messages              []Message
	LastMessageID         int
	LastMessageGeneration int
	LastMessageModel      string
}

// NewMessageChain creates a new MessageChain with the given session and branch IDs.
// It also initializes LastMessage by fetching the last message from the
func NewMessageChain(ctx context.Context, db DbOrTx, sessionID, branchID string) (mc *MessageChain, err error) {
	mc = &MessageChain{
		SessionID: sessionID,
		BranchID:  branchID,
		Messages:  []Message{},
	}

	// Get the last message ID for the current branch from the database
	mc.LastMessageID, mc.LastMessageModel, mc.LastMessageGeneration, err = GetLastMessageInBranch(db, mc.SessionID, mc.BranchID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No messages in this branch yet, LastMessage remains nil
			mc.LastMessageID = 0
			mc.LastMessageGeneration = 0
			mc.LastMessageModel = ""
		} else {
			return nil, fmt.Errorf("failed to get last message in branch for NewMessageChain: %w", err)
		}
	}

	if mc.LastMessageModel == "" {
		mc.LastMessageModel = DefaultGeminiModel
	}

	return mc, nil
}

// Add adds a message to the chain, updating parent_message_id and chosen_next_id.
// It returns the offset to mc.Messages.
func (mc *MessageChain) Add(ctx context.Context, db DbOrTx, msg Message) (Message, error) {
	msg.SessionID = mc.SessionID
	msg.BranchID = mc.BranchID

	var parentMessageID *int
	if mc.LastMessageID != 0 {
		parentMessageID = &mc.LastMessageID
	}
	msg.ParentMessageID = parentMessageID

	if msg.Generation == 0 {
		msg.Generation = mc.LastMessageGeneration // Default to last message's generation
	}
	if msg.Model == "" {
		msg.Model = mc.LastMessageModel // Default to last message's model
	}

	// Add the message to the database
	messageID, err := AddMessageToSession(ctx, db, msg)
	if err != nil {
		return Message{}, fmt.Errorf("failed to add message to session: %w", err)
	}
	msg.ID = messageID // Update the message ID after it's saved to DB

	// If there was a previous message, update its chosen_next_id
	if mc.LastMessageID != 0 {
		if err := UpdateMessageChosenNextID(db, mc.LastMessageID, &messageID); err != nil {
			return Message{}, fmt.Errorf("failed to update chosen_next_id for previous message: %w", err)
		}
	} else {
		// This is the first message in the chain, update session's chosen_first_id
		sqlDB, ok := db.(*sql.DB)
		if !ok {
			// Can't cast to *sql.DB, skip session update
			return msg, nil
		}
		if err := UpdateSessionChosenFirstID(sqlDB, mc.SessionID, &messageID); err != nil {
			// Non-fatal error, log but continue
			log.Printf("Failed to update chosen_first_id for session %s: %v", mc.SessionID, err)
		}
	}

	mc.Messages = append(mc.Messages, msg)
	mc.LastMessageID = msg.ID

	return msg, nil
}
