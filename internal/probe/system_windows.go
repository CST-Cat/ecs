//go:build windows

package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	windowsAllProcessorGroups          = 0xffff
	windowsRelationAll                 = 0xffff
	windowsRelationProcessorCore       = 0
	windowsRelationCache               = 2
	windowsRegistryLocalMachine        = syscall.Handle(0x80000002)
	windowsRegistryRead                = 0x00020019
	windowsRegistryString              = 1
	windowsRegistryExpandedString      = 2
	windowsRegistryDWORD               = 4
	windowsProcessorFeatureARMV8Crypto = 30
)

var (
	windowsSystemKernel32                   = syscall.NewLazyDLL("kernel32.dll")
	windowsSystemNTDLL                      = syscall.NewLazyDLL("ntdll.dll")
	windowsRtlGetVersion                    = windowsSystemNTDLL.NewProc("RtlGetVersion")
	windowsGetVersionEx                     = windowsSystemKernel32.NewProc("GetVersionExW")
	windowsGetProductInfo                   = windowsSystemKernel32.NewProc("GetProductInfo")
	windowsGetActiveProcessorCount          = windowsSystemKernel32.NewProc("GetActiveProcessorCount")
	windowsGetLogicalProcessorInformationEx = windowsSystemKernel32.NewProc("GetLogicalProcessorInformationEx")
	windowsGetTickCount64                   = windowsSystemKernel32.NewProc("GetTickCount64")
	windowsIsProcessorFeaturePresent        = windowsSystemKernel32.NewProc("IsProcessorFeaturePresent")
	windowsRegOpenKeyEx                     = syscall.NewLazyDLL("advapi32.dll").NewProc("RegOpenKeyExW")
	windowsRegQueryValueEx                  = syscall.NewLazyDLL("advapi32.dll").NewProc("RegQueryValueExW")
	windowsRegCloseKey                      = syscall.NewLazyDLL("advapi32.dll").NewProc("RegCloseKey")
)

type windowsOSVersionInfo struct {
	Size       uint32
	Major      uint32
	Minor      uint32
	Build      uint32
	PlatformID uint32
	CSDVersion [128]uint16
}

type windowsOSVersionInfoEx struct {
	Size             uint32
	Major            uint32
	Minor            uint32
	Build            uint32
	PlatformID       uint32
	CSDVersion       [128]uint16
	ServicePackMajor uint16
	ServicePackMinor uint16
	SuiteMask        uint16
	ProductType      uint8
	Reserved         uint8
}

type windowsVersionInfo struct {
	Major       uint32
	Minor       uint32
	Build       uint32
	ProductType uint8
	ProductInfo uint32
}

// collectPlatformSystem contains only native Windows system interfaces.  A
// failed API call leaves the initial unknown/unavailable values untouched so a
// restricted Server installation cannot turn an absent fact into zero.
func collectPlatformSystem(_ context.Context, s *systemSnapshot) {
	if s == nil {
		return
	}
	s.OS = "Windows"
	s.Load = "unavailable"
	s.Congestion = "unavailable"
	s.QDisc = "unavailable"
	s.LogicalCPUMethod = "win32-runtime-numcpu-fallback-v1"
	s.PhysicalCores = 0
	s.PhysicalCoresKnown = false

	if version, ok := readWindowsVersion(); ok {
		s.OS = windowsOSName(version)
		s.Kernel = windowsKernelName(version)
	}
	if logical, ok := windowsActiveProcessorCount(); ok {
		s.LogicalCPUs = logical
		s.LogicalCPUMethod = platformLogicalCPUCountMethod()
	}
	if physical, cache, ok := windowsProcessorTopology(); ok {
		if physical > 0 {
			s.PhysicalCores = physical
			s.PhysicalCoresKnown = true
		}
		if cache != "" {
			s.CPUCache = cache
		}
	}
	if model, frequency := windowsCPURegistryFacts(); model != "" || frequency != "" {
		if model != "" {
			s.CPUModel = model
		}
		if frequency != "" {
			s.CPUFrequency = frequency
		}
	}
	s.AES = windowsAESStatus()
	s.Nested = "unknown"
	s.Virtualization = windowsVirtualizationFromHardware(parseWindowsSMBIOSInventory(readWindowsSMBIOS()))

	if status, ok := readWindowsMemoryStatus(); ok {
		memory := windowsMemoryUsageFromStatus(status)
		s.MemoryTotal = memory.EffectiveTotalBytes
		s.MemoryUsed = memory.EffectiveUsedBytes
		s.MemoryFree = memory.EffectiveAvailableBytes
		s.MemoryUsage = memory.EffectiveUsagePercent
		s.MemoryTotalKnown = memory.EffectiveTotalBytes > 0
		s.MemoryUsedKnown = memory.EffectiveTotalBytes > 0 && memory.EffectiveAvailableKnown
		s.MemoryAvailableKnown = memory.EffectiveAvailableKnown
		s.MemoryMethod = windowsMemoryMethod
		if swap, swapOK := windowsMemorySwapFromStatus(status); swapOK {
			s.SwapTotal, s.SwapKnown = swap, true
		}
	}
	if uptime, ok := windowsUptimeSeconds(); ok {
		s.UptimeSeconds, s.UptimeKnown = uptime, true
	}
	if s.Kernel == "" {
		s.Kernel = "Windows"
	}
	// Windows has no Linux cgroup, PSI, balloon, KSM, or steal-time source in
	// this probe.  The explicit evidence strings remain visible in the report.
	s.MemoryLimit = 0
	s.StealPercent, s.StealKnown = 0, false
	s.BalloonReclaim = memoryFacility{Evidence: "unavailable on Windows: no native balloon reclaim interface"}
	s.KSM = memoryFacility{Evidence: "unavailable on Windows: no native KSM interface"}
}

