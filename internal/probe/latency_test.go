package probe

import (
	"testing"
	"time"
)

func TestLatencyStatisticsAndEmptyFallback(t *testing.T) {
	values := []time.Duration{3 * time.Millisecond, time.Millisecond, 2 * time.Millisecond}
	if got := medianDuration(values); got != 2*time.Millisecond {
		t.Fatalf("latency median = %s", got)
	}
	if got := percentileDuration(values, 0.95); got != 3*time.Millisecond {
		t.Fatalf("latency P95 = %s", got)
	}
	if got := stddevFloat([]float64{1, 2, 3}); got < 0.81 || got > 0.82 {
		t.Fatalf("latency jitter = %f", got)
	}
	if percentileDuration(nil, 0.95) != 0 || stddevFloat(nil) != 0 {
		t.Fatal("empty latency samples must remain unavailable")
	}
}
