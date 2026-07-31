package probe

import (
	"context"
	"net/http"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type Environment struct {
	Config     config.Runtime
	HTTPClient *http.Client
	UserAgent  string
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
		cpuProbe{},
		memoryProbe{},
		diskProbe{},
		dnsProbe{},
		latencyProbe{},
		speedProbe{},
		portsProbe{},
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
