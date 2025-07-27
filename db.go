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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

func CreateSession(sessionID string) error {
	_, err := db.Exec("INSERT INTO sessions (id) VALUES (?)", sessionID)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
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
