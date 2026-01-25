package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lifthrasiir/angel/internal/env"
	. "github.com/lifthrasiir/angel/internal/types"
)

// createSessionSchemaSQL is the SQL schema for session databases.
// It uses the 'S.' prefix for table/index/trigger definitions, but not for references within the same DB.
const createSessionSchemaSQL = `
	CREATE TABLE IF NOT EXISTS S.sessions (
		id TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		system_prompt TEXT,
		name TEXT DEFAULT '',
		workspace_id TEXT DEFAULT '',
		primary_branch_id TEXT,
		chosen_first_id INTEGER
	);

	CREATE TABLE IF NOT EXISTS S.branches (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		parent_branch_id TEXT,
		branch_from_message_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		pending_confirmation TEXT
	);

	CREATE TABLE IF NOT EXISTS S.messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		branch_id TEXT NOT NULL,
		parent_message_id INTEGER,
		chosen_next_id INTEGER,
		text TEXT NOT NULL,
		type TEXT NOT NULL,
		attachments TEXT,
		cumul_token_count INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		model TEXT NOT NULL,
		generation INTEGER DEFAULT 0,
		state TEXT NOT NULL DEFAULT '',
		aux TEXT NOT NULL DEFAULT '',
		indexed INTEGER DEFAULT 0 NOT NULL
	);

	CREATE INDEX IF NOT EXISTS S.idx_messages_attachments ON messages(attachments);

	CREATE TABLE IF NOT EXISTS S.blobs (
		id TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		ref_count INTEGER DEFAULT 1 NOT NULL
	);

	CREATE INDEX IF NOT EXISTS S.idx_blobs_ref_count ON blobs(ref_count);

	CREATE TRIGGER IF NOT EXISTS S.increment_blob_refs
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

	CREATE TRIGGER IF NOT EXISTS S.decrement_blob_refs
		AFTER DELETE ON messages
		WHEN OLD.attachments IS NOT NULL AND OLD.attachments != '[]'
	BEGIN
		UPDATE blobs SET ref_count = ref_count - 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(OLD.attachments)
			WHERE json_extract(json_each.value, '$.hash') IS NOT NULL
		);
		DELETE FROM blobs WHERE blobs.ref_count <= 0;
	END;

	CREATE TRIGGER IF NOT EXISTS S.update_blob_refs
		AFTER UPDATE ON messages
		WHEN NEW.attachments != OLD.attachments
	BEGIN
		UPDATE blobs SET ref_count = ref_count - 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(OLD.attachments)
			WHERE OLD.attachments IS NOT NULL AND OLD.attachments != '[]'
			AND json_extract(json_each.value, '$.hash') IS NOT NULL
		);

		UPDATE blobs SET ref_count = ref_count + 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(NEW.attachments)
			WHERE NEW.attachments IS NOT NULL AND NEW.attachments != '[]'
			AND json_extract(json_each.value, '$.hash') IS NOT NULL
		);

		DELETE FROM blobs WHERE blobs.ref_count <= 0;
	END;

	CREATE TABLE IF NOT EXISTS S.shell_commands (
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

	CREATE TABLE IF NOT EXISTS S.session_envs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		roots TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(session_id, generation)
	);
`

// InitSessionDBForMigration initializes a SQLite database connection for a session DB.
// This is only used for migration purposes.
// Session DBs are stored in angel-data/sessions/<mainSessionId>.db
func InitSessionDBForMigration(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open session database: %w", err)
	}

	// Configure connection pool for SQLite
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	// SQLite performance and concurrency optimizations
	pragmas := []string{
		"PRAGMA journal_mode=DELETE",
		"PRAGMA synchronous=FULL",
		"PRAGMA busy_timeout=30000",
		"PRAGMA cache_size=-65536",
		"PRAGMA mmap_size=268435456",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA foreign_keys=ON",
		"PRAGMA auto_vacuum=INCREMENTAL",
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
		return nil, fmt.Errorf("failed to connect to session database: %w", err)
	}

	// Create session-specific schema (main DB is not attached yet, so we use raw SQL)
	// In this context, there's no attached DB, so S. would refer to 'main' which is the session DB itself
	// We need to replace S. with nothing for this standalone session DB
	schemaSQL := strings.ReplaceAll(createSessionSchemaSQL, "S.", "")
	if _, err = db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create session schema: %w", err)
	}

	log.Printf("Session database initialized: %s", path)
	return db, nil
}

