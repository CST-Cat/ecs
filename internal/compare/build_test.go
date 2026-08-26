package compare

import (
	"errors"
	"math"
	"reflect"
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
		Run:           model.RunInfo{ID: id, Profile: profile, StartedAt: started},
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

func reportWithResults(id string, results ...model.Result) model.Report {
	report := comparisonTestReport(id, 0, "m-v1", "same", "rate", true)
	report.Results = results
	return report
}

func comparisonMeasurement(key, label string, value float64, unit, method string, higher bool) model.Measurement {
	return model.Measurement{
		Key: key, Label: label, Value: value, Unit: unit, Display: model.FormatRate(value, unit),
		Method: method, HigherIsBetter: model.BoolPtr(higher),
	}
}

func comparisonModuleResult(id, title string, status model.Status, evidence *model.Evidence, measurements ...model.Measurement) model.Result {
	return model.Result{
		ID: id, Title: title, Status: status, Evidence: evidence, Measurements: measurements,
		Methodology: model.Methodology{Kind: "inventory", Profile: "same", Parameters: map[string]string{"scope_revision": "1", "workload": "same"}},
	}
}

func findModule(report Report, id string) *Module {
	for index := range report.Modules {
		if report.Modules[index].ID == id {
			return &report.Modules[index]
		}
	}
	return nil
}

func findMetric(module *Module, key string) *Metric {
	if module == nil {
		return nil
	}
	for index := range module.Metrics {
		if module.Metrics[index].Key == key {
			return &module.Metrics[index]
		}
	}
	return nil
}

func TestBuildValidationAndReference(t *testing.T) {
	reports := []model.Report{
		comparisonTestReport("one", 10, "m-v1", "same", "rate", true),
		comparisonTestReport("two", 20, "m-v1", "same", "rate", true),
	}
	for _, test := range []struct {
		name      string
		reports   []model.Report
		reference int
		key       string
		arg       int
		errorText string
	}{
		{name: "too few", reports: reports[:1], key: "compare.help.inputs", errorText: "compare.help.inputs"},
		{name: "negative reference", reports: reports, reference: -1, key: "compare.help.referenceRange", arg: 2, errorText: "compare.help.referenceRange(2)"},
		{name: "reference past end", reports: reports, reference: 2, key: "compare.help.referenceRange", arg: 2, errorText: "compare.help.referenceRange(2)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Build(test.reports, Options{Reference: test.reference})
			var validation *ValidationError
			if err == nil || !errors.As(err, &validation) || validation.Key != test.key || validation.Error() != test.errorText {
				t.Fatalf("Build validation = %v, want %s", err, test.errorText)
			}
			if test.key == "compare.help.referenceRange" && (len(validation.Args) != 1 || validation.Args[0] != test.arg) {
				t.Fatalf("validation args = %#v", validation.Args)
			}
		})
	}
	data, err := Build(reports, Options{Reference: 1})
	if err != nil || data.Reference != 1 || data.Summary.Reports != 2 {
		t.Fatalf("valid nonzero reference = %+v, %v", data, err)
	}
}

