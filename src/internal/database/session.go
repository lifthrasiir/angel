package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/lifthrasiir/angel/internal/types"
)

// sessionMeta implements common methods for SessionDatabase and SessionTx.
type sessionMeta struct {
	id          string
	attachAlias string // Alias for the attached session DB (e.g., "session_db_1234567890")
}

// SessionId returns the full session ID associated with this SessionDatabase.
func (meta sessionMeta) SessionId() string { return meta.id }

// LocalSessionId returns the local session ID that should be stored in session-specific tables.
func (meta sessionMeta) LocalSessionId() string { return ToLocalSessionID(meta.id) }

// rewriteQuery rewrites queries to replace a pseudo-schema `S.` (for the current session) with the attached database alias.
// For example, "SELECT * FROM S.sessions" becomes "SELECT * FROM session_db_1234567890.sessions".
// If no attach alias is set (main DB queries), it removes the `S.` prefix.
// Since this is a simple string replacement, any query should NOT use a bare string that may contain `S.`.
func (meta sessionMeta) rewriteQuery(query string) string {
	if meta.attachAlias != "" {
		return strings.ReplaceAll(query, "S.", meta.attachAlias+".")
	}
	return strings.ReplaceAll(query, "S.", "")
}

// SessionDatabase is a wrapper around the main database connection for session-specific operations.
// It holds a reference to an attached session database via the AttachPool.
type SessionDatabase struct {
	*Database
	sessionMeta
	cleanup func() // Cleanup function to release the attach when done
}

// SessionTx is a wrapper around sql.Tx for session-specific operations within a transaction.
type SessionTx struct {
	*sql.Tx
	sessionMeta
}

// WithSession returns a SessionDatabase for a specific session ID. It should be `Close`d after use.
// This attaches the session database to the main connection and returns a SessionDatabase that
// queries the attached session DB when using the `S.` prefix.
func (db *Database) WithSession(sessionId string) (*SessionDatabase, error) {
	// Extract main session ID to determine which session DB file to use
	mainSessionID, _ := SplitSessionId(sessionId)

	// Check if session exists in main DB first
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ?)", sessionId).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		return nil, MakeNotFoundError("session not found: %s", sessionId)
	}

	// Get the session DB path
	sessionDBPath, err := GetSessionDBPath(db.ctx, mainSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session DB path: %w", err)
	}

	// Check if file exists (skip for in-memory databases)
	if sessionDBPath != ":memory:" {
		if _, err := os.Stat(sessionDBPath); os.IsNotExist(err) {
			return nil, MakeNotFoundError("session DB does not exist: %s", sessionDBPath)
		}
	}

	// Attach the session database to the main connection
	alias, cleanup, err := db.attachPool.Acquire(sessionDBPath, mainSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to attach session DB: %w", err)
	}

	meta := sessionMeta{id: sessionId, attachAlias: alias}
	return &SessionDatabase{Database: db, sessionMeta: meta, cleanup: cleanup}, nil
}