func readWindowsVersion() (windowsVersionInfo, bool) {
	var rtl windowsOSVersionInfo
	rtl.Size = uint32(unsafe.Sizeof(rtl))
	status, _, _ := windowsRtlGetVersion.Call(uintptr(unsafe.Pointer(&rtl)))
	if status != 0 {
		return windowsVersionInfo{}, false
	}
	version := windowsVersionInfo{Major: rtl.Major, Minor: rtl.Minor, Build: rtl.Build}
	var compat windowsOSVersionInfoEx
	compat.Size = uint32(unsafe.Sizeof(compat))
	if result, _, _ := windowsGetVersionEx.Call(uintptr(unsafe.Pointer(&compat))); result != 0 {
		version.ProductType = compat.ProductType
	}
	var product uint32
	if result, _, _ := windowsGetProductInfo.Call(
		uintptr(version.Major), uintptr(version.Minor), 0, 0,
		uintptr(unsafe.Pointer(&product)),
	); result != 0 {
		version.ProductInfo = product
	}
	return version, true
}

func windowsOSName(version windowsVersionInfo) string {
	server := version.ProductType == 3 || windowsServerProduct(version.ProductInfo)
	// RtlGetVersion supplies the actual build even when an application has no
	// compatibility manifest.  The supported Server releases have stable
	// build families, while an unknown build keeps its numeric evidence.
	if server {
		switch {
		case version.Build >= 26100:
			return "Windows Server 2025"
		case version.Build >= 20348:
			return "Windows Server 2022"
		case version.Build >= 17763:
			return "Windows Server 2019"
		default:
			return fmt.Sprintf("Windows Server (build %d)", version.Build)
		}
	}
	if version.Major == 0 && version.Minor == 0 && version.Build == 0 {
		return "Windows"
	}
	return fmt.Sprintf("Windows %d.%d (build %d)", version.Major, version.Minor, version.Build)
}

func windowsKernelName(version windowsVersionInfo) string {
	if version.Major == 0 && version.Minor == 0 && version.Build == 0 {
		return ""
	}
	return fmt.Sprintf("Windows NT %d.%d (build %d)", version.Major, version.Minor, version.Build)
}

func windowsServerProduct(product uint32) bool {
	switch product {
	case 0x00000007, 0x00000008, 0x00000009, 0x0000000a,
		0x0000000c, 0x0000000d, 0x0000000e, 0x00000012,
		0x00000013, 0x00000014, 0x00000015, 0x00000016,
		0x00000017, 0x00000018, 0x00000019, 0x00000021,
		0x0000002a, 0x0000002b, 0x0000002c:
		return true
	default:
		return false
	}
}

func windowsActiveProcessorCount() (int, bool) {
	count, _, _ := windowsGetActiveProcessorCount.Call(windowsAllProcessorGroups)
	if count == 0 || count > uintptr(^uint(0)>>1) {
		return 0, false
	}
	return int(count), true
}

func windowsProcessorTopology() (int, string, bool) {
	var length uint32
	_, _, _ = windowsGetLogicalProcessorInformationEx.Call(
		windowsRelationAll, 0, 0, uintptr(unsafe.Pointer(&length)),
	)
	if length == 0 || length > 4<<20 {
		return 0, "", false
	}
	data := make([]byte, length)
	result, _, _ := windowsGetLogicalProcessorInformationEx.Call(
		windowsRelationAll, 0, uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&length)),
	)
	if result == 0 {
		return 0, "", false
	}
	physical, cache := parseWindowsProcessorInformation(data[:length])
	return physical, cache, physical > 0 || cache != ""
}

