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

func TestRenderStripsLegacyRawTextBlocks(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "legacy.json")
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Run: model.RunInfo{
			Profile:   "standard",
			StartedAt: time.Unix(0, 0).UTC(),
		},
		Summary: model.Summary{Status: model.StatusOK, Headline: "完成"},
		Results: []model.Result{{
			ID: "legacy", Title: "旧结果", Status: model.StatusOK,
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
		"render", "--input", input, "--format", "json,md,html", "--output", out, "--color", "none",
	}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("render status = %d, stderr = %s", status, stderr.String())
	}
	for _, format := range []string{"json", "md", "html"} {
		path := filepath.Join(out, "legacy."+format)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if strings.Contains(string(data), "SECRET_RAW_TRANSCRIPT") {
			t.Fatalf("%s still contains legacy raw output", format)
		}
	}
	data, err := os.ReadFile(filepath.Join(out, "legacy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "text_blocks") || !strings.Contains(string(data), "structured") {
		t.Fatalf("rendered JSON lost schema compatibility or retained TextBlocks: %s", data)
	}
}
