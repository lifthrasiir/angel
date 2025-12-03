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
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/lifthrasiir/angel/fs"
)

var (
	walCheckpointTicker *time.Ticker
	walCheckpointMutex  sync.Mutex
)

func init() {
	sqlite_vec.Auto()
}

// StartWALCheckpointManager starts a background goroutine to periodically run WAL checkpoints
func StartWALCheckpointManager(db *sql.DB) {
	walCheckpointMutex.Lock()
	defer walCheckpointMutex.Unlock()

	// Stop existing ticker if running
	if walCheckpointTicker != nil {
		walCheckpointTicker.Stop()
	}

	// Create ticker to run checkpoint every 10 minutes
	walCheckpointTicker = time.NewTicker(10 * time.Minute)

	go func() {
		defer walCheckpointTicker.Stop()
		for range walCheckpointTicker.C {
			if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
				log.Printf("WAL checkpoint failed: %v", err)
			}
		}
	}()
}

// StopWALCheckpointManager stops the WAL checkpoint ticker
func StopWALCheckpointManager() {
	walCheckpointMutex.Lock()
	defer walCheckpointMutex.Unlock()

	if walCheckpointTicker != nil {
		walCheckpointTicker.Stop()
		walCheckpointTicker = nil
		log.Println("WAL checkpoint manager stopped")
	}
}

