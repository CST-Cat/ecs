package compare

import (
	"math"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func comparisonTestReport(id string, value float64, method, profile, label string, higher bool) model.Report {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: id, Profile: "standard", StartedAt: started},
		Results: []model.Result{{
			ID: "cpu", Title: "CPU", Status: model.StatusOK,
			Methodology: model.Methodology{
				Kind: "standard-benchmark", Engine: "sysbench", Profile: profile,
				Parameters: map[string]string{"scope_revision": "1", "workload": profile},
			},
			Evidence: model.NewEvidence(1, 1, "run"),
			Measurements: []model.Measurement{{
				Key: "rate", Label: label, Value: value, Unit: "events/s", Display: model.FormatRate(value, "events/s"),
				Method: method, HigherIsBetter: model.BoolPtr(higher),
			}},
		}},
	}
}

func TestBuildRequiresTwoReports(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangZH)
	if _, err := Build([]model.Report{{}}, Options{}); err == nil || !strings.Contains(err.Error(), "至少需要两份") {
		t.Fatalf("Build one report error = %v", err)
	}
}

func TestBuildPairRanksAndHighlightsDirectionAwareValues(t *testing.T) {
	base := comparisonTestReport("base", 100, "m-v1", "same", "中文标签", true)
	candidate := comparisonTestReport("candidate", 125, "m-v1", "same", "English label", true)
	data, err := Build([]model.Report{base, candidate}, Options{Labels: []string{"old", "new"}})
	if err != nil {
		t.Fatal(err)
	}
	if data.Summary.Reports != 2 || data.Summary.ComparableMetrics != 1 || data.Summary.Improved != 1 {
		t.Fatalf("unexpected summary: %+v", data.Summary)
	}
	metric := data.Modules[0].Metrics[0]
	if metric.Label != "中文标签" {
		t.Fatalf("first report label should be display-only, got %q", metric.Label)
	}
	if !metric.Values[1].Best || metric.Values[1].Rank != 1 || metric.Values[1].Outcome != OutcomeImproved {
		t.Fatalf("candidate was not highlighted as best: %+v", metric.Values[1])
	}
	if metric.Values[0].Best || !metric.Values[0].Worst || metric.Values[0].Outcome != OutcomeUnchanged {
		t.Fatalf("reference decoration = %+v", metric.Values[0])
	}
	if metric.Values[1].PerformanceChangePercent == nil || !nearlyEqual(*metric.Values[1].PerformanceChangePercent, 25) {
		t.Fatalf("candidate change = %v", metric.Values[1].PerformanceChangePercent)
	}
}

func TestBuildLowerIsBetterNormalizesPositiveChangeAsImprovement(t *testing.T) {
	base := comparisonTestReport("base", 100, "latency-v1", "same", "P95", false)
	candidate := comparisonTestReport("candidate", 80, "latency-v1", "same", "P95", false)
	data, err := Build([]model.Report{base, candidate}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	value := data.Modules[0].Metrics[0].Values[1]
	if value.Outcome != OutcomeImproved || value.PerformanceChangePercent == nil || !nearlyEqual(*value.PerformanceChangePercent, 20) {
		t.Fatalf("lower-is-better change = %+v", value)
	}
}

func TestBuildManyReportsKeepsEveryInputAndRanksTies(t *testing.T) {
	values := []float64{10, 30, 20, 30, 5, 15, 25}
	reports := make([]model.Report, 0, len(values))
	for index, value := range values {
		reports = append(reports, comparisonTestReport(string(rune('a'+index)), value, "m-v1", "same", "rate", true))
	}
	data, err := Build(reports, Options{})
	if err != nil {
		t.Fatal(err)
	}
	metric := data.Modules[0].Metrics[0]
	if len(data.Inputs) != len(values) || len(metric.Values) != len(values) {
		t.Fatalf("many-report values were lost: inputs=%d values=%d", len(data.Inputs), len(metric.Values))
	}
	if !metric.Values[1].Best || !metric.Values[3].Best || metric.Values[1].Rank != 1 || metric.Values[3].Rank != 1 {
		t.Fatalf("tie for best not preserved: %+v %+v", metric.Values[1], metric.Values[3])
	}
	if metric.Values[4].Rank != 7 || !metric.Values[4].Worst {
		t.Fatalf("worst rank = %+v", metric.Values[4])
	}
}

func TestBuildRejectsMismatchedMethodsAndMachineParameters(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		profile string
	}{
		{name: "method", method: "m-v2", profile: "same"},
		{name: "parameters", method: "m-v1", profile: "different"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			base := comparisonTestReport("base", 10, "m-v1", "same", "rate", true)
			candidate := comparisonTestReport("candidate", 20, testCase.method, testCase.profile, "rate", true)
			data, err := Build([]model.Report{base, candidate}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			module := data.Modules[0]
			if len(module.Metrics) != 0 || len(module.MetricIssues) != 1 || module.MetricIssues[0].Reason != "method_or_parameters_mismatch" {
				t.Fatalf("mismatch was compared: %+v", module)
			}
		})
	}
}

func TestBuildIgnoresLocalizedMethodologyProseAndMapInsertionOrder(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "中文说明", "中文标签", true)
	second := comparisonTestReport("second", 20, "m-v1", "English prose", "English label", true)
	first.Results[0].Methodology.Engine = "中文引擎名"
	second.Results[0].Methodology.Engine = "English engine name"
	first.Results[0].Methodology.Parameters = map[string]string{"scope_revision": "1", "threads": "4", "duration": "15s"}
	second.Results[0].Methodology.Parameters = map[string]string{"duration": "15s", "threads": "4", "scope_revision": "1"}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Modules[0].Metrics) != 1 {
		t.Fatalf("localized prose or map order changed the machine signature: %+v", data.Modules[0])
	}
}

