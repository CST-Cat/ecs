package probe

import (
	"fmt"
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

func TestParseNPBOutputAddsMOPSMeasurement(t *testing.T) {
	spec := npbBenchmarkSpecs[0]
	sample, err := parseNPBBenchmarkOutput(npbOutput(spec, 1, 100), spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Benchmark != spec.Name || sample.MOPS != 100 {
		t.Fatalf("parsed NPB sample = %+v", sample)
	}

	result := model.NewResult("npb", "npb")
	appendNPBMeasurements(&result, []npbBenchmarkSpec{spec}, map[string][]npbBenchmarkSample{
		spec.Name: {sample},
	}, 1)
	if !hasMeasurement(result, "npb_ep_1t_mops") {
		t.Fatalf("NPB measurements = %+v", result.Measurements)
	}
}

func TestParseNPBOutputRejectsMissingResult(t *testing.T) {
	if _, err := parseNPBBenchmarkOutput("not an NPB result", npbBenchmarkSpecs[0], 1); err == nil {
		t.Fatal("malformed NPB output unexpectedly parsed")
	}
}