func parseWindowsProcessorInformation(data []byte) (int, string) {
	physical := 0
	cacheSizes := make(map[uint8]uint32)
	for offset := 0; offset+8 <= len(data); {
		relationship := binary.LittleEndian.Uint32(data[offset : offset+4])
		recordSize := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		if recordSize < 8 || recordSize > uint32(len(data)-offset) {
			break
		}
		record := data[offset : offset+int(recordSize)]
		switch relationship {
		case windowsRelationProcessorCore:
			physical++
		case windowsRelationCache:
			if len(record) >= 16 {
				level := record[8]
				size := binary.LittleEndian.Uint32(record[12:16])
				if level > 0 && size > 0 && size > cacheSizes[level] {
					cacheSizes[level] = size
				}
			}
		}
		offset += int(recordSize)
	}
	levels := make([]int, 0, len(cacheSizes))
	for level := range cacheSizes {
		levels = append(levels, int(level))
	}
	for i := 1; i < len(levels); i++ {
		for j := i; j > 0 && levels[j] < levels[j-1]; j-- {
			levels[j], levels[j-1] = levels[j-1], levels[j]
		}
	}
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		parts = append(parts, fmt.Sprintf("L%d=%s", level, formatHardwareBytes(uint64(cacheSizes[uint8(level)]))))
	}
	return physical, strings.Join(parts, " · ")
}

func windowsCPURegistryFacts() (string, string) {
	const key = `HARDWARE\DESCRIPTION\System\CentralProcessor\0`
	model := readWindowsRegistryString(key, "ProcessorNameString")
	frequency := ""
	if value, ok := readWindowsRegistryDWORD(key, "~MHz"); ok && value > 0 {
		frequency = strconv.FormatUint(uint64(value), 10) + " MHz"
	}
	return model, frequency
}

func readWindowsRegistryString(subkey, value string) string {
	keyPtr, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return ""
	}
	valuePtr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	result, _, _ := windowsRegOpenKeyEx.Call(
		uintptr(windowsRegistryLocalMachine), uintptr(unsafe.Pointer(keyPtr)), 0,
		windowsRegistryRead, uintptr(unsafe.Pointer(&key)),
	)
	if result != 0 || key == 0 {
		return ""
	}
	defer windowsRegCloseKey.Call(uintptr(key))
	var kind, size uint32
	result, _, _ = windowsRegQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(valuePtr)), 0, uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size)))
	if result != 0 || (kind != windowsRegistryString && kind != windowsRegistryExpandedString) || size == 0 || size > 1<<20 {
		return ""
	}
	data := make([]byte, ((int(size)+1)/2)*2)
	result, _, _ = windowsRegQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(valuePtr)), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)))
	if result != 0 {
		return ""
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2)
	return strings.TrimSpace(syscall.UTF16ToString(units))
}

func readWindowsRegistryDWORD(subkey, value string) (uint32, bool) {
	keyPtr, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return 0, false
	}
	valuePtr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return 0, false
	}
	var key syscall.Handle
	result, _, _ := windowsRegOpenKeyEx.Call(
		uintptr(windowsRegistryLocalMachine), uintptr(unsafe.Pointer(keyPtr)), 0,
		windowsRegistryRead, uintptr(unsafe.Pointer(&key)),
	)
	if result != 0 || key == 0 {
		return 0, false
	}
	defer windowsRegCloseKey.Call(uintptr(key))
	var kind, size uint32
	var valueData uint32
	size = uint32(unsafe.Sizeof(valueData))
	result, _, _ = windowsRegQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(valuePtr)), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&valueData)), uintptr(unsafe.Pointer(&size)))
	return valueData, result == 0 && kind == windowsRegistryDWORD && size == uint32(unsafe.Sizeof(valueData))
}

func windowsAESStatus() string {
	// IsProcessorFeaturePresent has a native ARM crypto feature bit, but no
	// x86 AES bit.  Do not infer AES support from an unrelated SIMD feature.
	if runtime.GOARCH == "arm64" {
		available, _, _ := windowsIsProcessorFeaturePresent.Call(windowsProcessorFeatureARMV8Crypto)
		if available != 0 {
			return "available"
		}
		return "unavailable"
	}
	return "unknown"
}

func windowsUptimeSeconds() (uint64, bool) {
	// GetTickCount64 returns a scalar rather than a BOOL. Find reports DLL or
	// procedure lookup failure; LazyProc.Call's third result is GetLastError
	// and is not a success indicator for this API.
	if err := windowsGetTickCount64.Find(); err != nil {
		return 0, false
	}
	milliseconds, _, _ := windowsGetTickCount64.Call()
	return windowsUptimeFromMilliseconds(uint64(milliseconds))
}

func windowsUptimeFromMilliseconds(milliseconds uint64) (uint64, bool) {
	if milliseconds == 0 {
		return 0, false
	}
	seconds := milliseconds / 1000
	if seconds == 0 {
		return 0, false
	}
	return seconds, true
}

func platformLogicalCPUCountMethod() string { return "win32-getactiveprocessorcount-v1" }
