package report

import (
	"encoding/json"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func TestLocalizeReturnsIndependentCopyForEveryReportContainer(t *testing.T) {
	originalLanguage := i18n.Current()
	defer i18n.Set(originalLanguage)

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			data := localizeCopyFixture()
			before, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}

			localized := Localize(data)
			afterLocalize, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterLocalize) != string(before) {
				t.Fatal("Localize changed the canonical report")
			}
			if localized.Results[0].Fields[0].Label != i18n.Text("系统") ||
				localized.Results[0].Fields[0].Value != i18n.Text("Linux") {
				t.Fatalf("field display was not localized: %+v", localized.Results[0].Fields[0])
			}
			if localized.Results[0].Tables[0].Title != i18n.Text("当前值") ||
				localized.Results[0].Tables[0].Columns[0] != i18n.Text("当前值") ||
				localized.Results[0].Tables[0].Rows[0][0] != i18n.Text("Linux") {
				t.Fatalf("table display was not localized: %+v", localized.Results[0].Tables[0])
			}
			table := localized.Results[0].Tables[0]
			canonicalTable := data.Results[0].Tables[0]
			if table.Key != canonicalTable.Key ||
				len(table.ColumnKeys) != len(canonicalTable.ColumnKeys) ||
				table.ColumnKeys[0] != canonicalTable.ColumnKeys[0] ||
				table.RowIdentity != canonicalTable.RowIdentity {
				t.Fatalf("table machine schema was localized or lost: got=%+v want=%+v", table, canonicalTable)
			}

			mutateLocalizedReport(&localized)
			afterMutation, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterMutation) != string(before) {
				t.Fatal("mutating Localize's result changed the canonical report")
			}
			if data.SensitiveIPs[0] != "192.0.2.1" {
				t.Fatal("mutating Localize's result changed canonical sensitive IPs")
			}
		})
	}
}

func localizeCopyFixture() model.Report {
	data := sampleReport()
	data.Run.Requested = []string{"system", "disk"}
	data.Run.OutputFormats = []string{"json", "html"}
	data.Notices = []string{"系统"}
	data.SensitiveIPs = []string{"192.0.2.1"}
	data.Results[0].Methodology.Parameters = map[string]string{"scope": "固定值"}
	data.Results[0].Fields = []model.Field{{Key: "os", Label: "系统", Value: "Linux"}}
	higherIsBetter := true
	data.Results[0].Measurements = []model.Measurement{{
		Key: "cpu", Label: "当前值", Display: "1 point", HigherIsBetter: &higherIsBetter,
	}}
	data.Results[0].Tables = []model.Table{{
		// Deliberately use values that are also translatable probe text for the
		// machine keys: Localize must leave them byte-for-byte unchanged.
		Key: "当前值", Title: "当前值", Columns: []string{"当前值", "状态"},
		ColumnKeys: []string{"当前值", "状态"}, RowIdentity: "状态",
		Rows: [][]string{{"Linux", "完成"}}, NumericColumns: []int{1},
		NumericHigherIsBetter: []bool{true}, SensitiveColumns: []int{0},
	}}
	data.Results[0].TextBlocks = []model.TextBlock{{Title: "系统", Content: "raw output"}}
	data.Results[0].Notes = []string{"系统"}
	data.Results[0].Sources = []model.Source{{Name: "source", Purpose: "系统"}}
	data.Results[0].Failures = []model.Failure{{Message: "failure"}}
	data.Results[0].Evidence = model.NewEvidence(1, 2, "sample")
	data.Results[0].Retry = &model.RetryInfo{
		SelectionRule:  "系统",
		TriggerReasons: []string{"系统"},
		Attempts: []model.RetryAttempt{{
			Evidence: model.NewEvidence(1, 1, "sample"),
			Interference: model.Interference{
				Reasons:      []string{"系统"},
				Measurements: []model.Measurement{{Label: "当前值", Display: "1 point", HigherIsBetter: &higherIsBetter}},
			},
			Measurements: []model.Measurement{{Label: "当前值", Display: "1 point", HigherIsBetter: &higherIsBetter}},
		}},
	}
	return data
}

func mutateLocalizedReport(data *model.Report) {
	data.Run.Requested[0] = "changed"
	data.Run.OutputFormats[0] = "changed"
	data.Notices[0] = "changed"
	data.SensitiveIPs[0] = "198.51.100.1"

	result := &data.Results[0]
	result.Methodology.Parameters["scope"] = "changed"
	result.Fields[0].Label = "changed"
	result.Fields[0].Value = "changed"
	result.Measurements[0].Label = "changed"
	*result.Measurements[0].HigherIsBetter = false
	result.Tables[0].Title = "changed"
	result.Tables[0].Columns[0] = "changed"
	result.Tables[0].ColumnKeys[0] = "changed"
	result.Tables[0].Rows[0][0] = "changed"
	result.Tables[0].NumericColumns[0] = 99
	result.Tables[0].NumericHigherIsBetter[0] = false
	result.Tables[0].SensitiveColumns[0] = 99
	result.TextBlocks[0].Title = "changed"
	result.Notes[0] = "changed"
	result.Sources[0].Purpose = "changed"
	result.Failures[0].Message = "changed"
	result.Evidence.Valid = 0
	result.Retry.SelectionRule = "changed"
	result.Retry.TriggerReasons[0] = "changed"
	result.Retry.Attempts[0].Evidence.Valid = 0
	result.Retry.Attempts[0].Interference.Reasons[0] = "changed"
	*result.Retry.Attempts[0].Interference.Measurements[0].HigherIsBetter = false
	*result.Retry.Attempts[0].Measurements[0].HigherIsBetter = false
}
