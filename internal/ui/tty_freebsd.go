//go:build freebsd

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// FreeBSD exposes terminal attributes through TIOCGETA. Keep the ioctl
// request platform-specific instead of reusing Linux's TCGETS request.
type freeBSDWindowSize struct {
	rows, columns     uint16
	widthPx, heightPx uint16
}

// isTerminalFile verifies terminal capability with the FreeBSD tty driver.
// Character devices such as /dev/null must not be treated as terminals.
func isTerminalFile(file *os.File) bool {
	if file == nil {
		return false
	}
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		file.Fd(),
		syscall.TIOCGETA,
		uintptr(unsafe.Pointer(&termios)),
		0,
		0,
		0,
	)
	return errno == 0
}

// terminalFileWidth returns the current terminal width using FreeBSD's
// TIOCGWINSZ ioctl. A zero-width or non-terminal descriptor is unavailable.
func terminalFileWidth(file *os.File) int {
	if file == nil {
		return 0
	}
	var size freeBSDWindowSize
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
