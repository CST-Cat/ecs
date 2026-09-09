package probe

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"ecs/internal/config"
	"ecs/internal/model"
)

// traceHopSummaryMethod describes the facts consumed by the route module. It
// deliberately does not name an implementation: NextTrace and the FreeBSD
// base traceroute adapter both produce the same hop-count facts, while their
// source and engine provenance remain distinct below.
const traceHopSummaryMethod = "trace-hop-summary-v1"

const (
	traceNextTraceEngineName      = "nexttrace-tiny"
	traceNextTraceAdapter         = "nexttrace-json-v1"
	traceFreeBSDTracerouteAdapter = "freebsd-traceroute-text-v1"
	traceRawOutputTitle           = "probe.route.raw_output"
	traceRawStderrTitle           = "probe.route.raw_stderr"
	traceNormalizedJSONTitle      = "probe.route.normalized_trace_json"
	traceFreeBSDSourceName        = "probe.route.source.freebsd_traceroute.name"
	traceFreeBSDSourcePurpose     = "probe.route.source.freebsd_traceroute.purpose"
)

// traceResult is ECS's canonical trace fact model. Backends may use completely
// different wire formats, but route and future consumers only see this model.
// Empty optional values mean that the source did not provide that fact; they
// are never replaced with a fabricated zero or inferred identity.
type traceResult struct {
	Engine  string     `json:"engine"`
	Family  string     `json:"family"`
	Target  string     `json:"target"`
	Hops    []traceHop `json:"hops"`
	Adapter string     `json:"adapter,omitempty"`
}

type traceHop struct {
	Hop       int      `json:"hop"`
	Responded bool     `json:"responded"`
	IP        string   `json:"ip,omitempty"`
	RTTMS     *float64 `json:"rtt_ms,omitempty"`
	ASN       string   `json:"asn,omitempty"`
	Network   string   `json:"network,omitempty"`
	Location  string   `json:"location,omitempty"`
}

// traceBackend is the selected platform backend for one route run. The
// platform files provide discovery and execution; route.go never inspects a
// backend's native output format.
type traceBackend struct {
	Name    string
	Path    string
	Version string
	Adapter string
}

type traceCommandResult struct {
	Trace    traceResult
	Parsed   bool
	Stdout   []byte
	Stderr   []byte
	Err      error
	ParseErr error
}

// traceCommandSpec keeps the executable path separate from its arguments.
// The route result records only Args as its public command-argument fact; Path
// remains an execution-only platform boundary.
type traceCommandSpec struct {
	Path string
	Args []string
}

// traceArgumentVariant is the machine-stable command argument provenance for
// one concrete address family.  The executable path is intentionally kept out
// of this value: it is a platform execution boundary, while Args describes
// the arguments passed to that executable.
type traceArgumentVariant struct {
	Family string   `json:"family"`
	Args   []string `json:"args"`
}

// traceArgumentsForTargets records every distinct command-argument variant
// that route may execute for the selected targets.  A single variant retains
// the historical space-separated representation; mixed-family runs use a
// stable JSON array ordered by the first target that introduced each variant.
// The target itself is represented by <target>, as it was in the previous
// single-command provenance, so per-target addresses do not create spurious
// variants.  No localized presentation text is included.
func traceArgumentsForTargets(backend traceBackend, targets []config.Endpoint, mode string) string {
	return traceArgumentsForTargetsWithMaxHops(backend, targets, mode, 0)
}

