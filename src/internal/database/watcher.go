package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SessionWatcher watches session database files for changes and tracks them.
type SessionWatcher struct {
	db            *Database
	sessionDir    string
	watcher       *fsnotify.Watcher
	debounceTimer *time.Timer
	debounceMutex sync.Mutex
	polling       bool
	pollingStop   chan bool
	events        chan fsnotify.Event
	errors        chan error
	mu            sync.Mutex
}

// NewSessionWatcher creates a new SessionWatcher for watching session database changes.
func NewSessionWatcher(db *Database, sessionDir string) (*SessionWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &SessionWatcher{
		db:          db,
		sessionDir:  sessionDir,
		watcher:     watcher,
		polling:     false,
		pollingStop: make(chan bool),
		events:      make(chan fsnotify.Event, 100),
		errors:      make(chan error, 10),
	}, nil
}

// Start begins watching the session directory for changes.
func (sw *SessionWatcher) Start() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// Watch the session directory
	if err := sw.watcher.Add(sw.sessionDir); err != nil {
		log.Printf("SessionWatcher: Failed to watch directory %s: %v", sw.sessionDir, err)
		// Fall back to polling mode
		log.Printf("SessionWatcher: Falling back to polling mode")
		sw.polling = true
		go sw.pollingLoop()
		return nil
	}

	// Start event processing goroutine
	go sw.eventLoop()

	log.Printf("SessionWatcher: Started watching %s", sw.sessionDir)
	return nil
}

// Stop stops watching the session directory.
func (sw *SessionWatcher) Stop() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.polling {
		close(sw.pollingStop)
		log.Printf("SessionWatcher: Stopped polling")
		return nil
	}

	if sw.watcher != nil {
		if err := sw.watcher.Close(); err != nil {
			return err
		}
	}

	if sw.debounceTimer != nil {
		sw.debounceTimer.Stop()
	}

	log.Printf("SessionWatcher: Stopped watching %s", sw.sessionDir)
	return nil
}

// eventLoop processes filesystem events.
func (sw *SessionWatcher) eventLoop() {
	// Debounce duration
	const debounceDuration = 100 * time.Millisecond

	// Track pending events
	pendingEvents := make(map[string]fsnotify.Event)

	for {
		select {
		case event, ok := <-sw.watcher.Events:
			if !ok {
				return
			}
			// Only process .db files
			if !strings.HasSuffix(event.Name, ".db") {
				continue
			}

			sw.debounceMutex.Lock()
			pendingEvents[event.Name] = event

			// Reset or create debounce timer
			if sw.debounceTimer != nil {
				sw.debounceTimer.Stop()
			}
			sw.debounceTimer = time.AfterFunc(debounceDuration, func() {
				sw.debounceMutex.Lock()
				eventsToProcess := make([]fsnotify.Event, 0, len(pendingEvents))
				for _, e := range pendingEvents {
					eventsToProcess = append(eventsToProcess, e)
				}
				pendingEvents = make(map[string]fsnotify.Event)
				sw.debounceMutex.Unlock()

				for _, e := range eventsToProcess {
					sw.handleEvent(e)
				}
			})
			sw.debounceMutex.Unlock()

		case err, ok := <-sw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("SessionWatcher: Error watching events: %v", err)
			// Fall back to polling on error
			sw.mu.Lock()
			if !sw.polling {
				sw.polling = true
				go sw.pollingLoop()
			}
			sw.mu.Unlock()
		}
	}
}

// handleEvent handles a single filesystem event.
func (sw *SessionWatcher) handleEvent(event fsnotify.Event) {
	// Extract main session ID from filename
	filename := filepath.Base(event.Name)
	mainSessionID := strings.TrimSuffix(filename, ".db")

	log.Printf("SessionWatcher: Event %s on %s", event.Op, event.Name)

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		// New session DB file created
		sw.handleCreate(mainSessionID, event.Name)

	case event.Op&fsnotify.Write == fsnotify.Write:
		// Session DB file modified
		sw.handleWrite(mainSessionID, event.Name)

	case event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename:
		// Session DB file removed or renamed
		sw.handleRemove(mainSessionID, event.Name)
	}
}

