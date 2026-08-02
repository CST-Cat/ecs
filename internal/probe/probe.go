package probe

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type Environment struct {
	Config     config.Runtime
	HTTPClient *http.Client
	UserAgent  string
}

// tcpNetworkForMode turns the user-facing IP family setting into the network
// name accepted by net.Dialer.  Auto deliberately leaves family selection to
// the resolver/kernel, preserving the historical dual-stack behaviour.
func tcpNetworkForMode(mode string) string {
	switch mode {
	case config.IPVersion4:
		return "tcp4"
	case config.IPVersion6:
		return "tcp6"
	}
	return "tcp"
}

func udpNetworkForMode(mode string) string {
	switch mode {
	case config.IPVersion4:
		return "udp4"
	case config.IPVersion6:
		return "udp6"
	default:
		return "udp"
	}
}

// httpClientForMode returns the shared client for auto mode and a constrained
// client for an explicit family.  The caller owns the latter.
func httpClientForMode(env Environment) (*http.Client, func()) {
	if env.Config.IPVersion != config.IPVersion4 && env.Config.IPVersion != config.IPVersion6 {
		return env.HTTPClient, func() {}
	}
	client := newIPVersionHTTPClient(env.Config.HTTPTimeout, env.Config.IPVersion)
	return client, client.CloseIdleConnections
}

// endpointMatchesIPVersion filters only literal addresses.  A hostname is
// left in the set because the resolver may have records for the requested
// family and the probe should be allowed to discover that at runtime.
func endpointMatchesIPVersion(address, version string) bool {
	host := address
	if parsedHost, _, err := net.SplitHostPort(address); err == nil {
		host = parsedHost
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return true
	}
	switch version {
	case config.IPVersion4:
		return ip.To4() != nil
	case config.IPVersion6:
		return ip.To4() == nil
	default:
		return true
	}
}

func endpointsForIPVersion(endpoints []config.Endpoint, mode string) []config.Endpoint {
	if mode != config.IPVersion4 && mode != config.IPVersion6 {
		return append([]config.Endpoint(nil), endpoints...)
	}
	filtered := make([]config.Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Family == config.IPVersion4 || endpoint.Family == config.IPVersion6 {
			if endpoint.Family != mode {
				continue
			}
		}
		if endpointMatchesIPVersion(endpoint.Address, mode) {
			filtered = append(filtered, endpoint)
		}
	}
	return filtered
}

// endpointFamily chooses the concrete family for a single target.  It is
// intentionally kept at the last possible moment: auto mode can test both
// families, while a target marked family=6 must still reach an IPv6 hostname
// with an explicit -6 traceroute flag.
func endpointFamily(endpoint config.Endpoint, fallback string) string {
	if endpoint.Family == config.IPVersion4 || endpoint.Family == config.IPVersion6 {
		return endpoint.Family
	}
	host := endpoint.Address
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return config.IPVersion4
		}
		return config.IPVersion6
	}
	if strings.Contains(strings.ToLower(host), "-v6.") {
		return config.IPVersion6
	}
	if fallback == config.IPVersion4 || fallback == config.IPVersion6 {
		return fallback
	}
	return config.IPVersionAuto
}

type Probe interface {
	ID() string
	Title() string
	NeedsNetwork() bool
	Run(context.Context, Environment) model.Result
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxIdleConns = 16
	transport.MaxIdleConnsPerHost = 8
	transport.IdleConnTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = timeout
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func Registry() []Probe {
	return []Probe{
		systemProbe{},
		networkProbe{},
		bgpProbe{},
		cpuProbe{},
		memoryProbe{},
		diskProbe{},
		dnsProbe{},
		latencyProbe{},
		speedProbe{},
		portsProbe{},
		natProbe{},
		blacklistProbe{},
		appsProbe{},
		cnSpeedProbe{},
		ooklaProbe{},
		mediaProbe{},
		routeProbe{},
		backtraceProbe{},
	}
}

func MethodologyFor(id string) model.Methodology {
	switch id {
	case "system":
		return model.Methodology{Kind: "inventory", Label: "事实采集", Engine: "OS/runtime inspection", ComparisonScope: "资源快照；不是性能基准"}
	case "network":
		return model.Methodology{Kind: "provider-assessment", Label: "第三方评估", Engine: "multi-provider IP intelligence", ComparisonScope: "不同供应商分数不可直接混算"}
	case "bgp":
		return model.Methodology{Kind: "provider-assessment", Label: "第三方评估", Engine: "RouteViews current RIB API", ComparisonScope: "当前公共观测；不是私有互联全图或历史 BGP 分析"}
	case "cpu":
		return model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "sysbench", Profile: "cpu prime=20000"}
	case "memory":
		return model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "sysbench", Profile: "memory seq 1 MiB"}
	case "disk":
		return model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "fio", Profile: "Direct I/O"}
	case "dns":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "DNS/UDP", ComparisonScope: "现场诊断；不是基准分"}
	case "latency":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "TCP connect", ComparisonScope: "现场诊断；不是 ICMP ping"}
	case "speed":
		return model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "iperf3", Profile: "TCP multi-stream forward/reverse"}
	case "ports":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "TCP connect", ComparisonScope: "可达性诊断；不是性能基准"}
	case "nat":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "STUN (RFC 5389/5780)", ComparisonScope: "当次 UDP 路径上的 NAT 行为；不代表 TCP"}
	case "blacklist":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "DNSBL over DNS A lookup", ComparisonScope: "各名单收录标准不同，不可合并计分"}
	case "apps":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "native TCP connect", ComparisonScope: "网络可达性诊断；不代表服务本身可用"}
	case "cnspeed":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "HTTP download against speedtest.cn nodes", ComparisonScope: "到具体节点的 HTTP 带宽；不代表到该运营商全网"}
	case "ookla":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "official Ookla Speedtest CLI", ComparisonScope: "外部服务测量；Ookla 的条款与数据处理独立于 ecs"}
	case "media":
		return model.Methodology{Kind: "heuristic", Label: "启发式判断", Engine: "public HTTP evidence", ComparisonScope: "不等同账号播放、注册或支付能力"}
	case "route":
		return model.Methodology{Kind: "protocol-measurement", Label: "协议诊断", Engine: "NextTrace/traceroute/tracepath", ComparisonScope: "正向路径快照；不是性能基准"}
	case "backtrace":
		return model.Methodology{Kind: "heuristic", Label: "启发式判断", Engine: "NextTrace/traceroute + 骨干网段特征表", ComparisonScope: "路径特征推断；不是反向抓包"}
	default:
		return model.Methodology{}
	}
}
