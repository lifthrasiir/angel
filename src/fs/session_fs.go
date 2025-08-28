package fs

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// SetRoots sets the accessible root directories for the session.
// It replaces the existing roots with the provided list, handling additions and removals.
// It checks for existence and prevents overlapping roots among the new set.
func (sf *SessionFS) SetRoots(newRoots []string) error {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Normalize and validate new roots
	normalizedNewRoots := make([]string, 0, len(newRoots))
	for _, path := range newRoots {
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
		normalizedNewRoots = append(normalizedNewRoots, absPath)
	}

	// Check for overlapping roots within the new set
	for i, root1 := range normalizedNewRoots {
		for j, root2 := range normalizedNewRoots {
			if i != j && containsPath(root1, root2) {
				return fmt.Errorf("overlapping root detected: %s with %s", root1, root2)
			}
		}
	}

	sf.roots = normalizedNewRoots
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

// RunningCommand represents a shell command that is currently running.
type RunningCommand struct {
	Cmd       *exec.Cmd
	StdoutBuf *bytes.Buffer
	StderrBuf *bytes.Buffer
	sandbox   *Sandbox
	Cancel    context.CancelFunc
	done      chan struct{}
}

// Close cleans up the resources associated with the running command.
func (rc *RunningCommand) Close() error {
	var err error

	// Attempt to kill the process if it's still running
	if rc.Cmd != nil && rc.Cmd.Process != nil && rc.Cmd.ProcessState == nil {
		if killErr := rc.Cmd.Process.Kill(); killErr != nil {
			err = fmt.Errorf("failed to kill process: %w", killErr)
		}
	}

	// Wait for the command goroutine to finish if it hasn't already
	if rc.done != nil {
		<-rc.done
	}

	// Close the sandbox
	if rc.sandbox != nil {
		if sandboxErr := rc.sandbox.Close(); sandboxErr != nil {
			if err == nil {
				err = fmt.Errorf("failed to close sandbox: %w", sandboxErr)
			} else {
				err = fmt.Errorf("%w; failed to close sandbox: %v", err, sandboxErr)
			}
		}
	}

	// Cancel the context
	if rc.Cancel != nil {
		rc.Cancel()
	}

	return err
}

// Run executes a command within the specified working directory.
// If workingDir is empty, it defaults to the anonymous root (sandbox root).
// If workingDir is a relative path, it's resolved against the anonymous root.
// If workingDir is an absolute path, it must be within a registered root or the anonymous root.
// It returns a *RunningCommand handle.
func (sf *SessionFS) Run(ctx context.Context, command string, workingDir string) (*RunningCommand, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Create sandbox for the duration of the command execution
	sandbox, err := NewSandbox(sf.sessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox for command execution: %w", err)
	}

	var actualWorkingDir string

	if workingDir == "" {
		// Default to anonymous root (sandbox root)
		actualWorkingDir = sandbox.RootPath()
	} else if !filepath.IsAbs(workingDir) {
		// Relative path, resolve against anonymous root
		resolvedPath := filepath.Clean(workingDir)
		if strings.HasPrefix(resolvedPath, "..") || resolvedPath == ".." {
			_ = sandbox.Close() // Close sandbox on error
			return nil, fmt.Errorf("relative working directory \"%s\" attempts to escape the anonymous root", workingDir)
		}
		actualWorkingDir = filepath.Join(sandbox.RootPath(), resolvedPath)
	} else {
		// Absolute path, must be within a registered root or the anonymous root
		actualWorkingDir = filepath.Clean(workingDir)

		// Verify existence and containment within roots or sandbox root
		isValidPath := false
		for _, root := range sf.roots {
			if containsPath(root, actualWorkingDir) {
				isValidPath = true
				break
			}
		}
		if !isValidPath && containsPath(sandbox.RootPath(), actualWorkingDir) {
			isValidPath = true
		}

		if !isValidPath {
			_ = sandbox.Close() // Close sandbox on error
			return nil, fmt.Errorf("working directory %s is not within any accessible root or session temporary directory", actualWorkingDir)
		}

		// Final verification that actualWorkingDir exists and is a directory
		fileInfo, err := os.Stat(actualWorkingDir)
		if err != nil {
			_ = sandbox.Close() // Close sandbox on error
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("working directory does not exist: %s", actualWorkingDir)
			}
			return nil, fmt.Errorf("failed to stat working directory: %w", err)
		}
		if !fileInfo.IsDir() {
			_ = sandbox.Close() // Close sandbox on error
			return nil, fmt.Errorf("working directory is not a directory: %s", actualWorkingDir)
		}
	}

	cmdCtx, cancel := context.WithCancel(ctx)
	var execCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		execCmd = exec.CommandContext(cmdCtx, "cmd.exe", "/C", command)
	} else {
		execCmd = exec.CommandContext(cmdCtx, "bash", "-c", command)
	}

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	execCmd.Stdout = stdoutBuf
	execCmd.Stderr = stderrBuf
	execCmd.Dir = actualWorkingDir

	doneChan := make(chan struct{})
	go func() {
		defer close(doneChan)
		_ = execCmd.Run()
	}()

	rc := &RunningCommand{
		Cmd:       execCmd,
		StdoutBuf: stdoutBuf,
		StderrBuf: stderrBuf,
		sandbox:   sandbox,
		Cancel:    cancel,
		done:      doneChan,
	}

	return rc, nil
}

// Close cleans up the SessionFS resources.
func (sf *SessionFS) Close() error {
	return nil
}

// DestroySessionFS removes the session's sandbox directory and all its contents.
func DestroySessionFS(sessionId string) error {
	sandboxDir := GetSandboxBaseDir(sessionId)
	if err := os.RemoveAll(sandboxDir); err != nil {
		return fmt.Errorf("failed to remove session sandbox directory %s: %w", sandboxDir, err)
	}
	return nil
}