// CreateSessionDB creates a new session database file for the given main session ID.
// If the database file already exists, this is a no-op.
func (db *Database) CreateSessionDB(mainSessionID string) error {
	// Get session DB path
	sessionDBPath, err := GetSessionDBPath(db.ctx, mainSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session DB path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(sessionDBPath); err == nil {
		// File already exists, nothing to do
		return nil
	}

	// Ensure directory exists
	dbDir := filepath.Dir(sessionDBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create session DB directory %s: %w", dbDir, err)
	}

	// Attach the session database (SQLite will create the file if it doesn't exist)
	// Use skipWait=true because we're creating a new file that the watcher doesn't know about yet
	alias, cleanup, err := db.attachPool.acquireInternal(sessionDBPath, mainSessionID, true)
	if err != nil {
		return fmt.Errorf("failed to attach session DB for schema creation: %w", err)
	}
	defer cleanup()

	// Create SessionDatabase to use rewriteQuery
	meta := sessionMeta{id: mainSessionID, attachAlias: alias}
	sdb := &SessionDatabase{Database: db, sessionMeta: meta}

	// Create schema using S. prefix (rewriteQuery will convert it to the actual alias)
	_, err = sdb.Exec(createSessionSchemaSQL)
	if err != nil {
		return fmt.Errorf("failed to create session schema: %w", err)
	}

	// Mark the new file as tracked so subsequent Attach waits don't block
	if db.watcher != nil {
		db.watcher.TrackNewFile(mainSessionID)
	}

	log.Printf("Database: Created new session DB: %s", sessionDBPath)
	return nil
}

// WithSessionDB attaches an existing session database and returns a SessionDatabase.
// This is a lower-level alternative to WithSession that skips the file existence check.
func (db *Database) WithSessionDB(mainSessionID string) (*SessionDatabase, error) {
	// Get the session DB path
	sessionDBPath, err := GetSessionDBPath(db.ctx, mainSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session DB path: %w", err)
	}

	// Attach the session database to the main connection
	alias, cleanup, err := db.attachPool.Acquire(sessionDBPath, mainSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to attach session DB: %w", err)
	}

	meta := sessionMeta{id: mainSessionID, attachAlias: alias}
	return &SessionDatabase{Database: db, sessionMeta: meta, cleanup: cleanup}, nil
}

// WithSuffix returns a SessionDatabase for a session ID with the given suffix appended.
// It shares the same attach alias and cleanup with the parent (only the parent should call Close).
func (db *SessionDatabase) WithSuffix(suffix string) *SessionDatabase {
	if !strings.HasPrefix(suffix, ".") {
		panic("suffix must start with a dot")
	}
	meta := sessionMeta{id: db.sessionMeta.id + suffix, attachAlias: db.sessionMeta.attachAlias}
	return &SessionDatabase{Database: db.Database, sessionMeta: meta, cleanup: nil} // No cleanup for suffix
}

func (db *SessionDatabase) Begin() (*SessionTx, error) {
	tx, err := db.Database.Begin()
	if err != nil {
		return nil, err
	}
	return &SessionTx{Tx: tx, sessionMeta: db.sessionMeta}, nil
}

func (db *SessionDatabase) BeginTx(ctx context.Context, opts *sql.TxOptions) (*SessionTx, error) {
	tx, err := db.Database.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &SessionTx{Tx: tx, sessionMeta: db.sessionMeta}, nil
}

func (db *SessionDatabase) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.Database.Exec(db.rewriteQuery(query), args...)
}
func (db *SessionDatabase) Prepare(query string) (*sql.Stmt, error) {
	return db.Database.Prepare(db.rewriteQuery(query))
}
func (db *SessionDatabase) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.Database.Query(db.rewriteQuery(query), args...)
}
func (db *SessionDatabase) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.Database.QueryRow(db.rewriteQuery(query), args...)
}
func (db *SessionDatabase) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.Database.ExecContext(ctx, db.rewriteQuery(query), args...)
}
func (db *SessionDatabase) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return db.Database.PrepareContext(ctx, db.rewriteQuery(query))
}
func (db *SessionDatabase) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return db.Database.QueryContext(ctx, db.rewriteQuery(query), args...)
}
func (db *SessionDatabase) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return db.Database.QueryRowContext(ctx, db.rewriteQuery(query), args...)
}

func (tx *SessionTx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.Tx.Exec(tx.rewriteQuery(query), args...)
}
func (tx *SessionTx) Prepare(query string) (*sql.Stmt, error) {
	return tx.Tx.Prepare(tx.rewriteQuery(query))
}
func (tx *SessionTx) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return tx.Tx.Query(tx.rewriteQuery(query), args...)
}
func (tx *SessionTx) QueryRow(query string, args ...interface{}) *sql.Row {
	return tx.Tx.QueryRow(tx.rewriteQuery(query), args...)
}
func (tx *SessionTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, tx.rewriteQuery(query), args...)
}
func (tx *SessionTx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return tx.Tx.PrepareContext(ctx, tx.rewriteQuery(query))
}
func (tx *SessionTx) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, tx.rewriteQuery(query), args...)
}
func (tx *SessionTx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, tx.rewriteQuery(query), args...)
}

