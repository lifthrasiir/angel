package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/lifthrasiir/angel/internal/types"
)

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

// CleanupStaleShellCommands marks any previously running commands as failed on startup.
// This is used to clean up commands that were running when Angel restarted.
func CleanupStaleShellCommands(db *Database, now time.Time) error {
	result, err := db.Exec(`
		UPDATE shell_commands
		SET status = 'failed_on_startup',
		    end_time = ?,
		    error_message = 'Command failed because Angel restarted.'
		WHERE status = 'running'`,
		now.Unix())
	if err != nil {
		return fmt.Errorf("failed to update stale shell commands: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		fmt.Printf("Cleaned up %d stale shell commands.\n", rowsAffected)
	}
	return nil
}
