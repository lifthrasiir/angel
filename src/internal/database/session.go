package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"

	"github.com/lifthrasiir/angel/filesystem"
	. "github.com/lifthrasiir/angel/internal/types"
)

// WorkspaceWithSessions struct to hold workspace with its sessions
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

// CreateSession creates a new session in the database.
func CreateSession(db *sql.DB, sessionID string, systemPrompt string, workspaceID string) (string, error) {
	primaryBranchID := GenerateID() // Generate a new ID for the primary branch
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

// UpdateSessionLastUpdated updates the last_updated_at timestamp for a session.
func UpdateSessionLastUpdated(db *sql.DB, sessionID string) error {
	_, err := db.Exec("UPDATE sessions SET last_updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session last_updated_at: %w", err)
	}
	return nil
}

// UpdateSessionName updates the name of a session.
func UpdateSessionName(db *sql.DB, sessionID string, name string) error {
	_, err := db.Exec("UPDATE sessions SET name = ? WHERE id = ?", name, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session name: %w", err)
	}
	return nil
}

// UpdateSessionWorkspace updates the workspace ID of a session.
func UpdateSessionWorkspace(db *sql.DB, sessionID string, workspaceID string) error {
	_, err := db.Exec("UPDATE sessions SET workspace_id = ? WHERE id = ?", workspaceID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session workspace: %w", err)
	}
	return nil
}

// GetWorkspaceAndSessions retrieves a workspace and all its sessions.
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

// SessionExists checks if a session with the given ID exists.
func SessionExists(db *sql.DB, sessionID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ?)", sessionID).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check session existence: %w", err)
	}
	return exists, nil
}

// GetSession retrieves a session by its ID.
func GetSession(db *sql.DB, sessionID string) (Session, error) {
	var s Session
	row := db.QueryRow("SELECT id, last_updated_at, system_prompt, name, workspace_id, COALESCE(primary_branch_id, '') FROM sessions WHERE id = ?", sessionID)
	err := row.Scan(&s.ID, &s.LastUpdated, &s.SystemPrompt, &s.Name, &s.WorkspaceID, &s.PrimaryBranchID)
	if err != nil {
		return s, err
	}
	return s, nil
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
		if err := filesystem.DestroySessionFS(id); err != nil {
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
