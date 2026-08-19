package report

import (
	"strings"
	"testing"

	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func TestStructuredFailureRendersInText(t *testing.T) {
	data := sampleReport()
	data.Results[0].Evidence = model.NewEvidence(0, 1, "query")
	data.Results[0].Failures = []model.Failure{{
		Category: model.FailureTimeout, Stage: "fetch", Target: "api.example", Message: "timeout <raw>", Count: 1,
	}}

	output := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, marker := range []string{"api.example", "fetch", "timeout <raw>"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("text failure report missing %q:\n%s", marker, output)
		}
	}
}
