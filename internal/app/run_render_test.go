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
	"ecs/internal/score"
)

func writeAppRenderReport(t *testing.T, directory string) string {
	t.Helper()
	path := filepath.Join(directory, "sample.json")
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: "sample", Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK, Messages: []model.Message{model.NewMessage("message.summary.allOK", 1)}},
		Results: []model.Result{{
			ID: "system", Title: "系统", Status: model.StatusOK,
			Fields: []model.Field{{Key: "state", Label: "状态", Value: model.RawValue("系统")}},
			Tables: []model.Table{{
				Key: "state", Title: "当前值", Columns: []model.TableColumn{{Key: "status", Label: "状态"}},
				Rows: [][]model.Value{{model.RawValue("完成")}},
			}},
		}},
	}
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func invokeAppMain(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), args, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestRenderWritesRequestedFormats(t *testing.T) {
	root := t.TempDir()
	input := writeAppRenderReport(t, root)
	output := filepath.Join(root, "out")
	status, stdout, stderr := invokeAppMain(
		"--lang", "en", "render", "--input", input, "--output", output,
		"--format", "json,md,html",
	)
	if status != 0 || stderr != "" {
		t.Fatalf("render status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	markers := map[string]string{
		"json": `"schema_version"`,
		"md":   "# ecs VPS Benchmark Report",
		"html": "<html",
	}
	for format, marker := range markers {
		data, err := os.ReadFile(filepath.Join(output, "sample."+format))
		if err != nil {
			t.Fatalf("read %s: %v", format, err)
		}
		if len(data) == 0 || !strings.Contains(string(data), marker) {
			t.Fatalf("%s missing marker %q", format, marker)
		}
	}
}

func TestRenderLoadsScoreBaseline(t *testing.T) {
	root := t.TempDir()
	input := writeAppRenderReport(t, root)
	baselinePath := filepath.Join(root, "baseline.json")
	baseline, err := (score.Baseline{
		Schema:      score.BaselineSchema,
		Source:      "fixture",
		SampleCount: 1,
		Metrics:     map[string]float64{"cpu_single": 1},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, baseline, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "out")
	status, stdout, stderr := invokeAppMain(
		"--lang", "en", "render", "--input", input, "--score-baseline", baselinePath,
		"--output", output, "--format", "json",
	)
	if status != 0 || stderr != "" {
		t.Fatalf("render with baseline status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(output, "sample.json")); err != nil {
		t.Fatalf("render with baseline did not write JSON: %v", err)
	}
}

func TestRunCommandReportsPreExecutionFailures(t *testing.T) {
	cases := []struct {
		name   string
		args   func(string) []string
		setup  func(*testing.T, string) string
		marker string
	}{
		{
			name: "config read",
			args: func(configPath string) []string {
				return []string{"run", "--lang", "en", "--config", configPath}
			},
			marker: "read config file:",
		},
		{
			name: "config apply",
			setup: func(t *testing.T, directory string) string {
				t.Helper()
				path := filepath.Join(directory, "config.json")
				if err := os.WriteFile(path, []byte(`{"exposure":"invalid"}`), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			args: func(configPath string) []string {
				return []string{"run", "--lang", "en", "--config", configPath}
			},
			marker: "unknown exposure level",
		},
		{
			name:   "flag parse",
			args:   func(string) []string { return []string{"run", "--lang", "en", "--unknown"} },
			marker: "flag provided but not defined",
		},
		{
			name:   "extra argument",
			args:   func(string) []string { return []string{"run", "--lang", "en", "unexpected"} },
			marker: "error: unexpected arguments",
		},
		{
			name:   "IPv4 and IPv6 conflict",
			args:   func(string) []string { return []string{"run", "--lang", "en", "--4", "--6"} },
			marker: "-4 and -6 cannot be used together",
		},
		{
			name: "empty module selection",
			args: func(string) []string {
				return []string{"run", "--lang", "en", "--only", "system", "--skip", "system", "--yes"}
			},
			marker: "at least one module must be selected",
		},
		{
			name: "exposure limit",
			args: func(string) []string {
				return []string{"run", "--lang", "en", "--only", "network", "--exposure", "local", "--yes"}
			},
			marker: "above the current --exposure local",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			configPath := filepath.Join(directory, "missing.json")
			if test.setup != nil {
				configPath = test.setup(t, directory)
			}
			status, stdout, stderr := invokeAppMain(test.args(configPath)...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestRunCommandUsesFormalFlagValuesConsumedByName(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "profile-looking value", args: []string{"--lang", "en", "--name", "--profile=full", "--version"}},
		{name: "config-looking value", args: []string{"--lang", "en", "--name", "--config=/definitely/not/exist", "--version"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 0 || stderr != "" || !strings.HasPrefix(stdout, "ecs ") {
				t.Fatalf("run command status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestRunRejectsUnknownModuleBeforeRunnerAndReport(t *testing.T) {
	output := filepath.Join(t.TempDir(), "reports")
	status, stdout, stderr := invokeAppMain(
		"run", "--lang", "en", "--only", "system,nonexistent", "--exposure", "any",
		"--output", output, "--yes",
	)
	if status == 0 || stdout != "" || !strings.Contains(stderr, "unknown module") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	entries, err := os.ReadDir(output)
	if err == nil {
		if len(entries) != 0 {
			t.Fatalf("unknown module created report files: %v", entries)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect report directory: %v", err)
	}
}

func TestRenderCommandReportsInputAndOutputFailures(t *testing.T) {
	cases := []struct {
		name   string
		args   func(string, string, string, string) []string
		setup  func(*testing.T, string) string
		marker string
	}{
		{
			name:   "flag parse",
			args:   func(string, string, string, string) []string { return []string{"--lang", "en", "render", "--unknown"} },
			marker: "flag provided but not defined",
		},
		{
			name: "color flag parse",
			args: func(string, string, string, string) []string {
				return []string{"--lang", "en", "render", "--color", "none"}
			},
			marker: "flag provided but not defined: -color",
		},
		{
			name:   "input required",
			args:   func(string, string, string, string) []string { return []string{"--lang", "en", "render"} },
			marker: "error: --input is required",
		},
		{
			name:   "extra argument",
			args:   func(string, string, string, string) []string { return []string{"--lang", "en", "render", "unexpected"} },
			marker: "unexpected arguments",
		},
		{
			name: "load JSON",
			args: func(_, badInput, _, _ string) []string {
				return []string{"--lang", "en", "render", "--input", badInput}
			},
			marker: "error:",
		},
		{
			name: "baseline load",
			args: func(input, _, badBaseline, _ string) []string {
				return []string{"--lang", "en", "render", "--input", input, "--score-baseline", badBaseline}
			},
			marker: "read scoring leaderboard reference",
		},
		{
			name: "unsupported format",
			args: func(input, _, _, _ string) []string {
				return []string{"--lang", "en", "render", "--input", input, "--format", "txt"}
			},
			marker: `unknown output format "txt"`,
		},
		{
			name: "output directory",
			args: func(input, _, _, output string) []string {
				return []string{"--lang", "en", "render", "--input", input, "--output", output, "--format", "json"}
			},
			marker: "create output directory",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			input := writeAppRenderReport(t, directory)
			badInput := filepath.Join(directory, "bad.json")
			if err := os.WriteFile(badInput, []byte("not json\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			badBaseline := filepath.Join(directory, "missing-baseline.json")
			output := filepath.Join(directory, "output-file")
			if err := os.WriteFile(output, []byte("occupied"), 0o600); err != nil {
				t.Fatal(err)
			}
			status, stdout, stderr := invokeAppMain(test.args(input, badInput, badBaseline, output)...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestRenderRejectsEmptyFormatBeforeCreatingOutput(t *testing.T) {
	root := t.TempDir()
	input := writeAppRenderReport(t, root)
	output := filepath.Join(root, "not-created")
	status, stdout, stderr := invokeAppMain(
		"--lang", "en", "render", "--input", input, "--output", output, "--format", "",
	)
	if status != 1 || stdout != "" || !strings.Contains(stderr, "at least one output format is required") {
		t.Fatalf("empty format status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("empty format created output path: %v", err)
	}
}
