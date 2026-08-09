package probe

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestParsePSIAndCPUSet(t *testing.T) {
	parsed := parsePSI("some avg10=1.25 avg60=2.50 avg300=3.75 total=12345\nfull avg10=0.10 avg60=0.20 avg300=0.30 total=456\n")
	if !parsed.Some.Present || parsed.Some.Avg10 != 1.25 || parsed.Some.TotalUS != 12345 ||
		!parsed.Full.Present || parsed.Full.Avg300 != 0.30 || parsed.Full.TotalUS != 456 {
		t.Fatalf("parsePSI = %+v", parsed)
	}
	if got := cpuSetCount("0-3,6,8-9"); got != 7 {
		t.Fatalf("cpuSetCount = %d, want 7", got)
	}
	if got := cpuSetCount("bad,4-2,7"); got != 1 {
		t.Fatalf("invalid cpuset handling = %d, want 1", got)
	}
}

func TestPressurePercentUsesCounterDeltaAndRejectsSubMicrosecondWindows(t *testing.T) {
	before := psiValues{Present: true, TotalUS: 100}
	after := psiValues{Present: true, TotalUS: 250100}
	percent, ok := pressurePercent(before, after, time.Second)
	if !ok || percent != 25 {
		t.Fatalf("pressurePercent = %f, %v", percent, ok)
	}
	if _, ok := pressurePercent(before, after, time.Nanosecond); ok {
		t.Fatal("sub-microsecond pressure window should be rejected")
	}
	if _, ok := pressurePercent(after, before, time.Second); ok {
		t.Fatal("counter reset should be rejected")
	}
}

func TestAssessBenchmarkInterferenceTriggersOnPreexistingPressureAndEvents(t *testing.T) {
	start := time.Unix(100, 0)
	before := EnvironmentSnapshot{
		CapturedAt: start, LoadKnown: true, Load1: 4,
		CPUTimes: cpuTimeSample{Total: 1000, Steal: 10}, CPUTracked: true,
		CPUStat: cgroupCPUStats{NrThrottled: 10, ThrottledUS: 1000, Source: "cpu.stat", Present: true},
		Memory:  cgroupMemoryEvents{OOM: 1, OOMKill: 1, Source: "memory.events", Present: true},
		PSI: map[string]psiResource{
			"cpu":    {Some: psiValues{Avg10: 25, TotalUS: 100, Present: true}},
			"memory": {Some: psiValues{Avg10: 3, TotalUS: 100, Present: true}},
			"io":     {Some: psiValues{Avg10: 7, TotalUS: 100, Present: true}},
		},
		Limits: resourceLimits{CPU: cpuAllowance{Threads: 2}},
	}
	after := before
	after.CapturedAt = start.Add(time.Second)
	after.CPUTimes = cpuTimeSample{Total: 1100, Steal: 12}
	after.CPUStat.NrThrottled = 12
	after.CPUStat.ThrottledUS = 21000
	after.Memory.OOM = 2
	after.Memory.OOMKill = 2
	after.PSI = map[string]psiResource{
		"cpu":    {Some: psiValues{Avg10: 25, TotalUS: 200100, Present: true}},
		"memory": {Some: psiValues{Avg10: 3, TotalUS: 100100, Present: true}},
		"io":     {Some: psiValues{Avg10: 7, TotalUS: 50100, Present: true}},
	}
	assessment := AssessBenchmarkInterference("disk", before, after)
	if !assessment.Detected || assessment.Score < 10 || len(assessment.Reasons) < 5 {
		t.Fatalf("interference was not fully detected: %+v", assessment)
	}
	for _, key := range []string{"pretest_load_1m", "cpu_steal_percent_window", "cgroup_cpu_throttled_time_percent_window", "io_psi_some_percent_window", "cgroup_oom_kill_events_window"} {
		if !hasMeasurementKey(assessment.Measurements, key) {
			t.Errorf("interference measurements missing %q: %+v", key, assessment.Measurements)
		}
	}
}

func TestWorkloadGeneratedPSIIsReportedWithoutUnconditionalRetry(t *testing.T) {
	start := time.Unix(100, 0)
	before := EnvironmentSnapshot{
		CapturedAt: start,
		PSI: map[string]psiResource{
			"cpu": {Some: psiValues{Avg10: 0.5, TotalUS: 0, Present: true}},
		},
		Limits: resourceLimits{CPU: cpuAllowance{Threads: 2}},
	}
	after := before
	after.CapturedAt = start.Add(time.Second)
	after.PSI = map[string]psiResource{
		"cpu": {Some: psiValues{Avg10: 10, TotalUS: 800000, Present: true}},
	}
	assessment := AssessBenchmarkInterference("cpu", before, after)
	if assessment.Detected || assessment.Score != 0 {
		t.Fatalf("workload-generated PSI incorrectly triggered retry: %+v", assessment)
	}
	if !hasMeasurementKey(assessment.Measurements, "cpu_psi_some_percent_window") {
		t.Fatalf("workload PSI was not reported: %+v", assessment.Measurements)
	}
}

