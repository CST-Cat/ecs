package termcolor

import (
	"strings"
	"testing"
)

func TestPaletteWrapRatioRespectsColorCapability(t *testing.T) {
	const text = "score"
	if got := (Palette{Level: LevelNone}).WrapRatio(text, 0.5); got != text {
		t.Fatalf("disabled palette changed text: %q", got)
	}
	got := (Palette{Level: LevelTrueColor}).WrapRatio(text, 0.5)
	if !strings.Contains(got, text) || !strings.Contains(got, "\x1b[") {
		t.Fatalf("enabled palette did not color text: %q", got)
	}
}
