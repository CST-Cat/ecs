//go:build linux

package probe

func fioEngineCandidates() []fioEngineCandidate {
	return []fioEngineCandidate{
		{name: "io_uring", async: true},
		{name: "libaio", async: true},
		{name: "psync", async: false},
	}
}

func fioEngineProbeFailureRequiresFallback(output []byte) bool {
	return len(output) == 0
}

func fioEngineFallback(_ error, _ map[string]bool) fioEngine {
	// Keep the existing Unix safety fallback for environments whose fio build
	// does not expose an async engine. Windows has a stricter native contract.
	return fioEngine{Name: "psync", AsyncQueue: false}
}
