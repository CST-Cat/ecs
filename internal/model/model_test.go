package model

import (
	"testing"
)

func TestMask(t *testing.T) {
	tests := map[string]string{
		"203.0.113.42":          "203.0.113.x",
		"2001:db8:1234:5678::1": "2001:db8:1234::/48",
		"2001:db8::1":           "2001:db8:0::/48",
		"server-01":             "hidden",
		"":                      "",
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
			Fields: []Field{{Key: "ip", Value: "203.0.113.10", Sensitive: true}},
			Tables: []Table{{Columns: []string{"a"}, Rows: [][]string{{"b"}}}},
		}},
	}
	copy := RedactedCopy(original, false)
	if copy.Results[0].Fields[0].Value != "203.0.113.x" {
		t.Fatalf("redacted = %q", copy.Results[0].Fields[0].Value)
	}
	if original.Results[0].Fields[0].Value != "203.0.113.10" {
		t.Fatal("source report was mutated")
	}
	copy.Results[0].Tables[0].Rows[0][0] = "changed"
	if original.Results[0].Tables[0].Rows[0][0] != "b" {
		t.Fatal("nested table was not deep copied")
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
