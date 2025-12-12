package env

import "os"

// SandboxBaseDir determines the base directory for session sandboxes
// based on the presence of go.mod file in the current directory
func SandboxBaseDir() string {
	if _, err := os.Stat("go.mod"); err == nil {
		// go.mod exists in current directory, use _angel-data/sessions
		return "_angel-data/sessions"
	}
	// No go.mod found, use angel-data/sessions
	return "angel-data/sessions"
}
