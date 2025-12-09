//go:build darwin

package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

// Sandbox provides a sandboxed environment for code execution on macOS.
type Sandbox struct {
	baseDir string
}

// NewSandbox creates a new Sandbox instance.
func NewSandbox(sessionDir string) (*Sandbox, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (s *Sandbox) RootPath() string {
	return s.baseDir + "/"
}

// BaseDir returns the actual base directory path of the sandbox.
func (s *Sandbox) BaseDir() string {
	return s.baseDir
}

// Close cleans up the sandbox environment.
func (s *Sandbox) Close() error {
	// On Linux, the namespace cleanup happens automatically when the process exits
	// We just need to clean up the temporary directory
	return os.RemoveAll(s.baseDir)
}

// Open implements the fs.FS interface.
func (s *Sandbox) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	fullPath := filepath.Join(s.baseDir, name)
	return os.Open(fullPath)
}

// Run executes a command within the sandbox.
func (s *Sandbox) Run(command string, args ...string) error {
	return fmt.Errorf("not yet implemented")
}

// Glob returns the names of all files matching pattern in the sandbox.
func (s *Sandbox) Glob(pattern string) ([]string, error) {
	// Use doublestar.Glob which supports `**`.
	// We pass `s` as the filesystem, and it will use the `Open` method.
	matches, err := doublestar.Glob(s, pattern)
	if err != nil {
		return nil, err
	}

	// The matches are relative to the sandbox root. We need to prepend the base directory.
	for i, match := range matches {
		matches[i] = filepath.Join(s.baseDir, match)
	}

	return matches, nil
}

// AddRWPath adds a directory path to be mounted as read-write within the sandbox.
func (s *Sandbox) AddRWPath(path string) error {
	return fmt.Errorf("not yet implemented")
}
