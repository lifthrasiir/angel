//go:build windows

package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
)

var sandboxMutex sync.Mutex

// Sandbox provides a sandboxed environment for code execution.
type Sandbox struct {
	driveLetter string
	baseDir     string // Renamed from rootPath
}

// NewSandbox creates a new Sandbox instance.
// It creates a temporary directory under angel-sessions/<sessionId> and substs it.
func NewSandbox(sessionId string) (*Sandbox, error) {
	var err error

	baseDir := GetSandboxBaseDir(sessionId)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session temporary directory %s: %w", baseDir, err)
	}

	sandboxMutex.Lock()
	defer sandboxMutex.Unlock()

	driveLetter, err := findAvailableDriveLetter()
	if err != nil {
		os.RemoveAll(baseDir)
		return nil, fmt.Errorf("failed to find available drive letter: %w", err)
	}

	cmd := exec.Command("subst", driveLetter+":", baseDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(baseDir)
		return nil, fmt.Errorf("failed to subst drive: %w\nOutput: %s", err, string(output))
	}

	return &Sandbox{
		driveLetter: driveLetter,
		baseDir:     baseDir,
	}, nil
}

func (s *Sandbox) RootPath() string {
	return s.driveLetter + ":\\"
}

// BaseDir returns the actual base directory path of the sandbox.
func (s *Sandbox) BaseDir() string {
	return s.baseDir
}

// Close cleans up the sandbox environment.
func (s *Sandbox) Close() error {
	var firstErr error

	cmd := exec.Command("subst", s.driveLetter+":", "/D")
	if err := cmd.Run(); err != nil {
		firstErr = fmt.Errorf("failed to unsubst drive %s: %w", s.driveLetter, err)
	}

	return firstErr
}

const SandboxBaseDirPrefix = "angel-sessions"

// GetSandboxBaseDir returns the base directory path for a given session ID.
// This path is used for the actual temporary directory, not the subst drive.
func GetSandboxBaseDir(sessionId string) string {
	return filepath.Join(SandboxBaseDirPrefix, sessionId)
}

func findAvailableDriveLetter() (string, error) {
	var used int // Use an int as a bitmask for drive letters A-Z
	for l := 'A'; l <= 'Z'; l++ {
		_, err := os.Stat(string(l) + ":\\")
		if err == nil || !os.IsNotExist(err) {
			// Drive exists or we can't determine if it exists (permission error, etc.)
			// Treat it as used to be safe. Set the corresponding bit.
			used |= (1 << (l - 'A'))
		}
	}
	return findBestDriveLetter(used)
}

func findBestDriveLetter(used int) (string, error) {
	var currentGapStart rune = -1
	var largestGapStart rune
	var largestGapLen int

	// Find the largest gap of available drive letters
	for l := 'A'; l <= 'Z'+1; l++ {
		isUsed := false
		if l <= 'Z' {
			isUsed = (used>>(l-'A'))&1 == 1
		} else { // Treat the position after 'Z' as used to terminate the last gap
			isUsed = true
		}

		if !isUsed {
			if currentGapStart == -1 {
				currentGapStart = l
			}
		} else {
			if currentGapStart != -1 {
				currentGapLen := int(l - currentGapStart)
				if currentGapLen > largestGapLen {
					largestGapLen = currentGapLen
					largestGapStart = currentGapStart
				}
				currentGapStart = -1
			}
		}
	}

	if largestGapLen == 0 {
		return "", fmt.Errorf("no available drive letters")
	}

	// Return the middle of the largest gap
	middleRune := largestGapStart + rune(largestGapLen/2)
	return string(middleRune), nil
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
	cmd := exec.Command(command, args...)
	cmd.Dir = s.driveLetter + ":\\"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Glob returns the names of all files matching pattern in the sandbox.
func (s *Sandbox) Glob(pattern string) ([]string, error) {
	// Use doublestar.Glob which supports `**`.
	// We pass `s` as the filesystem, and it will use the `Open` method.
	matches, err := doublestar.Glob(s, pattern)
	if err != nil {
		return nil, err
	}

	// The matches are relative to the sandbox root. We need to prepend the drive letter.
	for i, match := range matches {
		matches[i] = filepath.Join(s.driveLetter+":\\", match)
	}

	return matches, nil
}

// AddRWPath adds a directory path to be mounted as read-write within the sandbox.
// On Windows, this is a no-op as the sandbox uses a subst drive.
func (s *Sandbox) AddRWPath(path string) error {
	// Windows sandbox uses subst drive, all paths within the drive are accessible
	return nil
}
