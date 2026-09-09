//go:build linux || freebsd

package ui

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

type terminalFixtureWindowSize struct {
	rows, columns     uint16
	widthPx, heightPx uint16
}

func setTerminalFixtureWidth(t *testing.T, file *os.File, columns uint16) {
	t.Helper()
	size := terminalFixtureWindowSize{rows: 24, columns: columns}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		file.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&size)),
		0,
		0,
		0,
	)
	if errno != 0 {
		t.Fatalf("set PTY width ioctl failed: %v", errno)
	}
}

func TestTerminalFileRejectsNonTTY(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if isTerminalFile(file) {
		t.Fatal("/dev/null was detected as a TTY")
	}
	if got := terminalFileWidth(file); got != 0 {
		t.Fatalf("/dev/null width = %d, want 0", got)
	}
	if isTerminalFile(nil) {
		t.Fatal("nil file was detected as a TTY")
	}
	if got := terminalFileWidth(nil); got != 0 {
		t.Fatalf("nil file width = %d, want 0", got)
	}
}

func TestTerminalFileDetectsPTYAndReadsWidth(t *testing.T) {
	file := openTerminalFixture(t)
	if !isTerminalFile(file) {
		t.Fatal("PTY was not detected as a TTY")
	}
	setTerminalFixtureWidth(t, file, 117)
	if got := terminalFileWidth(file); got != 117 {
		t.Fatalf("PTY width = %d, want 117", got)
	}
}
