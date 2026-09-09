package probe

import (
	"testing"
	"time"
)

// These parser and arithmetic checks are platform-neutral. Linux-specific
// pressure method catalogs remain in pressure_test.go, but the common facts
// must continue to be exercised by every supported OS build.
func TestPressureParsersAndCalculations(t *testing.T) {
	parsed := parsePSI("some avg10=1.50 avg60=2.00 avg300=3.00 total=100\nfull avg10=0.10 avg60=0.20 avg300=0.30 total=20\n")
	if !parsed.Some.Present || parsed.Some.Avg10 != 1.5 || parsed.Some.TotalUS != 100 || !parsed.Full.Present || parsed.Full.Avg10 != 0.1 || parsed.Full.TotalUS != 20 {
		t.Fatalf("portable PSI parse = %+v", parsed)
	}
	if malformed := parsePSI("not-a-psi-line\n"); malformed.Some.Present || malformed.Full.Present {
		t.Fatalf("malformed PSI reported as present: %+v", malformed)
	}
	if delta, ok := counterDelta(2, 5); !ok || delta != 3 {
		t.Fatalf("counter delta = %v/%v", delta, ok)
	}
	if delta, ok := counterDelta(5, 2); ok || delta != 0 {
		t.Fatalf("counter rollback delta = %v/%v", delta, ok)
	}
	if value, ok := pressurePercent(psiValues{TotalUS: 100, Present: true}, psiValues{TotalUS: 350_100, Present: true}, time.Second); !ok || value != 35 {
		t.Fatalf("pressure percentage = %v/%v", value, ok)
	}
	if value, ok := pressurePercent(psiValues{Present: true}, psiValues{Present: true, TotalUS: 2_000_000}, time.Second); !ok || value != 100 {
		t.Fatalf("pressure percentage clamp = %v/%v", value, ok)
	}
	if _, ok := pressurePercent(psiValues{TotalUS: 5, Present: true}, psiValues{TotalUS: 2, Present: true}, time.Second); ok {
		t.Fatal("PSI counter rollback produced a percentage")
	}
	if _, ok := pressurePercent(psiValues{}, parsed.Some, time.Second); ok {
		t.Fatal("unavailable PSI produced a percentage")
	}
	if _, ok := pressurePercent(parsed.Some, parsed.Some, 0); ok {
		t.Fatal("zero-length window produced a percentage")
	}
}
