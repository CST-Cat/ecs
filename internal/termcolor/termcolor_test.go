package termcolor

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// 手工核对色阶在各档能力下的呈现：go test -run TestVisualRamp -v ./internal/termcolor/
func TestVisualRamp(t *testing.T) {
	if testing.Short() {
		t.Skip("视觉核对用，-short 时跳过")
	}
	ratios := []float64{0.05, 0.2, 0.35, 0.5, 0.65, 0.8, 0.95, 1.0, 1.6}
	for _, level := range []Level{LevelTrueColor, LevelANSI256, LevelBasic, LevelNone} {
		p := Palette{Level: level}
		fmt.Printf("\n=== %s ===\n", level)
		for _, r := range ratios {
			fmt.Printf("  %5.0f%%  %s  %s\n", r*100, p.Bar(r, 28), p.WrapRatio(fmt.Sprintf("%4.0f", r*1000), r))
		}
	}
}

// 层次不能只靠颜色：无色档必须仍然区分得出高低，否则单色终端、纯文本文件和
// 被重定向的输出里柱状图就退化成一堆等长方块。
func TestDensityCarriesRankWithoutColor(t *testing.T) {
	p := Palette{Level: LevelNone}
	var previous int
	for index, ratio := range []float64{0.1, 0.4, 0.7, 0.95} {
		bar := p.Bar(ratio, 20)
		if strings.Contains(bar, "\x1b") {
			t.Fatalf("无色档不该输出转义序列：%q", bar)
		}
		filled := strings.Count(bar, string(Density(ratio)))
		if index > 0 && filled <= previous {
			t.Fatalf("柱长未随比例递增：ratio=%v filled=%d previous=%d", ratio, filled, previous)
		}
		previous = filled
	}
	// 四档密度字符必须两两不同，否则无色时层次消失。
	seen := map[rune]bool{}
	for _, ratio := range []float64{0.1, 0.45, 0.7, 0.95} {
		seen[Density(ratio)] = true
	}
	if len(seen) != 4 {
		t.Fatalf("密度字符没有覆盖四档：%v", seen)
	}
}

// 柱长必须真的随数值变化——参考实现里 183ms 与 113ms 画成一样长的柱子，
// 那是把装饰当信息。
func TestBarLengthTracksRatio(t *testing.T) {
	p := Palette{Level: LevelNone}
	if got := p.Bar(0.5, 20); strings.Count(got, "▒") != 10 {
		t.Fatalf("50%% 应填满一半：%q", got)
	}
	if got := p.Bar(1, 20); strings.Count(got, "█") != 20 {
		t.Fatalf("100%% 应填满：%q", got)
	}
	// 非零但极小的值也要看得见，否则 0.4% 与 0 无法区分。
	if got := p.Bar(0.004, 20); !strings.ContainsRune(got, '░') {
		t.Fatalf("极小值应至少占一格：%q", got)
	}
	if got := p.Bar(0, 20); strings.ContainsAny(got, "░▒▓█") {
		t.Fatalf("零值不该有填充：%q", got)
	}
	// 超过基线不截断，用尾标表示，否则"两倍达标"和"刚好达标"看起来一样。
	if got := p.Bar(1.6, 20); !strings.ContainsRune(got, '▸') {
		t.Fatalf("超出基线应有尾标：%q", got)
	}
}

func TestBarRelativeHandlesEmptyGroup(t *testing.T) {
	p := Palette{Level: LevelNone}
	if got := p.BarRelative(5, 0, 10); strings.ContainsAny(got, "░▒▓█") {
		t.Fatalf("组内最大值为 0 时不该有填充：%q", got)
	}
}

// 各档位都必须发出自己那一档的序列，不能悄悄用上终端读不懂的格式。
func TestEscapeMatchesLevel(t *testing.T) {
	cases := map[Level]string{
		LevelTrueColor: "\x1b[38;2;",
		LevelANSI256:   "\x1b[38;5;",
	}
	for level, prefix := range cases {
		got := Palette{Level: level}.WrapRatio("x", 0.5)
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("%v 档应以 %q 开头：%q", level, prefix, got)
		}
	}
	if got := (Palette{Level: LevelBasic}).WrapRatio("x", 0.5); !strings.HasPrefix(got, "\x1b[") ||
		strings.Contains(got, "38;") {
		t.Fatalf("8 色档不该使用扩展色序列：%q", got)
	}
	if got := (Palette{Level: LevelNone}).WrapRatio("x", 0.5); got != "x" {
		t.Fatalf("无色档应原样返回：%q", got)
	}
}