func TestBuildInputsAndStructuredNotices(t *testing.T) {
	reports := []model.Report{
		comparisonTestReport("one", 10, "m-v1", "same", "rate", true),
		comparisonTestReport("two", 10, "m-v1", "same", "rate", true),
		comparisonTestReport("three", 10, "m-v1", "same", "rate", true),
	}
	reports[0].SchemaVersion, reports[1].SchemaVersion, reports[2].SchemaVersion = "ecs.report/v1", "", "ecs.report/v2"
	reports[0].Tool.Version, reports[1].Tool.Version, reports[2].Tool.Version = "1", "2", "1"
	reports[1].Run.IPVersion, reports[1].Run.Redacted = "6", true
	options := Options{
		Labels: []string{"same", "same", ""},
		Tool:   model.ToolInfo{Name: "compare", Version: "test"},
	}
	data, err := Build(reports, options)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{data.Inputs[0].Label, data.Inputs[1].Label, data.Inputs[2].Label}; !reflect.DeepEqual(got, []string{"same", "same #2", "report-3"}) {
		t.Fatalf("unique labels = %v", got)
	}
	if data.Inputs[1].Index != 1 || data.Inputs[1].ReportID != "two" || data.Inputs[1].ToolVersion != "2" || data.Inputs[1].Profile != reports[1].Run.Profile || !data.Inputs[1].StartedAt.Equal(reports[1].Run.StartedAt) || data.Inputs[1].SchemaVersion != "" || data.Inputs[1].IPVersion != "6" || !data.Inputs[1].Redacted {
		t.Fatalf("input metadata = %+v", data.Inputs[1])
	}
	if !reflect.DeepEqual(data.Tool, options.Tool) {
		t.Fatalf("comparison tool metadata = %+v, want %+v", data.Tool, options.Tool)
	}
	if data.SchemaVersion != SchemaVersion || data.GeneratedAt.IsZero() || data.GeneratedAt.Location() != time.UTC {
		t.Fatalf("comparison metadata = schema %q generated_at=%v", data.SchemaVersion, data.GeneratedAt)
	}
	if !reflect.DeepEqual(data.SchemaVersions(), []string{"ecs.report/v1", "ecs.report/v2"}) || data.Summary.Comparability != PartiallyComparable {
		t.Fatalf("schema handling = %v, %+v", data.SchemaVersions(), data.Summary)
	}
	expectedNotices := []Notice{
		{Key: "compare.notice.schemaMixed", Args: []string{"ecs.report/v1, ecs.report/v2"}},
		{Key: "compare.notice.toolMixed", Args: []string{"1, 2"}},
		{Key: "compare.notice.scope"},
		{Key: "compare.notice.relative"},
		{Key: "compare.notice.observation"},
	}
	if !reflect.DeepEqual(data.Notices, expectedNotices) {
		t.Fatalf("structured notices = %#v, want %#v", data.Notices, expectedNotices)
	}

	defaults, err := Build(reports, Options{})
	if err != nil || defaults.Inputs[0].Label != "report-1" || defaults.Inputs[2].Label != "report-3" {
		t.Fatalf("default labels = %+v, %v", defaults.Inputs, err)
	}
	toolOnly := []model.Report{comparisonTestReport("a", 10, "m-v1", "same", "rate", true), comparisonTestReport("b", 11, "m-v1", "same", "rate", true)}
	toolOnly[0].Tool.Version, toolOnly[1].Tool.Version = "1", "2"
	toolData, err := Build(toolOnly, Options{})
	if err != nil || toolData.Summary.Comparability != Comparable || !hasNoticeKey(toolData.Notices, "compare.notice.toolMixed") || hasNoticeKey(toolData.Notices, "compare.notice.schemaMixed") {
		t.Fatalf("tool-only comparison = %+v, %v", toolData.Summary, toolData.Notices)
	}
}

func hasNoticeKey(notices []Notice, want string) bool {
	for _, notice := range notices {
		if notice.Key == want {
			return true
		}
	}
	return false
}

func TestBuildIsIndependentOfCurrentLanguage(t *testing.T) {
	reports := []model.Report{
		comparisonTestReport("one", 10, "m-v1", "same", "中文标签", true),
		comparisonTestReport("two", 11, "m-v1", "same", "English label", true),
	}
	original := i18n.Current()
	t.Cleanup(func() { i18n.Set(original) })
	build := func(lang i18n.Lang) Report {
		i18n.Set(lang)
		data, err := Build(reports, Options{})
		if err != nil {
			t.Fatal(err)
		}
		data.GeneratedAt = time.Time{}
		return data
	}
	zh, en := build(i18n.LangZH), build(i18n.LangEN)
	if !reflect.DeepEqual(zh, en) {
		t.Fatalf("Build changed with UI language:\nzh=%+v\nen=%+v", zh, en)
	}
}

