package main

import (
	"context"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/lifthrasiir/angel/fs"
)

func init() {
	sqlite_vec.Auto()
}

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
		chosen_first_id INTEGER -- Virtual root message pointer
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
		branch_id TEXT NOT NULL,
		parent_message_id INTEGER,
		chosen_next_id INTEGER,
		text TEXT NOT NULL,
		type TEXT NOT NULL,
		attachments TEXT, -- This will store JSON array of blob hashes
		cumul_token_count INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		model TEXT NOT NULL,
		generation INTEGER DEFAULT 0,
		state TEXT NOT NULL DEFAULT '', -- Opaque state that has to be relayed to the LLM provider
		aux TEXT NOT NULL DEFAULT ''    -- Angel-internal metadata that doesn't go to the LLM provider
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
		data BLOB NOT NULL,
		ref_count INTEGER DEFAULT 1 NOT NULL
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

	CREATE TABLE IF NOT EXISTS session_envs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		roots TEXT NOT NULL, -- JSON array of strings
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(session_id, generation),
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
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
	// Add new columns for generation tracking if they don't exist
	// SQLite's ALTER TABLE ADD COLUMN does not support IF NOT EXISTS directly.
	// We will attempt to add and log if it fails, assuming it's due to column existence.
	migrationStmts := []string{
		// Add chosen_first_id column to sessions table for first message editing
		"ALTER TABLE sessions ADD COLUMN chosen_first_id INTEGER",
		// Set chosen_first_id for existing sessions to their first message (where parent_message_id IS NULL)
		"UPDATE sessions SET chosen_first_id = (SELECT MIN(id) FROM messages WHERE session_id = sessions.id AND parent_message_id IS NULL) WHERE chosen_first_id IS NULL",

		// Skip generated column approach - it's causing compatibility issues
		// Focus on trigger-based solution which is more reliable

		// Create expression index for attachments JSON (compatible syntax)
		"CREATE INDEX IF NOT EXISTS idx_messages_attachments ON messages(attachments)",

		// Add ref_count column to blobs table
		"ALTER TABLE blobs ADD COLUMN ref_count INTEGER DEFAULT 1 NOT NULL",

		// Add indexed column to messages table for search indexing tracking
		"ALTER TABLE messages ADD COLUMN indexed INTEGER DEFAULT 0 NOT NULL",

		// Create triggers for automatic blob reference counting using json_each
		`CREATE TRIGGER IF NOT EXISTS increment_blob_refs
			AFTER INSERT ON messages
			WHEN NEW.attachments IS NOT NULL AND NEW.attachments != '[]'
		BEGIN
			UPDATE blobs SET ref_count = ref_count + 1
			WHERE id IN (
				SELECT json_extract(json_each.value, '$.hash')
				FROM json_each(NEW.attachments)
				WHERE json_extract(json_each.value, '$.hash') IS NOT NULL
			);
		END`,

		`CREATE TRIGGER IF NOT EXISTS decrement_blob_refs
			AFTER DELETE ON messages
			WHEN OLD.attachments IS NOT NULL AND OLD.attachments != '[]'
		BEGIN
			UPDATE blobs SET ref_count = ref_count - 1
			WHERE id IN (
				SELECT json_extract(json_each.value, '$.hash')
				FROM json_each(OLD.attachments)
				WHERE json_extract(json_each.value, '$.hash') IS NOT NULL
			);
			DELETE FROM blobs WHERE ref_count <= 0;
		END`,

		`CREATE TRIGGER IF NOT EXISTS update_blob_refs
			AFTER UPDATE ON messages
			WHEN NEW.attachments IS NOT NULL OR OLD.attachments IS NOT NULL
		BEGIN
			-- Decrease ref count for old attachments
			UPDATE blobs SET ref_count = ref_count - 1
			WHERE id IN (
				SELECT json_extract(json_each.value, '$.hash')
				FROM json_each(OLD.attachments)
				WHERE OLD.attachments IS NOT NULL AND OLD.attachments != '[]'
				AND json_extract(json_each.value, '$.hash') IS NOT NULL
			);

			-- Increase ref count for new attachments
			UPDATE blobs SET ref_count = ref_count + 1
			WHERE id IN (
				SELECT json_extract(json_each.value, '$.hash')
				FROM json_each(NEW.attachments)
				WHERE NEW.attachments IS NOT NULL AND NEW.attachments != '[]'
				AND json_extract(json_each.value, '$.hash') IS NOT NULL
			);

			-- Clean up blobs with ref_count <= 0
			DELETE FROM blobs WHERE ref_count <= 0;
		END`,
	}

	for _, stmt := range migrationStmts {
		_, err := db.Exec(stmt)
		if err != nil {
			// Check if the error is "duplicate column name" or similar
			// For SQLite, this typically means the column already exists.
			if strings.Contains(err.Error(), "duplicate column name") || strings.Contains(err.Error(), "already exists") {
				log.Printf("Column might already exist, skipping alter table: %s", stmt)
				continue
			}
			return fmt.Errorf("failed to execute migration statement '%s': %w", stmt, err)
		}
	}

	return nil
}

