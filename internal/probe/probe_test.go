package probe

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

	// quick 档只跑首尾两档混合，避免时长失控；Crystal/ATTO 仍必须完整保留。
	quickPlan := fioJobPlan(config.ProfileQuick)
	quickArgs := strings.Join(fioArguments("<tempfile>", 1<<20, time.Second, asyncEngine, quickPlan), " ")
	if strings.Contains(quickArgs, "--name=mix64k") {
		t.Fatalf("quick profile should skip 64k mixed job: %s", quickArgs)
	}
	for _, expected := range []string{"--name=crystal_read_rnd4k_q1", "--name=crystal_write_seq1m_q8", "--name=atto_read_512b", "--name=atto_write_64m"} {
		if !strings.Contains(quickArgs, expected) {
			t.Fatalf("quick profile must include complete matrix job %q: %s", expected, quickArgs)
		}
	}
}

// requireTool 返回真实基准工具的路径。
//
// 这些测试跑真实的 fio / sysbench / iperf3，不使用脚本替身：替身只能证明解析器
// 认得它自己造出来的输出，证明不了它认得工具的真实输出。CI 会安装这三个工具，
// 因此在 CI 上缺失一律直接失败，避免测试静默跳过后仍然显示为绿。
func requireTool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s 未安装：CI 必须以真实工具运行标准基准测试", name)
	}
	t.Skipf("%s 未安装，跳过真实基准测试；安装：apt-get install -y sysbench fio iperf3", name)
	return ""
}

func resultField(result model.Result, key string) string {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value
		}
	}
	return ""
}

// 探测结果必须真的出自 fio --enghelp 的可用列表，而不是猜的。
func TestDetectFIOEngineAgreesWithEnghelp(t *testing.T) {
	fioPath := requireTool(t, "fio")
	engine := detectFIOEngine(context.Background(), fioPath)
	if !engine.Detected {
		t.Fatalf("engine detection failed on a working fio: %+v", engine)
	}

	output, err := exec.Command(fioPath, "--enghelp").CombinedOutput()
	if err != nil {
		t.Fatalf("fio --enghelp: %v", err)
	}
	available := make(map[string]bool)
	for _, line := range strings.Split(sanitizeCommandOutput(output), "\n") {
		available[strings.TrimSpace(line)] = true
	}
	if !available[engine.Name] {
		t.Fatalf("detected engine %q is not in fio --enghelp: %v", engine.Name, output)
	}
	// AsyncQueue 决定报告里标注的队列深度，标错会让 QD1 成绩冒充 QD64。
	if want := engine.Name == "io_uring" || engine.Name == "libaio"; engine.AsyncQueue != want {
		t.Fatalf("engine %q AsyncQueue = %v, want %v", engine.Name, engine.AsyncQueue, want)
	}
	// 优先级必须是 io_uring > libaio > psync。
	if available["io_uring"] && engine.Name != "io_uring" {
		t.Fatalf("io_uring is available but %q was chosen", engine.Name)
	}
	if !available["io_uring"] && available["libaio"] && engine.Name != "libaio" {
		t.Fatalf("libaio is available but %q was chosen", engine.Name)
	}
	t.Logf("fio %s 探测到引擎 %s（异步=%v）", fioPath, engine.Name, engine.AsyncQueue)
}

