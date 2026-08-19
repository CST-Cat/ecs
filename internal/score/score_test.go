package score

import (
	"testing"

	"ecs/internal/model"
)

func TestComputeScoresReportAgainstBaseline(t *testing.T) {
	report := model.Report{Results: []model.Result{{
		ID: "cpu", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "sysbench_cpu_single_events_s", Value: 150},
			{Key: "sysbench_cpu_multi_events_s", Value: 50},
		},
	}}}
	baseline := Baseline{
		Schema: BaselineSchema, Source: "test", SampleCount: 1,
		Metrics: map[string]float64{"cpu_single": 100, "cpu_multi": 100},
	}

	got := Compute(report, baseline)
	if got == nil {
		t.Fatal("expected a score for the CPU report")
	}
	if got.Total != 1000 {
		t.Fatalf("total score = %v, want 1000", got.Total)
	}
	if got.Covered != 1 || got.Possible != len(Dimensions()) {
		t.Fatalf("coverage = %d/%d, want one covered dimension out of %d", got.Covered, got.Possible, len(Dimensions()))
	}
}
