package report

import (
	"fmt"
	"strings"
	"testing"

	comparison "ecs/internal/compare"
	"ecs/internal/model"
)

func comparisonReportFixture(t *testing.T, count, reference int) comparison.Report {
	t.Helper()
	reports := make([]model.Report, count)
	labels := make([]string, count)
	for index := range reports {
		data := sampleReport()
		data.Run.ID = fmt.Sprintf("run-%d", index+1)
		data.Results[0].Title = "系统"
		data.Results[0].Methodology = model.Methodology{
			Kind: "inventory", Parameters: map[string]string{"scope_revision": "1"},
		}
		state := "ready"
		if index > 0 {
			state = "done"
		}
		data.Results[0].Measurements = []model.Measurement{{
			Key: "cpu", Label: "CPU", Value: float64(100 + index*20),
			Unit: "points", Display: fmt.Sprintf("%d points", 100+index*20), Method: "fixture-v1", HigherIsBetter: model.BoolPtr(true),
		}}
		data.Results[0].Fields = []model.Field{{Key: "state", Label: "状态", Value: state}}
		data.Results[0].Tables = []model.Table{{
			Key: "system.state", Title: "状态", Columns: []string{"ID", "状态"},
			ColumnKeys: []string{"id", "state"}, RowIdentity: "id",
			Rows: [][]string{{"row-1", state}},
		}}
		reports[index], labels[index] = data, fmt.Sprintf("node-%d", index+1)
	}
	data, err := comparison.Build(reports, comparison.Options{Labels: labels, Reference: reference, Tool: model.ToolInfo{Name: "ecs", Version: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestComparisonMarkdownShowsMetricAndDiscreteChanges(t *testing.T) {
	data := comparisonReportFixture(t, 2, 0)
	if len(data.Modules) != 1 || len(data.Modules[0].Metrics) != 1 || len(data.Modules[0].Changes) < 2 {
		t.Fatalf("comparison changes = %+v", data.Modules)
	}
	markdown := ComparisonMarkdown(data)
	if !strings.Contains(markdown, "120 points") || !strings.Contains(markdown, "done") {
		t.Fatalf("comparison Markdown lost changed values:\n%s", markdown)
	}
}
