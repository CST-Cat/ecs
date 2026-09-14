//go:build windows

package probe

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestWindowsMemoryStatusPreservesNativeFacts(t *testing.T) {
	status := windowsMemoryStatusEx{
		TotalPhys:     8 << 30,
		AvailPhys:     2 << 30,
		TotalPageFile: 12 << 30,
	}
	memory := windowsMemoryUsageFromStatus(status)
	if !memory.AvailableKnown || !memory.EffectiveAvailableKnown || memory.HostTotalBytes != 8<<30 || memory.HostUsedBytes != 6<<30 || memory.HostAvailableBytes != 2<<30 {
		t.Fatalf("Windows memory status = %+v", memory)
	}
	if swap, ok := windowsMemorySwapFromStatus(status); !ok || swap != 4<<30 {
		t.Fatalf("Windows pagefile-derived swap = %d/%v", swap, ok)
	}

	missing := windowsMemoryUsageFromStatus(windowsMemoryStatusEx{})
	if missing.AvailableKnown || missing.EffectiveAvailableKnown || missing.EffectiveTotalBytes != 0 {
		t.Fatalf("missing Windows memory status became known: %+v", missing)
	}
	if swap, ok := windowsMemorySwapFromStatus(windowsMemoryStatusEx{}); ok || swap != 0 {
		t.Fatalf("missing Windows pagefile status became a zero swap fact: %d/%v", swap, ok)
	}
	if swap, ok := windowsMemorySwapFromStatus(windowsMemoryStatusEx{TotalPhys: 8, TotalPageFile: 4}); ok || swap != 0 {
		t.Fatalf("invalid pagefile relationship became a swap fact: %d/%v", swap, ok)
	}
	result := newMemoryResult()
	appendMemoryInventory(&result, missing, memoryFacility{}, memoryFacility{})
	for _, field := range result.Fields {
		if field.Key == "memory_total" || field.Key == "memory_used" || field.Key == "memory_available" || field.Key == "memory_usage_percent" {
			if field.Value.Text() == "0 B" || field.Value.Text() == "0.0 %" {
				t.Fatalf("missing Windows memory fact rendered as zero: %s=%q", field.Key, field.Value.Text())
			}
		}
	}
}

func TestWindowsDiskSpacePreservesUnavailableZeroTotal(t *testing.T) {
	disk, ok := windowsDiskSnapshotFromValues(`C:\data`, 3<<30, 10<<30, 4<<30)
	if !ok || !disk.DiskKnown || disk.DiskTotal != 10<<30 || disk.DiskUsed != 6<<30 || disk.DiskFree != 3<<30 || disk.DiskUsage != 60 {
		t.Fatalf("Windows disk facts = %+v/%v", disk, ok)
	}
	if _, ok := windowsDiskSnapshotFromValues(`C:\data`, 0, 0, 0); ok {
		t.Fatal("zero disk total was reported as known")
	}
	clamped, ok := windowsDiskSnapshotFromValues(`C:\data`, 20, 100, 200)
	if !ok || clamped.DiskFree != 100 || clamped.DiskUsed != 0 || clamped.DiskUsage != 0 {
		t.Fatalf("Windows disk bounds = %+v/%v", clamped, ok)
	}
	if got := systemDiskMeasurementMethod(); strings.Contains(got, "statfs") || !strings.Contains(got, "getdiskfreespaceex") {
		t.Fatalf("Windows disk method = %q", got)
	}
}

func TestWindowsSMBIOSParserReadsOnlyProductFacts(t *testing.T) {
	data := windowsSMBIOSDocument(
		windowsSMBIOSRecord(0, []byte{1, 2, 0, 0, 3}, "BIOS vendor", "BIOS version", "2026-01-01", "private-value"),
		windowsSMBIOSRecord(1, []byte{1, 2, 3, 4, 5, 6, 7}, "system vendor", "product", "version", "private-value", "private-value", "private-value", "private-value"),
		windowsSMBIOSRecord(2, []byte{1, 2, 3, 4, 5, 6, 7}, "board vendor", "board", "board version", "private-value", "private-value", "private-value", "private-value"),
		[]byte{127, 4, 0, 0, 0, 0},
	)
	hardware := parseWindowsSMBIOSInventory(data)
	for key, got := range map[string]string{
		"system vendor": hardware.SystemVendor,
		"product":       hardware.ProductName,
		"version":       hardware.ProductVersion,
		"board vendor":  hardware.BoardVendor,
		"board":         hardware.BoardName,
		"board version": hardware.BoardVersion,
		"BIOS vendor":   hardware.BIOSVendor,
		"BIOS version":  hardware.BIOSVersion,
		"2026-01-01":    hardware.BIOSDate,
	} {
		if got != key {
			t.Fatalf("SMBIOS field %q = %q", key, got)
		}
	}
	for _, value := range []string{hardware.SystemVendor, hardware.ProductName, hardware.ProductVersion, hardware.BoardVendor, hardware.BoardName, hardware.BoardVersion, hardware.BIOSVendor, hardware.BIOSVersion, hardware.BIOSDate} {
		if strings.Contains(value, "private-value") {
			t.Fatalf("SMBIOS parser retained an excluded value: %q", value)
		}
	}
}

