package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/Azure/go-ansiterm"
	"github.com/creack/pty"
	"github.com/mattn/go-runewidth"
)

// ptyError wraps PTY-specific errors for better error messages.
type ptyError struct {
	err error
}

func (e *ptyError) Error() string {
	return fmt.Sprintf("PTY error: %v (PTY may not be supported on this platform)", e.err)
}

func (e *ptyError) Unwrap() error {
	return e.err
}

// Snapshot represents the state of the terminal at a point in time.
type Snapshot struct {
	NewScrollbacks []string // New lines added to scrollback since last snapshot
	Window         []string // Current window contents
	SoftWrapped    []bool   // Whether each line in Window is soft-wrapped
}

// Terminal represents a terminal emulator with PTY.
type Terminal struct {
	cmd    *exec.Cmd
	pty    *os.File
	width  int
	height int

	// Terminal state
	lines      []string // Current window contents
	wrapped    []bool   // Soft wrap flags for each line
	scrollback []string // Scrollback buffer (lines that scrolled off)
	cursorX    int      // Current cursor position (0-indexed)
	cursorY    int      // Current line in window (0-indexed)

	// State tracking
	lastSnapshotLines int // Number of lines at last snapshot (for detecting scrollback)
	savedCursorX      int // Saved cursor position for DECSC
	savedCursorY      int

	mu     sync.Mutex
	closed bool
	parser *ansiterm.AnsiParser
}

// New creates a new Terminal instance from the given command.
// The command will be started with a PTY of the given dimensions.
func New(cmd *exec.Cmd, width, height int) (*Terminal, error) {
	t := &Terminal{
		cmd:               cmd,
		width:             width,
		height:            height,
		lines:             make([]string, height),
		wrapped:           make([]bool, height),
		scrollback:        make([]string, 0),
		cursorX:           0,
		cursorY:           0,
		lastSnapshotLines: 0,
	}

	// Start the command with a PTY
	var err error
	t.pty, err = pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
	if err != nil {
		return nil, &ptyError{err}
	}

	// Create an ANSI parser with our terminal as the handler
	t.parser = ansiterm.CreateParser("Ground", t)

	// Start parsing output in background
	go t.parseOutput()

	return t, nil
}

// parseOutput reads from the PTY and updates terminal state using ANSI parser.
func (t *Terminal) parseOutput() {
	buf := make([]byte, 4096)

	for {
		n, err := t.pty.Read(buf)
		if err != nil {
			if err != io.EOF {
				t.mu.Lock()
				t.closed = true
				t.mu.Unlock()
			}
			return
		}

		if n > 0 {
			t.mu.Lock()
			t.parser.Parse(buf[:n])
			t.mu.Unlock()
		}
	}
}

// Print processes a printable character.
func (t *Terminal) Print(b byte) error {
	t.handlePrintable(rune(b))
	return nil
}

// Execute executes C0 commands (like CR, LF, TAB, etc.).
func (t *Terminal) Execute(b byte) error {
	switch b {
	case '\r':
		t.cursorX = 0
	case '\n':
		t.advanceLine()
	case '\t':
		// Move to next tab stop (every 8 columns)
		nextTab := (t.cursorX + 8) &^ 7
		if nextTab >= t.width {
			nextTab = t.width - 1
		}
		t.cursorX = nextTab
	case '\b':
		// Backspace
		if t.cursorX > 0 {
			t.cursorX--
		}
	case '\x07':
		// Bell - ignore
	case '\x0b', '\x0c':
		// Vertical tab, form feed - treat as line feed
		t.advanceLine()
	}
	return nil
}

// CUU moves the cursor up.
func (t *Terminal) CUU(count int) error {
	if count == 0 {
		count = 1
	}
	t.cursorY -= count
	if t.cursorY < 0 {
		t.cursorY = 0
	}
	return nil
}

// CUD moves the cursor down.
func (t *Terminal) CUD(count int) error {
	if count == 0 {
		count = 1
	}
	t.cursorY += count
	if t.cursorY >= t.height {
		t.cursorY = t.height - 1
	}
	return nil
}

// CUF moves the cursor forward.
func (t *Terminal) CUF(count int) error {
	if count == 0 {
		count = 1
	}
	t.cursorX += count
	if t.cursorX >= t.width {
		t.cursorX = t.width - 1
	}
	return nil
}

// CUB moves the cursor backward.
func (t *Terminal) CUB(count int) error {
	if count == 0 {
		count = 1
	}
	t.cursorX -= count
	if t.cursorX < 0 {
		t.cursorX = 0
	}
	return nil
}

// CNL moves cursor to next line.
func (t *Terminal) CNL(count int) error {
	if count == 0 {
		count = 1
	}
	t.cursorY += count
	if t.cursorY >= t.height {
		t.cursorY = t.height - 1
	}
	t.cursorX = 0
	return nil
}

// CPL moves cursor to previous line.
func (t *Terminal) CPL(count int) error {
	if count == 0 {
		count = 1
	}
	t.cursorY -= count
	if t.cursorY < 0 {
		t.cursorY = 0
	}
	t.cursorX = 0
	return nil
}

