//go:build windows

package probe

import (
	"fmt"
	"syscall"
	"unsafe"
)

const windowsDriveTypeFixed = 3

var (
	windowsGetLogicalDrives = windowsSystemKernel32.NewProc("GetLogicalDrives")
	windowsGetDriveType     = windowsSystemKernel32.NewProc("GetDriveTypeW")
)

// discoverMountPoints enumerates assigned fixed volumes through Win32.  It
// does not parse shell output and never asks the volume API for persistent
// identifiers.
func discoverMountPoints() []mountPoint {
	return windowsFixedDriveMounts()
}

func windowsFixedDriveMounts() []mountPoint {
	if err := windowsGetLogicalDrives.Find(); err != nil {
		return nil
	}
	if err := windowsGetDriveType.Find(); err != nil {
		return nil
	}
	mask, _, _ := windowsGetLogicalDrives.Call()
	if mask == 0 {
		return nil
	}
	var mounts []mountPoint
	for letter := byte('A'); letter <= byte('Z'); letter++ {
		bit := uintptr(1) << (letter - 'A')
		if mask&bit == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", letter)
		name, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		driveType, _, _ := windowsGetDriveType.Call(uintptr(unsafe.Pointer(name)))
		if driveType != windowsDriveTypeFixed {
			continue
		}
		mounts = append(mounts, mountPoint{Path: root, Device: root, FSType: "windows-fixed"})
	}
	return mounts
}
