package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
)

func currentCompareFixturePath(t *testing.T, name string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed while locating compare fixture")
	}
	path := filepath.Join(filepath.Dir(source), "..", "report", "testdata", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("compare fixture %q is unavailable: %v", name, err)
	}
	return path
}

func TestCompareCommandBuildsCurrentFixtureMetricAndRelativeResult(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	reference := currentCompareFixturePath(t, "compare_reference.json")
	candidate := currentCompareFixturePath(t, "compare_candidate.json")
	output := t.TempDir()
	status, stdout, stderr := invokeAppMain(
		"--lang", "en", "compare", reference, candidate, "--format", "json", "--output", output,
		"--name", "fixture-comparison", "--reference", "1", "--no-color",
	)
	if status != 0 || stdout == "" || stderr != "" {
		t.Fatalf("current fixture comparison status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	content, err := os.ReadFile(filepath.Join(output, "fixture-comparison.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data comparison.Report
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatalf("comparison JSON is not readable: %v", err)
	}
	if data.SchemaVersion != comparison.SchemaVersion || data.Reference != 0 || len(data.Inputs) != 2 || data.Inputs[0].ReportID != "compare-reference" || data.Inputs[1].ReportID != "compare-candidate" {
		t.Fatalf("comparison metadata/order = schema=%q reference=%d inputs=%+v", data.SchemaVersion, data.Reference, data.Inputs)
	}
	wantSummary := comparison.Summary{
		Comparability: comparison.Comparable, Reports: 2, Modules: 1, ComparableMetrics: 1,
		Improved: 1, ObservedChanges: 2, StatusChanges: 1, EvidenceChanges: 1,
	}
	if !reflect.DeepEqual(data.Summary, wantSummary) {
		t.Fatalf("comparison summary = %+v, want %+v", data.Summary, wantSummary)
	}
	wantNotices := []comparison.Notice{
		{Key: "compare.notice.toolMixed", Args: []string{"fixture-1, fixture-2"}},
		{Key: "compare.notice.scope"},
		{Key: "compare.notice.relative"},
		{Key: "compare.notice.observation"},
	}
	if !reflect.DeepEqual(data.Notices, wantNotices) {
		t.Fatalf("comparison notices = %#v, want %#v", data.Notices, wantNotices)
	}
	if len(data.Modules) != 1 || data.Modules[0].Comparability != comparison.Comparable || len(data.Modules[0].MetricIssues) != 0 || len(data.Modules[0].Metrics) != 1 {
		t.Fatalf("comparison module/metric issues = %+v", data.Modules)
	}
	metric := data.Modules[0].Metrics[0]
	if metric.Key != "sysbench_cpu_single_events_s" || metric.Unit != "events/s" || metric.Method != "sysbench-cpu-prime20000-v1" || !metric.HigherIsBetter || len(metric.Values) != 2 || metric.Values[0].Value != 100 || metric.Values[1].Value != 125 || metric.Values[1].Outcome != comparison.OutcomeImproved || metric.Values[1].PerformanceChangePercent == nil || *metric.Values[1].PerformanceChangePercent != 25 {
		t.Fatalf("comparison metric = %+v", metric)
	}
}