func TestBuildModulesStatusesEvidenceAndUnion(t *testing.T) {
	cpu0 := comparisonTestReport("base", 100, "m-v1", "same", "rate", true).Results[0]
	cpu0.Evidence = &model.Evidence{Valid: 2, Expected: 2, Grade: model.EvidenceInsufficient}
	network := comparisonModuleResult("network", "Network", model.StatusOK, nil)
	base := reportWithResults("base", cpu0, network)
	cpu1 := comparisonTestReport("candidate", 110, "m-v1", "same", "rate", true).Results[0]
	cpu1.Status = model.StatusWarning
	cpu1.Evidence = &model.Evidence{Valid: 1, Expected: 2, Grade: model.EvidenceComplete}
	candidateOnly := comparisonModuleResult("candidate-only", "Candidate only", model.StatusOK, nil)
	candidate := reportWithResults("candidate", cpu1, candidateOnly)
	data, err := Build([]model.Report{base, candidate}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{data.Modules[0].ID, data.Modules[1].ID, data.Modules[2].ID}; !reflect.DeepEqual(got, []string{"cpu", "network", "candidate-only"}) {
		t.Fatalf("union module order = %v", got)
	}
	cpu := findModule(data, "cpu")
	if cpu == nil || cpu.Statuses[0].Status != model.StatusOK || cpu.Statuses[1].Status != model.StatusWarning || cpu.Evidence[0].Valid != 2 || cpu.Evidence[0].Grade != model.EvidenceComplete || cpu.Evidence[1].Grade != model.EvidencePartial || cpu.Evidence[1].Ratio != 0.5 {
		t.Fatalf("status/evidence normalization = %+v", cpu)
	}
	networkModule := findModule(data, "network")
	if networkModule == nil || !reflect.DeepEqual(networkModule.MissingReports, []int{1}) || networkModule.Comparability != NotComparable {
		t.Fatalf("missing module = %+v", networkModule)
	}
	candidateOnlyModule := findModule(data, "candidate-only")
	if candidateOnlyModule == nil || !reflect.DeepEqual(candidateOnlyModule.MissingReports, []int{0}) || candidateOnlyModule.Comparability != NotComparable {
		t.Fatalf("candidate-only module = %+v", candidateOnlyModule)
	}
	if data.Summary.Modules != 3 || data.Summary.ComparableMetrics != 1 || data.Summary.StatusChanges != 1 || data.Summary.EvidenceChanges != 1 || data.Summary.MissingModuleValues != 2 || data.Summary.Comparability != PartiallyComparable {
		t.Fatalf("summary status/evidence = %+v", data.Summary)
	}

	duplicate := reportWithResults("duplicate", cpu0, cpu0)
	duplicateData, err := Build([]model.Report{duplicate, candidate}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	duplicateModule := findModule(duplicateData, "cpu")
	if duplicateModule == nil || len(duplicateModule.MetricIssues) == 0 || duplicateModule.MetricIssues[0].Reason != "duplicate_module_id" || !reflect.DeepEqual(duplicateModule.MetricIssues[0].Reports, []int{0}) {
		t.Fatalf("duplicate module issue = %+v", duplicateModule)
	}
}

func TestBuildMetricsDirectionsRanksAndOutcomes(t *testing.T) {
	rateValues := []float64{100, 120, 100, 80}
	latencyValues := []float64{100, 80, 100, 120}
	reports := make([]model.Report, len(rateValues))
	for index := range reports {
		reports[index] = comparisonTestReport("r"+string(rune('0'+index)), rateValues[index], "m-v1", "same", "rate", true)
		reports[index].Results[0].Measurements = append(reports[index].Results[0].Measurements, comparisonMeasurement("latency", "latency", latencyValues[index], "ms", "m-v1", false))
		zeroValue := 0.0
		if index == 1 {
			zeroValue = 1
		}
		reports[index].Results[0].Measurements = append(reports[index].Results[0].Measurements, comparisonMeasurement("zero", "zero", zeroValue, "unit", "m-v1", true))
	}
	data, err := Build(reports, Options{})
	if err != nil {
		t.Fatal(err)
	}
	module := findModule(data, "cpu")
	rate, latency, zero := findMetric(module, "rate"), findMetric(module, "latency"), findMetric(module, "zero")
	if rate == nil || latency == nil || zero == nil || data.Summary.Improved != 3 || data.Summary.Regressed != 2 || data.Summary.Unchanged != 4 {
		t.Fatalf("metric outcomes = %+v summary=%+v", module, data.Summary)
	}
	if rate.Values[1].Outcome != OutcomeImproved || rate.Values[2].Outcome != OutcomeUnchanged || rate.Values[3].Outcome != OutcomeRegressed || rate.Values[1].Rank != 1 || !rate.Values[1].Best || rate.Values[0].Rank != 2 || rate.Values[2].Rank != 2 || !rate.Values[3].Worst || rate.Values[3].Rank != 4 {
		t.Fatalf("higher metric ranking = %+v", rate.Values)
	}
	if rate.Values[1].PerformanceChangePercent == nil || *rate.Values[1].PerformanceChangePercent < 19.9 || *rate.Values[1].PerformanceChangePercent > 20.1 || rate.Values[3].PerformanceChangePercent == nil || *rate.Values[3].PerformanceChangePercent > -19.9 || *rate.Values[3].PerformanceChangePercent < -20.1 || rate.Values[1].QualityRatio != 1 || rate.Values[3].QualityRatio != 0.15 {
		t.Fatalf("higher metric quality/change = %+v", rate.Values)
	}
	if latency.Values[1].Outcome != OutcomeImproved || latency.Values[3].Outcome != OutcomeRegressed || !latency.Values[1].Best || !latency.Values[3].Worst || latency.Values[1].PerformanceChangePercent == nil || *latency.Values[1].PerformanceChangePercent < 19.9 || *latency.Values[1].PerformanceChangePercent > 20.1 || latency.Values[3].PerformanceChangePercent == nil || *latency.Values[3].PerformanceChangePercent > -19.9 || *latency.Values[3].PerformanceChangePercent < -20.1 {
		t.Fatalf("lower metric outcomes = %+v", latency.Values)
	}
	if zero.Values[0].PerformanceChangePercent != nil || zero.Values[1].PerformanceChangePercent != nil || zero.Values[1].Outcome != OutcomeImproved {
		t.Fatalf("zero reference delta = %+v", zero.Values)
	}
}

func TestBuildReferenceNoReferenceAndParameterScope(t *testing.T) {
	first := comparisonTestReport("first", 100, "m-v1", "profile-a", "rate", true)
	second := comparisonTestReport("second", 120, "m-v1", "profile-a", "rate", true)
	third := comparisonTestReport("third", 130, "m-v1", "profile-a", "rate", true)
	third.Results[0].Measurements = nil
	first.Results[0].Methodology.Parameters["build_sha256"] = "1234567890abcdef"
	second.Results[0].Methodology.Parameters["build_sha256"] = "1234567890abcdef"
	data, err := Build([]model.Report{first, second, third}, Options{Reference: 1})
	if err != nil {
		t.Fatal(err)
	}
	metric := findMetric(findModule(data, "cpu"), "rate")
	if metric == nil || metric.Values[0].Outcome != OutcomeRegressed || metric.Values[2].Available || metric.Values[2].Outcome != OutcomeNoReference {
		t.Fatalf("alternate reference/no reference = %+v", metric)
	}
	if !strings.Contains(metric.ParameterScope, "profile-a") || !strings.Contains(metric.ParameterScope, "scope=v1") || !strings.Contains(metric.ParameterScope, "build#=1234567890ab") || strings.Contains(metric.ParameterScope, "1234567890abcdef") {
		t.Fatalf("parameter scope = %q", metric.ParameterScope)
	}
	first.Results[0].Methodology.Parameters["workload"] = "mutated"
	if metric.Parameters["workload"] != "profile-a" {
		t.Fatalf("metric parameter map was not cloned: %v", metric.Parameters)
	}
	first.Results[0].Methodology.Parameters["workload"] = "profile-a"
	missingReference, err := Build([]model.Report{first, second, third}, Options{Reference: 2})
	if err != nil {
		t.Fatal(err)
	}
	missingMetric := findMetric(findModule(missingReference, "cpu"), "rate")
	if missingMetric == nil || !missingMetric.Values[0].Available || !missingMetric.Values[1].Available || missingMetric.Values[0].Outcome != OutcomeNoReference || missingMetric.Values[1].Outcome != OutcomeNoReference || missingMetric.Values[0].PerformanceChangePercent != nil || missingMetric.Values[1].PerformanceChangePercent != nil {
		t.Fatalf("missing reference = %+v", missingMetric)
	}
}

func TestBuildMetricIssuesAndSignatureDifferences(t *testing.T) {
	for _, test := range []struct {
		name        string
		reason      string
		wantReports []int
		setup       func([]model.Report)
	}{
		{name: "missing key", reason: "missing_metric_key", wantReports: []int{0, 1}, setup: func(reports []model.Report) {
			reports[0].Results[0].Measurements[0].Key = ""
			reports[1].Results[0].Measurements[0].Key = ""
		}},
		{name: "no matching", reason: "no_matching_metric", wantReports: []int{0}, setup: func(reports []model.Report) {
			reports[0].Results[0].Measurements[0].Key = "only-first"
			reports[1].Results[0].Measurements = nil
		}},
		{name: "duplicate key", reason: "duplicate_metric_key", wantReports: []int{0}, setup: func(reports []model.Report) {
			reports[0].Results[0].Measurements = append(reports[0].Results[0].Measurements, reports[0].Results[0].Measurements[0])
		}},
		{name: "missing method or direction", reason: "missing_method_or_direction", wantReports: []int{0}, setup: func(reports []model.Report) {
			reports[0].Results[0].Measurements[0].Method = ""
		}},
		{name: "non finite", reason: "non_finite_value", wantReports: []int{0}, setup: func(reports []model.Report) {
			reports[0].Results[0].Measurements[0].Value = math.NaN()
		}},
		{name: "invalid parameter scope", reason: "missing_or_invalid_parameter_scope", wantReports: []int{0}, setup: func(reports []model.Report) {
			reports[0].Results[0].Methodology.Parameters = map[string]string{"workload": "same"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reports := []model.Report{comparisonTestReport("first", 10, "m-v1", "same", "rate", true), comparisonTestReport("second", 11, "m-v1", "same", "rate", true)}
			test.setup(reports)
			data, err := Build(reports, Options{})
			if err != nil {
				t.Fatal(err)
			}
			module := findModule(data, "cpu")
			if module == nil || len(module.MetricIssues) != 1 || module.MetricIssues[0].Reason != test.reason || !reflect.DeepEqual(module.MetricIssues[0].Reports, test.wantReports) || module.Comparability != NotComparable || data.Summary.MetricIssues != 1 || data.Summary.Comparability != NotComparable {
				t.Fatalf("metric issue = %+v", module)
			}
		})
	}

	full := []model.Report{
		comparisonTestReport("first", 10, "m-v1", "same", "rate", true),
		comparisonTestReport("second", 11, "m-v1", "same", "rate", true),
		comparisonTestReport("third", 12, "m-v1", "same", "rate", true),
	}
	full[1].Results[0].Measurements[0].Unit = "ms"
	full[1].Results[0].Measurements[0].Method = "m-v2"
	full[1].Results[0].Measurements[0].HigherIsBetter = model.BoolPtr(false)
	full[1].Results[0].Methodology.Kind = "protocol"
	full[1].Results[0].Methodology.Parameters["scope_revision"] = "2"
	full[1].Results[0].Methodology.Parameters["workload"] = "alternate"
	delete(full[2].Results[0].Methodology.Parameters, "workload")
	data, err := Build(full, Options{})
	if err != nil {
		t.Fatal(err)
	}
	issue := findModule(data, "cpu").MetricIssues[0]
	wantFields := []string{"unit", "method", "direction", "kind", "parameter:scope_revision", "parameter:workload"}
	if issue.Reason != "method_or_parameters_mismatch" || len(issue.Differences) != len(wantFields) {
		t.Fatalf("full signature issue = %+v", issue)
	}
	for index, difference := range issue.Differences {
		if difference.Field != wantFields[index] || len(difference.Values) != 3 || difference.Values[0].Report != 0 || difference.Values[1].Report != 1 || difference.Values[2].Report != 2 {
			t.Fatalf("signature difference = %+v", issue.Differences)
		}
		if difference.Field == "parameter:workload" && (difference.Values[0].Value != "same" || difference.Values[1].Value != "alternate" || difference.Values[2].Value != "") {
			t.Fatalf("workload signature difference = %+v", difference)
		}
	}

	partial := []model.Report{
		comparisonTestReport("first", 10, "m-v1", "same", "rate", true),
		comparisonTestReport("second", 11, "m-v1", "same", "rate", true),
		comparisonTestReport("third", 12, "m-v2", "same", "rate", true),
	}
	partialData, err := Build(partial, Options{})
	if err != nil {
		t.Fatal(err)
	}
	partialModule := findModule(partialData, "cpu")
	if len(partialModule.Metrics) != 1 || partialModule.MetricIssues[0].Reason != "some_reports_use_different_method_or_parameters" || partialModule.Comparability != PartiallyComparable {
		t.Fatalf("partial signature issue = %+v", partialModule)
	}
	var methodDifference *Difference
	for index := range partialModule.MetricIssues[0].Differences {
		if partialModule.MetricIssues[0].Differences[index].Field == "method" {
			methodDifference = &partialModule.MetricIssues[0].Differences[index]
			break
		}
	}
	if methodDifference == nil || len(methodDifference.Values) != 3 || methodDifference.Values[0].Report != 0 || methodDifference.Values[1].Report != 1 || methodDifference.Values[2].Report != 2 || methodDifference.Values[0].Value != "m-v1" || methodDifference.Values[1].Value != "m-v1" || methodDifference.Values[2].Value != "m-v2" {
		t.Fatalf("partial method difference = %+v", partialModule.MetricIssues[0].Differences)
	}
}

func TestBuildFieldObservationsUseStableKeys(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 10, "m-v1", "same", "rate", true)
	first.Results[0].Fields = []model.Field{
		{Key: "provider", Label: "服务商", Value: "A"},
		{Key: "duplicate", Value: "one"}, {Key: "duplicate", Value: "two"}, {Value: "ignored"},
	}
	second.Results[0].Fields = []model.Field{{Key: "provider", Label: "Provider", Value: "B"}, {Key: "duplicate", Value: "three"}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := findModule(data, "cpu").Changes
	if len(changes) != 1 || data.Summary.ObservedChanges != len(changes) || changes[0].Key != "field:provider" || changes[0].Label != "服务商" || changes[0].Source != "field" || changes[0].Values[0].Value != "A" || changes[0].Values[1].Value != "B" {
		t.Fatalf("field observations = %+v", changes)
	}
}

func TestBuildDeclaredTableIdentitySurvivesLocalizationReorderingAndDuplicates(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 10, "m-v1", "same", "rate", true)
	first.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "路由", Columns: []string{"类别", "数值", "标识"},
		ColumnKeys: []string{"kind", "value", "id"}, RowIdentity: "id",
		Rows: [][]string{{"same", "10", "route-a"}, {"same", "20", "route-b"}},
	}}
	second.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Routes", Columns: []string{"Kind", "Value", "ID"},
		ColumnKeys: []string{"kind", "value", "id"}, RowIdentity: "id",
		Rows: [][]string{{"same", "11", "route-b"}, {"same", "10", "route-a"}},
	}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := findModule(data, "cpu").Changes
	if len(changes) != 1 || changes[0].Source != "table" || !strings.Contains(changes[0].Key, "network.routes") || !strings.Contains(changes[0].Key, "route-b") || !strings.Contains(changes[0].Key, "value") || changes[0].Values[0].Value != "20" || changes[0].Values[1].Value != "11" {
		t.Fatalf("declared table identity = %+v", changes)
	}
}

