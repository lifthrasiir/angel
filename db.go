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
		name TEXT DEFAULT '',
		workspace_id TEXT DEFAULT ''
	);

		CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		text TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'text',
		attachments TEXT,
		cumul_token_count INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS oauth_tokens (
		id INTEGER PRIMARY KEY,
		token_data TEXT NOT NULL,
		user_email TEXT,
		project_id TEXT
	);

	CREATE TABLE IF NOT EXISTS workspaces (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		default_system_prompt TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS mcp_configs (
		name TEXT PRIMARY KEY,
		config_json TEXT NOT NULL,
		enabled BOOLEAN NOT NULL
	);`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	// Add cumul_token_count column to messages table if it doesn't exist
	_, err = db.Exec("ALTER TABLE messages ADD COLUMN cumul_token_count INTEGER;")
	if err != nil {
		// Check if the error is due to the column already existing
		if strings.Contains(err.Error(), "duplicate column name: cumul_token_count") {
			log.Println("Column 'cumul_token_count' already exists in 'messages' table. Skipping ALTER TABLE.")
		} else {
			log.Fatalf("Failed to add cumul_token_count column to messages table: %v", err)
		}
	} else {
		log.Println("Added cumul_token_count column to messages table.")
	}

	log.Println("Database initialized and tables created.")
}

// Session struct to hold session data
type Session struct {
	ID           string `json:"id"`
	LastUpdated  string `json:"last_updated_at"`
	SystemPrompt string `json:"system_prompt"`
	Name         string `json:"name"`
	WorkspaceID  string `json:"workspace_id"`
}

// FileAttachment struct to hold file attachment data
type FileAttachment struct {
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // Base64 encoded file content
}

// Message struct to hold message data for database interaction
type Message struct {
	ID              int              `json:"id"`
	SessionID       string           `json:"session_id"`
	Role            string           `json:"role"`
	Text            string           `json:"text"`
	Type            string           `json:"type"`
	Attachments     []FileAttachment `json:"attachments,omitempty"` // New field
	CumulTokenCount *int             `json:"cumul_token_count,omitempty"`
	CreatedAt       string           `json:"created_at"`
}

// FrontendMessage struct to match the frontend's ChatMessage interface
type FrontendMessage struct {
	ID              string           `json:"id"`
	Role            string           `json:"role"`
	Parts           []Part           `json:"parts"`
	Type            string           `json:"type"`
	Attachments     []FileAttachment `json:"attachments,omitempty"`
	CumulTokenCount *int             `json:"cumul_token_count,omitempty"`
}

type Workspace struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	DefaultSystemPrompt string `json:"default_system_prompt"`
	CreatedAt           string `json:"created_at"`
}

type WorkspaceWithSessions struct {
	Workspace Workspace `json:"workspace"`
	Sessions  []Session `json:"sessions"`
}

// CreateWorkspace creates a new workspace in the database.
func CreateWorkspace(workspaceID string, name string, defaultSystemPrompt string) error {
	_, err := db.Exec("INSERT INTO workspaces (id, name, default_system_prompt) VALUES (?, ?, ?)", workspaceID, name, defaultSystemPrompt)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	return nil
}

// GetWorkspace retrieves a single workspace by its ID.
func GetWorkspace(workspaceID string) (Workspace, error) {
	var w Workspace
	err := db.QueryRow("SELECT id, name, default_system_prompt, created_at FROM workspaces WHERE id = ?", workspaceID).Scan(&w.ID, &w.Name, &w.DefaultSystemPrompt, &w.CreatedAt)
	if err != nil {
		return w, fmt.Errorf("failed to get workspace: %w", err)
	}
	return w, nil
}

// GetAllWorkspaces retrieves all workspaces from the database.
func GetAllWorkspaces() ([]Workspace, error) {
	rows, err := db.Query("SELECT id, name, default_system_prompt, created_at FROM workspaces ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query all workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.DefaultSystemPrompt, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan workspace: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	// Ensure an empty slice is returned, not nil, for JSON marshaling
	if workspaces == nil {
		return []Workspace{}, nil
	}
	return workspaces, nil
}

// DeleteWorkspace deletes a workspace and all its associated sessions and messages.
func DeleteWorkspace(workspaceID string) error {
	// Start a transaction to ensure atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Delete messages associated with the sessions in the workspace
	_, err = tx.Exec("DELETE FROM messages WHERE session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", workspaceID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete messages for workspace %s: %w", workspaceID, err)
	}

	// Delete sessions associated with the workspace
	_, err = tx.Exec("DELETE FROM sessions WHERE workspace_id = ?", workspaceID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete sessions for workspace %s: %w", workspaceID, err)
	}

	// Delete the workspace itself
	_, err = tx.Exec("DELETE FROM workspaces WHERE id = ?", workspaceID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete workspace %s: %w", workspaceID, err)
	}

	return tx.Commit()
}

func CreateSession(sessionID string, systemPrompt string, workspaceID string) error {
	_, err := db.Exec("INSERT INTO sessions (id, system_prompt, name, workspace_id) VALUES (?, ?, ?, ?)", sessionID, systemPrompt, "", workspaceID)
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

func GetWorkspaceAndSessions(workspaceID string) (*WorkspaceWithSessions, error) {
	var wsWithSessions WorkspaceWithSessions

	// Get workspace information
	var workspace Workspace
	if workspaceID != "" {
		err := db.QueryRow("SELECT id, name, default_system_prompt, created_at FROM workspaces WHERE id = ?", workspaceID).Scan(&workspace.ID, &workspace.Name, &workspace.DefaultSystemPrompt, &workspace.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	} else {
		// Default workspace for sessions without a specific workspaceId
		workspace = Workspace{
			ID:   "",
			Name: "",
		}
	}
	wsWithSessions.Workspace = workspace

	// Get sessions for the workspace
	var query string
	var args []interface{}

	if workspaceID == "" {
		query = "SELECT id, last_updated_at, system_prompt, name, workspace_id FROM sessions WHERE workspace_id = '' ORDER BY last_updated_at DESC"
	} else {
		query = "SELECT id, last_updated_at, system_prompt, name, workspace_id FROM sessions WHERE workspace_id = ? ORDER BY last_updated_at DESC"
		args = append(args, workspaceID)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		wsWithSessions.Sessions = []Session{}
	} else {
		wsWithSessions.Sessions = sessions
	}

	return &wsWithSessions, nil
}

// AddMessageToSession now accepts a message type, attachments, and numTokens
func AddMessageToSession(sessionID string, role string, text string, msgType string, attachments []FileAttachment, cumulTokenCount *int) (int, error) {
	attachmentsJSON, err := json.Marshal(attachments)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal attachments: %w", err)
	}

	result, err := db.Exec("INSERT INTO messages (session_id, role, text, type, attachments, cumul_token_count) VALUES (?, ?, ?, ?, ?, ?)", sessionID, role, text, msgType, string(attachmentsJSON), cumulTokenCount)
	if err != nil {
		return 0, fmt.Errorf("failed to add message to session: %w", err)
	}

	lastInsertID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return int(lastInsertID), nil
}

func SaveOAuthToken(tokenJSON string, userEmail string, projectID string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO oauth_tokens (id, token_data, user_email, project_id) VALUES (1, ?, ?, ?)", tokenJSON, userEmail, projectID)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	return nil
}

func LoadOAuthToken() (string, string, string, error) {
	var tokenJSON string
	var nullUserEmail sql.NullString
	var nullProjectID sql.NullString
	err := db.QueryRow("SELECT token_data, user_email, project_id FROM oauth_tokens WHERE id = 1").Scan(&tokenJSON, &nullUserEmail, &nullProjectID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("LoadOAuthToken: No existing token found in DB.")
			return "", "", "", nil // No token found, not an error
		}
		return "", "", "", fmt.Errorf("failed to load OAuth token: %w", err)
	}
	userEmail := nullUserEmail.String
	projectID := nullProjectID.String

	return tokenJSON, userEmail, projectID, nil
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
	err := db.QueryRow("SELECT id, last_updated_at, system_prompt, name, workspace_id FROM sessions WHERE id = ?", sessionID).Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID)
	if err != nil {
		return s, fmt.Errorf("failed to get session: %w", err)
	}
	return s, nil
}

func GetSessionHistory(sessionId string, discardThoughts bool) ([]FrontendMessage, error) {
	rows, err := db.Query("SELECT id, role, text, type, attachments, cumul_token_count FROM messages WHERE session_id = ? ORDER BY created_at ASC", sessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to query chat history: %w", err)
	}
	defer rows.Close()

	var history []FrontendMessage
	for rows.Next() {
		var id int
		var role, message, msgType string
		var attachmentsJSON sql.NullString
		var cumulTokenCount sql.NullInt64
		if err := rows.Scan(&id, &role, &message, &msgType, &attachmentsJSON, &cumulTokenCount); err != nil {
			return nil, fmt.Errorf("failed to scan chat history row: %w", err)
		}
		// Filter out "thought" messages when retrieving history for the model
		if discardThoughts && role == "thought" {
			continue
		}

		var parts []Part
		var attachments []FileAttachment
		var tokens *int

		if cumulTokenCount.Valid {
			t := int(cumulTokenCount.Int64)
			tokens = &t
		}

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
			ID:              fmt.Sprintf("%d", id),
			Role:            role,
			Parts:           parts,
			Type:            msgType,
			Attachments:     attachments,
			CumulTokenCount: tokens,
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

// MCPServerConfig struct to hold MCP server configuration data
type MCPServerConfig struct {
	Name       string          `json:"name"`
	ConfigJSON json.RawMessage `json:"config_json"`
	Enabled    bool            `json:"enabled"`
}

// SaveMCPServerConfig saves an MCP server configuration to the database.
func SaveMCPServerConfig(config MCPServerConfig) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO mcp_configs (name, config_json, enabled)
		VALUES (?, ?, ?)
	`, config.Name, string(config.ConfigJSON), config.Enabled)
	if err != nil {
		return fmt.Errorf("failed to save MCP server config: %w", err)
	}
	return nil
}