func TestFinalizeBenchmarkRetrySelectsValidityThenLowerInterferenceNeverHigherScore(t *testing.T) {
	first := benchmarkRetryResult("first", 100, model.StatusOK, model.NewEvidence(1, 1, "run"))
	secondFast := benchmarkRetryResult("second", 9999, model.StatusOK, model.NewEvidence(1, 1, "run"))
	selected := FinalizeBenchmarkRetry(first, model.Interference{Score: 2, Reasons: []string{"load"}}, secondFast, model.Interference{Score: 5, Reasons: []string{"steal"}})
	if selected.Retry == nil || selected.Retry.SelectedAttempt != 1 || selected.Measurements[0].Value != 100 {
		t.Fatalf("higher benchmark score overrode interference selection: %+v", selected)
	}

	failedSecond := benchmarkRetryResult("failed", 0, model.StatusError, model.NewEvidence(0, 1, "run"))
	selected = FinalizeBenchmarkRetry(first, model.Interference{Score: 5}, failedSecond, model.Interference{Score: 0})
	if selected.Retry.SelectedAttempt != 1 || selected.Status != model.StatusOK {
		t.Fatalf("invalid low-interference attempt was selected: %+v", selected)
	}

	secondClean := benchmarkRetryResult("second", 50, model.StatusOK, model.NewEvidence(1, 1, "run"))
	selected = FinalizeBenchmarkRetry(first, model.Interference{Score: 5}, secondClean, model.Interference{Score: 1})
	if selected.Retry.SelectedAttempt != 2 || selected.Measurements[0].Value != 50 {
		t.Fatalf("cleaner valid retry was not selected: %+v", selected)
	}
	if !strings.Contains(selected.Retry.SelectionRule, "不按性能高低") || len(selected.Retry.Attempts) != 2 {
		t.Fatalf("retry audit trail incomplete: %+v", selected.Retry)
	}
}

func TestFinalizeBenchmarkRetryKeepsFirstOnInterferenceTie(t *testing.T) {
	first := benchmarkRetryResult("first", 10, model.StatusOK, model.NewEvidence(1, 1, "run"))
	second := benchmarkRetryResult("second", 20, model.StatusOK, model.NewEvidence(1, 1, "run"))
	selected := FinalizeBenchmarkRetry(first, model.Interference{Score: 2}, second, model.Interference{Score: 2})
	if selected.Retry.SelectedAttempt != 1 || selected.Measurements[0].Value != 10 {
		t.Fatalf("tie did not preserve first attempt: %+v", selected)
	}
}

func TestAppendSystemResourceDiagnosticsExposesLimitsPSIAndOOM(t *testing.T) {
	result := model.NewResult("system", "System")
	snapshot := EnvironmentSnapshot{
		Limits: resourceLimits{
			CPU:    cpuAllowance{Visible: 8, Quota: 2.5, Threads: 3, Source: "cpu.max"},
			CPUSet: "0-2", CPUSetCount: 3, CPUSetSource: "cpuset.cpus.effective",
			MemoryLimit: 2 << 30, MemoryLimitVia: "memory.max", MemoryCurrent: 1 << 30, MemoryCurrentVia: "memory.current",
		},
		CPUStat: cgroupCPUStats{NrThrottled: 7, ThrottledUS: 500000, Present: true, Source: "cpu.stat"},
		Memory:  cgroupMemoryEvents{OOM: 2, OOMKill: 1, Present: true, Source: "memory.events"},
		PSI: map[string]psiResource{
			"cpu":    {Some: psiValues{Avg10: 1, Present: true}, Source: "cpu.pressure"},
			"memory": {Some: psiValues{Avg10: 2, Present: true}, Full: psiValues{Avg10: 0.5, Present: true}, Source: "memory.pressure"},
			"io":     {},
		},
	}
	AppendSystemResourceDiagnostics(&result, snapshot)
	for _, key := range []string{"cgroup_cpu_quota", "cgroup_cpuset", "cgroup_memory_limit", "cgroup_memory_current"} {
		if !hasFieldKey(result.Fields, key) {
			t.Errorf("system diagnostics missing field %q: %+v", key, result.Fields)
		}
	}
	for _, key := range []string{"cgroup_cpu_quota_cores", "cpu_psi_some_avg10", "cgroup_cpu_nr_throttled", "cgroup_oom_kill_events"} {
		if !hasMeasurementKey(result.Measurements, key) {
			t.Errorf("system diagnostics missing measurement %q: %+v", key, result.Measurements)
		}
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "cgroup 与 PSI 压力诊断" || len(result.Tables[0].Rows) != 3 {
		t.Fatalf("system pressure table = %+v", result.Tables)
	}
}

func benchmarkRetryResult(id string, value float64, status model.Status, evidence *model.Evidence) model.Result {
	return model.Result{
		ID: id, Title: id, Status: status, StartedAt: time.Unix(100, 0), DurationMS: 100,
		Evidence: evidence,
		Measurements: []model.Measurement{{
			Key: "rate", Value: value, Display: model.FormatRate(value, "ops/s"), Method: "benchmark-v1", HigherIsBetter: model.BoolPtr(true),
		}},
		TextBlocks: []model.TextBlock{{Title: id + " raw", Content: id}},
	}
}

func hasMeasurementKey(measurements []model.Measurement, key string) bool {
	for _, measurement := range measurements {
		if measurement.Key == key {
			return true
		}
	}
	return false
}

func hasFieldKey(fields []model.Field, key string) bool {
	for _, field := range fields {
		if field.Key == key {
			return true
		}
	}
	return false
}
