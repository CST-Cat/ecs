package probe

import "testing"

func TestObservedASPathASNsDeduplicatesPrepends(t *testing.T) {
	got := observedASPathASNs("64500 64500 {64501,64502} 64503")
	want := []int{64500, 64503}
	if len(got) != len(want) {
		t.Fatalf("observed ASNs = %v, want %v", got, want)
	}
	for index, value := range want {
		if got[index] != value {
			t.Fatalf("observed ASNs = %v, want %v", got, want)
		}
	}
}
