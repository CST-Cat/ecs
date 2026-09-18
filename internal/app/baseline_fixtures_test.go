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
	start := time.Unix(1700000000, 0).UTC()
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run: model.RunInfo{
			ID: "app-submit-fixture", Profile: "full", StartedAt: start, CompletedAt: start.Add(2 * time.Second),
			DurationMS: 2000, Exposure: "local", Redacted: true,
			Requested: []string{"system", "cpu"}, OutputFormats: []string{"json"},
		},
		Summary: model.Summary{Status: model.StatusOK, OK: 2, Messages: []model.Message{model.NewMessage("message.summary.allOK", 2)}},
		Results: []model.Result{{
			ID: "cpu", Title: "module.cpu.title", Status: model.StatusOK, StartedAt: start, DurationMS: 1000,
			Methodology: model.Methodology{
				Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "sysbench",
				Profile: "probe.cpu.profile", ComparisonScope: "probe.cpu.comparison_scope",
				Parameters: map[string]string{"scope_revision": "1", "workload": "sysbench"},
			},
			Measurements: []model.Measurement{
				{Key: "sysbench_cpu_single_events_s", Label: "probe.cpu.metric.single_events_s", Value: 900, Unit: "events/s", Display: model.RawValue("900 events/s"), Method: "sysbench-cpu-fixture-v1", HigherIsBetter: model.BoolPtr(true)},
				{Key: "sysbench_cpu_multi_events_s", Label: "probe.cpu.metric.multi_events_s", Value: 3400, Unit: "events/s", Display: model.RawValue("3400 events/s"), Method: "sysbench-cpu-fixture-v1", HigherIsBetter: model.BoolPtr(true)},
			},
		}},
	}
	report.Results = append([]model.Result{{
		ID: "system", Title: "module.system.title", Status: model.StatusOK, StartedAt: start, DurationMS: 1000,
		Methodology: model.Methodology{
			Kind: "inventory", Label: "methodology.inventory", Engine: "system-inventory",
			Profile: "probe.system.profile", ComparisonScope: "probe.system.comparison_scope",
			Parameters: map[string]string{"scope_revision": "1", "workload": "inventory"},
		},
		Measurements: []model.Measurement{
			{Key: "logical_cpus", Label: "probe.system.metric.logical_cpus", Value: 4, Unit: "count", Display: model.RawValue("4"), Method: "system-inventory-fixture-v1", HigherIsBetter: model.BoolPtr(true)},
			{Key: "memory_total_bytes", Label: "probe.system.metric.memory_total_bytes", Value: 8 * (1 << 30), Unit: "bytes", Display: model.RawValue("8589934592"), Method: "system-inventory-fixture-v1", HigherIsBetter: model.BoolPtr(true)},
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
