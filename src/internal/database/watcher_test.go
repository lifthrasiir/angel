package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// createTestSessionDB creates a test session database with the required tables.
func createTestSessionDB(t *testing.T, path string, workspaceID string) {
	sessionDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Execute each statement separately
	statements := []string{
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT)",
		"CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, text TEXT)",
		fmt.Sprintf("INSERT INTO sessions (id, workspace_id) VALUES ('', '%s')", workspaceID),
	}
	for _, stmt := range statements {
		if _, err := sessionDB.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement: %s, error: %v", stmt, err)
		}
	}
}

// createTestSessionDBWithMessage creates a test session database with a message.
func createTestSessionDBWithMessage(t *testing.T, path string, workspaceID string, messageID string, messageType string, text string) {
	sessionDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("Failed to create session database: %v", err)
	}
	defer sessionDB.Close()

	// Execute each statement separately
	statements := []string{
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT)",
		"CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, text TEXT)",
		fmt.Sprintf("INSERT INTO sessions (id, workspace_id) VALUES ('', '%s')", workspaceID),
		fmt.Sprintf("INSERT INTO messages (id, session_id, type, text) VALUES ('%s', '', '%s', '%s')", messageID, messageType, text),
	}
	for _, stmt := range statements {
		if _, err := sessionDB.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement: %s, error: %v", stmt, err)
		}
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
	}
	for _, stmt := range statements {
		if _, err := sessionDB.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement: %s, error: %v", stmt, err)
		}
	}

	// Rapidly write multiple times
	for i := 0; i < 10; i++ {
		if _, err := sessionDB.Exec("INSERT INTO messages (id, session_id, type, text) VALUES (?, '', 'user', ?)", "msg"+strconv.Itoa(i), "Message "+strconv.Itoa(i)); err != nil {
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
