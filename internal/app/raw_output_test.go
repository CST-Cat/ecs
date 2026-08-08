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
