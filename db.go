package main

import (
	"context"
	"crypto/sha512" // For SHA-512/256
	"database/sql"  // For base64 encoding/decoding
	"encoding/hex"  // For encoding hash to string
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// createTables creates the necessary tables in the database.
func createTables(db *sql.DB) error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		system_prompt TEXT,
		name TEXT DEFAULT '',
		workspace_id TEXT DEFAULT '',
		primary_branch_id TEXT -- New column for primary branch
	);

	CREATE TABLE IF NOT EXISTS branches (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		parent_branch_id TEXT,
		branch_from_message_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL, -- Keep session_id to link to session
		branch_id TEXT NOT NULL, -- New column for branch
		parent_message_id INTEGER, -- New column for branching
		chosen_next_id INTEGER, -- New column for chosen next message
		role TEXT NOT NULL,
		text TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'text',
		attachments TEXT, -- This will store JSON array of blob hashes
		cumul_token_count INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		model TEXT NOT NULL
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
	);

	CREATE TABLE IF NOT EXISTS blobs (
		id TEXT PRIMARY KEY, -- SHA-512/256 hash of the data
		data BLOB NOT NULL
	);`
	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("Failed to create tables: %w", err)
	}
	return nil
}

// InitDB initializes the SQLite database connection and creates tables if they don't exist.
func InitDB(dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite (especially important for :memory: databases)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // No connection lifetime limit

	// Ping the database to ensure the connection is established
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create tables
	if err = createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	log.Println("Database initialized and tables created.")
	return db, nil
}

// Keep global db for non-test usage, but tests will use their own instance

// Session struct to hold session data
type Session struct {
	ID              string `json:"id"`
	LastUpdated     string `json:"last_updated_at"`
	SystemPrompt    string `json:"system_prompt"`
	Name            string `json:"name"`
	WorkspaceID     string `json:"workspace_id"`
	PrimaryBranchID string `json:"primary_branch_id"` // Changed to string
}

// Branch struct to hold branch data
type Branch struct {
	ID                  string  `json:"id"`
	SessionID           string  `json:"session_id"`
	ParentBranchID      *string `json:"parent_branch_id"`       // Pointer for nullable
	BranchFromMessageID *int    `json:"branch_from_message_id"` // Pointer for nullable
	CreatedAt           string  `json:"created_at"`
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
	ID              int              `json:"id"`
	SessionID       string           `json:"session_id"`        // Existing
	BranchID        string           `json:"branch_id"`         // New field
	ParentMessageID *int             `json:"parent_message_id"` // New field, pointer for nullable
	ChosenNextID    *int             `json:"chosen_next_id"`    // New field, pointer for nullable
	Role            string           `json:"role"`
	Text            string           `json:"text"`
	Type            string           `json:"type"`
	Attachments     []FileAttachment `json:"attachments,omitempty"`
	CumulTokenCount *int             `json:"cumul_token_count,omitempty"`
	CreatedAt       string           `json:"created_at"`
	Model           string           `json:"model,omitempty"` // New field for the model that generated the message
}

// PossibleNextMessage struct to hold possible next message data for the frontend
type PossibleNextMessage struct {
	MessageID string `json:"messageId"`
	BranchID  string `json:"branchId"`
}

// FrontendMessage struct to match the frontend's ChatMessage interface
type FrontendMessage struct {
	ID              string                `json:"id"`
	Role            string                `json:"role"`
	Parts           []Part                `json:"parts"`
	Type            string                `json:"type"`
	Attachments     []FileAttachment      `json:"attachments,omitempty"`
	CumulTokenCount *int                  `json:"cumul_token_count,omitempty"`
	SessionID       string                `json:"sessionId,omitempty"` // Add SessionID to FrontendMessage
	BranchID        string                `json:"branchId,omitempty"`
	ParentMessageID *string               `json:"parentMessageId,omitempty"`
	ChosenNextID    *string               `json:"chosenNextId,omitempty"`
	PossibleNextIDs []PossibleNextMessage `json:"possibleNextIds,omitempty"`
	Model           string                `json:"model,omitempty"`
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
func CreateWorkspace(db *sql.DB, workspaceID string, name string, defaultSystemPrompt string) error {
	_, err := db.Exec("INSERT INTO workspaces (id, name, default_system_prompt) VALUES (?, ?, ?)", workspaceID, name, defaultSystemPrompt)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	return nil
}

// GetWorkspace retrieves a single workspace by its ID.
func GetWorkspace(db *sql.DB, workspaceID string) (Workspace, error) {
	var w Workspace
	err := db.QueryRow("SELECT id, name, default_system_prompt, created_at FROM workspaces WHERE id = ?", workspaceID).Scan(&w.ID, &w.Name, &w.DefaultSystemPrompt, &w.CreatedAt)
	if err != nil {
		return w, fmt.Errorf("failed to get workspace: %w", err)
	}
	return w, nil
}

// GetAllWorkspaces retrieves all workspaces from the database.
func GetAllWorkspaces(db *sql.DB) ([]Workspace, error) {
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
func DeleteWorkspace(db *sql.DB, workspaceID string) error {
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

func CreateSession(db *sql.DB, sessionID string, systemPrompt string, workspaceID string) (string, error) {
	primaryBranchID := generateID() // Generate a new ID for the primary branch
	_, err := db.Exec("INSERT INTO sessions (id, system_prompt, name, workspace_id, primary_branch_id) VALUES (?, ?, ?, ?, ?)", sessionID, systemPrompt, "", workspaceID, primaryBranchID)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	// Also create the initial branch entry in the branches table
	_, err = db.Exec("INSERT INTO branches (id, session_id, parent_branch_id, branch_from_message_id) VALUES (?, ?, NULL, NULL)", primaryBranchID, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to create initial branch for session: %w", err)
	}
	return primaryBranchID, nil
}

// CreateBranch creates a new branch in the database.
func CreateBranch(db *sql.DB, branchID string, sessionID string, parentBranchID *string, branchFromMessageID *int) (string, error) {
	_, err := db.Exec("INSERT INTO branches (id, session_id, parent_branch_id, branch_from_message_id) VALUES (?, ?, ?, ?)", branchID, sessionID, parentBranchID, branchFromMessageID)
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}
	return branchID, nil
}

func UpdateSessionSystemPrompt(db *sql.DB, sessionID string, systemPrompt string) error {
	_, err := db.Exec("UPDATE sessions SET system_prompt = ? WHERE id = ?", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session system prompt: %w", err)
	}
	return nil
}

func UpdateSessionLastUpdated(db *sql.DB, sessionID string) error {
	_, err := db.Exec("UPDATE sessions SET last_updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session last_updated_at: %w", err)
	}
	return nil
}

func UpdateSessionName(db *sql.DB, sessionID string, name string) error {
	_, err := db.Exec("UPDATE sessions SET name = ? WHERE id = ?", name, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session name: %w", err)
	}
	return nil
}

func GetWorkspaceAndSessions(db *sql.DB, workspaceID string) (*WorkspaceWithSessions, error) {
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

// AddMessageToSession now accepts a message type, attachments, and numTokens, and branch_id, parent_message_id, chosen_next_id
func AddMessageToSession(ctx context.Context, db *sql.DB, msg Message) (int, error) {
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

	result, err := db.Exec("INSERT INTO messages (session_id, branch_id, parent_message_id, chosen_next_id, role, text, type, attachments, cumul_token_count, model) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", msg.SessionID, msg.BranchID, msg.ParentMessageID, msg.ChosenNextID, msg.Role, msg.Text, msg.Type, string(attachmentsJSON), msg.CumulTokenCount, msg.Model)
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
func UpdateMessageChosenNextID(db *sql.DB, messageID int, chosenNextID *int) error {
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

func SaveOAuthToken(db *sql.DB, tokenJSON string, userEmail string, projectID string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO oauth_tokens (id, token_data, user_email, project_id) VALUES (1, ?, ?, ?)", tokenJSON, userEmail, projectID)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	return nil
}

func LoadOAuthToken(db *sql.DB) (string, string, string, error) {
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
func DeleteOAuthToken(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM oauth_tokens WHERE id = 1")
	if err != nil {
		return fmt.Errorf("failed to delete OAuth token: %w", err)
	}
	return nil
}

// SessionExists checks if a session with the given ID exists.
func SessionExists(db *sql.DB, sessionID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ?)", sessionID).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check session existence: %w", err)
	}
	return exists, nil
}

func GetSession(db *sql.DB, sessionID string) (Session, error) {
	var s Session
	err := db.QueryRow("SELECT id, last_updated_at, system_prompt, name, workspace_id, COALESCE(primary_branch_id, '') FROM sessions WHERE id = ?", sessionID).Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID, &s.PrimaryBranchID)
	if err != nil {
		return s, err
	}
	return s, nil
}

func GetBranch(db *sql.DB, branchID string) (Branch, error) {
	var b Branch
	err := db.QueryRow("SELECT id, session_id, parent_branch_id, branch_from_message_id, created_at FROM branches WHERE id = ?", branchID).Scan(&b.ID, &b.SessionID, &b.ParentBranchID, &b.BranchFromMessageID, &b.CreatedAt)
	if err != nil {
		return b, fmt.Errorf("failed to get branch: %w", err)
	}
	return b, nil
}

// GetSessionHistory retrieves the chat history for a given session and its primary branch,
// recursively fetching messages from parent branches.
func GetSessionHistory(db *sql.DB, sessionID string, primaryBranchID string, discardThoughts bool) ([]FrontendMessage, error) {
	var history [][]FrontendMessage
	messageIdLimit := math.MaxInt
	branchID := primaryBranchID
	keepGoing := true
	for keepGoing {
		err := func() error {
			rows, err := db.Query(`
				SELECT
					m.id, m.session_id, m.branch_id, m.parent_message_id, m.chosen_next_id,
					m.role, m.text, m.type, m.attachments, m.cumul_token_count, m.created_at, m.model,
					coalesce(group_concat(mm.id || ',' || mm.branch_id), '')
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
					&m.Role, &m.Text, &m.Type, &attachmentsJSON, &m.CumulTokenCount, &m.CreatedAt, &m.Model,
					&possibleNextIDsAndBranchesStr,
				); err != nil {
					return fmt.Errorf("failed to scan message: %w", err)
				}
				if attachmentsJSON.Valid {
					if err := json.Unmarshal([]byte(attachmentsJSON.String), &m.Attachments); err != nil {
						log.Printf("Failed to unmarshal attachments for message %d: %v", m.ID, err)
					}
				}

				if discardThoughts && m.Role == "thought" {
					// Skip thought messages
					continue
				}

				var parts []Part
				var tokens *int

				if m.CumulTokenCount != nil {
					tokens = m.CumulTokenCount
				}

				if m.Text != "" {
					parts = append(parts, Part{Text: m.Text})
				}

				// m.Attachments is already []FileAttachment type, use it directly
				// No need for a separate 'attachments' variable here.

				var fmParentMessageID *string
				if m.ParentMessageID != nil {
					s := fmt.Sprintf("%d", *m.ParentMessageID)
					fmParentMessageID = &s
					if len(messages) == 0 {
						parentBranchMessageID = *m.ParentMessageID
					}
				}

				var fmChosenNextID *string
				if m.ChosenNextID != nil {
					s := fmt.Sprintf("%d", *m.ChosenNextID)
					fmChosenNextID = &s
				}

				var fmPossibleNextIDs []PossibleNextMessage
				if possibleNextIDsAndBranchesStr != "" {
					possibleNextIDsAndBranches := strings.Split(possibleNextIDsAndBranchesStr, ",")
					for i := 0; i < len(possibleNextIDsAndBranches); i += 2 {
						if i+1 < len(possibleNextIDsAndBranches) { // Ensure there's a branch ID for the message ID
							fmPossibleNextIDs = append(fmPossibleNextIDs, PossibleNextMessage{
								MessageID: possibleNextIDsAndBranches[i],
								BranchID:  possibleNextIDsAndBranches[i+1],
							})
						} else {
							log.Printf("Warning: Malformed possibleNextIDsAndBranchesStr for message %d: %s", m.ID, possibleNextIDsAndBranchesStr)
						}
					}
				}

				switch m.Type {
				case "function_call":
					var fc FunctionCall
					if err := json.Unmarshal([]byte(m.Text), &fc); err != nil {
						log.Printf("Failed to unmarshal FunctionCall for message %d: %v", m.ID, err)
					} else {
						parts = []Part{{FunctionCall: &fc}}
					}
				case "function_response":
					var fr FunctionResponse
					if err := json.Unmarshal([]byte(m.Text), &fr); err != nil {
						log.Printf("Failed to unmarshal FunctionResponse for message %d: %v", m.ID, err)
					} else {
						parts = []Part{{FunctionResponse: &fr}}
					}
				}

				messages = append(messages, FrontendMessage{
					ID:              fmt.Sprintf("%d", m.ID),
					Role:            m.Role,
					Parts:           parts,
					Type:            m.Type,
					Attachments:     m.Attachments, // Use m.Attachments directly
					CumulTokenCount: tokens,
					SessionID:       m.SessionID, // Populate SessionID
					BranchID:        m.BranchID,
					ParentMessageID: fmParentMessageID,
					ChosenNextID:    fmChosenNextID,
					PossibleNextIDs: fmPossibleNextIDs,
					Model:           m.Model,
				})
			}

			if len(messages) == 0 {
				keepGoing = false
				return nil
			}

			history = append(history, messages)
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

	var combinedHistory []FrontendMessage
	for i := len(history) - 1; i >= 0; i-- {
		combinedHistory = append(combinedHistory, history[i]...)
	}
	return combinedHistory, nil
}

