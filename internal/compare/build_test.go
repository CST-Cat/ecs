package compare

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

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
	_, err := Build([]model.Report{{}}, Options{})
	var validation *ValidationError
	if err == nil || !errors.As(err, &validation) || validation.Key != "compare.help.inputs" {
		t.Fatalf("Build one report error = %v", err)
	}
}

func TestParseNoticeRoundTripsArgumentsWithoutAmbiguity(t *testing.T) {
	want := []string{"ecs.report/v2", "value::with separator", "", "终端\x1b[31m"}
	encoded := canonicalNotice("compare.notice.schemaMixed", want...)
	key, got, ok := ParseNotice(encoded)
	if !ok || key != "compare.notice.schemaMixed" || !reflect.DeepEqual(got, want) {
		t.Fatalf("notice round trip failed: encoded=%q key=%q got=%v ok=%v", encoded, key, got, ok)
	}
	if key, args, ok := ParseNotice("compare.notice.scope"); !ok || key != "compare.notice.scope" || args != nil {
		t.Fatalf("static notice parse = key=%q args=%v ok=%v", key, args, ok)
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

func TestBuildUsesCanonicalFieldsAndTables(t *testing.T) {
	canonical := comparisonTestReport("same", 10, "m-v1", "same", "速率", true)
	canonical.Results[0].Fields = []model.Field{{Key: "state", Label: "状态", Value: "完成"}}
	canonical.Results[0].Tables = []model.Table{{
		Title:   "当前值",
		Columns: []string{"名称", "状态"},
		Rows:    [][]string{{"系统", "完成"}},
	}}

	// Build is a machine-data operation. Its inputs, observations, summary and
	// notices must be byte-stable for the same canonical reports.
	first, err := Build([]model.Report{canonical, canonical}, Options{})
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	second, err := Build([]model.Report{canonical, canonical}, Options{})
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if !reflect.DeepEqual(first.Notices, second.Notices) {
		t.Fatalf("canonical notices changed between builds: first=%v second=%v", first.Notices, second.Notices)
	}
	if first.Summary.ObservedChanges != 0 || second.Summary.ObservedChanges != 0 {
		t.Fatalf("canonical reports produced false observations: first=%+v second=%+v", first.Summary, second.Summary)
	}
	for _, comparison := range []Report{first, second} {
		for _, module := range comparison.Modules {
			if len(module.Changes) != 0 {
				t.Fatalf("canonical reports produced false module changes: %+v", module.Changes)
			}
		}
	}

	// GeneratedAt is intentionally run-dependent; every other comparison field,
	// including Notices, is canonical machine data.
	first.GeneratedAt = time.Time{}
	second.GeneratedAt = time.Time{}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("canonical comparison changed between builds:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestBuildVersionNoticesAndValidationErrorsUseCanonicalValues(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 20, "m-v1", "same", "rate", true)
	second.SchemaVersion = "ecs.report/v2"
	second.Tool.Version = "0.7.0"

	firstResult, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	firstResult.GeneratedAt = time.Time{}
	secondResult.GeneratedAt = time.Time{}
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("version notices changed between builds:\nfirst=%+v\nsecond=%+v", firstResult, secondResult)
	}
	if len(firstResult.Notices) < 2 {
		t.Fatalf("version notices missing: %v", firstResult.Notices)
	}
	key, args, ok := ParseNotice(firstResult.Notices[0])
	if !ok || key != "compare.notice.schemaMixed" || len(args) != 1 || !strings.Contains(args[0], "ecs.report/v2") {
		t.Fatalf("schema notice is not canonical: key=%q args=%v ok=%v", key, args, ok)
	}

	_, inputErr := Build([]model.Report{first}, Options{})
	var inputValidation *ValidationError
	if inputErr == nil || !errors.As(inputErr, &inputValidation) || inputValidation.Key != "compare.help.inputs" || inputErr.Error() != inputValidation.Key {
		t.Fatalf("input validation error is not canonical: err=%v value=%+v", inputErr, inputValidation)
	}

	_, rangeErr := Build([]model.Report{first, second}, Options{Reference: 2})
	var rangeValidation *ValidationError
	if rangeErr == nil || !errors.As(rangeErr, &rangeValidation) || rangeValidation.Key != "compare.help.referenceRange" || len(rangeValidation.Args) != 1 || rangeValidation.Args[0] != 2 {
		t.Fatalf("reference validation error is not canonical: err=%v value=%+v", rangeErr, rangeValidation)
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
	first.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Route", Columns: []string{"Target", "ASN"},
		ColumnKeys: []string{"target", "asn"}, RowIdentity: "target",
		Rows: [][]string{{"example", "AS1"}},
	}}
	second.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Route", Columns: []string{"Target", "ASN"},
		ColumnKeys: []string{"target", "asn"}, RowIdentity: "target",
		Rows: [][]string{{"example", "AS2"}},
	}}
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

