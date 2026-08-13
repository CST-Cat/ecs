package probe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ecs/internal/model"
)

// 真实 sysbench 的参数契约：CPU 两轮都必须使用官方 cpu 工作负载，
// 并固定 prime、按时长运行、保留 events/s 与 P95 统计。
func TestExecuteSysbenchCPUUsesStableOfficialWorkload(t *testing.T) {
	sysbenchPath := requireTool(t, "sysbench")
	workers := detectCPUAllowance().Threads
	for _, testCase := range []struct {
		name    string
		threads int
	}{
		{name: "single", threads: 1},
		{name: "multi", threads: workers},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := executeSysbenchCPU(context.Background(), sysbenchPath, testCase.threads, 1)
			if err != nil {
				t.Fatalf("executeSysbenchCPU() error = %v", err)
			}
			wantArgs := []string{
				"--threads=" + strconv.Itoa(testCase.threads),
				"--time=1",
				"--events=0",
				"--percentile=95",
				"cpu",
				"--cpu-max-prime=20000",
				"run",
			}
			if !reflect.DeepEqual(got.Args, wantArgs) {
				t.Fatalf("sysbench args = %v, want %v", got.Args, wantArgs)
			}
			if got.Rate <= 0 || got.Events == 0 || got.P95MS <= 0 {
				t.Fatalf("sysbench CPU statistics = %+v, want positive events/s, events and P95", got)
			}
			if !strings.Contains(got.Output, "events per second") {
				t.Fatalf("sysbench raw output lost events/s statistic: %q", got.Output)
			}
		})
	}
}

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
