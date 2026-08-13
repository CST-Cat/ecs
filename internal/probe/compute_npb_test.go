package probe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
 Compile date    =                  01 Jan 2000

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

func TestParseNPBBenchmarkOutputStrictClassAContract(t *testing.T) {
	for _, spec := range npbBenchmarkSpecs {
		t.Run(spec.Name, func(t *testing.T) {
			output := npbOutput(spec, 4, 400)
			sample, err := parseNPBBenchmarkOutput(output, spec, 4)
			if err != nil {
				t.Fatal(err)
			}
			if sample.Benchmark != spec.Name || sample.Class != "A" || sample.Size != spec.ExpectedSize ||
				sample.Iterations != spec.ExpectedIters || sample.Threads != 4 || sample.Available != 4 ||
				sample.MOPS != 400 || sample.MOPSPerThread != 100 || sample.Version != "3.4.4" ||
				sample.CompileFlags != npbCompileFlags || sample.Random != "randi8" {
				t.Fatalf("parsed NPB sample = %+v", sample)
			}
		})
	}
}

func TestParseNPBBenchmarkOutputRejectsIncomparableOrUnverifiedRun(t *testing.T) {
	spec := npbBenchmarkSpecs[0]
	valid := npbOutput(spec, 4, 400)
	for _, testCase := range []struct {
		name   string
		output string
	}{
		{name: "wrong class", output: strings.Replace(valid, "Class           =                        A", "Class           =                        B", 1)},
		{name: "wrong size", output: strings.Replace(valid, spec.ExpectedSize, "123", 1)},
		{name: "wrong iterations", output: strings.Replace(valid, "Iterations      =                        0", "Iterations      =                        1", 1)},
		{name: "wrong threads", output: strings.Replace(valid, "Total threads   =                        4", "Total threads   =                        3", 1)},
		{name: "thread warning", output: valid + "\n Warning: Threads used differ from threads available\n"},
		{name: "unverified", output: strings.Replace(valid, "SUCCESSFUL", "UNSUCCESSFUL", 1)},
		{name: "wrong version", output: strings.Replace(valid, "3.4.4", "3.4.3", 1)},
		{name: "wrong flags", output: strings.Replace(valid, npbCompileFlags, "-Ofast -fopenmp", 1)},
		{name: "wrong random", output: strings.Replace(valid, "RAND         = randi8", "RAND         = randdp", 1)},
		{name: "duplicate mops", output: valid + "\n Mop/s total = 1.00\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseNPBBenchmarkOutput(testCase.output, spec, 4); err == nil {
				t.Fatalf("malformed NPB output unexpectedly parsed: %s", testCase.output)
			}
		})
	}
}

