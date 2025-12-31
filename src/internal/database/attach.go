package database

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
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
	mainDB      *sql.DB
	attached    map[string]*AttachedDB // alias -> AttachedDB
	lruList     []string               // aliases in LRU order (least recently used first)
	maxAttached int
	mu          sync.RWMutex
	cond        *sync.Cond
}

// NewAttachPool creates a new AttachPool for managing database attachments.
func NewAttachPool(mainDB *sql.DB) *AttachPool {
	p := &AttachPool{
		mainDB:      mainDB,
		attached:    make(map[string]*AttachedDB),
		lruList:     make([]string, 0, maxAttached),
		maxAttached: maxAttached,
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Acquire attaches a session database and returns its alias and a cleanup function.
// If the database is already attached, it increments the refcount and returns the existing alias.
// If all slots are full and in use, it blocks until a slot becomes available.
// The cleanup function should be called when done with the attachment (decrements refcount).
func (p *AttachPool) Acquire(sessionDBPath, mainSessionID string) (string, func(), error) {
	p.mu.Lock()

	// Check if already attached
	for _, attached := range p.attached {
		if attached.Path == sessionDBPath {
			attached.RefCount++
			attached.LastUsed = time.Now()
			p.updateLRU(attached.Alias)
			p.mu.Unlock()
			log.Printf("AttachPool: Reusing existing attachment %s for %s (refcount now %d)",
				attached.Alias, sessionDBPath, attached.RefCount)
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
		// Detach immediately when refcount reaches zero
		log.Printf("AttachPool: Refcount reached zero for %s, detaching", alias)
		if err := p.detachInternal(alias); err != nil {
			log.Printf("AttachPool: Error detaching %s: %v", alias, err)
		}
		// Wake up any waiters
		p.cond.Broadcast()
	} else {
		log.Printf("AttachPool: Released %s (refcount now %d)", alias, attached.RefCount)
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
	attached, exists := p.attached[alias]
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

	log.Printf("AttachPool: Detached %s (%s)", alias, attached.Path)
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
