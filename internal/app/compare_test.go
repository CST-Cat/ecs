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

func writeLocalizedObservationInput(t *testing.T, directory, name, fieldValue, tableValue string) string {
	t.Helper()
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: name, Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), Redacted: true},
		Results: []model.Result{{
			ID: "system", Title: "系统", Status: model.StatusOK,
			Fields: []model.Field{{Key: "state", Label: "状态", Value: fieldValue}},
			Tables: []model.Table{{
				Key: "system.state", Title: "当前值",
				Columns: []string{"名称", "状态"}, ColumnKeys: []string{"name", "state"}, RowIdentity: "name",
				Rows: [][]string{{"系统", tableValue}},
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

func TestCompareCommandWritesReadableJSONAndPreservesCanonicalValues(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	inputs := t.TempDir()
	first := writeLocalizedObservationInput(t, inputs, "first", "系统", "系统")
	second := writeLocalizedObservationInput(t, inputs, "second", "完成", "完成")
	output := t.TempDir()
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{
		"compare", first, second, "--lang", "en", "--format", "json", "--output", output, "--name", "fleet", "--no-color",
	}, &stdout, &stderr); status != 0 {
		t.Fatalf("compare failed: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(output, "fleet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data comparison.Report
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatalf("written comparison is not readable: %v", err)
	}
	if len(data.Modules) != 1 {
		t.Fatalf("comparison lost its module: %+v", data.Modules)
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
			t.Fatalf("canonical value %q missing: %v", expected, values)
		}
	}
	for _, translated := range []string{"OS", "Done"} {
		if values[translated] {
			t.Fatalf("localized machine value leaked into JSON: %q", translated)
		}
	}
}

func TestCompareCommandRejectsTooFewReportsBeforeWriting(t *testing.T) {
	report := writeLocalizedObservationInput(t, t.TempDir(), "only", "系统", "系统")
	output := filepath.Join(t.TempDir(), "not-created")
	var stdout, stderr bytes.Buffer
	if status := Main(context.Background(), []string{"compare", report, "--output", output}, &stdout, &stderr); status == 0 {
		t.Fatalf("one-report compare unexpectedly succeeded: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid comparison created output: %v", err)
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
