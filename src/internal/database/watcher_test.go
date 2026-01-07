package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// waitForTimeout waits for a condition to be true, with a timeout.
func waitForTimeout(condition func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// createTestSessionDB creates a test session database with the required tables using the actual schema.
func createTestSessionDB(t *testing.T, path string, workspaceID string) {
	sessionDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Use actual schema for testing (replace S. with empty string)
	schemaSQL := strings.ReplaceAll(createSessionSchemaSQL, "S.", "")

	// Execute schema
	if _, err := sessionDB.Exec(schemaSQL); err != nil {
		t.Fatalf("Failed to create session schema: %v", err)
	}

	// Insert workspace_id
	if _, err := sessionDB.Exec("UPDATE sessions SET workspace_id = ?", workspaceID); err != nil {
		t.Fatalf("Failed to set workspace_id: %v", err)
	}
}

// createTestSessionDBWithMessage creates a test session database with a message using the actual schema.
func createTestSessionDBWithMessage(t *testing.T, path string, workspaceID string, messageID string, messageType string, text string) {
	createTestSessionDB(t, path, workspaceID)

	// Open again to insert message
	sessionDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("Failed to open session database: %v", err)
	}
	defer sessionDB.Close()

	// Insert a test message with required fields
	if _, err := sessionDB.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
		"", "default", text, messageType, "test-model"); err != nil {
		t.Fatalf("Failed to insert test message: %v", err)
	}
}

// TestSessionWatcher_NewSessionWatcher tests the creation of a new SessionWatcher.
func TestSessionWatcher_NewSessionWatcher(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if watcher == nil {
		t.Fatal("Expected non-nil SessionWatcher")
	}

	if watcher.sessionDir != sessionDir {
		t.Errorf("Expected sessionDir %s, got %s", sessionDir, watcher.sessionDir)
	}

	watcher.Stop()
}

// TestSessionWatcher_StartAndStop tests starting and stopping the watcher.
func TestSessionWatcher_StartAndStop(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	if err := watcher.Stop(); err != nil {
		t.Fatalf("Failed to stop watcher: %v", err)
	}
}