func TestBuildRejectsMissingOrInvalidMachineParameterScope(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 20, "m-v1", "same", "rate", true)
	first.Results[0].Methodology.Parameters = nil
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Modules[0].Metrics) != 0 || !hasIssue(data.Modules[0].MetricIssues, "missing_or_invalid_parameter_scope") {
		t.Fatalf("missing machine parameters were compared: %+v", data.Modules[0])
	}
}

func TestBuildReportsSplitComparableMethodGroups(t *testing.T) {
	reports := []model.Report{
		comparisonTestReport("a", 10, "a", "same", "rate", true),
		comparisonTestReport("b", 11, "a", "same", "rate", true),
		comparisonTestReport("c", 20, "b", "same", "rate", true),
		comparisonTestReport("d", 21, "b", "same", "rate", true),
	}
	data, err := Build(reports, Options{})
	if err != nil {
		t.Fatal(err)
	}
	module := data.Modules[0]
	if len(module.Metrics) != 2 || len(module.MetricIssues) != 1 || module.MetricIssues[0].Reason != "some_reports_use_different_method_or_parameters" {
		t.Fatalf("split methods should stay separate and visible: %+v", module)
	}
}

func TestBuildMarksOneOffMetricInsteadOfSilentlyDroppingIt(t *testing.T) {
	base := comparisonTestReport("base", 10, "m-v1", "same", "rate", true)
	candidate := comparisonTestReport("candidate", 20, "m-v1", "same", "rate", true)
	candidate.Results[0].Measurements = nil
	data, err := Build([]model.Report{base, candidate}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issues := data.Modules[0].MetricIssues
	if len(issues) != 1 || issues[0].Reason != "no_matching_metric" || len(issues[0].Reports) != 1 || issues[0].Reports[0] != 0 {
		t.Fatalf("one-off metric issue = %+v", issues)
	}
}

func TestBuildUsesSelectedReference(t *testing.T) {
	first := comparisonTestReport("first", 120, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 100, "m-v1", "same", "rate", true)
	data, err := Build([]model.Report{first, second}, Options{Reference: 1})
	if err != nil {
		t.Fatal(err)
	}
	values := data.Modules[0].Metrics[0].Values
	if values[0].Outcome != OutcomeImproved || values[1].Outcome != OutcomeUnchanged {
		t.Fatalf("reference outcomes = %+v", values)
	}
}

func TestBuildFlattensOnlyChangedSafeTableCells(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 11, "m-v1", "same", "rate", true)
	first.Results[0].Fields = []model.Field{{Key: "provider", Label: "Provider", Value: "A"}}
	second.Results[0].Fields = []model.Field{{Key: "provider", Label: "Provider", Value: "B"}}
	first.Results[0].Tables = []model.Table{{Title: "Route", Columns: []string{"Target", "ASN"}, Rows: [][]string{{"example", "AS1"}}}}
	second.Results[0].Tables = []model.Table{{Title: "Route", Columns: []string{"Target", "ASN"}, Rows: [][]string{{"example", "AS2"}}}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := data.Modules[0].Changes
	if len(changes) != 2 {
		t.Fatalf("changed field/table cells = %+v", changes)
	}
	if changes[0].Source != "field" || changes[1].Source != "table" {
		t.Fatalf("change sources = %+v", changes)
	}
}

func TestBuildRejectsMissingKeysNonFiniteValuesAndDuplicateModules(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
		second := comparisonTestReport("second", 20, "m-v1", "same", "rate", true)
		first.Results[0].Measurements[0].Key = ""
		second.Results[0].Measurements[0].Key = ""
		data, err := Build([]model.Report{first, second}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(data.Modules[0].Metrics) != 0 || !hasIssue(data.Modules[0].MetricIssues, "missing_metric_key") {
			t.Fatalf("missing keys were compared: %+v", data.Modules[0])
		}
	})

	t.Run("non finite", func(t *testing.T) {
		first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
		second := comparisonTestReport("second", math.Inf(1), "m-v1", "same", "rate", true)
		data, err := Build([]model.Report{first, second}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(data.Modules[0].Metrics) != 0 || !hasIssue(data.Modules[0].MetricIssues, "non_finite_value") {
			t.Fatalf("non-finite value was compared: %+v", data.Modules[0])
		}
	})

	t.Run("duplicate module", func(t *testing.T) {
		first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
		second := comparisonTestReport("second", 20, "m-v1", "same", "rate", true)
		second.Results = append(second.Results, second.Results[0])
		data, err := Build([]model.Report{first, second}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !hasIssue(data.Modules[0].MetricIssues, "duplicate_module_id") || len(data.Modules[0].MissingReports) != 1 {
			t.Fatalf("duplicate module was not quarantined: %+v", data.Modules[0])
		}
	})
}

func TestBuildCountsStatusChangesAndMakesAllLabelsUnique(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 20, "m-v1", "same", "rate", true)
	third := comparisonTestReport("third", 30, "m-v1", "same", "rate", true)
	second.Results[0].Status = model.StatusWarning
	data, err := Build([]model.Report{first, second, third}, Options{Labels: []string{"node", "node #2", "node"}})
	if err != nil {
		t.Fatal(err)
	}
	if data.Summary.StatusChanges != 1 {
		t.Fatalf("status changes = %d, want 1", data.Summary.StatusChanges)
	}
	want := []string{"node", "node #2", "node #3"}
	for index, input := range data.Inputs {
		if input.Label != want[index] {
			t.Errorf("input %d label = %q, want %q", index, input.Label, want[index])
		}
	}
}

func hasIssue(issues []MetricIssue, reason string) bool {
	for _, issue := range issues {
		if issue.Reason == reason {
			return true
		}
	}
	return false
}
