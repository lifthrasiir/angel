package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	

	"github.com/bmatcuk/doublestar/v4"
)

// Sandbox provides a sandboxed environment for code execution.
type Sandbox struct {
	driveLetter string
	rootPath    string
	isDevMode   bool
	mounts      map[string]string
}

// New creates a new Sandbox instance.
// If baseDir is not provided, a temporary directory will be created.
func New(baseDir ...string) (*Sandbox, error) {
	var rootPath string
	var err error

	if len(baseDir) > 0 && baseDir[0] != "" {
		rootPath, err = filepath.Abs(baseDir[0])
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for baseDir: %w", err)
		}
		err = os.MkdirAll(rootPath, 0755)
		if err != nil {
			return nil, fmt.Errorf("failed to create base directory: %w", err)
		}
	} else {
		rootPath, err = os.MkdirTemp("", "angel-sandbox-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp directory: %w", err)
		}
	}

	driveLetter, err := findAvailableDriveLetter()
	if err != nil {
		os.RemoveAll(rootPath)
		return nil, fmt.Errorf("failed to find available drive letter: %w", err)
	}

	cmd := exec.Command("subst", driveLetter+":", rootPath)
	if err := cmd.Run(); err != nil {
		os.RemoveAll(rootPath)
		return nil, fmt.Errorf("failed to subst drive: %w", err)
	}

	isDevMode, err := checkDeveloperMode(rootPath)
	if err != nil {
		// clean up before returning error
		exec.Command("subst", driveLetter+":", "/D").Run()
		os.RemoveAll(rootPath)
		return nil, fmt.Errorf("failed to check developer mode: %w", err)
	}

	return &Sandbox{
		driveLetter: driveLetter,
		rootPath:    rootPath,
		isDevMode:   isDevMode,
		mounts:      make(map[string]string),
	}, nil
}

// Close cleans up the sandbox environment.
func (s *Sandbox) Close() error {
	var firstErr error

	for target := range s.mounts {
		if err := s.Unmount(target); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to unmount %s: %w", target, err)
		}
	}

	cmd := exec.Command("subst", s.driveLetter+":", "/D")
	if err := cmd.Run(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("failed to unsubst drive %s: %w", s.driveLetter, err)
	}

	if err := os.RemoveAll(s.rootPath); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("failed to remove root path %s: %w", s.rootPath, err)
	}

	return firstErr
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

func checkDeveloperMode(testDir string) (bool, error) {
	// Attempt to create a symbolic link. This requires administrator privileges or Developer Mode on Windows.
	symlinkPath := filepath.Join(testDir, "dev_mode_test_link")
	targetPath := filepath.Join(testDir, "dev_mode_test_target")

	if err := os.Mkdir(targetPath, 0755); err != nil {
		return false, fmt.Errorf("failed to create target dir for dev mode check: %w", err)
	}
	defer os.RemoveAll(targetPath) // Clean up the target directory

	cmd := exec.Command("cmd", "/C", "mklink", "/D", symlinkPath, targetPath)
	output, err := cmd.CombinedOutput()

	if err == nil {
		// If successful, immediately remove the link.
		os.Remove(symlinkPath)
		return true, nil
	}

	// If there's an error, check if it's because of insufficient privilege.
	// A common error message for insufficient privileges on Windows is "A required privilege is not held by the client."
	
	if exitErr, ok := err.(*exec.ExitError); ok {
		// Check for specific error code for insufficient privileges (1314 on Windows)
		if exitErr.ExitCode() == 1314 {
			return false, nil // Not an error, just not in dev mode.
		}
	}

	// For other errors, return them.
	return false, fmt.Errorf("failed to run mklink for dev mode check: %w, output: %s", err, string(output))
}

// Open implements the fs.FS interface.
func (s *Sandbox) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	fullPath := filepath.Join(s.rootPath, name)
	return os.Open(fullPath)
}

// Mount creates a symlink or junction to a directory within the sandbox.
func (s *Sandbox) Mount(source, target string) error {
	if _, ok := s.mounts[target]; ok {
		return fmt.Errorf("target '%s' is already mounted", target)
	}

	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("could not get absolute path for source '%s': %w", source, err)
	}
	if _, err := os.Stat(sourceAbs); os.IsNotExist(err) {
		return fmt.Errorf("source path '%s' does not exist", sourceAbs)
	}

	targetOnDrive := filepath.Join(s.driveLetter+":\\", target)
	targetAbs := filepath.Join(s.rootPath, target)

	// Ensure parent directory of the target exists
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for target '%s': %w", target, err)
	}

	var cmd *exec.Cmd
	if s.isDevMode {
		// Use symbolic link
		cmd = exec.Command("cmd", "/C", "mklink", "/D", targetOnDrive, sourceAbs)
	} else {
		// Use junction
		cmd = exec.Command("cmd", "/C", "mklink", "/J", targetOnDrive, sourceAbs)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount '%s' to '%s': %w\nOutput: %s", source, target, err, string(output))
	}

	s.mounts[target] = sourceAbs
	return nil
}

// Unmount removes a mounted directory.
func (s *Sandbox) Unmount(target string) error {
	if _, ok := s.mounts[target]; !ok {
		return fmt.Errorf("target '%s' is not mounted", target)
	}

	targetAbs := filepath.Join(s.rootPath, target)

	// On Windows, both symlinks and junctions can be removed with os.Remove
	if err := os.Remove(targetAbs); err != nil {
		// If it fails, it might be because it's a non-empty directory junction.
		// Try RemoveAll as a fallback.
		if err := os.RemoveAll(targetAbs); err != nil {
			return fmt.Errorf("failed to remove mount point '%s': %w", target, err)
		}
	}

	delete(s.mounts, target)
	return nil
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
