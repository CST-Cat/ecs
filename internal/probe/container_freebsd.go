//go:build freebsd

package probe

// FreeBSD has no Linux cgroup CPU quota or /proc/stat steal-time interface.
// Returning unavailable keeps CPU allowance and steal facts honest; the
// runtime CPU count remains the visible execution allowance.
func platformCPUQuota() (float64, string, bool) {
	return 0, "", false
}

func readCPUTimes() (cpuTimeSample, bool) {
	return cpuTimeSample{}, false
}
