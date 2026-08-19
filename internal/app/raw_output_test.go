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

func TestRenderPreservesCanonicalRawDataAndLocalizesHumanOutput(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "sample.json")
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Run:           model.RunInfo{Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK, Headline: "完成"},
		Results: []model.Result{{
			ID: "sample", Title: "系统", Status: model.StatusOK,
			Fields: []model.Field{{Key: "state", Label: "状态", Value: "系统"}},
			Tables: []model.Table{{
				Key: "state", Title: "当前值", Columns: []string{"状态"},
				ColumnKeys: []string{"status"}, Rows: [][]string{{"完成"}},
			}},
			TextBlocks: []model.TextBlock{{Title: "原始输出", Content: "SECRET_RAW_TRANSCRIPT"}},
		}},
	}
	canonical, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, canonical, 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "out")
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{
		"render", "--lang", "en", "--input", input, "--output", output,
		"--format", "json,txt", "--color", "none",
	}, &stdout, &stderr); status != 0 {
		t.Fatalf("render status = %d, stderr = %s", status, stderr.String())
	}

	jsonData, err := os.ReadFile(filepath.Join(output, "sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(jsonData) != string(canonical) {
		t.Fatalf("render changed canonical JSON:\nwant:\n%s\ngot:\n%s", canonical, jsonData)
	}
	textData, err := os.ReadFile(filepath.Join(output, "sample.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"OS", "Current value", "Done", "SECRET_RAW_TRANSCRIPT"} {
		if !strings.Contains(string(textData), marker) {
			t.Fatalf("English text missing %q:\n%s", marker, textData)
		}
	}
}
