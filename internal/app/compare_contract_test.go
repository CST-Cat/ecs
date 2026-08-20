package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareHelpCallsFormatAnOutputFormat(t *testing.T) {
	status, stdout, stderr := invokeAppMain("compare", "--lang", "en", "--help")
	if status != 0 || stdout != "" {
		t.Fatalf("compare help status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if !strings.Contains(stderr, "comparison output formats: json, md, html") {
		t.Fatalf("compare help does not distinguish output formats from inputs: %q", stderr)
	}
}

func TestCompareRejectsMarkdownAndHTMLPresentationArtifacts(t *testing.T) {
	root := t.TempDir()
	markdown := filepath.Join(root, "report.md")
	html := filepath.Join(root, "report.html")
	if err := os.WriteFile(markdown, []byte("# ecs report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(html, []byte("<!doctype html><title>ecs report</title>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{markdown, html} {
		t.Run(filepath.Ext(input), func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(
				"compare", input, input, "--lang", "en", "--format", "json", "--output", filepath.Join(root, "out"),
			)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "error:") {
				t.Fatalf("presentation input status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestCompareAcceptsValidJSONWithoutJSONExtension(t *testing.T) {
	root := t.TempDir()
	firstJSON := writeLocalizedObservationInput(t, root, "first", "系统", "系统")
	secondJSON := writeLocalizedObservationInput(t, root, "second", "完成", "完成")
	first := filepath.Join(root, "first.report")
	second := filepath.Join(root, "second.report")
	for source, destination := range map[string]string{firstJSON: first, secondJSON: second} {
		content, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(root, "comparison")
	status, stdout, stderr := invokeAppMain(
		"compare", first, second, "--lang", "en", "--format", "json", "--output", output, "--name", "no-extension", "--no-color",
	)
	if status != 0 || stdout == "" || stderr != "" {
		t.Fatalf("extensionless JSON status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(output, "no-extension.json")); err != nil {
		t.Fatalf("valid ECS JSON with a non-json extension was rejected: %v", err)
	}
}