// initializeSearchTables creates search tables and migrates existing data.
// This function should only be called once when search tables are first created.
func initializeSearchTables(db *sql.DB) error {
	// Check if messages_searchable already exists
	var exists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0 FROM sqlite_master
		WHERE type = 'view' AND name = 'messages_searchable'
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if messages_searchable exists: %w", err)
	}

	// If search tables already exist, skip initialization
	if exists {
		log.Println("Search tables already exist, skipping initialization")
		return nil
	}

	log.Println("Initializing search tables...")

	// Create messages searchable view for FTS5
	_, err = db.Exec(`
		CREATE VIEW messages_searchable AS
			SELECT id, text FROM messages WHERE type IN ('user', 'model')
	`)
	if err != nil {
		return fmt.Errorf("failed to create messages_searchable: %w", err)
	}

	// Create FTS5 search tables for messages
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE message_stems USING fts5(
			text,
			content='messages_searchable',
			content_rowid='id',
			tokenize='porter unicode61'
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create message_stems table: %w", err)
	}

	_, err = db.Exec(`
		CREATE VIRTUAL TABLE message_trigrams USING fts5(
			text,
			content='messages_searchable',
			content_rowid='id',
			tokenize='trigram'
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create message_trigrams table: %w", err)
	}

	// Populate search tables with existing data using rebuild
	log.Println("Populating search tables with existing data...")

	_, err = db.Exec(`INSERT INTO message_stems(message_stems) VALUES('rebuild')`)
	if err != nil {
		return fmt.Errorf("failed to populate message_stems: %w", err)
	}

	_, err = db.Exec(`INSERT INTO message_trigrams(message_trigrams) VALUES('rebuild')`)
	if err != nil {
		return fmt.Errorf("failed to populate message_trigrams: %w", err)
	}

	log.Println("Search tables initialization completed")

	// Create triggers to keep search tables in sync with source data
	// Message triggers - unified for both search tables
	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS message_search_insert
		AFTER INSERT ON messages
		WHEN NEW.type IN ('user', 'model')
		BEGIN
			INSERT INTO message_stems(rowid, text) VALUES (NEW.id, NEW.text);
			INSERT INTO message_trigrams(rowid, text) VALUES (NEW.id, NEW.text);
		END
	`)
	if err != nil {
		return fmt.Errorf("failed to create message_search_insert trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS message_search_update
		AFTER UPDATE ON messages
		WHEN OLD.text != NEW.text OR OLD.type != NEW.type
		BEGIN
			-- Remove old record if it existed in search
			DELETE FROM message_stems WHERE rowid = OLD.id;
			DELETE FROM message_trigrams WHERE rowid = OLD.id;

			-- Add new record if new type is searchable
			INSERT INTO message_stems(rowid, text) SELECT NEW.id, NEW.text WHERE NEW.type IN ('user', 'model');
			INSERT INTO message_trigrams(rowid, text) SELECT NEW.id, NEW.text WHERE NEW.type IN ('user', 'model');
		END
	`)
	if err != nil {
		return fmt.Errorf("failed to create message_search_update trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS message_search_delete
		AFTER DELETE ON messages
		BEGIN
			DELETE FROM message_stems WHERE rowid = OLD.id;
			DELETE FROM message_trigrams WHERE rowid = OLD.id;
		END
	`)
	if err != nil {
		return fmt.Errorf("failed to create message_search_delete trigger: %w", err)
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

	// Initialize search tables (one-time setup)
	if err = initializeSearchTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize search tables: %w", err)
	}

	log.Println("Database initialized and tables created.")
	return db, nil
}

