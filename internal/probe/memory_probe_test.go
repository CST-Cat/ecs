package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestMemoryProbeMissingStreamReturnsStableResult(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	result := (memoryProbe{}).Run(context.Background(), Environment{})
	if result.ID != "memory" || result.Title != "module.memory.title" || result.Description != "probe.memory.description" {
		t.Fatalf("missing STREAM metadata = %+v", result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.memory.stream.profile" || result.Methodology.ComparisonScope != "probe.memory.comparison_scope" {
		t.Fatalf("missing STREAM methodology = %+v", result.Methodology)
	}
	if result.Status != model.StatusWarning || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.memory.stream_missing" {
		t.Fatalf("missing STREAM status/summary = %s/%+v", result.Status, result.SummaryMessages)
	}
	if len(result.Failures) != 1 || result.Failures[0].Category != model.FailureToolMissing || result.Failures[0].Stage != "tool_lookup" || result.Failures[0].Target != "stream" {
		t.Fatalf("missing STREAM failures = %+v", result.Failures)
	}
	if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected < 1 || result.Evidence.Unit != "run" {
		t.Fatalf("missing STREAM evidence = %+v", result.Evidence)
	}
	if result.StartedAt.IsZero() {
		t.Fatal("missing STREAM result was not finished by memoryProbe.Run")
	}
	if !containsString(result.Notes, "probe.memory.stream_missing") {
		t.Fatalf("missing STREAM notes = %v", result.Notes)
	}
}

func TestStreamProducerFailureEmitsStableResultDirectly(t *testing.T) {
	allowance := cpuAllowance{Visible: 1, Threads: 1, Source: "fixture"}
	result := runStreamMemoryWithAllowance(context.Background(), Environment{}, "/bin/false", allowance)

	if result.Title != "module.memory.title" || result.Description != "probe.memory.description.single_core" {
		t.Fatalf("STREAM failure metadata = %+v", result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.memory.stream.profile.single_core" || result.Methodology.ComparisonScope != "probe.memory.comparison_scope" {
		t.Fatalf("STREAM failure methodology = %+v", result.Methodology)
	}
	if result.Status != model.StatusWarning || len(result.Failures) != 1 || result.Failures[0].Stage != "stream_1t" || result.Failures[0].Target != "1T" || strings.TrimSpace(result.Failures[0].Message) == "" {
		t.Fatalf("STREAM failure status/evidence = %s/%+v", result.Status, result.Failures)
	}
	if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 1 || result.Evidence.Unit != "run" {
		t.Fatalf("STREAM failure evidence counters = %+v", result.Evidence)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.memory.stream.summary.none" {
		t.Fatalf("STREAM failure summary = %+v", result.SummaryMessages)
	}
	if !containsString(result.Notes, "probe.memory.stream.note.run_failed.1t") || !containsString(result.Notes, "probe.memory.stream.note.single_core") {
		t.Fatalf("STREAM failure notes = %v", result.Notes)
	}
	for _, table := range result.Tables {
		if table.Key == "" || len(table.Columns) != 5 || len(table.Rows) != 8 {
			t.Fatalf("STREAM failure table shape = %+v", table)
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				t.Fatalf("STREAM failure row width = %d/%d", len(row), len(table.Columns))
			}
		}
	}
	for _, field := range result.Fields {
		if field.Key == "cpu_allowance" {
			if raw, ok := field.Value.Raw(); !ok || raw != "visible=1;quota=unlimited" {
				t.Fatalf("STREAM failure cpu allowance = %+v", field.Value)
			}
			return
		}
	}
	t.Fatal("STREAM failure omitted cpu_allowance field")
}

