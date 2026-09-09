//go:build freebsd

package probe

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFreeBSDMemoryUsageUsesVMPageStatistics(t *testing.T) {
	values := map[string]string{
		"hw.physmem":                   "16777216000",
		"vm.stats.vm.v_page_size":      "4096",
		"vm.stats.vm.v_free_count":     "100",
		"vm.stats.vm.v_inactive_count": "200",
		"vm.stats.vm.v_cache_count":    "300",
	}
	got := freeBSDMemoryUsage(values)
	if !got.AvailableKnown || got.HostTotalBytes != 16777216000 || got.HostAvailableBytes != 600*4096 || got.HostUsedBytes != 16777216000-600*4096 {
		t.Fatalf("FreeBSD VM page memory = %+v", got)
	}
	if method := memoryMethodForFreeBSDValues(values); method != "freebsd-sysctl-vmstat-v1" {
		t.Fatalf("page-stat memory method = %q", method)
	}
}

func TestFreeBSDMemoryUsageUsermemFallbackHasDistinctProvenance(t *testing.T) {
	values := map[string]string{"hw.physmem": "1000", "hw.usermem": "400"}
	got := freeBSDMemoryUsage(values)
	if !got.AvailableKnown || got.HostAvailableBytes != 400 || got.HostUsedBytes != 600 {
		t.Fatalf("hw.usermem fallback = %+v", got)
	}
	if method := memoryMethodForFreeBSDValues(values); method != "freebsd-sysctl-hw-physmem-usermem-v1" {
		t.Fatalf("hw.usermem method = %q", method)
	}
	zero := map[string]string{"hw.physmem": "1000", "hw.usermem": "0"}
	if memory := freeBSDMemoryUsage(zero); memory.AvailableKnown || memoryMethodForFreeBSDValues(zero) != "freebsd-sysctl-hw-physmem-v1" {
		t.Fatalf("zero hw.usermem was treated as available: %+v / %q", memory, memoryMethodForFreeBSDValues(zero))
	}
}

func TestFreeBSDMemoryTotalOnlyKeepsPhysmemProvenance(t *testing.T) {
	values := map[string]string{"hw.physmem": "4096"}
	if method := memoryMethodForFreeBSDValues(values); method != "freebsd-sysctl-hw-physmem-v1" {
		t.Fatalf("total-only memory method = %q", method)
	}
	snapshot := systemSnapshot{
		MemoryTotal: 4096, MemoryTotalKnown: true, MemoryMethod: "freebsd-sysctl-hw-physmem-v1",
	}
	for _, measurement := range stableSystemMeasurements(snapshot, EnvironmentSnapshot{}) {
		if measurement.Key == "memory_total_bytes" {
			if measurement.Method != "freebsd-sysctl-hw-physmem-v1" || measurement.Value != 4096 {
				t.Fatalf("total-only measurement = %+v", measurement)
			}
			return
		}
	}
	t.Fatal("total-only memory measurement was omitted")
}

