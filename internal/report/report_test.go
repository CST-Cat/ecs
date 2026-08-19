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
			Profile:     "standard",
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
	if !strings.Contains(text, "&lt;safe&gt;") || !strings.Contains(text, i18n.T("report.local")) || !strings.Contains(text, "事实采集") {
		t.Fatalf("unexpected html: %s", text)
	}
}

// Human-readable formats intentionally differ in density, but file outputs
// must retain every result category that a reader needs to diagnose a run.  The
// interactive terminal view is compact by design; it is tested separately.
func TestHumanFormatsCoverStructuredDetailsAndScore(t *testing.T) {
	data := sampleReport()
	data.Run.Exposure = "public"
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
		Evidence: model.NewEvidence(3, 4, "sample"),
		Fields:   []model.Field{{Label: "field-label-marker", Value: "field-value-marker"}},
		Measurements: []model.Measurement{{
			Label: "measurement-label-marker", Display: "measurement-display-marker",
			Rating: "measurement-rating-marker", Method: "measurement-method-marker",
		}},
		Tables: []model.Table{{
			Title: "table-title-marker", Columns: []string{"column-marker"},
			Rows: [][]string{{"cell-marker"}},
		}},
		TextBlocks: []model.TextBlock{{Title: "raw-marker", Content: "raw-content-marker"}},
		Notes:      []string{"note-marker"},
		Sources:    []model.Source{{Name: "source-name-marker", URL: "https://example.com/source", Purpose: "source-purpose-marker"}},
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
			"source-purpose-marker", "notice-marker", "score-source-marker", "3/4", "75%",
		} {
			if !strings.Contains(format, marker) {
				t.Fatalf("%s format missing %q:\n%s", format, marker, format)
			}
		}
	}
	if !strings.Contains(html, `Evidence`) && !strings.Contains(html, `证据完整度`) {
		t.Fatalf("HTML lost evidence label: %s", html)
	}
	if !strings.Contains(html, `style="color:var(--warn)`) || !strings.Contains(html, `background:#`) {
		t.Fatalf("HTML evidence lost the shared semantic color scale: %s", html)
	}

	// JSON is the lossless structured artifact; unlike human renderers it does
	// not embed the optional computed score, which is intentionally derived.
	jsonBytes, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"field-value-marker", "measurement-display-marker", "cell-marker", "note-marker",
		"source-purpose-marker", "error-marker", "summary-marker", "notice-marker", `"evidence"`,
	} {
		if !strings.Contains(string(jsonBytes), marker) {
			t.Fatalf("JSON format missing %q:\n%s", marker, jsonBytes)
		}
	}
	txt := Text(data, TextOptions{})
	for _, marker := range []string{
		"method-label-marker", "method-engine-marker", "method-profile-marker", "method-scope-marker",
		"raw-content-marker", "note-marker", "source-name-marker", "source-purpose-marker", "notice-marker", "3/4",
	} {
		if !strings.Contains(txt, marker) {
			t.Fatalf("txt format missing %q:\n%s", marker, txt)
		}
	}
}

