package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
)

func writeComparisonInput(t *testing.T, directory, name string, value float64) string {
	t.Helper()
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run: model.RunInfo{
			ID: name, Profile: "standard", StartedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Redacted: true,
		},
		Results: []model.Result{{
			ID: "cpu", Title: "CPU 性能", Status: model.StatusOK,
			Methodology: model.Methodology{
				Kind: "standard-benchmark", Engine: "sysbench", Profile: "prime=20000",
				Parameters: map[string]string{"scope_revision": "1", "prime": "20000", "duration": "15s"},
			},
			Evidence: model.NewEvidence(1, 1, "run"),
			Measurements: []model.Measurement{{
				Key: "rate", Label: "单线程事件率", Value: value, Unit: "events/s", Display: model.FormatRate(value, "events/s"),
				Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
			}},
		}},
	}
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name+".json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompareCommandAcceptsTwoPathsThenFlagsAndWritesFourFormats(t *testing.T) {
	inputs := t.TempDir()
	oldReport := writeComparisonInput(t, inputs, "old", 100)
	newReport := writeComparisonInput(t, inputs, "new", 125)
	output := t.TempDir()
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"compare", oldReport, newReport,
		"--format", "json,txt,md,html", "--output", output, "--name", "fleet", "--color", "none",
	}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("compare status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	for _, format := range []string{"json", "txt", "md", "html"} {
		path := filepath.Join(output, "fleet."+format)
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Errorf("missing %s report: info=%v err=%v", format, info, err)
		}
	}
	if !strings.Contains(stdout.String(), "ecs 多报告对比") || !strings.Contains(stdout.String(), "★ 125") || !strings.Contains(stdout.String(), filepath.Join(output, "fleet.html")) {
		t.Fatalf("compare terminal output lost report/highlight/paths:\n%s", stdout.String())
	}
	htmlBytes, err := os.ReadFile(filepath.Join(output, "fleet.html"))
	if err != nil || !strings.Contains(string(htmlBytes), `class="layout-pair"`) {
		t.Fatalf("pair HTML missing adaptive class: err=%v html=%s", err, htmlBytes)
	}
}

func TestCompareCommandSupportsManyReportsAndSelectedReference(t *testing.T) {
	inputs := t.TempDir()
	args := []string{"compare"}
	for index := 0; index < 7; index++ {
		args = append(args, writeComparisonInput(t, inputs, "node-"+string(rune('a'+index)), float64(100+index)))
	}
	output := t.TempDir()
	args = append(args, "--format", "json", "--output", output, "--name", "many", "--reference", "4", "--no-color")
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), args, &stdout, &stderr); status != 0 {
		t.Fatalf("many compare failed: status=%d stderr=%s", status, stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(output, "many.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data comparison.Report
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Inputs) != 7 || data.Reference != 3 || len(data.Modules[0].Metrics[0].Values) != 7 {
		t.Fatalf("many comparison lost inputs/reference: %+v", data)
	}
	if !strings.Contains(stdout.String(), "node-g") || !strings.Contains(stdout.String(), "排名") {
		t.Fatalf("many terminal layout did not adapt:\n%s", stdout.String())
	}
}

func TestCompareCommandRejectsTooFewReportsBeforeWriting(t *testing.T) {
	inputs := t.TempDir()
	report := writeComparisonInput(t, inputs, "only", 100)
	output := filepath.Join(t.TempDir(), "not-created")
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"compare", report, "--output", output}, &stdout, &stderr)
	if status == 0 || !strings.Contains(stderr.String(), "至少需要两份") {
		t.Fatalf("one-report compare status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid comparison created output directory: %v", err)
	}
}

func TestCompareCommandEnglishOutput(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	inputs := t.TempDir()
	first := writeComparisonInput(t, inputs, "first", 10)
	second := writeComparisonInput(t, inputs, "second", 20)
	output := t.TempDir()
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"compare", first, second, "--lang", "en", "--format", "json", "--output", output, "--name", "english", "--no-color",
	}, &stdout, &stderr)
	if status != 0 || !strings.Contains(stdout.String(), "ecs multi-report comparison") || !strings.Contains(stdout.String(), "Comparison reports written") {
		t.Fatalf("English compare status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
}

func TestCompareBuildValidationErrorUsesCurrentLanguageAtCLI(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	_, err := comparison.Build([]model.Report{{}, {}}, comparison.Options{Reference: 2})
	if err == nil {
		t.Fatal("invalid comparison reference unexpectedly succeeded")
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		got := localizeComparisonError(err)
		want := i18n.Errorf("compare.help.referenceRange", 2).Error()
		if got != want || strings.Contains(got, "compare.help.referenceRange") {
			t.Fatalf("%s comparison validation error = %q, want %q", language, got, want)
		}
	}
}

func writeLocalizedObservationInput(t *testing.T, directory, name, fieldValue, tableValue string) string {
	t.Helper()
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run: model.RunInfo{
			ID: name, Profile: "standard", StartedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Redacted: true,
		},
		Results: []model.Result{{
			ID: "system", Title: "系统", Status: model.StatusOK,
			Fields: []model.Field{{Key: "state", Label: "状态", Value: fieldValue}},
			Tables: []model.Table{{
				Title:   "当前值",
				Columns: []string{"名称", "状态"},
				Rows:    [][]string{{"系统", tableValue}},
			}},
		}},
	}
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name+".json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompareUsesCanonicalObservationValuesWhenLanguageIsEnglish(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	inputs := t.TempDir()
	first := writeLocalizedObservationInput(t, inputs, "first", "系统", "系统")
	second := writeLocalizedObservationInput(t, inputs, "second", "完成", "完成")
	output := t.TempDir()
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"compare", first, second, "--lang", "en", "--format", "json", "--output", output, "--name", "canonical", "--no-color",
	}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("canonical compare status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(output, "canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data comparison.Report
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Modules) != 1 {
		t.Fatalf("canonical observations missing: %+v", data.Modules)
	}
	values := make(map[string]bool)
	for _, change := range data.Modules[0].Changes {
		for _, value := range change.Values {
			if value.Available {
				values[value.Value] = true
			}
		}
	}
	for _, expected := range []string{"系统", "完成"} {
		if !values[expected] {
			t.Fatalf("comparison localized canonical value %q: values=%v, json=%s", expected, values, content)
		}
	}
	for _, translated := range []string{"OS", "Done"} {
		if values[translated] {
			t.Fatalf("comparison unexpectedly used localized machine value %q: values=%v", translated, values)
		}
	}
}
