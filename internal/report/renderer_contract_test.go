package report

import (
	"strings"
	"testing"
)

func TestHTMLRendererEscapesUntrustedReportText(t *testing.T) {
	data := sampleReport()
	data.Results[0].Summary = "<script>alert(1)</script>"

	html, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	output := string(html)
	if !strings.Contains(output, "<html") || strings.Contains(output, "<script>alert(1)</script>") {
		t.Fatalf("HTML structure or escaping is invalid: %s", output)
	}
	if !strings.Contains(output, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("HTML did not escape untrusted text: %s", output)
	}
}
