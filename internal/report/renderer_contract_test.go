package report

import (
	"strings"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
)

func TestHTMLRendererEscapesUntrustedReportText(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].Summary = "<script>alert(1)</script>"
	data.Results[0].Sources = append(data.Results[0].Sources, model.Source{Name: "unsafe", URL: "javascript:alert(1)"})

	html, err := HTML(data, rendererScoreFixture())
	if err != nil {
		t.Fatal(err)
	}
	output := string(html)
	if !strings.Contains(output, "<html") || strings.Contains(output, "<script>alert(1)</script>") || strings.Contains(output, "javascript:alert(1)") {
		t.Fatalf("HTML structure or escaping is invalid: %s", output)
	}
	if !strings.Contains(output, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("HTML did not escape untrusted text: %s", output)
	}
	for _, marker := range []string{"Memory Benchmark", "Structured failures", "Standard benchmark", "Composite score", "Skipped", "Interrupted by user", "Not enough leaderboard samples", "The official STREAM executable"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("HTML rich report missing %q: %s", marker, output)
		}
	}
}

func TestMarkdownRendersRichReportAndSafeLinks(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].Summary = "<script>alert(1)</script>"
	data.Results[0].TextBlocks[0].Content = "raw output 192.0.2.10\n``` <payload>unsafe</payload>"
	data.Results[0].Sources = append(data.Results[0].Sources, model.Source{Name: "unsafe", URL: "javascript:alert(1)"})
	fallbackScore := rendererScoreFixture()
	fallbackScore.RankStatus = score.RankStatusUnavailable
	fallbackScore.TierLabel = ""
	output := Markdown(data, fallbackScore)
	for _, marker := range []string{"ecs VPS Benchmark Report", "Memory Benchmark", "Structured failures", "Standard benchmark", "Evidence coverage", "Composite score", "Leaderboard rank unavailable", "all sizes", "raw output 192.0.2.10", "采样", "The official STREAM executable", "```en"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("Markdown missing %q:\n%s", marker, output)
		}
	}
	if strings.Contains(output, "<script>alert(1)</script>") || strings.Contains(output, "javascript:alert(1)") || !strings.Contains(output, "&lt;script&gt;alert\\(1\\)&lt;/script&gt;") {
		t.Fatalf("Markdown safety contract failed:\n%s", output)
	}
	if strings.Count(output, "```") != 2 || !strings.Contains(output, "``\\` <payload>unsafe</payload>") {
		t.Fatalf("Markdown raw output fence was not kept safe:\n%s", output)
	}
	if !strings.Contains(output, "[kernel](https://example.test/source)") || !strings.Contains(output, "unsafe") {
		t.Fatalf("Markdown source links missing or unsafe link not rendered as text:\n%s", output)
	}
}
