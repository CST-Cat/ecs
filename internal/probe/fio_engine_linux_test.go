//go:build linux

package probe

import "testing"

func TestFIOEngineCandidatesPreserveLinuxOrder(t *testing.T) {
	candidates := fioEngineCandidates()
	if len(candidates) != 3 || candidates[0].name != "io_uring" || !candidates[0].async ||
		candidates[1].name != "libaio" || !candidates[1].async ||
		candidates[2].name != "psync" || candidates[2].async {
		t.Fatalf("Linux fio candidates = %+v", candidates)
	}
}
