//go:build linux

package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/sys/unix"
)

// shellEscape escapes a string for safe use in shell commands
func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	// If the string contains only safe characters, return as-is
	safe := true
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '=' || r == '.' || r == '/' || r == ',' || r == ':') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}

	// Otherwise, wrap in single quotes and escape existing quotes
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// Sandbox provides a sandboxed environment for code execution on Linux.
type Sandbox struct {
	baseDir     string
	mountPoint  string
	originalUID int
	originalGID int
}

// NewSandbox creates a new Sandbox instance.
// It creates a temporary directory but doesn't set up namespaces yet.
// Namespaces are created per-command execution in Run().
func NewSandbox(sessionDir string) (*Sandbox, error) {
	baseDir := sessionDir
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session temporary directory %s: %w", baseDir, err)
	}

	// Get current UID/GID for later use in namespaces
	originalUID := os.Getuid()
	originalGID := os.Getgid()

	return &Sandbox{
		baseDir:     baseDir,
		mountPoint:  baseDir,
		originalUID: originalUID,
		originalGID: originalGID,
	}, nil
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
	// Properly escape the base directory for shell usage
	escapedBaseDir := shellEscape(s.baseDir)

	// First, setup the sandbox filesystem in a separate process
	setupScript := fmt.Sprintf(`
		# First, make all filesystems private to avoid propagating mounts
		mount --make-rprivate /

		# Bind mount the sandbox directory to itself
		mount --bind %s %s
		mount --remount,bind %s %s

		# Mount root filesystem as read-only
		mount --remount,ro /

		# Mount common system directories as read-only
		for dir in /bin /sbin /usr /lib /lib64 /etc /var /opt /srv /media /mnt /dev /proc /sys; do
			if [ -d "$dir" ]; then
				mount --bind "$dir" "$dir" 2>/dev/null || true
				mount --remount,ro,bind "$dir" "$dir" 2>/dev/null || true
			fi
		done

		# Now execute the requested command
		exec "$@"
	`, escapedBaseDir, escapedBaseDir, escapedBaseDir, escapedBaseDir)

	// Create setup command
	setupCmd := exec.Command("/bin/sh", "-c", setupScript)

	// Pass the original command and arguments to the setup script
	// Build arguments properly escaped
	allArgs := []string{command}
	allArgs = append(allArgs, args...)

	setupCmd.Args = []string{"/bin/sh", "-c", setupScript}
	setupCmd.Args = append(setupCmd.Args, allArgs...)

	setupCmd.Dir = s.baseDir
	setupCmd.Stdout = os.Stdout
	setupCmd.Stderr = os.Stderr

	// Set up the sandbox environment for the child process
	setupCmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: s.originalUID, Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: s.originalGID, Size: 1},
		},
	}

	return setupCmd.Run()
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
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}

	// Check if path exists
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("path does not exist: %s: %w", path, err)
	}

	// Bind mount the path as read-write
	if err := unix.Mount(path, path, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to bind mount %s as read-write: %w", path, err)
	}

	// Remount to ensure it's read-write
	if err := unix.Mount(path, path, "", unix.MS_REMOUNT|unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to remount %s as read-write: %w", path, err)
	}

	return nil
}

// CheckPathAccess checks if a path is accessible within the sandbox constraints.
func (s *Sandbox) CheckPathAccess(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}

	// Normalize the path
	path = filepath.Clean(path)
	sandboxPath := filepath.Clean(s.baseDir)

	// Check if the path is within the sandbox directory
	if strings.HasPrefix(path, sandboxPath) {
		return nil // Path is within sandbox, allowed
	}

	// For Linux sandbox with namespace restrictions, we need to check if path is read-only
	// This is a simplified check - in practice, you might want to check mount flags
	return fmt.Errorf("path %s is outside sandbox and likely read-only", path)
}