// Session struct to hold session data
type Session struct {
	ID              string `json:"id"`
	LastUpdated     string `json:"last_updated_at"`
	SystemPrompt    string `json:"system_prompt"`
	Name            string `json:"name"`
	WorkspaceID     string `json:"workspace_id"`
	PrimaryBranchID string `json:"primary_branch_id"`
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

	// Delete session environments associated with the sessions in the workspace
	_, err = tx.Exec("DELETE FROM session_envs WHERE session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", workspaceID)
	if err != nil {
		return fmt.Errorf("failed to delete session environments for workspace %s: %w", workspaceID, err)
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
		query = "SELECT id, last_updated_at, name, workspace_id FROM sessions WHERE workspace_id = '' AND id NOT LIKE '%.%' ORDER BY last_updated_at DESC"
	} else {
		query = "SELECT id, last_updated_at, name, workspace_id FROM sessions WHERE workspace_id = ? AND id NOT LIKE '%.%' ORDER BY last_updated_at DESC"
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
		if err := rows.Scan(&s.ID, &s.LastUpdated, &s.Name, &s.WorkspaceID); err != nil {
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
	row := db.QueryRow("SELECT id, last_updated_at, system_prompt, name, workspace_id, COALESCE(primary_branch_id, '') FROM sessions WHERE id = ?", sessionID)
	err := row.Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID, &s.PrimaryBranchID)
	if err != nil {
		return s, err
	}
	return s, nil
}

// AddSessionEnv adds a new session environment entry.
// It automatically determines the next generation number for the session.
func AddSessionEnv(db DbOrTx, sessionID string, roots []string) (int, error) {
	rootsJSON, err := json.Marshal(roots)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal roots: %w", err)
	}

	// Get the latest generation for this session
	_, latestGeneration, err := GetLatestSessionEnv(db, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest session environment for generation calculation: %w", err)
	}

	newGeneration := latestGeneration + 1

	_, err = db.Exec("INSERT INTO session_envs (session_id, generation, roots) VALUES (?, ?, ?)", sessionID, newGeneration, string(rootsJSON))
	if err != nil {
		return 0, fmt.Errorf("failed to add session environment: %w", err)
	}
	return newGeneration, nil
}

// GetSessionEnv retrieves a session environment by session ID and generation.
func GetSessionEnv(db DbOrTx, sessionID string, generation int) ([]string, error) {
	var rootsJSON string
	err := db.QueryRow("SELECT roots FROM session_envs WHERE session_id = ? AND generation = ?", sessionID, generation).Scan(&rootsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			// If generation 0 is requested and not found, it means no initial environment was set.
			// In this specific case, we return empty roots as per the original intent for "empty env".
			if generation == 0 {
				return []string{}, nil
			}
			return nil, fmt.Errorf("session environment not found for session %s and generation %d", sessionID, generation)
		}
		return nil, fmt.Errorf("failed to get session environment: %w", err)
	}
	var roots []string
	if err := json.Unmarshal([]byte(rootsJSON), &roots); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session roots: %w", err)
	}
	return roots, nil
}

// GetLatestSessionEnv retrieves the latest session environment for a given session ID.
func GetLatestSessionEnv(db DbOrTx, sessionID string) ([]string, int, error) {
	var rootsJSON string
	var generation int
	err := db.QueryRow("SELECT roots, generation FROM session_envs WHERE session_id = ? ORDER BY generation DESC LIMIT 1", sessionID).Scan(&rootsJSON, &generation)
	if err != nil {
		if err == sql.ErrNoRows {
			return []string{}, 0, nil // No environment found, return empty roots and generation 0
		}
		return nil, 0, fmt.Errorf("failed to get latest session environment: %w", err)
	}
	var roots []string
	if err := json.Unmarshal([]byte(rootsJSON), &roots); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal latest session roots: %w", err)
	}
	return roots, generation, nil
}