// PerformWALCheckpoint manually triggers a WAL checkpoint
func PerformWALCheckpoint(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("failed to perform WAL checkpoint: %w", err)
	}
	log.Println("Manual WAL checkpoint completed successfully")
	return nil
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
		aux TEXT NOT NULL DEFAULT '',   -- Angel-internal metadata that doesn't go to the LLM provider
		indexed INTEGER DEFAULT 0 NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_messages_attachments ON messages(attachments);

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

	CREATE TRIGGER IF NOT EXISTS increment_blob_refs
		AFTER INSERT ON messages
		WHEN NEW.attachments IS NOT NULL AND NEW.attachments != '[]'
	BEGIN
		UPDATE blobs SET ref_count = ref_count + 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(NEW.attachments)
			WHERE json_extract(json_each.value, '$.hash') IS NOT NULL
		);
	END;

	CREATE TRIGGER IF NOT EXISTS decrement_blob_refs
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
	END;

	CREATE TRIGGER IF NOT EXISTS update_blob_refs
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
	END;

	CREATE TABLE IF NOT EXISTS global_prompts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL UNIQUE,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS app_configs (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL
	);

	CREATE TABLE IF NOT EXISTS openai_configs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		endpoint TEXT NOT NULL,
		api_key TEXT,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS gemini_api_configs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		api_key TEXT,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		last_used_by_model TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

	CREATE VIEW IF NOT EXISTS messages_searchable AS
		SELECT id, replace(replace(text, '<', '\x0e'), '>', '\x0f') as text, session_id,
			(SELECT workspace_id FROM sessions WHERE sessions.id = messages.session_id) as workspace_id
		FROM messages WHERE type IN ('user', 'model');

	CREATE VIRTUAL TABLE IF NOT EXISTS message_stems USING fts5(
		text,
		session_id UNINDEXED,
		workspace_id UNINDEXED,
		content='messages_searchable',
		content_rowid='id',
		tokenize='porter unicode61 remove_diacritics 1'
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS message_trigrams USING fts5(
		text,
		session_id UNINDEXED,
		workspace_id UNINDEXED,
		content='messages_searchable',
		content_rowid='id',
		tokenize='trigram remove_diacritics 1'
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
	// Migration for SI/SO HTML tag handling in FTS and trigger cleanup
	migrationStmts := []string{
		// Drop old FTS triggers if they exist
		`DROP TRIGGER IF EXISTS message_search_insert`,
		`DROP TRIGGER IF EXISTS message_search_update`,
		`DROP TRIGGER IF EXISTS message_search_delete`,

		// Drop and recreate the view to trigger FTS rebuild
		`DROP VIEW IF EXISTS messages_searchable`,

		// Recreate the view with SI/SO conversion
		`CREATE VIEW messages_searchable AS
			SELECT id, replace(replace(text, '<', '\x0e'), '>', '\x0f') as text, session_id,
			(SELECT workspace_id FROM sessions WHERE sessions.id = messages.session_id) as workspace_id
			FROM messages WHERE type IN ('user', 'model')`,

		// Add missing index for blob ref_count - critical for performance when deleting sessions with many blobs
		`CREATE INDEX IF NOT EXISTS idx_blobs_ref_count ON blobs(ref_count)`,
	}

	for _, stmt := range migrationStmts {
		_, err := db.Exec(stmt)
		if err != nil {
			return fmt.Errorf("failed to execute migration statement '%s': %w", stmt, err)
		}
	}

	return nil
}

// syncFTSOnStartup syncs FTS tables with messages that might be missing due to crashes during streaming
func syncFTSOnStartup(db *sql.DB) error {
	log.Println("Checking for FTS desync on startup...")

	// Count missing entries in message_stems
	var missingStems int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM messages_searchable
		WHERE id NOT IN (SELECT rowid FROM message_stems)
	`).Scan(&missingStems)
	if err != nil {
		return fmt.Errorf("failed to count missing FTS stems entries: %w", err)
	}

	// Count missing entries in message_trigrams
	var missingTrigrams int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM messages_searchable
		WHERE id NOT IN (SELECT rowid FROM message_trigrams)
	`).Scan(&missingTrigrams)
	if err != nil {
		return fmt.Errorf("failed to count missing FTS trigrams entries: %w", err)
	}

	if missingStems > 0 || missingTrigrams > 0 {
		log.Printf("Found %d missing stems and %d missing trigrams, syncing...", missingStems, missingTrigrams)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin FTS sync transaction: %w", err)
		}
		defer tx.Rollback()

		// Sync missing stems entries
		if missingStems > 0 {
			_, err = tx.Exec(`
				INSERT INTO message_stems(rowid)
				SELECT id FROM messages_searchable
				WHERE id NOT IN (SELECT rowid FROM message_stems)
			`)
			if err != nil {
				return fmt.Errorf("failed to sync missing stems entries: %w", err)
			}
		}

		// Sync missing trigrams entries
		if missingTrigrams > 0 {
			_, err = tx.Exec(`
				INSERT INTO message_trigrams(rowid)
				SELECT id FROM messages_searchable
				WHERE id NOT IN (SELECT rowid FROM message_trigrams)
			`)
			if err != nil {
				return fmt.Errorf("failed to sync missing trigrams entries: %w", err)
			}
		}

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit FTS sync: %w", err)
		}

		log.Printf("FTS sync completed successfully")
	} else {
		log.Println("FTS is already in sync")
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
	db.SetConnMaxLifetime(30 * time.Minute) // Should be far longer than typical test runs

	// SQLite performance and concurrency optimizations
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=30000",
		"PRAGMA cache_size=-65536",
		"PRAGMA mmap_size=268435456",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA foreign_keys=ON",
	}

	for _, pragma := range pragmas {
		if _, err = db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute pragma '%s': %w", pragma, err)
		}
	}

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

	// Sync FTS tables in case of desync from crashes
	if err = syncFTSOnStartup(db); err != nil {
		log.Printf("Warning: FTS sync failed: %v", err)
		// Continue even if sync fails, FTS will be updated on next message completion
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

func UpdateSessionWorkspace(db *sql.DB, sessionID string, workspaceID string) error {
	_, err := db.Exec("UPDATE sessions SET workspace_id = ? WHERE id = ?", workspaceID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session workspace: %w", err)
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

// getHighestOAuthTokenID returns the highest existing ID in oauth_tokens table
// Returns 0 if table is empty
func getHighestOAuthTokenID(db *sql.DB) (int, error) {
	var highestID int
	err := db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM oauth_tokens").Scan(&highestID)
	if err != nil {
		return 0, fmt.Errorf("failed to get highest OAuth token ID: %w", err)
	}
	return highestID, nil
}

func SaveOAuthToken(db *sql.DB, tokenJSON string, userEmail string, projectID string) error {
	// Get the highest existing ID, or use 1 if table is empty
	highestID, err := getHighestOAuthTokenID(db)
	if err != nil {
		return err
	}
	if highestID == 0 {
		highestID = 1
	}

	_, err = db.Exec(
		"INSERT OR REPLACE INTO oauth_tokens (id, token_data, user_email, project_id) VALUES (?, ?, ?, ?)",
		highestID, tokenJSON, userEmail, projectID)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	return nil
}

func LoadOAuthToken(db *sql.DB) (string, string, string, error) {
	// Get the highest existing ID, or return empty if table is empty
	highestID, err := getHighestOAuthTokenID(db)
	if err != nil {
		return "", "", "", err
	}
	if highestID == 0 {
		log.Println("LoadOAuthToken: No existing token found in DB.")
		return "", "", "", nil // No token found, not an error
	}

	var tokenJSON string
	var nullUserEmail sql.NullString
	var nullProjectID sql.NullString
	row := db.QueryRow("SELECT token_data, user_email, project_id FROM oauth_tokens WHERE id = ?", highestID)
	err = row.Scan(&tokenJSON, &nullUserEmail, &nullProjectID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to load OAuth token: %w", err)
	}
	userEmail := nullUserEmail.String
	projectID := nullProjectID.String

	return tokenJSON, userEmail, projectID, nil
}

// DeleteOAuthToken deletes the OAuth token from the database.
func DeleteOAuthToken(db *sql.DB) error {
	// Get the highest existing ID, or return if table is empty
	highestID, err := getHighestOAuthTokenID(db)
	if err != nil {
		return err
	}
	if highestID == 0 {
		return nil // Table is empty, nothing to delete
	}

	_, err = db.Exec("DELETE FROM oauth_tokens WHERE id = ?", highestID)
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

// OpenAIConfig struct to hold OpenAI-compatible API configuration data
type OpenAIConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	APIKey    string `json:"api_key"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// GeminiAPIConfig represents a Gemini API configuration
type GeminiAPIConfig struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	APIKey          string               `json:"api_key"`
	Enabled         bool                 `json:"enabled"`
	LastUsedByModel map[string]time.Time `json:"last_used_by_model"`
	CreatedAt       string               `json:"created_at"`
	UpdatedAt       string               `json:"updated_at"`
}

// SearchResult represents a single search result
type SearchResult struct {
	MessageID   int    `json:"message_id"`
	SessionID   string `json:"session_id"`
	Excerpt     string `json:"excerpt"`
	Type        string `json:"type"`
	CreatedAt   string `json:"created_at"`
	SessionName string `json:"session_name"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// SearchMessages searches for messages matching the query using FTS5 tables
func SearchMessages(db *sql.DB, query string, maxID int, limit int, workspaceID string) ([]SearchResult, bool, error) {
	// Validate query
	if strings.TrimSpace(query) == "" {
		return nil, false, fmt.Errorf("search query cannot be empty")
	}

	// Set default limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100 // Cap at 100 for performance
	}

	// Build search query with snippet directly from FTS tables
	// Use source_order to prefer stems over trigrams when both match the same message
	searchSubQueryFormat := `
		SELECT
			rowid as id,
			session_id,
			workspace_id,
			-- Convert SI/SO back to HTML tags, then escape for safe display
			replace(
				replace(
					replace(
						snippet(%s, 0, '<mark>', '</mark>', '...', 64),
						'&', '&amp;'
					),
					'\x0e', '&lt;'
				),
				'\x0f', '&gt;'
			) as excerpt,
			%d as source_order -- Add source order to identify which table this came from (stems=0, trigrams=1)
		FROM %[1]s WHERE %[1]s MATCH ?
	`
	args := []interface{}{query, query}
	if workspaceID != "" {
		searchSubQueryFormat += " AND workspace_id = ?"
		args = []interface{}{query, workspaceID, query, workspaceID}
	}

	// Combine results from both tables, preferring stems (order 0) over trigrams (order 1) for duplicates
	searchSubQuery := `
		SELECT id, session_id, workspace_id, excerpt
		FROM (
			SELECT * FROM (` + fmt.Sprintf(searchSubQueryFormat, "message_stems", 0) + `)
			UNION ALL
			SELECT * FROM (` + fmt.Sprintf(searchSubQueryFormat, "message_trigrams", 1) + `)
		)
		GROUP BY id
		ORDER BY MIN(source_order)
	`

	// Build final query joining with messages and sessions
	baseQuery := `
		SELECT DISTINCT
			m.id,
			m.session_id,
			fts.excerpt,
			m.type,
			m.created_at,
			COALESCE(s.name, '') as session_name,
			fts.workspace_id
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		JOIN (` + searchSubQuery + `) fts ON m.id = fts.id
	`

	// Add max_id filter for pagination (get messages older than max_id)
	if maxID > 0 {
		baseQuery += " WHERE m.id < ?"
		args = append(args, maxID)
	}

	// Order by message ID (descending for newest first) and limit
	baseQuery += " ORDER BY m.id DESC LIMIT ?"
	args = append(args, limit+1) // Request one more to check if there are more results

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed to search messages: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var result SearchResult
		err := rows.Scan(
			&result.MessageID,
			&result.SessionID,
			&result.Excerpt,
			&result.Type,
			&result.CreatedAt,
			&result.SessionName,
			&result.WorkspaceID,
		)
		if err != nil {
			return nil, false, fmt.Errorf("failed to scan search result: %w", err)
		}
		results = append(results, result)
	}

	// Check if there are more results
	hasMore := len(results) > limit
	if hasMore {
		// Remove the extra result we used for checking
		results = results[:limit]
	}

	return results, hasMore, nil
}

// SaveOpenAIConfig saves an OpenAI configuration to the database.
func SaveOpenAIConfig(db *sql.DB, config OpenAIConfig) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO openai_configs (id, name, endpoint, api_key, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, config.ID, config.Name, config.Endpoint, config.APIKey, config.Enabled)
	if err != nil {
		return fmt.Errorf("failed to save OpenAI config: %w", err)
	}
	return nil
}

// GetOpenAIConfigs retrieves all OpenAI configurations from the database.
func GetOpenAIConfigs(db *sql.DB) ([]OpenAIConfig, error) {
	rows, err := db.Query("SELECT id, name, endpoint, api_key, enabled, created_at, updated_at FROM openai_configs ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query OpenAI configs: %w", err)
	}
	defer rows.Close()

	var configs []OpenAIConfig
	for rows.Next() {
		var config OpenAIConfig
		err := rows.Scan(&config.ID, &config.Name, &config.Endpoint, &config.APIKey, &config.Enabled, &config.CreatedAt, &config.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OpenAI config: %w", err)
		}
		configs = append(configs, config)
	}
	if configs == nil {
		return []OpenAIConfig{}, nil
	}
	return configs, nil
}