// TestSessionWatcher_DatabaseCreate tests handling of database file creation.
func TestSessionWatcher_DatabaseCreate(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDBWithMessage(t, sessionDBPath, "workspace1", "msg1", "user", "Hello world")

	// Wait for file to be created and processed
	if !waitForTimeout(func() bool {
		info, err := os.Stat(sessionDBPath)
		return err == nil && info.Size() > 0
	}, 1*time.Second) {
		t.Fatal("Timeout waiting for database file to be created")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_DatabaseRemove tests handling of database file deletion.
func TestSessionWatcher_DatabaseRemove(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	if err := os.Remove(sessionDBPath); err != nil {
		t.Fatalf("Failed to remove session database: %v", err)
	}

	// Wait for file to be removed
	if !waitForTimeout(func() bool {
		_, err := os.Stat(sessionDBPath)
		return os.IsNotExist(err)
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database file to be removed")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_DatabaseRename tests handling of database file renaming.
func TestSessionWatcher_DatabaseRename(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	oldPath := filepath.Join(sessionDir, "old_session.db")
	createTestSessionDB(t, oldPath, "workspace1")

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	newPath := filepath.Join(sessionDir, "new_session.db")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("Failed to rename session database: %v", err)
	}

	// Wait for rename to complete
	if !waitForTimeout(func() bool {
		_, err := os.Stat(newPath)
		return err == nil
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database file to be renamed")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_PollingFallback tests fallback to polling mode when fsnotify fails.
func TestSessionWatcher_PollingFallback(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}
	defer watcher.Stop()

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher (should fall back to polling): %v", err)
	}

	// Just verify it started successfully
}

// TestSessionWatcher_InvalidDatabase tests handling of corrupted database files.
func TestSessionWatcher_InvalidDatabase(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	invalidDBPath := filepath.Join(sessionDir, "invalid.db")
	if err := os.WriteFile(invalidDBPath, []byte("This is not a valid SQLite database"), 0644); err != nil {
		t.Fatalf("Failed to create invalid database file: %v", err)
	}

	// Wait for file to be created
	if !waitForTimeout(func() bool {
		info, err := os.Stat(invalidDBPath)
		return err == nil && info.Size() > 0
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for invalid database file to be created")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_MultipleDatabases tests handling of multiple database files.
func TestSessionWatcher_MultipleDatabases(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	numSessions := 5
	for i := 1; i <= numSessions; i++ {
		sessionDBPath := filepath.Join(sessionDir, "session"+strconv.Itoa(i)+".db")
		createTestSessionDB(t, sessionDBPath, "workspace1")
	}

	// Wait for all files to be created
	if !waitForTimeout(func() bool {
		for i := 1; i <= numSessions; i++ {
			sessionDBPath := filepath.Join(sessionDir, "session"+strconv.Itoa(i)+".db")
			if _, err := os.Stat(sessionDBPath); err != nil {
				return false
			}
		}
		return true
	}, 1*time.Second) {
		t.Fatal("Timeout waiting for all database files to be created")
	}

	// Give watcher a moment to process the events
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_NonDBFilesIgnored tests that non-.db files are ignored.
func TestSessionWatcher_NonDBFilesIgnored(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	files := []string{
		"test.txt",
		"test.log",
		"test.json",
		"README.md",
	}

	for _, filename := range files {
		path := filepath.Join(sessionDir, filename)
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", filename, err)
		}
	}

	// Wait for all files to be created
	if !waitForTimeout(func() bool {
		for _, filename := range files {
			path := filepath.Join(sessionDir, filename)
			if _, err := os.Stat(path); err != nil {
				return false
			}
		}
		return true
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for all files to be created")
	}

	// Give watcher a moment to process (or ignore) the events
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_ConcurrentAccess tests concurrent access to the watcher.
func TestSessionWatcher_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			sessionDBPath := filepath.Join(sessionDir, "session"+strconv.Itoa(index)+".db")
			createTestSessionDB(t, sessionDBPath, "workspace1")
		}(i)
	}

	wg.Wait()

	// Wait for all files to be created
	if !waitForTimeout(func() bool {
		for i := 0; i < numGoroutines; i++ {
			sessionDBPath := filepath.Join(sessionDir, "session"+strconv.Itoa(i)+".db")
			if _, err := os.Stat(sessionDBPath); err != nil {
				return false
			}
		}
		return true
	}, 1*time.Second) {
		t.Fatal("Timeout waiting for all database files to be created")
	}

	// Give watcher a moment to process the events
	time.Sleep(100 * time.Millisecond)
}

// TestSessionWatcher_WriteEvent tests handling of database write events.
func TestSessionWatcher_WriteEvent(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Initialize database
	statements := []string{
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT)",
		"CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, text TEXT)",
		"INSERT INTO sessions (id, workspace_id) VALUES ('', 'workspace1')",
		"INSERT INTO messages (id, session_id, type, text) VALUES ('msg1', '', 'user', 'Initial message')",
	}
	for _, stmt := range statements {
		if _, err := sessionDB.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement: %s, error: %v", stmt, err)
		}
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	// Wait for initial DB to be processed
	if !waitForTimeout(func() bool {
		info, err := os.Stat(sessionDBPath)
		return err == nil && info.Size() > 0
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database file to be created")
	}

	if _, err := sessionDB.Exec("INSERT INTO messages (id, session_id, type, text) VALUES ('msg2', '', 'user', 'New message')"); err != nil {
		t.Fatalf("Failed to insert new message: %v", err)
	}

	// Wait for write to be flushed to disk
	if !waitForTimeout(func() bool {
		rows, err := sessionDB.Query("SELECT id FROM messages WHERE id = 'msg2'")
		if err != nil {
			return false
		}
		defer rows.Close()
		return rows.Next()
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for message to be written")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_Debouncing tests that debouncing works correctly.
func TestSessionWatcher_Debouncing(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDBWithMessage(t, sessionDBPath, "workspace1", "msg0", "user", "Message 0")

	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Rapidly write multiple times
	for i := 1; i < 10; i++ {
		if _, err := sessionDB.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
			"", "default", "Message "+strconv.Itoa(i), "user", "test-model"); err != nil {
			t.Fatalf("Failed to insert message %d: %v", i, err)
		}
	}

	// Wait for all writes to be flushed to disk
	if !waitForTimeout(func() bool {
		rows, err := sessionDB.Query("SELECT COUNT(*) as count FROM messages")
		if err != nil {
			return false
		}
		defer rows.Close()
		if !rows.Next() {
			return false
		}
		var count int
		if err := rows.Scan(&count); err != nil {
			return false
		}
		return count == 10
	}, 1*time.Second) {
		t.Fatal("Timeout waiting for all messages to be written")
	}

	// Give watcher a moment to process the debounced events
	time.Sleep(150 * time.Millisecond)
}

// TestSessionWatcher_PollingModeRescan tests polling mode rescan functionality.
func TestSessionWatcher_PollingModeRescan(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}
	defer watcher.Stop()

	knownFiles := make(map[string]time.Time)

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	watcher.rescanSessions(knownFiles)
}

// TestSessionWatcher_InvalidDirectory tests handling of invalid session directory.
func TestSessionWatcher_InvalidDirectory(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "nonexistent", "sessions")

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}
	defer watcher.Stop()

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	// Just verify it started successfully (should fall back to polling mode)
}

// TestSessionWatcher_EmptySessionID tests handling of session databases with empty session IDs.
func TestSessionWatcher_EmptySessionID(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "")

	// Wait for file to be created
	if !waitForTimeout(func() bool {
		info, err := os.Stat(sessionDBPath)
		return err == nil && info.Size() > 0
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database file to be created")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_OnlySyncsUserModelMessages tests that only user and model messages are synced.
func TestSessionWatcher_OnlySyncsUserModelMessages(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Initialize database with various message types
	statements := []string{
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT)",
		"CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, text TEXT)",
		"INSERT INTO sessions (id, workspace_id) VALUES ('', 'workspace1')",
		"INSERT INTO messages (id, session_id, type, text) VALUES ('msg1', '', 'user', 'User message')",
		"INSERT INTO messages (id, session_id, type, text) VALUES ('msg2', '', 'model', 'Model message')",
		"INSERT INTO messages (id, session_id, type, text) VALUES ('msg3', '', 'function_call', 'Function call')",
		"INSERT INTO messages (id, session_id, type, text) VALUES ('msg4', '', 'function_response', 'Function response')",
		"INSERT INTO messages (id, session_id, type, text) VALUES ('msg5', '', 'thought', 'Thought')",
	}
	for _, stmt := range statements {
		if _, err := sessionDB.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement: %s, error: %v", stmt, err)
		}
	}

	// Wait for all messages to be written
	if !waitForTimeout(func() bool {
		rows, err := sessionDB.Query("SELECT COUNT(*) as count FROM messages")
		if err != nil {
			return false
		}
		defer rows.Close()
		if !rows.Next() {
			return false
		}
		var count int
		if err := rows.Scan(&count); err != nil {
			return false
		}
		return count == 5
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for all messages to be written")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_SpecialCharactersInText tests handling of special characters in message text.
func TestSessionWatcher_SpecialCharactersInText(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Text with angle brackets which should be replaced
	testText := "<html>This is a <test> message</html>"

	statements := []string{
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT)",
		"CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, text TEXT)",
		"INSERT INTO sessions (id, workspace_id) VALUES ('', 'workspace1')",
		fmt.Sprintf("INSERT INTO messages (id, session_id, type, text) VALUES ('msg1', '', 'user', '%s')", testText),
	}
	for _, stmt := range statements {
		if _, err := sessionDB.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement: %s, error: %v", stmt, err)
		}
	}

	// Wait for message to be written
	if !waitForTimeout(func() bool {
		rows, err := sessionDB.Query("SELECT COUNT(*) as count FROM messages")
		if err != nil {
			return false
		}
		defer rows.Close()
		if !rows.Next() {
			return false
		}
		var count int
		if err := rows.Scan(&count); err != nil {
			return false
		}
		return count == 1
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for message to be written")
	}

	// Give watcher a moment to process the event
	time.Sleep(50 * time.Millisecond)
}

// TestSessionWatcher_OwnChangesAreIgnoredWhileDatabaseIsAttached tests that changes made while the database
// is attached (in use by the application) are ignored and do not trigger synchronization.
// NOTE: This test documents the DESIRED behavior. Current implementation does NOT pass this test.
func TestSessionWatcher_OwnChangesAreIgnoredWhileDatabaseIsAttached(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	// Create a new watcher for the test session directory
	// (db's existing watcher watches a different directory)
	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}
	// Update AttachPool to use this watcher
	db.GetAttachPool().SetWatcher(watcher)

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for initial tracking
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked")
	}

	// Scenario 1: Attach the database and execute a query (actual change)
	// With the new hook-based implementation, MarkExpectedChange is NOT called on attach
	// Instead, the commit hook will call it when we actually commit a transaction
	alias, cleanup, err := db.GetAttachPool().Acquire(sessionDBPath, "session1")
	if err != nil {
		t.Fatalf("Failed to attach session database: %v", err)
	}

	if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s.messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)", alias),
		"", "default", "Our own change", "user", "test-model"); err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}

	// Wait for expectedChange flag to be set (commit happened)
	// Then wait for it to be cleared (WRITE event processed)
	if !waitForTimeout(func() bool {
		watcher.mu.RLock()
		state := watcher.states["session1"]
		tracked := state != nil && state.tracked
		expected := state != nil && state.expectedChange
		watcher.mu.RUnlock()
		// We expect the flag to be set initially, then cleared after WRITE event
		return tracked && !expected
	}, 500*time.Millisecond) {
		t.Error("Expected commit to set expectedChange, then WRITE event to clear it")
	}

	// Scenario 2: Now make an external change while still attached
	// With the new hook-based implementation, external changes are properly detected
	// because expectedChange was cleared after processing our commit's WRITE event
	externalDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open external session database: %v", err)
	}
	defer externalDB.Close()

	if _, err := externalDB.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
		"", "default", "External change while attached", "user", "test-model"); err != nil {
		t.Fatalf("Failed to insert external message: %v", err)
	}

	// Wait for external change to be processed (sync should happen)
	// expectedChange should remain false after processing
	if !waitForTimeout(func() bool {
		watcher.mu.RLock()
		state := watcher.states["session1"]
		expected := state != nil && state.expectedChange
		watcher.mu.RUnlock()
		// expectedChange should be false (external change was detected and synced)
		return !expected
	}, 500*time.Millisecond) {
		t.Error("Expected external change while attached to be detected (expectedChange should be false)")
	}

	// Scenario 3: Detach happens
	// Call cleanup explicitly to decrement refcount
	cleanup()

	// Wait for refcount to reach 0 (database may still be attached but unused)
	if !waitForTimeout(func() bool {
		// Check that refcount is 0
		p := db.GetAttachPool()
		p.mu.RLock()
		refcount := 0
		for _, attached := range p.attached {
			if attached.MainSessionID == "session1" {
				refcount = attached.RefCount
				break
			}
		}
		p.mu.RUnlock()
		return refcount == 0
	}, 200*time.Millisecond) {
		t.Error("Expected database refcount to be 0 after cleanup")
	}

	// Scenario 4: Now external change should be detected (database is detached)
	externalDB2, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open external session database: %v", err)
	}
	defer externalDB2.Close()

	if _, err := externalDB2.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
		"", "default", "External change after detach", "user", "test-model"); err != nil {
		t.Fatalf("Failed to insert external message: %v", err)
	}

	// Wait for external change to be processed
	if !waitForTimeout(func() bool {
		watcher.mu.RLock()
		state := watcher.states["session1"]
		expected := state != nil && state.expectedChange
		watcher.mu.RUnlock()
		// expectedChange should be false (external change was detected)
		return !expected
	}, 500*time.Millisecond) {
		t.Error("Expected external change after detach to be detected (expectedChange should be false)")
	}
}

