//go:build windows

package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestSandboxLifecycle(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping lifecycle test in CI environment")
	}

	sessionId := "testSandboxLifecycle"
	testDir := createTestSandboxDir(sessionId)
	t.Cleanup(func() { removeTestSandboxDir(sessionId) })

	s, err := NewSandbox(testDir)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})

	drivePath := s.driveLetter + ":\\"
	if _, err := os.Stat(drivePath); os.IsNotExist(err) {
		t.Errorf("Sandbox drive is not accessible: %s", drivePath)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Failed to close sandbox: %v", err)
	}

	if _, err := os.Stat(drivePath); !os.IsNotExist(err) {
		t.Errorf("Sandbox drive was not unmounted: %s", drivePath)
	}
	if _, err := os.Stat(s.BaseDir()); err != nil {
		t.Errorf("Sandbox root directory should be retained even after close: %v", err)
	}
}

func TestSandboxFSInterface(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping FS interface test in CI environment")
	}

	sessionId := "testSandboxFSInterface"
	testDir := createTestSandboxDir(sessionId)
	t.Cleanup(func() { removeTestSandboxDir(sessionId) })

	s, err := NewSandbox(testDir)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})

	testContent := "hello world"
	testFile := "test.txt"
	err = os.WriteFile(filepath.Join(s.BaseDir(), testFile), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Use the fs.FS interface to read the file
	content, err := fs.ReadFile(s, testFile)
	if err != nil {
		t.Fatalf("Failed to read file via fs.FS: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("File content mismatch. Got '%s', want '%s'", string(content), testContent)
	}
}

func TestSandboxRunCommand(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping run command test in CI environment")
	}

	sessionId := "testSandboxRunCommand"
	testDir := createTestSandboxDir(sessionId)
	t.Cleanup(func() { removeTestSandboxDir(sessionId) })

	s, err := NewSandbox(testDir)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})

	testFile := "output.txt"
	testContent := "hello from command"

	// Use 'cmd /c' to run the echo command and redirect output
	err = s.Run("cmd", "/c", "echo "+testContent+" > "+testFile)
	if err != nil {
		t.Fatalf("Run command failed: %v", err)
	}

	// Verify the file was created with the correct content
	content, err := fs.ReadFile(s, testFile)
	if err != nil {
		t.Fatalf("Failed to read file created by command: %v", err)
	}

	// Note: 'echo' on windows might add trailing whitespace
	if strings.TrimSpace(string(content)) != testContent {
		t.Errorf("Command output mismatch. Got '%s', want '%s'", string(content), testContent)
	}
}

func TestSandboxGlob(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping glob test in CI environment")
	}

	sessionId := "testSandboxGlob"
	testDir := createTestSandboxDir(sessionId)
	t.Cleanup(func() { removeTestSandboxDir(sessionId) })

	s, err := NewSandbox(testDir)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})

	// Create some files and directories in the sandbox
	os.MkdirAll(filepath.Join(s.BaseDir(), "dir1", "subdir"), 0755)
	os.WriteFile(filepath.Join(s.BaseDir(), "dir1", "file1.txt"), []byte("file1"), 0644)
	os.WriteFile(filepath.Join(s.BaseDir(), "dir1", "subdir", "file2.log"), []byte("file2"), 0644)
	os.WriteFile(filepath.Join(s.BaseDir(), "root.txt"), []byte("root"), 0644)

	testCases := []struct {
		name     string
		pattern  string
		expected int
	}{
		{"all text files", "**/*.txt", 2},
		{"log files", "**/*.log", 1},
		{"files in dir1", "dir1/*", 2},
		{"all files", "**/*", 5},
		{"root file", "root.txt", 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := s.Glob(tc.pattern)
			if err != nil {
				t.Fatalf("Glob failed for pattern '%s': %v", tc.pattern, err)
			}
			if len(matches) != tc.expected {
				t.Errorf("Expected %d matches for pattern '%s', but got %d: %v", tc.expected, tc.pattern, len(matches), matches)
			}
		})
	}
}

func TestFindBestDriveLetter(t *testing.T) {
	// Helper function to convert a list of used drive letters to an int bitmask
	toBitmask := func(usedLetters ...rune) int {
		bitmask := 0
		for _, r := range usedLetters {
			bitmask |= (1 << (r - 'A'))
		}
		return bitmask
	}

	testCases := []struct {
		name     string
		used     int
		expected []string
		err      bool
	}{
		{
			name:     "All drives available",
			used:     0, // No bits set
			expected: []string{"M", "N"},
			err:      false,
		},
		{
			name:     "Only C used",
			used:     toBitmask('C'),
			expected: []string{"O", "P"},
			err:      false,
		},
		{
			name:     "C and D used",
			used:     toBitmask('C', 'D'),
			expected: []string{"P"},
			err:      false,
		},
		{
			name:     "C and Z used",
			used:     toBitmask('C', 'Z'),
			expected: []string{"N", "O"},
			err:      false,
		},
		{
			name:     "A and Z used",
			used:     toBitmask('A', 'Z'),
			expected: []string{"M", "N"},
			err:      false,
		},
		{
			name:     "Multiple gaps",
			used:     toBitmask('C', 'H', 'T'),
			expected: []string{"N", "O"},
			err:      false,
		},
		{
			name: "All drives used",
			used: (1 << 26) - 1, // All 26 bits set
			expected: []string{
				"",
			},
			err: true,
		},
		{
			name:     "No drives used except boundary",
			used:     toBitmask('A', 'B', 'Y', 'Z'),
			expected: []string{"M", "N"},
			err:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			letter, err := findBestDriveLetter(tc.used)
			if (err != nil) != tc.err {
				t.Fatalf("Expected error: %v, got: %v", tc.err, err)
			}
			if !tc.err { // Only check letter if no error is expected
				found := false
				for _, exp := range tc.expected {
					if letter == exp {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected drive letter to be one of %v, got: %s", tc.expected, letter)
				}
			}
		})
	}
}
