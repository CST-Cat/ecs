package probe

import (
	"context"
	"fmt"
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
	if table.Key != "benchmark.npb.results" || len(table.ColumnKeys) != len(table.Columns) || len(table.Rows) != 4 || table.Rows[0][7] != "SUCCESSFUL" {
		t.Fatalf("NPB result table = %+v", table)
	}
	singleRuns := map[string][]npbBenchmarkSample{}
	for _, spec := range npbBenchmarkSpecs {
		first := runs[spec.Name][0]
		singleRuns[spec.Name] = []npbBenchmarkSample{first, first}
	}
	single := npbResultsTable(npbBenchmarkSpecs, singleRuns, 1)
	if single.Rows[0][6] != "不适用" {
		t.Fatalf("single-core NPB output = table:%v", single.Rows[0])
	}
	partialRuns := map[string][]npbBenchmarkSample{"EP": {runs["EP"][0], {}}}
	partialResult := model.NewResult("npb", "npb")
	appendNPBMeasurements(&partialResult, []npbBenchmarkSpec{npbBenchmarkSpecs[0]}, partialRuns, 2)
	if hasMeasurement(partialResult, "npb_ep_nt_mops") || hasMeasurement(partialResult, "npb_ep_scaling_ratio") {
		t.Fatal("incomplete NPB sample emitted multi-thread measurements")
	}
	partial := npbResultsTable([]npbBenchmarkSpec{npbBenchmarkSpecs[0]}, partialRuns, 2)
	if len(partial.Rows) != 2 || partial.Rows[1][7] != "失败" {
		t.Fatalf("partial NPB output = %+v", partial.Rows)
	}
}
