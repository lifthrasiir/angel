package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// sessionTrackState holds the tracking state for a single session DB file.
type sessionTrackState struct {
	tracked        bool          // true if the file is being tracked
	expectedChange bool          // true if a change to this file is expected (from AttachPool)
	waitChan       chan struct{} // channel to signal tracking completion (nil if already tracked or no waiters)
}

// SessionWatcher watches session database files for changes and synchronizes them with the main database.
//
// "Tracking" means:
// - Synchronizing S.sessions to main DB's sessions table
// - Synchronizing S.messages (user/model types) to main DB's messages_searchable table
//
// "Untracking" means:
// - Removing synchronized data from main DB's sessions and messages_searchable tables
//
// Files that are not tracked by SessionWatcher should NOT be attachable via AttachPool,
// because we cannot determine if a write event was expected (from AttachPool.Acquire/Release)
// or unexpected (from external process).
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
	mu            sync.RWMutex
	states        map[string]*sessionTrackState // mainSessionID -> state
	done          chan struct{}                 // Closed when eventLoop exits
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
		states:      make(map[string]*sessionTrackState),
		done:        make(chan struct{}),
	}, nil
}

// scanAndTrackExisting scans the session directory and tracks all existing .db files.
func (sw *SessionWatcher) scanAndTrackExisting() error {
	// Ensure session directory exists
	if _, err := os.Stat(sw.sessionDir); os.IsNotExist(err) {
		if err := os.MkdirAll(sw.sessionDir, 0755); err != nil {
			return fmt.Errorf("failed to create session directory: %w", err)
		}
		return nil
	}

	// Read directory
	entries, err := os.ReadDir(sw.sessionDir)
	if err != nil {
		return fmt.Errorf("failed to read session directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}

		mainSessionID := strings.TrimSuffix(entry.Name(), ".db")
		sessionDBPath := filepath.Join(sw.sessionDir, entry.Name())

		if err := sw.trackFile(mainSessionID, sessionDBPath); err != nil {
			log.Printf("SessionWatcher: Failed to track existing session DB %s: %v", mainSessionID, err)
		}
	}

	return nil
}

// trackFile synchronizes a session DB file with the main database.
// It copies S.sessions -> sessions and S.messages -> messages_searchable.
func (sw *SessionWatcher) trackFile(mainSessionID, sessionDBPath string) error {
	// Attach the session DB to sync
	if err := syncSessionToMainDB(sw.db, mainSessionID, sessionDBPath); err != nil {
		return fmt.Errorf("failed to sync session to main DB: %w", err)
	}

	sw.mu.Lock()
	state := sw.states[mainSessionID]
	if state == nil {
		state = &sessionTrackState{}
		sw.states[mainSessionID] = state
	}
	state.tracked = true

	// Signal any waiters that this file is now tracked
	if state.waitChan != nil {
		close(state.waitChan)
		state.waitChan = nil
	}
	sw.mu.Unlock()

	log.Printf("SessionWatcher: Tracked session DB %s", mainSessionID)
	return nil
}

// untrackFile removes a session's synchronized data from the main database.
// This is called when a session DB file is deleted externally.
func (sw *SessionWatcher) untrackFile(mainSessionID string) error {
	// Delete from messages_searchable (including sub-sessions)
	_, err := sw.db.Exec("DELETE FROM messages_searchable WHERE session_id = ? OR session_id LIKE ? || '.%'", mainSessionID, mainSessionID)
	if err != nil {
		return fmt.Errorf("failed to delete from messages_searchable: %w", err)
	}

	// Delete FTS indexes
	_, err = sw.db.Exec("DELETE FROM message_stems WHERE rowid IN (SELECT id FROM messages_searchable WHERE session_id = ? OR session_id LIKE ? || '.%')", mainSessionID, mainSessionID)
	if err != nil {
		log.Printf("SessionWatcher: Warning - failed to delete message_stems: %v", err)
	}

	_, err = sw.db.Exec("DELETE FROM message_trigrams WHERE rowid IN (SELECT id FROM messages_searchable WHERE session_id = ? OR session_id LIKE ? || '.%')", mainSessionID, mainSessionID)
	if err != nil {
		log.Printf("SessionWatcher: Warning - failed to delete message_trigrams: %v", err)
	}

	// Delete from sessions (including sub-sessions)
	_, err = sw.db.Exec("DELETE FROM sessions WHERE id = ? OR id LIKE ? || '.%'", mainSessionID, mainSessionID)
	if err != nil {
		return fmt.Errorf("failed to delete from sessions: %w", err)
	}

	sw.mu.Lock()
	delete(sw.states, mainSessionID)
	sw.mu.Unlock()

	log.Printf("SessionWatcher: Untracked session DB %s", mainSessionID)
	return nil
}

// IsTracked returns true if the mainSessionID is currently being tracked.
func (sw *SessionWatcher) IsTracked(mainSessionID string) bool {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	state := sw.states[mainSessionID]
	return state != nil && state.tracked
}

// WaitUntilTracked blocks until the mainSessionID is tracked or the context is cancelled.
// Returns false if the context is cancelled before tracking completes.
func (sw *SessionWatcher) WaitUntilTracked(ctx context.Context, mainSessionID string) bool {
	// Slow path: need to wait (use Lock to avoid race condition with trackFile)
	sw.mu.Lock()
	state := sw.states[mainSessionID]
	tracked := state != nil && state.tracked
	if tracked {
		sw.mu.Unlock()
		return true
	}

	if state == nil {
		state = &sessionTrackState{}
		sw.states[mainSessionID] = state
	}
	if state.waitChan == nil {
		state.waitChan = make(chan struct{})
	}
	waitChan := state.waitChan
	sw.mu.Unlock()

	select {
	case <-waitChan:
		return true
	case <-ctx.Done():
		return false
	}
}

// MarkExpectedChange marks that a change to the session DB is expected.
// This should be called when a session DB is first attached.
func (sw *SessionWatcher) MarkExpectedChange(mainSessionID string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	state := sw.states[mainSessionID]
	if state == nil {
		state = &sessionTrackState{}
		sw.states[mainSessionID] = state
	}
	state.expectedChange = true
}

// ClearExpectedChange clears the expected change flag.
// This should be called after a session DB is detached.
func (sw *SessionWatcher) ClearExpectedChange(mainSessionID string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	state := sw.states[mainSessionID]
	if state != nil {
		state.expectedChange = false
	}
}

// Start begins watching the session directory for changes.
func (sw *SessionWatcher) Start() error {
	sw.mu.Lock()

	// Watch the session directory FIRST
	if err := sw.watcher.Add(sw.sessionDir); err != nil {
		sw.mu.Unlock()
		log.Printf("SessionWatcher: Failed to watch directory %s: %v", sw.sessionDir, err)
		// Fall back to polling mode
		log.Printf("SessionWatcher: Falling back to polling mode")
		sw.polling = true
		go sw.pollingLoop()
		return nil
	}

	// Start event processing goroutine
	go sw.eventLoop()

	sw.mu.Unlock()

	// NOW scan existing files (events during scan will be captured)
	if err := sw.scanAndTrackExisting(); err != nil {
		log.Printf("SessionWatcher: Failed to scan existing session DB files: %v", err)
	}

	log.Printf("SessionWatcher: Started watching %s", sw.sessionDir)
	return nil
}

// Stop stops watching the session directory.
func (sw *SessionWatcher) Stop() error {
	sw.mu.Lock()

	if sw.polling {
		close(sw.pollingStop)
		sw.mu.Unlock()
		log.Printf("SessionWatcher: Stopped polling")
		return nil
	}

	var watcherToClose *fsnotify.Watcher
	if sw.watcher != nil {
		watcherToClose = sw.watcher
		sw.watcher = nil
	}

	if sw.debounceTimer != nil {
		sw.debounceTimer.Stop()
		sw.debounceTimer = nil
	}

	sw.mu.Unlock()

	// Close the watcher outside the lock to avoid blocking eventLoop
	if watcherToClose != nil {
		watcherToClose.Close()
	}

	// Wait for eventLoop to exit (if it was started)
	// Use select with timeout to avoid hanging if eventLoop was never started
	select {
	case <-sw.done:
		// eventLoop exited normally
	case <-time.After(1 * time.Second):
		// Timeout: eventLoop was probably never started
		log.Printf("SessionWatcher: Warning - eventLoop did not exit after 1 second")
	}

	log.Printf("SessionWatcher: Stopped watching %s", sw.sessionDir)
	return nil
}

// eventLoop processes filesystem events.
func (sw *SessionWatcher) eventLoop() {
	defer close(sw.done)

	// Check if we're in polling mode (shouldn't happen, but handle it gracefully)
	sw.mu.Lock()
	if sw.polling {
		sw.mu.Unlock()
		return
	}
	watcher := sw.watcher
	sw.mu.Unlock()

	if watcher == nil {
		return
	}

	// Debounce duration
	const debounceDuration = 100 * time.Millisecond

	// Track pending events
	pendingEvents := make(map[string]fsnotify.Event)

	for {
		select {
		case event, ok := <-watcher.Events:
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

				// Check if watcher has been stopped BEFORE extracting events
				select {
				case <-sw.done:
					sw.debounceMutex.Unlock()
					return // Watcher stopped, don't process events
				default:
					// Watcher still running, continue processing
				}

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

		case err, ok := <-watcher.Errors:
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
		log.Printf("SessionWatcher: New session DB detected: %s", mainSessionID)
		if err := sw.trackFile(mainSessionID, event.Name); err != nil {
			log.Printf("SessionWatcher: Failed to track new session DB %s: %v", mainSessionID, err)
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		// Session DB file modified
		sw.mu.Lock()
		state := sw.states[mainSessionID]
		tracked := state != nil && state.tracked
		expected := state != nil && state.expectedChange

		// If not tracked yet, treat as create (fsnotify on Windows may not send Create events)
		if !tracked {
			sw.mu.Unlock()
			log.Printf("SessionWatcher: Untracked write detected for session DB %s - tracking now", mainSessionID)
			if err := sw.trackFile(mainSessionID, event.Name); err != nil {
				log.Printf("SessionWatcher: Failed to track new session DB %s: %v", mainSessionID, err)
			}
			return
		}

		if expected {
			// This change was expected (from our commit via AttachPool)
			// Clear the flag after processing this event
			state.expectedChange = false
			sw.mu.Unlock()
			log.Printf("SessionWatcher: Expected write detected for session DB %s (flag cleared)", mainSessionID)
		} else {
			// Unexpected change - file was modified externally
			sw.mu.Unlock()
			log.Printf("SessionWatcher: Unexpected write detected for session DB %s - syncing", mainSessionID)
			if err := sw.trackFile(mainSessionID, event.Name); err != nil {
				log.Printf("SessionWatcher: Failed to sync externally modified session DB %s: %v", mainSessionID, err)
			}
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename:
		// Session DB file removed
		log.Printf("SessionWatcher: Session DB removed: %s", mainSessionID)
		if err := sw.untrackFile(mainSessionID); err != nil {
			log.Printf("SessionWatcher: Failed to untrack session DB %s: %v", mainSessionID, err)
		}
	}
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
				if err := sw.trackFile(mainSessionID, path); err != nil {
					log.Printf("SessionWatcher: Failed to track new session DB %s: %v", mainSessionID, err)
				}
			} else if modTime.After(oldModTime) {
				// Modified file
				sw.mu.Lock()
				state := sw.states[mainSessionID]
				expected := state != nil && state.expectedChange
				if expected {
					// This change was expected (from our commit via AttachPool)
					// Clear the flag after processing this event
					state.expectedChange = false
					sw.mu.Unlock()
					log.Printf("SessionWatcher: Expected write detected for session DB %s (flag cleared)", mainSessionID)
				} else {
					// Unexpected change - file was modified externally
					sw.mu.Unlock()
					log.Printf("SessionWatcher: Unexpected write detected for session DB %s - syncing", mainSessionID)
					if err := sw.trackFile(mainSessionID, path); err != nil {
						log.Printf("SessionWatcher: Failed to sync externally modified session DB %s: %v", mainSessionID, err)
					}
				}
			}
		}
	}

	// Check for deleted files
	for mainSessionID := range knownFiles {
		if _, exists := currentFiles[mainSessionID]; !exists {
			if err := sw.untrackFile(mainSessionID); err != nil {
				log.Printf("SessionWatcher: Failed to untrack session DB %s: %v", mainSessionID, err)
			}
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
	attachAlias := fmt.Sprintf("`session:%s`", mainSessionID)
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

	// Sync sessions table
	_, err = db.Exec(fmt.Sprintf(`
		INSERT OR REPLACE INTO sessions (id, created_at, last_updated_at, system_prompt, name, workspace_id, primary_branch_id, chosen_first_id)
		SELECT ? || CASE
			WHEN id = '' THEN ''
			ELSE '.' || id
		END, created_at, last_updated_at, system_prompt, name, workspace_id, primary_branch_id, chosen_first_id
		FROM %s.sessions
	`, attachAlias), mainSessionID)
	if err != nil {
		return fmt.Errorf("failed to sync sessions to main DB: %w", err)
	}

	log.Printf("SessionTracker: Synced session %s to main DB", mainSessionID)
	return nil
}
