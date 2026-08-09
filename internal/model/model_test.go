package model

import (
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	tests := map[string]string{
		"203.0.113.42":                  "203.0.x.x",
		"2001:db8:1234:5678::1":         "2001:db8:1234:5678:x:x:x:x",
		"2001:db8::1":                   "2001:db8:0:0:x:x:x:x",
		"64.23.192.0/19":                "64.23.x.x/19",
		"2001:db8:1234:5678::/64":       "2001:db8:1234:5678:x:x:x:x/64",
		"203.0.113.42:54321":            "203.0.x.x:54321",
		"[2001:db8:1234:5678::1]:54321": "[2001:db8:1234:5678:x:x:x:x]:54321",
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
			Fields: []Field{{Key: "ip", Value: "203.0.113.10", Sensitive: true}},
			Tables: []Table{{Columns: []string{"a"}, Rows: [][]string{{"b"}}}},
		}},
	}
	copy := RedactedCopy(original, false)
	if copy.Results[0].Fields[0].Value != "203.0.x.x" {
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

func TestMaskIPsInTextMasksAllAddressForms(t *testing.T) {
	trace := " 3  202.97.94.1  32.118 ms\n 4  59.43.130.22  35.900 ms\n 5  2001:db8:1234:5678:abcd:ef01:2345:6789  40.000 ms"
	masked := MaskIPsInText(trace)
	if !strings.Contains(masked, "202.97.x.x") || !strings.Contains(masked, "59.43.x.x") ||
		!strings.Contains(masked, "2001:db8:1234:5678:x:x:x:x") {
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
	if !strings.Contains(cidr, "64.23.x.x/19") || !strings.Contains(cidr, "2001:db8:1234:5678:x:x:x:x/64") {
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
	want := "src 203.0.x.x:443 via 59.43.130.22 to [2001:db8:1234:5678:x:x:x:x]:8443 and 2001:db8:1234:5678::2"
	if got := redacted.Results[0].TextBlocks[0].Content; got != want {
		t.Fatalf("text block = %q, want %q", got, want)
	}
	if redacted.SensitiveIPs != nil {
		t.Fatal("internal sensitive IP allow-list must not survive redaction")
	}
}
