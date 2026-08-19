package probe

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestBuildDNSQuery(t *testing.T) {
	packet, id, err := buildDNSQuery("www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) < 20 {
		t.Fatalf("packet too short: %d", len(packet))
	}
	if binary.BigEndian.Uint16(packet[:2]) != id {
		t.Fatal("transaction id mismatch")
	}
	if binary.BigEndian.Uint16(packet[4:6]) != 1 {
		t.Fatal("question count is not one")
	}
	if !strings.Contains(string(packet), "example") {
		t.Fatal("encoded name missing")
	}
}

func TestStats(t *testing.T) {
	values := []time.Duration{5 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 2 * time.Millisecond}
	if got := medianDuration(values); got != 2500*time.Microsecond {
		t.Fatalf("median = %s", got)
	}
	if got := percentileDuration(values, .95); got != 5*time.Millisecond {
		t.Fatalf("p95 = %s", got)
	}
}

func TestParseSystemFiles(t *testing.T) {
	directory := t.TempDir()
	osRelease := filepath.Join(directory, "os-release")
	if err := os.WriteFile(osRelease, []byte("PRETTY_NAME=\"Test Linux\"\nID=test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values := parseOSRelease(osRelease)
	if values["PRETTY_NAME"] != "Test Linux" {
		t.Fatalf("os release = %v", values)
	}
	memInfo := filepath.Join(directory, "meminfo")
	if err := os.WriteFile(memInfo, []byte("MemTotal: 1024 kB\nMemAvailable: 512 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := parseMemInfo(memInfo)
	if mem["MemTotal"] != 1024 || mem["MemAvailable"] != 512 {
		t.Fatalf("mem info = %v", mem)
	}
}

func TestHelpers(t *testing.T) {
	if got := sanitizeCommandOutput([]byte("\x1b[31m 1  1.1.1.1\x1b[0m\n")); got != "1  1.1.1.1" {
		t.Fatalf("sanitized = %q", got)
	}
	routeFixture := `{"Hops":[[{"TTL":1,"Address":"10.0.0.1"}],[],[{"TTL":3,"Address":"192.0.2.1"}]]}`
	if got := routeHopCount("nexttrace", routeFixture); got != 0 {
		t.Fatalf("unsupported nexttrace hops = %d", got)
	}
	if got := routeHopCount("nexttrace-tiny", routeFixture); got != 2 {
		t.Fatalf("nexttrace-tiny hops = %d", got)
	}
	if got := parseUintDefault("2048", 0); got != 2048 {
		t.Fatalf("uint = %d", got)
	}
}

func TestCommandVersionUsesOneLineAndSupportsFallbackFlag(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name: "long output",
			script: `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'tool 1.2.3\nbuild details\n'
  exit 0
fi
exit 1
`,
			want: "tool 1.2.3",
		},
		{
			name: "short flag fallback",
			script: `#!/bin/sh
if [ "$1" = "-V" ]; then
  printf 'tool 4.5.6\nmore details\n'
  exit 0
fi
exit 1
`,
			want: "tool 4.5.6",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tool")
			if err := os.WriteFile(path, []byte(testCase.script), 0o700); err != nil {
				t.Fatal(err)
			}
			if got := commandVersion(context.Background(), path); got != testCase.want {
				t.Fatalf("commandVersion() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFIOJSONHelpers(t *testing.T) {
	var output fioOutput
	raw := []byte(`{
	  "fio version": "fio-3.42",
	  "jobs": [{
	    "jobname": "randread",
	    "error": 0,
	    "read": {
	      "bw": 2048,
	      "bw_bytes": 2097152,
	      "iops": 512,
	      "clat_ns": {"mean": 1500000, "max": 4000000, "percentile": {"95.000000": 2500000, "99.000000": 3000000}}
	    }
	  }]
	}`)
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	if output.Version != "fio-3.42" || len(output.Jobs) != 1 {
		t.Fatalf("fio output = %+v", output)
	}
	if got := fioBandwidthMiB(output.Jobs[0].Read); got != 2 {
		t.Fatalf("fio bandwidth = %f", got)
	}
	if got := fioP95Milliseconds(output.Jobs[0].Read); got != 2.5 {
		t.Fatalf("fio p95 = %f", got)
	}
	stats, ok := fioLatencyStatsFor(output.Jobs[0].Read)
	if !ok || !stats.AvgOK || !stats.P95OK || !stats.P99OK || !stats.MaxOK || stats.AvgMS != 1.5 || stats.P95MS != 2.5 || stats.P99MS != 3 || stats.MaxMS != 4 {
		t.Fatalf("fio latency stats = %+v, ok=%v", stats, ok)
	}
	asyncEngine := fioEngine{Name: "libaio", AsyncQueue: true, Detected: true}
	plan := fioJobPlan()
	args := strings.Join(fioArguments("<tempfile>", 64*1024*1024, asyncEngine, plan), " ")
	for _, expected := range []string{
		"--output-format=json", "--direct=1", "--name=seqwrite", "--name=randwrite",
		"--iodepth=32", "--name=mix4k", "--name=mix512k", "--rwmixread=50", "--iodepth=64", "--numjobs=2",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("fio args missing %q: %s", expected, args)
		}
	}

	// 同步引擎下队列深度必须降级为 1，不能照抄请求值。
	syncEngine := fioEngine{Name: "psync", Detected: true}
	if got := syncEngine.EffectiveDepth(64); got != 1 {
		t.Fatalf("psync effective depth = %d, want 1", got)
	}
	syncArgs := strings.Join(fioArguments("<tempfile>", 64*1024*1024, syncEngine, plan), " ")
	if strings.Contains(syncArgs, "--iodepth=64") || strings.Contains(syncArgs, "--iodepth=32") {
		t.Fatalf("psync args must not request an async queue depth: %s", syncArgs)
	}

	// 配置档只控制模块预设；选中 disk 时混合与 Crystal/ATTO 口径始终完整。
	for _, expected := range []string{"--name=mix4k", "--name=mix64k", "--name=mix512k", "--name=mix1m"} {
		if !strings.Contains(args, expected) {
			t.Fatalf("selected disk must include complete mixed job %q: %s", expected, args)
		}
	}
	for _, expected := range []string{"--name=crystal_read_rnd4k_q1", "--name=crystal_write_seq1m_q8", "--name=atto_read_512b", "--name=atto_write_64m"} {
		if !strings.Contains(args, expected) {
			t.Fatalf("selected disk must include complete matrix job %q: %s", expected, args)
		}
	}
}

func resultField(result model.Result, key string) string {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value
		}
	}
	return ""
}

func TestSysbenchParsers(t *testing.T) {
	cpuOutput := `
CPU speed:
    events per second:  1234.50
General statistics:
    total number of events:              2469
Latency (ms):
         95th percentile:                        1.25
`
	if rate, ok := parseFirstFloat(sysbenchEventsRatePattern, cpuOutput); !ok || rate != 1234.5 {
		t.Fatalf("CPU rate = %f, %v", rate, ok)
	}
}

// steal 采样必须夹住压测窗口，且累计口径与增量口径分开。
func TestStealSamplingReadsProcStat(t *testing.T) {
	sample, ok := readCPUTimes()
	if !ok || sample.Total == 0 {
		t.Fatalf("/proc/stat is unreadable on Linux: sample=%+v ok=%v", sample, ok)
	}
	if _, known := cumulativeStealPercent(sample); !known {
		t.Fatal("cumulative steal must be derivable from a valid sample")
	}
	before := cpuTimeSample{Total: 1000, Steal: 10}
	after := cpuTimeSample{Total: 2000, Steal: 60}
	percent, ok := stealPercent(before, after)
	if !ok || percent != 5 {
		t.Fatalf("steal delta = %f, ok = %v, want 5", percent, ok)
	}
	// 计数器倒退（宿主机重启、cgroup 迁移）时不能编造数字。
	if _, ok := stealPercent(after, before); ok {
		t.Fatal("a shrinking counter must not yield a steal percentage")
	}
}

// 每个模块都要有方法学标注：漏了报告里就缺"这个数字是怎么来的、能和什么比"，
// 而那正是本项目区别于跑分脚本的地方。
func TestMethodologyCoversEveryModule(t *testing.T) {
	for _, item := range Builtins() {
		methodology := MethodologyFor(item.ID())
		if methodology.Kind == "" || methodology.Label == "" {
			t.Errorf("模块 %q 缺少方法学标注（probe.go 的 MethodologyFor）", item.ID())
		}
	}
}
