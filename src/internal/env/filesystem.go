package env

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/lifthrasiir/angel/filesystem"
	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

// sessionFSEntry holds a SessionFS instance and its reference count.
type sessionFSEntry struct {
	sessionFS *filesystem.SessionFS
	refCount  int
}

// sessionFSMap stores SessionFS instances per session ID with reference counts.
var sessionFSMap = make(map[string]*sessionFSEntry)
var sessionFSMutex sync.Mutex // Mutex to protect sessionFSMap

// GetSessionFS retrieves or creates a SessionFS instance for a given session ID.
// It increments the reference count for the SessionFS instance.
func GetSessionFS(ctx context.Context, sessionId string) (*filesystem.SessionFS, error) {
	// Subsessions share the main session's SessionFS
	sessionId, _ = SplitSessionId(sessionId)

	sessionFSMutex.Lock()
	defer sessionFSMutex.Unlock()

	entry, ok := sessionFSMap[sessionId]
	if !ok {
		// Determine the base directory for session sandboxes
		baseDir := SandboxBaseDir()

		sf, err := filesystem.NewSessionFS(sessionId, baseDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create SessionFS for session %s: %w", sessionId, err)
		}

		// Get DB from context
		db, err := database.FromContext(ctx)
		if err != nil {
			return nil, err
		}

		// Get the session environment to retrieve roots
		roots, _, err := database.GetLatestSessionEnv(db, sessionId)
		if err != nil {
			log.Printf("getSessionFS: Failed to get session %s to retrieve roots: %v", sessionId, err)
			return nil, fmt.Errorf("failed to get session roots for session %s: %w", sessionId, err)
		}

		// Set the roots for the new SessionFS instance
		if err := sf.SetRoots(roots); err != nil {
			return nil, fmt.Errorf("failed to set roots for SessionFS for session %s: %w", sessionId, err)
		}

		entry = &sessionFSEntry{
			sessionFS: sf,
			refCount:  0, // Will be incremented below
		}
		sessionFSMap[sessionId] = entry
	}

	entry.refCount++
	return entry.sessionFS, nil
}

// ReleaseSessionFS decrements the reference count for a SessionFS instance.
// If the reference count drops to 0, the SessionFS instance is closed and removed from the map.
func ReleaseSessionFS(sessionId string) {
	// Subsessions share the main session's SessionFS
	sessionId, _ = SplitSessionId(sessionId)

	sessionFSMutex.Lock()
	defer sessionFSMutex.Unlock()

	entry, ok := sessionFSMap[sessionId]
	if !ok {
		log.Printf("Attempted to release SessionFS for non-existent session %s", sessionId)
		return
	}

	entry.refCount--

	if entry.refCount <= 0 {
		if err := entry.sessionFS.Close(); err != nil {
			log.Printf("Error closing SessionFS for session %s: %v", sessionId, err)
		}
		delete(sessionFSMap, sessionId)
	}
}
