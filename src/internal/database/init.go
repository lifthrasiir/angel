package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"path/filepath"
	"sync/atomic"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/lifthrasiir/angel/internal/env"
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

// Database is a wrapper around the main database connection with additional split-DB infrastructure.
type Database struct {
	*sql.DB
	ctx        context.Context // Context containing EnvConfig
	attachPool *AttachPool     // Pool for attaching session databases
	watcher    *SessionWatcher // Watcher for session database changes
	sessionDir string
}

// GetAttachPool returns the AttachPool for attaching session databases.
func (db *Database) GetAttachPool() *AttachPool {
	return db.attachPool
}

// Watcher returns the SessionWatcher for watching session database changes.
func (db *Database) Watcher() *SessionWatcher {
	return db.watcher
}

// Ctx returns the context containing EnvConfig for this database.
func (db *Database) Ctx() context.Context {
	return db.ctx
}

// Close closes the main database connection and stops the session watcher.
func (db *Database) Close() error {
	// Stop watcher first (if exists, may be nil in test mode)
	if db.watcher != nil {
		if err := db.watcher.Stop(); err != nil {
			log.Printf("Warning: Failed to stop session watcher: %v", err)
		}
	}

	// Close main DB
	return db.DB.Close()
}

// InitDB initializes the SQLite database connection and creates tables if they don't exist.
// This is the main database initialization function for production use.
func InitDB(ctx context.Context, dataSourceName string) (*Database, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite.
	// Avoid using SetConnMaxLifetime as it can interfere with AttachPool connections.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

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
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create tables
	if err = createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Run migrations
	if err = migrateDB(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Sync FTS tables in case of desync from crashes
	if err = syncFTSOnStartup(db); err != nil {
		log.Printf("Warning: FTS sync failed: %v", err)
		// Continue even if sync fails, FTS will be updated on next message completion
	}

	// Get session directory from EnvConfig
	envConfig, err := env.EnvConfigFromContext(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get env config: %w", err)
	}
	sessionDir := envConfig.SessionDir()

	// Create Database object (without watcher yet)
	database := &Database{
		DB:         db,
		ctx:        ctx,
		attachPool: nil, // Will be set below
		watcher:    nil, // Will be set below
		sessionDir: sessionDir,
	}

	// Initialize AttachPool for split-DB architecture
	database.attachPool = NewAttachPool(db)

	// Initialize SessionWatcher (skip for in-memory DB testing)
	var watcher *SessionWatcher
	if !envConfig.UseMemoryDB() {
		watcher, err = NewSessionWatcher(database, sessionDir)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create session watcher: %w", err)
		}
		database.watcher = watcher
		// Wire watcher to AttachPool
		database.attachPool.SetWatcher(watcher)

		// Start the watcher
		if err := watcher.Start(); err != nil {
			log.Printf("Warning: Failed to start session watcher: %v", err)
			// Continue even if watcher fails, sessions can still be managed manually
		}
	} else {
		log.Printf("SessionWatcher disabled (in-memory DB mode)")
		database.watcher = nil
	}

	log.Println("Database initialized and tables created.")
	return database, nil
}

// InitTestDB initializes an in-memory SQLite database for testing.
// This function uses a unique database name to prevent conflicts between tests.
// useMemorySessionDB controls whether session DBs use in-memory (true) or real files (false).
func InitTestDB(testName string, useMemorySessionDB bool) (*Database, error) {
	// Initialize an in-memory database for testing with unique name
	// Add an atomic counter suffix to prevent conflicts when tests run with -count=N
	counter := testDBCounter.Add(1)
	dbName := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_txlock=immediate&_foreign_keys=1&_journal_mode=WAL", testName, counter)

	// Create test EnvConfig with custom session dir for testing
	testEnvConfig := env.NewTestEnvConfig(useMemorySessionDB)
	ctx := env.ContextWithEnvConfig(context.Background(), testEnvConfig)

	db, err := InitDB(ctx, dbName)
	if err != nil {
		return nil, err
	}

	// Pooling should be disabled for :memory: databases
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// If using real filesystem for session DBs, we need to create a watcher
	// (InitDB skips watcher when UseMemoryDB() returns true, which it does for in-memory main DB)
	if !useMemorySessionDB && db.watcher == nil {
		sessionDir := testEnvConfig.SessionDir()
		watcher, err := NewSessionWatcher(db, sessionDir)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create session watcher: %w", err)
		}
		db.watcher = watcher
		db.attachPool.SetWatcher(watcher)

		// Start the watcher
		if err := watcher.Start(); err != nil {
			log.Printf("Warning: Failed to start session watcher: %v", err)
		}
	}

	return db, nil
}

