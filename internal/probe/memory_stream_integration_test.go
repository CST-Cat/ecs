//go:build integration

// 真实 STREAM 的端到端契约。

package probe

import (
	"context"
	"ecs/internal/config"
	"ecs/internal/model"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestRunStreamWithRealBinary intentionally executes only a real executable
// discovered on PATH.  It never creates a command substitute: a missing or
// non-STREAM `stream` command is a skip outside CI and a failure in CI.
func TestRunStreamWithRealBinary(t *testing.T) {
	path, err := exec.LookPath("stream")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("official STREAM is required in CI: %v", err)
		}
		t.Skip("非 CI 环境未安装官方 STREAM，跳过真实 smoke test")
	}
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CPUTime = time.Second
	allowance := detectCPUAllowance()
	result := runStreamMemoryWithAllowance(context.Background(), Environment{Config: cfg}, path, allowance)
	if len(result.Measurements) == 0 {
		if os.Getenv("CI") != "" {
			t.Fatalf("stream command did not produce official STREAM output: %+v", result)
		}
		t.Skipf("非 CI 环境没有可解析的官方 STREAM 输出（可能是同名非 STREAM 命令），明确跳过：%s", path)
	}
	if result.Status != model.StatusOK {
		t.Fatalf("real STREAM result = %+v", result)
	}
	wantMeasurements := 8
	if allowance.Threads > 1 {
		wantMeasurements = 12
	}
	if len(result.Measurements) != wantMeasurements {
		t.Fatalf("STREAM measurements = %d, want %d: %+v", len(result.Measurements), wantMeasurements, result.Measurements)
	}
	for _, contextName := range []string{"1t", "nt"} {
		for _, kernel := range []string{"copy", "scale", "add", "triad"} {
			key := "stream_" + kernel + "_" + contextName + "_mib_s"
			found := false
			for _, measurement := range result.Measurements {
				if measurement.Key != key {
					continue
				}
				found = true
				if measurement.Value <= 0 || measurement.Unit != "MiB/s" || measurement.Method != "stream-official-"+kernel+"-"+contextName+"-v1" {
					t.Fatalf("STREAM measurement contract = %+v", measurement)
				}
			}
			if !found {
				t.Fatalf("STREAM missing measurement %q", key)
			}
		}
	}
	for _, kernel := range []string{"copy", "scale", "add", "triad"} {
		key := "stream_" + kernel + "_scaling_ratio"
		found := false
		for _, measurement := range result.Measurements {
			if measurement.Key == key && measurement.Value > 0 && measurement.Unit == "x" {
				found = true
			}
		}
		if allowance.Threads > 1 && !found {
			t.Fatalf("STREAM missing scaling measurement %q", key)
		}
		if allowance.Threads <= 1 && found {
			t.Fatalf("single-core STREAM invented scaling measurement %q", key)
		}
	}
	if len(result.Tables) != 2 || len(result.Tables[0].Rows) != 8 || len(result.Tables[1].Rows) != 8 {
		t.Fatalf("STREAM table rows = %+v", result.Tables)
	}
	wantRuns := len(distinctBenchmarkThreadCounts(allowance.Threads))
	if result.Evidence == nil || result.Evidence.Valid != wantRuns || result.Evidence.Expected != wantRuns || result.Evidence.Unit != "run" {
		t.Fatalf("STREAM evidence = %+v, want %d/%d runs", result.Evidence, wantRuns, wantRuns)
	}
	if len(result.TextBlocks) != wantRuns {
		t.Fatalf("STREAM raw output blocks = %d, want %d", len(result.TextBlocks), wantRuns)
	}
}
