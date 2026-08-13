package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	tests := map[string]string{
		"203.0.113.42":                  "203.0.x.x",
		"2001:db8:1234:5678::1":         "2001:db8:x:x:x:x:x:x",
		"2001:db8::1":                   "2001:db8:x:x:x:x:x:x",
		"64.23.192.0/19":                "64.23.x.x/19",
		"2001:db8:1234:5678::/64":       "2001:db8:x:x:x:x:x:x/64",
		"203.0.113.42:54321":            "203.0.x.x:54321",
		"[2001:db8:1234:5678::1]:54321": "[2001:db8:x:x:x:x:x:x]:54321",
		"server-01":                     "hidden",
		"":                              "",
	}
	for input, want := range tests {
		if got := Mask(input); got != want {
			t.Errorf("Mask(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRedactedCopyDoesNotMutateSource(t *testing.T) {
	original := Report{
		Results: []Result{{
			Methodology: Methodology{Parameters: map[string]string{"target": "203.0.113.10:443"}},
			Evidence:    &Evidence{Valid: 2, Expected: 3, Unit: "sample"},
			Fields:      []Field{{Key: "ip", Value: "203.0.113.10", Sensitive: true}},
			Tables:      []Table{{Columns: []string{"a"}, Rows: [][]string{{"b"}}}},
		}},
	}
	copy := RedactedCopy(original, false)
	if copy.Results[0].Fields[0].Value != "203.0.x.x" {
		t.Fatalf("redacted = %q", copy.Results[0].Fields[0].Value)
	}
	if original.Results[0].Fields[0].Value != "203.0.113.10" {
		t.Fatal("source report was mutated")
	}
	if copy.Results[0].Methodology.Parameters["target"] != "203.0.x.x:443" || original.Results[0].Methodology.Parameters["target"] != "203.0.113.10:443" {
		t.Fatalf("machine parameter redaction/deep copy failed: copy=%q source=%q", copy.Results[0].Methodology.Parameters["target"], original.Results[0].Methodology.Parameters["target"])
	}
	copy.Results[0].Tables[0].Rows[0][0] = "changed"
	if original.Results[0].Tables[0].Rows[0][0] != "b" {
		t.Fatal("nested table was not deep copied")
	}
	copy.Results[0].Evidence.Valid = 0
	if original.Results[0].Evidence.Valid != 2 {
		t.Fatal("evidence pointer was not deep copied")
	}
}

func TestEvidenceNormalizationAndRatio(t *testing.T) {
	evidence := NewEvidence(12, 10, "query")
	if evidence.Valid != 10 || evidence.Expected != 10 || evidence.EvidenceRatio() != 1 {
		t.Fatalf("clamped evidence = %+v ratio=%f", evidence, evidence.EvidenceRatio())
	}
	empty := NewEvidence(-1, -2, "sample")
	if empty.Valid != 0 || empty.Expected != 0 || empty.EvidenceRatio() != 0 {
		t.Fatalf("normalized empty evidence = %+v ratio=%f", empty, empty.EvidenceRatio())
	}
	partial := NewEvidence(3, 4, "run")
	if partial.EvidenceRatio() != 0.75 {
		t.Fatalf("partial evidence ratio = %f, want .75", partial.EvidenceRatio())
	}
}

func TestEvidenceGradesAreDerivedFromCounters(t *testing.T) {
	tests := []struct {
		valid, expected int
		want            EvidenceGrade
	}{
		{valid: 4, expected: 4, want: EvidenceComplete},
		{valid: 2, expected: 4, want: EvidencePartial},
		{valid: 0, expected: 4, want: EvidenceInsufficient},
		{valid: 0, expected: 0, want: EvidenceNotPlanned},
	}
	for _, testCase := range tests {
		evidence := NewEvidence(testCase.valid, testCase.expected, "sample")
		if evidence.Grade != testCase.want || evidence.EffectiveGrade() != testCase.want {
			t.Errorf("grade for %d/%d = %q/%q, want %q", testCase.valid, testCase.expected, evidence.Grade, evidence.EffectiveGrade(), testCase.want)
		}
	}
	stale := Evidence{Valid: 0, Expected: 2, Grade: EvidenceComplete}
	stale.Normalize()
	if stale.Grade != EvidenceInsufficient {
		t.Fatalf("stale serialized grade survived normalization: %+v", stale)
	}
}

func TestAddFailureCoalescesOnlyIdenticalMachineDimensions(t *testing.T) {
	result := Result{}
	result.AddFailure(Failure{Category: FailureTimeout, Stage: "query", Target: "a", Retryable: true, Message: "timeout"})
	result.AddFailure(Failure{Category: FailureTimeout, Stage: "query", Target: "a", Retryable: true, Count: 2, Message: "timeout"})
	result.AddFailure(Failure{Category: FailureTimeout, Stage: "query", Target: "b", Retryable: true, Message: "timeout"})
	if len(result.Failures) != 2 || result.Failures[0].Count != 3 || result.Failures[1].Count != 1 {
		t.Fatalf("coalesced failures = %+v", result.Failures)
	}
}

func TestRedactedCopyDeepCopiesFailuresAndRetryAttempts(t *testing.T) {
	report := Report{Results: []Result{{
		Failures: []Failure{{Category: FailureTimeout, Message: "original"}},
		Retry: &RetryInfo{TriggerReasons: []string{"load"}, Attempts: []RetryAttempt{{
			Evidence:     NewEvidence(1, 1, "run"),
			Measurements: []Measurement{{Key: "rate", Display: "10"}},
			Interference: Interference{Reasons: []string{"steal"}, Measurements: []Measurement{{Key: "steal"}}},
		}}},
	}}}
	copy := RedactedCopy(report, true)
	copy.Results[0].Failures[0].Message = "changed"
	copy.Results[0].Retry.TriggerReasons[0] = "changed"
	copy.Results[0].Retry.Attempts[0].Evidence.Valid = 0
	copy.Results[0].Retry.Attempts[0].Measurements[0].Display = "changed"
	copy.Results[0].Retry.Attempts[0].Interference.Reasons[0] = "changed"
	if report.Results[0].Failures[0].Message != "original" || report.Results[0].Retry.TriggerReasons[0] != "load" ||
		report.Results[0].Retry.Attempts[0].Evidence.Valid != 1 || report.Results[0].Retry.Attempts[0].Measurements[0].Display != "10" ||
		report.Results[0].Retry.Attempts[0].Interference.Reasons[0] != "steal" {
		t.Fatalf("RedactedCopy shared structured diagnostic memory: %+v", report.Results[0])
	}
}

func TestSummarize(t *testing.T) {
	report := Report{Results: []Result{
		{Status: StatusOK},
		{Status: StatusWarning},
		{Status: StatusSkipped},
	}}
	Summarize(&report)
	if report.Summary.Status != StatusWarning || report.Summary.OK != 1 || report.Summary.Warnings != 1 || report.Summary.Skipped != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
}

func TestFormatBytes(t *testing.T) {
	if got := FormatBytes(1024 * 1024); got != "1.00 MiB" {
		t.Fatalf("got %q", got)
	}
}

func TestMaskIPsInTextMasksAllAddressForms(t *testing.T) {
	trace := " 3  202.97.94.1  32.118 ms\n 4  59.43.130.22  35.900 ms\n 5  2001:db8:1234:5678:abcd:ef01:2345:6789  40.000 ms"
	masked := MaskIPsInText(trace)
	if !strings.Contains(masked, "202.97.x.x") || !strings.Contains(masked, "59.43.x.x") ||
		!strings.Contains(masked, "2001:db8:x:x:x:x:x:x") {
		t.Fatalf("masked trace = %q", masked)
	}
	if strings.Contains(masked, "94.1") || strings.Contains(masked, "130.22") {
		t.Fatalf("masked trace still exposes the final two octets: %q", masked)
	}
	// 耗时里的小数不能被误当成 IP。
	if !strings.Contains(masked, "32.118 ms") {
		t.Fatalf("masked trace corrupted timings: %q", masked)
	}
	cidr := MaskIPsInText("prefix 64.23.192.0/19 and 2001:db8:1234:5678::/64")
	if !strings.Contains(cidr, "64.23.x.x/19") || !strings.Contains(cidr, "2001:db8:x:x:x:x:x:x/64") {
		t.Fatalf("masked CIDR = %q", cidr)
	}
	if got := MaskIPsInText(""); got != "" {
		t.Fatalf("empty text = %q", got)
	}
}

func TestRedactedCopyMasksTextBlocksAndTables(t *testing.T) {
	report := Report{SensitiveIPs: []string{"203.0.113.10"}, Results: []Result{{
		ID: "backtrace",
		TextBlocks: []TextBlock{
			{Content: "local 203.0.113.10", Sensitive: true},
			{Content: "hop 59.43.130.22"},
		},
		Tables: []Table{{
			Columns:          []string{"线路", "命中 IP"},
			Rows:             [][]string{{"local", "203.0.113.10"}},
			SensitiveColumns: []int{1},
		}},
	}}}

	redacted := RedactedCopy(report, false)
	if got := redacted.Results[0].TextBlocks[0].Content; got != "local 203.0.x.x" {
		t.Fatalf("sensitive text block = %q", got)
	}
	// 未标记的远端块保持原样，避免误伤路径信息。
	if got := redacted.Results[0].TextBlocks[1].Content; got != "hop 59.43.130.22" {
		t.Fatalf("unmarked text block = %q", got)
	}
	if got := redacted.Results[0].Tables[0].Rows[0][1]; got != "203.0.x.x" {
		t.Fatalf("sensitive column = %q", got)
	}
	if got := redacted.Results[0].Tables[0].Rows[0][0]; got != "local" {
		t.Fatalf("non-sensitive column changed: %q", got)
	}
	if report.Results[0].TextBlocks[0].Content != "local 203.0.113.10" {
		t.Fatal("RedactedCopy mutated the source text block")
	}

	revealed := RedactedCopy(report, true)
	if revealed.Results[0].Tables[0].Rows[0][1] != "203.0.113.10" {
		t.Fatal("--reveal must keep full values")
	}
}

func TestRedactedCopyMasksOnlyExactLocalIPsEverywhere(t *testing.T) {
	report := Report{
		SensitiveIPs: []string{"203.0.113.10", "2001:db8:1234:5678::1"},
		Results: []Result{{
			Summary: "local 203.0.113.10, remote 203.0.113.11",
			TextBlocks: []TextBlock{{
				Content: "src 203.0.113.10:443 via 59.43.130.22 to [2001:db8:1234:5678::1]:8443 and 2001:db8:1234:5678::2",
			}},
		}},
	}

	redacted := RedactedCopy(report, false)
	if got := redacted.Results[0].Summary; got != "local 203.0.x.x, remote 203.0.113.11" {
		t.Fatalf("summary = %q", got)
	}
	want := "src 203.0.x.x:443 via 59.43.130.22 to [2001:db8:x:x:x:x:x:x]:8443 and 2001:db8:1234:5678::2"
	if got := redacted.Results[0].TextBlocks[0].Content; got != want {
		t.Fatalf("text block = %q, want %q", got, want)
	}
	if redacted.SensitiveIPs != nil {
		t.Fatal("internal sensitive IP allow-list must not survive redaction")
	}
}

func TestRedactedCopyMasksExactLocalIPAcrossEntireReportSchema(t *testing.T) {
	const localIP = "203.0.113.77"
	const remoteIP = "203.0.113.78"
	withIPs := func(label string) string { return label + " local=" + localIP + " remote=" + remoteIP }
	report := Report{
		SchemaVersion: withIPs("schema"),
		Tool: ToolInfo{
			Name: withIPs("tool-name"), Version: withIPs("tool-version"),
			Commit: withIPs("commit"), BuildDate: withIPs("build-date"),
		},
		Run: RunInfo{
			ID: withIPs("run-id"), Profile: withIPs("profile"), Exposure: withIPs("exposure"),
			IPVersion: withIPs("family"), Requested: []string{withIPs("requested")},
			OutputFormats: []string{withIPs("format")},
		},
		Summary:      Summary{Headline: withIPs("headline")},
		Notices:      []string{withIPs("notice")},
		SensitiveIPs: []string{localIP},
		Results: []Result{{
			ID: withIPs("result-id"), Title: withIPs("title"), Description: withIPs("description"),
			Summary: withIPs("summary"), Error: withIPs("error"),
			Methodology: Methodology{
				Kind: withIPs("kind"), Label: withIPs("label"), Engine: withIPs("engine"),
				Profile: withIPs("method-profile"), ComparisonScope: withIPs("scope"),
				Parameters: map[string]string{"target": withIPs("parameter")},
			},
			Fields: []Field{{Key: withIPs("field-key"), Label: withIPs("field-label"), Value: withIPs("field-value")}},
			Measurements: []Measurement{{
				Key: withIPs("metric-key"), Label: withIPs("metric-label"), Unit: withIPs("unit"),
				Display: withIPs("display"), Rating: withIPs("rating"), Method: withIPs("method"),
			}},
			Tables: []Table{{
				Title: withIPs("table-title"), Columns: []string{withIPs("column")},
				Rows: [][]string{{withIPs("cell")}},
			}},
			TextBlocks: []TextBlock{{Title: withIPs("block-title"), Language: withIPs("language"), Content: withIPs("content")}},
			Notes:      []string{withIPs("note")},
			Sources:    []Source{{Name: withIPs("source-name"), URL: withIPs("source-url"), Purpose: withIPs("source-purpose")}},
			Failures:   []Failure{{Stage: withIPs("failure-stage"), Target: withIPs("failure-target"), Message: withIPs("failure-message")}},
			Retry: &RetryInfo{
				SelectionRule: withIPs("selection-rule"), TriggerReasons: []string{withIPs("trigger")},
				Attempts: []RetryAttempt{{
					Measurements: []Measurement{{Display: withIPs("retry-measurement")}},
					Interference: Interference{
						Reasons:      []string{withIPs("interference-reason")},
						Measurements: []Measurement{{Display: withIPs("interference-measurement")}},
					},
				}},
			},
		}},
	}

	redacted := RedactedCopy(report, false)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), localIP) {
		t.Fatalf("redacted report still contains exact local IP: %s", encoded)
	}
	if !strings.Contains(string(encoded), "203.0.x.x") || !strings.Contains(string(encoded), remoteIP) {
		t.Fatalf("redaction lost the masked local or untouched remote address: %s", encoded)
	}
	if !redacted.Run.Redacted {
		t.Fatal("the completed redacted copy must declare run.redacted=true")
	}
	if report.Results[0].Failures[0].Message != withIPs("failure-message") || report.Run.Requested[0] != withIPs("requested") {
		t.Fatal("full-schema redaction mutated the source report")
	}

	revealed := RedactedCopy(report, true)
	revealedJSON, err := json.Marshal(revealed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(revealedJSON), localIP) || revealed.Run.Redacted {
		t.Fatalf("--reveal did not retain the exact address and truthful state: %s", revealedJSON)
	}
}
