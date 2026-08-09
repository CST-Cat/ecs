package probe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
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
