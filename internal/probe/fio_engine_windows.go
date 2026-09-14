//go:build windows

package probe

// fioEngineCandidates lists actual fio ioengines, in descending preference.
// windowsaio is the native asynchronous engine and therefore the only Windows
// candidate that can preserve the requested queue depths without downgrading
// the methodology. psync is retained solely as an explicit, validated sync
// fallback when a real fio --enghelp response advertises it.
func fioEngineCandidates() []fioEngineCandidate {
	return []fioEngineCandidate{
		{name: "windowsaio", async: true},
		{name: "psync", async: false},
	}
}

func fioEngineProbeFailureRequiresFallback([]byte) bool { return true }

func fioEngineFallback(err error, available map[string]bool) fioEngine {
	if err != nil || !available["psync"] {
		return fioEngine{}
	}
	return fioEngine{Name: "psync", AsyncQueue: false, Detected: true}
}
