//go:build windows

package probe

import (
	"errors"
	"testing"
)

func TestFIOEngineCandidatesPreferWindowsAIO(t *testing.T) {
	candidates := fioEngineCandidates()
	if len(candidates) != 2 || candidates[0].name != "windowsaio" || !candidates[0].async ||
		candidates[1].name != "psync" || candidates[1].async {
		t.Fatalf("Windows fio candidates = %+v", candidates)
	}
}

func TestWindowsFailedEngineProbeIsStrict(t *testing.T) {
	got := fioEngineFromProbe(errors.New("fixture --enghelp failure"), []byte("windowsaio\npsync\n"))
	if got.Name != "" || got.Detected {
		t.Fatalf("Windows failed probe with candidates = %+v, want unavailable", got)
	}
	got = fioEngineFromProbe(nil, []byte("windowsaio\npsync\n"))
	if got.Name != "windowsaio" || !got.AsyncQueue || !got.Detected {
		t.Fatalf("Windows successful windowsaio probe = %+v", got)
	}
	got = fioEngineFromProbe(nil, []byte("psync\n"))
	if got.Name != "psync" || got.AsyncQueue || !got.Detected {
		t.Fatalf("Windows validated sync fallback = %+v", got)
	}
	got = fioEngineFromProbe(nil, []byte("unrelated\n"))
	if got.Name != "" || got.Detected {
		t.Fatalf("Windows successful probe without candidate = %+v, want unavailable", got)
	}
}

func TestFIOEngineFallbackRequiresSuccessfulCandidateContract(t *testing.T) {
	if got := fioEngineFallback(errors.New("fixture engine probe failure"), map[string]bool{"psync": true}); got.Name != "" {
		t.Fatalf("failed engine probe fallback = %+v, want unavailable", got)
	}
	if got := fioEngineFallback(nil, map[string]bool{}); got.Name != "" {
		t.Fatalf("unadvertised sync fallback = %+v, want unavailable", got)
	}
	if got := fioEngineFallback(nil, map[string]bool{"psync": true}); got.Name != "psync" || !got.Detected || got.AsyncQueue {
		t.Fatalf("validated sync fallback = %+v", got)
	}
}
