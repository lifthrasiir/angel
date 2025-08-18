package fs

import (
	"bytes" // For checking stdout/stderr
	"os"
	"path/filepath"
	"strings" // For checking string content
	"testing"
)

// Helper function to check for errors
func checkError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

// Helper function to check if an error occurred
func checkExpectedError(t *testing.T, err error, expectedSubstring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Expected an error containing \"%s\", but got no error", expectedSubstring)
	}
	if !strings.Contains(err.Error(), expectedSubstring) {
		t.Fatalf("Expected error to contain \"%s\", but got: %v", expectedSubstring, err)
	}
}

// Helper function to check if a directory exists
func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Fatalf("Directory does not exist: %s", path)
	}
	if err != nil {
		t.Fatalf("Error checking directory existence for %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("Path is not a directory: %s", path)
	}
}

// Helper function to check if a directory doesn't exist
func assertDirNotExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if !os.IsNotExist(err) {
		t.Fatalf("Directory should not exist: %s", path)
	}
}

func TestSessionFS_NewSessionFS(t *testing.T) {
	sf, err := NewSessionFS("testSession1")
	checkError(t, err, "NewSessionFS failed")

	defer removeSandboxBaseDir(sf.sessionId) // Clean up after test

	if sf == nil {
		t.Fatalf("NewSessionFS returned nil SessionFS")
	}
	if sf.sessionId != "testSession1" {
		t.Errorf("Expected sessionId 'testSession1', got '%s'", sf.sessionId)
	}

	// NewSessionFS no longer automatically adds a root.
	// The sandbox base directory is added as a root when it's first used (e.g., by Run or resolvePath).
	if len(sf.roots) != 0 {
		t.Errorf("Expected 0 roots, got %v", sf.roots)
	}

	checkError(t, sf.Close(), "sf.Close failed")
}

func TestSessionFS_AddRoot(t *testing.T) {
	sf, err := NewSessionFS("testSessionAddRoot")
	checkError(t, err, "NewSessionFS failed")
	defer func() {
		if err := sf.Close(); err != nil {
			t.Errorf("sf.Close failed: %v", err)
		}
	}()

	testRoot, err := os.MkdirTemp("", "test-root-*")
	checkError(t, err, "MkdirTemp failed")
	defer os.RemoveAll(testRoot)

	// Test adding a valid root
	err = sf.SetRoots([]string{testRoot})
	checkError(t, err, "SetRoots failed for valid root")
	if len(sf.Roots()) != 1 || sf.Roots()[0] != testRoot {
		t.Errorf("Expected roots to be [\"%s\"], but got %v", testRoot, sf.Roots())
	}

	// Test setting a non-existent path as root
	nonExistentPath := filepath.Join(testRoot, "non-existent")
	err = sf.SetRoots([]string{nonExistentPath})
	checkExpectedError(t, err, "root path does not exist")

	// Test setting a file (not a directory) as root
	testFile := filepath.Join(testRoot, "testfile.txt")
	err = os.WriteFile(testFile, []byte("hello"), 0644)
	checkError(t, err, "WriteFile failed")
	err = sf.SetRoots([]string{testFile})
	checkExpectedError(t, err, "is not a directory")

	// Test setting duplicate roots
	err = sf.SetRoots([]string{testRoot, testRoot})
	checkExpectedError(t, err, "overlapping root detected")

	// Test setting overlapping roots
	subDir := filepath.Join(testRoot, "subdir")
	checkError(t, os.Mkdir(subDir, 0755), "Mkdir failed for subdir")

	err = sf.SetRoots([]string{testRoot, subDir})
	checkExpectedError(t, err, "overlapping root detected")

	// Test setting a root that contains an existing root
	parentDir := filepath.Join(testRoot, "..") // Use ".." to get parent
	err = sf.SetRoots([]string{parentDir, testRoot})
	checkExpectedError(t, err, "overlapping root detected")
}