func TestWindowsProcessorInformationParser(t *testing.T) {
	data := make([]byte, 0, 64)
	data = appendWindowsProcessorRecord(data, windowsRelationProcessorCore, nil)
	data = appendWindowsProcessorRecord(data, windowsRelationProcessorCore, nil)
	cache := make([]byte, 24)
	binary.LittleEndian.PutUint32(cache[0:4], windowsRelationCache)
	binary.LittleEndian.PutUint32(cache[4:8], uint32(len(cache)))
	cache[8] = 2
	binary.LittleEndian.PutUint32(cache[12:16], 512<<10)
	data = append(data, cache...)
	physical, caches := parseWindowsProcessorInformation(data)
	if physical != 2 || caches != "L2=512.0 KiB" {
		t.Fatalf("Windows processor information = %d/%q", physical, caches)
	}
}

func TestWindowsOSAndVirtualizationSemantics(t *testing.T) {
	if got := windowsOSName(windowsVersionInfo{Major: 10, Build: 20348, ProductType: 3}); got != "Windows Server 2022" {
		t.Fatalf("Windows Server 2022 name = %q", got)
	}
	if got := windowsOSName(windowsVersionInfo{Major: 10, Build: 26100, ProductType: 3}); got != "Windows Server 2025" {
		t.Fatalf("Windows Server 2025 name = %q", got)
	}
	if got := windowsOSName(windowsVersionInfo{Major: 10, Build: 26100, ProductType: 1}); got == "Windows Server 2025" {
		t.Fatalf("Windows 11 client build was mislabeled as Server 2025: %q", got)
	}
	if got := windowsVirtualizationFromHardware(hardwareInventory{SystemVendor: "Microsoft Corporation", ProductName: "Virtual Machine"}); got != "Hyper-V" {
		t.Fatalf("Hyper-V signature = %q", got)
	}
	if got := windowsVirtualizationFromHardware(unknownWindowsHardwareInventory()); got != "unknown" {
		t.Fatalf("unknown virtualization = %q", got)
	}
	if got := windowsVirtualizationFromHardware(hardwareInventory{}); got != "unknown" {
		t.Fatalf("empty virtualization evidence = %q", got)
	}
}

func TestWindowsUptimeDoesNotPromoteZeroToKnown(t *testing.T) {
	for _, test := range []struct {
		milliseconds uint64
		seconds      uint64
		known        bool
	}{
		{milliseconds: 0, seconds: 0, known: false},
		{milliseconds: 999, seconds: 0, known: false},
		{milliseconds: 1000, seconds: 1, known: true},
		{milliseconds: 12_345, seconds: 12, known: true},
	} {
		seconds, known := windowsUptimeFromMilliseconds(test.milliseconds)
		if seconds != test.seconds || known != test.known {
			t.Fatalf("uptime from %d ms = %d/%v, want %d/%v", test.milliseconds, seconds, known, test.seconds, test.known)
		}
	}
}

func TestWindowsEnvironmentKeepsLinuxOnlyFactsUnavailable(t *testing.T) {
	var snapshot EnvironmentSnapshot
	capturePlatformEnvironment(&snapshot)
	if snapshot.LoadKnown || snapshot.CPUTracked || snapshot.CPUStat.Present || snapshot.Memory.Present {
		t.Fatalf("Windows exposed unavailable resource facts: %+v", snapshot)
	}
	for resource, pressure := range snapshot.PSI {
		if pressure.Some.Present || pressure.Full.Present || pressure.Source != "" {
			t.Fatalf("Windows exposed pressure for %s: %+v", resource, pressure)
		}
	}
	if measurements := BuildPressureMeasurements(snapshot, snapshot); len(measurements) != 0 {
		t.Fatalf("Windows emitted pressure measurements: %+v", measurements)
	}
	injected := snapshot
	injected.LoadKnown, injected.Load1 = true, 1
	if measurements := BuildPressureMeasurements(injected, injected); len(measurements) != 0 {
		t.Fatalf("Windows emitted an injected load measurement: %+v", measurements)
	}
	if params := platformKernelParams(); len(params) != 0 {
		t.Fatalf("Windows exposed Unix kernel parameters: %+v", params)
	}
}

func windowsSMBIOSDocument(records ...[]byte) []byte {
	var table []byte
	for _, record := range records {
		table = append(table, record...)
	}
	data := make([]byte, 8, 8+len(table))
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(table)))
	return append(data, table...)
}

func windowsSMBIOSRecord(recordType byte, formatted []byte, values ...string) []byte {
	record := make([]byte, 4+len(formatted))
	record[0] = recordType
	record[1] = byte(len(record))
	copy(record[4:], formatted)
	for _, value := range values {
		record = append(record, []byte(value)...)
		record = append(record, 0)
	}
	return append(record, 0)
}

func appendWindowsProcessorRecord(data []byte, relationship uint32, payload []byte) []byte {
	recordSize := 8 + len(payload)
	record := make([]byte, recordSize)
	binary.LittleEndian.PutUint32(record[0:4], relationship)
	binary.LittleEndian.PutUint32(record[4:8], uint32(recordSize))
	copy(record[8:], payload)
	return append(data, record...)
}
