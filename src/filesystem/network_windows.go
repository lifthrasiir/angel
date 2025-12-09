//go:build windows

package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe" // For Windows syscall.
)

// isNetworkFilesystemNative implements network filesystem detection for Windows.
func isNetworkFilesystemNative(filePath string) (bool, string, error) {
	// Find the root path (e.g., "C:\" for "C:\foo\bar.txt" or "\\server\share" for UNC path)
	// GetDriveTypeW needs a drive root or UNC path.
	rootPath := ""
	volumeName := filepath.VolumeName(filePath)

	if len(volumeName) > 0 { // This path has a volume name (e.g., "C:", "\\server\share")
		if len(volumeName) == 2 && volumeName[1] == ':' { // "C:"
			rootPath = volumeName + `\` // Add backslash "C:\"
		} else { // UNC path: "\\server\share"
			rootPath = volumeName
		}
	} else { // No volume name, likely a relative path or current drive.
		// Get current working directory's root to construct a full path.
		cwd, err := os.Getwd()
		if err != nil {
			return false, "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		// Combine with CWD and then get the volume name.
		fullPath := filepath.Join(cwd, filePath)
		volName := filepath.VolumeName(fullPath)
		if len(volName) > 0 {
			if len(volName) == 2 && volName[1] == ':' {
				rootPath = volName + `\`
			} else {
				rootPath = volName
			}
		} else {
			// If even after joining with CWD, no volume name (e.g., just "file.txt" with no drive),
			// it's problematic for GetDriveTypeW. Assume local in this edge case or error.
			return false, "", fmt.Errorf("could not determine root path for Windows path: %s", filePath)
		}
	}

	// Convert rootPath to UTF16 null-terminated string for Windows API.
	rootPathPtr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return false, "", fmt.Errorf("failed to convert path to UTF16Ptr: %w", err)
	}

	// Dynamically load kernel32.dll and GetDriveTypeW.
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDriveTypeW := kernel32.NewProc("GetDriveTypeW")

	if getDriveTypeW.Addr() == 0 {
		return false, "", fmt.Errorf("GetDriveTypeW not found in kernel32.dll")
	}

	// Call GetDriveTypeW.
	// Parameters: lpRootPathName (uintptr)
	// Returns: drive type (uint32)
	ret, _, errSyscall := getDriveTypeW.Call(uintptr(unsafe.Pointer(rootPathPtr)))
	// Check for a non-zero error number from syscall, but ignore "The operation completed successfully."
	if errSyscall != nil && errSyscall.Error() != "The operation completed successfully." {
		return false, "", fmt.Errorf("GetDriveTypeW failed for %s: %w", rootPath, errSyscall)
	}

	// DRIVE_REMOTE (4) indicates a network drive.
	const DRIVE_REMOTE = 4
	return uint32(ret) == DRIVE_REMOTE, "", nil
}
