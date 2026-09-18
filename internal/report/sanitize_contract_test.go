package report

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

// TestAllRenderersStripControlSequences pins the invariant that no output
// format can carry a terminal escape from report data. Markdown and HTML files
// are routinely read with cat or grep, where a surviving ESC executes exactly
// as it would in the text report.
func TestAllRenderersStripControlSequences(t *testing.T) {
	const payload = "clean\x1b[31mCSI\x1b[0mOSC\x1b]0;title\x07C0\x00\x01\x1fCR\rLF\nTAB\tC1\x80\x9b\x9fdone"
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Results: []model.Result{{
			ID: "system", Title: "module.system.title", Status: model.StatusOK,
			Fields:     []model.Field{{Key: "k", Label: "probe.system.field.os", Value: model.RawValue(payload)}},
			TextBlocks: []model.TextBlock{{Title: "t", Content: payload}},
			Tables: []model.Table{{
				Key: "tb", Columns: []model.TableColumn{{Key: "c", Label: "probe.system.field.os"}},
				Rows: [][]model.Value{{model.RawValue(payload)}}, RowIdentity: "c",
			}},
			Failures: []model.Failure{{Category: model.FailureUnknown, Message: payload, Count: 1}},
		}},
	}
	htmlBytes, err := HTML(data, nil)
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}
	for name, out := range map[string]string{
		"text":     Text(data, TextOptions{Width: 200}),
		"markdown": Markdown(data, nil),
		"html":     string(htmlBytes),
	} {
		sanitized := sanitizeTerminalText(payload)
		if !strings.Contains(out, sanitized) {
			t.Errorf("%s output lost sanitized control-bearing payload %q", name, sanitized)
		}
		for _, forbidden := range []string{
			"CSI\x1b", "OSC\x1b", "C0\x00", "C0\x01", "C0\x1f",
			"CR\r", "LF\nTAB", "TAB\t", "DEL\x7f", "C1\x80", "C1\x9b", "C1\x9f",
		} {
			if strings.Contains(out, forbidden) {
				t.Errorf("%s output retained raw control-bearing fragment %q", name, forbidden)
			}
		}
		if !strings.Contains(out, "clean") {
			t.Errorf("%s output lost the surrounding text", name)
		}
	}
}
