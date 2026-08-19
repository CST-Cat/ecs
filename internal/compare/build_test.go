package compare

import (
	"errors"
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

func TestBuildComparesMetricUsingDeclaredDirection(t *testing.T) {
	base := comparisonTestReport("base", 100, "latency-v1", "same", "P95", false)
	candidate := comparisonTestReport("candidate", 80, "latency-v1", "same", "P95", false)
	data, err := Build([]model.Report{base, candidate}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if data.Summary.ComparableMetrics != 1 || data.Summary.Improved != 1 {
		t.Fatalf("unexpected metric summary: %+v", data.Summary)
	}
	value := data.Modules[0].Metrics[0].Values[1]
	if !value.Available || value.Outcome != OutcomeImproved || value.PerformanceChangePercent == nil || !nearlyEqual(*value.PerformanceChangePercent, 20) {
		t.Fatalf("lower-is-better change = %+v", value)
	}
}

func TestBuildComparesCanonicalFieldsAndTables(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 11, "m-v1", "same", "rate", true)
	first.Results[0].Fields = []model.Field{{Key: "provider", Label: "Provider", Value: "A"}}
	second.Results[0].Fields = []model.Field{{Key: "provider", Label: "Provider", Value: "B"}}
	first.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Routes", Columns: []string{"Target", "ASN"},
		ColumnKeys: []string{"target", "asn"}, RowIdentity: "target", Rows: [][]string{{"example", "AS1"}},
	}}
	second.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Routes", Columns: []string{"Target", "ASN"},
		ColumnKeys: []string{"target", "asn"}, RowIdentity: "target", Rows: [][]string{{"example", "AS2"}},
	}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Modules[0].Changes) != 2 {
		t.Fatalf("field/table changes = %+v", data.Modules[0].Changes)
	}
	var field, table bool
	for _, change := range data.Modules[0].Changes {
		field = field || change.Source == "field"
		table = table || change.Source == "table"
	}
	if !field || !table {
		t.Fatalf("change sources = %+v", data.Modules[0].Changes)
	}
}

func TestBuildTableCompareUsesDeclaredIdentity(t *testing.T) {
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
	if len(changes) != 1 || len(changes[0].Values) != 2 || !changes[0].Values[0].Available || !changes[0].Values[1].Available {
		t.Fatalf("declared identity did not align rows: %+v", changes)
	}
	if changes[0].Values[0].Value != "10" || changes[0].Values[1].Value != "11" ||
		!strings.Contains(changes[0].Key, "network.routes") || !strings.Contains(changes[0].Key, "target") {
		t.Fatalf("declared table change = %+v", changes[0])
	}
}

func TestBuildUsesConservativeTableFallbackWithoutIdentity(t *testing.T) {
	first := comparisonTestReport("first", 10, "m-v1", "same", "rate", true)
	second := comparisonTestReport("second", 11, "m-v1", "same", "rate", true)
	first.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Routes", Columns: []string{"Target", "Value"},
		ColumnKeys: []string{"target", "value"}, Rows: [][]string{{"same", "10"}, {"same", "20"}},
	}}
	second.Results[0].Tables = []model.Table{{
		Key: "network.routes", Title: "Routes", Columns: []string{"Target", "Value"},
		ColumnKeys: []string{"target", "value"}, Rows: [][]string{{"same", "11"}, {"same", "20"}},
	}}
	data, err := Build([]model.Report{first, second}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := data.Modules[0].Changes
	if len(changes) != 1 || len(changes[0].Values) != 2 || changes[0].Values[0].Value != "10" || changes[0].Values[1].Value != "11" {
		t.Fatalf("undeclared identity did not use positional comparison: %+v", changes)
	}
	if !strings.Contains(changes[0].Key, "row-index") || strings.Contains(changes[0].Key, "same") || strings.Contains(changes[0].Key, "10") {
		t.Fatalf("fallback observation key used row data: %q", changes[0].Key)
	}
}
