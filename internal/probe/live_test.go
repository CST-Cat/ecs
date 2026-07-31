//go:build live

package probe

// 实网测试：真实调用第三方数据源与公共节点，验证解析器仍认得上游的当前响应。
//
// 为什么单独用 build tag 隔离，而不是并入默认测试：
//
//   - 第三方限流、改版、下线都会让这些测试变红，但代码没有任何问题。让它们
//     阻塞每一个 PR 会训练所有人忽略红灯，那比没有测试更糟。
//   - 它们会把运行机器的出口 IP 发给表内每一家数据源，不该在别人 clone 下来
//     跑 go test 时悄悄发生。
//
// 但它们必须存在。固定样本只能证明解析器认得**历史**格式，证明不了上游没变；
// 2026-07 实测中 ipinfo.check.place 对全部查询返回 403 与 Cloudflare 挑战页，
// 依赖它的四个免密数据源全线失效——这类问题只有真实调用才发现得了。
//
// 运行方式：
//
//	go test -tags=live ./internal/probe/ -run TestLive -v
//
// CI 以定时任务和手动触发运行它们，不挂在 push/PR 上。

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
)

func liveEnvironment(t *testing.T) Environment {
	t.Helper()
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTPTimeout = 20 * time.Second
	cfg.IPQualitySources = []string{"all"}
	return Environment{
		Config:     cfg,
		HTTPClient: NewHTTPClient(cfg.HTTPTimeout),
		UserAgent:  "ecs/live-test",
	}
}

// 出口发现是所有网络模块的前提，它挂了后面全都没有意义。
func TestLiveIPLookup(t *testing.T) {
	env := liveEnvironment(t)
	defer env.HTTPClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	data, latency, err := lookupIP(ctx, env, "4")
	if err != nil {
		t.Fatalf("IPv4 出口发现失败（ipapi.is）：%v", err)
	}
	if data.IP == "" {
		t.Fatal("ipapi.is 未返回 IP")
	}
	if data.ASN.ASN == 0 && data.Company.Name == "" {
		t.Fatalf("ipapi.is 响应缺少 ASN 与公司字段，上游格式可能已变：%+v", data)
	}
	t.Logf("出口 IPv4 %s · AS%d %s · %s/%s · %s",
		data.IP, data.ASN.ASN, data.ASN.Organization,
		data.Location.CountryCode, data.Location.City, latency.Round(time.Millisecond))
}

// 逐个数据源验证：解析器是否还认得它们当前返回的东西。
//
// 个别数据源失败不判失败——限流、改版、地区封锁都可能发生，报告本来就会如实
// 标记。但**全部**失败说明链路整体断了或解析层坏了，那必须失败。
func TestLiveIPQualityProviders(t *testing.T) {
	env := liveEnvironment(t)
	defer env.HTTPClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	data, latency, err := lookupIP(ctx, env, "4")
	if err != nil {
		t.Fatalf("IPv4 出口发现失败：%v", err)
	}
	bundle := collectIPQuality(ctx, env, ipLookup{Version: "4", Data: data, Latency: latency})

	var succeeded, failed, partial []string
	if bundle.Origin.Enabled {
		if bundle.Origin.Err != nil {
			failed = append(failed, qualitySourceLabels["maxmind"]+"（"+bundle.Origin.Err.Error()+"）")
		} else {
			succeeded = append(succeeded, qualitySourceLabels["maxmind"])
			t.Logf("MaxMind 使用地 %s / 注册地 %s → %s",
				bundle.Origin.UsageCountry, bundle.Origin.RegisteredCountry, bundle.Origin.Label)
		}
	}
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		if !finding.Enabled {
			continue
		}
		switch {
		case finding.Err != nil:
			failed = append(failed, finding.Name+"（"+finding.Err.Error()+"）")
		case finding.Partial != "":
			partial = append(partial, finding.Name+"（"+finding.Partial+"）")
		default:
			succeeded = append(succeeded, finding.Name)
		}
		if finding.Err == nil {
			// 拿到响应却一个字段都没解析出来，说明上游格式变了而解析器没跟上。
			if !findingHasEvidence(finding) {
				t.Errorf("%s 返回了响应但解析不出任何字段，上游格式可能已变", finding.Name)
			}
			score := "—"
			if finding.Score != nil {
				score = formatScore(*finding.Score) + "/100"
			}
			t.Logf("%-12s 通道=%-28s 国家=%-3s 分值=%-9s 风险=%s",
				finding.Name, finding.Access, fallback(finding.Country, "—"), score, fallback(finding.Risk, "—"))
		}
	}

	t.Logf("成功 %d：%s", len(succeeded), strings.Join(succeeded, "、"))
	if len(partial) > 0 {
		t.Logf("部分 %d：%s", len(partial), strings.Join(partial, "、"))
	}
	if len(failed) > 0 {
		t.Logf("失败 %d：%s", len(failed), strings.Join(failed, "、"))
	}
	if len(succeeded)+len(partial) == 0 {
		t.Fatalf("全部 IP 质量数据源均失败，链路或解析层已整体失效：%s", strings.Join(failed, "；"))
	}
}