func TestBuildTableCompareUsesDeclaredIdentityAndStableSchema(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 11, "m-v1", "same", "rate", true)
	first.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "路由", Columns: []string{"目标", "状态"},
		ColumnKeys: []string{"target", "state"}, RowIdentity: "target",
		Rows: [][]string{{"route-a", "10"}, {"route-b", "20"}},
	}}
	second.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Routes", Columns: []string{"Target", "State"},
		ColumnKeys: []string{"target", "state"}, RowIdentity: "target",
		Rows: [][]string{{"route-a", "11"}, {"route-b", "20"}},
	}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := data.Modules[0].Changes
	if len(changes) != 1 {
		t.Fatalf("localized table labels should not split the schema: %+v", changes)
	}
	if changes[0].Key != "table:network.routes\x1ftarget\x1estate:route-a:state" {
		t.Fatalf("table observation key = %q, want declared schema/identity/column key", changes[0].Key)
	}
	if len(changes[0].Values) != 2 || !changes[0].Values[0].Available || !changes[0].Values[1].Available {
		t.Fatalf("changed table cell was treated as a row add/remove: %+v", changes[0].Values)
	}
	if changes[0].Values[0].Value != "10" || changes[0].Values[1].Value != "11" {
		t.Fatalf("table cell values = %+v", changes[0].Values)
	}
}

func TestBuildTableCompareUsesConservativeFallbackForUndeclaredIdentity(t *testing.T) {
	tests := []struct {
		name           string
		firstIdentity  string
		secondIdentity string
	}{
		{name: "missing identity"},
		{name: "invalid identity", firstIdentity: "missing", secondIdentity: "missing"},
		{name: "duplicate identity", firstIdentity: "target", secondIdentity: "target"},
		{name: "mismatched identity", firstIdentity: "target", secondIdentity: "value"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
			second := comparisonTestReport("second", 11, "m-v1", "same", "rate", true)
			first.Results[0].Tables = []model.Table{{
				Key: "network.routes", Title: "Routes", Columns: []string{"Target", "Value"},
				ColumnKeys: []string{"target", "value"}, RowIdentity: testCase.firstIdentity,
				// The first column is deliberately duplicated while the numeric
				// value column is unique. It must never become an inferred key.
				Rows: [][]string{{"same", "10"}, {"same", "20"}},
			}}
			second.Results[0].Tables = []model.Table{{
				Key: "network.routes", Title: "Routes", Columns: []string{"Target", "Value"},
				ColumnKeys: []string{"target", "value"}, RowIdentity: testCase.secondIdentity,
				Rows: [][]string{{"same", "11"}, {"same", "20"}},
			}}
			data, err := Build([]model.Report{first, second}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			changes := data.Modules[0].Changes
			if len(changes) != 1 {
				t.Fatalf("undeclared/invalid identity should use one positional cell change: %+v", changes)
			}
			wantKey := "table:network.routes\x1ftarget\x1evalue:row-index:0:column:value"
			if changes[0].Key != wantKey {
				t.Fatalf("fallback observation key = %q, want %q", changes[0].Key, wantKey)
			}
			if strings.Contains(changes[0].Key, "10") || strings.Contains(changes[0].Key, "11") || strings.Contains(changes[0].Key, "same") {
				t.Fatalf("fallback observation key used row data as identity: %q", changes[0].Key)
			}
			if len(changes[0].Values) != 2 || !changes[0].Values[0].Available || !changes[0].Values[1].Available {
				t.Fatalf("fallback treated the numeric change as a row add/remove: %+v", changes[0].Values)
			}
			if changes[0].Values[0].Value != "10" || changes[0].Values[1].Value != "11" {
				t.Fatalf("fallback cell values = %+v", changes[0].Values)
			}
		})
	}
}

