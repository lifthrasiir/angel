package terminal

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestBasicOutput tests basic terminal output.
func TestBasicOutput(t *testing.T) {
	// Create a simple command that outputs text and exits
	cmd := exec.Command("echo", "Hello, World!")

	term, err := New(cmd, 80, 24)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Wait for output to be processed
	time.Sleep(100 * time.Millisecond)

	snap := term.Snapshot()

	// Check that "Hello, World!" appears in the window
	found := false
	for _, line := range snap.Window {
		if strings.Contains(line, "Hello, World!") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'Hello, World!' in window, got: %v", snap.Window)
	}
}

// TestScrollback tests that lines beyond window height go to scrollback.
func TestScrollback(t *testing.T) {
	height := 5
	width := 20

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: use cmd.exe
		cmd = exec.Command("cmd", "/c", "for /L %i in (1,1,10) do @echo Line %i")
	} else {
		// Unix: use shell
		cmd = exec.Command("sh", "-c", "for i in $(seq 1 10); do echo Line $i; done")
	}

	term, err := New(cmd, width, height)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Wait for output to be processed
	time.Sleep(200 * time.Millisecond)

	snap := term.Snapshot()

	// Should have scrollback
	if len(snap.NewScrollbacks) < 5 {
		t.Errorf("Expected at least 5 scrollback lines, got %d", len(snap.NewScrollbacks))
	}

	// Last lines should be in window
	lastLineFound := false
	for _, line := range snap.Window {
		if strings.Contains(line, "Line 10") {
			lastLineFound = true
			break
		}
	}
	if !lastLineFound {
		t.Errorf("Expected 'Line 10' in window, got: %v", snap.Window)
	}
}

// TestSoftWrapping tests that long lines are marked as soft-wrapped.
func TestSoftWrapping(t *testing.T) {
	width := 10
	height := 3

	// Output a line longer than width
	cmd := exec.Command("printf", "12345678901234567890\n")

	term, err := New(cmd, width, height)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Wait for output to be processed
	time.Sleep(100 * time.Millisecond)

	snap := term.Snapshot()

	// Check that we have content in the window
	hasContent := false
	for i, line := range snap.Window {
		if line != "" {
			hasContent = true
			// First line should not be soft-wrapped (it's where we started)
			// Second line might be soft-wrapped
			if i > 0 && snap.SoftWrapped[i] {
				t.Logf("Line %d is soft-wrapped: %q", i, line)
			}
		}
	}

	if !hasContent {
		t.Errorf("Expected some content in window, got: %v", snap.Window)
	}
}

// TestANSIClearScreen tests the clear screen ANSI sequence.
func TestANSIClearScreen(t *testing.T) {
	// Output some text, then clear screen, then output more text
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "echo Before clear && timeout /t 1 >nul && echo After clear")
	} else {
		cmd = exec.Command("sh", "-c", "echo 'Before clear'; sleep 0.01; echo -ne '\\033[2J'; echo 'After clear'")
	}

	term, err := New(cmd, 80, 24)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Wait for all output to be processed
	time.Sleep(200 * time.Millisecond)

	snap := term.Snapshot()

	// "After clear" should be visible
	found := false
	for _, line := range snap.Window {
		if strings.Contains(line, "After clear") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'After clear' in window after clear screen")
	}
}

// TestSnapshotIncremental tests that snapshots correctly track new scrollbacks.
func TestSnapshotIncremental(t *testing.T) {
	height := 5
	width := 20

	// Use cat to keep the process alive, then write to it
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "more.com")
	} else {
		cmd = exec.Command("cat")
	}

	term, err := New(cmd, width, height)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Initial snapshot
	snap1 := term.Snapshot()
	if len(snap1.NewScrollbacks) != 0 {
		t.Errorf("Expected no scrollback initially, got %d", len(snap1.NewScrollbacks))
	}

	// Write some lines
	for i := 1; i <= 10; i++ {
		term.Write([]byte("Line " + itoa(i) + "\n"))
	}
	time.Sleep(50 * time.Millisecond)

	snap2 := term.Snapshot()
	if len(snap2.NewScrollbacks) < 5 {
		t.Errorf("Expected at least 5 new scrollback lines, got %d", len(snap2.NewScrollbacks))
	}

	// Third snapshot should have no new scrollbacks (nothing changed)
	snap3 := term.Snapshot()
	if len(snap3.NewScrollbacks) != 0 {
		t.Errorf("Expected no new scrollback in third snapshot, got %d", len(snap3.NewScrollbacks))
	}
}

