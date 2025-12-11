package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lifthrasiir/angel/editor"
	"github.com/lifthrasiir/angel/filesystem"
	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/tool"
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

// getSessionFS retrieves or creates a SessionFS instance for a given session ID.
// It increments the reference count for the SessionFS instance.
func getSessionFS(ctx context.Context, sessionId string) (*filesystem.SessionFS, error) { // Modified signature
	sessionFSMutex.Lock()
	defer sessionFSMutex.Unlock()

	entry, ok := sessionFSMap[sessionId]
	if !ok {
		// Determine the base directory for session sandboxes
		baseDir := determineSandboxBaseDir()

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

	if entry.refCount <= 0 {
		if err := entry.sessionFS.Close(); err != nil {
			log.Printf("Error closing SessionFS for session %s: %v", sessionId, err)
		}
		delete(sessionFSMap, sessionId)
	}
}

// ReadFileTool handles the read_file tool call.
func ReadFileTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	if err := tool.EnsureKnownKeys("read_file", args, "file_path"); err != nil {
		return tool.HandlerResults{}, err
	}
	absolutePath, ok := args["file_path"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("invalid file_path argument for read_file")
	}

	sf, err := getSessionFS(ctx, params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to get SessionFS for read_file: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	content, err := sf.ReadFile(absolutePath)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to read file %s: %w", absolutePath, err)
	}

	// Determine MIME type
	contentType := http.DetectContentType(content)

	// Check if it's a known binary type that should be handled as attachment
	isBinary := !strings.HasPrefix(contentType, "text/")

	if isBinary {
		db, err := database.FromContext(ctx)
		if err != nil {
			return tool.HandlerResults{}, err
		}

		hash, err := database.SaveBlob(ctx, db, content)
		if err != nil {
			return tool.HandlerResults{}, fmt.Errorf("failed to save blob for %s: %w", absolutePath, err)
		}

		fileName := filepath.Base(absolutePath)
		attachment := FileAttachment{
			FileName: fileName,
			MimeType: contentType,
			Hash:     hash,
		}

		note := "This is "
		if strings.HasPrefix(contentType, "image/") {
			note += "an image"
		} else if strings.HasPrefix(contentType, "video/") {
			note += "a video"
		} else if strings.HasPrefix(contentType, "audio/") {
			note += "an audio"
		} else if contentType == "application/pdf" {
			note += "a PDF"
		} else {
			note += "a binary"
		}
		note += " file. The actual content follows this message."

		content := fmt.Sprintf("(%s, %d bytes)", contentType, len(content))

		return tool.HandlerResults{
			Value:       map[string]interface{}{"content": content, "note": note},
			Attachments: []FileAttachment{attachment},
		}, nil
	} else {
		// It's a text file
		return tool.HandlerResults{
			Value: map[string]interface{}{"content": string(content)},
		}, nil
	}
}

// WriteFileTool handles the write_file tool call.
func WriteFileTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	if err := tool.EnsureKnownKeys("write_file", args, "file_path", "content"); err != nil {
		return tool.HandlerResults{}, err
	}
	filePath, ok := args["file_path"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("invalid file_path argument for write_file")
	}
	newContentStr, ok := args["content"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("invalid content argument for write_file")
	}

	// Check if the path is absolute
	if !params.ConfirmationReceived && filepath.IsAbs(filePath) {
		// If it's an absolute path, signal for user confirmation
		// We don't perform the write here, it will be done after confirmation
		return tool.HandlerResults{}, &tool.PendingConfirmation{
			Data: map[string]interface{}{
				"tool":      "write_file",
				"file_path": filePath,
				"content":   newContentStr,
			},
		}
	}

	sf, err := getSessionFS(ctx, params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to get SessionFS for write_file: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	// 1. Read old content
	oldContentStr := ""
	oldContentBytes, err := sf.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return tool.HandlerResults{}, fmt.Errorf("failed to read old content of file %s: %w", filePath, err)
		}
		// File does not exist, oldContentStr remains empty
	} else {
		oldContentStr = string(oldContentBytes)
	}

	// 2. Write new content
	err = sf.WriteFile(filePath, []byte(newContentStr))
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	// 3. Calculate diff using the new editor package
	unifiedDiff := editor.Diff([]byte(oldContentStr), []byte(newContentStr), 3)

	return tool.HandlerResults{Value: map[string]interface{}{"status": "success", "unified_diff": unifiedDiff}}, nil
}

// ListDirectoryTool handles the list_directory tool call.
func ListDirectoryTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	if err := tool.EnsureKnownKeys("list_directory", args, "path"); err != nil {
		return tool.HandlerResults{}, err
	}
	path, ok := args["path"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("invalid path argument for list_directory")
	}

	sf, err := getSessionFS(ctx, params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to get SessionFS for list_directory: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	entries, err := sf.ReadDir(path)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to list directory %s: %w", path, err)
	}

	var fileNames []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		fileNames = append(fileNames, name)
	}

	return tool.HandlerResults{Value: map[string]interface{}{"files": fileNames}}, nil
}

// registerFSTools registers filesystem-related tools
func registerFSTools(tools *tool.Tools) {
	tools.Register(tool.Definition{
		Name:        "list_directory",
		Description: "Lists a directory. Can be also used to access the session-local anonymous working directory, which is useful for e.g. storing `NOTES.md`.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"path": {
					Type:        TypeString,
					Description: "The path to the directory to list. Both absolute and relative paths are supported. Relative paths are resolved against the session's anonymous working directory.",
				},
			},
			Required: []string{"path"},
		},
		Handler: ListDirectoryTool,
	})

	tools.Register(tool.Definition{
		Name:        "read_file",
		Description: "Reads a file. Can be also used to access the session-local anonymous working directory, which is useful for e.g. storing `NOTES.md`. Image, audio, video and PDF files are automatically converted to a readable format if possible.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"file_path": {
					Type:        TypeString,
					Description: "The path to the file to read. Both absolute and relative paths are supported. Relative paths are resolved against the session's anonymous working directory.",
				},
			},
			Required: []string{"file_path"},
		},
		Handler: ReadFileTool,
	})

	tools.Register(tool.Definition{
		Name:        "write_file",
		Description: "Writes content to a specified file. Can be also used to access the session-local anonymous working directory, which is useful for e.g. storing `NOTES.md` to keep track of things. Any updates return a unified diff, which is crucial for verifying your edits and, more importantly, implicitly reveals any unexpected external modifications, allowing for swift detection and adaptation.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"file_path": {
					Type:        TypeString,
					Description: "The path to the file to write to. Both absolute and relative paths are supported. Relative paths are resolved against the session's anonymous working directory.",
				},
				"content": {
					Type:        TypeString,
					Description: "The content to write to the file.",
				},
			},
			Required: []string{"file_path", "content"},
		},
		Handler: WriteFileTool,
	})
}
