//go:build freebsd

package ui

import (
	"os"
	"syscall"
	"testing"
)

func openTerminalFixture(t *testing.T) *os.File {
	t.Helper()
	fd, _, errno := syscall.Syscall(
		syscall.SYS_POSIX_OPENPT,
		uintptr(os.O_RDWR|syscall.O_CLOEXEC),
		0,
		0,
	)
	if errno != 0 {
		t.Fatalf("posix_openpt is unavailable: %v", errno)
	}
	file := os.NewFile(fd, "posix_openpt")
	if file == nil {
		_ = syscall.Close(int(fd))
		t.Fatal("posix_openpt returned an invalid descriptor")
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
