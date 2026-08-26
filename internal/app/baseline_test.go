package app

import (
	"fmt"
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

// 提交库按月份分子目录存放，只扫一层会什么都找不到——CI 上就是这么红的。
func TestExpandReportPathsRecursesIntoSubdirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "2026-08")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "top.json"),
		filepath.Join(nested, "a.json"),
		filepath.Join(nested, "b.json"),
		filepath.Join(root, "README.md"),
	} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 隐藏目录不该被收进来。
	hidden := filepath.Join(root, ".git")
	_ = os.MkdirAll(hidden, 0o755)
	_ = os.WriteFile(filepath.Join(hidden, "c.json"), []byte("{}"), 0o600)

	got := expandReportPaths([]string{root})
	if len(got) != 3 {
		t.Fatalf("应收集到 3 个 json（含子目录、不含隐藏目录与非 json），得到 %d 个：%v", len(got), got)
	}
	for _, path := range got {
		if filepath.Ext(path) != ".json" {
			t.Errorf("收集到非 json 文件：%s", path)
		}
		if filepath.Base(filepath.Dir(path)) == ".git" {
			t.Errorf("收集到隐藏目录里的文件：%s", path)
		}
	}
}

func submitTestReport() model.Report {
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{Profile: "full", StartedAt: time.Unix(1700000000, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK, OK: 2, Messages: []model.Message{model.NewMessage("message.summary.allOK", 2)}},
		Results: []model.Result{{
			ID: "cpu", Status: model.StatusOK,
			Measurements: []model.Measurement{
				{Key: "sysbench_cpu_single_events_s", Value: 900},
				{Key: "sysbench_cpu_multi_events_s", Value: 3400},
			},
		}},
	}
	report.Results = append([]model.Result{{
		ID: "system", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "logical_cpus", Value: 4},
			{Key: "memory_total_bytes", Value: 8 * (1 << 30)},
		},
	}}, report.Results...)
	return report
}

