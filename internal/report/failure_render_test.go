package report

import (
	"strings"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func TestStructuredFailuresAndEvidenceGradeRenderInAllFormats(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangZH)
	data := sampleReport()
	data.Results[0].Evidence = model.NewEvidence(0, 3, "query")
	data.Results[0].Failures = []model.Failure{{
		Category: model.FailureTimeout, Stage: "fetch", Target: "api.example", Retryable: true, Count: 2, Message: "timeout <raw>",
	}}
	jsonBytes, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	text := Text(data, TextOptions{})
	markdown := Markdown(data, nil)
	htmlBytes, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	htmlText := string(htmlBytes)

	if !strings.Contains(string(jsonBytes), `"grade": "insufficient"`) || !strings.Contains(string(jsonBytes), `"category": "timeout"`) {
		t.Fatalf("JSON lost stable diagnostic fields: %s", jsonBytes)
	}
	for format, content := range map[string]string{"txt": text, "md": markdown, "html": htmlText} {
		for _, marker := range []string{"api.example", "fetch", "证据不足", "超时"} {
			if !strings.Contains(content, marker) {
				t.Fatalf("%s missing %q:\n%s", format, marker, content)
			}
		}
	}
	if !strings.Contains(htmlText, "timeout &lt;raw&gt;") || strings.Contains(htmlText, "timeout <raw>") {
		t.Fatalf("HTML failure message was not escaped: %s", htmlText)
	}
}

func TestStructuredFailureLabelsTranslateToEnglish(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangEN)
	data := sampleReport()
	data.Results[0].Failures = []model.Failure{{Category: model.FailureDNS, Count: 1}}
	data.Results[0].Evidence = model.NewEvidence(1, 2, "query")
	text := Text(data, TextOptions{})
	if !strings.Contains(text, "DNS error") || !strings.Contains(text, "partial") {
		t.Fatalf("English structured diagnostics missing:\n%s", text)
	}
}
