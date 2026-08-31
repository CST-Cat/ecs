package probe

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
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
	for _, measurement := range assessment.Measurements {
		if !strings.HasPrefix(measurement.Label, "probe.pressure.metric.") {
			t.Fatalf("pressure measurement label is not a stable key: %+v", measurement)
		}
		for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
			if !i18n.Has(language, measurement.Label) {
				t.Fatalf("pressure measurement label %q is missing %s translation", measurement.Label, language)
			}
		}
	}
	for _, marker := range []string{"steal", "throttle", "PSI", "OOM"} {
		found := false
		for _, reason := range assessment.Reasons {
			if !strings.HasPrefix(reason.Key, "probe.pressure.reason.") {
				t.Fatalf("pressure reason is not a stable Message key: %+v", reason)
			}
			for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
				if !i18n.Has(language, reason.Key) {
					t.Fatalf("pressure reason %q is missing %s translation", reason.Key, language)
				}
			}
			if strings.Contains(strings.ToLower(reason.Key), strings.ToLower(marker)) {
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
	appendSystemResourceFields(&result.Fields, snapshot.Limits)
	result.Measurements = append(result.Measurements, systemResourceMeasurements(snapshot)...)
	result.Tables = append(result.Tables, systemPressureTable(snapshot))
	fieldValues := make(map[string]string, len(result.Fields))
	for _, field := range result.Fields {
		fieldValues[field.Key] = field.Value.Text()
	}
	if fieldValues["cgroup_memory_swap_limit_bytes"] != "unlimited" || len(result.Tables) != 1 || result.Tables[0].Key != "system.pressure.cgroup" {
		t.Fatalf("system diagnostics = fields:%v tables:%v", result.Fields, result.Tables)
	}
	assessment := model.Interference{Detected: true, Score: 2, Reasons: []model.Message{model.NewMessage("probe.pressure.reason.cpu_steal_high", "fixture")}, Measurements: []model.Measurement{{Key: "pressure", Label: "probe.pressure.metric.cpu_steal_percent_window", Display: model.RawValue("2"), Method: "fixture"}}}
	fieldCount, tableCount := len(result.Fields), len(result.Tables)
	AppendInterferenceDiagnostics(&result, assessment)
	if result.Status != model.StatusWarning || result.Interference == nil || result.Interference.Reasons[0].Key != assessment.Reasons[0].Key || len(result.Notes) != 0 || len(result.Fields) != fieldCount || len(result.Tables) != tableCount {
		t.Fatalf("interference diagnostics = %+v", result)
	}
	assessment.Reasons[0].Args[0] = "changed"
	assessment.Measurements[0].Display = model.RawValue("changed")
	if result.Interference.Reasons[0].Args[0] == "changed" || result.Interference.Measurements[0].Display.Text() == "changed" || result.Measurements[len(result.Measurements)-1].Display.Text() == "changed" {
		t.Fatal("interference diagnostics shared input slices")
	}
	cleanResult := model.NewResult("system", "system")
	AppendInterferenceDiagnostics(&cleanResult, model.Interference{Measurements: []model.Measurement{{Key: "pressure", Label: "probe.pressure.metric.cpu_steal_percent_window", Display: model.RawValue("0"), Method: "fixture"}}})
	if cleanResult.Status != model.StatusOK || cleanResult.Interference == nil || len(cleanResult.Tables) != 0 || len(cleanResult.Notes) != 0 || len(cleanResult.Fields) != 0 {
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
			firstInterference := model.Interference{Score: test.firstScore, Reasons: []model.Message{model.NewMessage("probe.pressure.reason.pretest_cpu_psi_high", "first")}, Measurements: []model.Measurement{{Key: "first-pressure", Display: model.RawValue("first")}}}
			secondInterference := model.Interference{Score: test.secondScore, Reasons: []model.Message{model.NewMessage("probe.pressure.reason.pretest_cpu_psi_high", "second")}, Measurements: []model.Measurement{{Key: "second-pressure", Display: model.RawValue("second")}}}
			selected := FinalizeBenchmarkRetry(first, firstInterference, second, secondInterference)
			if selected.Retry == nil || selected.Retry.SelectedAttempt != test.want || len(selected.Retry.Attempts) != 2 || selected.Status != model.StatusOK || selected.Evidence == nil || selected.Evidence.Valid != 1 || len(selected.TextBlocks) != 2 {
				t.Fatalf("retry selection = %+v", selected.Retry)
			}
			if selected.Measurements[0].Value != test.wantValue || selected.Retry.SelectionRule.Key != "probe.retry.selection_rule.interference_score" || selected.Retry.TriggerReasons[0].Key != "probe.pressure.reason.pretest_cpu_psi_high" || len(selected.Tables) != 0 || len(selected.Fields) != 0 || len(selected.Notes) != 0 {
				t.Fatalf("retry selected result = %+v", selected)
			}
			firstInterference.Reasons[0].Args[0] = "changed"
			firstInterference.Measurements[0].Display = model.RawValue("changed")
			if selected.Retry.TriggerReasons[0].Args[0] == "changed" || selected.Retry.Attempts[0].Interference.Reasons[0].Args[0] == "changed" || selected.Retry.Attempts[0].Interference.Measurements[0].Display.Text() == "changed" {
				t.Fatal("retry facts shared input slices")
			}
			for _, block := range selected.TextBlocks {
				wantAttempt := 1
				if block.Title == "second" {
					wantAttempt = 2
				}
				if block.Attempt != wantAttempt || strings.Contains(block.Title, "自动复测") {
					t.Fatalf("retry text block attempt = %+v", selected.TextBlocks)
				}
			}
		})
	}

	first := benchmarkRetryResult("first", 100)
	second := benchmarkRetryResult("second", 50)
	start := time.Unix(100, 0)
	retryBefore := EnvironmentSnapshot{
		CapturedAt: start,
		PSI:        map[string]psiResource{"cpu": {Some: psiValues{Avg10: 25, TotalUS: 100, Present: true}}},
	}
	retryAfter := retryBefore
	retryAfter.CapturedAt = start.Add(time.Second)
	retryAfter.PSI = map[string]psiResource{"cpu": {Some: psiValues{Avg10: 1, TotalUS: 2_000_000, Present: true}}}
	cleanRetryBefore := EnvironmentSnapshot{
		CapturedAt: start,
		PSI:        map[string]psiResource{"cpu": {Some: psiValues{Avg10: 1, TotalUS: 10, Present: true}}},
	}
	cleanRetryAfter := cleanRetryBefore
	cleanRetryAfter.CapturedAt = start.Add(time.Second)
	cleanRetryAfter.PSI = map[string]psiResource{"cpu": {Some: psiValues{Avg10: 1, TotalUS: 20, Present: true}}}
	firstAssessment := AssessBenchmarkInterference("memory", retryBefore, retryAfter)
	secondAssessment := AssessBenchmarkInterference("memory", cleanRetryBefore, cleanRetryAfter)
	selected := FinalizeBenchmarkRetry(first, firstAssessment, second, secondAssessment)
	if selected.Retry == nil || selected.Retry.SelectedAttempt != 2 {
		t.Fatalf("actual retry selection = %+v", selected.Retry)
	}
	AppendInterferenceDiagnostics(&selected, secondAssessment)
	canonical, err := json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	canonicalText := string(canonical)
	for _, prose := range []string{"测试窗口资源干扰", "检测到测试干扰", "自动复测", "自动复测判定", "采用规则"} {
		if strings.Contains(canonicalText, prose) {
			t.Fatalf("canonical retry JSON contains presentation prose %q: %s", prose, canonicalText)
		}
	}
	if !strings.Contains(canonicalText, `"attempt":1`) || !strings.Contains(canonicalText, `"attempt":2`) || !strings.Contains(canonicalText, `"interference"`) || !strings.Contains(canonicalText, `"selection_rule":{"key":"probe.retry.selection_rule.interference_score"}`) {
		t.Fatalf("canonical retry JSON lost structured facts: %s", canonicalText)
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

func TestBenchmarkRetryNormalizesEveryEvidenceLayer(t *testing.T) {
	first := benchmarkRetryResult("first", 100)
	first.Evidence = &model.Evidence{Valid: 4, Expected: 2, Unit: "run"}
	second := benchmarkRetryResult("second", 50)
	second.Evidence = &model.Evidence{Valid: -1, Expected: 0, Unit: "run"}

	selected := FinalizeBenchmarkRetry(first, model.Interference{}, second, model.Interference{})
	if selected.Evidence == nil || selected.Evidence.Valid != 2 || selected.Evidence.Expected != 2 {
		t.Fatalf("selected evidence = %+v", selected.Evidence)
	}
	if selected.Retry == nil || len(selected.Retry.Attempts) != 2 {
		t.Fatalf("retry evidence = %+v", selected.Retry)
	}
	want := [][2]int{{2, 2}, {0, 0}}
	for index, attempt := range selected.Retry.Attempts {
		if attempt.Evidence == nil || attempt.Evidence.Valid != want[index][0] || attempt.Evidence.Expected != want[index][1] {
			t.Fatalf("attempt %d evidence = %+v", index+1, attempt.Evidence)
		}
	}
}
