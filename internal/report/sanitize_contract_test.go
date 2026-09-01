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
	const payload = "clean\x1b[31mRED\x1b[0m\x07\x08done"
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
		for _, forbidden := range []string{"\x1b", "\x07", "\x08"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("%s output still contains control byte %q", name, forbidden)
			}
		}
		if !strings.Contains(out, "clean") {
			t.Errorf("%s output lost the surrounding text", name)
		}
	}
}
