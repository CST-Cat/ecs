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

	"ecs/internal/config"
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
	if got := routeHopCount("nexttrace", `{"Hops":[[{"TTL":1}],[],[{"TTL":3}]]}`); got != 2 {
		t.Fatalf("nexttrace hops = %d", got)
	}
	if got := parseUintDefault("2048", 0); got != 2048 {
		t.Fatalf("uint = %d", got)
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
	      "clat_ns": {"percentile": {"95.000000": 2500000}}
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
	asyncEngine := fioEngine{Name: "libaio", AsyncQueue: true, Detected: true}
	plan := fioJobPlan(config.ProfileStandard)
	args := strings.Join(fioArguments("<tempfile>", 64*1024*1024, 2*time.Second, asyncEngine, plan), " ")
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
	syncArgs := strings.Join(fioArguments("<tempfile>", 64*1024*1024, 2*time.Second, syncEngine, plan), " ")
	if strings.Contains(syncArgs, "--iodepth=64") || strings.Contains(syncArgs, "--iodepth=32") {
		t.Fatalf("psync args must not request an async queue depth: %s", syncArgs)
	}

	// quick 档只跑首尾两档混合，避免时长失控。
	quickPlan := fioJobPlan(config.ProfileQuick)
	quickArgs := strings.Join(fioArguments("<tempfile>", 1<<20, time.Second, asyncEngine, quickPlan), " ")
	if strings.Contains(quickArgs, "--name=mix64k") {
		t.Fatalf("quick profile should skip 64k mixed job: %s", quickArgs)
	}
}

// writeFIOAdapter 写一个假 fio，--enghelp 只报告 engine，其余调用返回固定 JSON。
//
// engine 决定被测的是哪条真实路径：libaio 是绝大多数 VPS 的实际情况，psync 是
// 精简发行版缺少 libaio 时的降级路径。两条都必须有用例覆盖。
func writeFIOAdapter(t *testing.T, directory, engine string) string {
	t.Helper()
	helper := filepath.Join(directory, "fio")
	script := `#!/bin/sh
case "$*" in
  *--enghelp*)
    printf '%s\n' '` + engine + `'
    exit 0
    ;;
esac
printf '%s\n' '{"fio version":"fio-test","jobs":[
{"jobname":"seqwrite","error":0,"write":{"bw_bytes":104857600,"iops":100}},
{"jobname":"seqread","error":0,"read":{"bw_bytes":209715200,"iops":200}},
{"jobname":"randread","error":0,"read":{"bw_bytes":4096000,"iops":1000,"clat_ns":{"percentile":{"95.000000":2000000}}}},
{"jobname":"randwrite","error":0,"write":{"bw_bytes":2048000,"iops":500,"clat_ns":{"percentile":{"95.000000":3000000}}}},
{"jobname":"mix4k","error":0,"read":{"bw_bytes":2097152,"iops":512},"write":{"bw_bytes":1048576,"iops":256}},
{"jobname":"mix1m","error":0,"read":{"bw_bytes":52428800,"iops":50},"write":{"bw_bytes":52428800,"iops":50}}
]}'
`
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return helper
}

func fioFieldValue(result model.Result, key string) string {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value
		}
	}
	return ""
}

