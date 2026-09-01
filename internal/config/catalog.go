package config

import "ecs/internal/i18n"

// ipQualitySourceIDs is the canonical set and order of configurable IP
// quality source identities. Keep presentation names and provider URLs out of
// this catalog: IDs are machine/configuration values and must not vary by
// language.
var ipQualitySourceIDs = []string{
	"maxmind",
	"ipinfo",
	"ipregistry",
	"ipapi",
	"ip2location",
	"abuseipdb",
	"scamalytics",
	"ipqs",
	"dbip",
	"ipdata",
	"ipwhois",
	"ipapicom",
	"ipsb",
}

// mediaRegionIDs is the canonical set and order of configurable media-region
// identities. Keep region-to-rule mappings in the probe package: this slice
// owns only the configuration identity and its stable selection order.
var mediaRegionIDs = []string{"global", "jp", "tw", "hk", "cn"}

// MediaRegionIDs returns the canonical media-region IDs in stable selection
// order. The returned slice is a copy, so callers cannot mutate the catalog.
func MediaRegionIDs() []string {
	return append([]string(nil), mediaRegionIDs...)
}

// IPQualitySourceIDs returns the canonical IP quality source IDs in their
// stable display, execution, and report order. The returned slice is a copy.
func IPQualitySourceIDs() []string {
	return append([]string(nil), ipQualitySourceIDs...)
}

// IsIPQualitySource reports whether id is a canonical configurable IP quality
// source identity. The all/none selectors are handled by their caller.
func IsIPQualitySource(id string) bool {
	for _, known := range ipQualitySourceIDs {
		if id == known {
			return true
		}
	}
	return false
}

// stunServerPool is the public STUN pool used for NAT behavior discovery.
func stunServerPool() []Endpoint {
	return []Endpoint{
		{Name: "Xiaomi", Address: "stun.miwifi.com:3478", Kind: STUNServerKindDualAddress},
		{Name: "1&1", Address: "stun.1und1.de:3478", Kind: STUNServerKindDualAddress},
		{Name: "Hoiio", Address: "stun.hoiio.com:3478", Kind: STUNServerKindDualAddress},
		{Name: "Google", Address: "stun.l.google.com:19302", Kind: STUNServerKindMappingOnly},
		{Name: "Cloudflare", Address: "stun.cloudflare.com:3478", Kind: STUNServerKindMappingOnly},
	}
}

// iperfNodePool is the curated public iperf3 node pool. Region is an ecs
// grouping used to keep the default selection geographically balanced.
func iperfNodePool() []IPerfEndpoint {
	return []IPerfEndpoint{
		{Name: "Clouvider", Host: "lon.speedtest.clouvider.net", PortStart: 5200, PortEnd: 5209, Location: "London, UK (10G)", Networks: "IPv4|IPv6", Region: "europe"},
		{Name: "Eranium", Host: "iperf-ams-nl.eranium.net", PortStart: 5201, PortEnd: 5210, Location: "Amsterdam, NL (100G)", Networks: "IPv4|IPv6", Region: "europe"},
		{Name: "Uztelecom", Host: "speedtest.uztelecom.uz", PortStart: 5200, PortEnd: 5209, Location: "Tashkent, UZ (10G)", Networks: "IPv4|IPv6", Region: "asia"},
		{Name: "Leaseweb", Host: "speedtest.sin1.sg.leaseweb.net", PortStart: 5201, PortEnd: 5210, Location: "Singapore, SG (10G)", Networks: "IPv4|IPv6", Region: "asia"},
		{Name: "Clouvider", Host: "la.speedtest.clouvider.net", PortStart: 5200, PortEnd: 5209, Location: "Los Angeles, CA, US (10G)", Networks: "IPv4|IPv6", Region: "america"},
		{Name: "Leaseweb", Host: "speedtest.nyc1.us.leaseweb.net", PortStart: 5201, PortEnd: 5210, Location: "NYC, NY, US (10G)", Networks: "IPv4|IPv6", Region: "america"},
		{Name: "Edgoo", Host: "speedtest.sao1.edgoo.net", PortStart: 9204, PortEnd: 9240, Location: "Sao Paulo, BR (1G)", Networks: "IPv4|IPv6", Region: "america"},
	}
}

// selectIPerfTargets selects at most perRegion nodes from each region.
func selectIPerfTargets(perRegion int) []IPerfEndpoint {
	if perRegion < 1 {
		return nil
	}
	counts := make(map[string]int)
	var selected []IPerfEndpoint
	for _, node := range iperfNodePool() {
		if counts[node.Region] >= perRegion {
			continue
		}
		counts[node.Region]++
		selected = append(selected, node)
	}
	return selected
}

