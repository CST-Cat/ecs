package probe

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestPressureParsersAndAssessment(t *testing.T) {
	psi := parsePSI("some avg10=1.50 avg60=2.00 avg300=3.00 total=100\nfull avg10=0.10 avg60=0.20 avg300=0.30 total=20\n")
	if !psi.Some.Present || psi.Some.TotalUS != 100 || !psi.Full.Present || psi.Full.Avg10 != 0.1 {
		t.Fatalf("PSI parse = %+v", psi)
	}
	if counters := parseKeyValueCounters("usage_usec 10\nbad nope\nnr_periods 2\n"); counters["usage_usec"] != 10 || counters["nr_periods"] != 2 || len(counters) != 2 {
		t.Fatalf("counter parse = %v", counters)
	}
	if cpuSetCount("0-2,4,invalid,7-6") != 4 {
		t.Fatalf("cpuset count = %d", cpuSetCount("0-2,4,invalid,7-6"))
	}
	if delta, ok := counterDelta(2, 5); !ok || delta != 3 {
		t.Fatalf("counter delta = %v/%v", delta, ok)
	}
	if _, ok := counterDelta(5, 2); ok {
		t.Fatal("counter/pressure invalid boundary accepted")
	}
	if _, ok := pressurePercent(psiValues{}, psi.Some, time.Second); ok {
		t.Fatal("counter/pressure invalid boundary accepted")
	}
	if value, ok := pressurePercent(psi.Some, psiValues{Present: true, TotalUS: 2_000_000}, time.Second); !ok || value != 100 {
		t.Fatalf("pressure clamp = %v/%v", value, ok)
	}

	start := time.Unix(100, 0)
	before := EnvironmentSnapshot{
		CapturedAt: start, Load1: 5, LoadKnown: true,
		CPUTimes: cpuTimeSample{Total: 1000, Steal: 10}, CPUTracked: true,
		CPUStat: cgroupCPUStats{NrThrottled: 1, ThrottledUS: 10, Source: "cpu.stat", Present: true},
		Memory:  cgroupMemoryEvents{OOM: 0, OOMKill: 0, Source: "memory.events", Present: true},
		PSI: map[string]psiResource{
			"cpu":    {Some: psiValues{Avg10: 25, TotalUS: 100, Present: true}},
			"memory": {Some: psiValues{Avg10: 3, TotalUS: 100, Present: true}},
			"io":     {Some: psiValues{Avg10: 6, TotalUS: 100, Present: true}},
		},
		Limits: resourceLimits{CPU: cpuAllowance{Threads: 2}},
	}
	after := before
	after.CapturedAt = start.Add(time.Second)
	after.CPUTimes = cpuTimeSample{Total: 1100, Steal: 70}
	after.CPUStat = cgroupCPUStats{NrThrottled: 3, ThrottledUS: 2_000_010, Source: "cpu.stat", Present: true}
	after.Memory = cgroupMemoryEvents{OOM: 1, OOMKill: 1, Source: "memory.events", Present: true}
	after.PSI = make(map[string]psiResource, len(before.PSI))
	for resource, value := range before.PSI {
		value.Some.TotalUS = 2_000_000
		after.PSI[resource] = value
	}
	assessment := AssessBenchmarkInterference("memory", before, after)
	if !assessment.Detected || assessment.Score == 0 || len(assessment.Measurements) < 4 {
		t.Fatalf("memory interference = %+v", assessment)
	}
	for _, marker := range []string{"steal", "throttle", "PSI", "OOM"} {
		found := false
		for _, reason := range assessment.Reasons {
			if strings.Contains(strings.ToLower(reason), strings.ToLower(marker)) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("interference reason %q missing: %v", marker, assessment.Reasons)
		}
	}
	diskAssessment := AssessBenchmarkInterference("disk", before, after)
	if len(diskAssessment.Reasons) <= len(assessment.Reasons) {
		t.Fatalf("disk PSI sensitivity was not added: memory=%v disk=%v", assessment.Reasons, diskAssessment.Reasons)
	}
	cleanBefore := EnvironmentSnapshot{CapturedAt: start, PSI: map[string]psiResource{"cpu": {Some: psiValues{TotalUS: 10, Present: true}}}}
	cleanAfter := EnvironmentSnapshot{CapturedAt: start.Add(time.Second), PSI: map[string]psiResource{"cpu": {Some: psiValues{TotalUS: 20, Present: true}}}}
	clean := AssessBenchmarkInterference("cpu", cleanBefore, cleanAfter)
	if clean.Detected || clean.Score != 0 {
		t.Fatalf("clean interference = %+v", clean)
	}
}