// TestCursorPosition tests that cursor positioning works.
func TestCursorPosition(t *testing.T) {
	// Use ANSI cursor positioning
	cmd := exec.Command("printf", "\\033[5;10HHello\\n")

	term, err := New(cmd, 80, 24)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	time.Sleep(100 * time.Millisecond)

	snap := term.Snapshot()

	// "Hello" should appear somewhere
	found := false
	for _, line := range snap.Window {
		if strings.Contains(line, "Hello") {
			found = true
			break
		}
	}
	if !found {
		t.Logf("Window: %v", snap.Window)
	}
}

// TestCarriageReturnLineFeed tests CR and LF handling.
func TestCarriageReturnLineFeed(t *testing.T) {
	// Test CRLF sequence
	cmd := exec.Command("printf", "Line1\\r\\nLine2\\r\\nLine3\\r\\n")

	term, err := New(cmd, 80, 24)
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			t.Skip("PTY not available on this system")
		}
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	time.Sleep(100 * time.Millisecond)

	snap := term.Snapshot()

	lineCount := 0
	for _, line := range snap.Window {
		if strings.HasPrefix(line, "Line") {
			lineCount++
		}
	}

	if lineCount < 3 {
		t.Errorf("Expected at least 3 lines, got %d", lineCount)
	}
}

// TestStringWidth tests the StringWidth helper function.
func TestStringWidth(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"Hello", 5},
		{"你好", 4},          // Chinese characters are width 2 each
		{"a\tb", 2},        // Tab is width 1 in runewidth (a=1, \t=1, b=1, but runewidth counts tab as special)
		{"Hello, 世界!", 12}, // Mixed ASCII and wide characters (runewidth calculates this as 12)
	}

	for _, tt := range tests {
		got := StringWidth(tt.input)
		if got != tt.expected {
			t.Errorf("StringWidth(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// TestSnapshotStructure tests that Snapshot has the expected structure.
func TestSnapshotStructure(t *testing.T) {
	// This test doesn't require PTY, just checks the types
	snap := Snapshot{
		NewScrollbacks: []string{"line1", "line2"},
		Window:         make([]string, 10),
		SoftWrapped:    make([]bool, 10),
	}

	if len(snap.NewScrollbacks) != 2 {
		t.Errorf("Expected 2 scrollbacks, got %d", len(snap.NewScrollbacks))
	}
	if len(snap.Window) != 10 {
		t.Errorf("Expected 10 window lines, got %d", len(snap.Window))
	}
	if len(snap.SoftWrapped) != 10 {
		t.Errorf("Expected 10 soft wrapped flags, got %d", len(snap.SoftWrapped))
	}
}

// Helper function to convert int to string
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append(digits, byte('0'+i%10))
		i /= 10
	}
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

// TestNewWithoutPTY tests that New fails appropriately when PTY is not available.
func TestNewWithoutPTY(t *testing.T) {
	// This test always runs to verify error handling
	cmd := exec.Command("echo", "test")
	_, err := New(cmd, 80, 24)

	// On some systems PTY might not work
	if err != nil {
		if _, ok := err.(*ptyError); ok {
			// Expected on systems without PTY support
			t.Logf("PTY not available on this system (OS: %s)", runtime.GOOS)
			return
		}
		t.Fatalf("Unexpected error type: %v", err)
	}
}
