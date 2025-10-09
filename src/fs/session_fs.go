package fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
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
// It provides atomic methods to take accumulated stdout/stderr.
type RunningCommand struct {
	Cmd     *exec.Cmd
	sandbox *Sandbox
	Cancel  context.CancelFunc
	done    chan struct{} // Closed when the command goroutine finishes

	stdoutMu  sync.Mutex
	stdoutBuf bytes.Buffer
	stderrMu  sync.Mutex
	stderrBuf bytes.Buffer
}

// Done returns a channel that is closed when the command goroutine finishes.
func (rc *RunningCommand) Done() <-chan struct{} {
	return rc.done
}

// TakeStdout atomically takes all currently accumulated stdout output and clears the buffer.
func (rc *RunningCommand) TakeStdout() []byte {
	rc.stdoutMu.Lock()
	defer rc.stdoutMu.Unlock()
	if rc.stdoutBuf.Len() == 0 {
		return nil
	}
	data := rc.stdoutBuf.Bytes() // Get a copy of the bytes
	rc.stdoutBuf.Reset()
	return data
}

// TakeStderr atomically takes all currently accumulated stderr output and clears the buffer.
func (rc *RunningCommand) TakeStderr() []byte {
	rc.stderrMu.Lock()
	defer rc.stderrMu.Unlock()
	if rc.stderrBuf.Len() == 0 {
		return nil
	}
	data := rc.stderrBuf.Bytes() // Get a copy of the bytes
	rc.stderrBuf.Reset()
	return data
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

// readPipeToBuffer reads from an io.ReadCloser and writes to a bytes.Buffer,
// protecting the buffer with a sync.Mutex. It signals completion via a WaitGroup.
func readPipeToBuffer(wg *sync.WaitGroup, pipe io.ReadCloser, mu *sync.Mutex, buf *bytes.Buffer) {
	defer wg.Done()
	defer pipe.Close() // Ensure the pipe is closed when the goroutine exits

	readBuf := make([]byte, 4096) // Small buffer to read in chunks
	for {
		n, err := pipe.Read(readBuf)
		if n > 0 {
			mu.Lock()
			_, writeErr := buf.Write(readBuf[:n])
			mu.Unlock()
			if writeErr != nil {
				log.Printf("readPipeToBuffer: Error writing to buffer: %v\n", writeErr)
				break
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("readPipeToBuffer: Error reading from pipe: %v\n", err)
			}
			break
		}
	}
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

	// Add roots as read-write paths to the sandbox
	for _, root := range sf.roots {
		if err := sandbox.AddRWPath(root); err != nil {
			_ = sandbox.Close()
			return nil, fmt.Errorf("failed to add root path %s as read-write: %w", root, err)
		}
	}

	var actualWorkingDir string
	createAnonymousRoot := false

	if workingDir == "" || filepath.Clean(workingDir) == "." {
		// Default to anonymous root (sandbox root)
		actualWorkingDir = sandbox.RootPath()
		createAnonymousRoot = true // Set flag to create anonymous root if it doesn't exist
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
		absPath := filepath.Clean(workingDir)

		// Verify existence and containment within roots or sandbox root
		isValidPath := false
		for _, root := range sf.roots {
			if containsPath(root, absPath) {
				isValidPath = true
				break
			}
		}
		if !isValidPath && containsPath(sandbox.RootPath(), absPath) {
			isValidPath = true
		}

		if !isValidPath {
			_ = sandbox.Close() // Close sandbox on error
			return nil, fmt.Errorf("working directory %s is not within any accessible root or session temporary directory", absPath)
		}
		actualWorkingDir = absPath
	}

	// If it's the anonymous root and it doesn't exist, create it.
	if createAnonymousRoot {
		if _, err := os.Stat(actualWorkingDir); os.IsNotExist(err) {
			if err := os.MkdirAll(actualWorkingDir, 0755); err != nil {
				_ = sandbox.Close() // Close sandbox on error
				return nil, fmt.Errorf("failed to create anonymous root directory %s: %w", actualWorkingDir, err)
			}
		}
	}

	// Now, proceed with the original existence and directory check for all cases.
	// This check will now also cover the newly created anonymous root.
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

	cmdCtx, cancel := context.WithCancel(ctx)
	var execCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		execCmd = exec.CommandContext(cmdCtx, "cmd.exe", "/C", command)
	} else {
		execCmd = exec.CommandContext(cmdCtx, "bash", "-c", command)
	}

	execCmd.Dir = actualWorkingDir

	stdoutPipe, err := execCmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = sandbox.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := execCmd.StderrPipe()
	if err != nil {
		cancel()
		_ = stdoutPipe.Close() // Close stdout pipe as well
		_ = sandbox.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	rc := &RunningCommand{
		Cmd:     execCmd,
		sandbox: sandbox,
		Cancel:  cancel,
		done:    make(chan struct{}),
	}

	go func() {
		var wg sync.WaitGroup
		wg.Add(2)

		go readPipeToBuffer(&wg, stdoutPipe, &rc.stdoutMu, &rc.stdoutBuf)
		go readPipeToBuffer(&wg, stderrPipe, &rc.stderrMu, &rc.stderrBuf)

		err = execCmd.Start()
		if err != nil {
			log.Printf("Error starting command: %v\n", err)
			cancel()            // Cancel the context
			_ = sandbox.Close() // Close the sandbox
			// Do not close doneChan here, as it's handled by the caller if this goroutine returns early.
			return
		}

		wg.Wait()          // Wait for stdout/stderr goroutines to finish reading
		_ = execCmd.Wait() // Wait for the command to finish
		close(rc.done)     // Close doneChan AFTER Wait() completes
	}()

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