// 社区中转是免密模式的关键一环，它的可用性必须能被单独看到。
func TestLiveCommunityGateway(t *testing.T) {
	env := liveEnvironment(t)
	defer env.HTTPClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	data, _, err := lookupIP(ctx, env, "4")
	if err != nil {
		t.Fatalf("IPv4 出口发现失败：%v", err)
	}
	client := newIPVersionHTTPClient(env.Config.HTTPTimeout, "4")
	defer client.CloseIdleConnections()

	for _, database := range []string{"", "abuseipdb", "scamalytics", "ipdata"} {
		label := database
		if label == "" {
			label = "maxmind(lang=en)"
		}
		body, latency, err := requestBytes(ctx, client, env.UserAgent, communityURL(data.IP, database), nil, 1024*1024)
		if err != nil {
			// 不判失败：这正是需要被观测的状态，而不是需要被隐藏的状态。
			t.Logf("check.place %-16s 不可用：%v（%s）", label, err, latency.Round(time.Millisecond))
			continue
		}
		t.Logf("check.place %-16s 可用：%d 字节（%s）", label, len(body), latency.Round(time.Millisecond))
	}
}

// 流媒体规则的强判定必须在真实页面上仍然成立。
func TestLiveMediaStrongRules(t *testing.T) {
	env := liveEnvironment(t)
	defer env.HTTPClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	unknown := 0
	strong := 0
	for _, check := range mediaChecks() {
		if check.Strength != strengthStrong {
			continue
		}
		strong++
		item := runMediaCheck(ctx, env, check)
		t.Logf("%-16s %-8s 地区=%-3s 证据=%s（状态码 %v）",
			check.Name, item.Verdict.State, fallback(item.Verdict.Region, "—"),
			item.Verdict.Evidence, item.Statuses)
		if item.Verdict.State == stateUnknown {
			unknown++
		}
	}
	if strong == 0 {
		t.Fatal("没有强规则可测")
	}
	// 强规则全部退化成"未知"说明页面结构变了或出口被全面拦截，需要人工核对。
	if unknown == strong {
		t.Errorf("全部 %d 条强规则都退化为“未知”，规则包可能已失效", strong)
	}
}

// 公共 iperf3 节点清单来自 YABS，需要定期确认还活着。
//
// 与 runIPerfDirection 保持一致：在端口范围内最多试三个。iperf3 服务端一次只
// 服务一个测试，忙碌时单个端口会直接 connection refused，只探首个端口会把
// "当时那个端口忙"误报成"节点挂了"。
func TestLiveIPerfNodeReachability(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IPerfTargets) == 0 {
		t.Fatal("节点池为空")
	}
	reachable := 0
	for _, target := range cfg.IPerfTargets {
		attempts := target.PortEnd - target.PortStart + 1
		if attempts > 3 {
			attempts = 3
		}
		if attempts < 1 {
			attempts = 1
		}
		var lastErr error
		connected := false
		for attempt := 0; attempt < attempts && !connected; attempt++ {
			port := target.PortStart + attempt
			address := net.JoinHostPort(target.Host, strconv.Itoa(port))
			start := time.Now()
			connection, err := net.DialTimeout("tcp", address, 10*time.Second)
			elapsed := time.Since(start)
			if err != nil {
				lastErr = err
				continue
			}
			_ = connection.Close()
			connected = true
			reachable++
			t.Logf("%-10s %-34s 可达（%s，端口 %d，%s）",
				target.Name, target.Host, target.Location, port, elapsed.Round(time.Millisecond))
		}
		if !connected {
			t.Logf("%-10s %-34s 端口 %d-%d 均不可达：%v",
				target.Name, target.Host, target.PortStart, target.PortStart+attempts-1, compactError(lastErr))
		}
	}
	if reachable == 0 {
		t.Fatalf("节点池里 %d 个公共 iperf3 节点全部不可达", len(cfg.IPerfTargets))
	}
	t.Logf("可达 %d/%d", reachable, len(cfg.IPerfTargets))
}
