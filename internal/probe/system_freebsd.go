//go:build freebsd

package probe

import (
	"context"
	"math"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const freeBSDSysctlPath = "/sbin/sysctl"
const freeBSDClockMonotonic = 4

func platformDFCommand() string    { return "/bin/df" }
func platformUnameCommand() string { return "/usr/bin/uname" }

// collectPlatformSystem contains the FreeBSD-only system interfaces.  None
// of the Linux procfs, sysfs, cgroup, PSI, or steal counters are consulted in
// this path.  An absent FreeBSD interface stays unavailable instead of being
// represented by a Linux-shaped zero.
func collectPlatformSystem(ctx context.Context, s *systemSnapshot) {
	s.OS = "FreeBSD"

	if model := freeBSDSysctlValue(ctx, "hw.model"); model != "" {
		s.CPUModel = model
	}
	if cpus, ok := parseFreeBSDUint(freeBSDSysctlValue(ctx, "hw.ncpu")); ok && cpus > 0 {
		s.LogicalCPUs = int(cpus)
	}
	// kern.smp.cores and threads_per_core are native topology facts.  Require
	// their product to agree with hw.ncpu before exposing physical cores; a
	// missing or inconsistent topology remains unknown rather than becoming a
	// fabricated logical=physical claim.
	s.PhysicalCores, s.PhysicalCoresKnown = freeBSDPhysicalCores(ctx, s.LogicalCPUs)
	if clock, ok := parseFreeBSDUint(freeBSDSysctlValue(ctx, "hw.clockrate")); ok && clock > 0 {
		s.CPUFrequency = strconv.FormatUint(clock, 10) + " MHz"
	}
	var caches []string
	for _, key := range []string{"hw.l1dcachesize", "hw.l2cachesize", "hw.l3cachesize"} {
		if value, ok := parseFreeBSDUint(freeBSDSysctlValue(ctx, key)); ok && value > 0 {
			caches = append(caches, key[strings.LastIndexByte(key, '.')+1:]+"="+formatHardwareBytes(value))
		}
	}
	if len(caches) > 0 {
		s.CPUCache = strings.Join(caches, " · ")
	}
	s.AES = freeBSDAESStatus(freeBSDSysctlValue(ctx, "hw.aesni"))

	guest := strings.TrimSpace(freeBSDSysctlValue(ctx, "kern.vm_guest"))
	jailed := strings.TrimSpace(freeBSDSysctlValue(ctx, "security.jail.jailed"))
	product := ""
	if strings.EqualFold(guest, "generic") {
		// Generic is FreeBSD's fallback value under several hypervisors;
		// consult the non-identifying SMBIOS product before exposing a
		// concrete virtualization engine.
		product = readFreeBSDKenvValue("smbios.system.product")
	}
	s.Virtualization = freeBSDVirtualization(guest, jailed, product)
	s.Nested = "unavailable"

	values := freeBSDSystemValues(ctx)
	memory := freeBSDMemoryUsage(values)
	s.MemoryTotal = memory.HostTotalBytes
	s.MemoryUsed = memory.HostUsedBytes
	s.MemoryFree = memory.HostAvailableBytes
	s.MemoryUsage = memory.HostUsagePercent
	s.MemoryTotalKnown = memory.HostTotalBytes > 0
	s.MemoryUsedKnown = memory.HostTotalBytes > 0 && memory.AvailableKnown
	s.MemoryAvailableKnown = memory.AvailableKnown
	s.MemoryMethod = memoryMethodForFreeBSDValues(values)
	if swap, ok := parseFreeBSDUint(values["vm.swap_total"]); ok {
		s.SwapTotal = swap
		s.SwapKnown = true
	}

	// CLOCK_MONOTONIC is the direct FreeBSD uptime fact. Prefer it to
	// subtracting kern.boottime from wall clock time: VM snapshot/RTC adjustment
	// can move CLOCK_REALTIME without changing elapsed time since boot.
	if uptime, ok := freeBSDMonotonicUptimeSeconds(); ok {
		s.UptimeSeconds, s.UptimeKnown = uptime, true
	} else if boot, ok := parseFreeBSDBootTime(values["kern.boottime"], time.Now()); ok {
		// Keep the formatted sysctl as a conservative fallback for environments
		// that deny clock_gettime unexpectedly.
		s.UptimeSeconds, s.UptimeKnown = boot, true
	}
	if load := parseFreeBSDLoadAverage(values["vm.loadavg"]); load != "" {
		s.Load = load
	}
	s.Congestion = "n/a"
	s.QDisc = "n/a"
	if kernel := commandOutput(ctx, platformUnameCommand(), "-sr"); kernel != "" {
		s.Kernel = kernel
	}
	if s.Kernel == "" {
		s.Kernel = "FreeBSD"
	}
	// FreeBSD has no Linux steal-time counter.  Leave both the value and its
	// presence flag empty so no proc-stat method can be emitted downstream.
	s.StealPercent, s.StealKnown = 0, false
	s.MemoryLimit = 0
	s.BalloonReclaim = memoryFacility{Evidence: "unavailable on FreeBSD: no Linux balloon interface"}
	s.KSM = memoryFacility{Evidence: "unavailable on FreeBSD: no Linux KSM interface"}
}

func freeBSDSysctlValue(ctx context.Context, key string) string {
	return commandOutput(ctx, freeBSDSysctlPath, "-n", key)
}

func freeBSDSystemValues(ctx context.Context) map[string]string {
	values := make(map[string]string)
	for _, key := range []string{
		"hw.physmem", "hw.pagesize", "vm.stats.vm.v_page_size",
		"vm.stats.vm.v_free_count", "vm.stats.vm.v_inactive_count", "vm.stats.vm.v_cache_count",
		"vm.swap_total", "kern.boottime", "vm.loadavg",
	} {
		if value := freeBSDSysctlValue(ctx, key); value != "" {
			values[key] = value
		}
	}
	return values
}

func parseFreeBSDUint(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	// Scalar sysctls are numeric, with only harmless punctuation occasionally
	// added by a formatted rendering.  Do not search arbitrary words for a
	// number: accepting "malformed 42" would turn an error into a fake fact.
	value = strings.TrimSpace(strings.Trim(value, "{},"))
	if number, err := strconv.ParseUint(value, 10, 64); err == nil {
		return number, true
	}
	return 0, false
}

// freeBSDMemoryUsage derives basic memory usage from FreeBSD VM page
// statistics. hw.usermem is deliberately not used: FreeBSD defines it as all
// non-wired memory, which includes active application pages and therefore is
// not a current-availability fact.
func freeBSDMemoryUsage(values map[string]string) memoryUsageSnapshot {
	result := memoryUsageSnapshot{}
	result.HostTotalBytes, _ = parseFreeBSDUint(values["hw.physmem"])
	if available, ok := freeBSDVMAvailableBytes(values); ok {
		result.HostAvailableBytes = available
		result.AvailableKnown = true
	}
	if result.HostTotalBytes > 0 && result.HostAvailableBytes > result.HostTotalBytes {
		result.HostAvailableBytes = result.HostTotalBytes
	}
	if result.AvailableKnown && result.HostTotalBytes >= result.HostAvailableBytes {
		result.HostUsedBytes = result.HostTotalBytes - result.HostAvailableBytes
	}
	if result.HostTotalBytes > 0 && result.AvailableKnown {
		result.HostUsagePercent = float64(result.HostUsedBytes) / float64(result.HostTotalBytes) * 100
	}
	result.EffectiveTotalBytes = result.HostTotalBytes
	result.EffectiveUsedBytes = result.HostUsedBytes
	result.EffectiveAvailableBytes = result.HostAvailableBytes
	result.EffectiveUsagePercent = result.HostUsagePercent
	result.EffectiveAvailableKnown = result.AvailableKnown
	return result
}

func memoryMethodForFreeBSDValues(values map[string]string) string {
	if _, ok := freeBSDVMAvailableBytes(values); ok {
		return "freebsd-sysctl-vmstat-v1"
	}
	physmem, physmemOK := parseFreeBSDUint(values["hw.physmem"])
	if physmemOK && physmem > 0 {
		return "freebsd-sysctl-hw-physmem-v1"
	}
	return "freebsd-sysctl-unavailable-v1"
}

func freeBSDVMAvailableBytes(values map[string]string) (uint64, bool) {
	pageSize, ok := parseFreeBSDUint(values["vm.stats.vm.v_page_size"])
	if !ok {
		pageSize, ok = parseFreeBSDUint(values["hw.pagesize"])
	}
	if !ok || pageSize == 0 {
		return 0, false
	}
	var pages uint64
	for _, key := range []string{"vm.stats.vm.v_free_count", "vm.stats.vm.v_inactive_count"} {
		count, ok := parseFreeBSDUint(values[key])
		if !ok || ^uint64(0)-pages < count {
			return 0, false
		}
		pages += count
	}
	// v_cache_count became a compatibility OID after the cache and inactive
	// queues were unified. Add it when a supported kernel exposes a real or
	// zero compatibility value, but do not make the obsolete OID mandatory.
	if cached, ok := parseFreeBSDUint(values["vm.stats.vm.v_cache_count"]); ok {
		if ^uint64(0)-pages < cached {
			return 0, false
		}
		pages += cached
	}
	if pages > ^uint64(0)/pageSize {
		return 0, false
	}
	return pages * pageSize, true
}

func parseFreeBSDLoadAverage(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "{}")
	fields := strings.Fields(value)
	if len(fields) < 3 {
		return ""
	}
	loads := make([]string, 0, 3)
	for _, field := range fields[:3] {
		field = strings.Trim(field, ",")
		parsed, err := strconv.ParseFloat(field, 64)
		if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return ""
		}
		loads = append(loads, field)
	}
	return strings.Join(loads, " / ")
}

