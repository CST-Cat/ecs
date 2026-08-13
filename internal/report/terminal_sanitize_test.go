package report

import (
	"regexp"
	"strings"
	"testing"

	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

var terminalSGRPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestTextSanitizesUntrustedTerminalControlsWithoutMutatingInput(t *testing.T) {
	payload := "before\x1b]52;c;copied\a\x1b[2J\r\n\t\b\u009b31mafter"
	data := textSampleReport()
	data.Summary.Headline = payload
	data.Results[0].Summary = payload
	data.Results[0].Methodology.Parameters = map[string]string{payload: payload}
	data.Results[0].Failures = []model.Failure{{Message: payload}}
	data.Notices = []string{payload}
	scored := &score.Report{BaselineSource: payload}

	plain := Text(data, TextOptions{Color: termcolor.LevelNone, Score: scored})
	assertTerminalOutputSafe(t, plain, false)
	if !strings.Contains(plain, "before") || !strings.Contains(plain, "after") {
		t.Fatalf("sanitization discarded printable diagnostics:\n%s", plain)
	}
	if data.Summary.Headline != payload || scored.BaselineSource != payload {
		t.Fatal("terminal sanitization mutated the caller-owned report or score")
	}

	colored := Text(data, TextOptions{Color: termcolor.LevelTrueColor, Score: scored})
	assertTerminalOutputSafe(t, colored, true)
}

func TestComparisonTextSanitizesUntrustedTerminalControlsWithoutMutatingInput(t *testing.T) {
	payload := "node\x1b]0;forged title\a\x1b[3J\r\nnext\u009b31m"
	data := comparisonReportFixture(t, 2, 0)
	data.Inputs[0].Label = payload
	data.Modules[0].Title = payload
	data.Modules[0].Metrics[0].Parameters = map[string]string{payload: payload}
	data.Notices = []string{payload}

	plain := ComparisonText(data, termcolor.LevelNone)
	assertTerminalOutputSafe(t, plain, false)
	if data.Inputs[0].Label != payload || data.Notices[0] != payload {
		t.Fatal("comparison terminal sanitization mutated the caller-owned model")
	}

	colored := ComparisonText(data, termcolor.LevelBasic)
	assertTerminalOutputSafe(t, colored, true)
}

func TestSanitizeTerminalTextCoversEveryTerminalControlRange(t *testing.T) {
	var input strings.Builder
	for character := rune(0); character <= 0x1f; character++ {
		input.WriteRune(character)
	}
	for character := rune(0x7f); character <= 0x9f; character++ {
		input.WriteRune(character)
	}
	input.WriteString("visible")

	got := sanitizeTerminalText(input.String())
	if got != " visible" {
		t.Fatalf("control ranges were not collapsed safely: %q", got)
	}
}

func assertTerminalOutputSafe(t *testing.T, output string, colored bool) {
	t.Helper()
	checked := output
	if colored {
		if !strings.Contains(output, "\x1b[") {
			t.Fatal("colored fixture did not exercise renderer-generated SGR")
		}
		checked = terminalSGRPattern.ReplaceAllString(output, "")
	}
	for _, character := range checked {
		if character == '\n' {
			continue
		}
		if terminalControlRune(character) {
			t.Fatalf("terminal output retained control U+%04X: %q", character, checked)
		}
	}
}