// DeleteSession deletes a session and all its associated messages, branches, shell commands, and session environments.
func DeleteSession(db *sql.DB, sessionID string) error {
	// Start a transaction to ensure atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// Delete messages associated with the session
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ? OR session_id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete messages for session %s and its sub-sessions: %w", sessionID, err)
	}

	// Delete shell commands associated with the session
	_, err = tx.Exec("DELETE FROM shell_commands WHERE branch_id IN (SELECT id FROM branches WHERE session_id = ? OR session_id LIKE ? || '.%')", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete shell commands for session %s and its sub-sessions: %w", sessionID, err)
	}

	// Delete session environments associated with the session
	_, err = tx.Exec("DELETE FROM session_envs WHERE session_id = ? OR session_id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session environments for session %s and its sub-sessions: %w", sessionID, err)
	}

	// Delete branches associated with the session
	_, err = tx.Exec("DELETE FROM branches WHERE session_id = ? OR session_id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete branches for session %s and its sub-sessions: %w", sessionID, err)
	}

	// Delete the session itself and all its sub-sessions
	_, err = tx.Exec("DELETE FROM sessions WHERE id = ? OR id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session %s and its sub-sessions: %w", sessionID, err)
	}

	// Get all session IDs (main and sub-sessions) that will be deleted
	var sessionIDsToDelete []string
	rows, err := tx.Query("SELECT id FROM sessions WHERE id = ? OR id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to query session IDs for deletion: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("failed to scan session ID for deletion: %w", err)
		}
		sessionIDsToDelete = append(sessionIDsToDelete, id)
	}

	// Destroy the session's file system sandbox directories for all identified sessions
	for _, id := range sessionIDsToDelete {
		if err := fs.DestroySessionFS(id); err != nil {
			log.Printf("Warning: Failed to destroy session FS for %s: %v", id, err)
		}
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

// SaveBlob saves a blob to the blobs table. This function ensures the blob data exists.
// It sets ref_count = 0 for new blobs, and triggers will manage counting when messages are saved.
// It returns the SHA-512/256 hash of the data.
func SaveBlob(ctx context.Context, db DbOrTx, data []byte) (string, error) {
	hasher := sha512.New512_256()
	hasher.Write(data)
	hash := hasher.Sum(nil)
	hashStr := hex.EncodeToString(hash)

	// Insert blob with ref_count = 0 if it doesn't exist
	// Triggers will increment ref_count when the message is actually saved
	_, err := db.ExecContext(ctx, `
		INSERT INTO blobs (id, data, ref_count) VALUES (?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET
			data = excluded.data
	`, hashStr, data)
	if err != nil {
		return "", fmt.Errorf("failed to save blob: %w", err)
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

// GetBlobAsFileAttachment retrieves a blob and returns it as a FileAttachment with detected MIME type and appropriate filename.
func GetBlobAsFileAttachment(db *sql.DB, hash string) (FileAttachment, error) {
	// Get blob data
	blobData, err := GetBlob(db, hash)
	if err != nil {
		return FileAttachment{}, fmt.Errorf("failed to retrieve blob for hash %s: %w", hash, err)
	}

	// Determine MIME type by detecting content type
	mimeType := http.DetectContentType(blobData)

	// Generate filename with extension based on MIME type
	var filename string
	switch {
	case mimeType == "image/jpeg":
		filename = hash + ".jpg"
	case mimeType == "image/png":
		filename = hash + ".png"
	case mimeType == "image/gif":
		filename = hash + ".gif"
	case mimeType == "image/webp":
		filename = hash + ".webp"
	default:
		filename = hash // No extension if MIME type is not recognized
	}

	return FileAttachment{
		Hash:     hash,
		MimeType: mimeType,
		FileName: filename,
	}, nil
}

// InitializeBlobRefCounts initializes blob reference counts for existing data.
// This should be called once during migration to set up ref_count values correctly.
// After initialization, triggers will handle all reference counting automatically.
func InitializeBlobRefCounts(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for ref count initialization: %w", err)
	}
	defer tx.Rollback()

	// Initialize all blobs to ref_count = 0
	_, err = tx.Exec("UPDATE blobs SET ref_count = 0")
	if err != nil {
		return fmt.Errorf("failed to reset blob ref counts: %w", err)
	}

	// Count references from all existing messages
	_, err = tx.Exec(`
		UPDATE blobs SET ref_count = (
			SELECT COUNT(*) FROM (
				SELECT json_extract(json_each.value, '$.hash') as blob_hash
				FROM messages, json_each(messages.attachments)
				WHERE json_extract(json_each.value, '$.hash') = blobs.id
			)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to calculate blob reference counts: %w", err)
	}

	// Remove blobs with ref_count = 0 (unused blobs)
	result, err := tx.Exec("DELETE FROM blobs WHERE ref_count = 0")
	if err != nil {
		return fmt.Errorf("failed to delete unused blobs: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit ref count initialization: %w", err)
	}

	log.Printf("Blob reference count initialization completed: removed %d unused blob(s)", rowsAffected)
	return nil
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

// SetInitialSessionEnv sets the initial session environment (generation 0).
// This should only be called once for a new session.
func SetInitialSessionEnv(db DbOrTx, sessionID string, roots []string) error {
	rootsJSON, err := json.Marshal(roots)
	if err != nil {
		return fmt.Errorf("failed to marshal roots for initial environment: %w", err)
	}

	// Check if generation 0 already exists
	var existingRootsJSON string
	err = db.QueryRow("SELECT roots FROM session_envs WHERE session_id = ? AND generation = 0", sessionID).Scan(&existingRootsJSON)
	if err == nil {
		return fmt.Errorf("initial session environment (generation 0) already exists for session %s", sessionID)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check for existing initial session environment: %w", err)
	}

	_, err = db.Exec("INSERT INTO session_envs (session_id, generation, roots) VALUES (?, 0, ?)", sessionID, string(rootsJSON))
	if err != nil {
		return fmt.Errorf("failed to set initial session environment: %w", err)
	}
	return nil
}
