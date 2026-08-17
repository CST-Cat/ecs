//go:build integration

// 真实 iperf3 / ping / NextTrace 的端到端契约。

package probe

import (
	"context"
	"ecs/internal/config"
	"ecs/internal/model"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SpeedThreads = 2
	cfg.IPerfDuration = time.Second
	cfg.IPerfTargets = []config.IPerfEndpoint{{
		Name: "loopback", Host: "127.0.0.1", PortStart: port, PortEnd: port,
		Location: "local", Networks: "IPv4",
	}}

	result := runIPerfSpeed(context.Background(), Environment{
		Config:  cfg,
		Network: NetworkCapabilities{IPv4Usable: true},
	}, iperfPath)
	if result.Status != model.StatusOK || result.Methodology.Kind != "standard-benchmark" {
		t.Fatalf("iperf result = %+v", result)
	}
	directions := make(map[string]float64, 2)
	udp := make(map[string]float64, 2)
	retransmits := make(map[string]float64, 2)
	stability := make(map[string]float64, 6)
	for _, measurement := range result.Measurements {
		switch {
		case strings.HasSuffix(measurement.Key, "_upload_mbps"):
			if measurement.Value <= 0 {
				t.Fatalf("real iperf3 returned a non-positive upload throughput: %+v", measurement)
			}
			directions["upload"] = measurement.Value
		case strings.HasSuffix(measurement.Key, "_download_mbps"):
			if measurement.Value <= 0 {
				t.Fatalf("real iperf3 returned a non-positive download throughput: %+v", measurement)
			}
			directions["download"] = measurement.Value
		case strings.HasSuffix(measurement.Key, "_udp_loss_percent"):
			if measurement.Value < 0 || measurement.Value > 100 {
				t.Fatalf("real iperf3 returned an invalid UDP loss percentage: %+v", measurement)
			}
			udp["loss"] = measurement.Value
		case strings.HasSuffix(measurement.Key, "_udp_jitter_ms"):
			if measurement.Value < 0 {
				t.Fatalf("real iperf3 returned a negative UDP jitter: %+v", measurement)
			}
			udp["jitter"] = measurement.Value
		case strings.HasSuffix(measurement.Key, "_upload_retransmits"):
			retransmits["upload"] = measurement.Value
		case strings.HasSuffix(measurement.Key, "_download_retransmits"):
			retransmits["download"] = measurement.Value
		case strings.Contains(measurement.Key, "_interval_"):
			if measurement.Value < 0 {
				t.Fatalf("real iperf3 returned a negative interval diagnostic: %+v", measurement)
			}
			stability[measurement.Key] = measurement.Value
		default:
			t.Fatalf("unexpected iperf3 measurement: %+v", measurement)
		}
	}
	if len(directions) != 2 {
		t.Fatalf("both directions must be recorded: %+v", result.Measurements)
	}
	if len(retransmits) != 2 || len(stability) != 6 {
		t.Fatalf("both directions need retransmission and min/P50/CV diagnostics: %+v", result.Measurements)
	}
	if len(udp) != 2 {
		t.Fatalf("UDP loss and jitter must be recorded for every selected speed module: %+v", result.Measurements)
	}
	if result.Evidence == nil || result.Evidence.Valid != 3 || result.Evidence.Expected != 3 || result.Evidence.Unit != "operation" {
		t.Fatalf("iperf3 evidence = %+v, want 3/3 operations", result.Evidence)
	}
	if version := resultField(result, "version"); !strings.Contains(strings.ToLower(version), "iperf") {
		t.Fatalf("iperf3 version field = %q", version)
	}
	if got := resultField(result, "threads"); got != "2" {
		t.Fatalf("iperf3 threads field = %q", got)
	}
}

// 选中 speed 模块时，UDP 丢包与抖动同样走真实 iperf3。
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
	stats := runICMPPingFamily(context.Background(), "127.0.0.1", 3, 2*time.Second, "")
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

// 真实 NextTrace：跳点解析必须对本机安装的实现成立。
func TestRouteEngineTracesLoopback(t *testing.T) {
	engine := detectRouteEngine(context.Background())
	if engine.Path == "" {
		t.Skip("本机没有 NextTrace，跳过真实路由探测")
	}
	if engine.SHA256 == "" || len(engine.SHA256) != 64 {
		t.Fatalf("路由引擎缺少可复核的 SHA-256：%+v", engine)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := runRouteCommandForFamily(ctx, engine, "127.0.0.1", 5, config.IPVersionAuto)
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