func TestExecuteNPBBenchmarkUsesFixedOpenMPEnvironmentAndNoArguments(t *testing.T) {
	spec := npbBenchmarkSpecs[0]
	directory := t.TempDir()
	tool := filepath.Join(directory, spec.Binary)
	script := `#!/bin/sh
test "$#" -eq 0 || { echo "unexpected arguments: $*" >&2; exit 8; }
test "$OMP_NUM_THREADS" = 4 || exit 9
test "$OMP_DYNAMIC" = FALSE || exit 10
test "$OMP_PROC_BIND" = close || exit 11
test "$OMP_PLACES" = cores || exit 12
test "$OMP_SCHEDULE" = static || exit 13
test "$OMP_DISPLAY_ENV" = FALSE || exit 14
test "$NPB_TIMER_FLAG" = 0 || exit 15
cat <<'EOF'
 NAS Parallel Benchmarks (NPB3.4-OMP) - EP Benchmark
 EP Benchmark Completed.
 Class           = A
 Size            = 536870912
 Iterations      = 0
 Time in seconds = 1.25
 Total threads   = 4
 Avail threads   = 4
 Mop/s total     = 400.00
 Mop/s/thread    = 100.00
 Operation type  = Random numbers generated
 Verification    = SUCCESSFUL
 Version         = 3.4.4
 Compile options:
    FC           = gfortran
    FLINK        = gfortran
    F_LIB        = (none)
    F_INC        = (none)
    FFLAGS       = -O3 -fopenmp -static
    FLINKFLAGS   = -O3 -fopenmp -static
    RAND         = randi8
EOF
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sample, err := executeNPBBenchmark(context.Background(), tool, spec, 4)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Threads != 4 || sample.MOPS != 400 || sample.BinarySHA256 == "" || len(sample.Environment) != 7 {
		t.Fatalf("NPB execution sample = %+v", sample)
	}
}

func TestRunNPBBenchmarksRecordsRawResultsScalingAndNoScore(t *testing.T) {
	directory := t.TempDir()
	originalPath := os.Getenv("PATH")
	for _, spec := range npbBenchmarkSpecs {
		tool := filepath.Join(directory, spec.Binary)
		baseMOPS := 100.0
		if spec.Name == "FT" {
			baseMOPS = 50
		}
		script := fmt.Sprintf(`#!/bin/sh
threads=${OMP_NUM_THREADS:-1}
mops=%s
if [ "$threads" -gt 1 ]; then mops=%s; fi
per_thread=$(awk -v m="$mops" -v t="$threads" 'BEGIN {printf "%%.2f", m/t}')
cat <<EOF
 NAS Parallel Benchmarks (NPB3.4-OMP) - %s Benchmark
 %s Benchmark Completed.
 Class           = A
 Size            = %s
 Iterations      = %d
 Time in seconds = 1.25
 Total threads   = $threads
 Avail threads   = $threads
 Mop/s total     = $mops
 Mop/s/thread    = $per_thread
 Operation type  = %s
 Verification    = SUCCESSFUL
 Version         = 3.4.4
 Compile options:
    FC           = gfortran
    FLINK        = gfortran
    F_LIB        = (none)
    F_INC        = (none)
    FFLAGS       = -O3 -fopenmp -static
    FLINKFLAGS   = -O3 -fopenmp -static
    RAND         = randi8
EOF
`, strconv.FormatFloat(baseMOPS, 'f', 2, 64), strconv.FormatFloat(baseMOPS*2, 'f', 2, 64),
			spec.Name, spec.Name, spec.ExpectedSize, spec.ExpectedIters, spec.ExpectedOp)
		if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	result := runNPBBenchmarksWithAllowance(context.Background(), Environment{}, npbBenchmarkSpecs,
		cpuAllowance{Visible: 4, Threads: 4, Source: "fixture"})
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 4 || result.Evidence.Expected != 4 {
		t.Fatalf("NPB status/evidence = %s %+v; failures=%+v notes=%v", result.Status, result.Evidence, result.Failures, result.Notes)
	}
	for _, key := range []string{
		"npb_ep_1t_mops", "npb_ep_nt_mops", "npb_ep_scaling_ratio",
		"npb_ft_1t_mops", "npb_ft_nt_mops", "npb_ft_scaling_ratio",
	} {
		if !hasMeasurement(result, key) {
			t.Errorf("NPB result missing measurement %q: %+v", key, result.Measurements)
		}
	}
	for _, key := range []string{
		"version", "method_version", "problem_class", "threads", "compiler_flags",
		"binary_ep_sha256", "binary_ft_sha256", "environment_1t", "environment_nt",
	} {
		if resultField(result, key) == "" {
			t.Errorf("NPB result missing field %q: %+v", key, result.Fields)
		}
	}
	if len(result.TextBlocks) != 4 || len(result.Tables) != 1 || len(result.Tables[0].Rows) != 4 {
		t.Fatalf("NPB raw/table evidence = blocks:%d tables:%+v", len(result.TextBlocks), result.Tables)
	}
	for _, measurement := range result.Measurements {
		if strings.Contains(strings.ToLower(measurement.Key), "score") {
			t.Fatalf("NPB emitted composite score: %+v", measurement)
		}
	}
}

func TestRunNPBBenchmarksKeepsAvailableBenchmarkWhenPeerBinaryIsMissing(t *testing.T) {
	directory := t.TempDir()
	originalPath := os.Getenv("PATH")
	spec := npbBenchmarkSpecs[0]
	tool := filepath.Join(directory, spec.Binary)
	script := `#!/bin/sh
threads=${OMP_NUM_THREADS:-1}
cat <<EOF
 NAS Parallel Benchmarks (NPB3.4-OMP) - EP Benchmark
 EP Benchmark Completed.
 Class = A
 Size = 536870912
 Iterations = 0
 Time in seconds = 1.00
 Total threads = $threads
 Avail threads = $threads
 Mop/s total = 100.00
 Mop/s/thread = 100.00
 Operation type = Random numbers generated
 Verification = SUCCESSFUL
 Version = 3.4.4
 FC = gfortran
 FLINK = gfortran
 FFLAGS = -O3 -fopenmp -static
 FLINKFLAGS = -O3 -fopenmp -static
 RAND = randi8
EOF
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	result := runNPBBenchmarksWithAllowance(context.Background(), Environment{}, npbBenchmarkSpecs,
		cpuAllowance{Visible: 4, Threads: 4, Source: "fixture"})
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 4 {
		t.Fatalf("partial NPB result = %s %+v failures=%+v", result.Status, result.Evidence, result.Failures)
	}
	if !hasMeasurement(result, "npb_ep_1t_mops") || hasMeasurement(result, "npb_ft_1t_mops") {
		t.Fatalf("partial NPB measurements = %+v", result.Measurements)
	}
}

