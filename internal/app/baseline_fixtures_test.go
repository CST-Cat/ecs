package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func submitTestReport() model.Report {
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: "app-submit-fixture", Profile: "full", StartedAt: time.Unix(1700000000, 0).UTC()},
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
	submission, err := score.BuildSubmission(newApplication().modules, submitTestReport(), score.SubmissionOptions{
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
	report.Run.ID = filepath.Base(path)
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
	submission, err := score.BuildSubmission(newApplication().modules, report, score.SubmissionOptions{Region: "us", Provider: "fixture"})
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
