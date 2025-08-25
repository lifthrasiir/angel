package main

import (
	"context"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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
		primary_branch_id TEXT, -- New column for primary branch
		roots TEXT DEFAULT '[]' -- New column for exposed roots, defaults to empty JSON array
	);

	CREATE TABLE IF NOT EXISTS branches (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		parent_branch_id TEXT,
		branch_from_message_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		pending_confirmation TEXT,
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
	);

	CREATE TABLE IF NOT EXISTS global_prompts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL UNIQUE,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS app_configs (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL
	);

	CREATE TABLE IF NOT EXISTS shell_commands (
		id TEXT PRIMARY KEY,
		branch_id TEXT NOT NULL,
		command TEXT NOT NULL,
		status TEXT NOT NULL,
		start_time INTEGER NOT NULL,
		end_time INTEGER,
		stdout BLOB,
		stderr BLOB,
		exit_code INTEGER,
		error_message TEXT,
		last_polled_at INTEGER NOT NULL,
		next_poll_delay INTEGER NOT NULL,
		stdout_offset INTEGER NOT NULL DEFAULT 0,
		stderr_offset INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE
	);
	`
	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}
	return nil
}

// migrateDB handles database schema migrations.
func migrateDB(db *sql.DB) error {
	// Add 'roots' column to 'sessions' table if it doesn't exist
	_, err := db.Exec(`ALTER TABLE sessions ADD COLUMN roots TEXT DEFAULT '[]'`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("failed to add roots column to sessions table: %w", err)
	}
	// Add 'pending_confirmation' column to 'branches' table if it doesn't exist
	_, err = db.Exec(`ALTER TABLE branches ADD COLUMN pending_confirmation TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("failed to add pending_confirmation column to branches table: %w", err)
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

	// Run migrations
	if err = migrateDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	log.Println("Database initialized and tables created.")
	return db, nil
}

