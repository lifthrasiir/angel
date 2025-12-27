package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"

	. "github.com/lifthrasiir/angel/internal/types"
)

// See https://github.com/ncruces/sqlite-vec-go/tree/main for rebuilding this binary.
//
//go:embed go-sqlite3-v0.30.3+sqlite-vec-v0.1.6.wasm
var sqliteBinary []byte

func init() {
	sqlite3.Binary = sqliteBinary
}

var (
	// testDBCounter is an atomic counter used to generate unique test database names
	testDBCounter atomic.Int64
)

// Database is a wrapper around the main database connection.
type Database struct {
	*sql.DB
}

// InitDB initializes the SQLite database connection and creates tables if they don't exist.
// This is the main database initialization function for production use.
func InitDB(dataSourceName string) (*Database, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite (especially important for :memory: databases)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute) // Should be far longer than typical test runs

	// SQLite performance and concurrency optimizations
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
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
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create tables
	if err = createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Run migrations
	if err = migrateDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Sync FTS tables in case of desync from crashes
	if err = syncFTSOnStartup(db); err != nil {
		log.Printf("Warning: FTS sync failed: %v", err)
		// Continue even if sync fails, FTS will be updated on next message completion
	}

	log.Println("Database initialized and tables created.")
	return &Database{
		DB: db,
	}, nil
}

// InitTestDB initializes an in-memory SQLite database for testing.
// This function uses a unique database name to prevent conflicts between tests.
func InitTestDB(testName string) (*Database, error) {
	// Initialize an in-memory database for testing with unique name
	// Add an atomic counter suffix to prevent conflicts when tests run with -count=N
	counter := testDBCounter.Add(1)
	dbName := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_txlock=immediate&_foreign_keys=1&_journal_mode=WAL", testName, counter)
	return InitDB(dbName)
}