func TestBuildTableFallbacksStayConservative(t *testing.T) {
	tableReports := func(first, second model.Table) []model.Report {
		left := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
		right := comparisonTestReport("second", 10, "m-v1", "same", "rate", true)
		left.Results[0].Tables, right.Results[0].Tables = []model.Table{first}, []model.Table{second}
		return []model.Report{left, right}
	}
	base := func(key, identity string, rows [][]string) model.Table {
		return model.Table{Key: key, Title: "Routes", Columns: []string{"Target", "Value"}, ColumnKeys: []string{"target", "value"}, RowIdentity: identity, Rows: rows}
	}
	t.Run("no identity is positional", func(t *testing.T) {
		data, err := Build(tableReports(
			base("routes", "", [][]string{{"a", "10"}, {"b", "20"}}),
			base("routes", "", [][]string{{"b", "20"}, {"a", "10"}})), Options{})
		if err != nil {
			t.Fatal(err)
		}
		changes := findModule(data, "cpu").Changes
		if len(changes) == 0 {
			t.Fatal("positional fallback hid row reorder")
		}
		for _, change := range changes {
			if !strings.Contains(change.Key, "row-index") || strings.Contains(change.Key, ":a:") || strings.Contains(change.Key, ":b:") {
				t.Fatalf("positional key guessed row data: %+v", change)
			}
		}
	})
	t.Run("invalid identity is positional", func(t *testing.T) {
		data, err := Build(tableReports(base("routes", "missing", [][]string{{"a", "10"}}), base("routes", "missing", [][]string{{"a", "11"}})), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if changes := findModule(data, "cpu").Changes; len(changes) == 0 || !strings.Contains(changes[0].Key, "row-index") {
			t.Fatalf("invalid identity fallback = %+v", changes)
		}
	})
	t.Run("legacy schema is whole snapshot", func(t *testing.T) {
		data, err := Build(tableReports(
			model.Table{Title: "Routes", Columns: []string{"Target"}, Rows: [][]string{{"a"}}},
			model.Table{Title: "Routes", Columns: []string{"Target"}, Rows: [][]string{{"b"}}}), Options{})
		if err != nil {
			t.Fatal(err)
		}
		changes := findModule(data, "cpu").Changes
		if len(changes) != 1 || changes[0].Key != "table:legacy:0" || !strings.Contains(changes[0].Values[0].Value, "columns=1") {
			t.Fatalf("legacy table fallback = %+v", changes)
		}
	})
	t.Run("malformed shape is whole snapshot", func(t *testing.T) {
		data, err := Build(tableReports(base("routes", "", [][]string{{"a", "10"}}), base("routes", "", [][]string{{"a"}})), Options{})
		if err != nil {
			t.Fatal(err)
		}
		changes := findModule(data, "cpu").Changes
		if len(changes) != 1 || !strings.Contains(changes[0].Key, "whole") {
			t.Fatalf("malformed shape fallback = %+v", changes)
		}
	})
	t.Run("different machine key is not compared", func(t *testing.T) {
		data, err := Build(tableReports(base("routes-a", "", [][]string{{"a", "10"}}), base("routes-b", "", [][]string{{"a", "11"}})), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if changes := findModule(data, "cpu").Changes; len(changes) != 0 {
			t.Fatalf("different table keys compared: %+v", changes)
		}
	})
	t.Run("duplicate schema falls back to legacy", func(t *testing.T) {
		left := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
		right := comparisonTestReport("second", 10, "m-v1", "same", "rate", true)
		left.Results[0].Tables = []model.Table{base("routes", "", [][]string{{"a", "10"}}), base("routes", "", [][]string{{"duplicate", "20"}})}
		right.Results[0].Tables = []model.Table{base("routes", "", [][]string{{"b", "11"}})}
		data, err := Build([]model.Report{left, right}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		changes := findModule(data, "cpu").Changes
		if len(changes) != 1 || changes[0].Key != "table:legacy:0" {
			t.Fatalf("duplicate schema fallback = %+v", changes)
		}
	})
}