func TestFreeBSDAESStatusPreservesUnknownOID(t *testing.T) {
	for _, test := range []struct {
		value, want string
	}{
		{value: "1", want: "available"},
		{value: "yes", want: "available"},
		{value: "0", want: "unavailable"},
		{value: "no", want: "unavailable"},
		{value: "", want: "unknown"},
		{value: "oid not found", want: "unknown"},
	} {
		if got := freeBSDAESStatus(test.value); got != test.want {
			t.Fatalf("freeBSDAESStatus(%q) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestFreeBSDSystemParsersRejectUnavailableOrMalformedFacts(t *testing.T) {
	if _, ok := parseFreeBSDUint("malformed 42"); ok {
		t.Fatal("malformed scalar was accepted as a FreeBSD fact")
	}
	if got := parseFreeBSDLoadAverage("{ 0.10 0.20 0.30 }"); got != "0.10 / 0.20 / 0.30" {
		t.Fatalf("load average = %q", got)
	}
	if parseFreeBSDLoadAverage("{ not-a-load }") != "" {
		t.Fatal("malformed load average was accepted")
	}
	if parseFreeBSDLoadAverage("{ NaN +Inf 0.30 }") != "" {
		t.Fatal("non-finite load average was accepted")
	}
	for _, test := range []struct {
		guest, jailed, product, want string
	}{
		{guest: "generic", product: "QEMU Virtual Machine", want: "QEMU"},
		{guest: "generic", jailed: "1", product: "QEMU Virtual Machine", want: "FreeBSD jail"},
		{guest: "none", want: "none/unknown"},
		{guest: "generic", product: "unknown", want: "virtual machine"},
		{guest: "generic", product: "", want: "virtual machine"},
	} {
		if got := freeBSDVirtualization(test.guest, test.jailed, test.product); got != test.want {
			t.Fatalf("FreeBSD virtualization(%q,%q,%q) = %q, want %q", test.guest, test.jailed, test.product, got, test.want)
		}
	}
	now := time.Unix(2_000, 0)
	if uptime, ok := parseFreeBSDBootTime("{ sec = 1234, usec = 0 }", now); !ok || uptime != 766 {
		t.Fatalf("boot time = %d/%v", uptime, ok)
	}
	if _, ok := parseFreeBSDBootTime("{ sec = 3000, usec = 0 }", now); ok {
		t.Fatal("future boot time was accepted")
	}
	partial := map[string]string{
		"hw.physmem":               "1000",
		"vm.stats.vm.v_page_size":  "4096",
		"vm.stats.vm.v_free_count": "1",
	}
	if got := freeBSDMemoryUsage(partial); got.AvailableKnown || got.HostUsedBytes != 0 || got.HostUsagePercent != 0 || memoryMethodForFreeBSDValues(partial) != "freebsd-sysctl-hw-physmem-v1" {
		t.Fatalf("partial page statistics were treated as complete: %+v / %q", got, memoryMethodForFreeBSDValues(partial))
	}
}

func TestFreeBSDPhysicalCoresRequireConsistentNativeTopology(t *testing.T) {
	if cores, ok := freeBSDPhysicalCoresFromValues(2, "2", "2"); ok || cores != 0 {
		t.Fatalf("inconsistent topology fixture result = %d/%v", cores, ok)
	}
	if cores, ok := freeBSDPhysicalCoresFromValues(8, "4", "2"); !ok || cores != 4 {
		t.Fatalf("consistent topology fixture result = %d/%v", cores, ok)
	}
	if got := systemCPUTopologyValue(systemSnapshot{LogicalCPUs: 2}); got != "logical=2;physical=unknown" {
		t.Fatalf("unknown physical topology = %q", got)
	}
}

func TestFreeBSDEnvironmentDoesNotExposeLinuxResourceFacts(t *testing.T) {
	snapshot := CaptureEnvironmentSnapshot()
	if snapshot.CPUTracked || snapshot.CPUStat.Present || snapshot.Memory.Present {
		t.Fatalf("FreeBSD exposed Linux CPU/cgroup facts: %+v", snapshot)
	}
	for resource, pressure := range snapshot.PSI {
		if pressure.Some.Present || pressure.Full.Present || pressure.Source != "" {
			t.Fatalf("FreeBSD exposed PSI for %s: %+v", resource, pressure)
		}
	}
	injected := EnvironmentSnapshot{
		Limits: resourceLimits{CPU: cpuAllowance{Quota: 1}, MemoryLimit: 1},
		PSI:    map[string]psiResource{"cpu": {Some: psiValues{Present: true}}},
	}
	if measurements := systemResourceMeasurements(injected); len(measurements) != 0 {
		t.Fatalf("FreeBSD emitted Linux resource measurements from injected facts: %+v", measurements)
	}
	injectedSnapshot := systemSnapshot{
		LogicalCPUs: 1, Allowance: cpuAllowance{Visible: 1, Threads: 1},
		MemoryTotal: 1, MemoryTotalKnown: true, MemoryMethod: "freebsd-sysctl-vmstat-v1",
		MemoryLimit: 1, StealKnown: true, StealPercent: 1,
	}
	for _, measurement := range stableSystemMeasurements(injectedSnapshot, injected) {
		if strings.Contains(measurement.Method, "proc-stat-steal") || strings.Contains(measurement.Method, "cgroup-memory") || strings.Contains(measurement.Method, "linux-psi") {
			t.Fatalf("FreeBSD emitted Linux method from injected snapshot: %+v", measurement)
		}
	}
}

func TestFreeBSDSystemResultUsesNativeMethodsAndUnavailableLinuxFacts(t *testing.T) {
	snapshot := collectSystem(context.Background(), "/")
	result := buildSystemResult(time.Now(), snapshot, CaptureEnvironmentSnapshot(), cloudIdentity{})
	appendKernelNetworkParams(&result)
	fields := make(map[string]string, len(result.Fields))
	for _, field := range result.Fields {
		fields[field.Key] = field.Value.Text()
	}
	for _, key := range []string{"os", "kernel", "arch", "cpu_model", "memory_total", "memory_used", "memory_available", "disk_total", "disk_used", "disk_available", "uptime_seconds", "load"} {
		if fields[key] == "" || fields[key] == "unknown" || fields[key] == "unavailable" {
			t.Fatalf("FreeBSD core field %s = %q; snapshot=%+v", key, fields[key], snapshot)
		}
	}
	for _, key := range []string{"cgroup_cpu_quota", "cgroup_cpuset", "cgroup_cpuset_source", "cgroup_memory_limit_bytes", "cgroup_memory_limit_source", "cgroup_memory_current_bytes", "cgroup_memory_current_source", "cgroup_memory_swap_limit_bytes", "cgroup_memory_swap_limit_source"} {
		if fields[key] != "unavailable" {
			t.Fatalf("FreeBSD Linux resource field %s = %q", key, fields[key])
		}
	}
	if fields["aes"] != "unknown" {
		t.Fatalf("FreeBSD arm64 missing hw.aesni OID became %q", fields["aes"])
	}
	if fields["tcp_available_congestion"] != "cubic" && fields["tcp_available_congestion"] != "unknown" {
		t.Fatalf("FreeBSD congestion-control list is not normalized: %q", fields["tcp_available_congestion"])
	}
	for _, measurement := range result.Measurements {
		for _, forbidden := range []string{"proc-meminfo-v1", "proc-stat-steal", "cgroup-memory", "linux-psi"} {
			if strings.Contains(measurement.Method, forbidden) {
				t.Fatalf("FreeBSD measurement %s uses Linux method %q", measurement.Key, measurement.Method)
			}
		}
		if measurement.Key == "tcp_rmem_max_bytes" || measurement.Key == "tcp_single_flow_window_limit_150ms_mbps" {
			t.Fatalf("FreeBSD reported a receive-buffer max/BDP measurement: %+v", measurement)
		}
	}
}

func TestFreeBSDRestrictedMemoryDoesNotEmitLinuxFallbackNote(t *testing.T) {
	result := newMemoryResult()
	appendMemoryInventory(&result, memoryUsageSnapshot{}, memoryFacility{}, memoryFacility{})
	for _, note := range result.Notes {
		if note == "probe.memory.note.memavailable_legacy_fallback" {
			t.Fatal("FreeBSD emitted Linux MemAvailable fallback note")
		}
	}
}

func TestSystemUnavailableDiskDoesNotBecomeZeroMeasurement(t *testing.T) {
	snapshot := systemSnapshot{LogicalCPUs: 1, Allowance: cpuAllowance{Visible: 1, Threads: 1}}
	result := buildSystemResult(time.Unix(100, 0), snapshot, EnvironmentSnapshot{}, cloudIdentity{})
	fields := make(map[string]string, len(result.Fields))
	for _, field := range result.Fields {
		fields[field.Key] = field.Value.Text()
	}
	for _, key := range []string{"disk_total", "disk_used", "disk_available", "disk_usage_percent"} {
		if fields[key] != "unavailable" {
			t.Fatalf("unknown disk field %s = %q", key, fields[key])
		}
	}
	for _, measurement := range result.Measurements {
		if strings.HasPrefix(measurement.Key, "disk_") {
			t.Fatalf("unknown disk emitted measurement %+v", measurement)
		}
	}
	snapshot.DiskTotal = 1024
	result = buildSystemResult(time.Unix(100, 0), snapshot, EnvironmentSnapshot{}, cloudIdentity{})
	for _, field := range result.Fields {
		if strings.HasPrefix(field.Key, "disk_") && field.Value.Text() != "unavailable" {
			t.Fatalf("untrusted disk value rendered as known: %s=%q", field.Key, field.Value.Text())
		}
	}
}

func TestFreeBSDRestrictedFactsRemainUnavailable(t *testing.T) {
	memory := freeBSDMemoryUsage(map[string]string{})
	if memory.AvailableKnown || memory.HostTotalBytes != 0 || memory.EffectiveTotalBytes != 0 || memoryMethodForFreeBSDValues(nil) != "freebsd-sysctl-unavailable-v1" {
		t.Fatalf("empty restricted FreeBSD memory facts = %+v / %q", memory, memoryMethodForFreeBSDValues(nil))
	}
	snapshot := systemSnapshot{
		OS: "FreeBSD", Arch: "arm64", MemoryMethod: "freebsd-sysctl-unavailable-v1",
		MemoryTotalKnown: false, MemoryUsedKnown: false, MemoryAvailableKnown: false,
		SwapKnown: false, DiskKnown: false,
	}
	result := buildSystemResult(time.Unix(100, 0), snapshot, EnvironmentSnapshot{}, cloudIdentity{})
	for _, field := range result.Fields {
		if strings.HasPrefix(field.Key, "memory_") || field.Key == "swap" || strings.HasPrefix(field.Key, "disk_") {
			if field.Value.Text() == "0 B" || field.Value.Text() == "0.0 %" {
				t.Fatalf("restricted FreeBSD fact rendered as zero: %s=%q", field.Key, field.Value.Text())
			}
		}
	}
}
