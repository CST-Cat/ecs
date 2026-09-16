package probe

import (
	"math"
	"testing"
)

func TestAggregateICMPSamplesPreservesLossAndRTTSemantics(t *testing.T) {
	stats := aggregateICMPSamples([]icmpSample{
		{Sent: true, Received: true, RTTMS: 1},
		{Sent: true, Received: false},
		{Sent: true, Received: true, RTTMS: 3},
	})
	if !stats.Available || !stats.LossKnown || math.Abs(stats.LossPercent-33.33333333333333) > 1e-12 {
		t.Fatalf("aggregated loss = %+v", stats)
	}
	if !stats.RTTKnown || stats.MinMS != 1 || stats.AvgMS != 2 || stats.MaxMS != 3 || !stats.StdDevKnown || stats.StdDevMS != 1 {
		t.Fatalf("aggregated RTT = %+v", stats)
	}

	timeout := aggregateICMPSamples([]icmpSample{{Sent: true}, {Sent: true}})
	if !timeout.Available || !timeout.LossKnown || timeout.LossPercent != 100 || timeout.RTTKnown || timeout.StdDevKnown {
		t.Fatalf("all-loss aggregation = %+v", timeout)
	}
}
