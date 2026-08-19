package report

import (
	"strings"
	"testing"

	"ecs/internal/termcolor"
)

func TestTextSanitizesRepresentativeControlWithoutMutatingInput(t *testing.T) {
	payload := "before\x00\x1b[2J\x7f\x9b31mafter"
	data := textSampleReport()
	data.Results[0].Fields[0].Value = payload

	output := Text(data, TextOptions{Color: termcolor.LevelNone})
	if strings.Contains(output, "\x1b") || !strings.Contains(output, "before") || !strings.Contains(output, "after") {
		t.Fatalf("terminal output was not sanitized safely:\n%s", output)
	}
	if data.Results[0].Fields[0].Value != payload {
		t.Fatal("terminal sanitization mutated the input report")
	}
}
