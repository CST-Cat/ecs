package probe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestExecuteSysbenchCPURejectsMissingP95(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysbench")
	script := "#!/bin/sh\nprintf '%s\\n' 'events per second: 1234.50' 'total number of events: 2469'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := executeSysbenchCPU(context.Background(), path, 1, 1)
	if err == nil || !strings.Contains(err.Error(), "95th percentile") {
		t.Fatalf("execute error = %v, want missing P95 error; result=%+v", err, got)
	}
}

func TestSingleCoreBenchmarkPlanAndCPULogicalMetricsAvoidFakeScaling(t *testing.T) {
	if got := distinctBenchmarkThreadCounts(1); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("single-core physical plan = %v, want one 1T run", got)
	}
	if got := distinctBenchmarkThreadCounts(4); !reflect.DeepEqual(got, []int{1, 4}) {
		t.Fatalf("multi-core physical plan = %v, want 1T and 4T", got)
	}
	if got := benchmarkThreadField(1); !strings.Contains(got, "同一次实测") {
		t.Fatalf("single-core thread disclosure is ambiguous: %q", got)
	}
	result := model.NewResult("cpu", "cpu")
	sample := sysbenchCPUResult{Rate: 100, Events: 1000, P95MS: 1}
	appendSysbenchCPUMeasurements(&result, sample, sample, 1)
	for _, key := range []string{"sysbench_cpu_single_events_s", "sysbench_cpu_multi_events_s", "sysbench_cpu_single_p95_ms", "sysbench_cpu_multi_p95_ms"} {
		if !hasMeasurement(result, key) {
			t.Errorf("single-core logical CPU metric missing %q", key)
		}
	}
	for _, key := range []string{"sysbench_cpu_scaling_ratio", "sysbench_cpu_per_thread_efficiency_percent"} {
		if hasMeasurement(result, key) {
			t.Errorf("single-core CPU result invented %q", key)
		}
	}
}

func TestRunSysbenchSingleCoreExecutesOnePhysicalWorkload(t *testing.T) {
	directory := t.TempDir()
	tool := filepath.Join(directory, "sysbench")
	logPath := filepath.Join(directory, "runs.log")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo 'sysbench 1.0.20'
  exit 0
fi
printf '%s\n' run >> "$ECS_RUN_LOG"
printf '%s\n' \
  'events per second: 1234.50' \
  'total number of events: 2469' \
  '95th percentile: 1.25'
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECS_RUN_LOG", logPath)
	result := runSysbenchCPUWithAllowance(context.Background(), Environment{}, tool, cpuAllowance{Visible: 1, Threads: 1, Source: "fixture"})
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(log), "run\n") != 1 {
		t.Fatalf("single-core sysbench physical executions = %q, want exactly one", log)
	}
	if result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 1 || len(result.TextBlocks) != 1 {
		t.Fatalf("single-core sysbench evidence/raw output is not physical: evidence=%+v blocks=%d", result.Evidence, len(result.TextBlocks))
	}
	for _, key := range []string{"sysbench_cpu_single_events_s", "sysbench_cpu_multi_events_s"} {
		if !hasMeasurement(result, key) {
			t.Errorf("single-core sysbench lost logical metric %q", key)
		}
	}
	if hasMeasurement(result, "sysbench_cpu_scaling_ratio") {
		t.Fatal("single-core sysbench emitted a scaling ratio")
	}
}