// handleCreate handles the creation of a new session DB file.
func (sw *SessionWatcher) handleCreate(mainSessionID, path string) {
	log.Printf("SessionWatcher: New session DB detected: %s", mainSessionID)

	// Sync messages from session DB to main DB for FTS indexing
	err := syncSessionToMainDB(sw.db, mainSessionID, path)
	if err != nil {
		log.Printf("SessionWatcher: Failed to sync session %s to main DB: %v", mainSessionID, err)
	}

	log.Printf("SessionWatcher: Session DB synced: %s", path)
}

// handleWrite handles the modification of a session DB file.
func (sw *SessionWatcher) handleWrite(mainSessionID, path string) {
	log.Printf("SessionWatcher: Session DB modified: %s", mainSessionID)
	// Session DB is accessed via AttachPool, no action needed
}

// handleRemove handles the removal of a session DB file.
func (sw *SessionWatcher) handleRemove(mainSessionID, path string) {
	log.Printf("SessionWatcher: Session DB removed: %s", mainSessionID)
	// Note: Main DB sessions catalog cleanup should be done via DeleteSession API
	// The watcher only manages the file system
}

// pollingLoop is a fallback polling mechanism for filesystems that don't support fsnotify.
func (sw *SessionWatcher) pollingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Track known files
	knownFiles := make(map[string]time.Time)

	// Initial scan
	sw.rescanSessions(knownFiles)

	for {
		select {
		case <-ticker.C:
			sw.rescanSessions(knownFiles)
		case <-sw.pollingStop:
			return
		}
	}
}

// rescanSessions scans the session directory for changes (polling fallback).
func (sw *SessionWatcher) rescanSessions(knownFiles map[string]time.Time) {
	log.Printf("SessionWatcher: Rescanning session directory (polling mode)")

	// Read directory
	entries, err := os.ReadDir(sw.sessionDir)
	if err != nil {
		log.Printf("SessionWatcher: Failed to read session directory: %v", err)
		return
	}

	// Check for new or modified files
	currentFiles := make(map[string]time.Time)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		mainSessionID := strings.TrimSuffix(entry.Name(), ".db")
		path := filepath.Join(sw.sessionDir, entry.Name())
		modTime := info.ModTime()

		currentFiles[mainSessionID] = modTime

		// Check if file is new or modified
		if oldModTime, exists := knownFiles[mainSessionID]; !exists || modTime.After(oldModTime) {
			if !exists {
				// New file
				sw.handleCreate(mainSessionID, path)
			} else if modTime.After(oldModTime) {
				// Modified file
				sw.handleWrite(mainSessionID, path)
			}
		}
	}

	// Check for deleted files
	for mainSessionID := range knownFiles {
		if _, exists := currentFiles[mainSessionID]; !exists {
			path := filepath.Join(sw.sessionDir, mainSessionID+".db")
			sw.handleRemove(mainSessionID, path)
		}
	}

	// Update known files
	for k, v := range currentFiles {
		knownFiles[k] = v
	}
}

// syncSessionToMainDB syncs messages from a session DB to the main DB's messages_searchable table.
// It attaches the session DB to the main DB and copies user/model messages.
func syncSessionToMainDB(db *Database, mainSessionID, sessionDBPath string) error {
	// Attach the session DB to the main DB
	attachAlias := "fts_sync_" + mainSessionID
	_, err := db.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS %s", sessionDBPath, attachAlias))
	if err != nil {
		return fmt.Errorf("failed to attach session DB: %w", err)
	}
	defer func() {
		db.Exec(fmt.Sprintf("DETACH DATABASE %s", attachAlias))
	}()

	// Get workspace_id from session DB
	var workspaceID string
	err = db.QueryRow(fmt.Sprintf("SELECT workspace_id FROM %s.sessions WHERE id = ''", attachAlias)).Scan(&workspaceID)
	if err != nil {
		// Default to empty workspace_id if not found
		workspaceID = ""
	}

	// Sync to messages_searchable table (only user/model messages for FTS)
	_, err = db.Exec(fmt.Sprintf(`
		INSERT OR REPLACE INTO messages_searchable (id, text, session_id, workspace_id)
		SELECT id, replace(replace(text, '<', '\x0e'), '>', '\x0f'), ? || CASE
			WHEN session_id = '' THEN ''
			ELSE '.' || session_id
		END, ?
		FROM %s.messages
		WHERE type IN ('user', 'model')
	`, attachAlias), mainSessionID, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to sync messages_searchable to main DB: %w", err)
	}

	log.Printf("SessionWatcher: Synced messages_searchable for session %s to main DB", mainSessionID)
	return nil
}