// MarshalTimeMap converts a map of time.Time to JSON (uses default serialization)
func MarshalTimeMap(m map[string]time.Time) ([]byte, error) {
	return json.Marshal(m)
}

// UnmarshalTimeMap converts JSON to a map of time.Time
func UnmarshalTimeMap(data string) (map[string]time.Time, error) {
	var result map[string]time.Time
	if data == "" || data == "null" {
		return make(map[string]time.Time), nil
	}

	err := json.Unmarshal([]byte(data), &result)
	if err != nil {
		log.Printf("Failed to unmarshal time map: %v, data: %s", err, data)
		return make(map[string]time.Time), nil
	}
	return result, nil
}

// GetGeminiAPIConfigs retrieves all Gemini API configurations from the database.
func GetGeminiAPIConfigs(db *sql.DB) ([]GeminiAPIConfig, error) {
	rows, err := db.Query("SELECT id, name, api_key, enabled, last_used_by_model, created_at, updated_at FROM gemini_api_configs ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query Gemini API configs: %w", err)
	}
	defer rows.Close()

	configs := []GeminiAPIConfig{}
	for rows.Next() {
		var config GeminiAPIConfig
		var lastUsedByModelStr sql.NullString

		err := rows.Scan(&config.ID, &config.Name, &config.APIKey, &config.Enabled, &lastUsedByModelStr, &config.CreatedAt, &config.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan Gemini API config: %w", err)
		}

		// JSON field deserialization - handle NULL
		var jsonStr string
		if lastUsedByModelStr.Valid {
			jsonStr = lastUsedByModelStr.String
		} else {
			jsonStr = "{}"
		}
		config.LastUsedByModel, err = UnmarshalTimeMap(jsonStr)
		if err != nil {
			log.Printf("Failed to unmarshal last_used_by_model for config %s: %v", config.ID, err)
			config.LastUsedByModel = make(map[string]time.Time)
		}

		configs = append(configs, config)
	}

	return configs, nil
}