// traceArgumentsForTargetsWithMaxHops is shared by route and backtrace. A
// zero limit preserves route's family-specific limits; a positive limit is
// used by backtrace because its 20-hop workload is part of its provenance.
func traceArgumentsForTargetsWithMaxHops(backend traceBackend, targets []config.Endpoint, mode string, maxHops int) string {
	variants := make([]traceArgumentVariant, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		family := endpointFamily(target, mode)
		limit := maxHops
		if limit <= 0 {
			limit = traceMaxHopsForFamily(family)
		}
		spec := traceCommandSpecForFamily(backend, "<target>", limit, family)
		if spec.Path == "" || len(spec.Args) == 0 {
			continue
		}
		identity := spec.Path + "\x00" + strings.Join(spec.Args, "\x00")
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		variants = append(variants, traceArgumentVariant{
			Family: traceFamilyName(family),
			Args:   append([]string(nil), spec.Args...),
		})
	}
	if len(variants) == 0 {
		return ""
	}
	if len(variants) == 1 {
		return strings.Join(variants[0].Args, " ")
	}
	encoded, err := json.Marshal(variants)
	if err != nil {
		// traceArgumentVariant contains only strings, so this is unreachable;
		// do not silently publish incomplete provenance if its shape changes.
		panic(fmt.Sprintf("marshal trace argument provenance: %v", err))
	}
	return string(encoded)
}

func (trace traceResult) canonicalJSON() ([]byte, error) {
	if strings.TrimSpace(trace.Engine) == "" || strings.TrimSpace(trace.Family) == "" || strings.TrimSpace(trace.Target) == "" {
		return nil, fmt.Errorf("trace result is missing engine, family, or target")
	}
	if len(trace.Hops) == 0 {
		return nil, fmt.Errorf("trace result contains no hop slots")
	}
	return json.Marshal(trace)
}

func traceFamilyName(family string) string {
	switch strings.TrimSpace(family) {
	case config.IPVersion4, "ipv4":
		return "ipv4"
	case config.IPVersion6, "ipv6":
		return "ipv6"
	default:
		return "auto"
	}
}

func traceMaxHopsParameter(targets []config.Endpoint, mode string) string {
	hasTwelve, hasTwenty := false, false
	for _, target := range targets {
		switch traceMaxHopsForFamily(endpointFamily(target, mode)) {
		case 20:
			hasTwenty = true
		default:
			hasTwelve = true
		}
	}
	switch {
	case hasTwelve && hasTwenty:
		return "12/20"
	case hasTwenty:
		return "20"
	default:
		return strconv.Itoa(routeSnapshotHops)
	}
}

// traceHopSummary is intentionally the only counter logic used by route. A
// slot is a hop row, a visible hop is a response, and every other slot is a
// timeout/no-response fact.
func traceHopSummary(trace traceResult) (slots, visible, timeouts int, ok bool) {
	if strings.TrimSpace(trace.Engine) == "" || strings.TrimSpace(trace.Family) == "" || strings.TrimSpace(trace.Target) == "" || len(trace.Hops) == 0 {
		return 0, 0, 0, false
	}
	for _, hop := range trace.Hops {
		if hop.Hop <= 0 {
			return 0, 0, 0, false
		}
		slots++
		if hop.Responded {
			visible++
		}
	}
	return slots, visible, slots - visible, true
}

func traceRTTMS(value string) (*float64, bool) {
	value = strings.TrimSpace(normalizeTraceLatency(value))
	if value == "" {
		return nil, false
	}
	value = strings.TrimSpace(strings.TrimSuffix(value, "ms"))
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil, false
	}
	return &parsed, true
}

func traceBackendSource(backend traceBackend) model.Source {
	if backend.Adapter == traceFreeBSDTracerouteAdapter {
		return model.Source{
			Name:    traceFreeBSDSourceName,
			URL:     "https://man.freebsd.org/cgi/man.cgi?query=traceroute&sektion=8&manpath=FreeBSD+15.1-RELEASE",
			Purpose: traceFreeBSDSourcePurpose,
		}
	}
	return model.Source{
		Name:    "probe.route.source.nexttrace.name",
		URL:     "https://github.com/nxtrace/NTrace-core",
		Purpose: "probe.route.source.nexttrace",
	}
}

func traceRawLanguage(backend traceBackend) string {
	if backend.Adapter == traceFreeBSDTracerouteAdapter {
		return "text"
	}
	return "json"
}
