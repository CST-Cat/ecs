//go:build linux

package ui

import (
	"os"
	"syscall"
	"testing"
)

func openTerminalFixture(t *testing.T) *os.File {
	t.Helper()
	file, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("PTY clone device is unavailable: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
