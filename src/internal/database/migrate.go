package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	. "github.com/lifthrasiir/angel/internal/types"
)

// MigrateToSplitDB migrates existing data from single DB to split DBs.
// This is the main entry point for the migration process.
//
// Process:
// 1. Create angel-data/sessions/ directory
// 2. Backup angel.db → angel.bak.db
// 3. Group sessions by main session ID
// 4. For each main session group:
//   - Create session DB file
//   - Copy session data with ID conversion
//   - Populate search index in main DB
//
// 5. Validate migration
func MigrateToSplitDB(ctx context.Context, mainDB *sql.DB, sessionDir string) error {
	log.Println("Starting migration to split-DB architecture...")

	// Step 1: Create session directory
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory %s: %w", sessionDir, err)
	}
	log.Printf("Created session directory: %s", sessionDir)

	// Step 2: Backup main database
	if err := backupMainDB(mainDB); err != nil {
		return fmt.Errorf("failed to backup main DB: %w", err)
	}
	log.Println("Main DB backup created: angel.bak.db")

	// Step 3: Group sessions by main session ID
	sessionGroups, err := GroupSessionsByMainSession(mainDB)
	if err != nil {
		return fmt.Errorf("failed to group sessions: %w", err)
	}
	log.Printf("Found %d main session groups to migrate", len(sessionGroups))

	// Step 4: Migrate each main session group
	for mainSessionID, sessionIDs := range sessionGroups {
		log.Printf("Migrating main session group: %s (%d sessions)", mainSessionID, len(sessionIDs))

		// Step 4a: Create session database
		sessionDB, err := CreateSessionDatabase(ctx, mainDB, mainSessionID, sessionIDs)
		if err != nil {
			return fmt.Errorf("failed to create session DB for %s: %w", mainSessionID, err)
		}

		// Step 4b: Copy session data
		if err := CopySessionData(mainDB, sessionDB, mainSessionID, sessionIDs); err != nil {
			sessionDB.Close()
			return fmt.Errorf("failed to copy session data for %s: %w", mainSessionID, err)
		}

		// Step 4c: Populate search index
		if err := PopulateSearchIndex(mainDB, sessionDB, sessionIDs); err != nil {
			sessionDB.Close()
			return fmt.Errorf("failed to populate search index for %s: %w", mainSessionID, err)
		}

		sessionDB.Close()
		log.Printf("Successfully migrated main session group: %s", mainSessionID)
	}

	// Step 5: Validate migration
	if err := ValidateMigration(ctx, mainDB); err != nil {
		return fmt.Errorf("migration validation failed: %w", err)
	}
	log.Println("Migration validation passed")

	log.Println("Migration to split-DB architecture completed successfully!")
	return nil
}

