package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func sampleReport() model.Report {
	start := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test", Commit: "abc"},
		Run: model.RunInfo{
			ID:          "run-1",
			Profile:     "quick",
			StartedAt:   start,
			CompletedAt: start.Add(time.Second),
			DurationMS:  1000,
			Redacted:    true,
		},
		Summary: model.Summary{Status: model.StatusOK, OK: 1, Headline: "1 项测试完成"},
		Results: []model.Result{{
			ID:      "system",
			Title:   "系统 | 信息",
			Status:  model.StatusOK,
			Summary: "<safe>",
			Methodology: model.Methodology{
				Kind:            "inventory",
				Label:           "事实采集",
				Engine:          "OS inspection",
				ComparisonScope: "资源快照；不是基准",
			},
			Fields:       []model.Field{{Key: "os", Label: "系统", Value: "Linux"}},
			Measurements: []model.Measurement{{Key: "cpu", Label: "CPU", Value: 1, Display: "1 point"}},
			Tables:       []model.Table{{Columns: []string{"列"}, Rows: [][]string{{"值"}}}},
		}},
	}
}

func TestMarkdownAndHTML(t *testing.T) {
	data := sampleReport()
	md := Markdown(data)
	if !strings.Contains(md, "系统 \\| 信息") || !strings.Contains(md, "&lt;safe&gt;") || !strings.Contains(md, "事实采集") {
		t.Fatalf("unexpected markdown:\n%s", md)
	}
	html, err := HTML(data)
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	if strings.Contains(text, "<script") {
		t.Fatal("standalone report must not contain scripts")
	}
	if !strings.Contains(text, "&lt;safe&gt;") || !strings.Contains(text, "零自动上传") || !strings.Contains(text, "事实采集") {
		t.Fatalf("unexpected html: %s", text)
	}
}

func TestWriteAndLoadJSON(t *testing.T) {
	directory := t.TempDir()
	written, err := WriteFiles(sampleReport(), directory, "report", []string{"json", "md", "html"})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "md", "html"} {
		path := written[format]
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s missing: %v", format, err)
		}
		if filepath.Dir(path) != directory {
			t.Fatalf("unexpected path %s", path)
		}
	}
	loaded, err := LoadJSON(written["json"])
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Run.ID != "run-1" {
		t.Fatalf("loaded id = %q", loaded.Run.ID)
	}
}

func TestLoadJSONIgnoresUnknownOptionalFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.json")
	content := []byte(`{
	  "schema_version": "ecs.report/v1",
	  "tool": {"name": "ecs", "future_tool_field": true},
	  "run": {"id": "future-run"},
	  "results": [],
	  "summary": {},
	  "notices": [],
	  "future_top_level": {"enabled": true}
	}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Run.ID != "future-run" {
		t.Fatalf("loaded id = %q", loaded.Run.ID)
	}
}

func TestLoadJSONRejectsTrailingDataAndUnknownSchema(t *testing.T) {
	directory := t.TempDir()
	trailing := filepath.Join(directory, "trailing.json")
	if err := os.WriteFile(trailing, []byte(`{"schema_version":"ecs.report/v1"} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadJSON(trailing); err == nil {
		t.Fatal("expected trailing JSON error")
	}
	unknown := filepath.Join(directory, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"schema_version":"ecs.report/v2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadJSON(unknown); err == nil {
		t.Fatal("expected unsupported schema error")
	}
}