// DeleteSession deletes a session and all its associated messages.
func DeleteSession(db *sql.DB, sessionID string) error {
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

// MCPServerConfig struct to hold MCP server configuration data
type MCPServerConfig struct {
	Name       string          `json:"name"`
	ConfigJSON json.RawMessage `json:"config_json"`
	Enabled    bool            `json:"enabled"`
}

// SaveMCPServerConfig saves an MCP server configuration to the database.
func SaveMCPServerConfig(db *sql.DB, config MCPServerConfig) error {
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
func GetMCPServerConfigs(db *sql.DB) ([]MCPServerConfig, error) {
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
func DeleteMCPServerConfig(db *sql.DB, name string) error {
	result, err := db.Exec("DELETE FROM mcp_configs WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("failed to delete MCP server config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("MCP config with name %s not found", name)
	}
	return nil
}

// UpdateMessageTokens updates the cumul_token_count for a specific message.
func UpdateMessageTokens(db *sql.DB, messageID int, cumulTokenCount int) error {
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
func GetLastMessageInBranch(db *sql.DB, sessionID string, branchID string) (int, string, error) {
	var lastMessageID int
	var lastMessageModel string
	err := db.QueryRow("SELECT id, model FROM messages WHERE session_id = ? AND branch_id = ? AND chosen_next_id IS NULL ORDER BY created_at DESC LIMIT 1", sessionID, branchID).Scan(&lastMessageID, &lastMessageModel)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get last message in branch: %w", err)
	}
	return lastMessageID, lastMessageModel, nil
}

// GetMessageDetails retrieves the role, type, parent_message_id, and branch_id for a given message ID.
func GetMessageDetails(db *sql.DB, messageID int) (string, string, sql.NullInt64, string, error) {
	var role, msgType, branchID string
	var parentMessageID sql.NullInt64
	err := db.QueryRow("SELECT role, type, parent_message_id, branch_id FROM messages WHERE id = ?", messageID).Scan(&role, &msgType, &parentMessageID, &branchID)
	if err != nil {
		return "", "", sql.NullInt64{}, "", fmt.Errorf("failed to get message details: %w", err)
	}
	return role, msgType, parentMessageID, branchID, nil
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
			role, text, type, attachments, cumul_token_count, created_at, model
		FROM messages
		WHERE id = ?
	`, messageID).Scan(
		&m.ID, &m.SessionID, &m.BranchID, &m.ParentMessageID, &m.ChosenNextID,
		&m.Role, &m.Text, &m.Type, &attachmentsJSON, &m.CumulTokenCount, &m.CreatedAt, &m.Model,
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

// SaveBlob saves a blob to the blobs table. If a blob with the same hash already exists, it replaces it.
// It returns the SHA-512/256 hash of the data.
func SaveBlob(ctx context.Context, db *sql.DB, data []byte) (string, error) {
	hasher := sha512.New512_256()
	hasher.Write(data)
	hash := hasher.Sum(nil)
	hashStr := hex.EncodeToString(hash)

	_, err := db.ExecContext(ctx, "INSERT OR REPLACE INTO blobs (id, data) VALUES (?, ?)", hashStr, data)
	if err != nil {
		return "", fmt.Errorf("failed to insert or replace blob: %w", err)
	}

	return hashStr, nil
}

// GetBlob retrieves a blob from the blobs table by its SHA-512/256 hash.
func GetBlob(db *sql.DB, hashStr string) ([]byte, error) {
	var data []byte
	err := db.QueryRow("SELECT data FROM blobs WHERE id = ?", hashStr).Scan(&data)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("blob not found for hash: %s", hashStr)
		}
		return nil, fmt.Errorf("failed to get blob: %w", err)
	}
	return data, nil
}
