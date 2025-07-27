package main

import (
	"database/sql"
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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (session_id) REFERENCES sessions(id)
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
	ID         string `json:"id"`
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

func GetSessionHistory(sessionID string) ([]Content, error) {
	rows, err := db.Query("SELECT role, text FROM messages WHERE session_id = ? ORDER BY created_at ASC", sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session history: %w", err)
	}
	defer rows.Close()

	var history []Content
	for rows.Next() {
		var role, text string
		if err := rows.Scan(&role, &text); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		switch role {
		case "user", "model":
			// acceptable
		case "thought":
			continue // should be omitted from the history
		default:
			log.Printf("GetSessionHistory: unexpected role %q", role)
			continue
		}

		history = append(history, Content{
			Role:  role,
			Parts: []Part{{Text: text}},
		})
	}
	return history, nil
}

func AddMessageToSession(sessionID string, role string, text string) error {
	_, err := db.Exec("INSERT INTO messages (session_id, role, text) VALUES (?, ?, ?)", sessionID, role, text)
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