func (db *SessionDatabase) Close() error {
	// Call the cleanup function to release the attach
	if db.cleanup != nil {
		db.cleanup()
		db.cleanup = nil
	}
	return nil
}

// WorkspaceWithSessions struct to hold workspace with its sessions
type WorkspaceWithSessions struct {
	Workspace Workspace `json:"workspace"`
	Sessions  []Session `json:"sessions"`
}

// CreateWorkspace creates a new workspace in the database.
func CreateWorkspace(db *Database, workspaceID string, name string, defaultSystemPrompt string) error {
	_, err := db.Exec("INSERT INTO workspaces (id, name, default_system_prompt) VALUES (?, ?, ?)", workspaceID, name, defaultSystemPrompt)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	return nil
}

// GetWorkspace retrieves a single workspace by its ID.
func GetWorkspace(db *Database, workspaceID string) (Workspace, error) {
	var w Workspace
	err := db.QueryRow("SELECT id, name, default_system_prompt, created_at FROM workspaces WHERE id = ?", workspaceID).Scan(&w.ID, &w.Name, &w.DefaultSystemPrompt, &w.CreatedAt)
	if err != nil {
		return w, fmt.Errorf("failed to get workspace: %w", err)
	}
	return w, nil
}

// GetAllWorkspaces retrieves all workspaces from the database.
func GetAllWorkspaces(db *Database) ([]Workspace, error) {
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
func DeleteWorkspace(db *Database, workspaceID string) error {
	// Get all session IDs in the workspace first (before transaction)
	rows, err := db.Query("SELECT id FROM sessions WHERE workspace_id = ?", workspaceID)
	if err != nil {
		return fmt.Errorf("failed to query sessions for workspace %s: %w", workspaceID, err)
	}
	defer rows.Close()

	var sessionIDs []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return fmt.Errorf("failed to scan session ID: %w", err)
		}
		sessionIDs = append(sessionIDs, sessionID)
	}

	// Delete session DB files for each session
	for _, sessionID := range sessionIDs {
		mainSessionID, _ := SplitSessionId(sessionID)
		sessionDBPath, err := GetSessionDBPathFromDB(db, mainSessionID)
		if err != nil {
			// Session DB path not found, skip
			continue
		}

		// Force detach from AttachPool before deleting the file
		if err := db.attachPool.ForceDetachByMainSessionID(mainSessionID); err != nil {
			log.Printf("Warning: Failed to detach session %s before deletion: %v", mainSessionID, err)
		}

		// Delete the session DB file (skip for in-memory databases)
		if sessionDBPath != ":memory:" {
			os.Remove(sessionDBPath)
		}
	}

	// Start a transaction to ensure atomicity for main DB operations
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// Delete search index entries for these sessions
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// Delete FTS index entries first (before deleting from messages_searchable)
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM message_stems WHERE rowid IN (SELECT id FROM messages_searchable WHERE session_id IN (%s))
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		log.Printf("Warning: Failed to delete message_stems for workspace %s: %v", workspaceID, err)
	}

	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM message_trigrams WHERE rowid IN (SELECT id FROM messages_searchable WHERE session_id IN (%s))
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		log.Printf("Warning: Failed to delete message_trigrams for workspace %s: %v", workspaceID, err)
	}

	// Delete from messages_searchable last
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM messages_searchable WHERE session_id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("failed to delete search index for workspace %s: %w", workspaceID, err)
	}

	// Delete sessions associated with the workspace from main DB
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