// backtraceCities owns the canonical city IDs, selection order, and reference
// targets for return-path classification. The target set is configuration data,
// not probe logic.
type backtraceCity struct {
	ID      string
	Targets []Endpoint
}

var backtraceCities = []backtraceCity{
	{ID: "beijing", Targets: []Endpoint{
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierTelecom, "ipv4"), Address: "219.141.136.12", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierUnicom, "ipv4"), Address: "202.106.50.1", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierMobile, "ipv4"), Address: "221.179.155.161", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierTelecom, "ipv6"), Address: "bj-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierUnicom, "ipv6"), Address: "bj-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierMobile, "ipv6"), Address: "bj-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	}},
	{ID: "guangzhou", Targets: []Endpoint{
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierTelecom, "ipv4"), Address: "58.60.188.222", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierUnicom, "ipv4"), Address: "210.21.196.6", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierMobile, "ipv4"), Address: "120.196.165.24", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierTelecom, "ipv6"), Address: "gd-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierUnicom, "ipv6"), Address: "gd-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierMobile, "ipv6"), Address: "gd-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	}},
	{ID: "shanghai", Targets: []Endpoint{
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierTelecom, "ipv4"), Address: "202.96.209.133", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierUnicom, "ipv4"), Address: "210.22.97.1", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierMobile, "ipv4"), Address: "211.136.112.200", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierTelecom, "ipv6"), Address: "sh-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierUnicom, "ipv6"), Address: "sh-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierMobile, "ipv6"), Address: "sh-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	}},
	{ID: "chengdu", Targets: []Endpoint{
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierTelecom, "ipv4"), Address: "61.139.2.69", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierUnicom, "ipv4"), Address: "119.6.6.6", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierMobile, "ipv4"), Address: "211.137.96.205", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierTelecom, "ipv6"), Address: "sc-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierUnicom, "ipv6"), Address: "sc-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierMobile, "ipv6"), Address: "sc-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	}},
}

func backtraceTargetNameKey(city, carrier, family string) string {
	return "probe.backtrace.target." + city + "." + carrier + "." + family
}

// BacktraceCityOrder returns the canonical display and selection order.
// The returned slice is a copy, so callers cannot mutate the catalog.
func BacktraceCityOrder() []string {
	order := make([]string, 0, len(backtraceCities))
	for _, city := range backtraceCities {
		order = append(order, city.ID)
	}
	return order
}

// defaultBacktraceCityIDs derives the default selection in canonical order.
func defaultBacktraceCityIDs() []string {
	defaults := make([]string, 0, 2)
	for _, city := range backtraceCities {
		if city.ID == "beijing" || city.ID == "guangzhou" {
			defaults = append(defaults, city.ID)
		}
	}
	return defaults
}

// BacktraceTargetsFor aggregates targets for the requested cities.
func BacktraceTargetsFor(cities []string) []Endpoint {
	var targets []Endpoint
	for _, city := range backtraceCities {
		if !contains(cities, city.ID) {
			continue
		}
		targets = append(targets, city.Targets...)
	}
	return targets
}

// validBacktraceCarrier reports whether a backtrace target has one of the
// machine carrier identities understood by the probe.
func validBacktraceCarrier(carrier string) bool {
	switch carrier {
	case BacktraceCarrierTelecom, BacktraceCarrierUnicom, BacktraceCarrierMobile:
		return true
	default:
		return false
	}
}

// ValidateMediaRegions validates the media region selection.
func ValidateMediaRegions(regions []string) error {
	for _, region := range regions {
		if !contains(mediaRegionIDs, region) {
			return i18n.Errorf("err.unknownMediaRegion", region)
		}
	}
	return nil
}

// ParseBacktraceCities parses city names; all selects every city.
func ParseBacktraceCities(raw string) ([]string, error) {
	items := ParseList(raw)
	if len(items) == 0 {
		return defaultBacktraceCityIDs(), nil
	}
	if contains(items, "all") {
		if len(items) > 1 {
			return nil, i18n.Errorf("err.cityAllCombo")
		}
		return BacktraceCityOrder(), nil
	}
	for _, item := range items {
		known := false
		for _, city := range backtraceCities {
			if city.ID == item {
				known = true
				break
			}
		}
		if !known {
			return nil, i18n.Errorf("err.unknownCity", item)
		}
	}
	return items, nil
}
