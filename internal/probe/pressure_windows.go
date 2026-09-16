//go:build windows

package probe

// Native Windows has no Linux PSI, cgroup counter, or steal-time interface in
// this probe.  Capture only the portable shape needed by the renderer; every
// unavailable resource fact remains absent from the snapshot.
func capturePlatformEnvironment(snapshot *EnvironmentSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Limits = resourceLimits{CPU: detectCPUAllowance()}
}

func platformPressureFactsAvailable() bool { return false }

func platformLoadAverageAvailable() bool { return false }

func platformLoadAverageMethod() string { return "win32-load-average-unavailable-v1" }
