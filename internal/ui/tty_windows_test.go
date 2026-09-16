//go:build windows

package ui

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"ecs/internal/model"
	"ecs/internal/runner"
	"ecs/internal/termcolor"
)

var windowsConsoleTestKernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	windowsTestAllocConsole = windowsConsoleTestKernel32.NewProc("AllocConsole")
	windowsTestFreeConsole  = windowsConsoleTestKernel32.NewProc("FreeConsole")
)

type windowsConsoleTestOutput struct {
	file          *os.File
	originalMode  uint32
	allocatedByUs bool
}

func openWindowsConsoleTestOutput(t *testing.T) *windowsConsoleTestOutput {
	t.Helper()

	// CONOUT$ is the authoritative way to detect an attached Windows console:
	// it also works when stdout itself was redirected. Only allocate a console
	// after opening the real console device fails.
	file, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	allocated := false
	if err != nil {
		result, _, callErr := windowsTestAllocConsole.Call()
		if result == 0 {
			if callErr == nil {
				callErr = syscall.EINVAL
			}
			t.Fatalf("CONOUT$ unavailable and AllocConsole failed: %v", callErr)
		}
		allocated = true
		file, err = os.OpenFile("CONOUT$", os.O_RDWR, 0)
	}
	if err != nil {
		if allocated {
			_, _, _ = windowsTestFreeConsole.Call()
		}
		t.Fatalf("open real CONOUT$ handle: %v", err)
	}

	mode, err := getWindowsConsoleMode(file)
	if err != nil {
		_ = file.Close()
		if allocated {
			_, _, _ = windowsTestFreeConsole.Call()
		}
		t.Fatalf("GetConsoleMode(CONOUT$): %v", err)
	}

	output := &windowsConsoleTestOutput{
		file:          file,
		originalMode:  mode,
		allocatedByUs: allocated,
	}
	t.Cleanup(func() {
		if err := setWindowsConsoleTestMode(output.file, output.originalMode); err != nil {
			t.Errorf("restore Windows console mode: %v", err)
		}
		if err := output.file.Close(); err != nil {
			t.Errorf("close CONOUT$ handle: %v", err)
		}
		if output.allocatedByUs {
			result, _, callErr := windowsTestFreeConsole.Call()
			if result == 0 {
				if callErr == nil {
					callErr = syscall.EINVAL
				}
				t.Errorf("FreeConsole: %v", callErr)
			}
		}
	})
	return output
}

func setWindowsConsoleTestMode(file *os.File, mode uint32) error {
	if file == nil {
		return os.ErrInvalid
	}
	result, _, callErr := windowsSetConsoleMode.Call(file.Fd(), uintptr(mode))
	if result != 0 {
		return nil
	}
	if callErr == nil {
		callErr = syscall.EINVAL
	}
	return callErr
}

func TestWindowsTerminalFileRejectsRedirectedOutput(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "redirected")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if isTerminalFile(file) {
		t.Fatal("regular file was detected as a Windows console")
	}
	if terminalFileWidth(file) != 0 {
		t.Fatalf("regular file width = %d, want 0", terminalFileWidth(file))
	}
	if terminalSupportsLiveProgress(file) {
		t.Fatal("regular file advertised VT live progress")
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	if isTerminalFile(writer) || terminalFileWidth(writer) != 0 || terminalSupportsLiveProgress(writer) {
		t.Fatal("pipe was detected as a Windows console")
	}
}

func TestWindowsRealConsoleCapabilityAndTerminalSelection(t *testing.T) {
	console := openWindowsConsoleTestOutput(t)
	if !isTerminalFile(console.file) {
		t.Fatal("real CONOUT$ handle was not detected as a Windows console")
	}
	if width := terminalFileWidth(console.file); width <= 0 {
		t.Fatalf("real CONOUT$ console width = %d, want positive width", width)
	}

	initialMode := console.originalMode
	initialVT := initialMode&ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0
	liveCapability := terminalSupportsLiveProgress(console.file)
	modeAfterProbe, err := getWindowsConsoleMode(console.file)
	if err != nil {
		t.Fatalf("GetConsoleMode after VT probe: %v", err)
	}
	if initialVT && !liveCapability {
		t.Fatalf("console mode initially advertised VT (0x%08x), but live capability was false", initialMode)
	}
	if liveCapability != (modeAfterProbe&ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0) {
		t.Fatalf("VT capability=%v disagrees with console mode after probe 0x%08x", liveCapability, modeAfterProbe)
	}

	// A second real Win32 mode probe must be stable. This also distinguishes a
	// successful SetConsoleMode from a synthetic capability assumption.
	repeatedCapability := terminalSupportsLiveProgress(console.file)
	modeAfterRepeat, err := getWindowsConsoleMode(console.file)
	if err != nil {
		t.Fatalf("GetConsoleMode after repeated VT probe: %v", err)
	}
	if repeatedCapability != liveCapability {
		t.Fatalf("repeated real VT capability=%v, first capability=%v", repeatedCapability, liveCapability)
	}
	if repeatedCapability != (modeAfterRepeat&ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0) {
		t.Fatalf("repeated VT capability=%v disagrees with console mode 0x%08x", repeatedCapability, modeAfterRepeat)
	}

	// Make progress policy deterministic without changing the console itself.
	// TERM=xterm permits the normal live/static policy; the capability result
	// alone decides which real console path Terminal.New selects.
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm")
	t.Setenv("COLORTERM", "")
	t.Setenv("ECS_PROGRESS_MODE", "")
	terminal := New(console.file, false)
	view := terminal.BeginProgress(1)
	view.Stop()
	if liveCapability {
		if !terminal.progressTTY || terminal.staticProgress || !view.live || view.static {
			t.Fatalf("VT-capable console selection = terminal live:%v static:%v, view live:%v static:%v", terminal.progressTTY, terminal.staticProgress, view.live, view.static)
		}
	} else if terminal.progressTTY || !terminal.staticProgress || view.live || !view.static {
		t.Fatalf("non-VT console selection = terminal live:%v static:%v, view live:%v static:%v", terminal.progressTTY, terminal.staticProgress, view.live, view.static)
	}

	if err := setWindowsConsoleTestMode(console.file, console.originalMode); err != nil {
		t.Fatalf("restore Windows console mode before test return: %v", err)
	}
	restoredMode, err := getWindowsConsoleMode(console.file)
	if err != nil {
		t.Fatalf("GetConsoleMode after explicit restore: %v", err)
	}
	if restoredMode != console.originalMode {
		t.Fatalf("console mode after explicit restore = 0x%08x, want 0x%08x", restoredMode, console.originalMode)
	}
}

func TestWindowsRedirectedProgressHasNoANSI(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("TERM", "")
	t.Setenv("COLORTERM", "")
	path := t.TempDir() + "\\progress.txt"
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	terminal := New(file, false)
	view := terminal.BeginProgress(1)
	view.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 1, Title: "fixture", Result: model.Result{}})
	view.Stop()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\x1b") {
		t.Fatalf("redirected progress emitted ANSI: %q", data)
	}
}

func TestWindowsNO_COLORRemainsColorFree(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if level := termcolor.Detect(true); level != termcolor.LevelNone {
		t.Fatalf("NO_COLOR level = %s, want none", level)
	}
}