// CHA sets cursor horizontal position absolute.
func (t *Terminal) CHA(position int) error {
	if position == 0 {
		position = 1
	}
	t.cursorX = position - 1
	if t.cursorX >= t.width {
		t.cursorX = t.width - 1
	}
	return nil
}

// VPA sets vertical line position absolute.
func (t *Terminal) VPA(position int) error {
	if position == 0 {
		position = 1
	}
	t.cursorY = position - 1
	if t.cursorY >= t.height {
		t.cursorY = t.height - 1
	}
	return nil
}

// CUP sets cursor position.
func (t *Terminal) CUP(row, col int) error {
	if row == 0 {
		row = 1
	}
	if col == 0 {
		col = 1
	}
	t.cursorY = row - 1
	t.cursorX = col - 1
	if t.cursorY >= t.height {
		t.cursorY = t.height - 1
	}
	if t.cursorX >= t.width {
		t.cursorX = t.width - 1
	}
	return nil
}

// HVP sets horizontal and vertical position.
func (t *Terminal) HVP(row, col int) error {
	return t.CUP(row, col)
}

// DECTCEM enables/disables text cursor enable mode.
func (t *Terminal) DECTCEM(enable bool) error {
	// We don't need to track cursor visibility for snapshots
	return nil
}

// DECOM sets origin mode.
func (t *Terminal) DECOM(enable bool) error {
	// We don't support origin mode for now
	return nil
}

// DECCOLM sets 132 column mode.
func (t *Terminal) DECCOLM(enable bool) error {
	// We don't support 132 column mode for now
	return nil
}

// ED erases in display.
func (t *Terminal) ED(mode int) error {
	switch mode {
	case 0, 2, 3:
		// Erase from cursor to end, entire screen, or entire screen with scrollback
		// For simplicity, we'll clear everything
		for i := range t.lines {
			t.lines[i] = ""
			t.wrapped[i] = false
		}
		if mode == 3 {
			// Clear scrollback too
			t.scrollback = t.scrollback[:0]
			t.lastSnapshotLines = 0
		}
	case 1:
		// Erase from start to cursor
		for i := 0; i < t.cursorY; i++ {
			t.lines[i] = ""
			t.wrapped[i] = false
		}
		if t.cursorY < t.height {
			t.lines[t.cursorY] = t.lines[t.cursorY][:t.cursorX]
		}
	}
	return nil
}

// EL erases in line.
func (t *Terminal) EL(mode int) error {
	if t.cursorY >= t.height {
		return nil
	}
	line := t.lines[t.cursorY]
	switch mode {
	case 0:
		// Erase from cursor to end of line
		if t.cursorX < len(line) {
			t.lines[t.cursorY] = line[:t.cursorX]
		} else {
			t.lines[t.cursorY] = line
		}
	case 1:
		// Erase from start of line to cursor
		if t.cursorX < len(line) {
			t.lines[t.cursorY] = line[t.cursorX:]
		} else {
			t.lines[t.cursorY] = ""
		}
	case 2:
		// Erase entire line
		t.lines[t.cursorY] = ""
	}
	return nil
}

// IL inserts lines.
func (t *Terminal) IL(count int) error {
	if count == 0 {
		count = 1
	}
	// Insert blank lines at cursor position, pushing lines down
	for i := 0; i < count; i++ {
		if t.cursorY < t.height {
			// Shift lines down
			copy(t.lines[t.cursorY+1:], t.lines[t.cursorY:])
			copy(t.wrapped[t.cursorY+1:], t.wrapped[t.cursorY:])
			t.lines[t.cursorY] = ""
			t.wrapped[t.cursorY] = false
		}
	}
	return nil
}

// DL deletes lines.
func (t *Terminal) DL(count int) error {
	if count == 0 {
		count = 1
	}
	// Delete lines at cursor position, pulling lines up
	for i := 0; i < count && t.cursorY+i < t.height-1; i++ {
		copy(t.lines[t.cursorY+i:], t.lines[t.cursorY+i+1:])
		copy(t.wrapped[t.cursorY+i:], t.wrapped[t.cursorY+i+1:])
		t.lines[t.height-1] = ""
		t.wrapped[t.height-1] = false
	}
	return nil
}

// ICH inserts characters.
func (t *Terminal) ICH(count int) error {
	if count == 0 {
		count = 1
	}
	// Insert blank characters at cursor position
	line := t.lines[t.cursorY]
	spaces := string(make([]byte, count))
	if t.cursorX < len(line) {
		t.lines[t.cursorY] = line[:t.cursorX] + spaces + line[t.cursorX:]
	} else {
		t.lines[t.cursorY] = line + spaces
	}
	return nil
}

// DCH deletes characters.
func (t *Terminal) DCH(count int) error {
	if count == 0 {
		count = 1
	}
	line := t.lines[t.cursorY]
	if t.cursorX < len(line) {
		if t.cursorX+count < len(line) {
			t.lines[t.cursorY] = line[:t.cursorX] + line[t.cursorX+count:]
		} else {
			t.lines[t.cursorY] = line[:t.cursorX]
		}
	}
	return nil
}

