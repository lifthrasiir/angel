//go:build !windows
// +build !windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type unixPTY struct {
	file *os.File
	cmd  *exec.Cmd
}

func startPlatformPTY(cmd *exec.Cmd, width, height int) (PTY, error) {
	// Start the command with a PTY of the given size
	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	return &unixPTY{
		file: f,
		cmd:  cmd,
	}, nil
}

func (p *unixPTY) Read(data []byte) (int, error) {
	return p.file.Read(data)
}

func (p *unixPTY) Write(data []byte) (int, error) {
	return p.file.Write(data)
}

func (p *unixPTY) Resize(width, height int) error {
	return pty.Setsize(p.file, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
}

func (p *unixPTY) Close() error {
	// Close the PTY file descriptor
	if err := p.file.Close(); err != nil {
		return fmt.Errorf("failed to close PTY: %w", err)
	}

	// Kill the process if it's still running
	if p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
	}

	return nil
}

func (p *unixPTY) Wait() (int, error) {
	if err := p.cmd.Wait(); err != nil {
		// Check if it's an ExitError
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus(), nil
			}
		}
		return -1, err
	}

	// Successfully exited with code 0
	return 0, nil
}
