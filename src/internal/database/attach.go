package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
)

const maxAttached = 10 // SQLite ATTACH DATABASE limit

// AttachedDB represents an attached database with reference counting.
type AttachedDB struct {
	Alias         string
	Path          string
	MainSessionID string
	RefCount      int
	LastUsed      time.Time
}

// AttachPool manages ATTACH DATABASE operations with LRU eviction and reference counting.
type AttachPool struct {
	mainDB          *sql.DB
	attached        map[string]*AttachedDB // alias -> AttachedDB
	lruList         []string               // aliases in LRU order (least recently used first)
	maxAttached     int
	watcher         *SessionWatcher // Watcher for session database synchronization
	mu              sync.RWMutex
	cond            *sync.Cond
	hooksRegistered bool // Whether commit/rollback hooks are registered
}

// NewAttachPool creates a new AttachPool for managing database attachments.
func NewAttachPool(mainDB *sql.DB) *AttachPool {
	p := &AttachPool{
		mainDB:      mainDB,
		attached:    make(map[string]*AttachedDB),
		lruList:     make([]string, 0, maxAttached),
		maxAttached: maxAttached,
		watcher:     nil, // Will be set later via SetWatcher
	}
	p.cond = sync.NewCond(&p.mu)

	// Register commit/rollback hooks to track when we make changes
	if err := p.registerHooks(); err != nil {
		log.Printf("AttachPool: Failed to register hooks: %v", err)
	}

	return p
}

// registerHooks registers commit and rollback hooks on the main database connection.
func (p *AttachPool) registerHooks() error {
	conn, err := p.mainDB.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	return conn.Raw(func(driverConn interface{}) error {
		// Use reflection to access the embedded *sqlite3.Conn field
		// driverConn is *driver.conn which embeds *sqlite3.Conn
		val := reflect.ValueOf(driverConn).Elem()
		embeddedConnField := val.FieldByName("Conn")
		if !embeddedConnField.IsValid() {
			return fmt.Errorf("could not find Conn field in driverConn")
		}

		sqliteConn, ok := embeddedConnField.Interface().(*sqlite3.Conn)
		if !ok {
			return fmt.Errorf("Conn field is not *sqlite3.Conn")
		}

		// Register commit hook
		sqliteConn.CommitHook(func() bool {
			p.mu.Lock()
			for _, attached := range p.attached {
				if p.watcher != nil && attached.RefCount > 0 {
					p.watcher.MarkExpectedChange(attached.MainSessionID)
				}
			}
			p.mu.Unlock()
			return true // Allow commit to proceed
		})

		// Register rollback hook
		sqliteConn.RollbackHook(func() {
			p.mu.Lock()
			for _, attached := range p.attached {
				if p.watcher != nil && attached.RefCount > 0 {
					p.watcher.ClearExpectedChange(attached.MainSessionID)
				}
			}
			p.mu.Unlock()
		})

		p.mu.Lock()
		p.hooksRegistered = true
		p.mu.Unlock()

		log.Printf("AttachPool: Registered commit/rollback hooks")
		return nil
	})
}

// SetWatcher sets the SessionWatcher for this AttachPool.
// This is called after the watcher is created in InitDB.
func (p *AttachPool) SetWatcher(watcher *SessionWatcher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watcher = watcher
}

// Acquire attaches a session database and returns its alias and a cleanup function.
// If the database is already attached, it increments the refcount and returns the existing alias.
// If all slots are full and in use, it blocks until a slot becomes available.
// The cleanup function should be called when done with the attachment (decrements refcount).
//
// IMPORTANT: Files that are not tracked by SessionWatcher will block until they are tracked.
// This prevents attaching files that were modified externally without synchronization.
func (p *AttachPool) Acquire(sessionDBPath, mainSessionID string) (string, func(), error) {
	return p.acquireInternal(sessionDBPath, mainSessionID, false)
}

