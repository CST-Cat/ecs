package probe

import (
	"context"
	"fmt"
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

// probeFactories contains implementation constructors only.  The concrete
// instance supplies its own ID when the factory is registered below; IDs and
// ordering therefore have a single source of truth in config's descriptor
// registry instead of being repeated in a second string-keyed table.
var probeFactories = []func() Probe{
	func() Probe { return systemProbe{} },
	func() Probe { return networkProbe{} },
	func() Probe { return bgpProbe{} },
	func() Probe { return cpuProbe{} },
	func() Probe { return memoryProbe{} },
	func() Probe { return diskProbe{} },
	func() Probe { return dnsProbe{} },
	func() Probe { return latencyProbe{} },
	func() Probe { return speedProbe{} },
	func() Probe { return portsProbe{} },
	func() Probe { return natProbe{} },
	func() Probe { return blacklistProbe{} },
	func() Probe { return appsProbe{} },
	func() Probe { return cnSpeedProbe{} },
	func() Probe { return ooklaProbe{} },
	func() Probe { return mediaProbe{} },
	func() Probe { return routeProbe{} },
	func() Probe { return backtraceProbe{} },
}

func init() {
	registerProbeFactories()
}

func registerProbeFactories() {
	for _, factory := range probeFactories {
		if factory == nil {
			continue
		}
		instance := factory()
		if instance == nil {
			continue
		}
		id := instance.ID()
		if id == "" {
			continue
		}
		factory := factory
		// The descriptor registry is the source of ordering and metadata; this
		// package only supplies the concrete constructor behind each instance ID.
		_ = config.RegisterModuleFactory(id, func() any { return factory() })
	}
}

// ValidateRegistry verifies that every concrete probe is represented exactly
// once by the canonical descriptor registry and that the two pieces of
// execution metadata agree.  Keeping this check next to registration catches
// the easy-to-miss failure modes when a new probe is added: a typo in ID, a
// stale NeedsNetwork declaration, or a missing constructor binding.
func ValidateRegistry() error {
	factoryIDs := make(map[string]bool, len(probeFactories))
	for index, factory := range probeFactories {
		if factory == nil {
			return fmt.Errorf("probe factory %d is nil", index)
		}
		instance := factory()
		if instance == nil {
			return fmt.Errorf("probe factory %d returned nil", index)
		}
		id := instance.ID()
		if id == "" {
			return fmt.Errorf("probe factory %d returned a probe with empty ID", index)
		}
		if factoryIDs[id] {
			return fmt.Errorf("duplicate probe ID %q", id)
		}
		factoryIDs[id] = true
		descriptor, ok := config.ModuleDescriptorFor(id)
		if !ok {
			return fmt.Errorf("probe %q has no module descriptor", id)
		}
		if instance.NeedsNetwork() != descriptor.NeedsNetwork {
			return fmt.Errorf("probe %q NeedsNetwork=%v disagrees with descriptor=%v", id, instance.NeedsNetwork(), descriptor.NeedsNetwork)
		}
	}

	descriptors := config.ModuleDescriptors()
	if len(factoryIDs) != len(descriptors) {
		return fmt.Errorf("probe factory count=%d, descriptor count=%d", len(factoryIDs), len(descriptors))
	}
	for _, descriptor := range descriptors {
		if descriptor.ProbeFactory == nil {
			return fmt.Errorf("module %q has no registered probe factory", descriptor.ID)
		}
		candidate, ok := descriptor.ProbeFactory().(Probe)
		if !ok || candidate == nil {
			return fmt.Errorf("module %q factory does not return a Probe", descriptor.ID)
		}
		if candidate.ID() != descriptor.ID {
			return fmt.Errorf("module descriptor %q factory returned probe %q", descriptor.ID, candidate.ID())
		}
		if candidate.NeedsNetwork() != descriptor.NeedsNetwork {
			return fmt.Errorf("module %q factory NeedsNetwork=%v disagrees with descriptor=%v", descriptor.ID, candidate.NeedsNetwork(), descriptor.NeedsNetwork)
		}
	}
	return nil
}

func Registry() []Probe {
	registerProbeFactories()
	descriptors := config.ModuleDescriptors()
	registry := make([]Probe, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.ProbeFactory == nil {
			// A missing constructor is intentionally omitted here.  Registry
			// consistency tests report the actionable ID rather than panicking
			// during package initialization.
			continue
		}
		candidate, ok := descriptor.ProbeFactory().(Probe)
		if !ok || candidate == nil {
			continue
		}
		registry = append(registry, candidate)
	}
	return registry
}

func MethodologyFor(id string) model.Methodology {
	if descriptor, ok := config.ModuleDescriptorFor(id); ok {
		return descriptor.Methodology
	}
	return model.Methodology{}
}
