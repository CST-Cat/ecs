package config

import (
	"strings"
	"time"

	"ecs/internal/i18n"
)

const (
	ProfileStandard = "standard"
	ProfileFull     = "full"

	// DiskMatrixTime lets Crystal/ATTO run with their normal timing window.
	DiskMatrixTime = "time"
	// IPerfUDPDuration is the fixed UDP sample duration used by speed rows.
	IPerfUDPDuration = 5 * time.Second
	// DiskMatrixFixed makes ATTO transfer a fixed byte count per size rather
	// than ending each size after a time window.
	DiskMatrixFixed = "fixed"

	// IPVersionAuto lets each probe choose the usable protocol family it
	// supports, while dual-stack probes keep results separate.
	IPVersionAuto = "auto"
	IPVersion4    = "4"
	IPVersion6    = "6"
)

// ParseDiskMatrixMode parses --disk-matrix-mode.
func ParseDiskMatrixMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DiskMatrixTime:
		return DiskMatrixTime, nil
	case DiskMatrixFixed:
		return DiskMatrixFixed, nil
	default:
		return "", i18n.Errorf("err.unknownDiskMatrixMode", raw)
	}
}

type Endpoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind,omitempty"`
	// Family optionally pins hostname resolution and command-line routing to
	// IPv4 or IPv6. Empty means the probe may choose automatically.
	Family string `json:"family,omitempty"`
}

// Route target kinds are machine identities owned by the route configuration
// contract. Custom route endpoint kinds remain user-provided values.
const (
	RouteTargetKindGlobal        = "global"
	RouteTargetKindMainlandChina = "mainland_china"
)

// Backtrace carrier kinds are the only machine identities accepted for
// backtrace targets. They are configuration-owned so producer matching and
// validation share the same contract.
const (
	BacktraceCarrierTelecom = "telecom"
	BacktraceCarrierUnicom  = "unicom"
	BacktraceCarrierMobile  = "mobile"
)

type IPerfEndpoint struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	PortStart int    `json:"port_start"`
	PortEnd   int    `json:"port_end"`
	Location  string `json:"location,omitempty"`
	Networks  string `json:"networks,omitempty"`
	// Region is used to select a balanced subset of the public node pool.
	Region string `json:"region,omitempty"`
}

// OoklaServer pins an official Ookla server ID to a carrier label. Server IDs
// are user/config supplied because the external catalogue changes over time.
type OoklaServer struct {
	Carrier string `json:"carrier"`
	ID      int    `json:"id"`
}

type Runtime struct {
	Profile string
	Modules []string
	// Exposure is the maximum permitted external-contact level.
	Exposure         Exposure
	Reveal           bool
	IPVersion        string
	IPQualitySources []string
	Formats          []string
	Output           string
	NoColor          bool
	CPUTime          time.Duration
	DiskMiB          int
	DiskPath         string
	DiskMulti        bool
	// DiskMatrixMode selects the Crystal/ATTO measurement mode.
	DiskMatrixMode   string
	IPerfDuration    time.Duration
	IPerfTargets     []IPerfEndpoint
	HTTPTimeout      time.Duration
	DNSAttempts      int
	LatencyAttempts  int
	SpeedThreads     int
	DNSResolvers     []Endpoint
	LatencyTargets   []Endpoint
	RouteTargets     []Endpoint
	BacktraceTargets []Endpoint
	STUNServers      []Endpoint
	MediaRegions     []string
	OoklaServers     []OoklaServer
}

type Estimate struct {
	DurationText string
	DiskMiB      int
	NetworkMiB   int
	Notes        []string
}

type File struct {
	Profile          string          `json:"profile,omitempty"`
	Only             []string        `json:"only,omitempty"`
	Skip             []string        `json:"skip,omitempty"`
	Exposure         string          `json:"exposure,omitempty"`
	Reveal           *bool           `json:"reveal,omitempty"`
	IPVersion        string          `json:"ip_version,omitempty"`
	IPQualitySources []string        `json:"ip_quality_sources,omitempty"`
	Formats          []string        `json:"formats,omitempty"`
	Output           string          `json:"output,omitempty"`
	NoColor          *bool           `json:"no_color,omitempty"`
	CPUTime          string          `json:"cpu_time,omitempty"`
	DiskMiB          *int            `json:"disk_mib,omitempty"`
	DiskPath         string          `json:"disk_path,omitempty"`
	DiskMulti        *bool           `json:"disk_multi,omitempty"`
	DiskMatrixMode   string          `json:"disk_matrix_mode,omitempty"`
	IPerfDuration    string          `json:"iperf_duration,omitempty"`
	IPerfTargets     []IPerfEndpoint `json:"iperf_targets,omitempty"`
	HTTPTimeout      string          `json:"http_timeout,omitempty"`
	DNSAttempts      *int            `json:"dns_attempts,omitempty"`
	LatencyAttempts  *int            `json:"latency_attempts,omitempty"`
	SpeedThreads     *int            `json:"speed_threads,omitempty"`
	DNSResolvers     []Endpoint      `json:"dns_resolvers,omitempty"`
	LatencyTargets   []Endpoint      `json:"latency_targets,omitempty"`
	RouteTargets     []Endpoint      `json:"route_targets,omitempty"`
	BacktraceTargets []Endpoint      `json:"backtrace_targets,omitempty"`
	STUNServers      []Endpoint      `json:"stun_servers,omitempty"`
	MediaRegions     []string        `json:"media_regions,omitempty"`
	OoklaServers     []OoklaServer   `json:"ookla_servers,omitempty"`
}
