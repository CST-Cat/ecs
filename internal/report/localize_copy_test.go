package report

import (
	"bytes"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func TestLocalizeReturnsIndependentDisplayCopy(t *testing.T) {
	originalLanguage := i18n.Current()
	defer i18n.Set(originalLanguage)
	i18n.Set(i18n.LangEN)

	data := sampleReport()
	data.Results[0].Fields = []model.Field{{Key: "state", Label: "系统", Value: "完成"}}
	data.Results[0].Tables = []model.Table{
		{Key: "system.state", Title: "当前值", Columns: []string{"状态"}, ColumnKeys: []string{"state"}, RowIdentity: "state", Rows: [][]string{{"完成"}}},
	}
	data.Results[0].Retry = &model.RetryInfo{SelectionRule: "系统", TriggerReasons: []string{"系统"}}
	before, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}

	localized := Localize(data)
	result := localized.Results[0]
	if result.Fields[0].Label != "OS" || result.Fields[0].Value != "Done" {
		t.Fatalf("localized field = %+v", result.Fields[0])
	}
	if result.Tables[0].Title != "Current value" || result.Tables[0].Columns[0] != "Status" || result.Tables[0].Rows[0][0] != "Done" {
		t.Fatalf("localized table = %+v", result.Tables[0])
	}
	if result.Tables[0].Key != "system.state" || result.Tables[0].ColumnKeys[0] != "state" || result.Tables[0].RowIdentity != "state" {
		t.Fatalf("machine table schema changed: %+v", result.Tables[0])
	}
	if result.Retry == data.Results[0].Retry || result.Retry.SelectionRule != "OS" || result.Retry.TriggerReasons[0] != "OS" {
		t.Fatalf("localized retry copy = %+v", result.Retry)
	}

	result.Fields[0].Label = "changed"
	result.Tables[0].Rows[0][0] = "changed"
	result.Retry.TriggerReasons[0] = "changed"
	after, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || data.Results[0].Fields[0].Label != "系统" || data.Results[0].Tables[0].Rows[0][0] != "完成" || data.Results[0].Retry.TriggerReasons[0] != "系统" {
		t.Fatal("mutating Localize result changed canonical report")
	}
}