// Session struct to hold session data
type Session struct {
	ID              string   `json:"id"`
	LastUpdated     string   `json:"last_updated_at"`
	SystemPrompt    string   `json:"system_prompt"`
	Name            string   `json:"name"`
	WorkspaceID     string   `json:"workspace_id"`
	PrimaryBranchID string   `json:"primary_branch_id"`
	Roots           []string `json:"roots"`
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
	defer tx.Rollback() // Rollback on error

	// Delete messages associated with the sessions in the workspace
	_, err = tx.Exec("DELETE FROM messages WHERE session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", workspaceID)
	if err != nil {
		return fmt.Errorf("failed to delete messages for workspace %s: %w", workspaceID, err)
	}

	// Delete shell commands associated with the sessions in the workspace
	_, err = tx.Exec("DELETE FROM shell_commands WHERE branch_id IN (SELECT id FROM branches WHERE session_id IN (SELECT id FROM sessions WHERE workspace_id = ?))", workspaceID)
	if err != nil {
		return fmt.Errorf("failed to delete shell commands for workspace %s: %w", workspaceID, err)
	}

	// Delete sessions associated with the workspace
	_, err = tx.Exec("DELETE FROM sessions WHERE workspace_id = ?", workspaceID)
	if err != nil {
		return fmt.Errorf("failed to delete sessions for workspace %s: %w", workspaceID, err)
	}

	// Delete the workspace itself
	_, err = tx.Exec("DELETE FROM workspaces WHERE id = ?", workspaceID)
	if err != nil {
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

func UpdateSessionSystemPrompt(db *sql.DB, sessionID string, systemPrompt string) error {
	_, err := db.Exec("UPDATE sessions SET system_prompt = ? WHERE id = ?", systemPrompt, sessionID)
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
		row := db.QueryRow("SELECT id, name, default_system_prompt, created_at FROM workspaces WHERE id = ?", workspaceID)
		err := row.Scan(&workspace.ID, &workspace.Name, &workspace.DefaultSystemPrompt, &workspace.CreatedAt)
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
		query = "SELECT id, last_updated_at, name, workspace_id, roots FROM sessions WHERE workspace_id = '' ORDER BY last_updated_at DESC"
	} else {
		query = "SELECT id, last_updated_at, name, workspace_id, roots FROM sessions WHERE workspace_id = ? ORDER BY last_updated_at DESC"
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
		var rootsJSON string
		if err := rows.Scan(&s.ID, &s.LastUpdated, &s.Name, &s.WorkspaceID, &rootsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		if err := json.Unmarshal([]byte(rootsJSON), &s.Roots); err != nil {
			log.Printf("Warning: Failed to unmarshal session roots for session %s: %v", s.ID, err)
			s.Roots = []string{}
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

func SaveOAuthToken(db *sql.DB, tokenJSON string, userEmail string, projectID string) error {
	_, err := db.Exec(
		"INSERT OR REPLACE INTO oauth_tokens (id, token_data, user_email, project_id) VALUES (1, ?, ?, ?)",
		tokenJSON, userEmail, projectID)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	return nil
}

func LoadOAuthToken(db *sql.DB) (string, string, string, error) {
	var tokenJSON string
	var nullUserEmail sql.NullString
	var nullProjectID sql.NullString
	row := db.QueryRow("SELECT token_data, user_email, project_id FROM oauth_tokens WHERE id = 1")
	err := row.Scan(&tokenJSON, &nullUserEmail, &nullProjectID)
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
	var rootsJSON string
	row := db.QueryRow("SELECT id, last_updated_at, system_prompt, name, workspace_id, COALESCE(primary_branch_id, ''), COALESCE(roots, '[]') FROM sessions WHERE id = ?", sessionID)
	err := row.Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID, &s.PrimaryBranchID, &rootsJSON)
	if err != nil {
		return s, err
	}

	if err := json.Unmarshal([]byte(rootsJSON), &s.Roots); err != nil {
		return s, fmt.Errorf("failed to unmarshal session roots: %w", err)
	}

	return s, nil
}

// UpdateSessionRoots updates the roots for a session.
func UpdateSessionRoots(db *sql.DB, sessionID string, roots []string) error {
	rootsJSON, err := json.Marshal(roots)
	if err != nil {
		return fmt.Errorf("failed to marshal roots: %w", err)
	}

	_, err = db.Exec("UPDATE sessions SET roots = ? WHERE id = ?", string(rootsJSON), sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session roots: %w", err)
	}
	return nil
}

// DeleteSession deletes a session and all its associated messages.
func DeleteSession(db *sql.DB, sessionID string) error {
	// Start a transaction to ensure atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// Delete messages associated with the session
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete messages for session %s: %w", sessionID, err)
	}

	// Delete shell commands associated with the session
	_, err = tx.Exec("DELETE FROM shell_commands WHERE branch_id IN (SELECT id FROM branches WHERE session_id = ?)", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete shell commands for session %s: %w", sessionID, err)
	}

	// Delete the session itself
	_, err = tx.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
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

// SaveBlob saves a blob to the blobs table. If a blob with the same hash already exists, it replaces it.
// It returns the SHA-512/256 hash of the data.
func SaveBlob(ctx context.Context, db DbOrTx, data []byte) (string, error) {
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

// GlobalPrompt struct to hold global prompt data
type PredefinedPrompt struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SaveGlobalPrompts saves a list of global prompts to the database.
// It deletes all existing global prompts and then inserts the new ones.
func SaveGlobalPrompts(db *sql.DB, prompts []PredefinedPrompt) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// Validate prompts for uniqueness and non-empty labels
	seenLabels := make(map[string]bool)
	for _, p := range prompts {
		if p.Label == "" {
			return fmt.Errorf("prompt label cannot be empty")
		}
		if seenLabels[p.Label] {
			return fmt.Errorf("duplicate prompt label found: %s", p.Label)
		}
		seenLabels[p.Label] = true
	}

	// Clear existing global prompts
	_, err = tx.Exec("DELETE FROM global_prompts")
	if err != nil {
		return fmt.Errorf("failed to clear existing global prompts: %w", err)
	}

	// Insert new global prompts
	stmt, err := tx.Prepare("INSERT INTO global_prompts (label, value) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement for global prompts: %w", err)
	}
	defer stmt.Close()

	for _, p := range prompts {
		_, err := stmt.Exec(p.Label, p.Value)
		if err != nil {
			return fmt.Errorf("failed to insert global prompt %s: %w", p.Label, err)
		}
	}

	return tx.Commit()
}

// GetGlobalPrompts retrieves all global prompts from the database.
func GetGlobalPrompts(db *sql.DB) ([]PredefinedPrompt, error) {

	rows, err := db.Query("SELECT label, value FROM global_prompts ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query global prompts: %w", err)
	}
	defer rows.Close()

	var prompts []PredefinedPrompt
	for rows.Next() {
		var p PredefinedPrompt
		if err := rows.Scan(&p.Label, &p.Value); err != nil {
			return nil, fmt.Errorf("failed to scan global prompt: %w", err)
		}
		prompts = append(prompts, p)
	}

	if prompts == nil {
		return []PredefinedPrompt{}, nil // Return empty slice instead of nil
	}
	return prompts, nil
}

const CSRFKeyName = "csrf_key"

// GetAppConfig retrieves a configuration value from the app_configs table.
func GetAppConfig(db *sql.DB, key string) ([]byte, error) {
	var value []byte
	err := db.QueryRow("SELECT value FROM app_configs WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Key not found, not an error
		}
		return nil, fmt.Errorf("failed to get app config for key %s: %w", key, err)
	}
	return value, nil
}

// SetAppConfig saves a configuration value to the app_configs table.
func SetAppConfig(db *sql.DB, key string, value []byte) error {
	_, err := db.Exec("INSERT OR REPLACE INTO app_configs (key, value) VALUES (?, ?)", key, value)
	if err != nil {
		return fmt.Errorf("failed to set app config for key %s: %w", key, err)
	}
	return nil
}

// ShellCommand struct to hold shell command data
type ShellCommand struct {
	ID            string
	BranchID      string
	Command       string
	Status        string
	StartTime     int64         // Unix timestamp
	EndTime       sql.NullInt64 // Unix timestamp, nullable
	Stdout        []byte
	Stderr        []byte
	ExitCode      sql.NullInt64  // Nullable
	ErrorMessage  sql.NullString // Nullable
	LastPolledAt  int64          // Unix timestamp
	NextPollDelay int64          // Seconds
	StdoutOffset  int64          // New: Last read offset for stdout
	StderrOffset  int64          // New: Last read offset for stderr
}

// InsertShellCommand inserts a new shell command into the database.
func InsertShellCommand(db DbOrTx, cmd ShellCommand) error {
	_, err := db.Exec(`
		INSERT INTO shell_commands (
			id, branch_id, command, status, start_time, last_polled_at, next_poll_delay, stdout_offset, stderr_offset
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cmd.ID, cmd.BranchID, cmd.Command, cmd.Status, cmd.StartTime, cmd.LastPolledAt, cmd.NextPollDelay, cmd.StdoutOffset, cmd.StderrOffset)
	if err != nil {
		return fmt.Errorf("failed to insert shell command: %w", err)
	}
	return nil
}

// UpdateShellCommand updates the status and results of a shell command in the database.
func UpdateShellCommand(db DbOrTx, cmd ShellCommand) error {
	_, err := db.Exec(`
		UPDATE shell_commands SET
			status = ?, end_time = ?, stdout = ?, stderr = ?, exit_code = ?, error_message = ?, last_polled_at = ?, next_poll_delay = ?, stdout_offset = ?, stderr_offset = ?
		WHERE id = ?`,
		cmd.Status, cmd.EndTime, cmd.Stdout, cmd.Stderr, cmd.ExitCode, cmd.ErrorMessage, cmd.LastPolledAt, cmd.NextPollDelay, cmd.StdoutOffset, cmd.StderrOffset, cmd.ID)
	if err != nil {
		return fmt.Errorf("failed to update shell command: %w", err)
	}
	return nil
}

// GetShellCommandByID retrieves a shell command by its ID.
func GetShellCommandByID(db DbOrTx, id string) (*ShellCommand, error) {
	var cmd ShellCommand
	row := db.QueryRow(`
		SELECT id, branch_id, command, status, start_time, end_time, stdout, stderr,
			exit_code, error_message, last_polled_at, next_poll_delay, stdout_offset, stderr_offset
		FROM shell_commands WHERE id = ?`, id)
	err := row.Scan(
		&cmd.ID, &cmd.BranchID, &cmd.Command, &cmd.Status, &cmd.StartTime, &cmd.EndTime,
		&cmd.Stdout, &cmd.Stderr, &cmd.ExitCode, &cmd.ErrorMessage, &cmd.LastPolledAt,
		&cmd.NextPollDelay, &cmd.StdoutOffset, &cmd.StderrOffset)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("shell command with ID %s not found", id)
		}
		return nil, fmt.Errorf("failed to get shell command by ID %s: %w", id, err)
	}
	return &cmd, nil
}

// GetAllRunningShellCommands retrieves all shell commands that are currently running.
func GetAllRunningShellCommands(db DbOrTx) ([]ShellCommand, error) {
	rows, err := db.Query(`
		SELECT id, branch_id, command, status, start_time, end_time, stdout, stderr,
			exit_code, error_message, last_polled_at, next_poll_delay, stdout_offset, stderr_offset
		FROM shell_commands WHERE status = 'running'
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get all running shell commands: %w", err)
	}
	defer rows.Close()

	var commands []ShellCommand
	for rows.Next() {
		var cmd ShellCommand
		if err := rows.Scan(
			&cmd.ID, &cmd.BranchID, &cmd.Command, &cmd.Status, &cmd.StartTime, &cmd.EndTime,
			&cmd.Stdout, &cmd.Stderr, &cmd.ExitCode, &cmd.ErrorMessage, &cmd.LastPolledAt,
			&cmd.NextPollDelay, &cmd.StdoutOffset, &cmd.StderrOffset); err != nil {
			return nil, fmt.Errorf("failed to scan shell command: %w", err)
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

// DeleteShellCommand deletes a shell command by its ID.
func DeleteShellCommand(db DbOrTx, id string) error {
	_, err := db.Exec("DELETE FROM shell_commands WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete shell command with ID %s: %w", id, err)
	}
	return nil
}
