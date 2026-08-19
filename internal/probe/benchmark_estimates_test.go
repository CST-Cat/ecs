package probe

import (
	"testing"
	"time"

	"ecs/internal/config"
)

func TestSingleCoreBenchmarkEstimatesMatchPhysicalPlan(t *testing.T) {
	runtime := config.Runtime{CPUTime: 5 * time.Second}
	if got := cpuBenchmarkEstimate(runtime, 1); got != 6*time.Second {
		t.Fatalf("single-core CPU estimate = %s, want 6s", got)
	}
	if got := cpuBenchmarkEstimate(runtime, 4); got != 11*time.Second {
		t.Fatalf("four-core CPU estimate = %s, want 11s", got)
	}
	if got := streamBenchmarkEstimate(runtime, 1); got != 11*time.Second {
		t.Fatalf("single-core STREAM estimate = %s, want 11s", got)
	}
	if got := streamBenchmarkEstimate(runtime, 4); got != 21*time.Second {
		t.Fatalf("four-core STREAM estimate = %s, want 21s", got)
	}
	if got := twoContextBenchmarkEstimate(60*time.Second, 1); got != 30*time.Second {
		t.Fatalf("single-core fixed estimate = %s, want 30s", got)
	}
	if got := twoContextBenchmarkEstimate(60*time.Second, 4); got != 60*time.Second {
		t.Fatalf("multi-core fixed estimate = %s, want 60s", got)
	}
}

func TestTwoContextEstimatesUseDescriptorDefaults(t *testing.T) {
	runtime := config.Runtime{}
	for _, id := range []string{"zstd", "npb", "crypto"} {
		descriptor, ok := config.ModuleDescriptorFor(id)
		if !ok {
			t.Fatalf("%s descriptor is missing", id)
		}
		if got, want := estimateModuleDuration(runtime, descriptor, 1), descriptor.Estimate/2; got != want {
			t.Fatalf("single-core %s estimate = %s, want descriptor estimate / 2 = %s", id, got, want)
		}
		if got, want := estimateModuleDuration(runtime, descriptor, 4), descriptor.Estimate; got != want {
			t.Fatalf("multi-core %s estimate = %s, want descriptor estimate = %s", id, got, want)
		}
	}
}

func TestEstimateForKeepsSelectionNotesAndTrafficSemantics(t *testing.T) {
	runtime, err := config.Defaults(config.ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Modules = []string{"system", "network"}
	estimate := EstimateFor(runtime)
	if estimate.DiskMiB != 0 || estimate.NetworkMiB != 0 {
		t.Fatalf("estimate = %+v", estimate)
	}
	if estimate.DurationText == "" {
		t.Fatal("duration estimate is empty")
	}

	runtime.Modules = []string{"disk", "speed"}
	estimate = EstimateFor(runtime)
	if estimate.DiskMiB != runtime.DiskMiB || estimate.NetworkMiB != -1 || len(estimate.Notes) == 0 {
		t.Fatalf("speed estimate = %+v", estimate)
	}

	runtime.Exposure = config.ExposureLocal
	runtime.Modules = []string{"route", "speed"}
	estimate = EstimateFor(runtime)
	if estimate.NetworkMiB != 0 {
		t.Fatalf("offline network estimate = %d, want 0", estimate.NetworkMiB)
	}
	if len(estimate.Notes) < 2 {
		t.Fatalf("offline route estimate notes = %v", estimate.Notes)
	}
}

func TestDiskEstimateMatchesCompleteFIOPlan(t *testing.T) {
	runtime := config.Runtime{}
	descriptor, ok := config.ModuleDescriptorFor("disk")
	if !ok {
		t.Fatal("disk descriptor is missing")
	}
	got := estimateModuleDuration(runtime, descriptor, detectCPUAllowance().Threads)
	want := FIOPlanDuration() + 10*time.Second
	if got != want {
		t.Fatalf("disk estimate = %s, want complete plan plus startup = %s", got, want)
	}
}
