package report

import (
	"strings"
	"testing"

	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func TestTextSanitizesRepresentativeControlWithoutMutatingInput(t *testing.T) {
	payload := "before\x00\x1b]0;title\x07\n\x1f\x7f\x9bafter"
	data := textSampleReport()
	data.Results[0].Fields[0].Value = model.RawValue(payload)
	keyFieldIndex := len(data.Results[0].Fields)
	data.Results[0].Fields = append(data.Results[0].Fields, model.Field{
		Key: "key-control", Label: "key-control", Value: model.KeyValue(payload),
	})

	output := Text(data, TextOptions{Color: termcolor.LevelNone})
	sanitized := sanitizeTerminalText(payload)
	if strings.Count(output, sanitized) != 2 || strings.Contains(output, payload) {
		t.Fatalf("terminal output was not sanitized safely:\n%s", output)
	}
	if strings.ContainsFunc(output, func(character rune) bool {
		return character != '\n' && terminalControlRune(character)
	}) {
		t.Fatalf("terminal output retained a control character:\n%s", output)
	}
	safe := sanitizedCopy(data)
	if raw, ok := safe.Results[0].Fields[0].Value.Raw(); !ok || raw != sanitized {
		t.Fatalf("sanitized raw value variant = %q, %v; want %q, raw variant", raw, ok, sanitized)
	}
	if key, ok := safe.Results[0].Fields[keyFieldIndex].Value.Key(); !ok || key != sanitized {
		t.Fatalf("sanitized key value variant = %q, %v; want %q, key variant", key, ok, sanitized)
	}
	if data.Results[0].Fields[0].Value.Text() != payload {
		t.Fatal("terminal sanitization mutated the input report")
	}
	if data.Results[0].Fields[keyFieldIndex].Value.Text() != payload {
		t.Fatal("terminal sanitization mutated the key input report")
	}
}
