package termcolor

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestDetectionAndLevelParsing(t *testing.T) {
	envKeys := []string{"NO_COLOR", "CI", "TERM", "COLORTERM"}
	old := make(map[string]string, len(envKeys))
	present := make(map[string]bool, len(envKeys))
	for _, key := range envKeys {
		old[key], present[key] = os.LookupEnv(key)
		os.Unsetenv(key)
	}
	t.Cleanup(func() {
		for _, key := range envKeys {
			if present[key] {
				_ = os.Setenv(key, old[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})

	for _, test := range []struct {
		name, term, colorterm string
		isTTY, ci             bool
		want                  Level
	}{
		{name: "redirected", want: LevelNone},
		{name: "dumb", term: "dumb", isTTY: true, want: LevelNone},
		{name: "truecolor", term: "xterm", colorterm: "truecolor", isTTY: true, want: LevelTrueColor},
		{name: "ansi256", term: "xterm-256color", isTTY: true, want: LevelANSI256},
		{name: "basic", term: "xterm", isTTY: true, want: LevelBasic},
		{name: "ci capture", term: "xterm", ci: true, want: LevelBasic},
	} {
		for _, key := range []string{"CI", "TERM", "COLORTERM"} {
			_ = os.Unsetenv(key)
		}
		if test.ci {
			_ = os.Setenv("CI", "1")
		}
		_ = os.Setenv("TERM", test.term)
		_ = os.Setenv("COLORTERM", test.colorterm)
		if got := Detect(test.isTTY); got != test.want {
			t.Errorf("%s: Detect = %s, want %s", test.name, got, test.want)
		}
	}
	_ = os.Setenv("NO_COLOR", "")
	if got := Detect(true); got != LevelNone {
		t.Fatalf("NO_COLOR did not disable color: %s", got)
	}

	for _, test := range []struct {
		raw        string
		want       Level
		wantString string
	}{
		{raw: "none", want: LevelNone, wantString: "none"},
		{raw: "basic", want: LevelBasic, wantString: "8"},
		{raw: "256", want: LevelANSI256, wantString: "256"},
		{raw: "rgb", want: LevelTrueColor, wantString: "truecolor"},
	} {
		got, ok := ParseLevel(test.raw)
		if !ok || got != test.want || got.String() != test.wantString {
			t.Errorf("ParseLevel(%q) = %s/%v", test.raw, got, ok)
		}
	}
	if _, ok := ParseLevel("terminal-magic"); ok {
		t.Fatal("invalid color level accepted")
	}
}

func TestPaletteBarsAndRelativeScales(t *testing.T) {
	color := RGB{R: 10, G: 80, B: 200}
	for _, test := range []struct {
		level  Level
		marker string
	}{
		{level: LevelNone},
		{level: LevelBasic, marker: "\x1b["},
		{level: LevelANSI256, marker: "\x1b[38;5;"},
		{level: LevelTrueColor, marker: "\x1b[38;2;"},
	} {
		wrapped := (Palette{Level: test.level}).Wrap("x", color)
		if test.level == LevelNone {
			if wrapped != "x" {
				t.Errorf("none palette wrapped text: %q", wrapped)
			}
		} else if !strings.Contains(wrapped, test.marker) || !strings.HasSuffix(wrapped, "\x1b[0m") {
			t.Errorf("%s palette encoding = %q", test.level, wrapped)
		} else if test.level == LevelBasic && (strings.Contains(wrapped, "38;5;") || strings.Contains(wrapped, "38;2;")) {
			t.Errorf("basic palette used a higher color encoding: %q", wrapped)
		}
	}

	if Color(-1) != Color(0) || Color(2) != Color(1) || Color(.5) == Color(0) {
		t.Fatalf("color bounds/interpolation are wrong")
	}
	for _, test := range []struct {
		ratio float64
		want  rune
	}{
		{ratio: .2, want: '░'},
		{ratio: .5, want: '▒'},
		{ratio: .8, want: '▓'},
		{ratio: .9, want: '█'},
	} {
		if got := Density(test.ratio); got != test.want {
			t.Errorf("Density(%v) = %q, want %q", test.ratio, got, test.want)
		}
	}
	palette := Palette{Level: LevelNone}
	for _, test := range []struct {
		ratio float64
		want  string
	}{
		{ratio: 0, want: "····"},
		{ratio: .001, want: "░···"},
		{ratio: .5, want: "▒▒··"},
		{ratio: 2, want: "████▸"},
		{ratio: math.NaN(), want: "····"},
	} {
		got := palette.Bar(test.ratio, 4)
		if got != test.want {
			t.Errorf("Bar(%v) = %q, want %q", test.ratio, got, test.want)
		}
	}
	if palette.Bar(.5, 0) != "" {
		t.Fatal("zero-width bar was not empty")
	}
	if RelativeRatio(5, 0, 10) != .5 || RelativeRatio(0, 1, 10) != 0 || RelativeRatio(math.NaN(), 1, 10) != 0 {
		t.Fatalf("linear/invalid relative ratio failed")
	}
	if mid := RelativeRatio(100, 1, 10000); math.Abs(mid-.5) > 1e-9 {
		t.Fatalf("wide relative range midpoint = %v, want .5", mid)
	}
	if got := palette.BarRelativeRange(5, 0, 10, 4); got == "····" {
		t.Fatal("relative range omitted a positive value")
	}
	if got := palette.BarRelativeRange(1, 0, 0, 4); got != "····" {
		t.Fatalf("invalid relative range = %q", got)
	}
}
