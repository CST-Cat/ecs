//go:build windows

package probe

// Windows does not expose a cgroup quota or the Linux CPU accounting counter.
// Keep the runtime-visible CPU count as the allowance and leave those
// platform-specific facts absent instead of treating a missing interface as
// an unlimited or zero-valued measurement.
func platformCPUQuota() (float64, string, bool) {
	return 0, "", false
}

func readCPUTimes() (cpuTimeSample, bool) {
	return cpuTimeSample{}, false
}
