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
	safe := sanitizedReportCopy(data)
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

func TestSanitizedReportCopyPreservesMapKeyIdentityAndMemberCount(t *testing.T) {
	const (
		controlKey = "same\x1b"
		plainKey   = "same "
	)
	if sanitized := sanitizeTerminalText(controlKey); sanitized != plainKey {
		t.Fatalf("fixture does not collide after sanitization: %q != %q", sanitized, plainKey)
	}

	data := model.Report{Results: []model.Result{{Methodology: model.Methodology{
		Parameters: map[string]string{
			controlKey: "value from control-key member",
			plainKey:   "value from plain-key member",
		},
	}}}}
	originalParameters := data.Results[0].Methodology.Parameters
	safe := sanitizedReportCopy(data)
	parameters := safe.Results[0].Methodology.Parameters

	if got, want := len(parameters), len(originalParameters); got != want {
		t.Fatalf("sanitized map member count = %d, want %d", got, want)
	}
	for key, want := range originalParameters {
		if got, ok := parameters[key]; !ok || got != want {
			t.Fatalf("sanitized map member %q = %q, %v; want %q, true", key, got, ok, want)
		}
	}
	if _, ok := parameters[controlKey]; !ok {
		t.Fatalf("control-bearing canonical map key was rewritten or lost: %#v", parameters)
	}
	if _, ok := parameters[plainKey]; !ok {
		t.Fatalf("plain canonical map key was rewritten or lost: %#v", parameters)
	}

	if got, want := len(originalParameters), 2; got != want {
		t.Fatalf("sanitization mutated original map member count = %d, want %d", got, want)
	}
	if originalParameters[controlKey] != "value from control-key member" || originalParameters[plainKey] != "value from plain-key member" {
		t.Fatalf("sanitization mutated original map identity or values: %#v", originalParameters)
	}
}
