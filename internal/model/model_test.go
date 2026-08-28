package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestResultLifecycleFailuresAndSummaries(t *testing.T) {
	start := time.Now().Add(-time.Second)
	result := NewResult("demo", "Demo")
	result.Finish(start)
	if result.ID != "demo" || result.Title != "Demo" || result.Status != StatusOK || result.StartedAt.IsZero() || result.StartedAt.Location() != time.UTC || !result.StartedAt.Equal(start.UTC()) || result.DurationMS < 0 {
		t.Fatalf("finished result = %+v", result)
	}
	result.AddFailure(Failure{Stage: "probe", Target: "target", Message: "unavailable"})
	result.AddFailure(Failure{Category: FailureUnknown, Stage: "probe", Target: "target", Message: "unavailable", Count: 2})
	result.AddFailure(Failure{Category: FailureUnknown, Stage: "probe", Target: "other", Message: "unavailable"})
	result.AddFailure(Failure{Category: FailureUnknown, Stage: "probe", Target: "target", Retryable: true, Message: "unavailable"})
	result.AddFailure(Failure{Category: FailureUnknown, Stage: "probe", Target: "target", Message: "changed"})
	result.AddFailure(Failure{Category: FailureTimeout, Stage: "probe", Target: "target", Retryable: true, Message: "timeout", Count: 2})
	if len(result.Failures) != 5 || result.Failures[0].Category != FailureUnknown || result.Failures[0].Count != 3 || result.Failures[1].Target != "other" || result.Failures[1].Count != 1 || !result.Failures[2].Retryable || result.Failures[3].Message != "changed" || result.Failures[4].Count != 2 {
		t.Fatalf("coalesced failures = %+v", result.Failures)
	}

	skipped := NewResult("skip", "Skip")
	skipMessage := NewMessage("message.runner.skip.offline")
	skipped.Skip(skipMessage)
	if skipped.Status != StatusSkipped || !reflect.DeepEqual(skipped.SummaryMessages, []Message{skipMessage}) {
		t.Fatalf("skipped result = %+v", skipped)
	}
	argResult := NewResult("skip-args", "Skip args")
	argMessage := NewMessage("message.notice.egressShared", "shared discovery")
	argResult.Skip(argMessage)
	argMessage.Args[0] = "mutated"
	if argResult.SummaryMessages[0].Args[0] != "shared discovery" {
		t.Fatal("Skip retained a shared Message argument slice")
	}
	failed := NewResult("fail", "Fail")
	failed.Fail(errors.New("broken"))
	if failed.Status != StatusError || failed.Error != "broken" || !reflect.DeepEqual(failed.SummaryMessages, []Message{{Key: "message.result.failed"}}) {
		t.Fatalf("failed result = %+v", failed)
	}

	for _, test := range []struct {
		name                          string
		results                       []Result
		status                        Status
		ok, warnings, skipped, errors int
		messages                      []Message
	}{
		{name: "empty", status: StatusOK, messages: []Message{{Key: "message.summary.allOK", Args: []string{"0"}}}},
		{name: "ok", results: []Result{{Status: StatusOK}, {Status: StatusOK}}, status: StatusOK, ok: 2, messages: []Message{{Key: "message.summary.allOK", Args: []string{"2"}}}},
		{name: "warning and skipped", results: []Result{{Status: StatusWarning}, {Status: StatusSkipped}}, status: StatusWarning, warnings: 1, skipped: 1, messages: []Message{{Key: "message.summary.withWarnings", Args: []string{"0", "1"}}, {Key: "message.summary.skipped", Args: []string{"1"}}}},
		{name: "error takes precedence", results: []Result{{Status: StatusOK}, {Status: StatusWarning}, {Status: StatusSkipped}, {Status: StatusError}}, status: StatusError, ok: 1, warnings: 1, skipped: 1, errors: 1, messages: []Message{{Key: "message.summary.withErrors", Args: []string{"1", "1"}}, {Key: "message.summary.skipped", Args: []string{"1"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := Report{Results: test.results}
			Summarize(&report)
			got := report.Summary
			if got.Status != test.status || got.OK != test.ok || got.Warnings != test.warnings || got.Skipped != test.skipped || got.Errors != test.errors || !reflect.DeepEqual(got.Messages, test.messages) {
				t.Fatalf("summary = %+v", got)
			}
		})
	}
}

func TestReportJSONUsesOnlyStructuredSummaryMessages(t *testing.T) {
	report := Report{
		Summary: Summary{Status: StatusWarning, Messages: []Message{NewMessage("message.summary.withWarnings", 1, 1)}},
		Results: []Result{{ID: "demo", Status: StatusWarning, SummaryMessages: []Message{NewMessage("message.test", "value")}}},
	}
	content, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(content, &object); err != nil {
		t.Fatal(err)
	}
	summaryObject, ok := object["summary"].(map[string]any)
	if !ok {
		t.Fatalf("report summary object missing: %s", content)
	}
	if _, ok := summaryObject["messages"]; !ok {
		t.Fatalf("structured global summary messages missing: %s", content)
	}
	if _, ok := summaryObject["headline"]; ok {
		t.Fatalf("legacy headline serialized: %s", content)
	}
	results, ok := object["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results missing: %s", content)
	}
	resultObject, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result object missing: %s", content)
	}
	if _, ok := resultObject["summary"]; ok {
		t.Fatalf("legacy result summary serialized: %s", content)
	}
	if _, ok := resultObject["summary_messages"]; !ok {
		t.Fatalf("structured result summary missing: %s", content)
	}
}