// acquireInternal is the internal implementation of Acquire with an option to skip waiting for tracking.
// skipWait should only be true for new files being created (e.g., in CreateSessionDB).
func (p *AttachPool) acquireInternal(sessionDBPath, mainSessionID string, skipWait bool) (string, func(), error) {
	p.mu.Lock()

	// Check if this file is tracked (skip for in-memory databases or when watcher is nil)
	if p.watcher != nil && !skipWait && sessionDBPath != ":memory:" {
		// Need to release lock to call WaitUntilTracked (which has its own lock)
		// Use a background context for indefinite wait
		p.mu.Unlock()
		if !p.watcher.WaitUntilTracked(context.Background(), mainSessionID) {
			return "", nil, fmt.Errorf("session DB %s tracking cancelled", mainSessionID)
		}
		p.mu.Lock()
	}

	// Check if already attached
	for _, attached := range p.attached {
		if attached.Path == sessionDBPath {
			attached.RefCount++
			attached.LastUsed = time.Now()
			p.updateLRU(attached.Alias)
			p.mu.Unlock()
			cleanup := func() {
				p.Release(attached.Alias)
			}
			return attached.Alias, cleanup, nil
		}
	}

	// Need to attach a new database
	// Wait until we have a slot available
	for len(p.attached) >= p.maxAttached {
		// Try to evict an unused database
		if err := p.evictLRUInternal(); err != nil {
			// All databases are in use, wait for someone to release
			log.Printf("AttachPool: All %d slots in use, waiting for available slot...", p.maxAttached)
			p.cond.Wait()
		}
	}

	// Generate a unique alias
	alias := fmt.Sprintf("`session:%s`", mainSessionID)

	// Attach the database
	if _, err := p.mainDB.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS %s", sessionDBPath, alias)); err != nil {
		p.mu.Unlock()
		return "", nil, fmt.Errorf("failed to attach database %s as %s: %w", sessionDBPath, alias, err)
	}

	// Configure pragmas for the attached database
	// Note: journal_mode and synchronous are database-specific settings
	pragmas := []string{
		fmt.Sprintf("PRAGMA %s.journal_mode=DELETE", alias),
		fmt.Sprintf("PRAGMA %s.synchronous=FULL", alias),
	}

	for _, pragma := range pragmas {
		if _, err := p.mainDB.Exec(pragma); err != nil {
			// Rollback: detach on pragma error
			p.mainDB.Exec(fmt.Sprintf("DETACH DATABASE %s", alias))
			p.mu.Unlock()
			return "", nil, fmt.Errorf("failed to set pragma '%s': %w", pragma, err)
		}
	}

	// Create AttachedDB entry
	attached := &AttachedDB{
		Alias:         alias,
		Path:          sessionDBPath,
		MainSessionID: mainSessionID,
		RefCount:      1,
		LastUsed:      time.Now(),
	}
	p.attached[alias] = attached
	p.lruList = append(p.lruList, alias)

	p.mu.Unlock()

	log.Printf("AttachPool: Attached %s as %s (refcount=1, total=%d/%d)",
		sessionDBPath, alias, len(p.attached), p.maxAttached)

	cleanup := func() {
		p.Release(alias)
	}
	return alias, cleanup, nil
}

// Release decrements the refcount for an attached database.
// If refcount reaches zero, it marks the database for potential eviction and wakes up waiters.
// The database will be detached either when:
// 1. A housekeeping job determines it's old enough (10 minutes by default)
// 2. All slots are full and a new attachment is needed (LRU eviction)
func (p *AttachPool) Release(alias string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	attached, exists := p.attached[alias]
	if !exists {
		log.Printf("AttachPool: Warning - Release called for non-existent alias %s", alias)
		return
	}

	attached.RefCount--
	attached.LastUsed = time.Now()

	if attached.RefCount <= 0 {
		// Mark as unused, but don't detach immediately; wake up any waiters
		p.cond.Broadcast()
	}
}

// evictLRUInternal finds and detaches the least recently used database with refcount == 0.
// Caller must hold p.mu lock.
func (p *AttachPool) evictLRUInternal() error {
	// Find LRU entry with refcount == 0
	for _, alias := range p.lruList {
		attached := p.attached[alias]
		if attached.RefCount == 0 {
			log.Printf("AttachPool: Evicting LRU database %s (%s)", alias, attached.Path)
			return p.detachInternal(alias)
		}
	}

	return fmt.Errorf("all %d attached databases are in use (refcount > 0)", len(p.attached))
}

