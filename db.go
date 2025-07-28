package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InitDB() {
	var err error
	db, err = sql.Open("sqlite3", "X:/angel.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		text TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'text',
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

	log.Println("Database initialized and tables created.")
}

// Session struct to hold session data
type Session struct {
	ID          string `json:"id"`
	LastUpdated string `json:"last_updated_at"`
}

func CreateSession(sessionID string) error {
	_, err := db.Exec("INSERT INTO sessions (id) VALUES (?)", sessionID)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
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

func GetAllSessions() ([]Session, error) {
	rows, err := db.Query("SELECT id, last_updated_at FROM sessions ORDER BY last_updated_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query all sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.LastUpdated); err != nil {
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

// AddMessageToSession now accepts a message type
func AddMessageToSession(sessionID string, role string, text string, msgType string) error {
	_, err := db.Exec("INSERT INTO messages (session_id, role, text, type) VALUES (?, ?, ?, ?)", sessionID, role, text, msgType)
	if err != nil {
		return fmt.Errorf("failed to add message to session: %w", err)
	}
	return nil
}

func SaveOAuthToken(tokenJSON string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO oauth_tokens (id, token_data) VALUES (1, ?)", tokenJSON)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	return nil
}

func LoadOAuthToken() (string, error) {
	var tokenJSON string
	err := db.QueryRow("SELECT token_data FROM oauth_tokens WHERE id = 1").Scan(&tokenJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // No token found, not an error
		}
		return "", fmt.Errorf("failed to load OAuth token: %w", err)
	}
	return tokenJSON, nil
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

func GetSessionHistory(sessionId string, discardThoughts bool) ([]Content, error) {
	rows, err := db.Query("SELECT role, text, type FROM messages WHERE session_id = ? ORDER BY created_at ASC", sessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to query chat history: %w", err)
	}
	defer rows.Close()

	var history []Content
	for rows.Next() {
		var role, message, msgType string
		if err := rows.Scan(&role, &message, &msgType); err != nil {
			return nil, fmt.Errorf("failed to scan chat history row: %w", err)
		}
		// Filter out "thought" messages when retrieving history for the model
		if discardThoughts && role == "thought" {
			continue
		}

		var part Part
		switch msgType {
		case "function_call":
			var fc FunctionCall
			if err := json.Unmarshal([]byte(message), &fc); err != nil {
				log.Printf("Failed to unmarshal FunctionCall: %v", err)
				continue
			}
			part = Part{FunctionCall: &fc}
		case "function_response":
			var fr FunctionResponse
			if err := json.Unmarshal([]byte(message), &fr); err != nil {
				log.Printf("Failed to unmarshal FunctionResponse: %v", err)
				continue
			}
			part = Part{FunctionResponse: &fr}
		default:
			part = Part{Text: message}
		}

		history = append(history, Content{
			Role:  role,
			Parts: []Part{part},
		})
	}

	return history, nil
}

func formatJSON(data interface{}) string {
	prettyJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(prettyJSON)
}
