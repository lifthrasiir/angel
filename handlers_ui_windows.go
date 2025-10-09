//go:build windows

package main

import (
	"os"
)

// getWindowsDrives returns a list of available Windows drives
func getWindowsDrives() ([]DirectoryInfo, error) {
	var drives []DirectoryInfo

	// Iterate through all possible drive letters A-Z
	for i := 'A'; i <= 'Z'; i++ {
		letter := string(i)
		drivePath := letter + ":\\"
		if _, err := os.Stat(drivePath); err == nil {
			drives = append(drives, DirectoryInfo{
				Name:     letter + ":",
				Path:     drivePath,
				IsParent: false,
				IsRoot:   true,
			})
		}
	}

	return drives, nil
}