// detachInternal executes DETACH DATABASE for the given alias.
// Also removes it from the LRU list.
// Caller must hold p.mu lock.
func (p *AttachPool) detachInternal(alias string) error {
	_, exists := p.attached[alias]
	if !exists {
		return fmt.Errorf("alias %s not found in attached map", alias)
	}

	if _, err := p.mainDB.Exec(fmt.Sprintf("DETACH DATABASE %s", alias)); err != nil {
		return fmt.Errorf("failed to detach database %s: %w", alias, err)
	}

	// Remove from map
	delete(p.attached, alias)

	// Remove from LRU list
	for i, a := range p.lruList {
		if a == alias {
			p.lruList = append(p.lruList[:i], p.lruList[i+1:]...)
			break
		}
	}

	return nil
}

// updateLRU moves the alias to the end of the LRU list (most recently used).
// Caller must hold p.mu lock.
func (p *AttachPool) updateLRU(alias string) {
	// Remove from current position
	for i, a := range p.lruList {
		if a == alias {
			p.lruList = append(p.lruList[:i], p.lruList[i+1:]...)
			break
		}
	}
	// Add to end (most recently used)
	p.lruList = append(p.lruList, alias)
}

// WithSessionDB is a convenience function that acquires a session database attachment,
// runs the provided function with the alias, and ensures cleanup is done.
func (p *AttachPool) WithSessionDB(sessionDBPath, mainSessionID string, fn func(alias string) error) error {
	alias, cleanup, err := p.Acquire(sessionDBPath, mainSessionID)
	if err != nil {
		return err
	}
	defer cleanup()

	return fn(alias)
}

// ForceDetachByMainSessionID forcefully detaches a session database by its main session ID.
// This is used when deleting a session database file, to ensure it's detached before deletion.
// It ignores the refcount and forcibly detaches the database.
func (p *AttachPool) ForceDetachByMainSessionID(mainSessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find the alias for this mainSessionID
	targetAlias := ""
	for alias, attached := range p.attached {
		if attached.MainSessionID == mainSessionID {
			targetAlias = alias
			break
		}
	}

	if targetAlias == "" {
		// Not attached, nothing to do
		return nil
	}

	attached := p.attached[targetAlias]
	if attached.RefCount > 0 {
		log.Printf("AttachPool: Warning - Force detaching session %s with refcount=%d", mainSessionID, attached.RefCount)
	}

	log.Printf("AttachPool: Force detaching session %s (%s)", mainSessionID, attached.Path)
	return p.detachInternal(targetAlias)
}

// Stats returns statistics about the attach pool.
func (p *AttachPool) Stats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	inUse := 0
	for _, attached := range p.attached {
		if attached.RefCount > 0 {
			inUse++
		}
	}

	return map[string]interface{}{
		"total_attached": len(p.attached),
		"in_use":         inUse,
		"max_attached":   p.maxAttached,
		"lru_list":       p.lruList,
	}
}

// Housekeeping detaches databases that have been unused (refcount == 0) for longer than the specified duration.
// This should be called periodically by a housekeeping job.
// Returns the number of databases detached.
func (p *AttachPool) Housekeeping(olderThan time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	detached := 0
	cutoff := time.Now().Add(-olderThan)

	// Find all databases with refcount == 0 that are older than the cutoff
	for alias, attached := range p.attached {
		if attached.RefCount == 0 && attached.LastUsed.Before(cutoff) {
			log.Printf("AttachPool: Housekeeping detaching %s (unused since %v)",
				alias, attached.LastUsed.Format(time.RFC3339))
			if err := p.detachInternal(alias); err != nil {
				log.Printf("AttachPool: Error detaching %s during housekeeping: %v", alias, err)
			} else {
				detached++
			}
		}
	}

	return detached
}
