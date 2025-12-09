//go:build linux

package filesystem

import (
	"os"
	"path/filepath"
)

const TestSandboxBaseDir = "angel-test-sessions"

func createTestSandboxDir(sessionId string) string {
	testDir := filepath.Join(TestSandboxBaseDir, sessionId)
	os.MkdirAll(testDir, 0755)
	return testDir
}

func removeTestSandboxDir(sessionId string) {
	testDir := filepath.Join(TestSandboxBaseDir, sessionId)
	os.RemoveAll(testDir)

	// If the parent TestSandboxBaseDir directory is totally empty, ensure that it is also removed.
	children, err := os.ReadDir(TestSandboxBaseDir)
	if err == nil && len(children) == 0 {
		os.RemoveAll(TestSandboxBaseDir)
	}
}
