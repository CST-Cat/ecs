package probe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestSysbenchProducerValidityAndWorkloadContracts(t *testing.T) {
	for _, test := range []struct {
		name          string
		allowance     cpuAllowance
		failMulti     bool
		wantStatus    model.Status
		wantValidity  string
		wantValid     int
		wantExpected  int
		wantThreadKey string
	}{
		{
			name: "valid", allowance: cpuAllowance{Visible: 2, Threads: 2, Source: "fixture"},
			wantStatus: model.StatusOK, wantValidity: "probe.cpu.validity.valid", wantValid: 2, wantExpected: 2, wantThreadKey: "1 / 2",
		},
		{
			name: "quota", allowance: cpuAllowance{Visible: 4, Quota: 1.5, Threads: 2, Source: "fixture"},
			wantStatus: model.StatusOK, wantValidity: "probe.cpu.validity.quota", wantValid: 2, wantExpected: 2, wantThreadKey: "1 / 2",
		},
		{
			name: "partial", allowance: cpuAllowance{Visible: 2, Threads: 2, Source: "fixture"}, failMulti: true,
			wantStatus: model.StatusWarning, wantValidity: "probe.cpu.validity.partial", wantValid: 1, wantExpected: 2, wantThreadKey: "1 / 2",
		},
		{
			name: "single core", allowance: cpuAllowance{Visible: 1, Threads: 1, Source: "fixture"},
			wantStatus: model.StatusOK, wantValidity: "probe.cpu.validity.valid", wantValid: 1, wantExpected: 1, wantThreadKey: "1 / 1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := writeSysbenchFixtureTool(t)
			if test.failMulti {
				t.Setenv("ECS_CPU_SYSBENCH_FAIL_MULTI", "1")
			}
			env := Environment{}
			env.Config.CPUTime = time.Second
			result := runSysbenchCPUWithAllowance(context.Background(), env, tool, test.allowance)

			if result.Status != test.wantStatus || cpuResultValidity(t, result) != test.wantValidity {
				t.Fatalf("CPU status/validity = %s/%q, want %s/%q", result.Status, cpuResultValidity(t, result), test.wantStatus, test.wantValidity)
			}
			if result.Evidence == nil || result.Evidence.Valid != test.wantValid || result.Evidence.Expected != test.wantExpected {
				t.Fatalf("CPU evidence = %+v, want %d/%d", result.Evidence, test.wantValid, test.wantExpected)
			}
			if field, ok := cpuFieldByKey(result, "threads"); !ok || field.Value.Text() != test.wantThreadKey {
				t.Fatalf("CPU thread field = %+v, want %q", field, test.wantThreadKey)
			}
			if field, ok := cpuFieldByKey(result, "duration"); !ok || field.Value.Text() != "1s" {
				t.Fatalf("CPU duration field = %+v, want 1s", field)
			}
			if field, ok := cpuFieldByKey(result, "prime"); !ok || field.Value.Text() != "20000" {
				t.Fatalf("CPU prime field = %+v, want 20000", field)
			}

			wantMeasurements := map[string]float64{
				"sysbench_cpu_single_events_s": 1234.5,
				"sysbench_cpu_single_p95_ms":   1.25,
			}
			if !test.failMulti {
				wantMeasurements["sysbench_cpu_multi_events_s"] = 1234.5
				wantMeasurements["sysbench_cpu_multi_p95_ms"] = 1.25
				if test.allowance.Threads > 1 {
					wantMeasurements["sysbench_cpu_multi_events_s"] = 2469
					wantMeasurements["sysbench_cpu_scaling_ratio"] = 2
					wantMeasurements["sysbench_cpu_per_thread_efficiency_percent"] = 100
					wantMeasurements["sysbench_cpu_multi_p95_ms"] = 2.5
				}
			}
			for key, want := range wantMeasurements {
				measurement, ok := cpuMeasurementByKey(result.Measurements, key)
				if !ok || measurement.Value != want {
					t.Errorf("CPU measurement %q = %+v, want %v", key, measurement, want)
				}
			}
			if test.failMulti {
				for _, key := range []string{"sysbench_cpu_multi_events_s", "sysbench_cpu_scaling_ratio", "sysbench_cpu_per_thread_efficiency_percent", "sysbench_cpu_multi_p95_ms"} {
					if hasMeasurement(result, key) {
						t.Errorf("unexpected incomplete/single-core measurement %q: %+v", key, result.Measurements)
					}
				}
			}
			if test.allowance.Threads == 1 && (hasMeasurement(result, "sysbench_cpu_scaling_ratio") || hasMeasurement(result, "sysbench_cpu_per_thread_efficiency_percent")) {
				t.Errorf("single-core scaling measurements unexpectedly emitted: %+v", result.Measurements)
			}
			if test.name == "single core" {
				if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.cpu.summary.single_core" || !containsCPUNote(result.Notes, "probe.cpu.note.single_core") {
					t.Errorf("single-core summary/notes = %+v/%v", result.SummaryMessages, result.Notes)
				}
			}
			if test.failMulti {
				if len(result.Failures) != 1 || result.Failures[0].Stage != "multi_thread_run" || len(result.TextBlocks) != 2 {
					t.Errorf("partial benchmark diagnostics = failures:%+v raw:%+v", result.Failures, result.TextBlocks)
				}
			}

			args, err := os.ReadFile(cpuSysbenchArgsPath(t))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(args)), "\n")
			wantArgs := []string{cpuSysbenchArgs(1)}
			if test.allowance.Threads > 1 {
				wantArgs = append(wantArgs, cpuSysbenchArgs(test.allowance.Threads))
			}
			if !reflect.DeepEqual(lines, wantArgs) {
				t.Fatalf("sysbench workload arguments = %v, want %v", lines, wantArgs)
			}
		})
	}
}

