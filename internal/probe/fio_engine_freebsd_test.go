//go:build freebsd

package probe

import (
	"errors"
	"testing"
)

func TestFIOEngineCandidatesPreferPOSIXAIO(t *testing.T) {
	candidates := fioEngineCandidates()
	if len(candidates) != 2 || candidates[0].name != "posixaio" || !candidates[0].async ||
		candidates[1].name != "psync" || candidates[1].async {
		t.Fatalf("FreeBSD fio candidates = %+v", candidates)
	}
}

func TestFreeBSDFailedEngineProbeWithOutputKeepsCandidateContract(t *testing.T) {
	failed := errors.New("fixture --enghelp failure")
	got := fioEngineFromProbe(failed, []byte("posixaio\npsync\n"))
	if got.Name != "posixaio" || !got.AsyncQueue || !got.Detected {
		t.Fatalf("FreeBSD failed probe with candidates = %+v, want posixaio", got)
	}
	got = fioEngineFromProbe(failed, nil)
	if got.Name != "psync" || got.Detected || got.AsyncQueue {
		t.Fatalf("FreeBSD failed probe without output = %+v, want legacy psync fallback", got)
	}
	got = fioEngineFromProbe(nil, []byte("unrelated\n"))
	if got.Name != "psync" || got.Detected || got.AsyncQueue {
		t.Fatalf("FreeBSD successful probe without candidates = %+v, want legacy psync fallback", got)
	}
}
