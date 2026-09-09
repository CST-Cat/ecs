//go:build freebsd

package probe

// FreeBSD does not expose Linux PSI or cgroup counters.  Keep the snapshot
// populated only with the portable load fact; all Linux resource fields stay
// unavailable and BuildPressureMeasurements emits no Linux method IDs.
func capturePlatformEnvironment(snapshot *EnvironmentSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Limits = resourceLimits{CPU: detectCPUAllowance()}
	snapshot.Load1, snapshot.LoadKnown = readLoadAverage1()
}

func platformPressureFactsAvailable() bool { return false }
