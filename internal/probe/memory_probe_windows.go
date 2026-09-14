//go:build windows

package probe

import "unsafe"

const windowsMemoryMethod = "win32-globalmemorystatusex-v1"

var windowsGlobalMemoryStatusEx = windowsSystemKernel32.NewProc("GlobalMemoryStatusEx")

// windowsMemoryStatusEx mirrors MEMORYSTATUSEX.  The API reports physical
// memory and the commit limit without requiring a shell, a performance
// counter provider, or administrator access.
type windowsMemoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func platformUsesMemoryAvailableFallback() bool { return false }

func collectPlatformMemoryUsageSnapshot() memoryUsageSnapshot {
	status, ok := readWindowsMemoryStatus()
	if !ok {
		return memoryUsageSnapshot{}
	}
	return windowsMemoryUsageFromStatus(status)
}

func readWindowsMemoryStatus() (windowsMemoryStatusEx, bool) {
	status := windowsMemoryStatusEx{Length: uint32(unsafe.Sizeof(windowsMemoryStatusEx{}))}
	result, _, _ := windowsGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	return status, result != 0
}

func collectPlatformMemoryFacilities() (memoryFacility, memoryFacility) {
	return memoryFacility{Evidence: "unavailable on Windows: no native balloon reclaim interface"},
		memoryFacility{Evidence: "unavailable on Windows: no native KSM interface"}
}

func systemMemoryMeasurementMethod(snapshot systemSnapshot) string {
	if snapshot.MemoryMethod != "" {
		return snapshot.MemoryMethod
	}
	return windowsMemoryMethod
}

func systemCgroupCPUValue(_ cpuAllowance) string { return "unavailable" }

func systemMemoryLimitMachineValue(_ resourceLimits) string { return "unavailable" }

func windowsMemoryUsageFromStatus(status windowsMemoryStatusEx) memoryUsageSnapshot {
	result := memoryUsageSnapshot{}
	result.HostTotalBytes = status.TotalPhys
	result.HostAvailableBytes = status.AvailPhys
	result.AvailableKnown = status.TotalPhys > 0
	if result.HostTotalBytes > 0 && result.HostAvailableBytes > result.HostTotalBytes {
		result.HostAvailableBytes = result.HostTotalBytes
	}
	if result.HostTotalBytes > 0 && result.HostAvailableBytes <= result.HostTotalBytes {
		result.HostUsedBytes = result.HostTotalBytes - result.HostAvailableBytes
		result.HostUsagePercent = float64(result.HostUsedBytes) / float64(result.HostTotalBytes) * 100
	}
	result.EffectiveTotalBytes = result.HostTotalBytes
	result.EffectiveUsedBytes = result.HostUsedBytes
	result.EffectiveAvailableBytes = result.HostAvailableBytes
	result.EffectiveUsagePercent = result.HostUsagePercent
	result.EffectiveAvailableKnown = result.AvailableKnown && result.HostTotalBytes > 0
	return result
}

func windowsMemorySwapFromStatus(status windowsMemoryStatusEx) (uint64, bool) {
	if status.TotalPhys == 0 || status.TotalPageFile == 0 || status.TotalPageFile < status.TotalPhys {
		return 0, false
	}
	return status.TotalPageFile - status.TotalPhys, true
}
