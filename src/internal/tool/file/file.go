package file

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lifthrasiir/angel/editor"
	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// ReadFileTool handles the read_file tool call.
func ReadFileTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	if err := tool.EnsureKnownKeys("read_file", args, "file_path"); err != nil {
		return tool.HandlerResults{}, err
	}
	absolutePath, ok := args["file_path"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("invalid file_path argument for read_file")
	}

	sf, err := database.GetSessionFS(ctx, params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to get SessionFS for read_file: %w", err)
	}
	defer database.ReleaseSessionFS(params.SessionId)

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
		sdb, err := db.WithSession(params.SessionId)
		if err != nil {
			return tool.HandlerResults{}, err
		}
		defer sdb.Close()

		hash, err := database.SaveBlob(ctx, sdb, content)
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

	sf, err := database.GetSessionFS(ctx, params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to get SessionFS for write_file: %w", err)
	}
	defer database.ReleaseSessionFS(params.SessionId)

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

	sf, err := database.GetSessionFS(ctx, params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to get SessionFS for list_directory: %w", err)
	}
	defer database.ReleaseSessionFS(params.SessionId)

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

var listDirectoryTool = tool.Definition{
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
}

var readFileTool = tool.Definition{
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
}

var writeFileTool = tool.Definition{
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
}

var AllTools = []tool.Definition{
	listDirectoryTool,
	readFileTool,
	writeFileTool,
}
