package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestDiskProbeMissingFIOEmitsStableResult(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	result := (diskProbe{}).Run(context.Background(), Environment{})

	if result.ID != "disk" || result.Title != "module.disk.title" || result.Description != "probe.disk.description" || result.Status != model.StatusWarning {
		t.Fatalf("disk missing result identity/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.disk.profile" || result.Methodology.ComparisonScope != "probe.disk.comparison_scope" {
		t.Fatalf("disk missing methodology = %+v", result.Methodology)
	}
	if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != len(fioJobPlan()) {
		t.Fatalf("disk missing evidence = %+v", result.Evidence)
	}
	if len(result.Failures) != 1 || result.Failures[0].Category != model.FailureToolMissing || result.Failures[0].Stage != "tool_lookup" || result.Failures[0].Message == "" {
		t.Fatalf("disk missing failure = %+v", result.Failures)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.disk.summary.tool_missing" {
		t.Fatalf("disk missing summary = %+v", result.SummaryMessages)
	}
	for _, note := range result.Notes {
		if !strings.HasPrefix(note, "probe.disk.note.") {
			t.Fatalf("disk missing note is not stable: %q", note)
		}
	}
}

func TestDiskProducerAssemblesStableMetadataAndStructuredStatus(t *testing.T) {
	result := newDiskResult()
	result.Fields = []model.Field{{Key: "engine", Label: diskFieldLabel("engine"), Value: model.RawValue("fio")}}
	if !appendFIOBaseMeasurement(&result, "fio_sequential_write_mib_s", 2, "MiB/s", "fixture") {
		t.Fatal("fixture base measurement was rejected")
	}
	appendCrystalMatrix(&result, completeCrystalJobs(), fioEngine{Name: "io_uring", AsyncQueue: true}, nil)
	result.TextBlocks = []model.TextBlock{{Title: "probe.disk.raw_output", Language: "text", Content: "raw fio diagnostic"}}
	result.Sources = []model.Source{{Name: "fio", Purpose: "probe.disk.source.fio"}}
	result.Evidence = model.NewEvidence(1, 2, "job")

	finalizeDiskResult(&result)

	if result.Title != "module.disk.title" || result.Description != "probe.disk.description" || result.Methodology.Engine != "fio" {
		t.Fatalf("disk stable identity = %+v", result)
	}
	if result.Fields[0].Label != "probe.disk.field.engine" || result.Measurements[0].Label != "probe.disk.metric.fio_sequential_write_mib_s" {
		t.Fatalf("disk stable metadata = fields:%+v measurements:%+v", result.Fields, result.Measurements)
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.disk.table.crystal" || result.Tables[0].Columns[0].Label != "probe.disk.column.workload" {
		t.Fatalf("disk stable table metadata = %+v", result.Tables)
	}
	status, ok := result.Tables[0].Rows[0][6].Key()
	if !ok || status != "probe.disk.status.complete" {
		t.Fatalf("disk table status = %#v", result.Tables[0].Rows[0][6])
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.disk.summary.values" || !strings.Contains(result.SummaryMessages[0].Args[0], "write=2.00 MiB/s") {
		t.Fatalf("disk stable summary = %+v", result.SummaryMessages)
	}
	if !containsDiskNote(result.Notes, "probe.disk.note.partial_results") {
		t.Fatalf("disk stable notes = %v", result.Notes)
	}
}

func TestDiskProducerFailureUsesStructuredDiagnosticAndStableSummary(t *testing.T) {
	result := newDiskResult()
	result.Status = model.StatusError
	result.AddFailure(model.Failure{Category: model.FailureParse, Stage: "parse", Target: "fio", Message: "fio JSON malformed: raw diagnostic"})
	result.Evidence = model.NewEvidence(0, 1, "job")
	finalizeDiskResult(&result)

	if len(result.Failures) != 1 || result.Failures[0].Message != "fio JSON malformed: raw diagnostic" {
		t.Fatalf("disk failure diagnostic changed: %+v", result.Failures)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.disk.summary.none" {
		t.Fatalf("disk failure summary = %+v", result.SummaryMessages)
	}
	if !containsDiskNote(result.Notes, "probe.disk.note.run_failure") {
		t.Fatalf("disk failure notes = %v", result.Notes)
	}
}

func TestDiskProducerReportsFIOJobErrorStructurally(t *testing.T) {
	directory := t.TempDir()
	fioPath := filepath.Join(directory, "fio")
	script := `#!/bin/sh
if [ "$1" = "--enghelp" ]; then
  printf '%s\n' 'psync'
  exit 0
fi
printf '%s\n' '{"fio version":"fio-fixture","jobs":[{"jobname":"seqwrite","write":{"io_bytes":1,"bw_bytes":2097152}},{"jobname":"seqread","read":{"io_bytes":1,"bw_bytes":2097152}},{"jobname":"randread","read":{"io_bytes":1,"iops":10}},{"jobname":"randwrite","write":{"io_bytes":1,"iops":10}},{"jobname":"fixture-error","error":7}]}'
`
	if err := os.WriteFile(fioPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result := runFIODisk(context.Background(), Environment{Config: config.Runtime{
		DiskPath: t.TempDir(), DiskMiB: 128,
	}}, fioPath)
	if result.Status != model.StatusWarning || len(result.Failures) != 1 {
		t.Fatalf("fio job error status/failures = %s/%+v", result.Status, result.Failures)
	}
	failure := result.Failures[0]
	if failure.Category != model.FailureUnknown || failure.Stage != "benchmark_run" || failure.Target != "fixture-error" || failure.Count != 1 || !strings.Contains(failure.Message, "error code 7") {
		t.Fatalf("fio job error failure = %+v", failure)
	}
}

func completeCrystalJobs() map[string]fioJob {
	jobs := make(map[string]fioJob, len(crystalJobSpecs()))
	for _, spec := range crystalJobSpecs() {
		job := fioJob{Name: spec.Name}
		if spec.Direction == "read" {
			job.Read = fioDirection{BWBytes: 2 * 1024 * 1024, IOPS: 10}
		} else {
			job.Write = fioDirection{BWBytes: 4 * 1024 * 1024, IOPS: 20}
		}
		jobs[spec.Name] = job
	}
	return jobs
}

func containsDiskNote(notes []string, want string) bool {
	for _, note := range notes {
		if note == want {
			return true
		}
	}
	return false
}
