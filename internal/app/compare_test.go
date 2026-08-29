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
			Fields: []model.Field{{Key: "state", Label: "状态", Value: model.RawValue(fieldValue)}},
			Tables: []model.Table{{
				Key: "system.state", Title: "当前值",
				Columns: []model.TableColumn{{Key: "name", Label: "名称"}, {Key: "state", Label: "状态"}}, RowIdentity: "name",
				Rows: [][]model.Value{{model.RawValue("系统"), model.RawValue(tableValue)}},
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
		"compare", first, second, "--lang", "en", "--format", "json,md,html", "--output", output, "--name", "fleet", "--reference", "2", "--no-color",
	}, &stdout, &stderr); status != 0 {
		t.Fatalf("compare failed: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	for _, format := range []string{"json", "md", "html"} {
		if _, err := os.Stat(filepath.Join(output, "fleet."+format)); err != nil {
			t.Fatalf("compare did not write %s: %v", format, err)
		}
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

func TestCompareCommandReportsDistinctFailures(t *testing.T) {
	root := t.TempDir()
	first := writeLocalizedObservationInput(t, root, "first", "系统", "系统")
	second := writeLocalizedObservationInput(t, root, "second", "完成", "完成")
	only := writeLocalizedObservationInput(t, root, "only", "系统", "系统")
	bad := filepath.Join(root, "bad.json")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyContent, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	legacyContent = bytes.Replace(legacyContent, []byte(buildinfo.SchemaVersion), []byte("ecs.report/v2"), 1)
	legacy := filepath.Join(root, "legacy.json")
	if err := os.WriteFile(legacy, legacyContent, 0o600); err != nil {
		t.Fatal(err)
	}
	outputFile := filepath.Join(root, "output-file")
	if err := os.WriteFile(outputFile, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		args     []string
		marker   string
		noOutput string
	}{
		{
			name:     "too few reports",
			args:     []string{"compare", only, "--lang", "en", "--output", filepath.Join(root, "too-few")},
			marker:   "requires at least two JSON reports",
			noOutput: filepath.Join(root, "too-few"),
		},
		{
			name:   "missing normalized flag value",
			args:   []string{"compare", "--lang", "en", "--format"},
			marker: "--format requires a value",
		},
		{
			name:   "load JSON",
			args:   []string{"compare", bad, second, "--lang", "en", "--format", "json"},
			marker: "error:",
		},
		{
			name:   "historical schema",
			args:   []string{"compare", legacy, second, "--lang", "en", "--format", "json"},
			marker: "unsupported schema_version",
		},
		{
			name:   "invalid reference",
			args:   []string{"compare", first, second, "--lang", "en", "--reference", "3"},
			marker: "--reference must be between 1 and 2",
		},
		{
			name:   "unsupported format",
			args:   []string{"compare", first, second, "--lang", "en", "--format", "txt"},
			marker: `unknown output format "txt"`,
		},
		{
			name:   "no formats",
			args:   []string{"compare", first, second, "--lang", "en", "--format", ""},
			marker: "at least one output format is required",
		},
		{
			name:   "output directory",
			args:   []string{"compare", first, second, "--lang", "en", "--format", "json", "--output", outputFile},
			marker: "create output directory",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if test.noOutput != "" {
				if _, err := os.Stat(test.noOutput); !os.IsNotExist(err) {
					t.Fatalf("invalid comparison created output: %v", err)
				}
			}
			if test.name == "invalid reference" && strings.Contains(stderr, "compare.help") {
				t.Fatalf("localized comparison error leaked its key: %q", stderr)
			}
		})
	}
}

func TestCompareCommandReportsHelpAndUnknownFlag(t *testing.T) {
	status, stdout, stderr := invokeAppMain("compare", "--lang", "en", "--help")
	if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs compare") {
		t.Fatalf("compare help status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = invokeAppMain("compare", "--lang", "en", "--unknown")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("compare unknown flag status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestCompareCommandRejectsEmptyOutputPath(t *testing.T) {
	root := t.TempDir()
	first := writeLocalizedObservationInput(t, root, "first", "系统", "系统")
	second := writeLocalizedObservationInput(t, root, "second", "完成", "完成")
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	status, stdout, stderr := invokeAppMain(
		"compare", first, second, "--lang", "en", "--format", "json", "--output=",
	)
	if status != 1 || stdout != "" || !strings.Contains(stderr, "comparison output path must not be empty") {
		t.Fatalf("empty output status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(workingDirectory, "reports")); !os.IsNotExist(err) {
		t.Fatalf("empty output created ./reports: %v", err)
	}
	entries, err := os.ReadDir(workingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty output created comparison artifacts: %v", entries)
	}
}

func TestCompareCommandPreservesDoubleDashForOptionLikeReports(t *testing.T) {
	root := t.TempDir()
	writeLocalizedObservationInput(t, root, "--first", "系统", "系统")
	writeLocalizedObservationInput(t, root, "--second", "完成", "完成")
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	output := t.TempDir()
	status, stdout, stderr := invokeAppMain(
		"compare", "--lang", "en", "--format", "json", "--output", output, "--name", "boundary", "--", "--first.json", "--second.json",
	)
	if status != 0 || stdout == "" || stderr != "" {
		t.Fatalf("double-dash option-like reports status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(output, "boundary.json")); err != nil {
		t.Fatalf("double-dash option-like reports did not write comparison: %v", err)
	}
}