// createTables creates the necessary tables in the database.
func createTables(db *sql.DB) error {
	// Drop legacy VIEW if it exists (for split-db migration)
	// This must happen before creating indexes on messages_searchable
	_, _ = db.Exec("DROP VIEW IF EXISTS messages_searchable")

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

	CREATE TABLE IF NOT EXISTS messages_searchable (
		id INTEGER PRIMARY KEY,
		text TEXT NOT NULL,
		session_id TEXT NOT NULL,
		workspace_id TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_messages_searchable_session_id ON messages_searchable(session_id);
	CREATE INDEX IF NOT EXISTS idx_messages_searchable_workspace_id ON messages_searchable(workspace_id);

	CREATE VIRTUAL TABLE IF NOT EXISTS message_stems USING fts5(
		text,
		session_id UNINDEXED,
		workspace_id UNINDEXED
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS message_trigrams USING fts5(
		text,
		session_id UNINDEXED,
		workspace_id UNINDEXED,
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
func migrateDB(ctx context.Context, db *sql.DB) error {
	// Migration 1: Convert messages_searchable from VIEW to table for split-db architecture
	// Note: For new databases, createTables already creates messages_searchable as a table
	// For existing databases, we need to drop the old VIEW and recreate as TABLE

	// First, check if messages_searchable is a VIEW (legacy database)
	var viewType string
	err := db.QueryRow("SELECT type FROM sqlite_master WHERE name='messages_searchable'").Scan(&viewType)
	if err == nil && viewType == "view" {
		// Legacy database with VIEW, need to migrate
		log.Println("Migrating messages_searchable from VIEW to TABLE...")

		// Drop the VIEW
		_, err = db.Exec("DROP VIEW IF EXISTS messages_searchable")
		if err != nil {
			return fmt.Errorf("failed to drop messages_searchable view: %w", err)
		}

		// Create TABLE (same structure as in createTables)
		_, err = db.Exec(`CREATE TABLE messages_searchable (
			id INTEGER PRIMARY KEY,
			text TEXT NOT NULL,
			session_id TEXT NOT NULL,
			workspace_id TEXT
		)`)
		if err != nil {
			return fmt.Errorf("failed to create messages_searchable table: %w", err)
		}

		// Create indexes
		_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_searchable_session_id ON messages_searchable(session_id)")
		if err != nil {
			log.Printf("Warning: Failed to create index: %v", err)
		}
		_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_searchable_workspace_id ON messages_searchable(workspace_id)")
		if err != nil {
			log.Printf("Warning: Failed to create index: %v", err)
		}

		// Migrate data from messages table
		_, err = db.Exec(`
			INSERT INTO messages_searchable (id, text, session_id, workspace_id)
			SELECT id, replace(replace(text, '<', '\x0e'), '>', '\x0f') as text, session_id,
				(SELECT workspace_id FROM sessions WHERE sessions.id = messages.session_id) as workspace_id
			FROM messages WHERE type IN ('user', 'model')
		`)
		if err != nil {
			log.Printf("Warning: Failed to migrate messages to messages_searchable: %v", err)
		} else {
			log.Println("Migration to messages_searchable completed")
		}

		// Drop and recreate FTS tables (they used external content)
		_, err = db.Exec("DROP TABLE IF EXISTS message_stems")
		if err != nil {
			log.Printf("Warning: Failed to drop message_stems: %v", err)
		}
		_, err = db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS message_stems USING fts5(
			text,
			session_id UNINDEXED,
			workspace_id UNINDEXED
		)`)
		if err != nil {
			log.Printf("Warning: Failed to create message_stems: %v", err)
		}

		_, err = db.Exec("DROP TABLE IF EXISTS message_trigrams")
		if err != nil {
			log.Printf("Warning: Failed to drop message_trigrams: %v", err)
		}
		_, err = db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS message_trigrams USING fts5(
			text,
			session_id UNINDEXED,
			workspace_id UNINDEXED,
			tokenize='trigram remove_diacritics 1'
		)`)
		if err != nil {
			log.Printf("Warning: Failed to create message_trigrams: %v", err)
		}

		log.Println("FTS tables recreated (population deferred)")
	}

	// Migration 2: Split-DB architecture migration
	// Check if migration is needed (main DB has messages but no session DBs)
	var messageCount int
	err = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&messageCount)
	if err != nil {
		log.Printf("Warning: Failed to count messages: %v", err)
		return nil
	}

	if messageCount > 0 {
		// Check if session DBs exist
		envConfig, err := env.EnvConfigFromContext(ctx)
		if err != nil {
			return fmt.Errorf("failed to get env config for migration: %w", err)
		}
		sessionDir := envConfig.SessionDir()

		// Check if any .db files exist in session directory
		files, err := filepath.Glob(filepath.Join(sessionDir, "*.db"))
		if err != nil {
			log.Printf("Warning: Failed to check session DB files: %v", err)
		} else if len(files) == 0 {
			// No session DB files exist, but we have messages - need migration
			log.Printf("Found %d messages in main DB but no session DBs, running split-DB migration...", messageCount)
			if err := MigrateToSplitDB(ctx, db, sessionDir); err != nil {
				return fmt.Errorf("split-DB migration failed: %w", err)
			}
			log.Println("Split-DB migration completed successfully")
		}
	}

	return nil
}

// syncFTSOnStartup syncs FTS tables with messages that might be missing due to crashes during streaming
func syncFTSOnStartup(db *sql.DB) error {
	log.Println("FTS sync temporarily disabled for split-db migration")
	// TODO: Re-implement FTS sync for the new messages_searchable table structure
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

// XXX: Full vacuum can panic due to the OOM
func (job *databaseJob) First() error     { return PerformVacuum(job.db, 1000) }
func (job *databaseJob) Sometimes() error { return PerformVacuum(job.db, 100) }
func (job *databaseJob) Last() error      { return nil }

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
