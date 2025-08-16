package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	fsPkg "github.com/lifthrasiir/angel/fs"
)

// sessionFSEntry holds a SessionFS instance and its reference count.
type sessionFSEntry struct {
	sessionFS *fsPkg.SessionFS
	refCount  int
}

// sessionFSMap stores SessionFS instances per session ID with reference counts.
var sessionFSMap = make(map[string]*sessionFSEntry)
var sessionFSMutex sync.Mutex // Mutex to protect sessionFSMap

// getSessionFS retrieves or creates a SessionFS instance for a given session ID.
// It increments the reference count for the SessionFS instance.
func getSessionFS(sessionId string) (*fsPkg.SessionFS, error) {
	sessionFSMutex.Lock()
	defer sessionFSMutex.Unlock()

	entry, ok := sessionFSMap[sessionId]
	if !ok {
		sf, err := fsPkg.NewSessionFS(sessionId)
		if err != nil {
			return nil, fmt.Errorf("failed to create SessionFS for session %s: %w", sessionId, err)
		}
		entry = &sessionFSEntry{
			sessionFS: sf,
			refCount:  0, // Will be incremented below
		}
		sessionFSMap[sessionId] = entry
	}

	entry.refCount++
	log.Printf("SessionFS for %s: refCount incremented to %d", sessionId, entry.refCount)
	return entry.sessionFS, nil
}

// releaseSessionFS decrements the reference count for a SessionFS instance.
// If the reference count drops to 0, the SessionFS instance is closed and removed from the map.
func releaseSessionFS(sessionId string) {
	sessionFSMutex.Lock()
	defer sessionFSMutex.Unlock()

	entry, ok := sessionFSMap[sessionId]
	if !ok {
		log.Printf("Attempted to release SessionFS for non-existent session %s", sessionId)
		return
	}

	entry.refCount--
	log.Printf("SessionFS for %s: refCount decremented to %d", sessionId, entry.refCount)

	if entry.refCount <= 0 {
		log.Printf("Closing and removing SessionFS for session %s (refCount is %d)", sessionId, entry.refCount)
		if err := entry.sessionFS.Close(); err != nil {
			log.Printf("Error closing SessionFS for session %s: %v", sessionId, err)
		}
		delete(sessionFSMap, sessionId)
	}
}

// ReadFileTool handles the read_file tool call.
func ReadFileTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	if err := EnsureKnownKeys("read_file", args, "file_path"); err != nil {
		return nil, err
	}
	absolutePath, ok := args["file_path"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid file_path argument for read_file")
	}

	sf, err := getSessionFS(params.SessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to get SessionFS for read_file: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	content, err := sf.ReadFile(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", absolutePath, err)
	}

	return map[string]interface{}{"content": string(content)}, nil
}

// WriteFileTool handles the write_file tool call.
func WriteFileTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	if err := EnsureKnownKeys("write_file", args, "file_path", "content"); err != nil {
		return nil, err
	}
	filePath, ok := args["file_path"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid file_path argument for write_file")
	}
	content, ok := args["content"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid content argument for write_file")
	}

	sf, err := getSessionFS(params.SessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to get SessionFS for write_file: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	err = sf.WriteFile(filePath, []byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	return map[string]interface{}{"status": "success"}, nil
}

// ListDirectoryTool handles the list_directory tool call.
func ListDirectoryTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	if err := EnsureKnownKeys("list_directory", args, "path"); err != nil {
		return nil, err
	}
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid path argument for list_directory")
	}

	sf, err := getSessionFS(params.SessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to get SessionFS for list_directory: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	entries, err := sf.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %s: %w", path, err)
	}

	var fileNames []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		fileNames = append(fileNames, name)
	}

	return map[string]interface{}{"files": fileNames}, nil
}
