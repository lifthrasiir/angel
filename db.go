package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InitDB() {
	var err error
	db, err = sql.Open("sqlite3", "angel.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		system_prompt TEXT,
		name TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		text TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'text',
		attachments TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS oauth_tokens (
		id INTEGER PRIMARY KEY,
		token_data TEXT NOT NULL
	);
	`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	// Attempt to add user_email column, ignore if it already exists
	alterTableSQL := `ALTER TABLE oauth_tokens ADD COLUMN user_email TEXT;`
	_, err = db.Exec(alterTableSQL)
	if err != nil {
		// Check if the error is "duplicate column name"
		if !strings.Contains(err.Error(), "duplicate column name") {
			log.Fatalf("Failed to add user_email column: %v", err)
		}
		log.Println("user_email column already exists, skipping migration.")
	} else {
		log.Println("user_email column added successfully.")
	}

	log.Println("Database initialized and tables created.")
}

// Session struct to hold session data
type Session struct {
	ID           string `json:"id"`
	LastUpdated  string `json:"last_updated_at"`
	SystemPrompt string `json:"system_prompt"`
	Name         string `json:"name"`
}

// FileAttachment struct to hold file attachment data
type FileAttachment struct {
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // Base64 encoded file content
}

// Message struct to hold message data for database interaction
type Message struct {
	ID          int              `json:"id"`
	SessionID   string           `json:"session_id"`
	Role        string           `json:"role"`
	Text        string           `json:"text"`
	Type        string           `json:"type"`
	Attachments []FileAttachment `json:"attachments,omitempty"` // New field
	CreatedAt   string           `json:"created_at"`
}

// FrontendMessage struct to match the frontend's ChatMessage interface
type FrontendMessage struct {
	ID          string           `json:"id"`
	Role        string           `json:"role"`
	Parts       []Part           `json:"parts"`
	Type        string           `json:"type"`
	Attachments []FileAttachment `json:"attachments,omitempty"`
}

func CreateSession(sessionID string, systemPrompt string) error {
	_, err := db.Exec("INSERT INTO sessions (id, system_prompt, name) VALUES (?, ?, ?)", sessionID, systemPrompt, "")
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

func UpdateSessionSystemPrompt(sessionID string, systemPrompt string) error {
	_, err := db.Exec("UPDATE sessions SET system_prompt = ? WHERE id = ?", systemPrompt, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session system prompt: %w", err)
	}
	return nil
}

func UpdateSessionLastUpdated(sessionID string) error {
	_, err := db.Exec("UPDATE sessions SET last_updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session last_updated_at: %w", err)
	}
	return nil
}

func UpdateSessionName(sessionID string, name string) error {
	_, err := db.Exec("UPDATE sessions SET name = ? WHERE id = ?", name, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session name: %w", err)
	}
	return nil
}

func GetAllSessions() ([]Session, error) {
	rows, err := db.Query("SELECT id, last_updated_at, system_prompt, name FROM sessions ORDER BY last_updated_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query all sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	// Ensure an empty slice is returned, not nil, for JSON marshaling
	if sessions == nil {
		return []Session{}, nil
	}
	return sessions, nil
}

// AddMessageToSession now accepts a message type and attachments
func AddMessageToSession(sessionID string, role string, text string, msgType string, attachments []FileAttachment) error {
	attachmentsJSON, err := json.Marshal(attachments)
	if err != nil {
		return fmt.Errorf("failed to marshal attachments: %w", err)
	}

	_, err = db.Exec("INSERT INTO messages (session_id, role, text, type, attachments) VALUES (?, ?, ?, ?, ?)", sessionID, role, text, msgType, string(attachmentsJSON))
	if err != nil {
		return fmt.Errorf("failed to add message to session: %w", err)
	}
	return nil
}

func SaveOAuthToken(tokenJSON string, userEmail string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO oauth_tokens (id, token_data, user_email) VALUES (1, ?, ?)", tokenJSON, userEmail)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	return nil
}

func LoadOAuthToken() (string, string, error) {
	var tokenJSON string
	var nullUserEmail sql.NullString
	err := db.QueryRow("SELECT token_data, user_email FROM oauth_tokens WHERE id = 1").Scan(&tokenJSON, &nullUserEmail)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("LoadOAuthToken: No existing token found in DB.")
			return "", "", nil // No token found, not an error
		}
		return "", "", fmt.Errorf("failed to load OAuth token: %w", err)
	}
	userEmail := nullUserEmail.String

	return tokenJSON, userEmail, nil
}

// DeleteOAuthToken deletes the OAuth token from the database.
func DeleteOAuthToken() error {
	_, err := db.Exec("DELETE FROM oauth_tokens WHERE id = 1")
	if err != nil {
		return fmt.Errorf("failed to delete OAuth token: %w", err)
	}
	return nil
}

// SessionExists checks if a session with the given ID exists.
func SessionExists(sessionID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ?)", sessionID).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check session existence: %w", err)
	}
	return exists, nil
}

func GetSession(sessionID string) (Session, error) {
	var s Session
	err := db.QueryRow("SELECT id, last_updated_at, system_prompt, name FROM sessions WHERE id = ?", sessionID).Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name)
	if err != nil {
		return s, fmt.Errorf("failed to get session: %w", err)
	}
	return s, nil
}

func GetSessionHistory(sessionId string, discardThoughts bool) ([]FrontendMessage, error) {
	rows, err := db.Query("SELECT id, role, text, type, attachments FROM messages WHERE session_id = ? ORDER BY created_at ASC", sessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to query chat history: %w", err)
	}
	defer rows.Close()

	var history []FrontendMessage
	for rows.Next() {
		var id int
		var role, message, msgType string
		var attachmentsJSON sql.NullString
		if err := rows.Scan(&id, &role, &message, &msgType, &attachmentsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan chat history row: %w", err)
		}
		// Filter out "thought" messages when retrieving history for the model
		if discardThoughts && role == "thought" {
			continue
		}

		var parts []Part
		var attachments []FileAttachment

		// Add text part
		if message != "" {
			parts = append(parts, Part{Text: message})
		}

		// Unmarshal attachments if present
		if attachmentsJSON.Valid && attachmentsJSON.String != "" {
			if err := json.Unmarshal([]byte(attachmentsJSON.String), &attachments); err != nil {
				log.Printf("Failed to unmarshal attachments: %v", err)
			} else {
				// For FrontendMessage, attachments are directly assigned
			}
		}

		switch msgType {
		case "function_call":
			var fc FunctionCall
			if err := json.Unmarshal([]byte(message), &fc); err != nil {
				log.Printf("Failed to unmarshal FunctionCall: %v", err)
				continue
			}
			parts = []Part{{FunctionCall: &fc}}
		case "function_response":
			var fr FunctionResponse
			if err := json.Unmarshal([]byte(message), &fr); err != nil {
				log.Printf("Failed to unmarshal FunctionResponse: %v", err)
				continue
			}
			parts = []Part{{FunctionResponse: &fr}}
		}

		history = append(history, FrontendMessage{
			ID:          fmt.Sprintf("%d", id),
			Role:        role,
			Parts:       parts,
			Type:        msgType,
			Attachments: attachments,
		})
	}

	return history, nil
}

// DeleteSession deletes a session and all its associated messages.
func DeleteSession(sessionID string) error {
	// Start a transaction to ensure atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Delete messages associated with the session
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete messages for session %s: %w", sessionID, err)
	}

	// Delete the session itself
	_, err = tx.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete session %s: %w", sessionID, err)
	}

	return tx.Commit()
}

// DeleteLastEmptyModelMessage deletes the last message in a session if it's an empty "model" type.
func DeleteLastEmptyModelMessage(sessionID string) error {
	result, err := db.Exec(`
		DELETE FROM messages
		WHERE id = (
			SELECT id FROM messages
			WHERE session_id = ? AND role = 'model' AND type = 'text' AND TRIM(text) = ''
			ORDER BY created_at DESC
			LIMIT 1
		);
	`, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete last empty model message: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Failed to get rows affected after deleting empty model message: %v", err)
	}

	if rowsAffected > 0 {
		log.Printf("Deleted %d empty model message(s) for session %s", rowsAffected, sessionID)
	}

	return nil
}