// GetMCPServerConfigs retrieves all MCP server configurations from the database.
func GetMCPServerConfigs() ([]MCPServerConfig, error) {
	rows, err := db.Query("SELECT name, config_json, enabled FROM mcp_configs")
	if err != nil {
		return nil, fmt.Errorf("failed to query MCP server configs: %w", err)
	}
	defer rows.Close()

	var configs []MCPServerConfig
	for rows.Next() {
		var config MCPServerConfig
		var connConfigJSON string
		if err := rows.Scan(&config.Name, &connConfigJSON, &config.Enabled); err != nil {
			return nil, fmt.Errorf("failed to scan MCP server config: %w", err)
		}
		config.ConfigJSON = json.RawMessage(connConfigJSON)
		configs = append(configs, config)
	}
	if configs == nil {
		return []MCPServerConfig{}, nil
	}
	return configs, nil
}

// DeleteMCPServerConfig deletes an MCP server configuration from the database.
func DeleteMCPServerConfig(name string) error {
	_, err := db.Exec("DELETE FROM mcp_configs WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("failed to delete MCP server config: %w", err)
	}
	return nil
}

// UpdateMessageTokens updates the cumul_token_count for a specific message.
func UpdateMessageTokens(messageID int, cumulTokenCount int) error {
	_, err := db.Exec("UPDATE messages SET cumul_token_count = ? WHERE id = ?", cumulTokenCount, messageID)
	if err != nil {
		return fmt.Errorf("failed to update message tokens: %w", err)
	}
	return nil
}