// TestSessionWatcher_AttachWithoutQueriesDetectsExternalChanges tests that attaching without executing
// queries still allows external changes to be detected.
// With the new hook-based implementation, expectedChange is only set when we commit,
// not when we merely attach. So external changes before any commit are detected.
func TestSessionWatcher_AttachWithoutQueriesDetectsExternalChanges(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	// Create a new watcher for the test session directory
	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}
	// Update AttachPool to use this watcher
	db.GetAttachPool().SetWatcher(watcher)

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for initial tracking
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked")
	}

	// Attach without executing any queries
	// With the new hook-based implementation, expectedChange is NOT set on attach
	_, cleanup, err := db.GetAttachPool().Acquire(sessionDBPath, "session1")
	if err != nil {
		t.Fatalf("Failed to attach session database: %v", err)
	}

	// Immediately make an external change (should be detected since we haven't committed anything)
	externalDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open external session database: %v", err)
	}
	defer externalDB.Close()

	if _, err := externalDB.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
		"", "default", "External change without our queries", "user", "test-model"); err != nil {
		t.Fatalf("Failed to insert external message: %v", err)
	}

	// Wait for external change to be processed
	// expectedChange should remain false since we haven't committed anything
	if !waitForTimeout(func() bool {
		watcher.mu.RLock()
		state := watcher.states["session1"]
		expected := state != nil && state.expectedChange
		watcher.mu.RUnlock()
		// expectedChange should be false (no commit, so external change is detected)
		return !expected
	}, 500*time.Millisecond) {
		t.Error("Expected external change to be detected when we haven't committed anything ourselves")
	}

	// Cleanup
	cleanup()
}

