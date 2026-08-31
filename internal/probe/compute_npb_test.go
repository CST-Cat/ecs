package probe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func npbOutput(spec npbBenchmarkSpec, threads int, mops float64) string {
	perThread := mops / float64(threads)
	return fmt.Sprintf(`

 NAS Parallel Benchmarks (NPB3.4-OMP) - %s Benchmark
 Number of available threads:                     %d
 %s Benchmark Completed.
 Class           =                        A
 Size            =                        %s
 Iterations      =                        %d
 Time in seconds =                      1.25
 Total threads   =                        %d
 Avail threads   =                        %d
 Mop/s total     =                   %.2f
 Mop/s/thread    =                   %.2f
 Operation type  = %s
 Verification    =               SUCCESSFUL
 Version         =                      3.4.4
 Compile options:
    FC           = gfortran
    FLINK        = gfortran
    F_LIB        = (none)
    F_INC        = (none)
    FFLAGS       = -O3 -fopenmp -static
    FLINKFLAGS   = -O3 -fopenmp -static
    RAND         = randi8
`, spec.Name, threads, spec.Name, spec.ExpectedSize, spec.ExpectedIters, threads, threads, mops, perThread, spec.ExpectedOp)
}

func TestParseNPBOutputAndFailureCategories(t *testing.T) {
	spec := npbBenchmarkSpecs[0]
	output := npbOutput(spec, 2, 100)
	sample, err := parseNPBBenchmarkOutput(output, spec, 2)
	if err != nil || sample.Benchmark != spec.Name || sample.Size != spec.ExpectedSize || sample.Threads != 2 || sample.MOPS != 100 || sample.MOPSPerThread != 50 {
		t.Fatalf("parsed NPB sample = %+v/%v", sample, err)
	}
	for _, test := range []struct {
		name, output, marker string
	}{
		{name: "official header", output: strings.Replace(output, "NAS Parallel Benchmarks (NPB3.4-OMP) - EP Benchmark", "broken", 1), marker: "官方 benchmark header"},
		{name: "class", output: strings.Replace(output, "Class           =                        A", "Class           =                        B", 1), marker: "Class"},
		{name: "size", output: strings.Replace(output, spec.ExpectedSize, "wrong-size", 1), marker: "Size"},
		{name: "iterations", output: strings.Replace(output, fmt.Sprintf("Iterations      =                        %d", spec.ExpectedIters), "Iterations      =                        1", 1), marker: "Iterations"},
		{name: "time", output: strings.Replace(output, "Time in seconds =                      1.25", "Time in seconds =                      0", 1), marker: "Time in seconds"},
		{name: "total threads", output: strings.Replace(output, "Total threads   =                        2", "Total threads   =                        1", 1), marker: "实际线程"},
		{name: "available threads", output: strings.Replace(output, "Avail threads   =                        2", "Avail threads   =                        1", 1), marker: "available threads"},
		{name: "total mops", output: strings.Replace(output, "100.00", "0.00", 1), marker: "Mop/s total"},
		{name: "per-thread mops", output: strings.Replace(output, "50.00", "0.00", 1), marker: "Mop/s/thread"},
		{name: "operation", output: strings.Replace(output, spec.ExpectedOp, "wrong operation", 1), marker: "Operation type"},
		{name: "verification", output: strings.Replace(output, "SUCCESSFUL", "FAILED", 1), marker: "Verification"},
		{name: "version", output: strings.Replace(output, "3.4.4", "3.4.3", 1), marker: "Version"},
		{name: "compiler", output: strings.Replace(output, "FC           = gfortran", "FC           = ifort", 1), marker: "FC"},
		{name: "linker", output: strings.Replace(output, "FLINK        = gfortran", "FLINK        = ld", 1), marker: "FLINK"},
		{name: "compile flags", output: strings.Replace(output, "FFLAGS       = -O3 -fopenmp -static", "FFLAGS       = -O2", 1), marker: "FFLAGS"},
		{name: "link flags", output: strings.Replace(output, "FLINKFLAGS   = -O3 -fopenmp -static", "FLINKFLAGS   = -O2", 1), marker: "FLINKFLAGS"},
		{name: "random", output: strings.Replace(output, "RAND         = randi8", "RAND         = rand", 1), marker: "RAND"},
		{name: "thread warning", output: output + "\nWarning: Threads used differ from threads available\n", marker: "线程使用"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseNPBBenchmarkOutput(test.output, spec, 2); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("NPB error = %v, want %q", err, test.marker)
			}
		})
	}
	environment := npbEnvironmentParameters(2)
	values := make(map[string]string)
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		if !ok || values[key] != "" {
			t.Fatalf("NPB environment duplicate/malformed = %v", environment)
		}
		values[key] = value
	}
	if values["OMP_NUM_THREADS"] != "2" || values["OMP_DYNAMIC"] != "FALSE" || values["NPB_TIMER_FLAG"] != "0" {
		t.Fatalf("NPB environment = %v", values)
	}
	if _, err := executeNPBBenchmark(context.Background(), "unused", spec, 0); err == nil || !strings.Contains(err.Error(), "线程数必须为正数") {
		t.Fatalf("NPB invalid execute input = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
		want model.FailureCategory
	}{
		{name: "cancelled", ctx: cancelled, err: fmt.Errorf("stopped"), want: model.FailureCanceled},
		{name: "timeout", ctx: deadline, err: fmt.Errorf("stopped"), want: model.FailureTimeout},
		{name: "permission", ctx: context.Background(), err: fmt.Errorf("permission denied"), want: model.FailurePermissionDenied},
		{name: "parse", ctx: context.Background(), err: fmt.Errorf("NPB 输出无效"), want: model.FailureParse},
		{name: "unknown", ctx: context.Background(), err: fmt.Errorf("other failure"), want: model.FailureUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := benchmarkFailureCategory(test.ctx, test.err); got != test.want {
				t.Fatalf("failure category = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNPBAssemblyAndEnvironment(t *testing.T) {
	runs := make(map[string][]npbBenchmarkSample, len(npbBenchmarkSpecs))
	for _, spec := range npbBenchmarkSpecs {
		first, err := parseNPBBenchmarkOutput(npbOutput(spec, 1, 100), spec, 1)
		if err != nil {
			t.Fatal(err)
		}
		second, err := parseNPBBenchmarkOutput(npbOutput(spec, 2, 200), spec, 2)
		if err != nil {
			t.Fatal(err)
		}
		runs[spec.Name] = []npbBenchmarkSample{first, second}
	}
	result := model.NewResult("npb", "npb")
	appendNPBMeasurements(&result, npbBenchmarkSpecs, runs, 2)
	for _, key := range []string{"npb_ep_1t_mops", "npb_ep_nt_mops", "npb_ep_scaling_ratio", "npb_ft_1t_mops", "npb_ft_nt_mops"} {
		if !hasMeasurement(result, key) {
			t.Fatalf("missing NPB measurement %q", key)
		}
	}
	table := npbResultsTable(npbBenchmarkSpecs, runs, 2)
	if table.Key != "benchmark.npb.results" || len(table.Columns) != 8 || len(table.Rows) != 4 || table.Rows[0][7].Text() != "probe.npb.verification.successful" {
		t.Fatalf("NPB result table = %+v", table)
	}
	if _, ok := table.Rows[0][1].Key(); !ok {
		t.Fatalf("NPB workload is not a tagged key: %#v", table.Rows[0][1])
	}
	if _, ok := table.Rows[0][7].Key(); !ok {
		t.Fatalf("NPB verification is not a tagged key: %#v", table.Rows[0][7])
	}
	singleRuns := map[string][]npbBenchmarkSample{}
	for _, spec := range npbBenchmarkSpecs {
		first := runs[spec.Name][0]
		singleRuns[spec.Name] = []npbBenchmarkSample{first, first}
	}
	single := npbResultsTable(npbBenchmarkSpecs, singleRuns, 1)
	if single.Rows[0][6].Text() != "na" {
		t.Fatalf("single-core NPB output = table:%v", single.Rows[0])
	}
	partialRuns := map[string][]npbBenchmarkSample{"EP": {runs["EP"][0], {}}}
	partialResult := model.NewResult("npb", "npb")
	appendNPBMeasurements(&partialResult, []npbBenchmarkSpec{npbBenchmarkSpecs[0]}, partialRuns, 2)
	if hasMeasurement(partialResult, "npb_ep_nt_mops") || hasMeasurement(partialResult, "npb_ep_scaling_ratio") {
		t.Fatal("incomplete NPB sample emitted multi-thread measurements")
	}
	partial := npbResultsTable([]npbBenchmarkSpec{npbBenchmarkSpecs[0]}, partialRuns, 2)
	if len(partial.Rows) != 2 || partial.Rows[1][7].Text() != "probe.npb.verification.failed" {
		t.Fatalf("partial NPB output = %+v", partial.Rows)
	}
}

func TestNPBProducerEmitsStableMachineResult(t *testing.T) {
	directory := t.TempDir()
	for _, spec := range npbBenchmarkSpecs {
		writeNPBFixtureTool(t, directory, spec.Binary, npbOutput(spec, 1, 100), npbOutput(spec, 2, 200))
	}
	t.Setenv("PATH", directory)
	result := runNPBBenchmarksWithAllowance(context.Background(), Environment{}, npbBenchmarkSpecs, cpuAllowance{Visible: 2, Threads: 2, Source: "fixture"})

	if result.ID != "npb" || result.Title != "module.npb.title" || result.Description != "probe.npb.description" || result.Status != model.StatusOK {
		t.Fatalf("NPB result identity/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.npb.profile" || result.Methodology.ComparisonScope != "probe.npb.comparison_scope" {
		t.Fatalf("NPB methodology = %+v", result.Methodology)
	}
	if result.Evidence == nil || result.Evidence.Valid != 4 || result.Evidence.Expected != 4 {
		t.Fatalf("NPB evidence = %+v", result.Evidence)
	}
	assertProducerParameterScope(t, result,
		"tool_version", "method_version", "problem_class", "threads", "implementation",
		"compiler_flags", "random_generator",
		"environment_1t", "environment_nt",
	)
	parameters := result.Methodology.Parameters
	if parameters["tool_version"] != npbExpectedVersion || parameters["method_version"] != npbMethodVersion || parameters["problem_class"] != npbExpectedClass || parameters["threads"] != "1 / 2" || parameters["implementation"] != "NPB3.4-OMP" || parameters["compiler_flags"] != npbCompileFlags || parameters["random_generator"] != npbRandomGenerator {
		t.Fatalf("NPB stable comparison parameters = %v", parameters)
	}
	if parameters["environment_1t"] != strings.Join(npbEnvironmentParameters(1), " ") || parameters["environment_nt"] != strings.Join(npbEnvironmentParameters(2), " ") {
		t.Fatalf("NPB environment comparison parameters = %v", parameters)
	}
	for _, field := range result.Fields {
		if !strings.HasPrefix(field.Label, "probe.npb.field.") {
			t.Fatalf("unstable NPB field label = %+v", field)
		}
	}
	if value, ok := npbField(result, "cpu_allowance").Value.Raw(); !ok || value != "visible=2;quota=unlimited" {
		t.Fatalf("NPB CPU allowance variant/value = %q/%v", value, ok)
	}
	if npbField(result, "arguments").Value.Text() != "(none)" || !strings.Contains(npbField(result, "environment_1t").Value.Text(), "OMP_NUM_THREADS=1") || !strings.Contains(npbField(result, "environment_nt").Value.Text(), "OMP_NUM_THREADS=2") {
		t.Fatalf("NPB parameter fields = %+v", result.Fields)
	}
	for _, measurement := range result.Measurements {
		if !strings.HasPrefix(measurement.Label, "probe.npb.metric.") {
			t.Fatalf("unstable NPB measurement label = %+v", measurement)
		}
	}
	if len(result.TextBlocks) != 4 {
		t.Fatalf("NPB raw block count = %d", len(result.TextBlocks))
	}
	for _, block := range result.TextBlocks {
		if block.Title != "probe.npb.raw_output" || block.Content == "" {
			t.Fatalf("NPB raw block = %+v", block)
		}
	}
	if len(result.Sources) != 1 || result.Sources[0].Purpose != "probe.npb.source.purpose" {
		t.Fatalf("NPB sources = %+v", result.Sources)
	}
	for _, note := range result.Notes {
		if !strings.HasPrefix(note, "probe.npb.note.") {
			t.Fatalf("unstable NPB note = %q", note)
		}
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.npb.summary.values" || len(result.SummaryMessages[0].Args) != 1 || result.SummaryMessages[0].Args[0] == "" {
		t.Fatalf("NPB summary = %+v", result.SummaryMessages)
	}

	table := result.Tables[0]
	if table.Title != "probe.npb.table.title" || len(table.Columns) != 8 || len(table.Rows) != 4 {
		t.Fatalf("NPB table shape = %+v", table)
	}
	if workload, ok := table.Rows[0][1].Key(); !ok || workload != "probe.npb.workload.ep" {
		t.Fatalf("NPB workload cell = %#v", table.Rows[0][1])
	}
	if verification, ok := table.Rows[0][7].Key(); !ok || verification != "probe.npb.verification.successful" {
		t.Fatalf("NPB verification cell = %#v", table.Rows[0][7])
	}
	if table.Rows[0][2].Text() != "1T" || table.Rows[1][2].Text() != "NT(2T)" || table.Rows[0][6].Text() != "1.00 x" {
		t.Fatalf("NPB table contexts/scaling = %+v", table.Rows)
	}
}

func TestNPBProducerMissingAndFailureDiagnosticsStayStructured(t *testing.T) {
	missingDirectory := t.TempDir()
	t.Setenv("PATH", missingDirectory)
	missing := runNPBBenchmarksWithAllowance(context.Background(), Environment{}, npbBenchmarkSpecs, cpuAllowance{Visible: 1, Threads: 1})
	if missing.Status != model.StatusWarning || len(missing.Failures) != 2 || missing.SummaryMessages[0].Key != "probe.npb.summary.none" || !containsNPBNote(missing.Notes, "probe.npb.note.tool_missing") {
		t.Fatalf("NPB missing result = %+v", missing)
	}
	for _, failure := range missing.Failures {
		if failure.Category != model.FailureToolMissing || failure.Stage != "tool_lookup" || failure.Message == "" {
			t.Fatalf("NPB missing failure = %+v", failure)
		}
	}

	directory := t.TempDir()
	writeNPBFixtureTool(t, directory, npbBenchmarkSpecs[0].Binary, "malformed NPB diagnostic", "malformed NPB diagnostic")
	t.Setenv("PATH", directory)
	failure := runNPBBenchmarksWithAllowance(context.Background(), Environment{}, npbBenchmarkSpecs[:1], cpuAllowance{Visible: 1, Threads: 1})
	if failure.Status != model.StatusWarning || len(failure.Failures) != 1 || failure.SummaryMessages[0].Key != "probe.npb.summary.none" || !containsNPBNote(failure.Notes, "probe.npb.note.run_failure") {
		t.Fatalf("NPB run failure result = %+v", failure)
	}
	if failure.Failures[0].Category != model.FailureParse || failure.Failures[0].Stage != "benchmark_run" || !strings.Contains(failure.Failures[0].Message, "官方 benchmark header") {
		t.Fatalf("NPB diagnostic/category = %+v", failure.Failures[0])
	}
	if len(failure.Tables) != 1 || len(failure.Tables[0].Rows) != 2 {
		t.Fatalf("NPB failed table = %+v", failure.Tables)
	}
	if verification, ok := failure.Tables[0].Rows[1][7].Key(); !ok || verification != "probe.npb.verification.failed" || failure.Tables[0].Rows[1][6].Text() != "na" {
		t.Fatalf("NPB failed table variants = %+v", failure.Tables[0].Rows[1])
	}
}

func writeNPBFixtureTool(t *testing.T, directory, name, oneThreadOutput, manyThreadOutput string) {
	t.Helper()
	path := filepath.Join(directory, name)
	script := "#!/bin/sh\ncase \"$OMP_NUM_THREADS\" in\n1) /bin/cat <<'EOF'\n" + oneThreadOutput + "\nEOF\n;;\n*) /bin/cat <<'EOF'\n" + manyThreadOutput + "\nEOF\n;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func npbField(result model.Result, key string) model.Field {
	for _, field := range result.Fields {
		if field.Key == key {
			return field
		}
	}
	return model.Field{}
}

func containsNPBNote(notes []string, want string) bool {
	for _, note := range notes {
		if note == want {
			return true
		}
	}
	return false
}