func TestSessionFS_FileOperations(t *testing.T) {
	sf, err := NewSessionFS("testSessionFileOps")
	checkError(t, err, "NewSessionFS failed")
	defer func() {
		if err := sf.Close(); err != nil {
			t.Errorf("sf.Close failed: %v", err)
		}
	}()

	// Get the sandbox base directory for this session
	sandboxBaseDir := GetSandboxBaseDir(sf.sessionId)
	defer removeSandboxBaseDir(sf.sessionId) // Clean up after test

	// Verify sandbox base directory doesn't exist yet
	assertDirNotExists(t, sandboxBaseDir)

	// Test WriteFile and ReadFile in sandbox base directory (relative path)
	content := []byte("hello from sandbox base directory")
	fileName := "anon_file.txt"
	err = sf.WriteFile(fileName, content)
	checkError(t, err, "WriteFile failed for anon_file.txt")

	// Verify sandbox base directory now exists
	assertDirExists(t, sandboxBaseDir)

	// Add this check: Verify anon_file.txt exists
	anonFilePath := filepath.Join(sandboxBaseDir, fileName)
	if _, err := os.Stat(anonFilePath); os.IsNotExist(err) {
		t.Fatalf("anon_file.txt was not created at %s: %v", anonFilePath, err)
	} else if err != nil {
		t.Fatalf("Error checking anon_file.txt existence: %v", err)
	}

	readContent, err := sf.ReadFile(fileName)
	checkError(t, err, "ReadFile failed for anon_file.txt")
	if !bytes.Equal(content, readContent) {
		t.Errorf("Expected content '%s', got '%s'", string(content), string(readContent))
	}

	// Test ReadDir in sandbox base directory
	dirEntries, err := sf.ReadDir(".")
	checkError(t, err, "ReadDir failed for sandboxBaseDir")
	t.Logf("Contents of %s:", sandboxBaseDir)
	for _, entry := range dirEntries {
		t.Logf("- %s (IsDir: %t)", entry.Name(), entry.IsDir())
	}
	if len(dirEntries) != 1 { // anon_file.txt
		t.Errorf("Expected 1 directory entry, got %d", len(dirEntries))
	}

	// Test file operations in a registered root
	testRoot, err := os.MkdirTemp("", "test-file-root-*")
	checkError(t, err, "MkdirTemp failed")
	defer os.RemoveAll(testRoot)
	err = sf.SetRoots([]string{testRoot})
	checkError(t, err, "SetRoots failed")

	content3 := []byte("content in registered root")
	regFileName := filepath.Join(testRoot, "reg_file.txt") // Use absolute path for file in registered root
	err = sf.WriteFile(regFileName, content3)
	checkError(t, err, "WriteFile failed for reg_file.txt")

	readContent3, err := sf.ReadFile(regFileName)
	checkError(t, err, "ReadFile failed for reg_file.txt")
	if !bytes.Equal(content3, readContent3) {
		t.Errorf("Expected content '%s', got '%s'", string(content3), string(readContent3))
	}

	// Test ReadDir in registered root
	dirEntries2, err := sf.ReadDir(testRoot) // Use absolute path for ReadDir
	checkError(t, err, "ReadDir failed for testRoot")
	if len(dirEntries2) != 1 {
		t.Errorf("Expected 1 directory entry, got %d", len(dirEntries2))
	}
}

