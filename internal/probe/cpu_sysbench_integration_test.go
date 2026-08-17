//go:build integration

// 真实 sysbench 的端到端契约。

package probe

import (
	"context"
	"ecs/internal/config"
	"ecs/internal/model"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
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

// 真实 sysbench 端到端：解析器必须认得当前安装版本的实际输出格式。
func TestRunSysbenchWithRealBinary(t *testing.T) {
	sysbenchPath := requireTool(t, "sysbench")
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CPUTime = time.Second

	allowance := detectCPUAllowance()
	cpu := runSysbenchCPUWithAllowance(context.Background(), Environment{Config: cfg}, sysbenchPath, allowance)
	// 宿主机 steal 高时降级为 warning 是正确行为，只有 error 才算失败。
	if cpu.Status == model.StatusError || cpu.Methodology.Kind != "standard-benchmark" {
		t.Fatalf("sysbench CPU result = %+v", cpu)
	}
	cpuValues := make(map[string]float64, len(cpu.Measurements))
	cpuMeasurements := make(map[string]model.Measurement, len(cpu.Measurements))
	for _, measurement := range cpu.Measurements {
		cpuValues[measurement.Key] = measurement.Value
		cpuMeasurements[measurement.Key] = measurement
	}
	// steal 缺失在 Linux 上就是 bug：/proc/stat 必然可读，读不到说明采样逻辑坏了。
	requiredMeasurements := []string{
		"sysbench_cpu_single_events_s",
		"sysbench_cpu_multi_events_s",
		"sysbench_cpu_single_p95_ms",
		"sysbench_cpu_multi_p95_ms",
		"cpu_steal_percent_during_test",
	}
	if allowance.Threads > 1 {
		requiredMeasurements = append(requiredMeasurements,
			"sysbench_cpu_scaling_ratio", "sysbench_cpu_per_thread_efficiency_percent")
	}
	for _, required := range requiredMeasurements {
		if _, ok := cpuValues[required]; !ok {
			t.Fatalf("sysbench CPU missing %q: %+v", required, cpu.Measurements)
		}
	}
	if cpuValues["sysbench_cpu_single_events_s"] <= 0 {
		t.Fatalf("real sysbench returned a non-positive event rate: %+v", cpu.Measurements)
	}
	if allowance.Threads > 1 && (cpuValues["sysbench_cpu_scaling_ratio"] <= 0 || cpuValues["sysbench_cpu_per_thread_efficiency_percent"] <= 0) {
		t.Fatalf("CPU scaling diagnostics must be positive: %+v", cpu.Measurements)
	}
	if allowance.Threads <= 1 && (hasMeasurement(cpu, "sysbench_cpu_scaling_ratio") || hasMeasurement(cpu, "sysbench_cpu_per_thread_efficiency_percent")) {
		t.Fatalf("single-core CPU invented scaling diagnostics: %+v", cpu.Measurements)
	}
	for _, key := range []string{"sysbench_cpu_single_events_s", "sysbench_cpu_multi_events_s"} {
		measurement := cpuMeasurements[key]
		if measurement.Unit != "events/s" || measurement.Method != "sysbench-cpu-prime20000-v1" ||
			measurement.HigherIsBetter == nil || !*measurement.HigherIsBetter {
			t.Fatalf("CPU measurement contract for %q = %+v", key, measurement)
		}
	}
	stealMeasurement := cpuMeasurements["cpu_steal_percent_during_test"]
	if stealMeasurement.Unit != "%" || stealMeasurement.Method != "proc-stat-steal-delta-v1" ||
		stealMeasurement.HigherIsBetter == nil || *stealMeasurement.HigherIsBetter {
		t.Fatalf("CPU steal measurement contract = %+v", stealMeasurement)
	}
	if steal := cpuValues["cpu_steal_percent_during_test"]; steal < 0 || steal > 100 {
		t.Fatalf("steal percentage out of range: %f", steal)
	}
	fieldValues := make(map[string]string, len(cpu.Fields))
	for _, field := range cpu.Fields {
		fieldValues[field.Key] = field.Value
	}
	for _, key := range []string{"engine", "version", "binary_sha256", "threads", "duration", "prime", "single_events", "multi_events", "result_validity", "pretest_load_1m"} {
		if fieldValues[key] == "" {
			t.Fatalf("sysbench CPU missing field %q: %+v", key, cpu.Fields)
		}
	}
	wantRuns := len(distinctBenchmarkThreadCounts(allowance.Threads))
	if cpu.Evidence == nil || cpu.Evidence.Valid != wantRuns || cpu.Evidence.Expected != wantRuns || cpu.Evidence.Unit != "run" {
		t.Fatalf("sysbench CPU evidence = %+v, want %d/%d runs", cpu.Evidence, wantRuns, wantRuns)
	}
	if got, want := fieldValues["engine"], "sysbench"; got != want {
		t.Fatalf("sysbench engine field = %q, want %q", got, want)
	}
	if got, want := fieldValues["threads"], benchmarkThreadField(allowance.Threads); got != want {
		t.Fatalf("sysbench threads field = %q, want %q", got, want)
	}
	if got, want := fieldValues["duration"], "1s"; got != want {
		t.Fatalf("sysbench duration field = %q, want %q", got, want)
	}
	if got, want := fieldValues["prime"], "20000"; got != want {
		t.Fatalf("sysbench prime field = %q, want %q", got, want)
	}
	if version := resultField(cpu, "version"); !strings.Contains(strings.ToLower(version), "sysbench") {
		t.Fatalf("sysbench version field = %q", version)
	}
	if got := resultField(cpu, "arguments"); got != "" {
		t.Fatalf("sysbench report must omit command arguments, got %q", got)
	}
	if len(cpu.TextBlocks) != 2 {
		t.Fatalf("raw sysbench output blocks = %d, want 2: %+v", len(cpu.TextBlocks), cpu.TextBlocks)
	}
	for _, block := range cpu.TextBlocks {
		if !strings.Contains(block.Content, "events per second") {
			t.Fatalf("raw sysbench output lost events/s statistic: %+v", cpu.TextBlocks)
		}
	}

}
