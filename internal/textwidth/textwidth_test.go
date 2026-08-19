package textwidth

import (
	"strings"
	"testing"
)

func TestWidthTruncateAndPadHandleMixedANSIText(t *testing.T) {
	value := "\x1b[38;2;255;0;0m中文abc\x1b[0m"
	if got := Width(value); got != 7 {
		t.Fatalf("mixed display width = %d, want 7", got)
	}
	if got := Width(Pad(value, 10)); got != 10 {
		t.Fatalf("padded display width = %d, want 10", got)
	}
	truncated := Truncate(value, 5)
	if Width(truncated) > 5 || !strings.Contains(truncated, "…") {
		t.Fatalf("truncated display value = %d/%q, want bounded text with ellipsis", Width(truncated), truncated)
	}
}
