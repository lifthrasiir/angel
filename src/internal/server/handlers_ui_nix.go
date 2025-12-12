//go:build !windows

package server

import (
	"errors"
)

// getWindowsDrives returns a list of available Windows drives
// This is a stub implementation for non-Windows platforms
func getWindowsDrives() ([]DirectoryInfo, error) {
	return nil, errors.New("Windows drives are only available on Windows platform")
}
