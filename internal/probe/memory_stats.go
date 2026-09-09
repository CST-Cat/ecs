package probe

// Memory inventory helpers shared by the system and memory probes. The
// platform-specific source readers live in memory_probe_linux.go and
// memory_probe_freebsd.go.

// memoryUsageSnapshot keeps host-visible values and effective values after a
// Linux cgroup limit is applied. FreeBSD fills the host fields from native VM
// page statistics and never applies Linux cgroup arithmetic.
type memoryUsageSnapshot struct {
	HostTotalBytes     uint64
	HostUsedBytes      uint64
	HostAvailableBytes uint64
	HostUsagePercent   float64

	EffectiveTotalBytes     uint64
	EffectiveUsedBytes      uint64
	EffectiveAvailableBytes uint64
	EffectiveUsagePercent   float64
	AvailableKnown          bool
	LimitApplied            bool
	EffectiveCurrentKnown   bool
	EffectiveAvailableKnown bool
}

func memoryUsageFromMemInfo(mem map[string]uint64, limit uint64) memoryUsageSnapshot {
	result := memoryUsageSnapshot{}
	result.HostTotalBytes = mem["MemTotal"] * 1024
	if available, ok := mem["MemAvailable"]; ok {
		result.HostAvailableBytes = available * 1024
		result.AvailableKnown = true
	} else {
		// MemAvailable was added in Linux 3.14. This fallback mirrors the
		// existing Linux behavior while retaining the evidence boundary.
		result.HostAvailableBytes = (mem["MemFree"] + mem["Buffers"] + mem["Cached"]) * 1024
	}
	if result.HostTotalBytes > 0 && result.HostAvailableBytes > result.HostTotalBytes {
		result.HostAvailableBytes = result.HostTotalBytes
	}
	if result.HostTotalBytes >= result.HostAvailableBytes {
		result.HostUsedBytes = result.HostTotalBytes - result.HostAvailableBytes
	}
	if result.HostTotalBytes > 0 {
		result.HostUsagePercent = float64(result.HostUsedBytes) / float64(result.HostTotalBytes) * 100
	}

	result.EffectiveTotalBytes = result.HostTotalBytes
	if limit > 0 {
		if result.EffectiveTotalBytes == 0 || limit < result.EffectiveTotalBytes {
			result.EffectiveTotalBytes = limit
		}
		result.LimitApplied = true
	}
	result.EffectiveAvailableBytes = result.HostAvailableBytes
	if result.EffectiveAvailableBytes > result.EffectiveTotalBytes && result.EffectiveTotalBytes > 0 {
		result.EffectiveAvailableBytes = result.EffectiveTotalBytes
	}
	result.EffectiveAvailableKnown = result.AvailableKnown
	if result.EffectiveTotalBytes >= result.EffectiveAvailableBytes {
		result.EffectiveUsedBytes = result.EffectiveTotalBytes - result.EffectiveAvailableBytes
	}
	if result.EffectiveTotalBytes > 0 {
		result.EffectiveUsagePercent = float64(result.EffectiveUsedBytes) / float64(result.EffectiveTotalBytes) * 100
	}
	return result
}

// memoryFacility is the result of an optional kernel facility probe. Evidence
// is a path and a small value summary, never a claim based on virtualization.
type memoryFacility struct {
	Available bool
	Evidence  string
}

func (f memoryFacility) Status() string {
	if f.Available {
		return "available"
	}
	return "unavailable"
}
