//go:build linux

package probe

import (
	"errors"
	"testing"
)

func TestFIOEngineCandidatesPreserveLinuxOrder(t *testing.T) {
	candidates := fioEngineCandidates()
	if len(candidates) != 3 || candidates[0].name != "io_uring" || !candidates[0].async ||
		candidates[1].name != "libaio" || !candidates[1].async ||
		candidates[2].name != "psync" || candidates[2].async {
		t.Fatalf("Linux fio candidates = %+v", candidates)
	}
}

func TestLinuxFailedEngineProbeWithOutputKeepsCandidateContract(t *testing.T) {
	failed := errors.New("fixture --enghelp failure")
	got := fioEngineFromProbe(failed, []byte("io_uring\npsync\n"))
	if got.Name != "io_uring" || !got.AsyncQueue || !got.Detected {
		t.Fatalf("Linux failed probe with candidates = %+v, want io_uring", got)
	}
	got = fioEngineFromProbe(failed, nil)
	if got.Name != "psync" || got.Detected || got.AsyncQueue {
		t.Fatalf("Linux failed probe without output = %+v, want legacy psync fallback", got)
	}
	got = fioEngineFromProbe(nil, []byte("unrelated\n"))
	if got.Name != "psync" || got.Detected || got.AsyncQueue {
		t.Fatalf("Linux successful probe without candidates = %+v, want legacy psync fallback", got)
	}
}
