package probe

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
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
	if got := parseHumanBytes("1.5G"); got != 1610612736 {
		t.Fatalf("bytes = %d", got)
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

func TestRunFIODiskWithLocalJSONAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses /bin/sh")
	}
	directory := t.TempDir()
	helper := filepath.Join(directory, "fio")
	script := `#!/bin/sh
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

func TestSysbenchParsersAndAdapters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses /bin/sh")
	}
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
	script := `#!/bin/sh
case "$*" in
  *--version*)
    printf '%s\n' 'sysbench 1.0.20'
    ;;
  *cpu*)
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
	if cpu.Status != "ok" || cpu.Methodology.Kind != "standard-benchmark" || len(cpu.Measurements) != 2 {
		t.Fatalf("sysbench CPU result = %+v", cpu)
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

func TestRunIPerfWithLocalJSONAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses /bin/sh")
	}
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