func TestLegacySummaryInputIsIgnoredWithoutCompatibilityFields(t *testing.T) {
	var report Report
	if err := json.Unmarshal([]byte(`{"summary":{"headline":"legacy"},"results":[{"id":"demo","summary":"legacy"}]}`), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Summary.Messages) != 0 || len(report.Results) != 1 || len(report.Results[0].SummaryMessages) != 0 {
		t.Fatalf("legacy summary input affected structured state: %+v", report)
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	original := NewMessage("message.test", "ipv6", 2)
	content, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Message
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("Message round trip = %#v, want %#v", decoded, original)
	}
}

func TestEvidenceNormalizationAndCoverageGrades(t *testing.T) {
	unnormalized := Evidence{Valid: 9, Expected: 2}
	if got := unnormalized.EvidenceRatio(); got != 1 {
		t.Fatalf("unnormalized EvidenceRatio = %v, want 1", got)
	}
	for _, test := range []struct {
		name                  string
		valid, expected       int
		wantValid, wantExpect int
		grade                 EvidenceGrade
		ratio                 float64
	}{
		{name: "not planned", valid: 0, expected: 0, grade: EvidenceNotPlanned},
		{name: "insufficient", valid: 0, expected: 3, wantExpect: 3, grade: EvidenceInsufficient},
		{name: "partial", valid: 1, expected: 2, wantValid: 1, wantExpect: 2, grade: EvidencePartial, ratio: 0.5},
		{name: "complete", valid: 2, expected: 2, wantValid: 2, wantExpect: 2, grade: EvidenceComplete, ratio: 1},
		{name: "clamped", valid: 9, expected: 2, wantValid: 2, wantExpect: 2, grade: EvidenceComplete, ratio: 1},
		{name: "negative", valid: -1, expected: -2, grade: EvidenceNotPlanned},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := NewEvidence(test.valid, test.expected, "sample")
			if evidence.Valid != test.wantValid || evidence.Expected != test.wantExpect || evidence.Grade != test.grade || evidence.EffectiveGrade() != test.grade || evidence.EvidenceRatio() != test.ratio {
				t.Fatalf("evidence = %+v ratio=%v", evidence, evidence.EvidenceRatio())
			}
		})
	}
	var nilEvidence *Evidence
	nilEvidence.Normalize()
}

