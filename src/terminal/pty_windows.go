//go:build windows
// +build windows

package terminal

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

var ptyCloseMutex sync.Mutex

type windowsPTY struct {
	cpty     *conpty.ConPty
	codePage uint32
}

// multiByteToUTF8 converts a byte array from the given code page to UTF-8.
func multiByteToUTF8(data []byte, codePage uint32) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	// First, convert multi-byte to UTF-16
	// Determine required buffer size
	size, err := windows.MultiByteToWideChar(codePage, 0, &data[0], int32(len(data)), nil, 0)
	if err != nil {
		return "", fmt.Errorf("MultiByteToWideChar size check failed: %w", err)
	}
	if size == 0 {
		return "", nil
	}

	// Allocate UTF-16 buffer
	utf16Buf := make([]uint16, size)

	// Perform conversion to UTF-16
	_, err = windows.MultiByteToWideChar(codePage, 0, &data[0], int32(len(data)), &utf16Buf[0], size)
	if err != nil {
		return "", fmt.Errorf("MultiByteToWideChar conversion failed: %w", err)
	}

	// Convert UTF-16 to Go string (which is UTF-8)
	return windows.UTF16ToString(utf16Buf), nil
}

func startPlatformPTY(cmd *exec.Cmd, width, height int) (PTY, error) {
	// Build command line from exec.Cmd
	commandLine, err := buildCommandLine(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to build command line: %w", err)
	}

	// Get working directory
	workDir := ""
	if cmd.Dir != "" {
		workDir = cmd.Dir
	}

	// Get environment variables
	var env []string
	if len(cmd.Env) > 0 {
		env = cmd.Env
	}

	// Get the console output code page for encoding conversion
	cp, err := windows.GetConsoleOutputCP()
	if err != nil {
		// Default to UTF-8 if we can't get the code page
		cp = 65001
	}

	// Start ConPTY with the command line
	cpty, err := conpty.Start(
		commandLine,
		conpty.ConPtyDimensions(width, height),
		conpty.ConPtyWorkDir(workDir),
		conpty.ConPtyEnv(env),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start ConPTY: %w", err)
	}

	return &windowsPTY{
		cpty:     cpty,
		codePage: cp,
	}, nil
}

// buildCommandLine constructs a command line string from exec.Cmd.
// This is needed for Windows ConPTY which takes a command line string.
func buildCommandLine(cmd *exec.Cmd) (string, error) {
	if len(cmd.Args) == 0 {
		return "", fmt.Errorf("no command specified")
	}

	// Get the command path
	path := cmd.Path
	if path == "" {
		path = cmd.Args[0]
	}

	// For Windows, we need to handle the command line properly
	// If it's cmd.exe or powershell.exe, use their specific syntax
	if strings.Contains(strings.ToLower(path), "cmd.exe") {
		// cmd.exe /C "command"
		if len(cmd.Args) > 1 && strings.ToLower(cmd.Args[1]) == "/c" {
			// Already has /C flag, join the rest
			return strings.Join(cmd.Args, " "), nil
		}
		// Add /C flag
		return fmt.Sprintf("%s /C %s", path, strings.Join(cmd.Args[1:], " ")), nil
	}

	if strings.Contains(strings.ToLower(path), "powershell.exe") ||
		strings.Contains(strings.ToLower(path), "pwsh.exe") {
		// PowerShell: powershell -Command "command"
		if len(cmd.Args) > 1 && (strings.ToLower(cmd.Args[1]) == "-command" ||
			strings.ToLower(cmd.Args[1]) == "-c") {
			return strings.Join(cmd.Args, " "), nil
		}
		return fmt.Sprintf("%s -Command %s", path, strings.Join(cmd.Args[1:], " ")), nil
	}

	// For other commands, just join with spaces
	// TODO: Properly quote arguments with spaces
	return strings.Join(cmd.Args, " "), nil
}

// errnoFromError extracts the Windows error number (errno) from an error.
// Returns the errno if found, otherwise returns 0.
func errnoFromError(err error) syscall.Errno {
	// Direct syscall.Errno
	if errno, ok := err.(syscall.Errno); ok {
		return errno
	}
	// Wrapped syscall errors (e.g., from os.PathError)
	if sysErr, ok := err.(interface{ SyscallErrno() syscall.Errno }); ok {
		return sysErr.SyscallErrno()
	}
	return 0
}

func (p *windowsPTY) Read(data []byte) (int, error) {
	n, err := p.cpty.Read(data)
	if err != nil {
		// On Windows, when the process exits and the pipe is closed,
		// we get ERROR_BROKEN_PIPE (109). Treat this as EOF.
		if errnoFromError(err) == 109 { // ERROR_BROKEN_PIPE
			return n, io.EOF
		}
	}

	// If code page is not UTF-8 (65001), convert the output
	if p.codePage != 65001 && n > 0 {
		// Check if the data is already valid UTF-8
		if !utf8.Valid(data[:n]) {
			// Convert from the detected code page to UTF-8
			converted, convErr := multiByteToUTF8(data[:n], p.codePage)
			if convErr == nil {
				// Copy converted data back to the buffer
				convertedBytes := []byte(converted)
				copy(data, convertedBytes)
				// Return the length of converted data (truncated if buffer is too small)
				if len(convertedBytes) > len(data) {
					return len(data), err
				}
				return len(convertedBytes), err
			}
			// If conversion fails, return original data
		}
	}

	return n, err
}

func (p *windowsPTY) Write(data []byte) (int, error) {
	return p.cpty.Write(data)
}

func (p *windowsPTY) Resize(width, height int) error {
	return p.cpty.Resize(width, height)
}

func (p *windowsPTY) Close() error {
	// Serialize ConPTY Close calls to prevent heap corruption
	// when multiple PTYs are closed concurrently (e.g., during tests with -count=N)
	ptyCloseMutex.Lock()
	defer ptyCloseMutex.Unlock()
	return p.cpty.Close()
}

func (p *windowsPTY) Wait() (int, error) {
	// ConPTY Wait returns uint32
	exitCode, err := p.cpty.Wait(context.Background())
	if err != nil {
		return -1, err
	}
	return int(exitCode), nil
}
