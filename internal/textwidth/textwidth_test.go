package textwidth

import (
	"strings"
	"testing"

	"ecs/internal/i18n"
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

func TestECSBilingualReportTextWidthContract(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, test := range []struct {
		language i18n.Lang
		key      string
		text     string
		width    int
	}{
		{language: i18n.LangZH, key: "module.system.title", text: "系统与资源", width: 10},
		{language: i18n.LangEN, key: "module.system.title", text: "System & Resources", width: 18},
		{language: i18n.LangZH, key: "module.memory.title", text: "内存性能", width: 8},
		{language: i18n.LangEN, key: "module.memory.title", text: "Memory Performance", width: 18},
		{language: i18n.LangZH, key: "module.disk.title", text: "磁盘性能", width: 8},
		{language: i18n.LangEN, key: "module.disk.title", text: "Disk Performance", width: 16},
	} {
		t.Run(string(test.language)+"/"+test.key, func(t *testing.T) {
			i18n.Set(test.language)
			value := i18n.T(test.key)
			if value != test.text || Width(value) != test.width {
				t.Fatalf("ECS %s text = %q/%d, want %q/%d", test.key, value, Width(value), test.text, test.width)
			}
			if got := Width(Pad(value, test.width+2)); got != test.width+2 {
				t.Fatalf("padded ECS %s width = %d, want %d", test.key, got, test.width+2)
			}
			truncated := Truncate(value, 7)
			if got := Width(truncated); got > 7 || !strings.HasSuffix(truncated, "…") {
				t.Fatalf("truncated ECS %s = %d/%q", test.key, got, truncated)
			}
		})
	}
}