func freeBSDMonotonicUptimeSeconds() (uint64, bool) {
	var timestamp syscall.Timespec
	_, _, errno := syscall.Syscall(
		syscall.SYS_CLOCK_GETTIME,
		uintptr(freeBSDClockMonotonic),
		uintptr(unsafe.Pointer(&timestamp)),
		0,
	)
	if errno != 0 || timestamp.Sec < 0 {
		return 0, false
	}
	return uint64(timestamp.Sec), true
}

func parseFreeBSDBootTime(value string, now time.Time) (uint64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	var seconds uint64
	fields := strings.Fields(value)
	for index, field := range fields {
		if strings.Trim(field, "{}") != "sec" || index+2 >= len(fields) || fields[index+1] != "=" {
			continue
		}
		parsed, ok := parseFreeBSDUint(strings.Trim(fields[index+2], ",}"))
		if ok {
			seconds = parsed
			break
		}
	}
	if seconds == 0 {
		if parsed, ok := parseFreeBSDUint(value); ok {
			seconds = parsed
		}
	}
	nowSeconds := now.Unix()
	if seconds == 0 || nowSeconds < 0 || seconds > uint64(nowSeconds) {
		return 0, false
	}
	return uint64(nowSeconds) - seconds, true
}

func systemMemoryMeasurementMethod(snapshot systemSnapshot) string {
	if snapshot.MemoryMethod != "" {
		return snapshot.MemoryMethod
	}
	return "freebsd-sysctl-unavailable-v1"
}