// GroupSessionsByMainSession groups session IDs by their main session ID.
// Returns a map where key is main session ID (e.g., 'foo') and value is list of
// related session IDs (e.g., ['foo', 'foo.bar', 'foo.bar.baz']).
//
// Main sessions: ID doesn't contain '.'
// Sub-sessions: ID contains '.'
func GroupSessionsByMainSession(mainDB *sql.DB) (map[string][]string, error) {
	// Query all session IDs from main DB
	rows, err := mainDB.Query("SELECT id FROM sessions ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var allSessionIDs []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, fmt.Errorf("failed to scan session ID: %w", err)
		}
		allSessionIDs = append(allSessionIDs, sessionID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	// Group by main session ID
	groups := make(map[string][]string)
	for _, sessionID := range allSessionIDs {
		mainSessionID, _ := SplitSessionId(sessionID)
		groups[mainSessionID] = append(groups[mainSessionID], sessionID)
	}

	return groups, nil
}

// CreateSessionDatabase creates a new session database file for a main session group.
// Returns the database connection.
func CreateSessionDatabase(ctx context.Context, mainDB *sql.DB, mainSessionID string, sessionIDs []string) (*sql.DB, error) {
	// Determine DB file path
	dbPath, err := GetSessionDBPath(ctx, mainSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session DB path: %w", err)
	}
	absPath := dbPath
	if !filepath.IsAbs(dbPath) {
		var err error
		absPath, err = filepath.Abs(dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for %s: %w", dbPath, err)
		}
	}

	// Check if file already exists
	if _, err := os.Stat(absPath); err == nil {
		log.Printf("Session DB already exists: %s (skipping creation)", absPath)
		// Open existing DB
		return InitSessionDBForMigration(absPath)
	}

	// Create new session DB with schema
	log.Printf("Creating session DB: %s", absPath)
	sessionDB, err := InitSessionDBForMigration(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session DB: %w", err)
	}

	return sessionDB, nil
}

// CopySessionData copies data from main DB to session DB for a group of sessions.
// Converts full session IDs to local IDs for session DB storage.
//
// Data copied:
// - sessions (with local IDs)
// - messages (with local session IDs)
// - branches (with local session IDs)
// - session_envs (with local session IDs)
// - shell_commands
// - blobs
func CopySessionData(mainDB *sql.DB, sessionDB *sql.DB, mainSessionID string, sessionIDs []string) error {
	log.Printf("Copying session data for %s (%d sessions)", mainSessionID, len(sessionIDs))

	// Begin transaction for session DB
	tx, err := sessionDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Copy sessions table
	if err := copySessions(mainDB, tx, sessionIDs); err != nil {
		return fmt.Errorf("failed to copy sessions: %w", err)
	}

	// Copy messages table
	if err := copyMessages(mainDB, tx, sessionIDs); err != nil {
		return fmt.Errorf("failed to copy messages: %w", err)
	}

	// Copy branches table
	if err := copyBranches(mainDB, tx, sessionIDs); err != nil {
		return fmt.Errorf("failed to copy branches: %w", err)
	}

	// Copy session_envs table
	if err := copySessionEnvs(mainDB, tx, sessionIDs); err != nil {
		return fmt.Errorf("failed to copy session_envs: %w", err)
	}

	// Copy shell_commands table
	if err := copyShellCommands(mainDB, tx, sessionIDs); err != nil {
		return fmt.Errorf("failed to copy shell_commands: %w", err)
	}

	// Copy blobs table (note: triggers will handle refcounts)
	if err := copyBlobs(mainDB, tx, sessionIDs); err != nil {
		return fmt.Errorf("failed to copy blobs: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// copySessions copies sessions from main DB to session DB, converting to local IDs.
func copySessions(mainDB *sql.DB, tx *sql.Tx, sessionIDs []string) error {
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO sessions (id, created_at, last_updated_at, system_prompt, name, workspace_id, primary_branch_id, chosen_first_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, sessionID := range sessionIDs {
		// Query session data from main DB
		var createdAT, lastUpdatedAt, systemPrompt, name, workspaceID, primaryBranchID sql.NullString
		var chosenFirstID sql.NullInt64

		err := mainDB.QueryRow(`
			SELECT created_at, last_updated_at, system_prompt, name, workspace_id, primary_branch_id, chosen_first_id
			FROM sessions WHERE id = ?
		`, sessionID).Scan(&createdAT, &lastUpdatedAt, &systemPrompt, &name, &workspaceID, &primaryBranchID, &chosenFirstID)
		if err != nil {
			return fmt.Errorf("failed to query session %s: %w", sessionID, err)
		}

		// Convert to local ID
		localID := ToLocalSessionID(sessionID)

		// Insert into session DB
		_, err = stmt.Exec(
			localID,
			nullString(createdAT),
			nullString(lastUpdatedAt),
			nullString(systemPrompt),
			nullString(name),
			nullString(workspaceID),
			nullString(primaryBranchID),
			nullInt64(chosenFirstID),
		)
		if err != nil {
			return fmt.Errorf("failed to insert session %s (local: %s): %w", sessionID, localID, err)
		}
	}

	return nil
}

// copyMessages copies messages from main DB to session DB, converting session IDs.
func copyMessages(mainDB *sql.DB, tx *sql.Tx, sessionIDs []string) error {
	stmt, err := tx.Prepare(`
		INSERT INTO messages (id, session_id, branch_id, parent_message_id, chosen_next_id, text, type, attachments, cumul_token_count, created_at, model, generation, state, aux, indexed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Build query with IN clause
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, session_id, branch_id, parent_message_id, chosen_next_id,
		       text, type, attachments, cumul_token_count, created_at, model, generation, state, aux, indexed
		FROM messages WHERE session_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := mainDB.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var sessionID, branchID, text, msgType, attachments sql.NullString
		var parentMessageID, chosenNextID, cumulTokenCount, generation sql.NullInt64
		var createdAt, model, state, aux sql.NullString
		var indexed sql.NullInt64

		err := rows.Scan(&id, &sessionID, &branchID, &parentMessageID, &chosenNextID,
			&text, &msgType, &attachments, &cumulTokenCount, &createdAt, &model, &generation, &state, &aux, &indexed)
		if err != nil {
			return fmt.Errorf("failed to scan message: %w", err)
		}

		// Convert session ID to local ID
		localSessionID := ToLocalSessionID(sessionID.String)

		// Insert into session DB
		_, err = stmt.Exec(
			id,
			localSessionID,
			nullString(branchID),
			nullInt64(parentMessageID),
			nullInt64(chosenNextID),
			nullString(text),
			nullString(msgType),
			nullString(attachments),
			nullInt64(cumulTokenCount),
			nullString(createdAt),
			nullString(model),
			nullInt64(generation),
			nullString(state),
			nullString(aux),
			nullInt64(indexed),
		)
		if err != nil {
			return fmt.Errorf("failed to insert message %d: %w", id, err)
		}
	}

	return rows.Err()
}

// copyBranches copies branches from main DB to session DB, converting session IDs.
func copyBranches(mainDB *sql.DB, tx *sql.Tx, sessionIDs []string) error {
	stmt, err := tx.Prepare(`
		INSERT INTO branches (id, session_id, parent_branch_id, branch_from_message_id, created_at, pending_confirmation)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Build query with IN clause
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, session_id, parent_branch_id, branch_from_message_id, created_at, pending_confirmation
		FROM branches WHERE session_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := mainDB.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query branches: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, sessionID sql.NullString
		var parentBranchID sql.NullString
		var branchFromMessageID sql.NullInt64
		var createdAt, pendingConfirmation sql.NullString

		err := rows.Scan(&id, &sessionID, &parentBranchID, &branchFromMessageID, &createdAt, &pendingConfirmation)
		if err != nil {
			return fmt.Errorf("failed to scan branch: %w", err)
		}

		// Convert session ID to local ID
		localSessionID := ToLocalSessionID(sessionID.String)

		// Insert into session DB
		_, err = stmt.Exec(
			nullString(id),
			localSessionID,
			nullString(parentBranchID),
			nullInt64(branchFromMessageID),
			nullString(createdAt),
			nullString(pendingConfirmation),
		)
		if err != nil {
			return fmt.Errorf("failed to insert branch %s: %w", id.String, err)
		}
	}

	return rows.Err()
}

// copySessionEnvs copies session_envs from main DB to session DB, converting session IDs.
func copySessionEnvs(mainDB *sql.DB, tx *sql.Tx, sessionIDs []string) error {
	stmt, err := tx.Prepare(`
		INSERT INTO session_envs (id, session_id, generation, roots, created_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Build query with IN clause
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, session_id, generation, roots, created_at
		FROM session_envs WHERE session_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := mainDB.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query session_envs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var sessionID, roots sql.NullString
		var generation sql.NullInt64
		var createdAt sql.NullString

		err := rows.Scan(&id, &sessionID, &generation, &roots, &createdAt)
		if err != nil {
			return fmt.Errorf("failed to scan session_env: %w", err)
		}

		// Convert session ID to local ID
		localSessionID := ToLocalSessionID(sessionID.String)

		// Insert into session DB
		_, err = stmt.Exec(
			id,
			localSessionID,
			nullInt64(generation),
			nullString(roots),
			nullString(createdAt),
		)
		if err != nil {
			return fmt.Errorf("failed to insert session_env %d: %w", id, err)
		}
	}

	return rows.Err()
}

// copyShellCommands copies shell_commands from main DB to session DB.
func copyShellCommands(mainDB *sql.DB, tx *sql.Tx, sessionIDs []string) error {
	// Get branch IDs for these sessions
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// First, get all branch IDs for these sessions
	branchQuery := fmt.Sprintf(`
		SELECT id FROM branches WHERE session_id IN (%s)
	`, strings.Join(placeholders, ","))

	branchRows, err := mainDB.Query(branchQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to query branch IDs: %w", err)
	}
	defer branchRows.Close()

	var branchIDs []string
	for branchRows.Next() {
		var branchID sql.NullString
		if err := branchRows.Scan(&branchID); err != nil {
			return fmt.Errorf("failed to scan branch ID: %w", err)
		}
		if branchID.Valid {
			branchIDs = append(branchIDs, branchID.String)
		}
	}

	if len(branchIDs) == 0 {
		// No shell commands to copy
		return nil
	}

	// Copy shell commands
	stmt, err := tx.Prepare(`
		INSERT INTO shell_commands (id, branch_id, command, status, start_time, end_time, stdout, stderr, exit_code, error_message, last_polled_at, next_poll_delay, stdout_offset, stderr_offset)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Build query with IN clause for branch IDs
	placeholders = make([]string, len(branchIDs))
	args = make([]interface{}, len(branchIDs))
	for i, id := range branchIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	cmdQuery := fmt.Sprintf(`
		SELECT id, branch_id, command, status, start_time, end_time, stdout, stderr, exit_code, error_message, last_polled_at, next_poll_delay, stdout_offset, stderr_offset
		FROM shell_commands WHERE branch_id IN (%s)
	`, strings.Join(placeholders, ","))

	cmdRows, err := mainDB.Query(cmdQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to query shell_commands: %w", err)
	}
	defer cmdRows.Close()

	for cmdRows.Next() {
		var id, branchID, command, status sql.NullString
		var startTime, endTime, lastPolledAt, nextPollDelay, stdoutOffset, stderrOffset sql.NullInt64
		var stdout, stderr []byte
		var exitCode sql.NullInt64
		var errorMessage sql.NullString

		err := cmdRows.Scan(&id, &branchID, &command, &status, &startTime, &endTime,
			&stdout, &stderr, &exitCode, &errorMessage, &lastPolledAt, &nextPollDelay, &stdoutOffset, &stderrOffset)
		if err != nil {
			return fmt.Errorf("failed to scan shell_command: %w", err)
		}

		// Insert into session DB
		_, err = stmt.Exec(
			nullString(id),
			nullString(branchID),
			nullString(command),
			nullString(status),
			nullInt64(startTime),
			nullInt64(endTime),
			stdout,
			stderr,
			nullInt64(exitCode),
			nullString(errorMessage),
			nullInt64(lastPolledAt),
			nullInt64(nextPollDelay),
			nullInt64(stdoutOffset),
			nullInt64(stderrOffset),
		)
		if err != nil {
			return fmt.Errorf("failed to insert shell_command %s: %w", id.String, err)
		}
	}

	return cmdRows.Err()
}

// copyBlobs copies blobs from main DB to session DB.
// Only copies blobs that are referenced by messages in the given sessions.
// Note: The triggers in session DB will handle refcount updates.
func copyBlobs(mainDB *sql.DB, tx *sql.Tx, sessionIDs []string) error {
	// Build a set of all blob hashes used by these sessions
	blobHashes := make(map[string]bool)

	// Build query with IN clause for session IDs
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT attachments FROM messages
		WHERE session_id IN (%s) AND attachments IS NOT NULL AND attachments != '[]'
	`, strings.Join(placeholders, ","))

	rows, err := mainDB.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query message attachments: %w", err)
	}
	defer rows.Close()

	// Extract all blob hashes from attachments
	for rows.Next() {
		var attachments sql.NullString
		if err := rows.Scan(&attachments); err != nil {
			return fmt.Errorf("failed to scan attachments: %w", err)
		}

		if !attachments.Valid || attachments.String == "" || attachments.String == "[]" {
			continue
		}

		// Parse JSON array of attachments
		var attList []map[string]interface{}
		if err := json.Unmarshal([]byte(attachments.String), &attList); err != nil {
			log.Printf("Warning: Failed to parse attachments JSON: %v", err)
			continue
		}

		// Extract blob hashes
		for _, att := range attList {
			if hash, ok := att["hash"].(string); ok && hash != "" {
				blobHashes[hash] = true
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// If no blobs needed, return early
	if len(blobHashes) == 0 {
		return nil
	}

	// Copy only the blobs that are actually used
	stmt, err := tx.Prepare("INSERT OR REPLACE INTO blobs (id, data, ref_count) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for hash := range blobHashes {
		var data []byte
		var refCount sql.NullInt64

		err := mainDB.QueryRow("SELECT data, ref_count FROM blobs WHERE id = ?", hash).Scan(&data, &refCount)
		if err != nil {
			log.Printf("Warning: Blob %s referenced but not found in blobs table: %v", hash, err)
			continue
		}

		// Insert into session DB
		_, err = stmt.Exec(hash, data, nullInt64(refCount))
		if err != nil {
			return fmt.Errorf("failed to insert blob %s: %w", hash, err)
		}
	}

	return nil
}

// PopulateSearchIndex populates the main DB search index with text copies from session DB.
// This enables cross-session full-text search using FTS5.
func PopulateSearchIndex(mainDB *sql.DB, sessionDB *sql.DB, sessionIDs []string) error {
	log.Printf("Populating search index for %d sessions", len(sessionIDs))

	// Begin transaction on main DB
	tx, err := mainDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction on main DB: %w", err)
	}
	defer tx.Rollback()

	// Prepare insert statement for search index
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO messages_searchable (id, text, session_id, workspace_id)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Query indexed messages from session DB
	rows, err := sessionDB.Query(`
		SELECT id, session_id, text, type, created_at, model, indexed
		FROM messages WHERE indexed = 1
	`)
	if err != nil {
		return fmt.Errorf("failed to query indexed messages: %w", err)
	}
	defer rows.Close()

	// Build a map of local session IDs to full session IDs
	localToFullID := make(map[string]string)
	for _, sessionID := range sessionIDs {
		localID := ToLocalSessionID(sessionID)
		localToFullID[localID] = sessionID
	}

	for rows.Next() {
		var id int
		var localSessionID, text, msgType, createdAt, model sql.NullString
		var indexed sql.NullInt64

		err := rows.Scan(&id, &localSessionID, &text, &msgType, &createdAt, &model, &indexed)
		if err != nil {
			return fmt.Errorf("failed to scan message: %w", err)
		}

		// Convert local session ID back to full ID
		fullSessionID, ok := localToFullID[localSessionID.String]
		if !ok {
			log.Printf("Warning: Could not find full session ID for local ID: %s", localSessionID.String)
			continue
		}

		// Get workspace_id from sessions table (in session DB)
		var workspaceID sql.NullString
		err = sessionDB.QueryRow("SELECT workspace_id FROM sessions WHERE id = ?", localSessionID.String).Scan(&workspaceID)
		if err != nil {
			log.Printf("Warning: Failed to query workspace_id for session %s: %v", localSessionID.String, err)
			workspaceID.String = ""
			workspaceID.Valid = true
		}

		// Insert into search index with full session ID
		_, err = stmt.Exec(
			id,
			nullString(text),
			fullSessionID,
			nullString(workspaceID),
		)
		if err != nil {
			return fmt.Errorf("failed to insert search index entry for message %d: %w", id, err)
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit search index transaction: %w", err)
	}

	// Sync to FTS tables
	if err := syncFTSTables(mainDB); err != nil {
		return fmt.Errorf("failed to sync FTS tables: %w", err)
	}

	return nil
}

// syncFTSTables syncs the messages_searchable with FTS5 virtual tables.
func syncFTSTables(mainDB *sql.DB) error {
	// Insert into message_stems
	_, err := mainDB.Exec(`
		INSERT INTO message_stems(rowid, text, session_id, workspace_id)
		SELECT id, text, session_id, workspace_id FROM messages_searchable
		WHERE id NOT IN (SELECT rowid FROM message_stems)
	`)
	if err != nil {
		return fmt.Errorf("failed to sync message_stems: %w", err)
	}

	// Insert into message_trigrams
	_, err = mainDB.Exec(`
		INSERT INTO message_trigrams(rowid, text, session_id, workspace_id)
		SELECT id, text, session_id, workspace_id FROM messages_searchable
		WHERE id NOT IN (SELECT rowid FROM message_trigrams)
	`)
	if err != nil {
		return fmt.Errorf("failed to sync message_trigrams: %w", err)
	}

	return nil
}

// ValidateMigration validates that the migration was successful.
// Checks row counts, verifies all sessions exist, and verifies search index coverage.
func ValidateMigration(ctx context.Context, mainDB *sql.DB) error {
	log.Println("Validating migration...")

	// Get all session IDs from main DB
	rows, err := mainDB.Query("SELECT id FROM sessions")
	if err != nil {
		return fmt.Errorf("failed to query sessions for validation: %w", err)
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

	if err := rows.Err(); err != nil {
		return err
	}

	// Group by main session ID
	groups := make(map[string][]string)
	for _, sessionID := range sessionIDs {
		mainSessionID, _ := SplitSessionId(sessionID)
		groups[mainSessionID] = append(groups[mainSessionID], sessionID)
	}

	// Validate each group
	for mainSessionID, sessionIDs := range groups {
		sessionDBPath, err := GetSessionDBPath(ctx, mainSessionID)
		if err != nil {
			return fmt.Errorf("failed to get session DB path for %s: %w", mainSessionID, err)
		}
		if _, err := os.Stat(sessionDBPath); err != nil {
			return fmt.Errorf("session DB file missing: %s", sessionDBPath)
		}

		// Open session DB
		sessionDB, err := sql.Open("sqlite3", sessionDBPath)
		if err != nil {
			return fmt.Errorf("failed to open session DB %s: %w", sessionDBPath, err)
		}

		// Count sessions in session DB
		var sessionCount int
		err = sessionDB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
		if err != nil {
			sessionDB.Close()
			return fmt.Errorf("failed to count sessions in %s: %w", sessionDBPath, err)
		}

		sessionDB.Close()

		if sessionCount != len(sessionIDs) {
			return fmt.Errorf("session count mismatch for %s: expected %d, got %d",
				mainSessionID, len(sessionIDs), sessionCount)
		}
	}

	// Verify search index coverage
	var indexedCount, totalCount int
	err = mainDB.QueryRow("SELECT COUNT(*) FROM messages_searchable").Scan(&indexedCount)
	if err != nil {
		return fmt.Errorf("failed to count indexed messages: %w", err)
	}

	// Count all messages across all session DBs
	totalCount = 0
	for mainSessionID := range groups {
		sessionDBPath, err := GetSessionDBPath(ctx, mainSessionID)
		if err != nil {
			return fmt.Errorf("failed to get session DB path for counting: %w", err)
		}
		sessionDB, err := sql.Open("sqlite3", sessionDBPath)
		if err != nil {
			return fmt.Errorf("failed to open session DB for counting: %w", err)
		}

		var count int
		err = sessionDB.QueryRow("SELECT COUNT(*) FROM messages WHERE indexed = 1").Scan(&count)
		sessionDB.Close()
		if err != nil {
			return fmt.Errorf("failed to count indexed messages in %s: %w", sessionDBPath, err)
		}

		totalCount += count
	}

	if indexedCount != totalCount {
		return fmt.Errorf("search index coverage mismatch: expected %d, got %d",
			totalCount, indexedCount)
	}

	log.Printf("Migration validation passed: %d sessions, %d indexed messages", len(sessionIDs), indexedCount)
	return nil
}

// backupMainDB creates a backup of the main database.
func backupMainDB(mainDB *sql.DB) error {
	// Get the path of the main database
	var seq int
	var name string
	var dbPath string
	err := mainDB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &dbPath)
	if err != nil {
		return fmt.Errorf("failed to get database path: %w", err)
	}

	// Create backup path
	backupPath := strings.TrimSuffix(dbPath, ".db") + ".bak.db"

	// Check if backup already exists
	if _, err := os.Stat(backupPath); err == nil {
		log.Printf("Backup already exists: %s", backupPath)
		return nil
	}

	// Use SQLite's backup API
	_, err = mainDB.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath))
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	return nil
}

// Helper functions for NULL handling

func nullString(s sql.NullString) interface{} {
	if s.Valid {
		return s.String
	}
	return nil
}

func nullInt64(i sql.NullInt64) interface{} {
	if i.Valid {
		return i.Int64
	}
	return nil
}
