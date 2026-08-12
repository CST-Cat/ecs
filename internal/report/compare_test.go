package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func comparisonReportFixture(t *testing.T, count int, reference int) comparison.Report {
	t.Helper()
	reports := make([]model.Report, 0, count)
	labels := make([]string, 0, count)
	for index := 0; index < count; index++ {
		report := sampleReport()
		report.Run.ID = "run-" + formatComparisonNumber(float64(index+1))
		report.Results[0].Title = "系统 <比较>"
		report.Results[0].Methodology = model.Methodology{
			Kind: "inventory", Engine: "ecs", Profile: "same-scope",
			Parameters: map[string]string{"scope_revision": "1", "fixture": "same"},
		}
		report.Results[0].Evidence = model.NewEvidence(index+1, count, "sample")
		report.Results[0].Measurements = []model.Measurement{{
			Key: "cpu", Label: "CPU <rate>", Value: float64(100 + index*20), Unit: "points",
			Display: formatComparisonNumber(float64(100+index*20)) + " points", Method: "fixture-v1", HigherIsBetter: model.BoolPtr(true),
		}}
		report.Results[0].Fields = []model.Field{{Key: "zone", Label: "Zone", Value: formatComparisonNumber(float64(index + 1))}}
		reports = append(reports, report)
		labels = append(labels, "node-"+formatComparisonNumber(float64(index+1)))
	}
	data, err := comparison.Build(reports, comparison.Options{Labels: labels, Reference: reference, Tool: model.ToolInfo{Name: "ecs", Version: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestComparisonFormatsPreserveHighlightsBarsAndEvidence(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangZH)
	data := comparisonReportFixture(t, 2, 0)

	jsonBytes, err := ComparisonJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	text := ComparisonText(data, termcolor.LevelNone)
	markdown := ComparisonMarkdown(data)
	htmlBytes, err := ComparisonHTML(data)
	if err != nil {
		t.Fatal(err)
	}
	htmlText := string(htmlBytes)

	for format, content := range map[string]string{
		"json": string(jsonBytes), "txt": text, "md": markdown, "html": htmlText,
	} {
		for _, marker := range []string{"node-1", "node-2", "CPU", "fixture-v1"} {
			if !strings.Contains(content, marker) {
				t.Fatalf("%s missing %q:\n%s", format, marker, content)
			}
		}
	}
	if !strings.Contains(string(jsonBytes), `"schema_version": "ecs.compare/v1"`) || !strings.Contains(string(jsonBytes), `"best": true`) {
		t.Fatalf("comparison JSON lost machine fields: %s", jsonBytes)
	}
	if !strings.Contains(text, "★ 120 points") || !strings.Contains(text, "████") || !strings.Contains(text, "+20%") {
		t.Fatalf("txt lost highlight, bar or delta:\n%s", text)
	}
	if !strings.Contains(markdown, "**★ 120 points**") || !strings.Contains(markdown, "`████") || !strings.Contains(markdown, "1/2") {
		t.Fatalf("Markdown lost highlight, bar or evidence:\n%s", markdown)
	}
	for _, marker := range []string{`class="layout-pair"`, `value-card best`, `background:#`, `@media (max-width:760px)`, "★ 120 points"} {
		if !strings.Contains(htmlText, marker) {
			t.Fatalf("HTML missing %q: %s", marker, htmlText)
		}
	}
	if strings.Contains(htmlText, "<script") || strings.Contains(htmlText, "%!") || !strings.Contains(htmlText, "CPU &lt;rate&gt;") {
		t.Fatalf("HTML is unsafe or malformed: %s", htmlText)
	}
}

func TestComparisonLayoutsAdaptFromMatrixToMany(t *testing.T) {
	matrix := comparisonReportFixture(t, 4, 0)
	many := comparisonReportFixture(t, 7, 0)
	matrixHTML, err := ComparisonHTML(matrix)
	if err != nil {
		t.Fatal(err)
	}
	manyHTML, err := ComparisonHTML(many)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(matrixHTML), `class="layout-matrix"`) {
		t.Fatalf("four reports did not use matrix layout: %s", matrixHTML)
	}
	if !strings.Contains(string(manyHTML), `class="layout-many"`) {
		t.Fatalf("seven reports did not use many layout: %s", manyHTML)
	}
	manyText := ComparisonText(many, termcolor.LevelNone)
	for index := 1; index <= 7; index++ {
		if !strings.Contains(manyText, "node-"+formatComparisonNumber(float64(index))) {
			t.Fatalf("many layout lost node %d:\n%s", index, manyText)
		}
	}
	if !strings.Contains(manyText, i18n.T("compare.rank")) || !strings.Contains(manyText, "#") {
		t.Fatalf("many txt layout is not ranked:\n%s", manyText)
	}
}

func TestComparisonPairUsesNonReferenceDelta(t *testing.T) {
	data := comparisonReportFixture(t, 2, 1)
	text := ComparisonText(data, termcolor.LevelNone)
	if !strings.Contains(text, "-16.67%") || !strings.Contains(text, comparisonInputLabel(data, 1)) {
		t.Fatalf("pair comparison did not use report 2 as reference:\n%s", text)
	}
}

func TestWriteComparisonFilesWritesAllRequestedFormats(t *testing.T) {
	data := comparisonReportFixture(t, 3, 0)
	directory := t.TempDir()
	written, err := WriteComparisonFiles(data, directory, "fleet", []string{"json", "txt", "md", "html"}, ComparisonOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "txt", "md", "html"} {
		path := filepath.Join(directory, "fleet."+format)
		if written[format] != path {
			t.Errorf("%s path = %q, want %q", format, written[format], path)
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() == 0 {
			t.Errorf("%s artifact missing/empty: info=%v err=%v", format, info, statErr)
		}
	}
}

// TestComparisonHTMLEscapesUntrustedFields 守住 compare_html.go 的手工转义。
//
// 该渲染器用 strings.Builder + html.EscapeString 拼 HTML，而不是 html/template
// 的自动转义：一次遗漏就是注入面。报告标签直接来自命令行给出的文件名，
// Linux 下文件名几乎可以是任意字节。
func TestComparisonHTMLEscapesUntrustedFields(t *testing.T) {
	payload := `<script>alert(1)</script>`
	data := comparison.Report{
		SchemaVersion: comparison.SchemaVersion,
		Tool:          model.ToolInfo{Name: payload, Version: payload},
		Reference:     0,
		Inputs: []comparison.Input{
			{Index: 0, Label: payload, ReportID: payload, Profile: payload, ToolVersion: payload},
			{Index: 1, Label: "b", ReportID: "b"},
		},
		Summary: comparison.Summary{Reports: 2, Comparability: comparison.PartiallyComparable},
		Modules: []comparison.Module{{
			ID:            payload,
			Title:         payload,
			Comparability: comparison.PartiallyComparable,
			Statuses: []comparison.StatusValue{
				{Report: 0, Available: true, Status: model.StatusOK},
				{Report: 1, Available: true, Status: model.StatusOK},
			},
			Evidence: []comparison.EvidenceValue{{Report: 0}, {Report: 1}},
			Metrics: []comparison.Metric{{
				Key: payload, Label: payload, Unit: payload, Method: payload,
				ParameterScope: payload, HigherIsBetter: true,
				Values: []comparison.MetricValue{
					{Report: 0, Available: true, Value: 1, Display: payload},
					{Report: 1, Available: true, Value: 2, Display: payload},
				},
			}},
			Changes: []comparison.Observation{{
				Key: payload, Label: payload, Source: payload,
				Values: []comparison.ObservationValue{
					{Report: 0, Available: true, Value: payload},
					{Report: 1, Available: true, Value: payload},
				},
			}},
			MetricIssues: []comparison.MetricIssue{{
				Key: payload, Label: payload, Reason: payload, Reports: []int{0},
			}},
		}},
		Notices: []string{payload},
	}

	rendered, err := ComparisonHTML(data)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	if strings.Contains(text, "<script>alert(1)</script>") {
		t.Fatal("比较报告 HTML 输出了未转义的脚本标签")
	}
	if !strings.Contains(text, "&lt;script&gt;") {
		t.Fatal("比较报告 HTML 没有把不可信文本转义成实体")
	}
	// CSP 是第二道防线，缺了它一次转义遗漏就能直接执行脚本。
	if !strings.Contains(text, "default-src 'none'") {
		t.Fatal("比较报告 HTML 缺少 default-src 'none' 的 CSP")
	}
}