func TestSessionFS_Run(t *testing.T) {
	sf, err := NewSessionFS("testSessionRun")
	checkError(t, err, "NewSessionFS failed")
	defer func() {
		if err := sf.Close(); err != nil {
			t.Errorf("sf.Close failed: %v", err)
		}
	}()

	// Get the sandbox base directory for this session
	sandboxBaseDir := GetSandboxBaseDir(sf.sessionId)
	defer removeSandboxBaseDir(sf.sessionId) // Clean up after test

	// --- Test Case 1: Run command with empty workingDir (defaults to anonymous root) ---
	t.Run("Run_EmptyWorkingDir", func(t *testing.T) {
		stdout, stderr, exitCode, err := sf.Run("echo hello world", "")
		checkError(t, err, "Run failed for 'echo hello world' with empty workingDir")
		if exitCode != 0 {
			t.Errorf("Expected exitCode 0, got %d", exitCode)
		}
		if !strings.Contains(stdout, "hello world") {
			t.Errorf("Expected stdout to contain 'hello world', got '%s'", stdout)
		}
		if stderr != "" {
			t.Errorf("Expected empty stderr, got '%s'", stderr)
		}
		assertDirExists(t, sandboxBaseDir) // Anonymous root should be created
	})

	// --- Test Case 2: Run command with relative workingDir ---
	t.Run("Run_RelativeWorkingDir", func(t *testing.T) {
		subDir := "test_subdir"
		// Create the subdirectory within the sandbox base directory before running the command.
		// Run function itself will not create the working directory.
		subDirPath := filepath.Join(sandboxBaseDir, subDir)
		checkError(t, os.MkdirAll(subDirPath, 0755), "MkdirAll failed for test_subdir")

		stdout, _, exitCode, err := sf.Run("dir", subDir)
		checkError(t, err, "Run failed for 'dir' with relative workingDir")
		if exitCode != 0 {
			t.Errorf("Expected exitCode 0, got %d", exitCode)
		}
		// Verify that the subdirectory is listed in the output of 'dir'
		if !strings.Contains(stdout, subDir) {
			t.Errorf("Expected stdout to contain '%s', got '%s'", subDir, stdout)
		}
		assertDirExists(t, subDirPath) // Verify the subdirectory exists
	})

	// --- Test Case 3: Run command with relative workingDir attempting to escape anonymous root ---
	t.Run("Run_RelativeWorkingDir_EscapeAnonymousRoot", func(t *testing.T) {
		_, _, exitCode, err := sf.Run("echo test", "../escaped_dir")
		checkExpectedError(t, err, "attempts to escape the anonymous root")
		if exitCode == 0 {
			t.Errorf("Expected non-zero exitCode, got %d", exitCode)
		}
	})

	// --- Test Case 4: Run command with absolute workingDir within a registered root ---
	t.Run("Run_AbsoluteWorkingDir_WithinRoot", func(t *testing.T) {
		testRoot, err := os.MkdirTemp("", "test-run-root-*")
		checkError(t, err, "MkdirTemp failed")
		defer os.RemoveAll(testRoot)

		err = sf.SetRoots([]string{testRoot})

		checkError(t, err, "AddRoot failed")

		stdout, _, exitCode, err := sf.Run("dir", testRoot)
		checkError(t, err, "Run failed for 'dir' with absolute workingDir")
		if exitCode != 0 {
			t.Errorf("Expected exitCode 0, got %d", exitCode)
		}
		if stdout == "" {
			t.Errorf("Expected non-empty stdout for 'dir' command, got empty")
		}
	})

	// --- Test Case 5: Run command with absolute workingDir outside registered roots ---
	t.Run("Run_AbsoluteWorkingDir_OutsideRoot", func(t *testing.T) {
		outsideDir, err := os.MkdirTemp("", "outside-dir-*")
		checkError(t, err, "MkdirTemp failed for outsideDir")
		defer os.RemoveAll(outsideDir)

		_, _, exitCode, err := sf.Run("echo test", outsideDir)
		checkExpectedError(t, err, "is not within any accessible root")
		if exitCode == 0 {
			t.Errorf("Expected non-zero exitCode, got %d", exitCode)
		}
	})

	// --- Test Case 6: Run command that fails ---
	t.Run("Run_CommandFails", func(t *testing.T) {
		_, stderr, exitCode, err := sf.Run("nonexistent_command", "")
		if err == nil {
			t.Errorf("Expected an error for nonexistent_command, but got none")
		}
		if exitCode == 0 {
			t.Errorf("Expected non-zero exitCode for nonexistent_command, got %d", exitCode)
		}
		if stderr == "" {
			t.Errorf("Expected non-empty stderr for nonexistent_command, got empty")
		}
	})
}

func TestSessionFS_Close(t *testing.T) {
	sf, err := NewSessionFS("testSessionClose")
	checkError(t, err, "NewSessionFS failed")

	// Get the sandbox base directory for this session
	sandboxBaseDir := GetSandboxBaseDir(sf.sessionId)
	defer removeSandboxBaseDir(sf.sessionId) // Clean up after test

	// Trigger anonymous root creation and mounting
	t.Logf("TestSessionFS_Close: Running echo test to trigger anonymous root creation.")
	// Use Run with empty workingDir to trigger sandbox creation
	_, _, _, err = sf.Run("echo test", "")
	checkError(t, err, "Run failed to trigger anonymous root creation")

	t.Logf("TestSessionFS_Close: Sandbox base directory: %s", sandboxBaseDir)
	assertDirExists(t, sandboxBaseDir)

	// Close the session FS
	t.Logf("TestSessionFS_Close: Calling sf.Close()...")
	err = sf.Close()
	t.Logf("TestSessionFS_Close: sf.Close() returned: %v", err)
	checkError(t, err, "sf.Close failed")

	t.Logf("TestSessionFS_Close: Test completed successfully.")
}