func newCPUWindowFixture(load float64, before, after cpuTimeSample) (EnvironmentSnapshot, EnvironmentSnapshot) {
	start := time.Unix(100, 0)
	return EnvironmentSnapshot{
		CapturedAt: start,
		Load1:      load,
		LoadKnown:  true,
		CPUTimes:   before,
		CPUTracked: true,
	}, EnvironmentSnapshot{
		CapturedAt: start.Add(time.Second),
		Load1:      load,
		LoadKnown:  true,
		CPUTimes:   after,
		CPUTracked: true,
	}
}

func cpuSemanticResult(result model.Result) model.Result {
	result.StartedAt = time.Time{}
	result.DurationMS = 0
	return result
}

func cpuResultValidity(t *testing.T, result model.Result) string {
	t.Helper()
	field, ok := cpuFieldByKey(result, "result_validity")
	if !ok {
		t.Fatalf("CPU result omitted result_validity field: %+v", result.Fields)
	}
	value, ok := field.Value.Key()
	if !ok {
		t.Fatalf("CPU result validity was not a stable key: %+v", field)
	}
	return value
}

func cpuFieldByKey(result model.Result, key string) (model.Field, bool) {
	for _, field := range result.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return model.Field{}, false
}

func cpuMeasurementByKey(measurements []model.Measurement, key string) (model.Measurement, bool) {
	for _, measurement := range measurements {
		if measurement.Key == key {
			return measurement, true
		}
	}
	return model.Measurement{}, false
}

func containsCPUNote(notes []string, want string) bool {
	for _, note := range notes {
		if note == want {
			return true
		}
	}
	return false
}

func writeSysbenchFixtureTool(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "sysbench")
	logPath := filepath.Join(directory, "args.log")
	t.Setenv("ECS_CPU_SYSBENCH_ARGS", logPath)
	t.Setenv("ECS_CPU_SYSBENCH_FAIL_MULTI", "")
	script := `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-V" ]; then
  printf '%s\n' 'sysbench 1.0.20 (fixture)'
  exit 0
fi
printf '%s\n' "$*" >> "$ECS_CPU_SYSBENCH_ARGS"
case "$1" in
--threads=1)
  printf '%s\n' 'events per second: 1234.50'
  printf '%s\n' 'total number of events: 2469'
  printf '%s\n' '95th percentile: 1.25'
  ;;
--threads=2)
  if [ "${ECS_CPU_SYSBENCH_FAIL_MULTI:-}" = "1" ]; then
    printf '%s\n' 'fixture multi-thread failure' >&2
    exit 7
  fi
  printf '%s\n' 'events per second: 2469.00'
  printf '%s\n' 'total number of events: 4938'
  printf '%s\n' '95th percentile: 2.50'
  ;;
*)
  exit 2
  ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func cpuSysbenchArgsPath(t *testing.T) string {
	t.Helper()
	return os.Getenv("ECS_CPU_SYSBENCH_ARGS")
}

func cpuSysbenchArgs(threads int) string {
	return strings.Join([]string{
		"--threads=" + strconv.Itoa(threads),
		"--time=1",
		"--events=0",
		"--percentile=95",
		"cpu",
		"--cpu-max-prime=20000",
		"run",
	}, " ")
}
