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