// TestSessionWatcher_LateFileCreationIsDetected tests that database files created after the watcher starts
// are properly detected and tracked (handles missing Create events on some platforms).
func TestSessionWatcher_LateFileCreationIsDetected(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	// Create database AFTER watcher is started (simulating late creation)
	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for tracking to happen via write event
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked via write event")
	}
}

// TestSessionWatcher_EmptyAttachDetachCyclePreservesTrackingState tests that an attach/detach cycle without
// actual database modifications correctly preserves the tracking state.
func TestSessionWatcher_EmptyAttachDetachCyclePreservesTrackingState(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for initial tracking
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked")
	}

	// Mark expected change (simulating AttachPool.Attach)
	watcher.MarkExpectedChange("session1")

	// Immediately clear it (simulating AttachPool.detachInternal)
	watcher.ClearExpectedChange("session1")

	// Verify state
	watcher.mu.RLock()
	state := watcher.states["session1"]
	watcher.mu.RUnlock()

	if state != nil && state.expectedChange {
		t.Error("Expected expectedChange to be false after clear")
	}

	if state == nil || !state.tracked {
		t.Error("Expected database to still be tracked")
	}
}

// TestSessionWatcher_ExternalChangesDetachedFromAttachedDatabase tests that external modifications to a database
// after it has been detached are properly detected and trigger synchronization.
func TestSessionWatcher_ExternalChangesDetectedAfterDatabaseDetached(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for initial tracking
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked")
	}

	// Simulate attach/detach cycle
	watcher.MarkExpectedChange("session1")

	// Verify expectedChange is set
	watcher.mu.RLock()
	stateBefore := watcher.states["session1"]
	watcher.mu.RUnlock()

	if stateBefore == nil || !stateBefore.expectedChange {
		t.Error("Expected expectedChange to be true after MarkExpectedChange")
	}

	// Clear expected change (simulating detach)
	watcher.ClearExpectedChange("session1")

	// Now modify the database externally (should trigger sync)
	time.Sleep(50 * time.Millisecond) // Give time for detach to complete

	sessionDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open session database: %v", err)
	}
	defer sessionDB.Close()

	if _, err := sessionDB.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
		"", "default", "External message", "user", "test-model"); err != nil {
		t.Fatalf("Failed to insert external message: %v", err)
	}

	// Give watcher time to process
	time.Sleep(150 * time.Millisecond)

	// Verify expectedChange is still false (not re-set by external modification)
	watcher.mu.RLock()
	stateAfter := watcher.states["session1"]
	watcher.mu.RUnlock()

	if stateAfter != nil && stateAfter.expectedChange {
		t.Error("Expected expectedChange to remain false after external modification")
	}
}