// CreateSession creates a new session in both the main database and the session database.
// Returns the SessionDatabase (already attached) and the primary branch ID.
func CreateSession(db *Database, sessionID string, systemPrompt string, workspaceID string) (*SessionDatabase, string, error) {
	primaryBranchID := GenerateID() // Generate a new ID for the primary branch

	// Insert into main DB's sessions table
	_, err := db.Exec("INSERT INTO sessions (id, system_prompt, name, workspace_id, primary_branch_id) VALUES (?, ?, ?, ?, ?)",
		sessionID, systemPrompt, "", workspaceID, primaryBranchID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create session in main DB: %w", err)
	}

	// Create the session database file explicitly
	mainSessionID, _ := SplitSessionId(sessionID)
	if err := db.CreateSessionDB(mainSessionID); err != nil {
		return nil, "", fmt.Errorf("failed to create session DB: %w", err)
	}

	// Attach the session database to perform initial setup
	sdb, err := db.WithSession(sessionID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to attach session DB: %w", err)
	}

	// Use LOCAL session ID for session DB storage
	localSessionID := ToLocalSessionID(sessionID)

	// Insert into session DB's sessions table for consistency
	_, err = sdb.Exec("INSERT OR REPLACE INTO S.sessions (id, system_prompt, name, workspace_id, primary_branch_id) VALUES (?, ?, ?, ?, ?)",
		localSessionID, systemPrompt, "", workspaceID, primaryBranchID)
	if err != nil {
		sdb.Close()
		return nil, "", fmt.Errorf("failed to create session in session DB: %w", err)
	}

	// Create the initial branch entry in the session DB's branches table
	_, err = sdb.Exec("INSERT INTO S.branches (id, session_id, parent_branch_id, branch_from_message_id) VALUES (?, ?, NULL, NULL)", primaryBranchID, localSessionID)
	if err != nil {
		sdb.Close()
		return nil, "", fmt.Errorf("failed to create initial branch for session: %w", err)
	}

	// Return the already-attached SessionDatabase
	return sdb, primaryBranchID, nil
}