func TestRedactedCopyCoversReportContainersAndIsolation(t *testing.T) {
	local4, textIP, tableIP := "192.0.2.10", "2001:db8::10", "203.0.113.77"
	report := Report{
		Notices:      []Message{NewMessage("message.test", local4)},
		SensitiveIPs: []string{local4},
		Run:          RunInfo{Requested: []string{"system", "network"}, OutputFormats: []string{"json", "txt"}},
		Summary:      Summary{Messages: []Message{NewMessage("message.test", local4)}},
		Results: []Result{{
			SummaryMessages: []Message{NewMessage("message.test", local4)},
			Evidence:        &Evidence{Valid: 1, Expected: 2},
			Interference:    &Interference{Reasons: []Message{NewMessage("message.test", local4)}, Measurements: []Measurement{{Key: "result-interference", Display: RawValue(local4)}}},
			Fields: []Field{
				{Key: "secret", Value: RawValue("secret-token"), Sensitive: true},
				{Key: "remote", Value: RawValue("198.51.100.2")},
				{Key: "status", Value: KeyValue("probe.status.ok"), Sensitive: true},
				{Key: "local_text", Value: RawValue("local " + local4)},
			},
			Measurements: []Measurement{{Key: "local", Label: local4, Display: RawValue(local4)}},
			Methodology:  Methodology{Parameters: map[string]string{"local": local4}},
			Tables: []Table{{
				Columns: []TableColumn{
					{Key: "id", Label: "id"},
					{Key: "address", Label: "address", Sensitive: true},
				},
				Rows: [][]Value{
					{RawValue("row-1"), RawValue(tableIP)},
					{RawValue("row-key"), KeyValue(local4)},
				},
			}},
			TextBlocks: []TextBlock{{Content: "trace " + textIP, Sensitive: true}},
			Notes:      []string{local4},
			Sources:    []Source{{URL: "https://" + local4 + "/info"}},
			Failures:   []Failure{{Message: local4}},
			Retry: &RetryInfo{
				SelectionRule:  NewMessage("message.test", local4),
				TriggerReasons: []Message{NewMessage("message.test", local4)},
				Attempts: []RetryAttempt{{
					Evidence:     &Evidence{Valid: 1, Expected: 1},
					Measurements: []Measurement{{Key: "attempt", Display: RawValue(local4)}},
					Interference: Interference{
						Reasons:      []Message{NewMessage("message.test", local4)},
						Measurements: []Measurement{{Key: "interference", Display: RawValue(local4)}},
					},
				}},
			},
		}},
	}

	before, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	hidden := RedactedCopy(report, false)
	masked4, maskedTextIP, maskedTableIP := Mask(local4), Mask(textIP), Mask(tableIP)
	result := hidden.Results[0]
	statusKey, statusIsKey := result.Fields[2].Value.Key()
	tableKey, tableKeyIsKey := result.Tables[0].Rows[1][1].Key()
	if !hidden.Run.Redacted || hidden.SensitiveIPs != nil || result.Tables[0].Rows[0][1].Text() != maskedTableIP || result.Tables[0].Rows[1][1].Text() != local4 || !tableKeyIsKey || tableKey != local4 || hidden.Notices[0].Args[0] != masked4 || hidden.Summary.Messages[0].Args[0] != masked4 || result.SummaryMessages[0].Args[0] != masked4 || result.Evidence == nil || result.Interference == nil || result.Interference.Reasons[0].Args[0] != masked4 || result.Interference.Measurements[0].Display.Text() != masked4 || result.Fields[0].Value.Text() != "hidden" || result.Fields[1].Value.Text() != "198.51.100.2" || result.Fields[2].Value.Text() != "probe.status.ok" || !statusIsKey || statusKey != "probe.status.ok" || result.Fields[3].Value.Text() != "local "+masked4 || result.Measurements[0].Display.Text() != masked4 || result.Methodology.Parameters["local"] != masked4 || result.TextBlocks[0].Content != "trace "+maskedTextIP || result.Sources[0].URL != "https://"+masked4+"/info" || result.Failures[0].Message != masked4 || result.Retry.SelectionRule.Args[0] != masked4 || result.Retry.TriggerReasons[0].Args[0] != masked4 || result.Retry.Attempts[0].Evidence == nil || result.Retry.Attempts[0].Measurements[0].Display.Text() != masked4 || result.Retry.Attempts[0].Interference.Reasons[0].Args[0] != masked4 || result.Retry.Attempts[0].Interference.Measurements[0].Display.Text() != masked4 || result.Notes[0] != masked4 {
		t.Fatalf("redacted containers = %+v", result)
	}
	hidden.Notices[0].Args[0] = "changed"
	hidden.Summary.Messages[0].Args[0] = "changed"
	hidden.Run.Requested[0], hidden.Run.OutputFormats[0] = "changed", "changed"
	hidden.Results[0].SummaryMessages[0].Args[0] = "changed"
	hidden.Results[0].Evidence.Valid = 99
	hidden.Results[0].Fields[0].Value = RawValue("changed")
	hidden.Results[0].Measurements[0].Display = RawValue("changed")
	hidden.Results[0].Failures[0].Message = "changed"
	hidden.Results[0].Interference.Reasons[0].Args[0] = "changed"
	hidden.Results[0].Interference.Measurements[0].Display = RawValue("changed")
	hidden.Results[0].Retry.SelectionRule.Args[0] = "changed"
	hidden.Results[0].Retry.TriggerReasons[0].Args[0] = "changed"
	hidden.Results[0].Retry.Attempts[0].Evidence.Valid = 99
	hidden.Results[0].Retry.Attempts[0].Measurements[0].Display = RawValue("changed")
	hidden.Results[0].Retry.Attempts[0].Interference.Reasons[0].Args[0] = "changed"
	hidden.Results[0].Retry.Attempts[0].Interference.Measurements[0].Display = RawValue("changed")
	hidden.Results[0].Notes[0] = "changed"
	hidden.Results[0].Sources[0].URL = "changed"
	hidden.Results[0].TextBlocks[0].Content = "changed"
	hidden.Results[0].Tables[0].Columns[0].Label = "changed"
	hidden.Results[0].Tables[0].Columns[0].Key = "changed"
	hidden.Results[0].Tables[0].Columns[1].Numeric = true
	hidden.Results[0].Tables[0].Columns[1].HigherIsBetter = true
	hidden.Results[0].Tables[0].Columns[1].Sensitive = false
	hidden.Results[0].Tables[0].Rows[0][0] = RawValue("changed")
	hidden.Results[0].Tables[0].Rows[0][1] = RawValue("changed")
	hidden.Results[0].Tables[0].Rows[1][1] = KeyValue("changed")
	hidden.Results[0].Methodology.Parameters["local"] = "changed"
	after, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || report.SensitiveIPs[0] != local4 {
		t.Fatal("RedactedCopy shared nested data")
	}

	revealed := RedactedCopy(report, true)
	revealedStatusKey, revealedStatusIsKey := revealed.Results[0].Fields[2].Value.Key()
	revealedTableKey, revealedTableKeyIsKey := revealed.Results[0].Tables[0].Rows[1][1].Key()
	if revealed.Run.Redacted || revealed.SensitiveIPs != nil || revealed.Notices[0].Args[0] != local4 || revealed.Summary.Messages[0].Args[0] != local4 || revealed.Results[0].SummaryMessages[0].Args[0] != local4 || revealed.Results[0].Fields[0].Value.Text() != "secret-token" || revealed.Results[0].Fields[2].Value.Text() != "probe.status.ok" || !revealedStatusIsKey || revealedStatusKey != "probe.status.ok" || revealed.Results[0].Fields[3].Value.Text() != "local "+local4 || revealed.Results[0].Tables[0].Rows[0][1].Text() != tableIP || revealed.Results[0].Tables[0].Rows[1][1].Text() != local4 || !revealedTableKeyIsKey || revealedTableKey != local4 || revealed.Results[0].TextBlocks[0].Content != "trace "+textIP {
		t.Fatalf("revealed copy = %+v", revealed.Results[0])
	}
}

