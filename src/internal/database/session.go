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
	id string
}

// SessionId returns the full session ID associated with this SessionDatabase.
func (meta sessionMeta) SessionId() string { return meta.id }

// LocalSessionId returns the local session ID that should be stored in session-specific tables.
func (meta sessionMeta) LocalSessionId() string { return meta.id }

// rewriteQuery rewrites queries to replace a pseudo-schema `S.` (for the current session) with the correct SQL syntax.
// Since this is a simple string replacement, any query should NOT use a bare string that may contain `S.`.
func (meta sessionMeta) rewriteQuery(query string) string {
	return strings.ReplaceAll(query, "S.", "") // TODO: Use "`sess:<quoted sessionId>`." instead
}

// SessionDatabase is a wrapper around the main database connection for session-specific operations.
type SessionDatabase struct {
	*Database
	sessionMeta
}

// SessionTx is a wrapper around sql.Tx for session-specific operations within a transaction.
type SessionTx struct {
	*sql.Tx
	sessionMeta
}

// WithSession returns a SessionDatabase for a specific session ID. It should be `Close`d after use.
func (db *Database) WithSession(sessionId string) (*SessionDatabase, error) {
	meta := sessionMeta{id: sessionId}
	return &SessionDatabase{Database: db, sessionMeta: meta}, nil
}

// WithSuffix returns a SessionDatabase for a session ID with the given suffix appended.
func (db *SessionDatabase) WithSuffix(suffix string) *SessionDatabase {
	if !strings.HasPrefix(suffix, ".") {
		panic("suffix must start with a dot")
	}
	meta := sessionMeta{id: db.sessionMeta.id + suffix}
	return &SessionDatabase{Database: db.Database, sessionMeta: meta}
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
	// For now does nothing, but will eventually do something after per-session DB connections are implemented
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

// CreateSession creates a new session in both the main database and the session database.
// Returns the SessionDatabase (already attached) and the primary branch ID.
func CreateSession(db *Database, sessionID string, systemPrompt string, workspaceID string) (*SessionDatabase, string, error) {
	primaryBranchID := GenerateID() // Generate a new ID for the primary branch
	_, err := db.Exec(
		"INSERT INTO sessions (id, system_prompt, name, workspace_id, primary_branch_id) VALUES (?, ?, ?, ?, ?)",
		sessionID, systemPrompt, "", workspaceID, primaryBranchID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create session: %w", err)
	}

	// Also create the initial branch entry in the branches table
	_, err = db.Exec(
		"INSERT INTO branches (id, session_id, parent_branch_id, branch_from_message_id) VALUES (?, ?, NULL, NULL)",
		primaryBranchID, sessionID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create initial branch for session: %w", err)
	}

	sdb, err := db.WithSession(sessionID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get session database: %w", err)
	}
	return sdb, primaryBranchID, nil
}

// UpdateSessionLastUpdated updates the last_updated_at timestamp for a session.
func UpdateSessionLastUpdated(db *SessionDatabase) error {
	_, err := db.Exec("UPDATE S.sessions SET last_updated_at = CURRENT_TIMESTAMP WHERE id = ?", db.LocalSessionId())
	if err != nil {
		return fmt.Errorf("failed to update session last_updated_at: %w", err)
	}
	return nil
}

// UpdateSessionName updates the name of a session.
func UpdateSessionName(db *SessionDatabase, name string) error {
	_, err := db.Exec("UPDATE S.sessions SET name = ? WHERE id = ?", name, db.LocalSessionId())
	if err != nil {
		return fmt.Errorf("failed to update session name: %w", err)
	}
	return nil
}

// UpdateSessionWorkspace updates the workspace ID of a session.
func UpdateSessionWorkspace(db *SessionDatabase, workspaceID string) error {
	_, err := db.Exec("UPDATE S.sessions SET workspace_id = ? WHERE id = ?", workspaceID, db.LocalSessionId())
	if err != nil {
		return fmt.Errorf("failed to update session workspace: %w", err)
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

// DeleteSession deletes a session and all its associated messages, branches, shell commands, and session environments.
func DeleteSession(db *Database, sessionID string, sandboxBaseDir string) error {
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
		sessionDir := filepath.Join(sandboxBaseDir, id)
		if err := os.RemoveAll(sessionDir); err != nil {
			log.Printf("Warning: Failed to destroy session FS for %s: %v", id, err)
		}
	}

	return tx.Commit()
}

// GenerateID generates a random ID for sessions and branches
func GenerateID() string {
	for {
		b := make([]byte, 8) // 8 bytes will result in an 11-character base64 string
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
		// If no uppercase letters, return the ID
		if !hasUppercase {
			return id
		}
		// Otherwise, try again (very unlikely to happen multiple times)
	}
}

// CleanupOldTemporarySessions deletes temporary sessions older than the specified duration
func CleanupOldTemporarySessions(db *Database, olderThan time.Duration, sandboxBaseDir string) error {
	// Start a transaction to ensure atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Find old temporary sessions
	cutoffTime := time.Now().Add(-olderThan)
	rows, err := tx.Query(`
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

	// Delete messages associated with the sessions
	placeholders := make([]string, len(sessionIDsToDelete))
	args := make([]interface{}, len(sessionIDsToDelete))
	for i, id := range sessionIDsToDelete {
		placeholders[i] = "?"
		args[i] = id
	}

	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM messages WHERE session_id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("failed to delete messages for temporary sessions: %w", err)
	}

	// Delete shell commands associated with the sessions
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM shell_commands WHERE branch_id IN (
			SELECT id FROM branches WHERE session_id IN (%s)
		)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("failed to delete shell commands for temporary sessions: %w", err)
	}

	// Delete session environments associated with the sessions
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM session_envs WHERE session_id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("failed to delete session environments for temporary sessions: %w", err)
	}

	// Delete branches associated with the sessions
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM branches WHERE session_id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("failed to delete branches for temporary sessions: %w", err)
	}

	// Delete the sessions themselves
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM sessions WHERE id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("failed to delete temporary sessions: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit cleanup transaction: %w", err)
	}

	// Destroy the session's file system sandbox directories
	for _, id := range sessionIDsToDelete {
		sessionDir := filepath.Join(sandboxBaseDir, id)
		if err := os.RemoveAll(sessionDir); err != nil {
			log.Printf("Warning: Failed to destroy session FS for %s: %v", id, err)
		}
	}

	log.Printf("Successfully cleaned up %d old temporary sessions", len(sessionIDsToDelete))
	return nil
}
