//go:build windows

package probe

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var windowsGetDiskFreeSpaceEx = windowsSystemKernel32.NewProc("GetDiskFreeSpaceExW")

func collectPlatformDisk(_ context.Context, diskPath string, s *systemSnapshot) {
	if s == nil {
		return
	}
	path := strings.TrimSpace(diskPath)
	if path == "" {
		path = "."
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	var available, total, totalFree uint64
	result, _, _ := windowsGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if result == 0 {
		return
	}
	parsed, ok := windowsDiskSnapshotFromValues(path, available, total, totalFree)
	if !ok {
		return
	}
	s.DiskDevice = parsed.DiskDevice
	s.DiskMount = parsed.DiskMount
	s.DiskTotal = parsed.DiskTotal
	s.DiskUsed = parsed.DiskUsed
	s.DiskFree = parsed.DiskFree
	s.DiskUsage = parsed.DiskUsage
	s.DiskKnown = true
}

func windowsDiskSnapshotFromValues(path string, available, total, totalFree uint64) (systemSnapshot, bool) {
	if total == 0 {
		return systemSnapshot{}, false
	}
	if totalFree > total {
		totalFree = total
	}
	if available > total {
		available = total
	}
	absPath, err := filepath.Abs(path)
	if err != nil || strings.TrimSpace(absPath) == "" {
		absPath = path
	}
	used := total - totalFree
	return systemSnapshot{
		DiskDevice: filepath.VolumeName(absPath),
		DiskMount:  absPath,
		DiskTotal:  total,
		DiskUsed:   used,
		DiskFree:   available,
		DiskUsage:  float64(used) / float64(total) * 100,
		DiskKnown:  true,
	}, true
}

func systemDiskMeasurementMethod() string { return "win32-getdiskfreespaceex-v1" }
