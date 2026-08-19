package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
)

func TestRenderPreservesRawTextBlocksAcrossFormats(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "sample.json")
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Run: model.RunInfo{
			Profile:   "standard",
			StartedAt: time.Unix(0, 0).UTC(),
		},
		Summary: model.Summary{Status: model.StatusOK, Headline: "完成"},
		Results: []model.Result{{
			ID: "sample", Title: "示例结果", Status: model.StatusOK,
			Fields:     []model.Field{{Key: "structured", Value: "kept"}},
			TextBlocks: []model.TextBlock{{Title: "原始工具输出", Content: "SECRET_RAW_TRANSCRIPT"}},
		}},
	}
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, content, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"render", "--input", input, "--output", out, "--color", "none",
	}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("render status = %d, stderr = %s", status, stderr.String())
	}
	for _, format := range []string{"json", "txt", "md", "html"} {
		path := filepath.Join(out, "sample."+format)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if !strings.Contains(string(data), "SECRET_RAW_TRANSCRIPT") {
			t.Fatalf("%s lost raw output", format)
		}
	}
	data, err := os.ReadFile(filepath.Join(out, "sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "text_blocks") || !strings.Contains(string(data), "structured") || !strings.Contains(string(data), "SECRET_RAW_TRANSCRIPT") {
		t.Fatalf("rendered JSON lost structured or raw evidence: %s", data)
	}
}

func TestRenderKeepsCanonicalJSONAndLocalizesHumanFormats(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)

	root := t.TempDir()
	input := filepath.Join(root, "canonical.json")
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Run: model.RunInfo{
			ID: "canonical", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), Redacted: true,
		},
		Summary: model.Summary{Status: model.StatusOK, Headline: "完成"},
		Results: []model.Result{{
			ID: "system", Title: "系统", Status: model.StatusOK,
			Fields: []model.Field{{Key: "state", Label: "状态", Value: "系统"}},
		}},
	}
	canonical, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, canonical, 0o600); err != nil {
		t.Fatal(err)
	}

	englishOutput := filepath.Join(root, "english")
	for _, testCase := range []struct {
		language string
		output   string
		label    string
		value    string
	}{
		{language: "zh", output: filepath.Join(root, "chinese"), label: "状态", value: "系统"},
		{language: "en", output: englishOutput, label: "Status", value: "OS"},
	} {
		var stdout, stderr bytes.Buffer
		status := Main(context.Background(), []string{
			"render", "--lang", testCase.language, "--input", input,
			"--output", testCase.output, "--format", "json,txt,html", "--color", "none",
		}, &stdout, &stderr)
		if status != 0 {
			t.Fatalf("render %s status=%d stdout=%s stderr=%s", testCase.language, status, stdout.String(), stderr.String())
		}
		jsonBytes, err := os.ReadFile(filepath.Join(testCase.output, "canonical.json"))
		if err != nil {
			t.Fatalf("read %s JSON: %v", testCase.language, err)
		}
		if string(jsonBytes) != string(canonical) {
			t.Fatalf("render %s changed canonical JSON:\nwant:\n%s\ngot:\n%s", testCase.language, canonical, jsonBytes)
		}
		for _, format := range []string{"txt", "html"} {
			content, err := os.ReadFile(filepath.Join(testCase.output, "canonical."+format))
			if err != nil {
				t.Fatalf("read %s %s: %v", testCase.language, format, err)
			}
			if !strings.Contains(string(content), testCase.label) || !strings.Contains(string(content), testCase.value) {
				t.Fatalf("render %s %s did not use localized label/value %q/%q:\n%s", testCase.language, format, testCase.label, testCase.value, content)
			}
		}
	}

	// The English render still contains canonical Chinese JSON. Rendering that
	// artifact back to Chinese must therefore work without any reverse
	// translation or state leaking from the previous render.
	chineseAgain := filepath.Join(root, "chinese-again")
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"render", "--lang", "zh", "--input", filepath.Join(englishOutput, "canonical.json"),
		"--output", chineseAgain, "--format", "txt,html", "--color", "none",
	}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("render Chinese after English status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	for _, format := range []string{"txt", "html"} {
		content, err := os.ReadFile(filepath.Join(chineseAgain, "canonical."+format))
		if err != nil {
			t.Fatalf("read Chinese-after-English %s: %v", format, err)
		}
		if !strings.Contains(string(content), "状态") || !strings.Contains(string(content), "系统") {
			t.Fatalf("Chinese-after-English %s lost canonical display text:\n%s", format, content)
		}
	}
}

func TestRunWritesAllDefaultReportFormats(t *testing.T) {
	output := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{
		"run", "--only", "system", "--exposure", "local", "--yes",
		"--output", output, "--name", "default", "--no-color",
	}
	if status := Main(context.Background(), args, &stdout, &stderr); status != 0 {
		t.Fatalf("run status = %d, stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	for _, format := range []string{"json", "txt", "md", "html"} {
		path := filepath.Join(output, "default."+format)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("default run did not write %s: %v\nstdout=%s", format, err, stdout.String())
		}
	}
}
