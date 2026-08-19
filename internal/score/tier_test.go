package score

import "testing"

func TestTierSelectsReferenceForRepresentativeHost(t *testing.T) {
	baseline := Baseline{
		SampleCount: 6,
		Metrics:     map[string]float64{"cpu_multi": 100},
		Tiers: []Tier{{
			VCPUMin: 4, SampleCount: 5,
			Metrics:            map[string]float64{"cpu_multi": 200},
			MetricSampleCounts: map[string]int{"cpu_multi": 5},
		}},
	}

	if TierKeyFor(6) != 4 {
		t.Fatalf("TierKeyFor(6) = %d, want 4", TierKeyFor(6))
	}
	metrics, tierMin, samples := baseline.MetricsForHost(6)
	if tierMin != 4 || samples != 5 || metrics["cpu_multi"] != 200 {
		t.Fatalf("host reference = metrics %v, tier %d, samples %d; want tier value 200", metrics, tierMin, samples)
	}
}