func TestStreamProducerSuccessfulMultiThreadResult(t *testing.T) {
	path, logPath := writeOfficialStreamFixture(t)
	t.Setenv("MEMORY_STREAM_LOG", logPath)

	result := runStreamMemoryWithAllowance(context.Background(), Environment{}, path, cpuAllowance{Visible: 8, Threads: 4, Source: "fixture"})
	if result.Status != model.StatusOK || result.Title != "module.memory.title" || result.Description != "probe.memory.description" {
		t.Fatalf("STREAM successful metadata/status = %s/%+v", result.Status, result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.memory.stream.profile" || result.Methodology.ComparisonScope != "probe.memory.comparison_scope" {
		t.Fatalf("STREAM successful methodology = %+v", result.Methodology)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("STREAM successful failures = %+v", result.Failures)
	}
	if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 2 || result.Evidence.Unit != "run" {
		t.Fatalf("STREAM successful evidence = %+v", result.Evidence)
	}
	assertProducerParameterScope(t, result, "tool_version", "threads", "kernel_order")
	parameters := result.Methodology.Parameters
	if parameters["tool_version"] != "STREAM version 5.10" || parameters["threads"] != "1 / 4" || parameters["kernel_order"] != "Copy / Scale / Add / Triad" {
		t.Fatalf("STREAM comparison parameters = %v", parameters)
	}
	if got, err := os.ReadFile(logPath); err != nil || strings.TrimSpace(string(got)) != "1\n4" {
		t.Fatalf("STREAM physical invocations = %q (err=%v)", got, err)
	}

	wantMeasurements := []string{
		"stream_copy_1t_mib_s", "stream_copy_nt_mib_s",
		"stream_triad_1t_mib_s", "stream_triad_nt_mib_s",
		"stream_scale_1t_mib_s", "stream_scale_nt_mib_s",
		"stream_add_1t_mib_s", "stream_add_nt_mib_s",
		"stream_copy_scaling_ratio", "stream_scale_scaling_ratio",
		"stream_add_scaling_ratio", "stream_triad_scaling_ratio",
	}
	if len(result.Measurements) != len(wantMeasurements) {
		t.Fatalf("STREAM successful measurements = %d, want %d", len(result.Measurements), len(wantMeasurements))
	}
	for index, measurement := range result.Measurements {
		if measurement.Key != wantMeasurements[index] || measurement.Value <= 0 {
			t.Fatalf("STREAM measurement %d = %+v, want key %q and positive value", index, measurement, wantMeasurements[index])
		}
		if _, ok := measurement.Display.Raw(); !ok {
			t.Fatalf("STREAM measurement %q display is not raw: %+v", measurement.Key, measurement.Display)
		}
		if _, ok := measurement.Display.Key(); ok {
			t.Fatalf("STREAM measurement %q display unexpectedly has key variant", measurement.Key)
		}
	}
	if result.Measurements[0].Label != "probe.memory.stream.metric.copy.1t" || result.Measurements[1].Label != "probe.memory.stream.metric.copy.nt" || result.Measurements[8].Label != "probe.memory.stream.metric.copy.scaling" {
		t.Fatalf("STREAM measurement labels = %+v", result.Measurements)
	}

	for _, key := range []string{"engine", "version", "threads", "cpu_allowance", "kernel_order", "thread_control", "rate_unit"} {
		field := findMemoryField(result, key)
		if field.Key == "" {
			t.Fatalf("STREAM successful field %q is missing", key)
		}
		if _, ok := field.Value.Raw(); !ok {
			t.Fatalf("STREAM successful field %q is not raw: %+v", key, field.Value)
		}
		if _, ok := field.Value.Key(); ok {
			t.Fatalf("STREAM successful field %q unexpectedly has key variant", key)
		}
	}
	if field := findMemoryField(result, "engine"); field.Label != "probe.memory.stream.field.engine" {
		t.Fatalf("STREAM engine field label = %q", field.Label)
	}

	if len(result.TextBlocks) != 2 || result.TextBlocks[0].Title != "probe.memory.stream.raw.1t" || result.TextBlocks[1].Title != "probe.memory.stream.raw.nt" {
		t.Fatalf("STREAM successful raw blocks = %+v", result.TextBlocks)
	}
	for _, block := range result.TextBlocks {
		if !strings.Contains(block.Content, "STREAM version 5.10") {
			t.Fatalf("STREAM raw block omitted fixture output: %+v", block)
		}
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.memory.stream.summary.values" || len(result.SummaryMessages[0].Args) != 1 {
		t.Fatalf("STREAM successful summary = %+v", result.SummaryMessages)
	}
	if !containsString(result.Notes, "probe.memory.stream.note.separate_runs") || !containsString(result.Notes, "probe.memory.stream.note.units_normalized") {
		t.Fatalf("STREAM successful notes = %v", result.Notes)
	}

	if len(result.Tables) != 2 {
		t.Fatalf("STREAM successful tables = %d, want 2", len(result.Tables))
	}
	bandwidth := result.Tables[0]
	if bandwidth.Title != "probe.memory.stream.table.bandwidth" || len(bandwidth.Columns) != 5 || len(bandwidth.Rows) != 8 {
		t.Fatalf("STREAM bandwidth table metadata = %+v", bandwidth)
	}
	if bandwidth.Columns[1].Key != "best_rate_mibs" || !bandwidth.Columns[1].Numeric || !bandwidth.Columns[1].HigherIsBetter {
		t.Fatalf("STREAM bandwidth numeric column = %+v", bandwidth.Columns[1])
	}
	if _, ok := bandwidth.Rows[0][0].Raw(); !ok {
		t.Fatalf("STREAM bandwidth row identity is not raw: %+v", bandwidth.Rows[0][0])
	}
	if key, ok := bandwidth.Rows[0][4].Key(); !ok || key != "probe.memory.stream.evidence.best_rate" {
		t.Fatalf("STREAM bandwidth evidence variant = %+v", bandwidth.Rows[0][4])
	}
	if _, ok := bandwidth.Rows[0][4].Raw(); ok {
		t.Fatalf("STREAM bandwidth evidence unexpectedly has raw variant")
	}
	for tableIndex, table := range result.Tables {
		for rowIndex, row := range table.Rows {
			if len(row) != len(table.Columns) {
				t.Fatalf("STREAM table %d row %d width = %d/%d", tableIndex, rowIndex, len(row), len(table.Columns))
			}
		}
	}
}

func TestStreamProducerSuccessfulSingleCoreReuse(t *testing.T) {
	path, logPath := writeOfficialStreamFixture(t)
	t.Setenv("MEMORY_STREAM_LOG", logPath)

	result := runStreamMemoryWithAllowance(context.Background(), Environment{}, path, cpuAllowance{Visible: 1, Threads: 1, Source: "fixture"})
	if result.Status != model.StatusOK || result.Description != "probe.memory.description.single_core" {
		t.Fatalf("STREAM single-core status/description = %s/%q", result.Status, result.Description)
	}
	if result.Methodology.Profile != "probe.memory.stream.profile.single_core" {
		t.Fatalf("STREAM single-core methodology profile = %q", result.Methodology.Profile)
	}
	if got, err := os.ReadFile(logPath); err != nil || strings.TrimSpace(string(got)) != "1" {
		t.Fatalf("STREAM single-core physical invocations = %q (err=%v)", got, err)
	}
	if result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 1 || result.Evidence.Unit != "run" {
		t.Fatalf("STREAM single-core evidence = %+v", result.Evidence)
	}
	if len(result.Measurements) != 8 {
		t.Fatalf("STREAM single-core measurements = %d, want 8", len(result.Measurements))
	}
	for _, measurement := range result.Measurements {
		if strings.Contains(measurement.Key, "scaling") {
			t.Fatalf("STREAM single-core has scaling measurement: %+v", measurement)
		}
	}
	if result.Measurements[0].Value != result.Measurements[1].Value || result.Measurements[0].Display.Text() != result.Measurements[1].Display.Text() {
		t.Fatalf("STREAM single-core 1T/NT values were not reused: %+v / %+v", result.Measurements[0], result.Measurements[1])
	}
	if len(result.TextBlocks) != 1 || result.TextBlocks[0].Title != "probe.memory.stream.raw.1t" {
		t.Fatalf("STREAM single-core raw blocks = %+v", result.TextBlocks)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.memory.stream.summary.values" {
		t.Fatalf("STREAM single-core summary = %+v", result.SummaryMessages)
	}
	if !containsString(result.Notes, "probe.memory.stream.note.single_core") || !containsString(result.Notes, "probe.memory.stream.note.units_normalized") || containsString(result.Notes, "probe.memory.stream.note.separate_runs") {
		t.Fatalf("STREAM single-core notes = %v", result.Notes)
	}

	bandwidth := result.Tables[0]
	if key, ok := bandwidth.Rows[1][4].Key(); !ok || key != "probe.memory.stream.evidence.reused" {
		t.Fatalf("STREAM single-core reused evidence = %+v", bandwidth.Rows[1][4])
	}
	if bandwidth.Rows[1][0].Text() != "Copy / NT(1T-reused)" {
		t.Fatalf("STREAM single-core reused row identity = %q", bandwidth.Rows[1][0].Text())
	}
}

func writeOfficialStreamFixture(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "stream")
	logPath := filepath.Join(directory, "invocations.log")
	script := `#!/bin/sh
# STREAM version
# Number of Threads requested
# Best Rate
# Function
printf '%s\n' "$OMP_NUM_THREADS" >> "$MEMORY_STREAM_LOG"
case "$OMP_NUM_THREADS" in
1)
  copy=1000
  scale=800
  add=700
  triad=900
  ;;
4)
  copy=4000
  scale=3200
  add=2800
  triad=3600
  ;;
*)
  exit 2
  ;;
esac
cat <<EOF
STREAM version 5.10
Number of Threads requested = $OMP_NUM_THREADS
Function        Best Rate MB/s     Avg time     Min time     Max time
Copy:           $copy             1.000000     0.900000     1.100000
Scale:          $scale            2.000000     1.900000     2.100000
Add:            $add              3.000000     2.900000     3.100000
Triad:          $triad            4.000000     3.900000     4.100000
EOF
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if !IsOfficialStreamBinary(path) {
		t.Fatal("STREAM fixture did not pass official binary marker check")
	}
	return path, logPath
}

func findMemoryField(result model.Result, key string) model.Field {
	for _, field := range result.Fields {
		if field.Key == key {
			return field
		}
	}
	return model.Field{}
}