// GetSessionDBPath returns the file path for a session DB.
// e.g., "foo" -> "angel-data/sessions/foo.db"
// For testing with UseMemoryDB(), returns ":memory:".
// Uses EnvConfig from context to determine the session directory.
// If context doesn't have EnvConfig, tries to get it from database in context.
func GetSessionDBPath(ctx context.Context, mainSessionID string) (string, error) {
	envConfig, err := env.EnvConfigFromContext(ctx)
	if err != nil {
		// Try to get EnvConfig from database in context
		db, dbErr := FromContext(ctx)
		if dbErr == nil && db.ctx != nil {
			envConfig, err = env.EnvConfigFromContext(db.ctx)
			if err != nil {
				return "", fmt.Errorf("failed to get env config from db context: %w", err)
			}
		} else {
			return "", fmt.Errorf("failed to get env config: %w", err)
		}
	}

	// For testing, use in-memory databases
	if envConfig.UseMemoryDB() {
		return ":memory:", nil
	}

	sessionDir := envConfig.SessionDir()
	return filepath.Join(sessionDir, mainSessionID+".db"), nil
}

// GetSessionDBPathFromDB returns the file path for a session DB using the database's internal context.
// This is a convenience function for when you have a *Database but no context.
func GetSessionDBPathFromDB(db *Database, mainSessionID string) (string, error) {
	if db.ctx == nil {
		return "", fmt.Errorf("database has no context")
	}
	return GetSessionDBPath(db.ctx, mainSessionID)
}

// ToLocalSessionID converts a full session ID to a local ID for session DB storage.
// e.g., "foo.bar.baz" -> "bar.baz", "foo" -> "" (empty string for main session)
func ToLocalSessionID(sessionID string) string {
	_, suffix := SplitSessionId(sessionID)
	if suffix == "" {
		return ""
	}
	return suffix[1:] // suffix[0] is always '.'
}

// ToFullSessionID reconstructs a full session ID from main session ID and local ID.
// e.g., "foo" + "bar.baz" -> "foo.bar.baz", "foo" + "" -> "foo"
func ToFullSessionID(mainSessionID, localSessionID string) string {
	if localSessionID == "" {
		return mainSessionID
	}
	return mainSessionID + "." + localSessionID
}

// GetSessionApplicationID retrieves the application_id from a session database file.
// Returns 0 if the file doesn't exist or application_id is not set.
// Reads directly from the SQLite file format at offset 68 (big-endian 4-byte integer).
// See: https://sqlite.org/fileformat2.html
func GetSessionApplicationID(dbPath string) (int, error) {
	// Check if file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return 0, nil
	}

	// Open file for reading
	f, err := os.Open(dbPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open session DB file: %w", err)
	}
	defer f.Close()

	// Read application_id at offset 68 (4 bytes, big-endian)
	// SQLite header: 100 bytes total, application_id is at offset 68
	buf := make([]byte, 4)
	_, err = f.ReadAt(buf, 68)
	if err != nil {
		return 0, fmt.Errorf("failed to read application_id from file: %w", err)
	}

	// Convert big-endian bytes to int
	appID := int(buf[0])<<24 | int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
	return appID, nil
}

// SetSessionApplicationID sets the application_id for a session database file.
// Uses the SQLite PRAGMA interface to write the application_id.
func SetSessionApplicationID(dbPath string, appID int) error {
	// Open database directly
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open session DB: %w", err)
	}
	defer db.Close()

	// Set application_id using PRAGMA
	_, err = db.Exec(fmt.Sprintf("PRAGMA application_id = %d", appID))
	if err != nil {
		return fmt.Errorf("failed to set application_id: %w", err)
	}

	return nil
}

// IsSessionArchived checks if a session is archived by examining its application_id.
func IsSessionArchived(dbPath string) (bool, error) {
	appID, err := GetSessionApplicationID(dbPath)
	if err != nil {
		return false, err
	}
	return appID == ApplicationIDArchived, nil
}

// TriggerWatcherSync manually triggers a watcher sync for a session DB file.
// This is useful when the application_id is changed and we want to immediately update the main DB.
func TriggerWatcherSync(db *Database, mainSessionID, sessionDBPath string) error {
	if db.watcher == nil {
		return nil // No watcher, nothing to sync
	}
	// Use the watcher's trackFile method to sync
	if err := db.watcher.trackFile(mainSessionID, sessionDBPath); err != nil {
		return fmt.Errorf("failed to trigger watcher sync: %w", err)
	}
	return nil
}