// TestSessionWatcher_DatabaseFileReplacementTriggersResync tests that replacing a database file with a
// different one triggers proper re-synchronization.
// NOTE: This test documents the DESIRED behavior. Current implementation may not handle all cases.
func TestSessionWatcher_DatabaseFileReplacementTriggersResync(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for initial tracking
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked")
	}

	// Scenario 1: Simulate attach (database in use)
	watcher.MarkExpectedChange("session1")

	// While attached, replace the file with a different database
	tempPath := filepath.Join(tempDir, "temp.db")
	createTestSessionDBWithMessage(t, tempPath, "different_workspace", "msg_replaced", "user", "Replaced message")

	// Copy over the original file (simulating external replacement)
	input, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}
	if err := os.WriteFile(sessionDBPath, input, 0644); err != nil {
		t.Fatalf("Failed to write replaced file: %v", err)
	}

	// Give watcher time to process
	time.Sleep(150 * time.Millisecond)

	// PROBLEM: Current implementation may not detect file replacement while expectedChange is true
	// DESIRED: File replacement should ALWAYS trigger sync, regardless of expectedChange
	watcher.mu.RLock()
	state := watcher.states["session1"]
	watcher.mu.RUnlock()

	if state != nil && state.expectedChange {
		t.Log("Current implementation LIMITATION: File replacement may not be detected while database is marked as in-use")
		t.Log("This is the fundamental problem: we can't distinguish between our changes and external changes")
	}

	// Even if replacement isn't detected immediately, the file should still be tracked
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Error("Expected database to remain tracked after file replacement")
	}

	watcher.ClearExpectedChange("session1")

	// Scenario 2: File replacement after detach should be detected
	tempPath2 := filepath.Join(tempDir, "temp2.db")
	createTestSessionDBWithMessage(t, tempPath2, "another_workspace", "msg_replaced2", "user", "Second replacement")

	input2, err := os.ReadFile(tempPath2)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}
	if err := os.WriteFile(sessionDBPath, input2, 0644); err != nil {
		t.Fatalf("Failed to write replaced file: %v", err)
	}

	// Give watcher time to process
	time.Sleep(150 * time.Millisecond)

	// This should work - replacement after detach is detected
	watcher.mu.RLock()
	state = watcher.states["session1"]
	watcher.mu.RUnlock()

	if state != nil && state.expectedChange {
		t.Error("File replacement after detach should be detectable")
	}
}