func TestEvidenceHTMLLabelColorKeepsSemanticHierarchy(t *testing.T) {
	cases := []struct {
		evidence *model.Evidence
		want     string
	}{
		{model.NewEvidence(1, 1, "run"), "var(--ok)"},
		{model.NewEvidence(1, 2, "run"), "var(--warn)"},
		{model.NewEvidence(0, 2, "run"), "var(--error)"},
		{model.NewEvidence(0, 0, "run"), "var(--skip)"},
	}
	for _, testCase := range cases {
		if got := string(evidenceHTMLLabelColor(testCase.evidence)); got != testCase.want {
			t.Errorf("evidence label color for %+v = %q, want %q", testCase.evidence, got, testCase.want)
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
	written, err := WriteFiles(sampleReport(), directory, "report", []string{"json", "txt", "md", "html"})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "txt", "md", "html"} {
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

func TestWriteFilesKeepsCanonicalJSONAcrossLanguages(t *testing.T) {
	originalLanguage := i18n.Current()
	defer i18n.Set(originalLanguage)

	data := sampleReport()
	data.Results[0].Fields = []model.Field{{Key: "state", Label: "系统", Value: "系统"}}
	data.Results[0].Tables = []model.Table{{
		Title:   "当前值",
		Columns: []string{"当前值", "状态"},
		Rows:    [][]string{{"系统", "完成"}},
	}}

	var canonicalJSON []byte
	var chineseMarkdown, englishMarkdown string
	for _, testCase := range []struct {
		language i18n.Lang
		output   *string
	}{
		{language: i18n.LangZH, output: &chineseMarkdown},
		{language: i18n.LangEN, output: &englishMarkdown},
	} {
		i18n.Set(testCase.language)
		written, err := WriteFiles(data, t.TempDir(), "report", []string{"json", "md"})
		if err != nil {
			t.Fatalf("write %s report: %v", testCase.language, err)
		}
		jsonBytes, err := os.ReadFile(written["json"])
		if err != nil {
			t.Fatalf("read %s JSON: %v", testCase.language, err)
		}
		if canonicalJSON == nil {
			canonicalJSON = jsonBytes
		} else if string(jsonBytes) != string(canonicalJSON) {
			t.Fatalf("JSON changed with language %s:\n%s\n---\n%s", testCase.language, canonicalJSON, jsonBytes)
		}
		markdown, err := os.ReadFile(written["md"])
		if err != nil {
			t.Fatalf("read %s Markdown: %v", testCase.language, err)
		}
		*testCase.output = string(markdown)
	}
	for _, marker := range []string{`"系统"`, `"完成"`} {
		if !strings.Contains(string(canonicalJSON), marker) {
			t.Fatalf("canonical JSON lost machine value %s:\n%s", marker, canonicalJSON)
		}
	}
	if !strings.Contains(chineseMarkdown, "当前值") || !strings.Contains(chineseMarkdown, "完成") {
		t.Fatalf("Chinese Markdown did not keep Chinese display text:\n%s", chineseMarkdown)
	}
	if !strings.Contains(englishMarkdown, "Current value") || !strings.Contains(englishMarkdown, "Done") {
		t.Fatalf("English Markdown did not localize display text:\n%s", englishMarkdown)
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
	// v0 是永远不会被使用的哨兵版本：拒绝用例不能拿一个可能升上来的版本号做
	// 反例，否则下次升级 schema 时这条断言会静默失效。
	if err := os.WriteFile(unknown, []byte(`{"schema_version":"ecs.report/v0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadJSON(unknown); err == nil {
		t.Fatal("expected unsupported schema error")
	}
}

// LoadJSONForComparison 放宽的是版本，不是格式。
//
// 硬拒绝跨 schema 版本会让 schema 每升一次版，用户手里所有旧报告立刻永久
// 不可比；而"比较不同时期的报告"正是 compare 存在的理由。真正保证比较安全
// 的是指标签名，不是版本号（见 compare.Build）。
func TestLoadJSONForComparisonAcceptsOtherSchemaVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.json")
	payload := `{"schema_version":"ecs.report/v2","tool":{"name":"ecs"},"run":{"id":"r1"}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadJSON(path); err == nil {
		t.Fatal("严格加载必须继续拒绝其它 schema 版本")
	}
	loaded, err := LoadJSONForComparison(path)
	if err != nil {
		t.Fatalf("比较路径应接受其它 schema 版本：%v", err)
	}
	if loaded.SchemaVersion != "ecs.report/v2" {
		t.Fatalf("schema 版本必须原样保留：%q", loaded.SchemaVersion)
	}
}

func TestLoadJSONForComparisonRejectsForeignAndMissingSchemas(t *testing.T) {
	directory := t.TempDir()
	for name, payload := range map[string]string{
		"foreign.json": `{"schema_version":"something.else/v1","run":{"id":"r1"}}`,
		"missing.json": `{"run":{"id":"r1"}}`,
		"empty.json":   `{"schema_version":"","run":{"id":"r1"}}`,
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadJSONForComparison(path); err == nil {
			t.Fatalf("%s 必须被拒绝：放宽的是版本，不是格式", name)
		}
	}
}