func systemCgroupCPUValue(_ cpuAllowance) string { return "unavailable" }

func systemMemoryLimitMachineValue(_ resourceLimits) string { return "unavailable" }

func freeBSDVirtualization(guest, jailed, product string) string {
	if strings.EqualFold(strings.TrimSpace(jailed), "1") {
		return "FreeBSD jail"
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(guest), "none"):
		return "none/unknown"
	case strings.EqualFold(strings.TrimSpace(guest), "generic"):
		product = strings.ToLower(product)
		switch {
		case strings.Contains(product, "qemu"):
			return "QEMU"
		case strings.Contains(product, "bhyve"):
			return "bhyve"
		default:
			return "virtual machine"
		}
	case strings.TrimSpace(guest) != "":
		return strings.TrimSpace(guest)
	default:
		return "unknown"
	}
}

func freeBSDAESStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return "available"
	case "0", "false", "no":
		return "unavailable"
	default:
		// An OID can be absent because the architecture does not expose that
		// particular sysctl, not because AES instructions are unavailable.
		return "unknown"
	}
}

func freeBSDPhysicalCores(ctx context.Context, logical int) (int, bool) {
	return freeBSDPhysicalCoresFromValues(logical,
		freeBSDSysctlValue(ctx, "kern.smp.cores"),
		freeBSDSysctlValue(ctx, "kern.smp.threads_per_core"))
}

func freeBSDPhysicalCoresFromValues(logical int, coresValue, threadsValue string) (int, bool) {
	cores, coresOK := parseFreeBSDUint(coresValue)
	threads, threadsOK := parseFreeBSDUint(threadsValue)
	if !coresOK || !threadsOK || cores == 0 || threads == 0 || logical <= 0 || threads > ^uint64(0)/cores {
		return 0, false
	}
	if cores*threads != uint64(logical) {
		return 0, false
	}
	return int(cores), true
}