func TestBuildLegacyTableFallbackDoesNotUseDisplaySchemaOrRowValuesAsKeys(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 11, "m-v1", "same", "rate", true)
	first.Results[0].Tables = []model.Table{{
		Title: "路由", Columns: []string{"目标", "数值"}, Rows: [][]string{{"route-a", "10"}},
	}}
	second.Results[0].Tables = []model.Table{{
		Title: "Routes", Columns: []string{"Target", "Value"}, Rows: [][]string{{"route-a", "11"}},
	}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := data.Modules[0].Changes
	if len(changes) != 1 || changes[0].Key != "table:legacy:0" {
		t.Fatalf("legacy table should use a positional whole-table observation: %+v", changes)
	}
	if strings.Contains(changes[0].Key, "route-a") || strings.Contains(changes[0].Key, "11") {
		t.Fatalf("legacy observation key used display/data values: %+v", changes[0])
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

// ---- 不可比原因的差异归因 ----
//
// "method 或参数口径不一致"这个结论本身不可行动。本项目约定工作负载语义变了
// 就升 measurement.method，跨版本报告因此经常撞上它，用户需要知道的是究竟哪
// 一项变了。下面的用例把这份明细钉住。

// differenceFor 返回某个具名分量的差异，找不到时返回 nil。
func differenceFor(issue MetricIssue, field string) *Difference {
	for index := range issue.Differences {
		if issue.Differences[index].Field == field {
			return &issue.Differences[index]
		}
	}
	return nil
}

func issueFor(issues []MetricIssue, reason string) (MetricIssue, bool) {
	for _, issue := range issues {
		if issue.Reason == reason {
			return issue, true
		}
	}
	return MetricIssue{}, false
}

func TestBuildExplainsMethodRevisionAsTheOnlyDifference(t *testing.T) {
	first := comparisonTestReport("first", 100, "sysbench-cpu-prime20000-v1", "same", "rate", true)
	second := comparisonTestReport("second", 120, "sysbench-cpu-prime20000-v2", "same", "rate", true)
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := issueFor(data.Modules[0].MetricIssues, "method_or_parameters_mismatch")
	if !ok {
		t.Fatalf("expected a mismatch issue: %+v", data.Modules[0].MetricIssues)
	}
	// 只有 method 变了，明细里就不该出现别的分量——否则真正变了的那一项会被淹掉。
	if len(issue.Differences) != 1 {
		t.Fatalf("differences = %+v, want only the method", issue.Differences)
	}
	method := differenceFor(issue, "method")
	if method == nil {
		t.Fatalf("method difference missing: %+v", issue.Differences)
	}
	want := []DifferenceValue{
		{Report: 0, Value: "sysbench-cpu-prime20000-v1"},
		{Report: 1, Value: "sysbench-cpu-prime20000-v2"},
	}
	if !reflect.DeepEqual(method.Values, want) {
		t.Fatalf("method values = %+v, want %+v", method.Values, want)
	}
}

func TestBuildNamesTheParameterThatChangedInsteadOfTheWholeScope(t *testing.T) {
	first := comparisonTestReport("first", 100, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 120, "m-v1", "same", "rate", true)
	first.Results[0].Methodology.Parameters = map[string]string{"scope_revision": "1", "threads": "4", "duration": "15s"}
	second.Results[0].Methodology.Parameters = map[string]string{"scope_revision": "1", "threads": "8", "duration": "15s"}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := issueFor(data.Modules[0].MetricIssues, "method_or_parameters_mismatch")
	if !ok {
		t.Fatalf("expected a mismatch issue: %+v", data.Modules[0].MetricIssues)
	}
	// 相同的 duration 与 scope_revision 不该出现。
	if len(issue.Differences) != 1 {
		t.Fatalf("differences = %+v, want only threads", issue.Differences)
	}
	threads := differenceFor(issue, "parameter:threads")
	if threads == nil {
		t.Fatalf("threads difference missing: %+v", issue.Differences)
	}
	if threads.Values[0].Value != "4" || threads.Values[1].Value != "8" {
		t.Fatalf("threads values = %+v", threads.Values)
	}
}

func TestBuildReportsEveryChangedComponentTogether(t *testing.T) {
	first := comparisonTestReport("first", 100, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 120, "m-v2", "same", "rate", true)
	second.Results[0].Measurements[0].Unit = "ops/s"
	second.Results[0].Methodology.Kind = "observation"
	second.Results[0].Methodology.Parameters = map[string]string{"scope_revision": "2", "workload": "same"}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := issueFor(data.Modules[0].MetricIssues, "method_or_parameters_mismatch")
	if !ok {
		t.Fatalf("expected a mismatch issue: %+v", data.Modules[0].MetricIssues)
	}
	for _, field := range []string{"unit", "method", "kind", "parameter:scope_revision"} {
		if differenceFor(issue, field) == nil {
			t.Errorf("difference for %q is missing: %+v", field, issue.Differences)
		}
	}
	// workload 两边相同，不该被列进去。
	if differenceFor(issue, "parameter:workload") != nil {
		t.Errorf("identical parameter was reported as a difference: %+v", issue.Differences)
	}
}

func TestBuildTreatsAMissingParameterKeyAsADifference(t *testing.T) {
	first := comparisonTestReport("first", 100, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 120, "m-v1", "same", "rate", true)
	first.Results[0].Methodology.Parameters = map[string]string{"scope_revision": "1", "threads": "4"}
	second.Results[0].Methodology.Parameters = map[string]string{"scope_revision": "1"}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := issueFor(data.Modules[0].MetricIssues, "method_or_parameters_mismatch")
	if !ok {
		t.Fatalf("expected a mismatch issue: %+v", data.Modules[0].MetricIssues)
	}
	threads := differenceFor(issue, "parameter:threads")
	if threads == nil {
		t.Fatalf("a key present in only one report must be reported: %+v", issue.Differences)
	}
	// 缺这个键要能表达出来，而不是被当成两边都没有。
	if threads.Values[0].Value != "4" || threads.Values[1].Value != "" {
		t.Fatalf("missing key values = %+v", threads.Values)
	}
}

// 判定与明细必须出自同一组分量：可比的指标不该带出任何差异，
// 否则就会出现"明细说有差异、判定却说可比"的自相矛盾输出。
func TestBuildProducesNoDifferencesWhenSignaturesAgree(t *testing.T) {
	first := comparisonTestReport("first", 100, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 120, "m-v1", "same", "rate", true)
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	module := data.Modules[0]
	if len(module.Metrics) != 1 {
		t.Fatalf("identical signatures must stay comparable: %+v", module)
	}
	for _, issue := range module.MetricIssues {
		if len(issue.Differences) > 0 {
			t.Fatalf("comparable metric reported differences: %+v", issue)
		}
	}
}

// 取不到签名的报告（数值非有限、缺 method、参数口径损坏）由各自的 issue 说明，
// 混进差异归因只会把用户引向错误的结论。
func TestBuildKeepsUnsignedReportsOutOfDifferences(t *testing.T) {
	first := comparisonTestReport("first", 100, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 120, "m-v2", "same", "rate", true)
	third := comparisonTestReport("third", math.NaN(), "m-v3", "same", "rate", true)
	data, err := Build([]model.Report{first, second, third}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := issueFor(data.Modules[0].MetricIssues, "method_or_parameters_mismatch")
	if !ok {
		t.Fatalf("expected a mismatch issue: %+v", data.Modules[0].MetricIssues)
	}
	method := differenceFor(issue, "method")
	if method == nil {
		t.Fatalf("method difference missing: %+v", issue.Differences)
	}
	for _, value := range method.Values {
		if value.Report == 2 {
			t.Fatalf("a report without a signature leaked into the differences: %+v", method.Values)
		}
	}
}

// ---- 跨版本比较 ----
//
// 硬拒绝跨 schema 版本的代价是：schema 一升版，用户手里所有旧报告立刻永久
// 不可比，而"比较不同时期的报告"正是 compare 存在的理由。放宽之后必须钉住
// 两件事：结论仍然出得来，且可比性被如实降级。

func TestBuildComparesAcrossSchemaVersionsAndDowngradesComparability(t *testing.T) {
	first := comparisonTestReport("a", 100, "m1", "p", "速率", true)
	second := comparisonTestReport("b", 120, "m1", "p", "速率", true)
	second.SchemaVersion = "ecs.report/v2"

	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatalf("跨 schema 版本必须仍能比较：%v", err)
	}
	if data.Summary.Comparability != PartiallyComparable {
		t.Fatalf("跨 schema 版本的可比性 = %q，want %q", data.Summary.Comparability, PartiallyComparable)
	}
	// 签名一致的指标照常出结论——降级针对的是签名覆盖不到的部分。
	if data.Summary.ComparableMetrics != 1 || data.Summary.Improved != 1 {
		t.Fatalf("签名一致的指标应照常比较：metrics=%d improved=%d",
			data.Summary.ComparableMetrics, data.Summary.Improved)
	}
	if len(data.Notices) == 0 || !strings.Contains(data.Notices[0], "schema") {
		t.Fatalf("跨版本提示必须排在最前：%v", data.Notices)
	}
	for _, version := range []string{"ecs.report/v1", "ecs.report/v2"} {
		if !strings.Contains(data.Notices[0], version) {
			t.Fatalf("提示里缺少版本 %q：%q", version, data.Notices[0])
		}
	}
	if got := data.SchemaVersions(); !reflect.DeepEqual(got, []string{"ecs.report/v1", "ecs.report/v2"}) {
		t.Fatalf("SchemaVersions() = %v", got)
	}
	if data.Inputs[1].SchemaVersion != "ecs.report/v2" {
		t.Fatalf("每份输入都要记录自己的 schema：%+v", data.Inputs[1])
	}
}

func TestBuildExplainsDifferentToolVersionsWithoutDowngrading(t *testing.T) {
	first := comparisonTestReport("a", 100, "m1", "p", "速率", true)
	second := comparisonTestReport("b", 120, "m1", "p", "速率", true)
	second.Tool.Version = "0.7.0"

	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// ecs 版本不同不影响结论可信度，只是需要给下方的差异一个解释。
	if data.Summary.Comparability != Comparable {
		t.Fatalf("同 schema 不同 ecs 版本不应降级：%q", data.Summary.Comparability)
	}
	if len(data.Notices) == 0 || !strings.Contains(data.Notices[0], "compare.notice.toolMixed") ||
		!strings.Contains(data.Notices[0], "0.7.0") {
		t.Fatalf("缺少 ecs 版本差异提示 key/参数：%v", data.Notices)
	}
	key, args, ok := ParseNotice(data.Notices[0])
	if !ok || key != "compare.notice.toolMixed" || len(args) != 1 || !strings.Contains(args[0], "0.7.0") {
		t.Fatalf("ecs 版本差异提示不是 canonical key/参数：key=%q args=%v ok=%v", key, args, ok)
	}
}

func TestBuildKeepsNoticesUnchangedWhenVersionsAgree(t *testing.T) {
	first := comparisonTestReport("a", 100, "m1", "p", "速率", true)
	second := comparisonTestReport("b", 120, "m1", "p", "速率", true)

	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if data.Summary.Comparability != Comparable {
		t.Fatalf("全部一致时可比性 = %q", data.Summary.Comparability)
	}
	// 版本一致是常态，不该为常态多出任何提示行。
	if len(data.Notices) != 3 {
		t.Fatalf("版本一致时提示数 = %d，want 3：%v", len(data.Notices), data.Notices)
	}
	if len(data.SchemaVersions()) != 1 {
		t.Fatalf("SchemaVersions() = %v", data.SchemaVersions())
	}
}

func TestBuildStillFlagsMethodChangesAcrossSchemaVersions(t *testing.T) {
	first := comparisonTestReport("a", 100, "m1", "p", "速率", true)
	second := comparisonTestReport("b", 120, "m2", "p", "速率", true)
	second.SchemaVersion = "ecs.report/v2"

	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// 保护比较安全的始终是签名而不是版本号：method 变了照样拦下并归因。
	if data.Summary.ComparableMetrics != 0 || data.Summary.MetricIssues != 1 {
		t.Fatalf("method 不同必须落进 issue：metrics=%d issues=%d",
			data.Summary.ComparableMetrics, data.Summary.MetricIssues)
	}
	issue := data.Modules[0].MetricIssues[0]
	if len(issue.Differences) != 1 || issue.Differences[0].Field != "method" {
		t.Fatalf("差异必须归因到 method：%+v", issue.Differences)
	}
}