// GetNextGeminiAPIConfig selects the next Gemini API configuration to use based on the least recently used strategy for a specific model.
func GetNextGeminiAPIConfig(db *sql.DB, modelName string) (selectedConfig *GeminiAPIConfig, err error) {
	configs, err := GetGeminiAPIConfigs(db)
	if err != nil {
		return
	}

	oldestTime := time.Now()

	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		// Get the last_used time for the specific model
		lastUsed := config.LastUsedByModel[modelName]

		// Select the oldest one
		if lastUsed.Before(oldestTime) {
			oldestTime = lastUsed
			selectedConfig = &config
		}
	}

	return
}

// UpdateModelLastUsed updates the last used time for a specific model in the Gemini API configuration.
func UpdateModelLastUsed(db *sql.DB, id, modelName string) error {
	config, err := GetGeminiAPIConfig(db, id)
	if err != nil {
		return err
	}

	// Initialize and update the map
	if config.LastUsedByModel == nil {
		config.LastUsedByModel = make(map[string]time.Time)
	}
	config.LastUsedByModel[modelName] = time.Now()

	// Convert to JSON (time.Time automatically serializes to RFC3339)
	lastUsedJSON, err := MarshalTimeMap(config.LastUsedByModel)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		UPDATE gemini_api_configs
		SET last_used_by_model = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, string(lastUsedJSON), id)

	return err
}