// libaio 是真实 VPS 上的主路径：队列深度必须真实生效，成绩按 QD32/QD64 标注。
func TestRunFIODiskWithAsyncEngine(t *testing.T) {
	directory := t.TempDir()
	helper := writeFIOAdapter(t, directory, "libaio")
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DiskPath = directory
	cfg.DiskMiB = 16
	result := runFIODisk(context.Background(), Environment{Config: cfg}, helper)
	if result.Status != "ok" {
		t.Fatalf("fio result = %+v", result)
	}
	if !strings.Contains(result.Summary, "100.0 MiB/s") {
		t.Fatalf("fio summary = %q", result.Summary)
	}

	// 探测到异步引擎后必须真的请求高队列深度，否则测到的是 QD1 成绩。
	arguments := fioFieldValue(result, "arguments")
	for _, expected := range []string{"--ioengine=libaio", "--iodepth=32", "--iodepth=64"} {
		if !strings.Contains(arguments, expected) {
			t.Fatalf("libaio args missing %q: %s", expected, arguments)
		}
	}
	if engine := fioFieldValue(result, "ioengine"); !strings.Contains(engine, "libaio") || !strings.Contains(engine, "异步") {
		t.Fatalf("ioengine field = %q", engine)
	}

	// 方法名必须体现实际生效的队列深度，否则不同 QD 的成绩会被混为一谈。
	methods := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		methods[measurement.Key] = measurement.Method
	}
	if got := methods["fio_random_read_4k_iops"]; !strings.Contains(got, "qd32") {
		t.Fatalf("random read method = %q, want qd32", got)
	}
	if got := methods["fio_mixed_4k_read_mib_s"]; !strings.Contains(got, "qd64") {
		t.Fatalf("mixed method = %q, want qd64", got)
	}
	for _, note := range result.Notes {
		if strings.Contains(note, "队列深度对它无效") {
			t.Fatalf("async engine must not be labelled as synchronous: %q", note)
		}
	}

	// 四项基础指标 + 两个 P95 + 两档混合各读写两项。
	if len(result.Measurements) != 10 {
		t.Fatalf("fio measurements = %d: %+v", len(result.Measurements), result.Measurements)
	}
	mixedFound := false
	for _, table := range result.Tables {
		if strings.Contains(table.Title, "混合") && len(table.Rows) == 2 {
			mixedFound = true
		}
	}
	if !mixedFound {
		t.Fatalf("mixed matrix table missing: %+v", result.Tables)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".ecs-fio-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("fio temporary files remain: %v", matches)
	}
}

// 精简发行版没有 libaio 时退到 psync：队列深度对同步引擎无效，
// 参数和方法名都必须按实际生效的 QD1 标注，不能照抄请求值。
func TestRunFIODiskDowngradesSynchronousEngine(t *testing.T) {
	directory := t.TempDir()
	helper := writeFIOAdapter(t, directory, "psync")
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DiskPath = directory
	cfg.DiskMiB = 16
	result := runFIODisk(context.Background(), Environment{Config: cfg}, helper)
	if result.Status != "ok" {
		t.Fatalf("fio result = %+v", result)
	}

	arguments := fioFieldValue(result, "arguments")
	if !strings.Contains(arguments, "--ioengine=psync") {
		t.Fatalf("psync args = %s", arguments)
	}
	if strings.Contains(arguments, "--iodepth=32") || strings.Contains(arguments, "--iodepth=64") {
		t.Fatalf("psync must not request an async queue depth: %s", arguments)
	}
	if engine := fioFieldValue(result, "ioengine"); !strings.Contains(engine, "同步") {
		t.Fatalf("ioengine field = %q", engine)
	}

	for _, measurement := range result.Measurements {
		if measurement.Key != "fio_random_read_4k_iops" {
			continue
		}
		if !strings.Contains(measurement.Method, "qd1") {
			t.Fatalf("synchronous random read method = %q, want qd1", measurement.Method)
		}
	}
	downgradeNoted := false
	for _, note := range result.Notes {
		if strings.Contains(note, "队列深度对它无效") {
			downgradeNoted = true
		}
	}
	if !downgradeNoted {
		t.Fatalf("psync run must disclose the queue-depth downgrade: %+v", result.Notes)
	}
}

