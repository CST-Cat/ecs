package compare

import (
	"reflect"
	"testing"

	"ecs/internal/model"
)

func TestBuildKeepsEachMeasurementSignatureMismatchOutOfMetrics(t *testing.T) {
	cases := []struct {
		name          string
		field         string
		firstValue    string
		secondValue   string
		mutateReports func([]model.Report)
	}{
		{
			name:        "method",
			field:       "method",
			firstValue:  "m-v1",
			secondValue: "m-v2",
			mutateReports: func(reports []model.Report) {
				reports[1].Results[0].Measurements[0].Method = "m-v2"
			},
		},
		{
			name:        "unit",
			field:       "unit",
			firstValue:  "events/s",
			secondValue: "ms",
			mutateReports: func(reports []model.Report) {
				reports[1].Results[0].Measurements[0].Unit = "ms"
			},
		},
		{
			name:        "direction",
			field:       "direction",
			firstValue:  "higher",
			secondValue: "lower",
			mutateReports: func(reports []model.Report) {
				reports[1].Results[0].Measurements[0].HigherIsBetter = model.BoolPtr(false)
			},
		},
		{
			name:        "methodology parameters",
			field:       "parameter:workload",
			firstValue:  "same",
			secondValue: "different",
			mutateReports: func(reports []model.Report) {
				reports[1].Results[0].Methodology.Parameters["workload"] = "different"
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reports := []model.Report{
				comparisonTestReport("first", 100, "m-v1", "same", "rate", true),
				comparisonTestReport("second", 120, "m-v1", "same", "rate", true),
			}
			test.mutateReports(reports)

			data, err := Build(reports, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(data.Modules) != 1 {
				t.Fatalf("modules = %+v", data.Modules)
			}
			module := data.Modules[0]
			if module.Comparability != NotComparable || len(module.Metrics) != 0 || len(module.MetricIssues) != 1 {
				t.Fatalf("mismatch was compared: comparability=%q metrics=%+v issues=%+v", module.Comparability, module.Metrics, module.MetricIssues)
			}
			issue := module.MetricIssues[0]
			if issue.Key != "rate" || issue.Reason != "method_or_parameters_mismatch" || !reflect.DeepEqual(issue.Reports, []int{0, 1}) {
				t.Fatalf("metric issue = %+v", issue)
			}
			if len(issue.Differences) != 1 || issue.Differences[0].Field != test.field || len(issue.Differences[0].Values) != 2 {
				t.Fatalf("signature differences = %+v, want only %q", issue.Differences, test.field)
			}
			values := issue.Differences[0].Values
			if values[0].Report != 0 || values[0].Value != test.firstValue || values[1].Report != 1 || values[1].Value != test.secondValue {
				t.Fatalf("signature values = %+v, want reports 0/1 values %q/%q", values, test.firstValue, test.secondValue)
			}
			wantSummary := Summary{Comparability: NotComparable, Reports: 2, Modules: 1, MetricIssues: 1}
			if !reflect.DeepEqual(data.Summary, wantSummary) {
				t.Fatalf("mismatch summary = %+v, want %+v", data.Summary, wantSummary)
			}
		})
	}
}