// createTables creates the necessary tables in the database.
func createTables(db *sql.DB) error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		system_prompt TEXT,
		name TEXT DEFAULT '',
		workspace_id TEXT DEFAULT '',
		primary_branch_id TEXT, -- New column for primary branch
		chosen_first_id INTEGER -- Virtual root message pointer
	);

	CREATE TABLE IF NOT EXISTS branches (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		parent_branch_id TEXT,
		branch_from_message_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		pending_confirmation TEXT,
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL, -- Keep session_id to link to session
		branch_id TEXT NOT NULL,
		parent_message_id INTEGER,
		chosen_next_id INTEGER,
		text TEXT NOT NULL,
		type TEXT NOT NULL,
		attachments TEXT, -- This will store JSON array of blob hashes
		cumul_token_count INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		model TEXT NOT NULL,
		generation INTEGER DEFAULT 0,
		state TEXT NOT NULL DEFAULT '', -- Opaque state that has to be relayed to the LLM provider
		aux TEXT NOT NULL DEFAULT '',   -- Angel-internal metadata that doesn't go to the LLM provider
		indexed INTEGER DEFAULT 0 NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_messages_attachments ON messages(attachments);

	CREATE TABLE IF NOT EXISTS oauth_tokens (
		id INTEGER PRIMARY KEY,
		token_data TEXT NOT NULL,
		user_email TEXT,
		project_id TEXT,
		kind TEXT NOT NULL DEFAULT 'geminicli',
		last_used_by_model TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_email, kind)
	);

	CREATE TABLE IF NOT EXISTS workspaces (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		default_system_prompt TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS mcp_configs (
		name TEXT PRIMARY KEY,
		config_json TEXT NOT NULL,
		enabled BOOLEAN NOT NULL
	);

	CREATE TABLE IF NOT EXISTS blobs (
		id TEXT PRIMARY KEY, -- SHA-512/256 hash of the data
		data BLOB NOT NULL,
		ref_count INTEGER DEFAULT 1 NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_blobs_ref_count ON blobs(ref_count);

	CREATE TRIGGER IF NOT EXISTS increment_blob_refs
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

	CREATE TRIGGER IF NOT EXISTS decrement_blob_refs
		AFTER DELETE ON messages
		WHEN OLD.attachments IS NOT NULL AND OLD.attachments != '[]'
	BEGIN
		UPDATE blobs SET ref_count = ref_count - 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(OLD.attachments)
			WHERE json_extract(json_each.value, '$.hash') IS NOT NULL
		);
		DELETE FROM blobs WHERE ref_count <= 0;
	END;

	CREATE TRIGGER IF NOT EXISTS update_blob_refs
		AFTER UPDATE ON messages
		WHEN NEW.attachments IS NOT NULL OR OLD.attachments IS NOT NULL
	BEGIN
		-- Decrease ref count for old attachments
		UPDATE blobs SET ref_count = ref_count - 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(OLD.attachments)
			WHERE OLD.attachments IS NOT NULL AND OLD.attachments != '[]'
			AND json_extract(json_each.value, '$.hash') IS NOT NULL
		);

		-- Increase ref count for new attachments
		UPDATE blobs SET ref_count = ref_count + 1
		WHERE id IN (
			SELECT json_extract(json_each.value, '$.hash')
			FROM json_each(NEW.attachments)
			WHERE NEW.attachments IS NOT NULL AND NEW.attachments != '[]'
			AND json_extract(json_each.value, '$.hash') IS NOT NULL
		);

		-- Clean up blobs with ref_count <= 0
		DELETE FROM blobs WHERE ref_count <= 0;
	END;

	CREATE TABLE IF NOT EXISTS global_prompts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL UNIQUE,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS app_configs (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL
	);

	CREATE TABLE IF NOT EXISTS openai_configs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		endpoint TEXT NOT NULL,
		api_key TEXT,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS gemini_api_configs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		api_key TEXT,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		last_used_by_model TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS shell_commands (
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

	CREATE TABLE IF NOT EXISTS session_envs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		roots TEXT NOT NULL, -- JSON array of strings
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(session_id, generation),
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);

	CREATE VIEW IF NOT EXISTS messages_searchable AS
		SELECT id, replace(replace(text, '<', '\x0e'), '>', '\x0f') as text, session_id,
			(SELECT workspace_id FROM sessions WHERE sessions.id = messages.session_id) as workspace_id
		FROM messages WHERE type IN ('user', 'model');

	CREATE VIRTUAL TABLE IF NOT EXISTS message_stems USING fts5(
		text,
		session_id UNINDEXED,
		workspace_id UNINDEXED,
		content='messages_searchable',
		content_rowid='id',
		tokenize='porter unicode61 remove_diacritics 1'
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS message_trigrams USING fts5(
		text,
		session_id UNINDEXED,
		workspace_id UNINDEXED,
		content='messages_searchable',
		content_rowid='id',
		tokenize='trigram remove_diacritics 1'
	);
	`
	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}
	return nil
}

// migrateDB handles database schema migrations.
func migrateDB(db *sql.DB) error {
	// Keep empty for now, add migration statements as needed
	migrationStmts := []string{}

	for _, stmt := range migrationStmts {
		_, err := db.Exec(stmt)
		if err != nil {
			// Ignore "duplicate column name" errors for ALTER TABLE statements
			if strings.Contains(err.Error(), "duplicate column name") ||
				strings.Contains(err.Error(), "no such table") {
				log.Printf("Migration step ignored (expected error): %s", err.Error())
				continue
			}
			return fmt.Errorf("failed to execute migration statement '%s': %w", stmt, err)
		}
	}

	return nil
}

// syncFTSOnStartup syncs FTS tables with messages that might be missing due to crashes during streaming
func syncFTSOnStartup(db *sql.DB) error {
	log.Println("Checking for FTS desync on startup...")

	// Count missing entries in message_stems
	var missingStems int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM messages_searchable
		WHERE id NOT IN (SELECT rowid FROM message_stems)
	`).Scan(&missingStems)
	if err != nil {
		return fmt.Errorf("failed to count missing FTS stems entries: %w", err)
	}

	// Count missing entries in message_trigrams
	var missingTrigrams int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM messages_searchable
		WHERE id NOT IN (SELECT rowid FROM message_trigrams)
	`).Scan(&missingTrigrams)
	if err != nil {
		return fmt.Errorf("failed to count missing FTS trigrams entries: %w", err)
	}

	if missingStems > 0 || missingTrigrams > 0 {
		log.Printf("Found %d missing stems and %d missing trigrams, syncing...", missingStems, missingTrigrams)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin FTS sync transaction: %w", err)
		}
		defer tx.Rollback()

		// Sync missing stems entries
		if missingStems > 0 {
			_, err = tx.Exec(`
				INSERT INTO message_stems(rowid)
				SELECT id FROM messages_searchable
				WHERE id NOT IN (SELECT rowid FROM message_stems)
			`)
			if err != nil {
				return fmt.Errorf("failed to sync missing stems entries: %w", err)
			}
		}

		// Sync missing trigrams entries
		if missingTrigrams > 0 {
			_, err = tx.Exec(`
				INSERT INTO message_trigrams(rowid)
				SELECT id FROM messages_searchable
				WHERE id NOT IN (SELECT rowid FROM message_trigrams)
			`)
			if err != nil {
				return fmt.Errorf("failed to sync missing trigrams entries: %w", err)
			}
		}

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit FTS sync: %w", err)
		}

		log.Printf("FTS sync completed successfully")
	} else {
		log.Println("FTS is already in sync")
	}

	return nil
}

type databaseJob struct {
	db *Database
}

// Job returns a housekeeping job that performs periodic database maintenance tasks.
func Job(db *Database) HousekeepingJob {
	return &databaseJob{db: db}
}

func (job *databaseJob) Name() string {
	return "Periodic database maintenance"
}

func (job *databaseJob) First() error {
	// XXX: Full vacuum can panic due to the OOM
	if err := PerformVacuum(job.db, 1000); err != nil {
		return err
	}
	return nil
}

func (job *databaseJob) Sometimes() error {
	if err := PerformWALCheckpoint(job.db); err != nil {
		return err
	}
	if err := PerformVacuum(job.db, 100); err != nil {
		return err
	}
	return nil
}

func (job *databaseJob) Last() error {
	if err := PerformWALCheckpoint(job.db); err != nil {
		return err
	}
	return nil
}

// PerformWALCheckpoint triggers a WAL checkpoint.
func PerformWALCheckpoint(db *Database) error {
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("failed to perform WAL checkpoint: %w", err)
	}
	return nil
}

// PerformVacuum performs a full or incremental vacuum to reclaim space.
func PerformVacuum(db *Database, npages int) error {
	if npages <= 0 {
		if _, err := db.Exec("VACUUM"); err != nil {
			return fmt.Errorf("failed to perform full vacuum: %w", err)
		}
	} else {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA incremental_vacuum(%d)", npages)); err != nil {
			return fmt.Errorf("failed to perform incremental vacuum: %w", err)
		}
	}
	return nil
}