func TestSysbenchParsersAndAdapters(t *testing.T) {
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
	if rate := memoryRateToMiB(2, "GiB"); rate != 2048 {
		t.Fatalf("memory rate = %f", rate)
	}

	directory := t.TempDir()
	helper := filepath.Join(directory, "sysbench")
	// cpu 分支里的 sleep 让压测窗口跨过真实的挂钟时间：/proc/stat 是 jiffies
	// 计数，瞬时返回的假脚本会让前后两次采样完全相同，steal 增量就无从计算。
	script := `#!/bin/sh
case "$*" in
  *--version*)
    printf '%s\n' 'sysbench 1.0.20'
    ;;
  *cpu*)
    sleep 0.2
    case "$*" in
      *--threads=1*) rate=100 ;;
      *) rate=800 ;;
    esac
    printf '%s\n' "CPU speed:
    events per second:  $rate
General statistics:
    total number of events:              1000
Latency (ms):
         95th percentile:                        1.25"
    ;;
  *memory*)
    case "$*" in
      *--memory-oper=read*) rate=4096 ;;
      *) rate=2048 ;;
    esac
    printf '%s\n' "1024.00 MiB transferred ($rate MiB/sec)"
    ;;
esac
`
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CPUTime = 100 * time.Millisecond
	cpu := runSysbenchCPU(context.Background(), Environment{Config: cfg}, helper)
	// 宿主机 steal 高时降级为 warning 是正确行为，只有 error 才算失败。
	if cpu.Status == "error" || cpu.Methodology.Kind != "standard-benchmark" {
		t.Fatalf("sysbench CPU result = %+v", cpu)
	}
	cpuKeys := make(map[string]bool, len(cpu.Measurements))
	for _, measurement := range cpu.Measurements {
		cpuKeys[measurement.Key] = true
	}
	// steal 缺失在 Linux 上就是 bug：/proc/stat 必然可读，读不到说明采样逻辑坏了。
	for _, required := range []string{
		"sysbench_cpu_single_events_s",
		"sysbench_cpu_multi_events_s",
		"cpu_steal_percent_during_test",
	} {
		if !cpuKeys[required] {
			t.Fatalf("sysbench CPU missing %q: %+v", required, cpu.Measurements)
		}
	}
	for _, measurement := range cpu.Measurements {
		if strings.Contains(measurement.Key, "efficiency") {
			t.Fatalf("CPU must not emit derived score: %+v", measurement)
		}
	}
	memory := runSysbenchMemory(context.Background(), Environment{Config: cfg}, helper)
	if memory.Status != "ok" || memory.Methodology.Kind != "standard-benchmark" || len(memory.Measurements) != 4 {
		t.Fatalf("sysbench memory result = %+v", memory)
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

func TestRunIPerfWithLocalJSONAdapter(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "iperf3")
	script := `#!/bin/sh
case "$*" in
  *--version*)
    printf '%s\n' 'iperf 3.16'
    exit 0
    ;;
esac
printf '%s\n' '{
  "start":{"connected":[{"local_host":"192.0.2.2","remote_host":"192.0.2.1"}]},
  "end":{
    "sum_sent":{"seconds":1,"bytes":12500000,"bits_per_second":100000000,"retransmits":2},
    "sum_received":{"seconds":1,"bytes":12000000,"bits_per_second":96000000}
  }
}'
`
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SpeedThreads = 2
	cfg.IPerfDuration = time.Second
	cfg.IPerfTargets = []config.IPerfEndpoint{{
		Name: "test", Host: "example.com", PortStart: 5201, PortEnd: 5201, Location: "local", Networks: "IPv4",
	}}
	result := runIPerfSpeed(context.Background(), Environment{Config: cfg}, helper)
	if result.Status != "ok" || result.Methodology.Kind != "standard-benchmark" || len(result.Measurements) != 2 {
		t.Fatalf("iperf result = %+v", result)
	}
	if result.Measurements[0].Value != 96 {
		t.Fatalf("iperf Mbps = %+v", result.Measurements)
	}
	for _, measurement := range result.Measurements {
		if strings.Contains(measurement.Key, "median") || strings.Contains(measurement.Key, "average") {
			t.Fatalf("iperf3 must preserve per-target values: %+v", measurement)
		}
	}
}
