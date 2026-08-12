package textwidth

import (
	"strings"
	"testing"
)

// 对齐的根基：中文占两列。按 rune 数或字节数算都会让表格歪掉。
func TestWidthCountsCJKAsTwoColumns(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"abc":     3,
		"中文":      4,
		"中文abc":   7,
		"ｆｕｌｌ":    8, // 全角字母
		"，。":      4, // 全角标点
		"日本語カナ한글": 14,
	}
	for value, want := range cases {
		if got := Width(value); got != want {
			t.Errorf("Width(%q) = %d，期望 %d", value, got, want)
		}
	}
}

func TestZeroWidthCharactersDoNotCount(t *testing.T) {
	if got := Width("a\u200bb"); got != 2 {
		t.Errorf("零宽空格应不占列，得到 %d", got)
	}
	if got := Width("\ufeffabc"); got != 3 {
		t.Errorf("BOM 应不占列，得到 %d", got)
	}
}

func TestPadUsesDisplayWidth(t *testing.T) {
	if got := Pad("中文", 8); Width(got) != 8 {
		t.Errorf("Pad 后宽度 = %d，期望 8", Width(got))
	}
	if got := PadLeft("中文", 8); Width(got) != 8 {
		t.Errorf("PadLeft 后宽度 = %d，期望 8", Width(got))
	}
	// 已经超宽时原样返回：截断会悄悄丢信息，让列歪掉反而看得见。
	if got := Pad("中文中文", 2); got != "中文中文" {
		t.Errorf("超宽时应原样返回，得到 %q", got)
	}
}

func TestCenter(t *testing.T) {
	got := Center("中文", 10)
	if Width(got) != 10 {
		t.Fatalf("居中后宽度 = %d，期望 10", Width(got))
	}
	if got[:3] != "   " {
		t.Errorf("左侧留白不足：%q", got)
	}
}

// 截断要按显示宽度，且不能把一个宽字符切成半个。
func TestTruncateNeverExceedsWidth(t *testing.T) {
	for _, width := range []int{1, 2, 3, 5, 8} {
		got := Truncate("中文混合abc内容", width)
		if Width(got) > width {
			t.Errorf("Truncate 到 %d 列后实际 %d 列：%q", width, Width(got), got)
		}
	}
	if got := Truncate("短", 10); got != "短" {
		t.Errorf("未超宽不该截断，得到 %q", got)
	}
	if got := Truncate("很长的中文内容", 6); !contains(got, "…") {
		t.Errorf("截断应以省略号结尾，得到 %q", got)
	}
}

// 着色后的字符串仍要参与对齐，转义序列本身不占列。
func TestVisibleWidthIgnoresEscapes(t *testing.T) {
	colored := "\x1b[38;2;255;0;0m中文\x1b[0m"
	if got := Width(colored); got != 4 {
		t.Errorf("VisibleWidth = %d，期望 4", got)
	}
	if got := Pad(colored, 10); Width(got) != 10 {
		t.Errorf("Pad 后可见宽度 = %d，期望 10", Width(got))
	}
}

func TestTruncatePreservesANSISequences(t *testing.T) {
	colored := "\x1b[38;2;255;0;0m123456789\x1b[0m"
	got := Truncate(colored, 5)
	if Width(got) > 5 || !contains(got, "…") {
		t.Fatalf("ANSI truncate width/content = %d/%q", Width(got), got)
	}
	if !strings.HasSuffix(got, "\x1b[0m…") {
		t.Fatalf("truncation must close an active style before ellipsis: %q", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
