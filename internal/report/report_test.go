package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
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
	md := Markdown(data, nil)
	if !strings.Contains(md, "系统 \\| 信息") || !strings.Contains(md, "&lt;safe&gt;") || !strings.Contains(md, "事实采集") {
		t.Fatalf("unexpected markdown:\n%s", md)
	}
	html, err := HTML(data, nil)
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

// Human-readable formats intentionally differ in density, but they must retain
// every structured result category that a reader needs to diagnose a run.  The
// terminal view is compact by design (see text_test.go for its explicit
// omissions); Markdown and HTML are the complete human-facing views.
func TestHumanFormatsCoverStructuredDetailsAndScore(t *testing.T) {
	data := sampleReport()
	data.Run.Exposure = "public"
	data.Run.Accepted = []string{"ookla"}
	data.Run.IPVersion = "4"
	data.Run.Canceled = true
	data.Summary = model.Summary{
		Status: model.StatusWarning, Warnings: 1, Headline: "summary-marker",
	}
	data.Notices = []string{"notice-marker"}
	data.Results = []model.Result{{
		ID: "coverage", Title: "result-title-marker", Description: "description-marker",
		Methodology: model.Methodology{
			Kind: "custom", Label: "method-label-marker", Engine: "method-engine-marker",
			Profile: "method-profile-marker", ComparisonScope: "method-scope-marker",
		},
		Status: model.StatusError, Summary: "result-summary-marker", Error: "error-marker",
		Fields: []model.Field{{Label: "field-label-marker", Value: "field-value-marker"}},
		Measurements: []model.Measurement{{
			Label: "measurement-label-marker", Display: "measurement-display-marker",
			Rating: "measurement-rating-marker", Method: "measurement-method-marker",
		}},
		Tables: []model.Table{{
			Title: "table-title-marker", Columns: []string{"column-marker"},
			Rows: [][]string{{"cell-marker"}},
		}},
		Notes:   []string{"note-marker"},
		Sources: []model.Source{{Name: "source-name-marker", URL: "https://example.com/source", Purpose: "source-purpose-marker"}},
	}}
	scored := &score.Report{
		Total: 123, Ratio: 0.123, Covered: 1, Possible: 2,
		BaselineSource: "score-source-marker", BaselineSample: 3,
		Dimensions: []score.DimensionScore{{Key: "cpu", Score: 123, Ratio: 0.123}},
	}

	markdown := Markdown(data, scored)
	htmlBytes, err := HTML(data, scored)
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	for _, format := range map[string]string{"markdown": markdown, "html": html} {
		for _, marker := range []string{
			"summary-marker", "result-title-marker", "description-marker", "method-label-marker",
			"method-engine-marker", "method-profile-marker", "method-scope-marker", "result-summary-marker",
			"error-marker", "field-label-marker", "field-value-marker", "measurement-label-marker",
			"measurement-display-marker", "measurement-rating-marker", "measurement-method-marker",
			"table-title-marker", "column-marker", "cell-marker", "note-marker", "source-name-marker",
			"source-purpose-marker", "notice-marker", "score-source-marker",
		} {
			if !strings.Contains(format, marker) {
				t.Fatalf("%s format missing %q:\n%s", format, marker, format)
			}
		}
	}

	// JSON is the lossless structured artifact; unlike human renderers it does
	// not embed the optional computed score, which is intentionally derived.
	jsonBytes, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"field-value-marker", "measurement-display-marker", "cell-marker", "note-marker",
		"source-purpose-marker", "error-marker", "summary-marker", "notice-marker",
	} {
		if !strings.Contains(string(jsonBytes), marker) {
			t.Fatalf("JSON format missing %q:\n%s", marker, jsonBytes)
		}
	}
}

func TestRankStatusRendersAcrossHumanFormats(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangZH)
	data := sampleReport()
	scored := &score.Report{
		Total: 1000, Ratio: 1, Covered: 1, Possible: 1,
		BaselineSource: "fleet", BaselineSample: 5,
		RankStatus: score.RankStatusAvailable, TopPercent: 20, RankSamples: 5,
		RankMinSamples: score.DefaultRankMinSamples,
	}
	txt := Text(data, TextOptions{Score: scored})
	md := Markdown(data, scored)
	htmlBytes, err := HTML(data, scored)
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"txt": txt, "md": md, "html": string(htmlBytes)} {
		if !strings.Contains(output, "排行榜前") {
			t.Fatalf("%s output missing available rank:\n%s", name, output)
		}
	}

	scored.RankStatus = score.RankStatusInsufficient
	scored.RankSamples = 3
	scored.BaselineSample = 3
	txt = Text(data, TextOptions{Score: scored})
	md = Markdown(data, scored)
	htmlBytes, err = HTML(data, scored)
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"txt": txt, "md": md, "html": string(htmlBytes)} {
		if !strings.Contains(output, "排行榜样本不足") {
			t.Fatalf("%s output missing sparse rank:\n%s", name, output)
		}
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
