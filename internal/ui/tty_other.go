//go:build !linux

package ui

import "os"

// Keep a conservative fallback for non-Linux builds; Linux uses a real TTY
// ioctl in tty_linux.go.
func isTerminalFile(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Width detection is deliberately conservative off Linux. ecs release
// binaries target Linux; unknown platforms retain the existing unbounded
// progress formatting instead of guessing a terminal width.
func terminalFileWidth(*os.File) int { return 0 }
