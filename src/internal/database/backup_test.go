package database

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestBackupEnvironment creates a temporary sandbox directory with test files.
func setupTestBackupEnvironment(t *testing.T) (sandboxDir string, cleanup func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "angel-backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create test files
	testFiles := map[string][]byte{
		"file1.txt":                          []byte("Hello, World!"),
		filepath.Join("subdir", "file2.txt"): []byte("Nested file content"),
		"binary.dat":                         {0x00, 0x01, 0x02, 0x03},
	}

	for relPath, data := range testFiles {
		fullPath := filepath.Join(tempDir, relPath)
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			t.Fatalf("Failed to create parent dir: %v", err)
		}
		if err := os.WriteFile(fullPath, data, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Set specific modification time for testing
		mtime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		if err := os.Chtimes(fullPath, time.Now(), mtime); err != nil {
			t.Fatalf("Failed to set mtime: %v", err)
		}
	}

	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, cleanup
}

// setupTestSessionDB creates a temporary session database file with files table.
func setupTestSessionDB(t *testing.T) (dbPath string, cleanup func()) {
	t.Helper()

	tempFile, err := os.CreateTemp("", "angel-test-session-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp DB file: %v", err)
	}
	dbPath = tempFile.Name()
	tempFile.Close()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		os.Remove(dbPath)
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// Create files table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			data BLOB NOT NULL,
			metadata TEXT NOT NULL
		)
	`)
	if err != nil {
		os.Remove(dbPath)
		t.Fatalf("Failed to create files table: %v", err)
	}

	cleanup = func() {
		os.Remove(dbPath)
	}

	return dbPath, cleanup
}

// TestBackupAnonymousRoot tests backing up files from sandbox to DB.
func TestBackupAnonymousRoot(t *testing.T) {
	sandboxDir, sandboxCleanup := setupTestBackupEnvironment(t)
	defer sandboxCleanup()

	sessionDBPath, dbCleanup := setupTestSessionDB(t)
	defer dbCleanup()

	// Perform backup
	err := BackupAnonymousRoot(sessionDBPath, sandboxDir)
	if err != nil {
		t.Fatalf("BackupAnonymousRoot failed: %v", err)
	}

	// Open DB to verify files were backed up
	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open session DB: %v", err)
	}
	defer sessionDB.Close()

	rows, err := sessionDB.Query("SELECT path, data, metadata FROM files ORDER BY path")
	if err != nil {
		t.Fatalf("Failed to query files: %v", err)
	}
	defer rows.Close()

	fileCount := 0
	expectedFiles := map[string]bool{
		"file1.txt":                          false,
		filepath.Join("subdir", "file2.txt"): false,
		"binary.dat":                         false,
	}

	for rows.Next() {
		var path string
		var data []byte
		var metadataJSON string

		if err := rows.Scan(&path, &data, &metadataJSON); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}

		// Check file was expected
		if _, ok := expectedFiles[path]; !ok {
			t.Errorf("Unexpected file in backup: %s", path)
			continue
		}
		expectedFiles[path] = true

		// Parse metadata
		var metadata FileMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			t.Errorf("Failed to parse metadata for %s: %v", path, err)
			continue
		}

		// Verify mtime
		expectedMtime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()
		if metadata.Mtime != expectedMtime {
			t.Errorf("File %s: expected mtime %d, got %d", path, expectedMtime, metadata.Mtime)
		}

		// Verify file data
		expectedData := map[string][]byte{
			"file1.txt":                          []byte("Hello, World!"),
			filepath.Join("subdir", "file2.txt"): []byte("Nested file content"),
			"binary.dat":                         {0x00, 0x01, 0x02, 0x03},
		}

		if expected, ok := expectedData[path]; ok {
			if string(data) != string(expected) {
				t.Errorf("File %s: data mismatch", path)
			}
		}

		fileCount++
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("Error iterating rows: %v", err)
	}

	// Check all expected files were backed up
	for path, found := range expectedFiles {
		if !found {
			t.Errorf("Expected file not found in backup: %s", path)
		}
	}

	if fileCount != len(expectedFiles) {
		t.Errorf("Expected %d files, got %d", len(expectedFiles), fileCount)
	}
}

// TestRestoreAnonymousRoot tests restoring files from DB to sandbox.
func TestRestoreAnonymousRoot(t *testing.T) {
	// Setup: backup first, then restore to new location
	sourceDir, sourceCleanup := setupTestBackupEnvironment(t)
	defer sourceCleanup()

	sessionDBPath, dbCleanup := setupTestSessionDB(t)
	defer dbCleanup()

	// Backup
	if err := BackupAnonymousRoot(sessionDBPath, sourceDir); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Remove source directory
	sourceCleanup()

	// Restore to same location
	if err := RestoreAnonymousRoot(sessionDBPath, sourceDir); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify restored files
	expectedFiles := map[string]string{
		"file1.txt":                          "Hello, World!",
		filepath.Join("subdir", "file2.txt"): "Nested file content",
		"binary.dat":                         "\x00\x01\x02\x03",
	}

	for relPath, expectedContent := range expectedFiles {
		fullPath := filepath.Join(sourceDir, relPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read restored file %s: %v", relPath, err)
			continue
		}

		if string(data) != expectedContent {
			t.Errorf("File %s: content mismatch", relPath)
		}

		// Verify mtime
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Errorf("Failed to stat restored file %s: %v", relPath, err)
			continue
		}

		expectedMtime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		if info.ModTime().Unix() != expectedMtime.Unix() {
			t.Errorf("File %s: mtime mismatch", relPath)
		}
	}
}

// TestBackupRestoreRoundTrip tests complete backup/restore cycle.
func TestBackupRestoreRoundTrip(t *testing.T) {
	sourceDir, sourceCleanup := setupTestBackupEnvironment(t)
	defer sourceCleanup()

	tempDBFile := filepath.Join(os.TempDir(), "angel-test-session.db")
	defer os.Remove(tempDBFile)

	// Create session DB
	sessionDB, err := sql.Open("sqlite3", tempDBFile)
	if err != nil {
		t.Fatalf("Failed to create session DB: %v", err)
	}
	defer sessionDB.Close()

	// Create schema
	_, err = sessionDB.Exec(`
		CREATE TABLE files (
			path TEXT PRIMARY KEY,
			data BLOB NOT NULL,
			metadata TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create files table: %v", err)
	}
	sessionDB.Close()

	// Backup
	if err := BackupAnonymousRoot(tempDBFile, sourceDir); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Remove source
	sourceCleanup()

	// Restore
	if err := RestoreAnonymousRoot(tempDBFile, sourceDir); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify
	expectedFiles := []string{"file1.txt", filepath.Join("subdir", "file2.txt"), "binary.dat"}
	for _, relPath := range expectedFiles {
		fullPath := filepath.Join(sourceDir, relPath)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("Restored file not found: %s (%v)", relPath, err)
		}
	}
}