func TestSemanticTonesRespectPaletteLevels(t *testing.T) {
	tones := []func(Palette, string) string{
		func(p Palette, text string) string { return p.Accent(text) },
		func(p Palette, text string) string { return p.Info(text) },
		func(p Palette, text string) string { return p.Success(text) },
		func(p Palette, text string) string { return p.Warning(text) },
		func(p Palette, text string) string { return p.Error(text) },
		func(p Palette, text string) string { return p.Label(text) },
	}
	for _, level := range []Level{LevelNone, LevelBasic, LevelANSI256, LevelTrueColor} {
		p := Palette{Level: level}
		for _, apply := range tones {
			got := apply(p, "semantic")
			if level == LevelNone {
				if got != "semantic" {
					t.Fatalf("none level changed semantic text: %q", got)
				}
				continue
			}
			if !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, "\x1b[0m") {
				t.Fatalf("level %v semantic tone should wrap with ANSI: %q", level, got)
			}
		}
		bold := p.AccentBold("heading")
		if level == LevelNone {
			if bold != "heading" {
				t.Fatalf("none level changed bold semantic text: %q", bold)
			}
		} else if !strings.HasPrefix(bold, "\x1b[1;") || !strings.HasSuffix(bold, "\x1b[0m") {
			t.Fatalf("level %v semantic bold tone should combine style: %q", level, bold)
		}
	}
}

func TestSemanticToneDoesNotNestExistingANSI(t *testing.T) {
	p := Palette{Level: LevelTrueColor}
	styled := p.Success("already styled")
	if got := p.Warning(styled); got != styled {
		t.Fatalf("semantic tone nested an existing ANSI value: %q", got)
	}
}

// 8 色档只有几个色相，若低比例与高比例落到同一个色加同一个亮度位，
// 这一档就只剩柱长可读了。
func TestBasicPaletteSeparatesLowAndHigh(t *testing.T) {
	p := Palette{Level: LevelBasic}
	low, high := p.WrapRatio("x", 0.05), p.WrapRatio("x", 1.0)
	if low == high {
		t.Fatalf("8 色档下最低与最高比例的着色相同：%q", low)
	}
	// 低比例不应加粗：亮度本身要参与表达层次。
	if strings.HasPrefix(low, "\x1b[1;") {
		t.Fatalf("最低比例不该用亮色：%q", low)
	}
	if !strings.HasPrefix(high, "\x1b[1;") {
		t.Fatalf("最高比例应用亮色：%q", high)
	}
}

func TestDetectRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if got := Detect(true); got != LevelNone {
		t.Fatalf("NO_COLOR 被设置时应关闭颜色，得到 %v", got)
	}
}

func TestDetectLevels(t *testing.T) {
	cases := []struct {
		term, colorTerm string
		want            Level
	}{
		{"xterm-256color", "", LevelANSI256},
		{"xterm-256color", "truecolor", LevelTrueColor},
		{"xterm-ghostty", "", LevelTrueColor},
		{"xterm", "", LevelBasic},
		{"dumb", "truecolor", LevelNone},
		{"screen", "", LevelBasic},
	}
	for _, item := range cases {
		os.Unsetenv("NO_COLOR")
		t.Setenv("TERM", item.term)
		t.Setenv("COLORTERM", item.colorTerm)
		if got := Detect(true); got != item.want {
			t.Errorf("TERM=%q COLORTERM=%q -> %v，期望 %v", item.term, item.colorTerm, got, item.want)
		}
	}
}

// 非 TTY 默认不着色：报告被重定向进文件或管道时，转义序列会变成可见垃圾。
func TestDetectSkipsNonTTY(t *testing.T) {
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("CI")
	t.Setenv("TERM", "xterm-256color")
	if got := Detect(false); got != LevelNone {
		t.Fatalf("非 TTY 应关闭颜色，得到 %v", got)
	}
}

func TestParseLevelRoundTrip(t *testing.T) {
	for _, name := range LevelNames() {
		level, ok := ParseLevel(name)
		if !ok {
			t.Fatalf("ParseLevel(%q) 失败", name)
		}
		if level.String() != name && !(name == "basic" && level == LevelBasic) {
			t.Fatalf("%q -> %v -> %q", name, level, level.String())
		}
	}
	if _, ok := ParseLevel("rainbow"); ok {
		t.Fatal("未知档位应被拒绝")
	}
}
