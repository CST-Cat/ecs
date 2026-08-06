//go:build linux

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

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
