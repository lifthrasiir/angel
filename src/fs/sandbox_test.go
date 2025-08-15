package sandbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycle(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping lifecycle test in CI environment")
	}

	s, err := New()
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

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
	if _, err := os.Stat(s.rootPath); !os.IsNotExist(err) {
		t.Errorf("Sandbox root directory was not removed: %s", s.rootPath)
	}
}

func TestFSInterface(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping FS interface test in CI environment")
	}

	s, err := New()
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	defer s.Close()

	testContent := "hello world"
	testFile := "test.txt"
	err = os.WriteFile(filepath.Join(s.rootPath, testFile), []byte(testContent), 0644)
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

func TestMountUnmount(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping mount/unmount test in CI environment")
	}

	// 1. Create sandbox
	s, err := New()
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	defer s.Close()

	// 2. Create a source directory with a file to mount
	sourceDir, err := os.MkdirTemp("", "mount-source-*")
	if err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}
	defer os.RemoveAll(sourceDir)

	testContent := "mounted file content"
	testFile := "mounted.txt"
	err = os.WriteFile(filepath.Join(sourceDir, testFile), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write to source file: %v", err)
	}

	// 3. Mount the source directory into the sandbox
	mountTarget := "my-project"
	err = s.Mount(sourceDir, mountTarget)
	if err != nil {
		t.Fatalf("Failed to mount directory: %v", err)
	}

	// 4. Verify the mounted file can be accessed
	mountedFilePathInSandbox := filepath.Join(mountTarget, testFile)
	content, err := fs.ReadFile(s, mountedFilePathInSandbox)
	if err != nil {
		t.Fatalf("Failed to read mounted file: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("Mounted file content mismatch. Got '%s', want '%s'", string(content), testContent)
	}

	// 5. Unmount the directory
	err = s.Unmount(mountTarget)
	if err != nil {
		t.Fatalf("Failed to unmount directory: %v", err)
	}

	// 6. Verify the mounted file is no longer accessible
	_, err = fs.ReadFile(s, mountedFilePathInSandbox)
	if err == nil {
		t.Error("Expected error reading from unmounted path, but got none")
	}
}

func TestRunCommand(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping run command test in CI environment")
	}

	s, err := New()
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	defer s.Close()

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

func TestGlob(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping glob test in CI environment")
	}

	s, err := New()
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	defer s.Close()

	// Create some files and directories in the sandbox
	os.MkdirAll(filepath.Join(s.rootPath, "dir1", "subdir"), 0755)
	os.WriteFile(filepath.Join(s.rootPath, "dir1", "file1.txt"), []byte("file1"), 0644)
	os.WriteFile(filepath.Join(s.rootPath, "dir1", "subdir", "file2.log"), []byte("file2"), 0644)
	os.WriteFile(filepath.Join(s.rootPath, "root.txt"), []byte("root"), 0644)

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
		used     int // Changed to int
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