// TestCleanupAnonymousRoot tests cleanup function.
func TestCleanupAnonymousRoot(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "angel-cleanup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create some files
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Cleanup
	if err := CleanupAnonymousRoot(tempDir); err != nil {
		t.Fatalf("CleanupAnonymousRoot failed: %v", err)
	}

	// Verify directory is removed
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("Directory still exists after cleanup")
	}
}

// TestClearFilesTable tests clearing files table.
func TestClearFilesTable(t *testing.T) {
	sessionDBPath, dbCleanup := setupTestSessionDB(t)
	defer dbCleanup()

	// Insert some test data
	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open session DB: %v", err)
	}
	defer sessionDB.Close()

	_, err = sessionDB.Exec("INSERT INTO files (path, data, metadata) VALUES (?, ?, ?)",
		"test.txt", []byte("data"), `{"mtime": 1234567890, "mode": 0644}`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Clear table
	if err := ClearFilesTable(sessionDBPath); err != nil {
		t.Fatalf("ClearFilesTable failed: %v", err)
	}

	// Verify table is empty
	var count int
	err = sessionDB.QueryRow("SELECT COUNT(*) FROM files").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 rows, got %d", count)
	}
}

// TestBackupEmptyDirectory tests backing up an empty directory.
func TestBackupEmptyDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "angel-empty-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	sessionDBPath, dbCleanup := setupTestSessionDB(t)
	defer dbCleanup()

	// Backup empty directory
	if err := BackupAnonymousRoot(sessionDBPath, tempDir); err != nil {
		t.Fatalf("BackupAnonymousRoot failed on empty directory: %v", err)
	}

	// Verify no files in table
	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open session DB: %v", err)
	}
	defer sessionDB.Close()

	var count int
	err = sessionDB.QueryRow("SELECT COUNT(*) FROM files").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 rows for empty directory, got %d", count)
	}
}

// TestRestoreFromEmptyTable tests restoring when files table is empty.
func TestRestoreFromEmptyTable(t *testing.T) {
	sessionDBPath, dbCleanup := setupTestSessionDB(t)
	defer dbCleanup()

	tempDir, err := os.MkdirTemp("", "angel-restore-empty-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Restore from empty table (should succeed with no files)
	if err := RestoreAnonymousRoot(sessionDBPath, tempDir); err != nil {
		t.Fatalf("RestoreAnonymousRoot failed on empty table: %v", err)
	}

	// Verify directory exists but is empty
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Expected empty directory, got %d entries", len(entries))
	}
}
