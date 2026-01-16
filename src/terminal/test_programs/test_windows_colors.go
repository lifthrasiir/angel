//go:build windows
// +build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleTextAttribute    = kernel32.NewProc("SetConsoleTextAttribute")
	procWriteConsoleW              = kernel32.NewProc("WriteConsoleW")
	procGetStdHandle               = kernel32.NewProc("GetStdHandle")
)

const (
	FOREGROUND_BLUE      = 0x0001
	FOREGROUND_GREEN     = 0x0002
	FOREGROUND_RED       = 0x0004
	FOREGROUND_INTENSITY = 0x0008
	STD_OUTPUT_HANDLE    = ^uintptr(10) // Equivalent to -11
)

type COORD struct {
	X int16
	Y int16
}

type SMALL_RECT struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type CONSOLE_SCREEN_BUFFER_INFO struct {
	DwSize              COORD
	DwCursorPosition    COORD
	WAttributes         uint16
	SrWindow            SMALL_RECT
	DwMaximumWindowSize COORD
}

func main() {
	// Get stdout handle
	handle, _, _ := procGetStdHandle.Call(uintptr(STD_OUTPUT_HANDLE))
	if handle == 0 {
		fmt.Println("Failed to get stdout handle")
		return
	}

	// Save original attributes
	var csbi CONSOLE_SCREEN_BUFFER_INFO
	procGetConsoleScreenBufferInfo.Call(handle, uintptr(unsafe.Pointer(&csbi)))
	originalAttr := csbi.WAttributes

	fmt.Println("=== Windows Console API Color Test ===")
	fmt.Println()

	// Test 1: Red text on black background
	fmt.Println("Setting RED text (Windows Console API)...")
	procSetConsoleTextAttribute.Call(handle, uintptr(FOREGROUND_RED|FOREGROUND_INTENSITY))
	fmt.Println("This text should be RED!")
	fmt.Println()

	// Test 2: Green text
	fmt.Println("Setting GREEN text...")
	procSetConsoleTextAttribute.Call(handle, uintptr(FOREGROUND_GREEN|FOREGROUND_INTENSITY))
	fmt.Println("This text should be GREEN!")
	fmt.Println()

	// Test 3: Blue text
	fmt.Println("Setting BLUE text...")
	procSetConsoleTextAttribute.Call(handle, uintptr(FOREGROUND_BLUE|FOREGROUND_INTENSITY))
	fmt.Println("This text should be BLUE!")
	fmt.Println()

	// Test 4: Yellow (Red + Green)
	fmt.Println("Setting YELLOW text (Red + Green)...")
	procSetConsoleTextAttribute.Call(handle, uintptr(FOREGROUND_RED|FOREGROUND_GREEN|FOREGROUND_INTENSITY))
	fmt.Println("This text should be YELLOW!")
	fmt.Println()

	// Test 5: Bright White (Red + Green + Blue + Intensity)
	fmt.Println("Setting BRIGHT WHITE text...")
	procSetConsoleTextAttribute.Call(handle, uintptr(FOREGROUND_RED|FOREGROUND_GREEN|FOREGROUND_BLUE|FOREGROUND_INTENSITY))
	fmt.Println("This text should be BRIGHT WHITE!")
	fmt.Println()

	// Restore original attributes
	procSetConsoleTextAttribute.Call(handle, uintptr(originalAttr))
	fmt.Println("Restored to original colors")
}
