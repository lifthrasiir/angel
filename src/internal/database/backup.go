package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileMetadata stores file metadata for backup/restore.
type FileMetadata struct {
	Mtime int64 `json:"mtime"` // Modification time as Unix timestamp
	Mode  int   `json:"mode"`  // File mode (permissions)
}

// BackupAnonymousRoot backs up all files from the anonymous root directory (sandboxDir)
// to the files table in the session database.
// It walks through the directory recursively and stores each file with its metadata.
func BackupAnonymousRoot(sessionDBPath, sandboxDir string) error {
	// Open session database
	db, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		return fmt.Errorf("failed to open session DB: %w", err)
	}
	defer db.Close()

	// Start transaction for atomic backup
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Clear existing files table
	if _, err := tx.Exec("DELETE FROM files"); err != nil {
		return fmt.Errorf("failed to clear files table: %w", err)
	}

	// Prepare insert statement
	stmt, err := tx.Prepare("INSERT INTO files (path, data, metadata) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Walk through sandbox directory
	insertCount := 0
	walkErr := filepath.Walk(sandboxDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories (we create them during restore)
		if info.IsDir() {
			return nil
		}

		// Calculate relative path from sandboxDir
		relPath, err := filepath.Rel(sandboxDir, path)
		if err != nil {
			return fmt.Errorf("failed to calculate relative path for %s: %w", path, err)
		}

		// Read file data
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		// Collect metadata
		metadata := FileMetadata{
			Mtime: info.ModTime().Unix(),
			Mode:  int(info.Mode()),
		}
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata for %s: %w", path, err)
		}

		// Insert into files table
		if _, err := stmt.Exec(relPath, data, string(metadataJSON)); err != nil {
			return fmt.Errorf("failed to insert file %s: %w", relPath, err)
		}

		insertCount++
		return nil
	})

	if walkErr != nil {
		return fmt.Errorf("error walking sandbox directory: %w", walkErr)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if insertCount == 0 {
		// No files to backup, delete the files table entry if any
		if _, err := db.Exec("DELETE FROM files"); err != nil {
			return fmt.Errorf("failed to clear files table after empty backup: %w", err)
		}
	}

	return nil
}

// RestoreAnonymousRoot restores all files from the files table to the anonymous root directory.
func RestoreAnonymousRoot(sessionDBPath, sandboxDir string) error {
	// Open session database
	db, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		return fmt.Errorf("failed to open session DB: %w", err)
	}
	defer db.Close()

	// Query all files from files table
	rows, err := db.Query("SELECT path, data, metadata FROM files")
	if err != nil {
		return fmt.Errorf("failed to query files table: %w", err)
	}
	defer rows.Close()

	// Restore each file
	for rows.Next() {
		var relPath string
		var data []byte
		var metadataJSON string

		if err := rows.Scan(&relPath, &data, &metadataJSON); err != nil {
			return fmt.Errorf("failed to scan file row: %w", err)
		}

		// Parse metadata
		var metadata FileMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return fmt.Errorf("failed to unmarshal metadata for %s: %w", relPath, err)
		}

		// Construct full path
		fullPath := filepath.Join(sandboxDir, relPath)

		// Ensure parent directory exists
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", relPath, err)
		}

		// Write file data
		if err := os.WriteFile(fullPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", relPath, err)
		}

		// Set modification time
		if err := os.Chtimes(fullPath, time.Now(), time.Unix(metadata.Mtime, 0)); err != nil {
			return fmt.Errorf("failed to set mtime for %s: %w", relPath, err)
		}

		// Set file mode
		if err := os.Chmod(fullPath, os.FileMode(metadata.Mode)); err != nil {
			return fmt.Errorf("failed to set mode for %s: %w", relPath, err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating files: %w", err)
	}

	return nil
}

// CleanupAnonymousRoot removes the entire anonymous root directory.
// This is a fire-and-forget operation that can be called asynchronously.
func CleanupAnonymousRoot(sandboxDir string) error {
	if err := os.RemoveAll(sandboxDir); err != nil {
		return fmt.Errorf("failed to remove sandbox directory %s: %w", sandboxDir, err)
	}
	return nil
}

// ClearFilesTable removes all entries from the files table.
// This is a fire-and-forget operation that can be called asynchronously after unarchive.
func ClearFilesTable(sessionDBPath string) error {
	db, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		return fmt.Errorf("failed to open session DB: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("DELETE FROM files"); err != nil {
		return fmt.Errorf("failed to clear files table: %w", err)
	}

	return nil
}
