package report

import (
	"bytes"
	"reflect"
	"testing"

	"ecs/internal/i18n"
)

func TestLocalizeEnglishKeepsMachineFieldsAndRawEvidence(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	data := sampleReport()
	localized := Localize(data)
	result := localized.Results[0]
	if localized.Summary.Headline != "1 checks completed" || !reflect.DeepEqual(localized.Notices, data.Notices) || renderMessage(localized.Notices[0]) != "The official STREAM executable was not found; the memory benchmark did not run." || renderMessage(localized.Notices[1]) != "OS" {
		t.Fatalf("localized report frame = summary=%q notices=%+v", localized.Summary.Headline, localized.Notices)
	}
	if localized.Summary.Status != data.Summary.Status || localized.Summary.OK != data.Summary.OK || result.Title != "OS" || result.Description != "Resource snapshot; not a performance benchmark" || result.Summary != "Done" || result.Error != "Failed" || result.Methodology.Label != "Inventory" || result.Methodology.Engine != "Inventory" || result.Methodology.Profile != "OS" || result.Methodology.Parameters["workload"] != "standard" || result.Methodology.ComparisonScope != "Resource snapshot; not a performance benchmark" {
		t.Fatalf("localized result text = %+v", result)
	}
	if result.Fields[0].Label != "OS" || result.Fields[0].Value != "Done" || result.Measurements[0].Label != "Single-thread event rate" || result.Measurements[0].Display != "Done" || result.Measurements[0].Rating != "Done" {
		t.Fatalf("localized field/measurement = %+v / %+v", result.Fields, result.Measurements)
	}
	table := result.Tables[0]
	if table.Title != "Current value" || table.Columns[0] != "Status" || table.Columns[1] != "Value" || table.Rows[0][0] != "Done" || result.Notes[0] != "OS" || result.Sources[0].Purpose != "Note" {
		t.Fatalf("localized table/supporting text = %+v notes=%v sources=%v", table, result.Notes, result.Sources)
	}
	if result.Retry.SelectionRule != "OS" || result.Retry.TriggerReasons[0] != "OS" || result.Retry.Attempts[0].Interference.Reasons[0] != "OS" || result.Retry.Attempts[0].Measurements[0].Label != "Single-thread event rate" || result.Retry.Attempts[0].Measurements[0].Display != "Done" || result.Retry.Attempts[0].Interference.Measurements[0].Label != "OS" || result.Retry.Attempts[0].Interference.Measurements[0].Display != "Done" || result.TextBlocks[0].Title != "Note" || result.TextBlocks[0].Language != "en" || result.TextBlocks[0].Content != "raw output 192.0.2.10" || result.Failures[0].Message != "raw timeout" {
		t.Fatalf("raw evidence was translated: blocks=%+v failures=%+v", result.TextBlocks, result.Failures)
	}
	if localized.Run.ID != "run-1" || localized.Run.Exposure != "local" || !localized.Run.Offline || result.ID != "system" || result.Status != data.Results[0].Status || result.Methodology.Kind != "inventory" || result.Methodology.Parameters["scope_revision"] != "1" || result.Fields[0].Key != "state" || result.Fields[0].Sensitive || result.Measurements[0].Key != "events" || result.Measurements[0].Unit != "events/s" || result.Measurements[0].Method != "sysbench-v1" || result.Measurements[0].HigherIsBetter == nil || !*result.Measurements[0].HigherIsBetter || table.Key != "system.state" || table.ColumnKeys[0] != "state" || table.RowIdentity != "state" || len(table.NumericColumns) != 1 || table.NumericColumns[0] != 1 || len(table.NumericHigherIsBetter) != 1 || !table.NumericHigherIsBetter[0] || len(table.SensitiveColumns) != 1 || table.SensitiveColumns[0] != 0 || result.Retry.Attempts[0].Measurements[0].Key != "events" || result.Retry.Attempts[0].Measurements[0].Unit != "events/s" || result.Retry.Attempts[0].Measurements[0].HigherIsBetter == nil || !*result.Retry.Attempts[0].Measurements[0].HigherIsBetter || result.Retry.Attempts[0].Interference.Measurements[0].Key != "load" || result.Retry.Attempts[0].Interference.Measurements[0].Unit != "load" || result.Retry.Attempts[0].Interference.Measurements[0].HigherIsBetter == nil || *result.Retry.Attempts[0].Interference.Measurements[0].HigherIsBetter {
		t.Fatalf("machine fields changed during localization: %+v", result)
	}
}

func TestLocalizeReturnsIndependentDisplayCopy(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	data := sampleReport()
	before, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	localized := Localize(data)
	result := &localized.Results[0]
	if result.Retry == data.Results[0].Retry || result.Evidence == data.Results[0].Evidence || result.Measurements[0].HigherIsBetter == data.Results[0].Measurements[0].HigherIsBetter || result.Retry.Attempts[0].Evidence == data.Results[0].Retry.Attempts[0].Evidence || result.Retry.Attempts[0].Measurements[0].HigherIsBetter == data.Results[0].Retry.Attempts[0].Measurements[0].HigherIsBetter || result.Retry.Attempts[0].Interference.Measurements[0].HigherIsBetter == data.Results[0].Retry.Attempts[0].Interference.Measurements[0].HigherIsBetter {
		t.Fatal("Localize reused mutable pointers")
	}
	localized.Run.Requested[0] = "changed"
	localized.Run.OutputFormats[0] = "changed"
	localized.Notices[0].Key = "changed"
	localized.SensitiveIPs[0] = "changed"
	result.Methodology.Parameters["scope_revision"] = "changed"
	result.Fields[0].Value = "changed"
	*result.Measurements[0].HigherIsBetter = false
	result.Tables[0].Columns[0] = "changed"
	result.Tables[0].ColumnKeys[0] = "changed"
	result.Tables[0].NumericColumns[0] = 0
	result.Tables[0].NumericHigherIsBetter[0] = false
	result.Tables[0].SensitiveColumns[0] = 1
	result.Tables[0].Rows[0][0] = "changed"
	result.TextBlocks[0].Title = "changed"
	result.Notes[0] = "changed"
	result.Sources[0].Purpose = "changed"
	result.Failures[0].Message = "changed"
	result.Evidence.Valid = 99
	result.Retry.SelectionRule = "changed"
	result.Retry.TriggerReasons[0] = "changed"
	result.Retry.Attempts[0].Evidence.Valid = 99
	result.Retry.Attempts[0].Interference.Reasons[0] = "changed"
	*result.Retry.Attempts[0].Measurements[0].HigherIsBetter = false
	*result.Retry.Attempts[0].Interference.Measurements[0].HigherIsBetter = true
	result.Retry.Attempts[0].Measurements[0].Label = "changed"
	after, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("mutating Localize result changed canonical report")
	}
}
