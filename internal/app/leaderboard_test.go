package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func writeLeaderboardReport(t *testing.T, path, runID string, metric float64) string {
	return writeLeaderboardReportWithVCPU(t, path, runID, 4, metric)
}

func writeLeaderboardReportWithVCPU(t *testing.T, path, runID string, vcpu int, metric float64) string {
	t.Helper()
	report := submitTestReport()
	report.Run.ID = runID
	foundMetric, foundVCPU := false, false
	for resultIndex := range report.Results {
		for measurementIndex := range report.Results[resultIndex].Measurements {
			measurement := &report.Results[resultIndex].Measurements[measurementIndex]
			switch measurement.Key {
			case "sysbench_cpu_single_events_s":
				measurement.Value = metric
				foundMetric = true
			case "logical_cpus":
				measurement.Value = float64(vcpu)
				foundVCPU = true
			}
		}
	}
	if !foundMetric || !foundVCPU {
		t.Fatalf("leaderboard fixture measurements missing: metric=%v vcpu=%v", foundMetric, foundVCPU)
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

func writeLeaderboardSubmission(t *testing.T, path string, report model.Report) string {
	t.Helper()
	submission, err := score.BuildSubmission(report, score.SubmissionOptions{})
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

func TestLeaderboardDeduplicatesRepeatedPath(t *testing.T) {
	root := t.TempDir()
	input := writeLeaderboardReport(t, filepath.Join(root, "a.json"), "path-repeat", 100)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("leaderboard", "--lang", "en", "--output", output, input, input)
	if status != 0 || !strings.Contains(stdout, "written") || !strings.Contains(strings.ToLower(stderr), "duplicate") {
		t.Fatalf("repeated path status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("repeated path output is not loadable: %v", err)
	}
	if baseline.SampleCount != 1 || baseline.Metrics["cpu_single"] != 100 {
		t.Fatalf("repeated path baseline = %+v", baseline)
	}
}

func TestLeaderboardDeduplicatesDirectoryAndFilePath(t *testing.T) {
	root := t.TempDir()
	reports := filepath.Join(root, "reports")
	if err := os.MkdirAll(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	input := writeLeaderboardReport(t, filepath.Join(reports, "a.json"), "directory-repeat", 100)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("leaderboard", "--lang", "en", "--output", output, reports, input)
	if status != 0 || !strings.Contains(stdout, "written") || !strings.Contains(strings.ToLower(stderr), "duplicate") {
		t.Fatalf("directory and file status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("directory and file output is not loadable: %v", err)
	}
	if baseline.SampleCount != 1 || baseline.Metrics["cpu_single"] != 100 {
		t.Fatalf("directory and file baseline = %+v", baseline)
	}
}

func TestLeaderboardDeduplicatesFullReportsByRunIDAndPreservesMean(t *testing.T) {
	root := t.TempDir()
	first := writeLeaderboardReport(t, filepath.Join(root, "a.json"), "same-run", 100)
	copy := writeLeaderboardReport(t, filepath.Join(root, "copy.json"), "same-run", 900)
	second := writeLeaderboardReport(t, filepath.Join(root, "b.json"), "different-run", 300)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("leaderboard", "--lang", "en", "--output", output, first, copy, second)
	if status != 0 || !strings.Contains(stdout, "written") || !strings.Contains(strings.ToLower(stderr), "duplicate") {
		t.Fatalf("duplicate Run.ID status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("duplicate Run.ID output is not loadable: %v", err)
	}
	if baseline.SampleCount != 2 {
		t.Fatalf("duplicate Run.ID sample count = %d, want 2", baseline.SampleCount)
	}
	if got := baseline.Metrics["cpu_single"]; got != 200 {
		t.Fatalf("duplicate Run.ID mean = %v, want 200", got)
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != 2 || baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 {
		t.Fatalf("duplicate Run.ID tier statistics = %+v", baseline.Tiers)
	}
}

func TestLeaderboardStrictRejectsDuplicatesWithoutWriting(t *testing.T) {
	root := t.TempDir()
	first := writeLeaderboardReport(t, filepath.Join(root, "a.json"), "strict-run", 100)
	copy := writeLeaderboardReport(t, filepath.Join(root, "copy.json"), "strict-run", 100)
	for _, test := range []struct {
		name   string
		inputs []string
	}{
		{name: "path", inputs: []string{first, first}},
		{name: "run ID", inputs: []string{first, copy}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "baseline.json")
			args := []string{"leaderboard", "--lang", "en", "--strict", "--output", output}
			args = append(args, test.inputs...)
			status, stdout, stderr := invokeAppMain(args...)
			if status != 1 || stdout != "" || !strings.Contains(strings.ToLower(stderr), "duplicate") {
				t.Fatalf("strict duplicate status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("strict duplicate wrote output: %v", err)
			}
		})
	}
}

func TestLeaderboardKeepsDifferentRunIDsEvenWhenContentMatches(t *testing.T) {
	root := t.TempDir()
	first := writeLeaderboardReport(t, filepath.Join(root, "a.json"), "run-one", 250)
	second := writeLeaderboardReport(t, filepath.Join(root, "b.json"), "run-two", 250)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("leaderboard", "--lang", "en", "--output", output, first, second)
	if status != 0 || stderr != "" || !strings.Contains(stdout, "written") {
		t.Fatalf("different Run.ID status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("different Run.ID output is not loadable: %v", err)
	}
	if baseline.SampleCount != 2 || baseline.Metrics["cpu_single"] != 250 {
		t.Fatalf("different Run.ID baseline = %+v", baseline)
	}
}

func TestLeaderboardKeepsSubmissionIDDeduplication(t *testing.T) {
	root := t.TempDir()
	setMetric := func(report *model.Report, value float64) {
		for resultIndex := range report.Results {
			for measurementIndex := range report.Results[resultIndex].Measurements {
				measurement := &report.Results[resultIndex].Measurements[measurementIndex]
				if measurement.Key == "sysbench_cpu_single_events_s" {
					measurement.Value = value
					return
				}
			}
		}
		t.Fatal("leaderboard submission fixture CPU measurement missing")
	}
	firstReport := submitTestReport()
	setMetric(&firstReport, 100)
	uniqueReport := submitTestReport()
	setMetric(&uniqueReport, 300)
	first := writeLeaderboardSubmission(t, filepath.Join(root, "first.json"), firstReport)
	duplicate := writeLeaderboardSubmission(t, filepath.Join(root, "duplicate.json"), firstReport)
	unique := writeLeaderboardSubmission(t, filepath.Join(root, "unique.json"), uniqueReport)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("leaderboard", "--lang", "en", "--output", output, first, first, duplicate, unique)
	if status != 0 || !strings.Contains(stdout, "written") || !strings.Contains(strings.ToLower(stderr), "duplicate") {
		t.Fatalf("duplicate submission status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("duplicate submission output is not loadable: %v", err)
	}
	if baseline.SampleCount != 2 || baseline.Metrics["cpu_single"] != 200 {
		t.Fatalf("duplicate submission aggregate = %+v, want 2 samples with mean 200", baseline)
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != 2 || baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 {
		t.Fatalf("duplicate submission tier = %+v", baseline.Tiers)
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
