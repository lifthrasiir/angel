//go:build darwin

package fs

import (
	"fmt"
	"strings"
	"syscall"
)

// isNetworkFilesystemNative implements network filesystem detection for macOS.
func isNetworkFilesystemNative(filePath string) (bool, string, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(filePath, &stat)
	if err != nil {
		return false, "", fmt.Errorf("statfs failed for %s: %w", filePath, err)
	}

	// On macOS, stat.Fstypename provides string names like "nfs", "smbfs", "afpfs", "cifs".
	// The Fstypename field is a fixed-size byte array, need to convert and trim null terminators.
	fsTypeNameBytes := make([]byte, len(stat.Fstypename))
	for i, b := range stat.Fstypename {
		fsTypeNameBytes[i] = byte(b)
	}
	fsTypeName := string(fsTypeNameBytes)
	fsTypeName = strings.TrimRight(fsTypeName, "\x00") // Remove null terminators

	switch fsTypeName {
	case "nfs", "smbfs", "afpfs", "cifs": // afpfs is Apple File Protocol
		return true, fsTypeName, nil
	}

	return false, "", nil
}