// 真实 fio 端到端：报告标注的队列深度必须与实际生效的引擎能力一致。
//
// 断言写成不变式而不是硬编码某个引擎名——同一份代码在有 io_uring 的内核、
// 只有 libaio 的内核和只有 psync 的精简发行版上都必须自洽。
func TestRunFIODiskWithRealFIO(t *testing.T) {
	fioPath := requireTool(t, "fio")
	directory := t.TempDir()
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DiskPath = directory
	cfg.DiskMiB = 16
	engine := detectFIOEngine(context.Background(), fioPath)
	result := runFIODisk(context.Background(), Environment{Config: cfg}, fioPath)
	if result.Status != model.StatusOK {
		t.Fatalf("fio result = %+v", result)
	}

	if got := resultField(result, "ioengine"); !strings.Contains(got, engine.Name) {
		t.Fatalf("ioengine field = %q, want it to name %q", got, engine.Name)
	}
	if got := resultField(result, "version"); !strings.HasPrefix(got, "fio-") {
		t.Fatalf("fio version field = %q", got)
	}
	if got := resultField(result, "binary_sha256"); len(got) != 64 {
		t.Fatalf("fio SHA-256 = %q", got)
	}

	methods := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		methods[measurement.Key] = measurement.Method
		if measurement.Value <= 0 {
			t.Fatalf("real fio produced a non-positive value: %+v", measurement)
		}
	}
	arguments := resultField(result, "arguments")
	randomMethod := methods["fio_random_read_4k_iops"]
	mixedMethod := methods["fio_mixed_4k_read_mib_s"]
	if engine.AsyncQueue {
		for _, expected := range []string{"--iodepth=32", "--iodepth=64"} {
			if !strings.Contains(arguments, expected) {
				t.Fatalf("async engine %q must request %s: %s", engine.Name, expected, arguments)
			}
		}
		if !strings.Contains(randomMethod, "qd32") || !strings.Contains(mixedMethod, "qd64") {
			t.Fatalf("async methods = %q / %q, want qd32 / qd64", randomMethod, mixedMethod)
		}
		if !strings.Contains(resultField(result, "ioengine"), "异步") {
			t.Fatalf("async engine must be labelled as such: %q", resultField(result, "ioengine"))
		}
		for _, note := range result.Notes {
			if strings.Contains(note, "队列深度对它无效") {
				t.Fatalf("async engine must not be labelled synchronous: %q", note)
			}
		}
	} else {
		if strings.Contains(arguments, "--iodepth=32") || strings.Contains(arguments, "--iodepth=64") {
			t.Fatalf("sync engine %q must not request a queue depth: %s", engine.Name, arguments)
		}
		if !strings.Contains(randomMethod, "qd1") || !strings.Contains(mixedMethod, "qd1") {
			t.Fatalf("sync methods = %q / %q, want qd1", randomMethod, mixedMethod)
		}
		downgradeNoted := false
		for _, note := range result.Notes {
			if strings.Contains(note, "队列深度对它无效") {
				downgradeNoted = true
			}
		}
		if !downgradeNoted {
			t.Fatalf("sync engine must disclose the queue-depth downgrade: %+v", result.Notes)
		}
	}

	// 四项基础指标 + 两个 P95 + quick 档两档混合各读写两项 +
	// Crystal 8 个读写单元 × 吞吐/IOPS + ATTO 36 个读写单元 × 吞吐/IOPS。
	if len(result.Measurements) != 98 {
		t.Fatalf("fio measurements = %d, want 98: %+v", len(result.Measurements), result.Measurements)
	}
	measurementKeys := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		measurementKeys[measurement.Key] = true
	}
	for _, block := range attoBlockSizes {
		for _, suffix := range []string{"read_mib_s", "read_iops", "write_mib_s", "write_iops"} {
			key := "atto_" + block.FIO + "_" + suffix
			if !measurementKeys[key] {
				t.Fatalf("quick result missing ATTO measurement %q", key)
			}
		}
	}
	crystalKeys := 0
	for key := range measurementKeys {
		if strings.HasPrefix(key, "crystal_") {
			crystalKeys++
		}
	}
	if crystalKeys != 16 {
		t.Fatalf("quick result Crystal measurement cells = %d, want 16", crystalKeys)
	}
	mixedFound := false
	crystalFound, attoFound := false, false
	for _, table := range result.Tables {
		if strings.Contains(table.Title, "混合") && len(table.Rows) == 2 {
			mixedFound = true
		}
		if table.Title == "Crystal" {
			if len(table.Rows) != 4 || len(table.Rows)*2 != 8 {
				t.Fatalf("quick Crystal rows/cells = %d/%d, want 4/8", len(table.Rows), len(table.Rows)*2)
			}
			crystalFound = true
		}
		if table.Title == "ATTO" {
			if len(table.Rows) != 18 || len(table.Rows)*2 != 36 {
				t.Fatalf("quick ATTO rows/cells = %d/%d, want 18/36", len(table.Rows), len(table.Rows)*2)
			}
			for _, row := range table.Rows {
				if len(row) < 5 || row[1] == "—" || row[2] == "—" || row[3] == "—" || row[4] == "—" {
					t.Fatalf("quick ATTO row has missing read/write cell: %v", row)
				}
			}
			attoFound = true
		}
	}
	if !mixedFound {
		t.Fatalf("mixed matrix table missing: %+v", result.Tables)
	}
	if !crystalFound || !attoFound {
		t.Fatalf("quick complete matrix tables missing: crystal=%v atto=%v", crystalFound, attoFound)
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(jsonResult)
	for _, key := range []string{"atto_512b_read_mib_s", "atto_512b_write_iops", "atto_64m_read_mib_s", "atto_64m_write_iops"} {
		if !strings.Contains(jsonText, key) {
			t.Fatalf("quick JSON missing ATTO measurement key %q", key)
		}
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".ecs-fio-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("fio temporary files remain: %v", matches)
	}
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
	if rate := memoryRateToMiB(2, "GiB"); rate != 2048 {
		t.Fatalf("memory rate = %f", rate)
	}
}

