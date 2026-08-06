//go:build linux

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

type terminalWindowSize struct {
	rows, columns     uint16
	widthPx, heightPx uint16
}

// isTerminalFile verifies terminal capability with the kernel rather than
// treating every character device (for example /dev/null) as a TTY.
func isTerminalFile(file *os.File) bool {
	if file == nil {
		return false
	}
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		file.Fd(),
		syscall.TCGETS,
		uintptr(unsafe.Pointer(&termios)),
		0,
		0,
		0,
	)
	return errno == 0
}

// terminalFileWidth returns the current number of terminal columns. Querying
// it for every redraw also makes a resized terminal take effect immediately.
func terminalFileWidth(file *os.File) int {
	if file == nil {
		return 0
	}
	var size terminalWindowSize
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		file.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&size)),
		0,
		0,
		0,
	)
	if errno != 0 || size.columns == 0 {
		return 0
	}
	return int(size.columns)
}
