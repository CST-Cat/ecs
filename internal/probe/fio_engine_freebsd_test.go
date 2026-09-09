//go:build freebsd

package probe

import "testing"

func TestFIOEngineCandidatesPreferPOSIXAIO(t *testing.T) {
	candidates := fioEngineCandidates()
	if len(candidates) != 2 || candidates[0].name != "posixaio" || !candidates[0].async ||
		candidates[1].name != "psync" || candidates[1].async {
		t.Fatalf("FreeBSD fio candidates = %+v", candidates)
	}
}
