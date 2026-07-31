package model

import (
	"strings"
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

func TestMaskIPsInTextKeepsRoutePrefixes(t *testing.T) {
	// 线路特征必须保留：59.43 段是判定 CN2 的依据，整段抹掉会让复核失去意义。
	trace := " 3  202.97.94.1  32.118 ms\n 4  59.43.130.22  35.900 ms"
	masked := MaskIPsInText(trace)
	if !strings.Contains(masked, "202.97.94.x") || !strings.Contains(masked, "59.43.130.x") {
		t.Fatalf("masked trace = %q", masked)
	}
	if strings.Contains(masked, "130.22") {
		t.Fatalf("masked trace still exposes the final octet: %q", masked)
	}
	// 耗时里的小数不能被误当成 IP。
	if !strings.Contains(masked, "32.118 ms") {
		t.Fatalf("masked trace corrupted timings: %q", masked)
	}
	if got := MaskIPsInText(""); got != "" {
		t.Fatalf("empty text = %q", got)
	}
}

func TestRedactedCopyMasksTextBlocksAndTables(t *testing.T) {
	report := Report{Results: []Result{{
		ID: "backtrace",
		TextBlocks: []TextBlock{
			{Content: "hop 59.43.130.22", Sensitive: true},
			{Content: "hop 59.43.130.22"},
		},
		Tables: []Table{{
			Columns:          []string{"线路", "命中 IP"},
			Rows:             [][]string{{"CN2", "59.43.130.22"}},
			SensitiveColumns: []int{1},
		}},
	}}}

	redacted := RedactedCopy(report, false)
	if got := redacted.Results[0].TextBlocks[0].Content; got != "hop 59.43.130.x" {
		t.Fatalf("sensitive text block = %q", got)
	}
	// 未标记的块保持原样，避免误伤非路由输出。
	if got := redacted.Results[0].TextBlocks[1].Content; got != "hop 59.43.130.22" {
		t.Fatalf("unmarked text block = %q", got)
	}
	if got := redacted.Results[0].Tables[0].Rows[0][1]; got != "59.43.130.x" {
		t.Fatalf("sensitive column = %q", got)
	}
	if got := redacted.Results[0].Tables[0].Rows[0][0]; got != "CN2" {
		t.Fatalf("non-sensitive column changed: %q", got)
	}
	if report.Results[0].TextBlocks[0].Content != "hop 59.43.130.22" {
		t.Fatal("RedactedCopy mutated the source text block")
	}

	revealed := RedactedCopy(report, true)
	if revealed.Results[0].Tables[0].Rows[0][1] != "59.43.130.22" {
		t.Fatal("--reveal must keep full values")
	}
}
