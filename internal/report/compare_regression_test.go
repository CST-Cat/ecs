package report

import (
	"embed"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// Keep comparison inputs as real current-schema artifacts. Loading the files
// through LoadJSON is part of this regression: a model assembled in memory
// would not exercise the Value/Table persistence contract at the compare
// boundary.
var (
	//go:embed testdata/compare_reference.json testdata/compare_candidate.json
	compareRegressionFixtures embed.FS
)

func loadCompareRegressionFixture(t *testing.T, name string) model.Report {
	t.Helper()
	content, err := compareRegressionFixtures.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read comparison fixture %q: %v", name, err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write comparison fixture %q: %v", name, err)
	}
	data, err := LoadJSON(path)
	if err != nil {
		t.Fatalf("LoadJSON comparison fixture %q: %v", name, err)
	}
	return data
}

func TestCurrentCompareFixturesBuildMetricsChangesAndRelativeResults(t *testing.T) {
	reference := loadCompareRegressionFixture(t, "compare_reference.json")
	candidate := loadCompareRegressionFixture(t, "compare_candidate.json")
	reports := []model.Report{reference, candidate}

	if reference.SchemaVersion != "ecs.report/v1" || candidate.SchemaVersion != "ecs.report/v1" {
		t.Fatalf("fixture schemas = %q and %q", reference.SchemaVersion, candidate.SchemaVersion)
	}
	if key, ok := reference.Results[0].Fields[0].Value.Key(); !ok || key != "probe.cpu.validity.valid" {
		t.Fatalf("reference field Value = %#v, want stable key", reference.Results[0].Fields[0].Value)
	}
	if raw, ok := reference.Results[0].Measurements[0].Display.Raw(); !ok || raw != "100.0 events/s" {
		t.Fatalf("reference measurement display = %#v, want raw fixture value", reference.Results[0].Measurements[0].Display)
	}
	if key, ok := candidate.Results[0].Tables[0].Rows[0][1].Key(); !ok || key != "probe.cpu.validity.partial" {
		t.Fatalf("candidate table Value = %#v, want stable key", candidate.Results[0].Tables[0].Rows[0][1])
	}

	data, err := comparison.Build(reports, comparison.Options{
		Labels:    []string{"reference", "candidate"},
		Reference: 0,
		Tool:      model.ToolInfo{Name: "ecs", Version: "compare-regression"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if data.SchemaVersion != comparison.SchemaVersion || data.Reference != 0 {
		t.Fatalf("comparison metadata = schema %q reference %d", data.SchemaVersion, data.Reference)
	}
	if got := []string{data.Inputs[0].Label, data.Inputs[1].Label}; !reflect.DeepEqual(got, []string{"reference", "candidate"}) {
		t.Fatalf("comparison input order = %v", got)
	}
	if data.Inputs[0].Index != 0 || data.Inputs[0].ReportID != "compare-reference" || data.Inputs[0].SchemaVersion != "ecs.report/v1" || data.Inputs[0].ToolVersion != "fixture-1" || data.Inputs[1].Index != 1 || data.Inputs[1].ReportID != "compare-candidate" || data.Inputs[1].SchemaVersion != "ecs.report/v1" || data.Inputs[1].ToolVersion != "fixture-2" {
		t.Fatalf("comparison inputs = %+v", data.Inputs)
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

	if len(data.Modules) != 1 {
		t.Fatalf("comparison modules = %+v", data.Modules)
	}
	module := data.Modules[0]
	if module.ID != "cpu" || module.Title != "module.cpu.title" || module.Comparability != comparison.Comparable {
		t.Fatalf("comparison module = %+v", module)
	}
	wantStatuses := []comparison.StatusValue{
		{Report: 0, Available: true, Status: model.StatusOK},
		{Report: 1, Available: true, Status: model.StatusWarning},
	}
	if !reflect.DeepEqual(module.Statuses, wantStatuses) {
		t.Fatalf("module statuses = %+v, want %+v", module.Statuses, wantStatuses)
	}
	wantEvidence := []comparison.EvidenceValue{
		{Report: 0, Available: true, Valid: 2, Expected: 2, Unit: "run", Ratio: 1},
		{Report: 1, Available: true, Valid: 1, Expected: 2, Unit: "run", Ratio: 0.5},
	}
	if !reflect.DeepEqual(module.Evidence, wantEvidence) {
		t.Fatalf("module evidence = %+v, want %+v", module.Evidence, wantEvidence)
	}

	if len(module.Metrics) != 1 {
		t.Fatalf("module metrics = %+v", module.Metrics)
	}
	metric := module.Metrics[0]
	wantParameters := map[string]string{
		"scope_revision":      "1",
		"configured_duration": "5s",
		"tool_version":        "sysbench-fixture",
		"tool_sha256":         "fixture-sha256",
		"threads":             "1 / 2",
		"duration":            "5s",
		"prime":               "20000",
	}
	if metric.Key != "sysbench_cpu_single_events_s" || metric.Label != "probe.cpu.metric.single_events_s" || metric.Unit != "events/s" || metric.Method != "sysbench-cpu-prime20000-v1" || !metric.HigherIsBetter || !reflect.DeepEqual(metric.Parameters, wantParameters) {
		t.Fatalf("comparison metric metadata = %+v", metric)
	}
	if len(metric.Values) != 2 {
		t.Fatalf("comparison metric values = %+v", metric.Values)
	}
	referenceValue, candidateValue := metric.Values[0], metric.Values[1]
	if referenceValue.Report != 0 || !referenceValue.Available || referenceValue.Value != 100 || referenceValue.Display != "100.0 events/s" || referenceValue.Outcome != comparison.OutcomeUnchanged || referenceValue.Rank != 2 || referenceValue.Best || !referenceValue.Worst || math.Abs(referenceValue.QualityRatio-0.15) > 1e-9 || referenceValue.PerformanceChangePercent == nil || math.Abs(*referenceValue.PerformanceChangePercent) > 1e-9 {
		t.Fatalf("reference metric value = %+v", referenceValue)
	}
	if candidateValue.Report != 1 || !candidateValue.Available || candidateValue.Value != 125 || candidateValue.Display != "125.0 events/s" || candidateValue.Outcome != comparison.OutcomeImproved || candidateValue.Rank != 1 || !candidateValue.Best || candidateValue.Worst || math.Abs(candidateValue.QualityRatio-1) > 1e-9 || candidateValue.PerformanceChangePercent == nil || math.Abs(*candidateValue.PerformanceChangePercent-25) > 1e-9 {
		t.Fatalf("candidate metric value = %+v", candidateValue)
	}

	if len(module.Changes) != 2 {
		t.Fatalf("observed changes = %+v", module.Changes)
	}
	fieldChange := module.Changes[0]
	if fieldChange.Key != "field:result_validity" || fieldChange.Label != "probe.cpu.field.result_validity" || fieldChange.Source != "field" || len(fieldChange.Values) != 2 || fieldChange.Values[0].Report != 0 || !fieldChange.Values[0].Available || fieldChange.Values[0].Value != "probe.cpu.validity.valid" || fieldChange.Values[1].Report != 1 || !fieldChange.Values[1].Available || fieldChange.Values[1].Value != "probe.cpu.validity.partial" {
		t.Fatalf("field change = %+v", fieldChange)
	}
	tableChange := module.Changes[1]
	if tableChange.Key != "table:cpu.execution\x1frun_id\x1estate:run-1:state" || tableChange.Label != "probe.cpu.table.execution · run-1 · probe.cpu.column.state" || tableChange.Source != "table" || len(tableChange.Values) != 2 || tableChange.Values[0].Value != "probe.cpu.validity.valid" || tableChange.Values[1].Value != "probe.cpu.validity.partial" {
		t.Fatalf("table change = %+v", tableChange)
	}

	alternate, err := comparison.Build(reports, comparison.Options{Labels: []string{"reference", "candidate"}, Reference: 1})
	if err != nil {
		t.Fatal(err)
	}
	if alternate.Reference != 1 {
		t.Fatalf("alternate reference = %d, want 1", alternate.Reference)
	}
	alternateMetric := alternate.Modules[0].Metrics[0]
	if alternateMetric.Values[0].Outcome != comparison.OutcomeRegressed || alternateMetric.Values[0].PerformanceChangePercent == nil || math.Abs(*alternateMetric.Values[0].PerformanceChangePercent+20) > 1e-9 || alternateMetric.Values[1].Outcome != comparison.OutcomeUnchanged {
		t.Fatalf("alternate reference metric values = %+v", alternateMetric.Values)
	}
}

func TestCurrentCompareFixturesPreserveMultiReportRanksAndStatistics(t *testing.T) {
	reference := loadCompareRegressionFixture(t, "compare_reference.json")
	candidate := loadCompareRegressionFixture(t, "compare_candidate.json")
	third := loadCompareRegressionFixture(t, "compare_reference.json")
	third.Run.ID = "compare-third"
	third.Results[0].Measurements[0].Value = 110
	third.Results[0].Measurements[0].Display = model.RawValue("110.0 events/s")

	data, err := comparison.Build([]model.Report{reference, candidate, third}, comparison.Options{
		Labels: []string{"reference", "candidate", "third"}, Reference: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{data.Inputs[0].ReportID, data.Inputs[1].ReportID, data.Inputs[2].ReportID}; !reflect.DeepEqual(got, []string{"compare-reference", "compare-candidate", "compare-third"}) {
		t.Fatalf("multi-report input order = %v", got)
	}
	wantSummary := comparison.Summary{
		Comparability: comparison.Comparable, Reports: 3, Modules: 1, ComparableMetrics: 1,
		Improved: 2, ObservedChanges: 2, StatusChanges: 1, EvidenceChanges: 1,
	}
	if !reflect.DeepEqual(data.Summary, wantSummary) {
		t.Fatalf("multi-report summary = %+v, want %+v", data.Summary, wantSummary)
	}
	metric := data.Modules[0].Metrics[0]
	if len(metric.Values) != 3 {
		t.Fatalf("multi-report metric values = %+v", metric.Values)
	}
	if metric.Values[0].Rank != 3 || !metric.Values[0].Worst || metric.Values[0].Best || metric.Values[0].Outcome != comparison.OutcomeUnchanged || metric.Values[1].Rank != 1 || !metric.Values[1].Best || metric.Values[1].Worst || metric.Values[1].Outcome != comparison.OutcomeImproved || metric.Values[2].Rank != 2 || metric.Values[2].Best || metric.Values[2].Worst || metric.Values[2].Outcome != comparison.OutcomeImproved {
		t.Fatalf("multi-report rank/outcome values = %+v", metric.Values)
	}
	if metric.Values[1].PerformanceChangePercent == nil || math.Abs(*metric.Values[1].PerformanceChangePercent-25) > 1e-9 || metric.Values[2].PerformanceChangePercent == nil || math.Abs(*metric.Values[2].PerformanceChangePercent-10) > 1e-9 {
		t.Fatalf("multi-report relative values = %+v", metric.Values)
	}
}

func TestCompareDoesNotMigrateCurrentSchemaPositionalTables(t *testing.T) {
	reference := loadCompareRegressionFixture(t, "compare_reference.json")
	candidate := loadCompareRegressionFixture(t, "compare_candidate.json")
	reference.Results[0].Tables[0].Key = ""
	candidate.Results[0].Tables[0].Key = ""

	loaded := make([]model.Report, 0, 2)
	for index, data := range []model.Report{reference, candidate} {
		content, err := JSON(data)
		if err != nil {
			t.Fatalf("marshal positional fixture %d: %v", index, err)
		}
		path := filepath.Join(t.TempDir(), "positional-"+string(rune('1'+index))+".json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write positional fixture %d: %v", index, err)
		}
		decoded, err := LoadJSON(path)
		if err != nil {
			t.Fatalf("LoadJSON positional fixture %d: %v", index, err)
		}
		loaded = append(loaded, decoded)
	}
	data, err := comparison.Build(loaded, comparison.Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := data.Modules[0].Changes
	if len(changes) != 1 || changes[0].Key != "field:result_validity" || changes[0].Source != "field" {
		t.Fatalf("positional table was migrated into observed changes = %+v", changes)
	}
}

func TestCompareLoaderRejectsHistoricalAndLegacyReportInputs(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	fixture, err := compareRegressionFixtures.ReadFile("testdata/compare_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	historical := []byte(strings.Replace(string(fixture), `"ecs.report/v1"`, `"ecs.report/v2"`, 1))
	cases := []struct {
		name    string
		content []byte
		marker  string
	}{
		{name: "historical schema", content: historical, marker: `unsupported schema_version "ecs.report/v2"; this renderer supports "ecs.report/v1"`},
		{name: "legacy positional value", content: []byte(`{"schema_version":"ecs.report/v1","results":[{"id":"cpu","tables":[{"columns":[{"key":"id","label":"ID"}],"rows":[["legacy"]]}]}]}`), marker: "model value must be a tagged object"},
		{name: "malformed current row", content: []byte(`{"schema_version":"ecs.report/v1","results":[{"id":"cpu","tables":[{"key":"cpu.execution","columns":[{"key":"id","label":"ID"},{"key":"state","label":"State"}],"rows":[[{"raw":"row-1"}]]}]}]}`), marker: "table row 0 has 1 cells, want 2"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("LoadJSON error = %v, want semantic rejection %q", err, test.marker)
			}
		})
	}
}