// 真实 sysbench 端到端：解析器必须认得当前安装版本的实际输出格式。
func TestRunSysbenchWithRealBinary(t *testing.T) {
	sysbenchPath := requireTool(t, "sysbench")
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CPUTime = time.Second

	cpu := runSysbenchCPU(context.Background(), Environment{Config: cfg}, sysbenchPath)
	// 宿主机 steal 高时降级为 warning 是正确行为，只有 error 才算失败。
	if cpu.Status == model.StatusError || cpu.Methodology.Kind != "standard-benchmark" {
		t.Fatalf("sysbench CPU result = %+v", cpu)
	}
	cpuValues := make(map[string]float64, len(cpu.Measurements))
	for _, measurement := range cpu.Measurements {
		cpuValues[measurement.Key] = measurement.Value
		if strings.Contains(measurement.Key, "efficiency") {
			t.Fatalf("CPU must not emit derived score: %+v", measurement)
		}
	}
	// steal 缺失在 Linux 上就是 bug：/proc/stat 必然可读，读不到说明采样逻辑坏了。
	for _, required := range []string{
		"sysbench_cpu_single_events_s",
		"sysbench_cpu_multi_events_s",
		"cpu_steal_percent_during_test",
	} {
		if _, ok := cpuValues[required]; !ok {
			t.Fatalf("sysbench CPU missing %q: %+v", required, cpu.Measurements)
		}
	}
	if cpuValues["sysbench_cpu_single_events_s"] <= 0 {
		t.Fatalf("real sysbench returned a non-positive event rate: %+v", cpu.Measurements)
	}
	if steal := cpuValues["cpu_steal_percent_during_test"]; steal < 0 || steal > 100 {
		t.Fatalf("steal percentage out of range: %f", steal)
	}
	if version := resultField(cpu, "version"); !strings.Contains(strings.ToLower(version), "sysbench") {
		t.Fatalf("sysbench version field = %q", version)
	}
	if len(cpu.TextBlocks) == 0 || !strings.Contains(cpu.TextBlocks[0].Content, "events per second") {
		t.Fatalf("raw sysbench output was not preserved: %+v", cpu.TextBlocks)
	}

	memory := runSysbenchMemory(context.Background(), Environment{Config: cfg}, sysbenchPath)
	if memory.Status != model.StatusOK || memory.Methodology.Kind != "standard-benchmark" {
		t.Fatalf("sysbench memory result = %+v", memory)
	}
	if len(memory.Measurements) < 8 {
		t.Fatalf("sysbench memory measurements = %d, want throughput plus latency for four contexts: %+v", len(memory.Measurements), memory.Measurements)
	}
	for _, measurement := range memory.Measurements {
		if measurement.Value <= 0 {
			t.Fatalf("real sysbench memory returned a non-positive rate: %+v", measurement)
		}
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

// startIPerf3Server 在回环地址上起一个真实 iperf3 服务端，返回它监听的端口。
//
// 用真实服务端而不是伪造 JSON：iperf3 的 JSON 字段名在版本间有过变化，
// 只有让真实服务端产出报文才能证明解析器跟得上当前安装的版本。
func startIPerf3Server(t *testing.T, path string) int {
	t.Helper()
	// 先让内核分配一个空闲端口再交给 iperf3，避免固定端口在开发机上撞车。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := exec.CommandContext(ctx, path, "-s", "-B", "127.0.0.1", "-p", strconv.Itoa(port))
	// 输出必须被消费，否则管道写满会把服务端卡住；内容本身不用于判断就绪。
	server.Stdout = io.Discard
	server.Stderr = io.Discard
	if err := server.Start(); err != nil {
		cancel()
		t.Fatalf("启动 iperf3 服务端: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = server.Wait()
	})

	// iperf3 的 "Server listening" 横幅在输出重定向到管道时会被 stdio 全缓冲
	// 卡住，端口却已经在监听了，所以只能轮询端口而不是扫描输出。
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			_ = connection.Close()
			// 让 iperf3 处理完这个探测用的空连接，再把端口交给被测代码。
			time.Sleep(300 * time.Millisecond)
			return port
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("iperf3 服务端在 %s 上未进入监听状态", address)
	return 0
}

// 真实 iperf3 端到端：逐节点、逐方向的原值必须被保留，不做任何聚合。
func TestRunIPerfWithRealServer(t *testing.T) {
	iperfPath := requireTool(t, "iperf3")
	port := startIPerf3Server(t, iperfPath)
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SpeedThreads = 2
	cfg.IPerfDuration = time.Second
	cfg.IPerfTargets = []config.IPerfEndpoint{{
		Name: "loopback", Host: "127.0.0.1", PortStart: port, PortEnd: port,
		Location: "local", Networks: "IPv4",
	}}

	result := runIPerfSpeed(context.Background(), Environment{Config: cfg}, iperfPath)
	if result.Status != model.StatusOK || result.Methodology.Kind != "standard-benchmark" {
		t.Fatalf("iperf result = %+v", result)
	}
	// 一个节点、一个协议族、上传与下载两个方向。
	if len(result.Measurements) != 2 {
		t.Fatalf("iperf measurements = %d: %+v", len(result.Measurements), result.Measurements)
	}
	directions := make(map[string]float64, 2)
	for _, measurement := range result.Measurements {
		if strings.Contains(measurement.Key, "median") || strings.Contains(measurement.Key, "average") {
			t.Fatalf("iperf3 must preserve per-target values: %+v", measurement)
		}
		if measurement.Value <= 0 {
			t.Fatalf("real iperf3 returned a non-positive throughput: %+v", measurement)
		}
		switch {
		case strings.HasSuffix(measurement.Key, "_upload_mbps"):
			directions["upload"] = measurement.Value
		case strings.HasSuffix(measurement.Key, "_download_mbps"):
			directions["download"] = measurement.Value
		}
	}
	if len(directions) != 2 {
		t.Fatalf("both directions must be recorded: %+v", result.Measurements)
	}
	if version := resultField(result, "version"); !strings.Contains(strings.ToLower(version), "iperf") {
		t.Fatalf("iperf3 version field = %q", version)
	}
	if got := resultField(result, "threads"); got != "2" {
		t.Fatalf("iperf3 threads field = %q", got)
	}
}

// full 档的 UDP 丢包与抖动同样走真实 iperf3。
func TestRunIPerfUDPWithRealServer(t *testing.T) {
	iperfPath := requireTool(t, "iperf3")
	port := startIPerf3Server(t, iperfPath)
	result := runIPerfUDP(context.Background(), iperfPath, "127.0.0.1", port, "IPv4", "10M", 2)
	if !result.Available {
		t.Fatalf("real iperf3 UDP run failed: %+v", result)
	}
	if result.Packets <= 0 {
		t.Fatalf("UDP packet count = %d", result.Packets)
	}
	if result.LostPercent < 0 || result.LostPercent > 100 {
		t.Fatalf("UDP loss out of range: %f", result.LostPercent)
	}
	if result.JitterMS < 0 {
		t.Fatalf("UDP jitter must not be negative: %f", result.JitterMS)
	}
	if result.Mbps <= 0 {
		t.Fatalf("UDP throughput = %f", result.Mbps)
	}
}

// 真实 ping：统计行的解析必须对本机安装的 ping 实现成立。
//
// 只断言在 iputils 与 busybox 上都成立的不变式，不假设某一种实现——
// 这正是四段/三段两条正则要覆盖的差异。
func TestRunICMPPingAgainstLoopback(t *testing.T) {
	requireTool(t, pingCommand)
	stats := runICMPPing(context.Background(), "127.0.0.1", 3, 2*time.Second)
	if !stats.Available {
		// 容器与部分 CI 会禁止 ICMP。这时必须给出明确错误，
		// 而不是静默返回一组零值冒充测量结果。
		if stats.Err == nil {
			t.Fatal("ICMP 不可用却没有报告错误")
		}
		t.Skipf("本机不允许 ICMP，已按设计降级：%v", stats.Err)
	}
	if stats.LossPercent != 0 {
		t.Fatalf("回环不应丢包：%f%%", stats.LossPercent)
	}
	if stats.AvgMS <= 0 {
		t.Fatalf("解析出的平均往返为 %f，说明统计行没被正确解析", stats.AvgMS)
	}
	if !(stats.MinMS <= stats.AvgMS && stats.AvgMS <= stats.MaxMS) {
		t.Fatalf("min/avg/max 不自洽：%+v", stats)
	}
	// busybox 不报告标准差，此时必须留空而不是填 0 冒充测量值。
	if stats.StdDevKnown && stats.StdDevMS < 0 {
		t.Fatalf("标准差为负：%f", stats.StdDevMS)
	}
	t.Logf("真实 ping 解析结果 min=%.3f avg=%.3f max=%.3f 标准差可用=%v",
		stats.MinMS, stats.AvgMS, stats.MaxMS, stats.StdDevKnown)
}

// 真实 traceroute：跳点解析必须对本机安装的实现成立。
func TestRouteEngineTracesLoopback(t *testing.T) {
	engine := detectRouteEngine(context.Background())
	if engine.Path == "" {
		requireTool(t, "traceroute")
	}
	if engine.SHA256 == "" || len(engine.SHA256) != 64 {
		t.Fatalf("路由引擎缺少可复核的 SHA-256：%+v", engine)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := runRouteCommand(ctx, engine, "127.0.0.1", 5)
	clean := sanitizeCommandOutput(output)
	if clean == "" {
		t.Fatalf("%s 没有产生任何输出：%v", engine.Name, err)
	}
	hops := extractTraceHops(engine.Name, clean)
	if len(hops) == 0 {
		t.Fatalf("%s 的输出解析不出跳点：\n%s", engine.Name, clean)
	}
	if hops[0] != "127.0.0.1" {
		t.Fatalf("第一跳 = %q，want 127.0.0.1；原始输出：\n%s", hops[0], clean)
	}
	if count := routeHopCount(engine.Name, clean); count < 1 {
		t.Fatalf("跳数统计 = %d：\n%s", count, clean)
	}
	t.Logf("真实 %s 解析出 %d 跳", engine.Name, len(hops))
}

// IPv6 可用性判定必须排除 ULA：有地址不等于能出网。
//
// 这个用例来自一次真实误判：机器上只有 Tailscale 的 fd7a::/48 ULA、没有 IPv6
// 默认路由，却被判为支持 IPv6，于是每个 iperf3 节点都白跑一轮并全部失败。
func TestHasGlobalUnicastIPv6(t *testing.T) {
	parse := func(cidr string) net.Addr {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		address, _, _ := net.ParseCIDR(cidr)
		network.IP = address
		return network
	}
	cases := []struct {
		name      string
		addresses []net.Addr
		want      bool
	}{
		{"Tailscale ULA", []net.Addr{parse("fd7a:115c:a1e0::cd37:b07d/128")}, false},
		{"链路本地", []net.Addr{parse("fe80::e2d4:e8ff:fe4b:579e/64")}, false},
		{"回环", []net.Addr{parse("::1/128")}, false},
		{"仅 IPv4", []net.Addr{parse("192.168.31.199/24")}, false},
		{"公网 IPv6", []net.Addr{parse("2001:db8::1/64")}, true},
		{"ULA 与公网并存", []net.Addr{
			parse("fd7a:115c:a1e0::cd37:b07d/128"),
			parse("2404:6800:4004::200e/64"),
		}, true},
		{"空列表", nil, false},
	}
	for _, testCase := range cases {
		if got := hasGlobalUnicastIPv6(testCase.addresses); got != testCase.want {
			t.Errorf("%s: hasGlobalUnicastIPv6 = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// 每个模块都要有方法学标注：漏了报告里就缺"这个数字是怎么来的、能和什么比"，
// 而那正是本项目区别于跑分脚本的地方。
func TestMethodologyCoversEveryModule(t *testing.T) {
	for _, item := range Registry() {
		methodology := MethodologyFor(item.ID())
		if methodology.Kind == "" || methodology.Label == "" {
			t.Errorf("模块 %q 缺少方法学标注（probe.go 的 MethodologyFor）", item.ID())
		}
	}
}

// 注册表与 ModuleOrder 必须一致，否则模块要么选不到，要么选了不跑。
func TestRegistryMatchesModuleOrder(t *testing.T) {
	registered := make(map[string]bool)
	for _, item := range Registry() {
		registered[item.ID()] = true
	}
	for _, id := range config.ModuleOrder {
		if !registered[id] {
			t.Errorf("ModuleOrder 里的 %q 没有对应探针", id)
		}
	}
	for id := range registered {
		found := false
		for _, ordered := range config.ModuleOrder {
			if ordered == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("探针 %q 不在 ModuleOrder 里，用户选不到它", id)
		}
	}
}
