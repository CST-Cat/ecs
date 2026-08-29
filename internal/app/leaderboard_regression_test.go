package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func TestLeaderboardRegressionDeduplicatesPathsAndRunIDsWithStatistics(t *testing.T) {
	root := t.TempDir()
	first := writeLeaderboardReport(t, filepath.Join(root, "a.json"), "copied-run", 100)
	copy := writeLeaderboardReport(t, filepath.Join(root, "copy.json"), "copied-run", 900)
	unique := writeLeaderboardReport(t, filepath.Join(root, "b.json"), "unique-run", 300)
	output := filepath.Join(root, "baseline.json")

	status, stdout, stderr := invokeAppMain(
		"leaderboard", "--lang", "en", "--output", output,
		first, copy, first, unique,
	)
	if status != 0 || !strings.Contains(stdout, "written") {
		t.Fatalf("duplicate regression status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if duplicates := strings.Count(strings.ToLower(stderr), "duplicate"); duplicates != 2 {
		t.Fatalf("duplicate regression warnings=%d stderr=%q, want path and Run.ID warnings", duplicates, stderr)
	}

	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("duplicate regression output is not loadable: %v", err)
	}
	if baseline.Schema != score.BaselineSchema || baseline.SampleCount != 2 {
		t.Fatalf("duplicate regression metadata = %+v", baseline)
	}
	if got := baseline.Metrics["cpu_single"]; got != 200 {
		t.Fatalf("duplicate regression mean = %v, want 200", got)
	}
	if len(baseline.ScoreSamples) != 2 || baseline.ScoreSamples[0] != 750 || baseline.ScoreSamples[1] != 1250 {
		t.Fatalf("duplicate regression score samples = %v, want [750 1250]", baseline.ScoreSamples)
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].VCPUMin != 4 || baseline.Tiers[0].SampleCount != 2 ||
		baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 {
		t.Fatalf("duplicate regression tier = %+v", baseline.Tiers)
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"metric_sample_counts"`) {
		t.Fatalf("generated baseline omitted current tier sample counts: %s", encoded)
	}
	if !strings.Contains(stdout, "Leaderboard statistics from 2 reports") ||
		!strings.Contains(stdout, "cpu_single") || !strings.Contains(stdout, "200.00") ||
		!strings.Contains(stdout, "2 samples") {
		t.Fatalf("duplicate regression output omitted aggregate statistics: %q", stdout)
	}
}

func TestLeaderboardRegressionAcceptsMixedCurrentReportAndSubmissionBySchema(t *testing.T) {
	root := t.TempDir()
	fullPath := writeLeaderboardReport(t, filepath.Join(root, "full.json"), "full-run", 100)
	submissionReport := submitTestReport()
	for resultIndex := range submissionReport.Results {
		for measurementIndex := range submissionReport.Results[resultIndex].Measurements {
			measurement := &submissionReport.Results[resultIndex].Measurements[measurementIndex]
			if measurement.Key == "sysbench_cpu_single_events_s" {
				measurement.Value = 300
			}
		}
	}
	submissionPath := writeLeaderboardSubmission(t, filepath.Join(root, "submission.json"), submissionReport)
	output := filepath.Join(root, "baseline.json")

	if _, err := reporter.LoadJSON(fullPath); err != nil {
		t.Fatalf("full current report did not load as report: %v", err)
	}
	if _, err := score.LoadSubmission(fullPath); err == nil {
		t.Fatal("full report was accepted as an ecs.submission/v1 artifact")
	}
	if _, err := score.LoadSubmission(submissionPath); err != nil {
		t.Fatalf("current submission did not load as submission: %v", err)
	}
	if _, err := reporter.LoadJSON(submissionPath); err == nil {
		t.Fatal("submission was accepted as an ecs.report/v1 artifact")
	}

	status, stdout, stderr := invokeAppMain(
		"leaderboard", "--lang", "en", "--output", output, fullPath, submissionPath,
	)
	if status != 0 || stdout == "" || stderr != "" {
		t.Fatalf("mixed input status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("mixed input output is not loadable: %v", err)
	}
	if baseline.SampleCount != 2 || baseline.Metrics["cpu_single"] != 200 {
		t.Fatalf("mixed input baseline = %+v", baseline)
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != 2 ||
		baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 {
		t.Fatalf("mixed input tier statistics = %+v", baseline.Tiers)
	}
}

func TestLeaderboardRegressionReportsActiveAndFallbackStatistics(t *testing.T) {
	root := t.TempDir()
	inputs := make([]string, 0, 9)
	for index := 0; index < 5; index++ {
		inputs = append(inputs, writeLeaderboardReportWithVCPU(t,
			filepath.Join(root, fmt.Sprintf("tier4-%d.json", index)),
			fmt.Sprintf("tier4-%d", index), 4, 100+float64(index)))
	}
	for index := 0; index < 4; index++ {
		inputs = append(inputs, writeLeaderboardReportWithVCPU(t,
			filepath.Join(root, fmt.Sprintf("tier8-%d.json", index)),
			fmt.Sprintf("tier8-%d", index), 8, 300+float64(index)))
	}
	output := filepath.Join(root, "baseline.json")

	args := []string{"leaderboard", "--lang", "en", "--output", output}
	args = append(args, inputs...)
	status, stdout, stderr := invokeAppMain(args...)
	if status != 0 || stderr != "" {
		t.Fatalf("tier statistics status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("tier statistics output is not loadable: %v", err)
	}
	if len(baseline.Tiers) != 2 {
		t.Fatalf("tier statistics tiers = %+v", baseline.Tiers)
	}
	if baseline.Tiers[0].VCPUMin != 4 || baseline.Tiers[0].SampleCount != 5 ||
		baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 5 {
		t.Fatalf("active tier statistics = %+v", baseline.Tiers[0])
	}
	if baseline.Tiers[1].VCPUMin != 8 || baseline.Tiers[1].SampleCount != 4 ||
		baseline.Tiers[1].MetricSampleCounts["cpu_single"] != 4 {
		t.Fatalf("fallback tier statistics = %+v", baseline.Tiers[1])
	}
	wantGlobal := 1716.0 / 9
	if math.Abs(baseline.Metrics["cpu_single"]-wantGlobal) > 1e-9 {
		t.Fatalf("global metric mean = %v, want %v", baseline.Metrics["cpu_single"], wantGlobal)
	}
	activeMetrics, activeMin, activeSamples := baseline.MetricsForHost(4)
	if activeMin != 4 || activeSamples != 5 || activeMetrics["cpu_single"] != 102 {
		t.Fatalf("active tier reference = %v, tier=%d samples=%d", activeMetrics, activeMin, activeSamples)
	}
	fallbackMetrics, fallbackMin, fallbackSamples := baseline.MetricsForHost(8)
	if fallbackMin != 0 || fallbackSamples != 9 || math.Abs(fallbackMetrics["cpu_single"]-wantGlobal) > 1e-9 {
		t.Fatalf("fallback tier reference = %v, tier=%d samples=%d", fallbackMetrics, fallbackMin, fallbackSamples)
	}
	findTierLine := func(label string) string {
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, label) {
				return line
			}
		}
		return ""
	}
	activeLine := findTierLine("4–7 vCPU")
	if !strings.Contains(activeLine, "5 samples") || !strings.Contains(activeLine, "active") {
		t.Fatalf("active tier output line = %q", activeLine)
	}
	fallbackLine := findTierLine("8–15 vCPU")
	if !strings.Contains(fallbackLine, "4 samples") || !strings.Contains(fallbackLine, "too few samples") || !strings.Contains(fallbackLine, "needs 5") {
		t.Fatalf("fallback tier output line = %q", fallbackLine)
	}
	if !strings.Contains(stdout, "cpu_single") || !strings.Contains(stdout, "190.67") || !strings.Contains(stdout, "9 samples") {
		t.Fatalf("global metric sample count missing from output: %q", stdout)
	}
}

func TestLeaderboardRegressionPreservesUndecidableOutlierStatistics(t *testing.T) {
	root := t.TempDir()
	values := []float64{98, 99, 100, 101, 102, 103, 104, 1000}
	inputs := make([]string, 0, len(values))
	submissions := make([]score.Submission, 0, len(values))
	for index, value := range values {
		var multi *float64
		if index == 0 {
			multiValue := 3400.0
			multi = &multiValue
		}
		path := writeOutlierSubmissionFixture(t, filepath.Join(root, fmt.Sprintf("submission-%d.json", index)), value, multi)
		inputs = append(inputs, path)
		submission, err := score.LoadSubmission(path)
		if err != nil {
			t.Fatal(err)
		}
		submissions = append(submissions, submission)
	}
	expected := score.DetectOutliers(submissions)
	if len(expected.Outliers) == 0 || len(expected.Undecidable) == 0 {
		t.Fatalf("fixture did not produce both outlier states: %+v", expected)
	}
	output := filepath.Join(root, "baseline.json")
	args := []string{"leaderboard", "--lang", "en", "--annotate", "--verbose", "--output", output}
	args = append(args, inputs...)
	status, stdout, stderr := invokeAppMain(args...)
	if status != 0 || stderr != "" {
		t.Fatalf("undecidable statistics status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("undecidable statistics output is not loadable: %v", err)
	}
	if baseline.SampleCount != len(values) || len(baseline.Tiers) != 1 || baseline.Tiers[0].SampleCount != len(values) ||
		baseline.Tiers[0].MetricSampleCounts["cpu_single"] != len(values) {
		t.Fatalf("undecidable statistics baseline = %+v", baseline)
	}
	if baseline.Metrics["cpu_single"] != 1707.0/8 {
		t.Fatalf("undecidable statistics mean = %v, want %v", baseline.Metrics["cpu_single"], 1707.0/8)
	}
	if !strings.Contains(stdout, "Combinations left unjudged for lack of samples:") {
		t.Fatalf("undecidable output header missing: %q", stdout)
	}
	if warnings := strings.Count(stdout, "::warning::"); warnings != len(expected.Outliers) {
		t.Fatalf("annotated outlier count=%d, want %d", warnings, len(expected.Outliers))
	}
	for _, notice := range expected.Undecidable {
		if !strings.Contains(stdout, notice) {
			t.Fatalf("undecidable notice %q missing from output", notice)
		}
	}
}
