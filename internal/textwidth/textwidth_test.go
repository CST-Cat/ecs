package textwidth

import (
	"strings"
	"testing"
)

func TestDisplayWidthAlignmentAndSafeTruncation(t *testing.T) {
	for _, test := range []struct {
		name, value string
		want        int
	}{
		{name: "ascii", value: "abc", want: 3},
		{name: "cjk", value: "中a", want: 3},
		{name: "ansi", value: "\x1b[31m中文\x1b[0m", want: 4},
		{name: "combining", value: "e\u0301\u200b", want: 1},
	} {
		if got := Width(test.value); got != test.want {
			t.Errorf("%s width = %d, want %d", test.name, got, test.want)
		}
	}
	if got := Pad("中", 4); got != "中  " {
		t.Fatalf("right padding = %q, want %q", got, "中  ")
	}
	if got := PadLeft("中", 4); got != "  中" {
		t.Fatalf("left padding = %q, want %q", got, "  中")
	}
	if got := Center("中", 5); got != " 中  " {
		t.Fatalf("center padding = %q, want %q", got, " 中  ")
	}
	if Pad("long", 2) != "long" || Truncate("short", 8) != "short" {
		t.Fatal("already-fitting values were changed")
	}
	if Truncate("text", 0) != "" || Truncate("text", -1) != "" {
		t.Fatal("non-positive truncation width was not empty")
	}

	value := "\x1b[38;2;255;0;0m中文abc\x1b[0m"
	truncated := Truncate(value, 5)
	if Width(truncated) > 5 || !strings.HasSuffix(truncated, "…") || !strings.Contains(truncated, "\x1b[0m") {
		t.Fatalf("ANSI truncation = %d/%q", Width(truncated), truncated)
	}
	if got := Truncate("中文", 2); Width(got) > 2 || !strings.HasSuffix(got, "…") {
		t.Fatalf("wide-character boundary truncation = %d/%q", Width(got), got)
	}
}
