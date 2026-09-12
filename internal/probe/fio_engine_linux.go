//go:build linux

package probe

func fioEngineCandidates() []fioEngineCandidate {
	return []fioEngineCandidate{
		{name: "io_uring", async: true},
		{name: "libaio", async: true},
		{name: "psync", async: false},
	}
}