func TestPressureDiagnosticsAndRetry(t *testing.T) {
	snapshot := EnvironmentSnapshot{
		Limits: resourceLimits{
			CPU: cpuAllowance{Threads: 2, Quota: 1.5, Source: "fixture"}, CPUSet: "0-1", CPUSetCount: 2,
			MemoryLimit: 1024, MemoryLimitVia: "memory.max", MemoryCurrent: 512, MemoryCurrentVia: "memory.current", MemorySwapMax: true, MemorySwapVia: "memory.swap.max",
		},
		PSI:     map[string]psiResource{"cpu": {Some: psiValues{Avg10: 1, Present: true}}, "memory": {}},
		CPUStat: cgroupCPUStats{NrThrottled: 2, ThrottledUS: 1_000_000, Present: true},
		Memory:  cgroupMemoryEvents{High: 1, Max: 2, OOM: 0, OOMKill: 0, Present: true},
	}
	result := model.NewResult("system", "system")
	AppendSystemResourceDiagnostics(&result, snapshot)
	if len(result.Fields) < 5 || result.Fields[4].Key != "cgroup_memory_swap_limit" || !strings.Contains(result.Fields[4].Value, "unlimited") || len(result.Tables) != 1 || result.Tables[0].Key != "system.pressure.cgroup" {
		t.Fatalf("system diagnostics = fields:%v tables:%v", result.Fields, result.Tables)
	}
	assessment := model.Interference{Detected: true, Score: 2, Reasons: []string{"fixture pressure"}, Measurements: []model.Measurement{{Key: "pressure", Label: "pressure", Display: "2", Method: "fixture"}}}
	AppendInterferenceDiagnostics(&result, assessment)
	if result.Status != model.StatusWarning || len(result.Notes) == 0 || len(result.Tables) != 2 {
		t.Fatalf("interference diagnostics = %+v", result)
	}
	cleanResult := model.NewResult("system", "system")
	AppendInterferenceDiagnostics(&cleanResult, model.Interference{Measurements: []model.Measurement{{Key: "pressure", Label: "pressure", Display: "0", Method: "fixture"}}})
	if cleanResult.Status != model.StatusOK || len(cleanResult.Tables) != 1 || len(cleanResult.Notes) != 0 {
		t.Fatalf("clean interference diagnostics = %+v", cleanResult)
	}

	for _, test := range []struct {
		name        string
		firstScore  int
		secondScore int
		want        int
		wantValue   float64
		firstOK     bool
	}{
		{name: "same score keeps first", firstScore: 1, secondScore: 1, want: 1, wantValue: 100, firstOK: true},
		{name: "second cleaner", firstScore: 3, secondScore: 1, want: 2, wantValue: 50, firstOK: true},
		{name: "second usable", firstScore: 1, secondScore: 3, want: 2, wantValue: 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := benchmarkRetryResult("first", 100)
			second := benchmarkRetryResult("second", 50)
			if !test.firstOK {
				first.Status = model.StatusError
				first.Evidence = model.NewEvidence(0, 1, "run")
			}
			selected := FinalizeBenchmarkRetry(first, model.Interference{Score: test.firstScore}, second, model.Interference{Score: test.secondScore})
			if selected.Retry == nil || selected.Retry.SelectedAttempt != test.want || len(selected.Retry.Attempts) != 2 || selected.Status != model.StatusOK || selected.Evidence == nil || selected.Evidence.Valid != 1 || len(selected.TextBlocks) != 2 {
				t.Fatalf("retry selection = %+v", selected.Retry)
			}
			if selected.Measurements[0].Value != test.wantValue || len(selected.Tables) == 0 || selected.Tables[len(selected.Tables)-1].RowIdentity != "attempt" || len(selected.Tables[len(selected.Tables)-1].ColumnKeys) != len(selected.Tables[len(selected.Tables)-1].Columns) {
				t.Fatalf("retry selected result = %+v", selected)
			}
			if !strings.Contains(selected.TextBlocks[1].Title, "自动复测第") {
				t.Fatalf("retry preserved text block = %+v", selected.TextBlocks)
			}
		})
	}
}

func benchmarkRetryResult(id string, value float64) model.Result {
	return model.Result{
		ID: id, Title: id, Status: model.StatusOK,
		Evidence:     model.NewEvidence(1, 1, "run"),
		Measurements: []model.Measurement{{Key: "rate", Value: value}},
		TextBlocks:   []model.TextBlock{{Title: id, Content: id}},
	}
}
