package probe

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/failure"
	"ecs/internal/model"
)

type Environment struct {
	Config     config.Runtime
	HTTPClient *http.Client
	UserAgent  string
	// Network is the one local capability snapshot for this run. It is filled
	// by runner before DiscoverEgress and is shared by all network probes.
	// The zero value remains valid for direct probe/unit-test callers.
	Network NetworkCapabilities
	// Egress 是本次运行共享的出口地址快照，由 runner 在探针启动前发现一次。
	// 见 egress.go：需要出口 IP 的模块读这里，不再各自去查。
	Egress Egress
}

// tcpNetworkForMode turns the user-facing IP family setting into the network
// name accepted by net.Dialer. Auto deliberately leaves family selection to
// the resolver/kernel while keeping dual-stack results separate.
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
// with an explicit -6 NextTrace flag.
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

// addFailure keeps per-target operational failures structured while retaining
// the original error text for diagnosis. Repeated identical observations are
// coalesced by model.Result.AddFailure.
func addFailure(result *model.Result, stage, target string, err error, count ...int) {
	if result == nil || err == nil {
		return
	}
	entry := failure.FromError(stage, target, err)
	if len(count) > 0 && count[0] > 0 {
		entry.Count = count[0]
	}
	result.AddFailure(entry)
}

func addFailureMessage(result *model.Result, stage, target, message string, count ...int) {
	if result == nil || strings.TrimSpace(message) == "" {
		return
	}
	entry := failure.FromMessage(stage, target, message)
	if len(count) > 0 && count[0] > 0 {
		entry.Count = count[0]
	}
	result.AddFailure(entry)
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

// Builtins returns fresh concrete probe values for all built-in modules.
// Config owns module metadata and canonical order; probe only owns this typed
// implementation list. There is no runtime registration or mutable index.
func Builtins() []Probe {
	return []Probe{
		systemSemanticProbe{},
		networkProbe{},
		bgpProbe{},
		cpuProbe{},
		zstdProbe{},
		npbProbe{},
		memoryProbe{},
		cryptoProbe{},
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
	if descriptor, ok := config.ModuleDescriptorFor(id); ok {
		return descriptor.Methodology
	}
	return model.Methodology{}
}