// HandleModelRateLimit handles rate limiting for a specific model by updating its last used time with a future timestamp.
func HandleModelRateLimit(db *sql.DB, id, modelName string, retryAfter time.Duration) error {
	// Current time + retryAfter + buffer (30 seconds)
	futureTime := time.Now().Add(retryAfter).Add(30 * time.Second)

	// Get the current configuration
	config, err := GetGeminiAPIConfig(db, id)
	if err != nil {
		return err
	}

	// Initialize and update the map
	if config.LastUsedByModel == nil {
		config.LastUsedByModel = make(map[string]time.Time)
	}
	config.LastUsedByModel[modelName] = futureTime

	// Convert to JSON
	lastUsedJSON, err := MarshalTimeMap(config.LastUsedByModel)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		UPDATE gemini_api_configs
		SET last_used_by_model = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, string(lastUsedJSON), id)

	return err
}

// SaveGeminiAPIConfig saves a Gemini API configuration to the database.
func SaveGeminiAPIConfig(db *sql.DB, config GeminiAPIConfig) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO gemini_api_configs (id, name, api_key, enabled, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, config.ID, config.Name, config.APIKey, config.Enabled)
	if err != nil {
		return fmt.Errorf("failed to save Gemini API config: %w", err)
	}
	return nil
}

// GetGeminiAPIConfig retrieves a single Gemini API configuration by its ID.
func GetGeminiAPIConfig(db *sql.DB, id string) (*GeminiAPIConfig, error) {
	var config GeminiAPIConfig
	var lastUsedByModelStr sql.NullString
	err := db.QueryRow("SELECT id, name, api_key, enabled, last_used_by_model, created_at, updated_at FROM gemini_api_configs WHERE id = ?", id).
		Scan(&config.ID, &config.Name, &config.APIKey, &config.Enabled, &lastUsedByModelStr, &config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			//lint:ignore ST1005 Gemini is a proper noun
			return nil, fmt.Errorf("Gemini API config with id %s not found", id)
		}
		return nil, fmt.Errorf("failed to get Gemini API config: %w", err)
	}

	// JSON field deserialization - handle NULL
	var jsonStr string
	if lastUsedByModelStr.Valid {
		jsonStr = lastUsedByModelStr.String
	} else {
		jsonStr = "{}"
	}
	config.LastUsedByModel, err = UnmarshalTimeMap(jsonStr)
	if err != nil {
		log.Printf("Failed to unmarshal last_used_by_model for config %s: %v", config.ID, err)
		config.LastUsedByModel = make(map[string]time.Time)
	}

	return &config, nil
}

// DeleteGeminiAPIConfig deletes a Gemini API configuration from the database.
func DeleteGeminiAPIConfig(db *sql.DB, id string) error {
	result, err := db.Exec("DELETE FROM gemini_api_configs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete Gemini API config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		//lint:ignore ST1005 Gemini is a proper noun
		return fmt.Errorf("Gemini API config with id %s not found", id)
	}
	return nil
}

// GetOpenAIConfig retrieves a single OpenAI configuration by its ID.
func GetOpenAIConfig(db *sql.DB, id string) (*OpenAIConfig, error) {
	var config OpenAIConfig
	err := db.QueryRow("SELECT id, name, endpoint, api_key, enabled, created_at, updated_at FROM openai_configs WHERE id = ?", id).
		Scan(&config.ID, &config.Name, &config.Endpoint, &config.APIKey, &config.Enabled, &config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("OpenAI config with id %s not found", id)
		}
		return nil, fmt.Errorf("failed to get OpenAI config: %w", err)
	}
	return &config, nil
}

// DeleteOpenAIConfig deletes an OpenAI configuration from the database.
func DeleteOpenAIConfig(db *sql.DB, id string) error {
	result, err := db.Exec("DELETE FROM openai_configs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete OpenAI config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("OpenAI config with id %s not found", id)
	}
	return nil
}
