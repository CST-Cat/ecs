package probe

import (
	"testing"
	"time"

	"ecs/internal/config"
)

func TestCPUEstimateUsesConfiguredRuntime(t *testing.T) {
	runtime := config.Runtime{CPUTime: 5 * time.Second}
	if got := cpuBenchmarkEstimate(runtime, 1); got != 6*time.Second {
		t.Fatalf("CPU estimate = %s, want 6s", got)
	}
}