func TestNPBSingleCoreKeepsLogicalMetricsWithoutScaling(t *testing.T) {
	spec := npbBenchmarkSpecs[0]
	sample := npbBenchmarkSample{Benchmark: spec.Name, Threads: 1, MOPS: 100, MOPSPerThread: 100, Seconds: 1}
	runs := map[string][]npbBenchmarkSample{spec.Name: {sample, sample}}
	result := model.NewResult("npb", "npb")
	appendNPBMeasurements(&result, []npbBenchmarkSpec{spec}, runs, 1)
	for _, key := range []string{"npb_ep_1t_mops", "npb_ep_nt_mops"} {
		if !hasMeasurement(result, key) {
			t.Errorf("single-core logical metric missing %q", key)
		}
	}
	if hasMeasurement(result, "npb_ep_scaling_ratio") {
		t.Fatal("single-core NPB result invented a scaling ratio")
	}
	table := npbResultsTable([]npbBenchmarkSpec{spec}, runs, 1)
	for _, row := range table.Rows {
		if row[6] != "不适用" {
			t.Fatalf("single-core NPB scaling cell = %q, want 不适用: %v", row[6], row)
		}
	}
}

func TestRunNPBSingleCoreExecutesOnePhysicalWorkloadPerBenchmark(t *testing.T) {
	directory := t.TempDir()
	originalPath := os.Getenv("PATH")
	spec := npbBenchmarkSpecs[0]
	tool := filepath.Join(directory, spec.Binary)
	logPath := filepath.Join(directory, "runs.log")
	script := `#!/bin/sh
printf '%s\n' run >> "$ECS_RUN_LOG"
cat <<EOF
 NAS Parallel Benchmarks (NPB3.4-OMP) - EP Benchmark
 EP Benchmark Completed.
 Class = A
 Size = 536870912
 Iterations = 0
 Time in seconds = 1.00
 Total threads = 1
 Avail threads = 1
 Mop/s total = 100.00
 Mop/s/thread = 100.00
 Operation type = Random numbers generated
 Verification = SUCCESSFUL
 Version = 3.4.4
 FC = gfortran
 FLINK = gfortran
 FFLAGS = -O3 -fopenmp -static
 FLINKFLAGS = -O3 -fopenmp -static
 RAND = randi8
EOF
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	t.Setenv("ECS_RUN_LOG", logPath)
	result := runNPBBenchmarksWithAllowance(context.Background(), Environment{}, []npbBenchmarkSpec{spec},
		cpuAllowance{Visible: 1, Threads: 1, Source: "fixture"})
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(log), "run\n") != 1 {
		t.Fatalf("single-core NPB physical executions = %q, want one for one benchmark", log)
	}
	if result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 1 || len(result.TextBlocks) != 1 {
		t.Fatalf("single-core NPB evidence/raw output is not physical: evidence=%+v blocks=%d", result.Evidence, len(result.TextBlocks))
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 2 {
		t.Fatalf("single-core NPB lost logical table contexts: %+v", result.Tables)
	}
}