func TestMaskAndFormattingCategories(t *testing.T) {
	for _, test := range []struct {
		value, want string
	}{
		{value: "192.0.2.10", want: "192.0.x.x"},
		{value: "2001:db8::10", want: "2001:db8:x:x:x:x:x:x"},
		{value: "192.0.2.10/24", want: "192.0.x.x/24"},
		{value: "192.0.2.10:443", want: "192.0.x.x:443"},
		{value: "[2001:db8::10]:443", want: "[2001:db8:x:x:x:x:x:x]:443"},
		{value: "example.com", want: "hidden"},
		{value: "", want: ""},
	} {
		if got := Mask(test.value); got != test.want {
			t.Errorf("Mask(%q) = %q, want %q", test.value, got, test.want)
		}
	}
	text := MaskIPsInText("local 192.0.2.10 and 2001:db8::10; remote 198.51.100.2")
	if text != "local 192.0.x.x and 2001:db8:x:x:x:x:x:x; remote 198.51.x.x" {
		t.Fatalf("MaskIPsInText = %q", text)
	}
	for _, test := range []struct {
		value uint64
		want  string
	}{
		{value: 0, want: "0 B"},
		{value: 1024, want: "1.00 KiB"},
		{value: 1024 * 1024, want: "1.00 MiB"},
	} {
		if got := FormatBytes(test.value); got != test.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		value float64
		want  string
	}{
		{value: math.NaN(), want: "n/a"},
		{value: 12.345, want: "12.35 unit"},
		{value: 123.45, want: "123.5 unit"},
		{value: 1234.5, want: "1234 unit"},
	} {
		if got := FormatRate(test.value, "unit"); got != test.want {
			t.Errorf("FormatRate(%v) = %q, want %q", test.value, got, test.want)
		}
	}
	if BoolPtr(true) == nil || !*BoolPtr(true) || BoolPtr(false) == nil || *BoolPtr(false) {
		t.Fatal("BoolPtr did not preserve both boolean values")
	}
}
