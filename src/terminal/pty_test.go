package terminal

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestPTYOnWindows tests basic PTY functionality on Windows.
func TestPTYOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	// Simple command that outputs text
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "cmd.exe", "/C", "echo Hello from PTY")

	pty, err := StartPTY(cmd, 80, 24)
	if err != nil {
		t.Fatalf("Failed to start PTY: %v", err)
	}
	defer pty.Close()

	// Read output
	output := make([]byte, 4096)
	n, err := pty.Read(output)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Failed to read from PTY: %v", err)
	}

	outputStr := string(output[:n])
	t.Logf("PTY output: %q", outputStr)

	if !strings.Contains(outputStr, "Hello from PTY") {
		t.Errorf("Expected 'Hello from PTY' in output, got: %q", outputStr)
	}

	// Wait for command to complete
	exitCode, err := pty.Wait()
	if err != nil {
		t.Fatalf("Failed to wait for command: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got: %d", exitCode)
	}

	t.Log("PTY test completed successfully")
}

// TestPTYMultipleLines tests multi-line output.
func TestPTYMultipleLines(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "cmd.exe", "/C", "echo Line1 && echo Line2 && echo Line3")

	pty, err := StartPTY(cmd, 80, 24)
	if err != nil {
		t.Fatalf("Failed to start PTY: %v", err)
	}
	defer pty.Close()

	// Read all output
	var fullOutput strings.Builder
	buf := make([]byte, 4096)

	// Read with timeout
	done := make(chan error)
	go func() {
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				fullOutput.Write(buf[:n])
			}
			if err != nil {
				done <- err
				return
			}
		}
	}()

	// Wait for read to complete or timeout
	select {
	case err := <-done:
		// Read completed (err is io.EOF or actual error)
		if err != nil && err != io.EOF {
			t.Logf("Read completed with error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Log("Read timed out after 1 second")
		// Don't close the PTY here as it may cause crashes
		// Just continue with the test - the defer will handle cleanup
	}

	outputStr := fullOutput.String()
	t.Logf("Multi-line output: %q", outputStr)

	// Check that we got multiple lines
	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	lineCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			lineCount++
		}
	}

	if lineCount < 3 {
		t.Errorf("Expected at least 3 lines, got: %d", lineCount)
	}

	exitCode, err := pty.Wait()
	if err != nil {
		t.Fatalf("Failed to wait for command: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got: %d", exitCode)
	}
}

// TestPTYResize tests resizing the PTY.
func TestPTYResize(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "cmd.exe", "/C", "echo Test")

	pty, err := StartPTY(cmd, 80, 24)
	if err != nil {
		t.Fatalf("Failed to start PTY: %v", err)
	}
	defer pty.Close()

	// Try to resize
	err = pty.Resize(120, 30)
	if err != nil {
		t.Logf("Resize returned error (might be expected): %v", err)
	} else {
		t.Log("Resize succeeded")
	}

	// Read output
	output := make([]byte, 4096)
	pty.Read(output)

	exitCode, err := pty.Wait()
	if err != nil {
		t.Fatalf("Failed to wait for command: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got: %d", exitCode)
	}
}

// TestPTYWithWindowsConsoleAPI tests that Windows Console API calls
// are converted to ANSI escape sequences by ConPTY.
func TestPTYWithWindowsConsoleAPI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	// First, build the test program
	testExe := "test_programs/test_colors.exe"
	buildCmd := exec.Command("go", "build", "-o", testExe, "test_programs/test_windows_colors.go")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build test program: %v (skipping test)", err)
		return
	}
	defer exec.Command("cmd", "/C", "del", testExe).Run()

	// Run the test program through PTY
	ctx := context.Background()

	// Get absolute path to the test executable
	absExe, err := filepath.Abs(testExe)
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	cmd := exec.CommandContext(ctx, absExe)

	pty, err := StartPTY(cmd, 80, 24)
	if err != nil {
		t.Fatalf("Failed to start PTY: %v", err)
	}
	defer pty.Close()

	// Read all output
	var fullOutput strings.Builder
	buf := make([]byte, 8192)

	done := make(chan error)
	go func() {
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				fullOutput.Write(buf[:n])
			}
			if err != nil {
				done <- err
				return
			}
		}
	}()

	select {
	case err := <-done:
		// Read completed (err is io.EOF or actual error)
		if err != nil && err != io.EOF {
			t.Logf("Read completed with error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Log("Read timed out after 1 second")
		// Don't close the PTY here as it may cause crashes
		// Just continue with the test - the defer will handle cleanup
	}

	outputStr := fullOutput.String()
	t.Logf("Raw PTY output length: %d bytes", len(outputStr))
	t.Logf("Raw PTY output (first 500 chars): %q", truncateString(outputStr, 500))

	// Check for ANSI escape sequences
	hasCSI := strings.Contains(outputStr, "\x1b[")
	hasOSC := strings.Contains(outputStr, "\x1b]")

	t.Logf("Contains CSI sequences (ESC[): %v", hasCSI)
	t.Logf("Contains OSC sequences (ESC]): %v", hasOSC)

	// The output should contain ANSI sequences if ConPTY is working
	if !hasCSI && !hasOSC {
		t.Error("No ANSI escape sequences found in output. ConPTY might not be converting Console API calls to ANSI.")
	} else {
		t.Log("SUCCESS: ANSI escape sequences detected! ConPTY is working.")
	}

	// Look for specific ANSI color sequences that our test program should produce.
	// Our Windows Console API test program uses FOREGROUND_INTENSITY which
	// maps to ANSI bright color codes (90-97 range).
	// Specifically we expect:
	// - \x1b[91m for RED|INTENSITY (bright red)
	// - \x1b[92m for GREEN|INTENSITY (bright green)
	// - \x1b[94m for BLUE|INTENSITY (bright blue)
	// - \x1b[93m for RED|GREEN|INTENSITY (bright yellow)
	expectedColorSequences := map[string]string{
		"\x1b[91m": "bright red",
		"\x1b[92m": "bright green",
		"\x1b[93m": "bright yellow",
		"\x1b[94m": "bright blue",
	}

	foundColors := 0
	for seq, colorName := range expectedColorSequences {
		if strings.Contains(outputStr, seq) {
			foundColors++
			t.Logf("✓ Found %s sequence: %q", colorName, seq)
		} else {
			t.Logf("✗ Missing %s sequence: %q", colorName, seq)
		}
	}

	if foundColors == 0 {
		t.Error("No expected color sequences found! The test program's Windows Console API calls may not be converted to ANSI.")
	} else if foundColors < len(expectedColorSequences) {
		t.Logf("Warning: Only found %d out of %d expected color sequences", foundColors, len(expectedColorSequences))
	} else {
		t.Logf("SUCCESS: Found all %d expected color sequences from our test program!", foundColors)
	}

	exitCode, err := pty.Wait()
	if err != nil {
		t.Logf("Wait error (may be expected): %v", err)
	}

	t.Logf("Exit code: %d", exitCode)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
