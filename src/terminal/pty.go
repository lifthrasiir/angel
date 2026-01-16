package terminal

import (
	"io"
	"os/exec"
)

// PTY represents a pseudo-terminal for running commands with terminal emulation.
// It combines process management with terminal I/O.
type PTY interface {
	// Read reads output from the command's pseudo-terminal.
	io.Reader

	// Write writes input to the command's pseudo-terminal (like stdin).
	io.Writer

	// Resize resizes the pseudo-terminal to the given dimensions.
	Resize(width, height int) error

	// Close closes the PTY and terminates the command.
	Close() error

	// Wait waits for the command to complete and returns the exit code.
	// Returns -1 if the exit code cannot be determined.
	Wait() (int, error)
}

// StartPTY starts a command with a pseudo-terminal.
// The command will be executed with terminal emulation, allowing interactive
// programs and ANSI escape sequences to work properly.
//
// On Unix/Linux/macOS, it uses github.com/creack/pty.
// On Windows, it uses github.com/UserExistsError/conpty (ConPTY API).
//
// The width and height parameters set the initial terminal size in characters.
func StartPTY(cmd *exec.Cmd, width, height int) (PTY, error) {
	return startPlatformPTY(cmd, width, height)
}