// SGR sets graphics rendition (colors, bold, etc.).
func (t *Terminal) SGR(params []int) error {
	// We don't track graphics rendition for snapshots
	return nil
}

// SU pans down (scroll up).
func (t *Terminal) SU(count int) error {
	if count == 0 {
		count = 1
	}
	// Move lines up, adding blank lines at bottom
	for i := 0; i < count; i++ {
		t.scrollback = append(t.scrollback, t.lines[0])
		copy(t.lines, t.lines[1:])
		copy(t.wrapped, t.wrapped[1:])
		t.lines[t.height-1] = ""
		t.wrapped[t.height-1] = false
	}
	return nil
}

// SD pans up (scroll down).
func (t *Terminal) SD(count int) error {
	if count == 0 {
		count = 1
	}
	// Move lines down, adding blank lines at top
	for i := 0; i < count; i++ {
		copy(t.lines[1:], t.lines[:])
		copy(t.wrapped[1:], t.wrapped[:])
		t.lines[0] = ""
		t.wrapped[0] = false
	}
	return nil
}

// DA handles device attributes.
func (t *Terminal) DA(params []string) error {
	// We don't need to respond to device attributes
	return nil
}

// DECSTBM sets top and bottom margins.
func (t *Terminal) DECSTBM(top, bottom int) error {
	// We don't support scrolling margins for now
	return nil
}

// IND moves cursor down one line, scrolling if necessary.
func (t *Terminal) IND() error {
	return t.CUD(1)
}

// RI moves cursor up one line, reverse scrolling if necessary.
func (t *Terminal) RI() error {
	if t.cursorY == 0 {
		// Scroll down: insert blank line at top
		copy(t.lines[1:], t.lines[:])
		copy(t.wrapped[1:], t.wrapped[:])
		t.lines[0] = ""
		t.wrapped[0] = false
	} else {
		t.cursorY--
	}
	return nil
}

// Flush updates from previous commands.
func (t *Terminal) Flush() error {
	return nil
}

// handlePrintable processes a printable character.
func (t *Terminal) handlePrintable(r rune) {
	rw := runewidth.RuneWidth(r)
	if t.cursorX+rw > t.width {
		// Line is full, mark as soft-wrapped and move to next line
		t.wrapped[t.cursorY] = true
		t.advanceLine()
	}

	// Insert character at current position
	line := t.lines[t.cursorY]
	if t.cursorX >= len(line) {
		t.lines[t.cursorY] = line + string(r)
	} else {
		// Insert in middle (rare case)
		t.lines[t.cursorY] = line[:t.cursorX] + string(r) + line[t.cursorX:]
	}
	t.cursorX += rw
}

// advanceLine moves to the next line, handling scrolling.
func (t *Terminal) advanceLine() {
	t.cursorX = 0
	t.cursorY++

	if t.cursorY >= t.height {
		// Scroll: move current line to scrollback
		t.scrollback = append(t.scrollback, t.lines[0])

		// Shift all lines up
		copy(t.lines, t.lines[1:])
		copy(t.wrapped, t.wrapped[1:])

		// Clear last line
		t.lines[t.height-1] = ""
		t.wrapped[t.height-1] = false

		t.cursorY = t.height - 1
	}
}

// Snapshot captures the current terminal state.
func (t *Terminal) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	snap := Snapshot{
		Window:      make([]string, t.height),
		SoftWrapped: make([]bool, t.height),
	}

	// Copy window contents
	copy(snap.Window, t.lines)
	copy(snap.SoftWrapped, t.wrapped)

	// Calculate new scrollbacks since last snapshot
	currentScrollbackSize := len(t.scrollback)
	if currentScrollbackSize > t.lastSnapshotLines {
		snap.NewScrollbacks = make([]string, currentScrollbackSize-t.lastSnapshotLines)
		copy(snap.NewScrollbacks, t.scrollback[t.lastSnapshotLines:])
	}
	t.lastSnapshotLines = currentScrollbackSize

	return snap
}

// Close closes the PTY and terminates the command.
func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.pty != nil {
		t.pty.Close()
	}

	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}

	t.closed = true
	return nil
}

// Resize resizes the terminal window.
func (t *Terminal) Resize(width, height int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	err := pty.Setsize(t.pty, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
	if err != nil {
		return err
	}

	// Update internal state
	if height > t.height {
		// Grow the buffers
		newLines := make([]string, height)
		newWrapped := make([]bool, height)
		copy(newLines, t.lines)
		copy(newWrapped, t.wrapped)
		t.lines = newLines
		t.wrapped = newWrapped
	}

	t.width = width
	t.height = height
	return nil
}

// Write writes data to the terminal.
func (t *Terminal) Write(data []byte) (int, error) {
	return t.pty.Write(data)
}

// StringWidth calculates the display width of a string.
func StringWidth(s string) int {
	width := 0
	for _, r := range s {
		width += runewidth.RuneWidth(r)
	}
	return width
}