// UpdateSessionLastUpdated updates the last_updated_at timestamp for a session.
func UpdateSessionLastUpdated(db *SessionDatabase) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update main DB sessions table
	_, err = tx.Exec("UPDATE sessions SET last_updated_at = CURRENT_TIMESTAMP WHERE id = ?", db.SessionId())
	if err != nil {
		return fmt.Errorf("failed to update main DB session last_updated_at: %w", err)
	}

	// Update session DB sessions table
	_, err = tx.Exec("UPDATE S.sessions SET last_updated_at = CURRENT_TIMESTAMP WHERE id = ?", db.LocalSessionId())
	if err != nil {
		return fmt.Errorf("failed to update session DB session last_updated_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateSessionName updates the name of a session.
func UpdateSessionName(db *SessionDatabase, name string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update main DB sessions table
	_, err = tx.Exec("UPDATE sessions SET name = ? WHERE id = ?", name, db.SessionId())
	if err != nil {
		return fmt.Errorf("failed to update main DB session name: %w", err)
	}

	// Update session DB sessions table
	_, err = tx.Exec("UPDATE S.sessions SET name = ? WHERE id = ?", name, db.LocalSessionId())
	if err != nil {
		return fmt.Errorf("failed to update session DB session name: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateSessionWorkspace updates the workspace ID of a session.
func UpdateSessionWorkspace(db *SessionDatabase, workspaceID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update main DB sessions table
	_, err = tx.Exec("UPDATE sessions SET workspace_id = ? WHERE id = ?", workspaceID, db.SessionId())
	if err != nil {
		return fmt.Errorf("failed to update main DB session workspace: %w", err)
	}

	// Update session DB sessions table
	_, err = tx.Exec("UPDATE S.sessions SET workspace_id = ? WHERE id = ?", workspaceID, db.LocalSessionId())
	if err != nil {
		return fmt.Errorf("failed to update session DB session workspace: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetWorkspaceAndSessions retrieves a workspace and all its sessions.
func GetWorkspaceAndSessions(db *Database, workspaceID string) (*WorkspaceWithSessions, error) {
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
	var placeholder string
	var args []interface{}
	if workspaceID == "" {
		placeholder = "''"
	} else {
		placeholder = "?"
		args = append(args, workspaceID)
	}
	rows, err := db.Query(
		fmt.Sprintf(`
			SELECT id, last_updated_at, name, workspace_id FROM sessions
			WHERE workspace_id = %s AND id NOT LIKE '%%.%%' ORDER BY last_updated_at DESC`, placeholder),
		args...)
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

// SessionExists checks if a session with the given ID exists.
// In split-db architecture, this queries the main DB's sessions table (not the session DB).
func SessionExists(db *SessionDatabase) (bool, error) {
	var exists bool
	// Use db.Database directly to query main DB, bypassing S. prefix rewriting
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ?)", db.SessionId()).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check session existence: %w", err)
	}
	return exists, nil
}

// GetSession retrieves a session by its ID.
// In split-db architecture, this queries the main DB's sessions table (not the session DB).
func GetSession(db *SessionDatabase) (Session, error) {
	var s Session
	row := db.QueryRow(
		"SELECT id, last_updated_at, system_prompt, name, workspace_id, COALESCE(primary_branch_id, '') FROM sessions WHERE id = ?",
		db.SessionId())
	err := row.Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID, &s.PrimaryBranchID)
	if err != nil {
		return s, err
	}
	return s, nil
}

// DeleteSession deletes a session and all its associated data.
// In split-db architecture, this deletes the session DB file and main DB entries.
func DeleteSession(db *Database, sessionID string, sandboxBaseDir string) error {
	// Get the main session ID to detach from AttachPool
	mainSessionID, _ := SplitSessionId(sessionID)

	// Force detach from AttachPool before deleting
	if err := db.attachPool.ForceDetachByMainSessionID(mainSessionID); err != nil {
		log.Printf("Warning: Failed to detach session %s before deletion: %v", mainSessionID, err)
	}

	// Start a transaction to ensure atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// Delete FTS index entries first
	_, err = tx.Exec("DELETE FROM message_stems WHERE rowid IN (SELECT id FROM messages_searchable WHERE session_id = ? OR session_id LIKE ? || '.%')", sessionID, sessionID)
	if err != nil {
		log.Printf("Warning: Failed to delete message_stems for session %s: %v", sessionID, err)
	}

	_, err = tx.Exec("DELETE FROM message_trigrams WHERE rowid IN (SELECT id FROM messages_searchable WHERE session_id = ? OR session_id LIKE ? || '.%')", sessionID, sessionID)
	if err != nil {
		log.Printf("Warning: Failed to delete message_trigrams for session %s: %v", sessionID, err)
	}

	// Delete from messages_searchable
	_, err = tx.Exec("DELETE FROM messages_searchable WHERE session_id = ? OR session_id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete search index for session %s: %w", sessionID, err)
	}

	// Delete the session entry and all its sub-sessions from main DB
	_, err = tx.Exec("DELETE FROM sessions WHERE id = ? OR id LIKE ? || '.%'", sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session %s and its sub-sessions: %w", sessionID, err)
	}

	// Get all session IDs (main and sub-sessions) for cleanup
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

	// Commit the transaction first
	if err := tx.Commit(); err != nil {
		return err
	}

	// Delete the session DB file after successful commit
	sessionDBPath, err := GetSessionDBPathFromDB(db, mainSessionID)
	if err != nil {
		log.Printf("Warning: Failed to get session DB path for deletion: %v", err)
	} else if sessionDBPath != ":memory:" {
		// Only delete if it's a real file (not :memory:)
		if err := os.Remove(sessionDBPath); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: Failed to delete session DB file %s: %v", sessionDBPath, err)
		}
	}

	// Destroy the session's file system sandbox directories for all identified sessions
	for _, id := range sessionIDsToDelete {
		sessionDir := filepath.Join(sandboxBaseDir, id)
		if err := os.RemoveAll(sessionDir); err != nil {
			log.Printf("Warning: Failed to destroy session FS for %s: %v", id, err)
		}
	}

	return nil
}

// GenerateID generates a random ID for sessions and branches
func GenerateID() string {
	for {
		b := make([]byte, 6) // 6 bytes will result in an 8-character base64 string
		if _, err := rand.Read(b); err != nil {
			log.Panicf("Error generating random ID: %v", err)
		}
		id := base64.RawURLEncoding.EncodeToString(b)
		// Check if the ID contains any uppercase letters
		hasUppercase := false
		for _, c := range id {
			if c >= 'A' && c <= 'Z' {
				hasUppercase = true
				break
			}
		}
		// If it has uppercase letters, return it
		if hasUppercase {
			return id
		}
		// Otherwise, try again (very unlikely to happen multiple times)
	}
}

// CleanupOldTemporarySessions deletes temporary sessions older than the specified duration
// In split-db architecture, this uses DeleteSession to handle each session's cleanup.
func CleanupOldTemporarySessions(db *Database, olderThan time.Duration, sandboxBaseDir string) error {
	// Find old temporary sessions
	cutoffTime := time.Now().Add(-olderThan)
	rows, err := db.Query(`
		SELECT id FROM sessions
		WHERE id LIKE '.%' AND last_updated_at < ?
		ORDER BY last_updated_at ASC
	`, cutoffTime.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to query old temporary sessions: %w", err)
	}
	defer rows.Close()

	var sessionIDsToDelete []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("failed to scan session ID: %w", err)
		}
		sessionIDsToDelete = append(sessionIDsToDelete, id)
	}

	if len(sessionIDsToDelete) == 0 {
		return nil
	}

	log.Printf("Cleaning up %d old temporary sessions", len(sessionIDsToDelete))

	// Delete each session using DeleteSession
	successCount := 0
	for _, sessionID := range sessionIDsToDelete {
		if err := DeleteSession(db, sessionID, sandboxBaseDir); err != nil {
			log.Printf("Warning: Failed to delete temporary session %s: %v", sessionID, err)
		} else {
			successCount++
		}
	}

	log.Printf("Successfully cleaned up %d/%d old temporary sessions", successCount, len(sessionIDsToDelete))
	return nil
}

// GetSessionsWithDetails retrieves sessions with additional details including first message date and last message preview.
// Filters by workspace if workspaceID is provided.
func GetSessionsWithDetails(db *Database, workspaceID string) ([]SessionWithDetails, error) {
	// Build the WHERE clause based on workspace filter
	// Always filter by workspace_id (empty string for anonymous workspace)
	// Include temporary sessions (starting with '.') but exclude sub-sessions (have '.' not at start)
	whereClause := "WHERE workspace_id = ? AND SUBSTR(id, 2) NOT LIKE '%.%'"
	args := []interface{}{workspaceID}

	// Query sessions from main DB
	rows, err := db.Query(`
		SELECT id, created_at, last_updated_at, name, workspace_id FROM sessions
		`+whereClause+` ORDER BY last_updated_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionWithDetails
	for rows.Next() {
		var s SessionWithDetails
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.LastUpdatedAt, &s.Name, &s.WorkspaceID); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		return []SessionWithDetails{}, nil
	}

	// For each session, attach to session DB and query for details
	for i := range sessions {
		// Use a closure to ensure the session DB is closed immediately after each iteration
		func() {
			mainSessionID, _ := SplitSessionId(sessions[i].ID)

			sdb, err := db.WithSessionDB(mainSessionID)
			if err != nil {
				// Session DB might not exist, skip this session
				return
			}
			defer sdb.Close()

			// Query for first user message date
			localSessionID := ToLocalSessionID(sessions[i].ID)
			var firstMessageAt sql.NullString
			err = sdb.QueryRow(`
				SELECT MIN(created_at) FROM S.messages
				WHERE session_id = ? AND type = ?
			`, localSessionID, TypeUserText).Scan(&firstMessageAt)
			if err == nil && firstMessageAt.Valid {
				sessions[i].FirstMessageAt = firstMessageAt.String
			}

			// Query for last user message text (preview)
			var lastMessageText sql.NullString
			err = sdb.QueryRow(`
				SELECT text FROM S.messages
				WHERE session_id = ? AND type = ?
				ORDER BY created_at DESC LIMIT 1
			`, localSessionID, TypeUserText).Scan(&lastMessageText)
			if err == nil && lastMessageText.Valid {
				// Truncate to ~100 characters (rune-aware for UTF-8)
				text := lastMessageText.String
				runes := []rune(text)
				if len(runes) > 100 {
					text = string(runes[:97]) + "..."
				}
				sessions[i].LastMessageText = text
			}
		}()
	}

	return sessions, nil
}
