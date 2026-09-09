//go:build freebsd

package probe

func fioEngineCandidates() []fioEngineCandidate {
	return []fioEngineCandidate{
		{name: "posixaio", async: true},
		{name: "psync", async: false},
	}
}
