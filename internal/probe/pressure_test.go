package probe

import (
	"testing"
	"time"

	"ecs/internal/model"
)

func TestAssessBenchmarkInterferenceDistinguishesCleanAndContendedSnapshots(t *testing.T) {
	start := time.Unix(100, 0)
	cleanBefore := EnvironmentSnapshot{
		CapturedAt: start,
		CPUTimes:   cpuTimeSample{Total: 1000, Steal: 10},
		CPUTracked: true,
	}
	cleanAfter := cleanBefore
	cleanAfter.CapturedAt = start.Add(time.Second)
	cleanAfter.CPUTimes = cpuTimeSample{Total: 1100, Steal: 11}
	if assessment := AssessBenchmarkInterference("cpu", cleanBefore, cleanAfter); assessment.Detected {
		t.Fatalf("clean snapshot reported interference: %+v", assessment)
	}

	busyBefore := cleanBefore
	busyAfter := cleanAfter
	busyAfter.CPUTimes.Steal = 70
	assessment := AssessBenchmarkInterference("cpu", busyBefore, busyAfter)
	if !assessment.Detected || assessment.Score == 0 {
		t.Fatalf("contended snapshot was not detected: %+v", assessment)
	}
}

func TestFinalizeBenchmarkRetryChoosesLowerInterferenceValidResult(t *testing.T) {
	first := benchmarkRetryResult("first", 100)
	second := benchmarkRetryResult("second", 50)
	selected := FinalizeBenchmarkRetry(
		first, model.Interference{Score: 5},
		second, model.Interference{Score: 1},
	)
	if selected.Retry == nil || selected.Retry.SelectedAttempt != 2 || len(selected.Measurements) == 0 || selected.Measurements[0].Value != 50 {
		t.Fatalf("retry selection = %+v", selected)
	}
}

func benchmarkRetryResult(id string, value float64) model.Result {
	return model.Result{
		ID: id, Title: id, Status: model.StatusOK,
		Evidence:     model.NewEvidence(1, 1, "run"),
		Measurements: []model.Measurement{{Key: "rate", Value: value}},
	}
}
