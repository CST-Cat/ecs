//go:build windows

package ui

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

const ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004

var windowsConsoleKernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	windowsGetConsoleMode             = windowsConsoleKernel32.NewProc("GetConsoleMode")
	windowsSetConsoleMode             = windowsConsoleKernel32.NewProc("SetConsoleMode")
	windowsGetConsoleScreenBufferInfo = windowsConsoleKernel32.NewProc("GetConsoleScreenBufferInfo")
)

type windowsCoord struct {
	X int16
	Y int16
}

type windowsSmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type windowsConsoleScreenBufferInfo struct {
	Size              windowsCoord
	CursorPosition    windowsCoord
	Attributes        uint16
	Window            windowsSmallRect
	MaximumWindowSize windowsCoord
}

func getWindowsConsoleMode(file *os.File) (uint32, error) {
	if file == nil {
		return 0, os.ErrInvalid
	}
	var mode uint32
	result, _, callErr := windowsGetConsoleMode.Call(file.Fd(), uintptr(unsafe.Pointer(&mode)))
	if result == 0 {
		if callErr == nil {
			callErr = syscall.EINVAL
		}
		return 0, callErr
	}
	return mode, nil
}

// isTerminalFile recognizes a Windows console output handle. GetConsoleMode
// deliberately rejects regular files and pipes, so redirected output never
// enters the console-only progress path.
func isTerminalFile(file *os.File) bool {
	_, err := getWindowsConsoleMode(file)
	return err == nil
}

// terminalFileWidth queries the visible console window rather than treating
// every character device as a terminal. A redirected handle fails this API.
func terminalFileWidth(file *os.File) int {
	if file == nil {
		return 0
	}
	var info windowsConsoleScreenBufferInfo
	result, _, _ := windowsGetConsoleScreenBufferInfo.Call(
		file.Fd(),
		uintptr(unsafe.Pointer(&info)),
	)
	if result == 0 {
		return 0
	}
	width := int(info.Window.Right) - int(info.Window.Left) + 1
	if width <= 0 {
		width = int(info.Size.X)
	}
	return max(0, width)
}

// terminalSupportsLiveProgress enables VT processing only on a real console.
// Legacy console handles remain valid terminals, but they must use static
// progress because cursor sequences are not interpreted there.
func terminalSupportsLiveProgress(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	mode, err := getWindowsConsoleMode(file)
	if err != nil {
		return false
	}
	if mode&ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	result, _, _ := windowsSetConsoleMode.Call(
		file.Fd(),
		uintptr(mode|ENABLE_VIRTUAL_TERMINAL_PROCESSING),
	)
	return result != 0
}
