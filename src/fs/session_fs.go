package fs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// SessionFS manages file system operations for a specific session,
// including root directories, current working directory, and anonymous root.
type SessionFS struct {
	sessionId string
	// roots are absolute paths
	roots []string
	mu    sync.Mutex
}

// NewSessionFS creates a new SessionFS instance for the given session ID.
func NewSessionFS(sessionId string) (*SessionFS, error) {
	sf := &SessionFS{
		sessionId: sessionId,
		roots:     []string{},
	}

	return sf, nil
}

// AddRoot adds a new root directory to the session's accessible roots.
// It checks for existence and prevents overlapping roots.
func (sf *SessionFS) AddRoot(path string) error {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("root path does not exist: %s", absPath)
		}
		return fmt.Errorf("failed to stat root path: %w", err)
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("root path is not a directory: %s", absPath)
	}

	for _, existingRoot := range sf.roots {
		if absPath == existingRoot {
			return errors.New("root already added")
		}
		// Check for overlapping roots
		if containsPath(existingRoot, absPath) || containsPath(absPath, existingRoot) {
			return fmt.Errorf("overlapping root detected: %s with %s", absPath, existingRoot)
		}
	}

	sf.roots = append(sf.roots, absPath)
	return nil
}

// Roots returns the list of accessible root directories.
func (sf *SessionFS) Roots() []string {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Return a copy to prevent external modification
	rootsCopy := make([]string, len(sf.roots))
	copy(rootsCopy, sf.roots)
	return rootsCopy
}

// containsPath checks if 'path' is within 'root'.
func containsPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	// If rel is "." or a path that doesn't start with ".." (meaning it's a subpath)
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "..")
}

// resolvePath resolves the given path relative to the sandbox base directory if it's a relative path,
// and ensures it's within an accessible root or the session's temporary directory.
func (sf *SessionFS) resolvePath(p string) (string, error) {
	var absPath string
	if filepath.IsAbs(p) {
		absPath = p
	} else {
		// If path is relative, resolve it against the sandbox base directory.
		// This is the "anonymous root".
		absPath = filepath.Clean(filepath.Join(GetSandboxBaseDir(sf.sessionId), p))

		// Check if the relative path attempts to escape the anonymous root (e.g., contains "..")
		// This check is crucial for security and consistency.
		relPath, err := filepath.Rel(GetSandboxBaseDir(sf.sessionId), absPath)
		if err != nil {
			return "", fmt.Errorf("invalid relative path resolution: %w", err)
		}
		if strings.HasPrefix(relPath, "..") || relPath == ".." {
			return "", fmt.Errorf("relative path \"%s\" attempts to escape the anonymous root", p)
		}
	}

	// Ensure the resolved path is within an accessible root or the session's temporary directory
	isValidPath := false
	for _, root := range sf.roots {
		if containsPath(root, absPath) {
			isValidPath = true
			break
		}
	}

	if !isValidPath && containsPath(GetSandboxBaseDir(sf.sessionId), absPath) {
		isValidPath = true
	}

	if !isValidPath {
		return "", fmt.Errorf("path %s is not within any accessible root or session temporary directory", absPath)
	}

	return absPath, nil
}

// ReadFile reads the content of a file from the session's file system.
func (sf *SessionFS) ReadFile(path string) ([]byte, error) {
	resolvedPath, err := sf.resolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolvedPath)
}

// WriteFile writes data to a file in the session's file system.
func (sf *SessionFS) WriteFile(path string, data []byte) error {
	resolvedPath, err := sf.resolvePath(path)
	if err != nil {
		return err
	}
	// Ensure parent directory exists
	parentDir := filepath.Dir(resolvedPath)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", resolvedPath, err)
		}
	}
	return os.WriteFile(resolvedPath, data, 0644)
}

// ReadDir reads the directory entries from the session's file system.
func (sf *SessionFS) ReadDir(path string) ([]fs.DirEntry, error) {
	resolvedPath, err := sf.resolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(resolvedPath)
}

// verifyWorkingDir resolves and validates the working directory for command execution.
// It returns the actual absolute working directory, a Sandbox instance if created (which needs to be closed by the caller), and an error if any.
func (sf *SessionFS) verifyWorkingDir(workingDir string) (actualWorkingDir string, sandbox *Sandbox, err error) {
	if workingDir == "" {
		// Default to anonymous root
		sandbox, err = NewSandbox(sf.sessionId)
		if err != nil {
			err = fmt.Errorf("failed to create sandbox for command execution: %w", err)
			return
		}
		actualWorkingDir = sandbox.RootPath()
	} else if !filepath.IsAbs(workingDir) {
		// Relative path, resolve against anonymous root
		sandbox, err = NewSandbox(sf.sessionId)
		if err != nil {
			err = fmt.Errorf("failed to create sandbox for command execution: %w", err)
			return
		}

		resolvedPath := filepath.Clean(workingDir)
		if strings.HasPrefix(resolvedPath, "..") {
			err = fmt.Errorf("relative working directory \"%s\" attempts to escape the anonymous root", workingDir)
			return
		}

		actualWorkingDir = filepath.Join(sandbox.RootPath(), resolvedPath)
	} else {
		// Absolute path, must be within a registered root
		actualWorkingDir = filepath.Clean(workingDir)

		// Verify existence and containment within roots
		var fileInfo os.FileInfo
		fileInfo, err = os.Stat(actualWorkingDir)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("working directory does not exist: %s", actualWorkingDir)
			} else {
				err = fmt.Errorf("failed to stat working directory: %w", err)
			}
			return
		}
		if !fileInfo.IsDir() {
			err = fmt.Errorf("working directory is not a directory: %s", actualWorkingDir)
			return
		}

		isValidPath := false
		for _, root := range sf.roots {
			if containsPath(root, actualWorkingDir) {
				isValidPath = true
				break
			}
		}

		if !isValidPath {
			err = fmt.Errorf("working directory %s is not within any accessible root", actualWorkingDir)
			return
		}
	}

	// Final verification that actualWorkingDir exists and is a directory
	fileInfo, err := os.Stat(actualWorkingDir)
	if err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("working directory does not exist: %s", actualWorkingDir)
		} else {
			err = fmt.Errorf("failed to stat working directory: %w", err)
		}
		return
	}
	if !fileInfo.IsDir() {
		err = fmt.Errorf("working directory is not a directory: %s", actualWorkingDir)
		return
	}

	return
}

// Run executes a command within the specified working directory.
// If workingDir is empty, it defaults to the anonymous root.
// If workingDir is a relative path, it's resolved against the anonymous root.
// If workingDir is an absolute path, it must be within a registered root.
func (sf *SessionFS) Run(command string, workingDir string) (stdout, stderr string, exitCode int, err error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	cmd := exec.Command("cmd.exe", "/C", command) // For Windows environment

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	actualWorkingDir, sandbox, err := sf.verifyWorkingDir(workingDir)
	if sandbox != nil {
		defer sandbox.Close() // Ensure sandbox is closed after use
	}
	if err != nil {
		return "", "", 1, err
	}

	cmd.Dir = actualWorkingDir

	err = cmd.Run()

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1 // Default error code
		}
		return stdout, stderr, exitCode, fmt.Errorf("command execution failed: %w", err)
	}

	return stdout, stderr, 0, nil
}

// Close cleans up the SessionFS resources, including unmounting the sandbox drive.
func (sf *SessionFS) Close() error {
	return nil
}