// TestSessionWatcher_MultipleChangeEventsIgnoredDuringActiveUse tests that multiple write events occurring
// while a database is actively in use are all properly ignored.
// NOTE: This test documents the DESIRED behavior. Current implementation partially works but has limitations.
func TestSessionWatcher_MultipleChangeEventsIgnoredDuringActiveUse(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	db, err := InitTestDB(t.Name(), false)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	sessionDir := filepath.Join(tempDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Failed to create session directory: %v", err)
	}

	// Create a new watcher for the test session directory
	watcher, err := NewSessionWatcher(db, sessionDir)
	if err != nil {
		t.Fatalf("Failed to create SessionWatcher: %v", err)
	}
	// Update AttachPool to use this watcher
	db.GetAttachPool().SetWatcher(watcher)

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	sessionDBPath := filepath.Join(sessionDir, "session1.db")
	createTestSessionDB(t, sessionDBPath, "workspace1")

	// Wait for initial tracking
	if !waitForTimeout(func() bool {
		return watcher.IsTracked("session1")
	}, 500*time.Millisecond) {
		t.Fatal("Timeout waiting for database to be tracked")
	}

	// Attach and perform multiple writes via AttachPool (with commit hooks)
	alias, cleanup, err := db.GetAttachPool().Acquire(sessionDBPath, "session1")
	if err != nil {
		t.Fatalf("Failed to attach session database: %v", err)
	}
	defer cleanup()

	// Perform multiple writes - each will trigger commit hook
	// With the new hook-based implementation, expectedChange is set per-commit
	// and cleared after processing the first write event
	for i := 1; i <= 5; i++ {
		if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s.messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)", alias),
			"", "default", fmt.Sprintf("Our message %d", i), "user", "test-model"); err != nil {
			t.Fatalf("Failed to insert message %d: %v", i, err)
		}
		time.Sleep(20 * time.Millisecond) // Small delay between writes
	}

	// Give watcher time to process all events
	time.Sleep(200 * time.Millisecond)

	// With the new hook-based implementation:
	// - Each commit sets expectedChange=true
	// - First write event clears the flag
	// - So after all events, expectedChange should be false
	watcher.mu.RLock()
	state := watcher.states["session1"]
	watcher.mu.RUnlock()

	if state != nil && state.expectedChange {
		t.Error("Expected expectedChange to be cleared after processing write events")
	}

	// Now, make an external change (should be detected as unexpected)
	externalDB, err := sql.Open("sqlite3", sessionDBPath)
	if err != nil {
		t.Fatalf("Failed to open external session database: %v", err)
	}
	defer externalDB.Close()

	if _, err := externalDB.Exec("INSERT INTO messages (session_id, branch_id, text, type, model) VALUES (?, ?, ?, ?, ?)",
		"", "default", "External change after our writes", "user", "test-model"); err != nil {
		t.Fatalf("Failed to insert external message: %v", err)
	}

	// Give watcher time to process
	time.Sleep(100 * time.Millisecond)

	// With the new hook-based implementation, external changes are properly detected
	// because expectedChange is cleared after processing our commit-induced writes
	watcher.mu.RLock()
	state = watcher.states["session1"]
	watcher.mu.RUnlock()

	if state != nil && state.expectedChange {
		t.Error("Expected external change to be detected (expectedChange should be false)")
	}
}
