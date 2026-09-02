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
	submission, err := score.BuildSubmission(newApplication().modules, report, score.SubmissionOptions{})
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
		Metrics: map[string]float64{"cpu_single": 1},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previousPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "leaderboard.json")
	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--source", "fixture", "--output", output, previousPath, input)
	if status != 0 || stderr != "" || !strings.Contains(stdout, "written") {
		t.Fatalf("leaderboard status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("written leaderboard reference is not readable: %v", err)
	}
	if baseline.Schema != score.BaselineSchema || baseline.SampleCount != 1 || len(baseline.Metrics) == 0 || baseline.Source != "fixture" {
		t.Fatalf("unexpected leaderboard result: %+v", baseline)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("leaderboard did not write output: %v", err)
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
		args := []string{"--lang", "en", "leaderboard", "--source", "fixture", "--annotate", "--verbose", "--output", output}
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

	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--output", output, input, input)
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

	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--output", output, reports, input)
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

func TestLeaderboardDeduplicatesReportAndSubmissionBySampleID(t *testing.T) {
	root := t.TempDir()
	report := submitTestReport()
	report.Run.ID = "shared-sample-run"
	foundMetric := false
	for resultIndex := range report.Results {
		for measurementIndex := range report.Results[resultIndex].Measurements {
			measurement := &report.Results[resultIndex].Measurements[measurementIndex]
			if measurement.Key == "sysbench_cpu_single_events_s" {
				measurement.Value = 100
				foundMetric = true
			}
		}
	}
	if !foundMetric {
		t.Fatal("shared sample fixture CPU measurement missing")
	}
	fullPath := filepath.Join(root, "report.json")
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	submissionPath := writeLeaderboardSubmission(t, filepath.Join(root, "submission.json"), report)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain(
		"--lang", "en", "leaderboard", "--output", output, fullPath, submissionPath,
	)
	if status != 0 || !strings.Contains(stdout, "written") || !strings.Contains(strings.ToLower(stderr), "duplicate sample") {
		t.Fatalf("cross-artifact duplicate status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("cross-artifact output is not loadable: %v", err)
	}
	if baseline.SampleCount != 1 || baseline.Metrics["cpu_single"] != 100 || len(baseline.ScoreSamples) != 1 {
		t.Fatalf("cross-artifact aggregate = %+v", baseline)
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != 1 || baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 1 {
		t.Fatalf("cross-artifact tier statistics = %+v", baseline.Tiers)
	}
}

func TestLeaderboardStrictRejectsCrossArtifactDuplicateSample(t *testing.T) {
	root := t.TempDir()
	report := submitTestReport()
	report.Run.ID = "strict-cross-artifact-run"
	fullPath := filepath.Join(root, "report.json")
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	submissionPath := writeLeaderboardSubmission(t, filepath.Join(root, "submission.json"), report)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain(
		"--lang", "en", "leaderboard", "--strict", "--output", output, fullPath, submissionPath,
	)
	if status != 1 || stdout != "" || !strings.Contains(strings.ToLower(stderr), "strict mode rejected") || !strings.Contains(strings.ToLower(stderr), "duplicate sample") {
		t.Fatalf("strict cross-artifact duplicate status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("strict cross-artifact duplicate wrote output: %v", err)
	}
}

func TestLeaderboardDeduplicatesFullReportsByRunIDAndPreservesMean(t *testing.T) {
	root := t.TempDir()
	first := writeLeaderboardReport(t, filepath.Join(root, "a.json"), "same-run", 100)
	copy := writeLeaderboardReport(t, filepath.Join(root, "copy.json"), "same-run", 900)
	second := writeLeaderboardReport(t, filepath.Join(root, "b.json"), "different-run", 300)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--output", output, first, copy, second)
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
			args := []string{"--lang", "en", "leaderboard", "--strict", "--output", output}
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

	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--output", output, first, second)
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
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != 2 || baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 || len(baseline.ScoreSamples) != 2 {
		t.Fatalf("different Run.ID statistics = %+v", baseline)
	}
}

func TestLeaderboardKeepsDifferentSubmissionSampleIDsWithSameContent(t *testing.T) {
	root := t.TempDir()
	firstReport := submitTestReport()
	firstReport.Run.ID = "submission-run-one"
	secondReport := firstReport
	secondReport.Run.ID = "submission-run-two"
	first := writeLeaderboardSubmission(t, filepath.Join(root, "first.json"), firstReport)
	second := writeLeaderboardSubmission(t, filepath.Join(root, "second.json"), secondReport)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--output", output, first, second)
	if status != 0 || stderr != "" || !strings.Contains(stdout, "written") {
		t.Fatalf("different submission samples status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("different submission samples output is not loadable: %v", err)
	}
	if baseline.SampleCount != 2 || baseline.Metrics["cpu_single"] != 900 || len(baseline.ScoreSamples) != 2 {
		t.Fatalf("different submission samples aggregate = %+v", baseline)
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != 2 || baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 {
		t.Fatalf("different submission samples tier statistics = %+v", baseline.Tiers)
	}
}

func TestLeaderboardRejectsEmptyRunID(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonstrict", true: "strict"}[strict], func(t *testing.T) {
			root := t.TempDir()
			input := writeLeaderboardReport(t, filepath.Join(root, "empty-run.json"), "", 100)
			output := filepath.Join(root, "baseline.json")
			args := []string{"--lang", "en", "leaderboard", "--output", output}
			if strict {
				args = append(args, "--strict")
			}
			args = append(args, input)
			status, stdout, stderr := invokeAppMain(args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "Run.ID") {
				t.Fatalf("empty Run.ID status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("empty Run.ID wrote output: %v", err)
			}
		})
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
	uniqueReport.Run.ID = "unique-submission-run"
	setMetric(&uniqueReport, 300)
	first := writeLeaderboardSubmission(t, filepath.Join(root, "first.json"), firstReport)
	duplicate := writeLeaderboardSubmission(t, filepath.Join(root, "duplicate.json"), firstReport)
	unique := writeLeaderboardSubmission(t, filepath.Join(root, "unique.json"), uniqueReport)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--output", output, first, first, duplicate, unique)
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

func TestLeaderboardHelp(t *testing.T) {
	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--help")
	if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs leaderboard") {
		t.Fatalf("leaderboard help status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestLeaderboardRejectsUnknownFlag(t *testing.T) {
	status, stdout, stderr := invokeAppMain("--lang", "en", "leaderboard", "--unknown")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("leaderboard unknown flag status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}
