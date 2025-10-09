//go:build linux

package fs

import (
	"os"
	"sync"
)

var removeSandboxBaseDirMutex sync.Mutex

func removeSandboxBaseDir(sessionId string) {
	removeSandboxBaseDirMutex.Lock()
	defer removeSandboxBaseDirMutex.Unlock()

	os.RemoveAll(GetSandboxBaseDir(sessionId))

	// If the directory SandboxBaseDirPrefix is totally empty, ensure that it is also removed.
	children, err := os.ReadDir(SandboxBaseDirPrefix)
	if err == nil && len(children) == 0 {
		os.RemoveAll(SandboxBaseDirPrefix)
	}
}
