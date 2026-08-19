package report

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func comparisonReportFixture(t *testing.T, count, reference int) comparison.Report {
	t.Helper()
	reports := make([]model.Report, count)
	labels := make([]string, count)
	for index := range reports {
		data := sampleReport()
		data.Run.ID = "run-" + string(rune('1'+index))
		data.SchemaVersion = "ecs.report/v1"
		if index%2 == 1 {
			data.SchemaVersion = "ecs.report/v2"
		}
		data.Tool.Version = "test-" + string(rune('1'+index%2))
		result := data.Results[0]
		result.Title = "系统"
		result.Status = model.StatusOK
		switch index % 4 {
		case 1:
			result.Status = model.StatusWarning
		case 2:
			result.Status = model.StatusError
		case 3:
			result.Status = model.StatusSkipped
		}
		result.Methodology = model.Methodology{
			Kind: "inventory", Profile: "standard", Parameters: map[string]string{"scope_revision": "1", "workload": "standard"},
		}
		result.Evidence = &model.Evidence{Valid: 1, Expected: 1, Grade: model.EvidenceComplete}
		if index%4 == 1 {
			result.Evidence = &model.Evidence{Valid: 1, Expected: 2, Grade: model.EvidencePartial}
		}
		if index%4 == 2 {
			result.Evidence = &model.Evidence{Valid: 0, Expected: 2, Grade: model.EvidenceInsufficient}
		}
		if index%4 == 3 {
			result.Evidence = &model.Evidence{Valid: 0, Expected: 0, Grade: model.EvidenceNotPlanned}
		}
		result.Measurements = []model.Measurement{
			{Key: "cpu", Label: "CPU", Value: float64(100 + index*20), Unit: "points", Display: fmt.Sprintf("%d points", 100+index*20), Method: "fixture-v1", HigherIsBetter: model.BoolPtr(true)},
			{Key: "latency", Label: "Latency", Value: float64(20 - index), Unit: "ms", Display: fmt.Sprintf("%d ms", 20-index), Method: "fixture-v1", HigherIsBetter: model.BoolPtr(false)},
			{Key: "ratio", Label: "Ratio", Value: 0.125 + float64(index)/1000, Unit: "ratio", Method: "fixture-v1", HigherIsBetter: model.BoolPtr(true)},
		}
		result.Fields = []model.Field{{Key: "state", Label: "状态", Value: map[bool]string{true: "ready", false: "done"}[index == 0]}}
		result.Tables = []model.Table{{
			Key: "system.state", Title: "状态", Columns: []string{"ID", "状态"},
			ColumnKeys: []string{"id", "state"}, RowIdentity: "id", Rows: [][]string{{"row-1", map[bool]string{true: "ready", false: "done"}[index == 0]}},
		}}
		if count >= 6 {
			switch index {
			case count - 2:
				result.Measurements[0].Method = "mismatch-v2"
			case count - 1:
				result.Measurements = result.Measurements[2:]
			}
		}
		if count == 3 && index == count-1 {
			result.Measurements[0].Method = "mismatch-v2"
		}
		data.Results = []model.Result{result}
		if count >= 3 && index == 0 {
			data.Results = append(data.Results, model.Result{ID: "network", Title: "网络", Status: model.StatusOK, Evidence: model.NewEvidence(1, 1, "query")})
		}
		reports[index], labels[index] = data, fmt.Sprintf("node-%d", index+1)
	}
	data, err := comparison.Build(reports, comparison.Options{Labels: labels, Reference: reference, Tool: model.ToolInfo{Name: "ecs", Version: "compare-test"}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sparseComparisonFixture() comparison.Report {
	return comparison.Report{
		SchemaVersion: comparison.SchemaVersion, GeneratedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Reference: 0,
		Inputs:  []comparison.Input{{Index: 0, Label: "empty-1"}, {Index: 1, Label: "empty-2"}},
		Summary: comparison.Summary{Comparability: comparison.NotComparable, Reports: 2, Modules: 1},
		Modules: []comparison.Module{{ID: "empty", Title: "Empty", Comparability: comparison.NotComparable, Statuses: []comparison.StatusValue{{Report: 0}, {Report: 1}}, Evidence: []comparison.EvidenceValue{{Report: 0}, {Report: 1}}}},
		Notices: []string{"compare.notice.scope"},
		Tool:    model.ToolInfo{Name: "ecs", Version: "test"},
	}
}

func TestComparisonRenderersCoverLayoutsAndStates(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	pair := comparisonReportFixture(t, 2, 0)
	payload := "<payload>|[unsafe]\x1b[31m"
	pair.Inputs[0].Label = payload
	pair.Modules[0].Metrics[0].Label = payload
	pair.Notices = append(pair.Notices, payload)
	pairBefore, err := ComparisonJSON(pair)
	if err != nil {
		t.Fatal(err)
	}
	textPair := ComparisonText(pair, termcolor.LevelNone)
	if strings.Contains(textPair, "\x1b") || !strings.Contains(textPair, "<payload>") || !strings.Contains(textPair, "vs reference") || !strings.Contains(textPair, "node-2") {
		t.Fatalf("pair comparison text omitted basic metric/change:\n%s", textPair)
	}
	markdownPair := ComparisonMarkdown(pair)
	if !strings.Contains(markdownPair, "120 points") || !strings.Contains(markdownPair, "done") || !strings.Contains(markdownPair, "ecs multi-report comparison") || !strings.Contains(markdownPair, "&lt;payload&gt;") || !strings.Contains(markdownPair, "\\|") || strings.Contains(markdownPair, "<payload>") {
		t.Fatalf("pair comparison Markdown omitted changed values:\n%s", markdownPair)
	}
	htmlPair, err := ComparisonHTML(pair)
	if err != nil || !strings.Contains(string(htmlPair), "layout-pair") || !strings.Contains(string(htmlPair), "metric-card") {
		t.Fatalf("pair comparison HTML = %v:\n%s", err, htmlPair)
	}
	if strings.Contains(string(htmlPair), "<payload>") || !strings.Contains(string(htmlPair), "&lt;payload&gt;") {
		t.Fatalf("pair comparison HTML did not escape payload:\n%s", htmlPair)
	}
	pairAfter, err := ComparisonJSON(pair)
	if err != nil || !bytes.Equal(pairAfter, pairBefore) {
		t.Fatalf("comparison renderers mutated input: %v", err)
	}

	for _, count := range []int{3, 6} {
		data := comparisonReportFixture(t, count, count-1)
		text := ComparisonText(data, termcolor.LevelNone)
		if !strings.Contains(text, "Method issues") || !strings.Contains(text, "What differs") || !strings.Contains(text, "Input reports") || !strings.Contains(text, "Input reports declare different schema versions") || (count == 6 && !strings.Contains(text, "no reference")) {
			t.Fatalf("rich comparison text (%d) omitted diagnostic state:\n%s", count, text)
		}
		if count == 6 {
			for _, marker := range []string{"OK", "Attention", "Error", "Skipped", "complete", "partial", "insufficient evidence", "no samples planned", "Regressed", "no reference", "0.125 ratio"} {
				if !strings.Contains(text, marker) {
					t.Fatalf("many comparison text omitted %q:\n%s", marker, text)
				}
			}
		}
		markdown := ComparisonMarkdown(data)
		if !strings.Contains(markdown, "Method issues") || !strings.Contains(markdown, "What differs") || !strings.Contains(markdown, "Input reports declare different schema versions") || (count == 6 && !strings.Contains(markdown, "no reference")) {
			t.Fatalf("rich comparison Markdown (%d) omitted diagnostic state:\n%s", count, markdown)
		}
		html, err := ComparisonHTML(data)
		if err != nil {
			t.Fatal(err)
		}
		layout := "layout-matrix"
		if count == 6 {
			layout = "layout-many"
		}
		if !strings.Contains(string(html), layout) || !strings.Contains(string(html), "Method issues") || !strings.Contains(string(html), "Input reports declare different schema versions") {
			t.Fatalf("rich comparison HTML (%d) omitted layout/diagnostic state", count)
		}
	}
	sparse := sparseComparisonFixture()
	if output := ComparisonText(sparse, termcolor.LevelNone); !strings.Contains(output, "not comparable") || !strings.Contains(output, "No differences need to be shown") {
		t.Fatalf("sparse comparison text omitted not-comparable state:\n%s", output)
	}
	if output := ComparisonMarkdown(sparse); !strings.Contains(output, "not comparable") || !strings.Contains(output, "No differences need to be shown") {
		t.Fatalf("sparse comparison Markdown omitted not-comparable state:\n%s", output)
	}
	if output, err := ComparisonHTML(sparse); err != nil || !strings.Contains(string(output), "not comparable") || !strings.Contains(string(output), "No differences need to be shown") {
		t.Fatalf("sparse comparison HTML omitted not-comparable state: %v\n%s", err, output)
	}
}

func TestWriteComparisonFilesAndCanonicalErrors(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := comparisonReportFixture(t, 2, 0)
	canonical, err := ComparisonJSON(data)
	if err != nil || len(canonical) == 0 || canonical[len(canonical)-1] != '\n' {
		t.Fatalf("comparison JSON = %v", err)
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		localizedJSON, jsonErr := ComparisonJSON(data)
		if jsonErr != nil || !bytes.Equal(localizedJSON, canonical) {
			t.Fatalf("comparison JSON changed for %s: %v", language, jsonErr)
		}
	}
	i18n.Set(i18n.LangEN)
	directory := t.TempDir()
	written, err := WriteComparisonFiles(data, directory, "compare unsafe", []string{"json", "txt", "md", "html"}, ComparisonOptions{TextColor: termcolor.LevelNone})
	if err != nil || len(written) != 4 {
		t.Fatalf("comparison files = %v, %v", written, err)
	}
	for _, format := range []string{"json", "txt", "md", "html"} {
		path := written[format]
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 || filepath.Base(path) != "compare-unsafe."+format {
			t.Fatalf("comparison %s path/mode = %q/%v", format, path, info.Mode().Perm())
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if format == "json" && !bytes.Equal(content, canonical) {
			t.Fatal("comparison JSON changed during file writing")
		}
		if format != "json" && !strings.Contains(string(content), "CPU") {
			t.Fatalf("comparison %s omitted metric", format)
		}
	}
	defaultWritten, err := WriteComparisonFiles(data, t.TempDir(), "", []string{"json"}, ComparisonOptions{})
	if err != nil || !strings.HasPrefix(filepath.Base(defaultWritten["json"]), "ecs-compare-") {
		t.Fatalf("comparison default filename = %v, %v", defaultWritten, err)
	}
	workingDirectory := t.TempDir()
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })
	currentDirectoryWritten, err := WriteComparisonFiles(data, "", "", []string{"json"}, ComparisonOptions{})
	if err != nil || filepath.Dir(currentDirectoryWritten["json"]) != filepath.Join(workingDirectory, "reports") {
		t.Fatalf("comparison empty-directory output = %v, %v", currentDirectoryWritten, err)
	}

	partial, err := WriteComparisonFiles(data, t.TempDir(), "partial", []string{"json", "unknown"}, ComparisonOptions{})
	if err == nil || !strings.Contains(err.Error(), "unknown report format") || len(partial) != 1 {
		t.Fatalf("comparison partial unknown-format result = %v, %v", partial, err)
	}
	invalid := comparisonReportFixture(t, 2, 0)
	invalid.Modules[0].Metrics[0].Values[0].Value = math.NaN()
	if written, err := WriteComparisonFiles(invalid, t.TempDir(), "invalid", []string{"json"}, ComparisonOptions{}); err == nil || !strings.Contains(err.Error(), "generate json report") || len(written) != 0 {
		t.Fatalf("comparison JSON generation failure = %v, %v", written, err)
	}
	parentFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteComparisonFiles(data, filepath.Join(parentFile, "child"), "x", []string{"json"}, ComparisonOptions{}); err == nil || !strings.Contains(err.Error(), "create output directory") {
		t.Fatalf("comparison mkdir failure = %v", err)
	}
	atomicDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(atomicDirectory, "atomic.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if written, err := WriteComparisonFiles(data, atomicDirectory, "atomic", []string{"json"}, ComparisonOptions{}); err == nil || !strings.Contains(err.Error(), "write json report") || len(written) != 0 {
		t.Fatalf("comparison atomic failure = %v, %v", written, err)
	}
}
