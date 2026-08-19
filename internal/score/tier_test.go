package score

import "testing"

func TestTierLabelsAndReferenceFallback(t *testing.T) {
	labels := []struct {
		vcpu  int
		key   int
		label string
	}{
		{vcpu: 0, key: 0, label: "0 vCPU"},
		{vcpu: 1, key: 1, label: "1 vCPU"},
		{vcpu: 6, key: 4, label: "4–7 vCPU"},
		{vcpu: 64, key: 64, label: "64+ vCPU"},
	}
	for _, test := range labels {
		if got := TierKeyFor(test.vcpu); got != test.key || TierLabel(got) != test.label {
			t.Fatalf("tier %d = key %d/label %q, want %d/%q", test.vcpu, got, TierLabel(got), test.key, test.label)
		}
	}

	baseline := Baseline{
		SampleCount: 10,
		Metrics:     map[string]float64{"cpu_single": 100, "disk_seq_read": 100},
		Tiers: []Tier{{
			VCPUMin: 4, SampleCount: 6,
			Metrics:            map[string]float64{"cpu_single": 200, "disk_seq_read": 300},
			MetricSampleCounts: map[string]int{"cpu_single": 5, "disk_seq_read": 2},
		}},
	}
	metrics, tierMin, samples := baseline.MetricsForHost(6)
	if tierMin != 4 || samples != 6 || metrics["cpu_single"] != 200 || metrics["disk_seq_read"] != 100 {
		t.Fatalf("tier reference = %v, tier %d, samples %d", metrics, tierMin, samples)
	}
	metrics["cpu_single"] = 999
	if baseline.Metrics["cpu_single"] != 100 || baseline.Tiers[0].Metrics["cpu_single"] != 200 {
		t.Fatal("tier reference mutation leaked into baseline")
	}

	insufficient := baseline
	insufficient.Tiers = append([]Tier(nil), baseline.Tiers...)
	insufficient.Tiers[0].MetricSampleCounts = map[string]int{"cpu_single": 4, "disk_seq_read": 2}
	legacy := baseline
	legacy.Tiers = append([]Tier(nil), baseline.Tiers...)
	legacy.Tiers[0].MetricSampleCounts = nil
	for _, test := range []struct {
		name       string
		baseline   Baseline
		vcpu       int
		wantMetric float64
		wantMin    int
		wantSample int
	}{
		{name: "tier metric fallback", baseline: insufficient, vcpu: 6, wantMetric: 100, wantMin: 0, wantSample: 10},
		{name: "legacy tier fallback", baseline: legacy, vcpu: 6, wantMetric: 100, wantMin: 0, wantSample: 10},
		{name: "missing tier fallback", baseline: baseline, vcpu: 32, wantMetric: 100, wantMin: 0, wantSample: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, gotMin, gotSamples := test.baseline.MetricsForHost(test.vcpu)
			if got["cpu_single"] != test.wantMetric || gotMin != test.wantMin || gotSamples != test.wantSample {
				t.Fatalf("reference = %v, tier %d, samples %d", got, gotMin, gotSamples)
			}
		})
	}
}