func writeBaselineReport(t *testing.T, name string, report model.Report) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSubmissionFixture(t *testing.T, path string) string {
	t.Helper()
	submission, err := score.BuildSubmission(submitTestReport(), score.SubmissionOptions{
		Region: "us", Provider: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := submission.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeOutlierSubmissionFixture(t *testing.T, path string, single float64, multi *float64) string {
	t.Helper()
	report := submitTestReport()
	for index := range report.Results {
		if report.Results[index].ID != "cpu" {
			continue
		}
		report.Results[index].Measurements = []model.Measurement{{Key: "sysbench_cpu_single_events_s", Value: single}}
		if multi != nil {
			report.Results[index].Measurements = append(report.Results[index].Measurements,
				model.Measurement{Key: "sysbench_cpu_multi_events_s", Value: *multi})
		}
	}
	submission, err := score.BuildSubmission(report, score.SubmissionOptions{Region: "us", Provider: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	content, err := submission.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLeaderboardCommandsWriteReadableResults(t *testing.T) {
	root := t.TempDir()
	input := writeBaselineReport(t, "report.json", submitTestReport())
	previousPath := filepath.Join(root, "previous-baseline.json")
	previous, err := (score.Baseline{
		Schema: score.BaselineSchema, Source: "previous", SampleCount: 1,
		Metrics: map[string]float64{"sysbench_cpu_single_events_s": 1},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previousPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"baseline", "leaderboard"} {
		t.Run(command, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), command+".json")
			status, stdout, stderr := invokeAppMain(command, "--lang", "en", "--source", "fixture", "--output", output, previousPath, input)
			if status != 0 || stderr != "" || !strings.Contains(stdout, "written") {
				t.Fatalf("%s status=%d stdout=%q stderr=%q", command, status, stdout, stderr)
			}
			if command == "baseline" {
				baseline, err := score.LoadBaseline(output)
				if err != nil {
					t.Fatalf("written baseline is not readable: %v", err)
				}
				if baseline.Schema != score.BaselineSchema || baseline.SampleCount != 1 || len(baseline.Metrics) == 0 || baseline.Source != "fixture" {
					t.Fatalf("unexpected baseline result: %+v", baseline)
				}
			} else if _, err := os.Stat(output); err != nil {
				t.Fatalf("leaderboard did not write output: %v", err)
			}
		})
	}
	t.Run("leaderboard annotations", func(t *testing.T) {
		root := t.TempDir()
		values := []float64{98, 99, 100, 101, 102, 103, 104, 1000}
		inputs := make([]string, 0, len(values))
		for index, value := range values {
			var multi *float64
			if index == 0 {
				onlyMulti := 3400.0
				multi = &onlyMulti
			}
			inputs = append(inputs, writeOutlierSubmissionFixture(t, filepath.Join(root, fmt.Sprintf("submission-%d.json", index)), value, multi))
		}
		output := filepath.Join(root, "annotated.json")
		args := []string{"leaderboard", "--lang", "en", "--source", "fixture", "--annotate", "--verbose", "--output", output}
		args = append(args, inputs...)
		status, stdout, stderr := invokeAppMain(args...)
		if status != 0 || stderr != "" || !strings.Contains(stdout, "Outlier notices:") ||
			!strings.Contains(stdout, "::warning::") ||
			!strings.Contains(stdout, "Combinations left unjudged for lack of samples:") ||
			!strings.Contains(stdout, "active") {
			t.Fatalf("leaderboard annotations status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
		if _, err := score.LoadBaseline(output); err != nil {
			t.Fatalf("annotated leaderboard output is not loadable: %v", err)
		}
	})
}

func TestBaselineStrictRejectsInvalidInputWithoutWriting(t *testing.T) {
	valid := writeBaselineReport(t, "valid.json", submitTestReport())
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	for _, test := range []struct {
		name  string
		input []string
	}{
		{name: "bad JSON", input: []string{valid, bad}},
		{name: "missing path", input: []string{missing}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "baseline.json")
			args := append([]string{"baseline", "--lang", "en", "--strict", "--output", output}, test.input...)
			status, stdout, stderr := invokeAppMain(args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "strict mode rejected") {
				t.Fatalf("strict input status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("strict input wrote output: %v", err)
			}
		})
	}
}

func TestBaselineInputStates(t *testing.T) {
	root := t.TempDir()
	valid := writeBaselineReport(t, "valid.json", submitTestReport())
	bad := filepath.Join(root, "bad.json")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	noMetrics := writeSubmitFixtureReport(t, filepath.Join(root, "no-metrics.json"), modelReportWithoutScoreableMeasurements())
	firstSubmission := writeSubmissionFixture(t, filepath.Join(root, "first-submission.json"))
	secondSubmission := writeSubmissionFixture(t, filepath.Join(root, "second-submission.json"))
	cases := []struct {
		name       string
		args       func(string) []string
		wantStatus int
		markers    []string
		checkOut   bool
	}{
		{
			name:       "no input",
			args:       func(output string) []string { return []string{"--lang", "en", "--output", output} },
			wantStatus: 1,
			markers:    []string{"at least one report file"},
		},
		{
			name: "nonstrict skips bad report",
			args: func(output string) []string {
				return []string{"--lang", "en", "--output", output, valid, bad}
			},
			markers:  []string{"Skipped"},
			checkOut: true,
		},
		{
			name: "nonstrict skips duplicate submission",
			args: func(output string) []string {
				return []string{"--lang", "en", "--output", output, firstSubmission, secondSubmission}
			},
			markers:  []string{"Skipped"},
			checkOut: true,
		},
		{
			name:       "no usable report",
			args:       func(output string) []string { return []string{"--lang", "en", "--output", output, noMetrics} },
			wantStatus: 1,
			markers:    []string{"Skipped", "no scoreable measurements", "no usable report files"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "baseline.json")
			status, stdout, stderr := invokeAppMain(append([]string{"baseline"}, test.args(output)...)...)
			if status != test.wantStatus {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			for _, marker := range test.markers {
				if !strings.Contains(stdout+stderr, marker) {
					t.Fatalf("missing marker %q in stdout=%q stderr=%q", marker, stdout, stderr)
				}
			}
			if test.checkOut {
				if _, err := score.LoadBaseline(output); err != nil {
					t.Fatalf("nonstrict input did not produce baseline: %v", err)
				}
			}
		})
	}
}

func TestBaselineReportsOutputWriteFailure(t *testing.T) {
	input := writeBaselineReport(t, "report.json", submitTestReport())
	outputDirectory := t.TempDir()
	status, stdout, stderr := invokeAppMain("baseline", "--lang", "en", "--output", outputDirectory, input)
	if status != 1 || stdout != "" || !strings.Contains(stderr, "is a directory") {
		t.Fatalf("baseline output failure status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(outputDirectory); err != nil {
		t.Fatalf("output failure changed the destination: %v", err)
	}
}

func TestBaselineAndLeaderboardHelp(t *testing.T) {
	for _, test := range []struct {
		command string
		marker  string
	}{
		{command: "baseline", marker: "Usage: ecs baseline"},
		{command: "leaderboard", marker: "Usage: ecs leaderboard"},
	} {
		t.Run(test.command, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.command, "--lang", "en", "--help")
			if status != 0 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("%s help status=%d stdout=%q stderr=%q", test.command, status, stdout, stderr)
			}
		})
	}
}

func TestBaselineAndLeaderboardRejectUnknownFlag(t *testing.T) {
	status, stdout, stderr := invokeAppMain("baseline", "--lang", "en", "--unknown")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("baseline unknown flag status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}
