package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsNetworkFilesystem checks if the given file path resides on a network file system.
// It tries to determine the filesystem of the directory where the file would exist,
// or the file itself if it exists. Also returns the network file system name if detected.
//
// This function handles cross-platform differences for Windows, macOS, and Linux (including WSL2 9p).
func IsNetworkFilesystem(filePath string) (bool, string, error) {
	// Get the absolute path to normalize it.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false, "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Determine the path to check. If the file doesn't exist, check its parent directory.
	// This covers scenarios where a new SQLite file is about to be created.
	pathToCheck := absPath
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		pathToCheck = filepath.Dir(absPath)
	}

	// Resolve symlinks to check the actual underlying filesystem.
	// If EvalSymlinks fails (e.g., broken symlink), proceed with pathToCheck.
	realPath, err := filepath.EvalSymlinks(pathToCheck)
	if err != nil && !os.IsNotExist(err) {
		realPath = pathToCheck // Fallback to pathToCheck if symlink resolution fails unexpectedly
	} else if os.IsNotExist(err) {
		// If the target of the symlink (or pathToCheck itself) does not exist,
		// we still need to check the filesystem of its parent.
		realPath = pathToCheck
	}

	return isNetworkFilesystemNative(realPath)
}
