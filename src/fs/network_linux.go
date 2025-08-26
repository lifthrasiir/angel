//go:build linux

package fs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// isNetworkFilesystemNative implements network filesystem detection for Linux.
func isNetworkFilesystemNative(filePath string) (bool, string, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(filePath, &stat)
	if err != nil {
		return false, "", fmt.Errorf("statfs failed for %s: %w", filePath, err)
	}

	// Common network filesystem magic numbers on Linux
	const (
		NFS_SUPER_MAGIC_V2 = 0x6969 // Older NFS
		// NFS_SUPER_MAGIC (Linux specific for NFSv3/v4) and V9FS_MAGIC (Plan 9 File System, used by 9p in WSL2)
		// both share the same magic number: 0x01021994.
		// Since both are network filesystems, we treat this magic number as indicating a network FS.
		NETWORK_COMMON_NFS_9P_MAGIC = 0x01021994
		CIFS_MAGIC_NUMBER           = 0xFF534D42
		SMB_SUPER_MAGIC             = 0xFE534D42 // Another common SMB magic
	)

	// Check against known network filesystem types using magic numbers.
	// If any of these match, it's a network filesystem.
	switch stat.Type {
	case NFS_SUPER_MAGIC_V2:
		return true, "nfs", nil
	case NETWORK_COMMON_NFS_9P_MAGIC:
		return true, "nfs or 9p", nil
	case CIFS_MAGIC_NUMBER:
		return true, "cifs", nil
	case SMB_SUPER_MAGIC:
		return true, "smbfs", nil
	}

	// Fallback/refinement: For WSL2 9p and other cases, parse /proc/mounts.
	// This is particularly useful for `/mnt/*` paths in WSL2, where '9p' type is explicitly listed.
	// This check happens if the magic number didn't already identify it as a network FS.
	if strings.HasPrefix(filePath, "/mnt/") { // Only do /proc/mounts check if it's potentially a WSL2 mount
		file, err := os.Open("/proc/mounts")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not open /proc/mounts for 9p check: %v\n", err)
			return false, "", nil // Not failing the entire check, but can't verify via /proc/mounts
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		// Track the most specific mount point found that matches the path
		// and its filesystem type.
		var bestMatchFsType string
		longestMatchLen := 0

		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				mountPoint := fields[1] // Second field is the mount point
				fsType := fields[2]     // Third field is the filesystem type

				// Normalize mountPoint to ensure correct comparison (e.g., trailing slash)
				mountPoint = filepath.Clean(mountPoint)

				// Check if filePath is on this mountPoint
				// Using strings.HasPrefix and ensuring it's a full path component or exact match.
				if strings.HasPrefix(filePath, mountPoint) &&
					(len(filePath) == len(mountPoint) || filePath[len(mountPoint)] == os.PathSeparator) {
					// If this mountPoint is longer/more specific, update the best match
					if len(mountPoint) > longestMatchLen {
						longestMatchLen = len(mountPoint)
						bestMatchFsType = fsType
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Error reading /proc/mounts: %v\n", err)
		}

		// Check if the best matching filesystem type is a known network type from /proc/mounts
		switch bestMatchFsType {
		case "9p", "nfs", "cifs", "smbfs":
			return true, bestMatchFsType, nil
		}
	}

	return false, "", nil
}
