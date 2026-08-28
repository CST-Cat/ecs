package config

import "ecs/internal/i18n"

// stunServerPool is the public STUN pool used for NAT behavior discovery.
func stunServerPool() []Endpoint {
	return []Endpoint{
		{Name: "Xiaomi", Address: "stun.miwifi.com:3478", Kind: "双 IP"},
		{Name: "1&1", Address: "stun.1und1.de:3478", Kind: "双 IP"},
		{Name: "Hoiio", Address: "stun.hoiio.com:3478", Kind: "双 IP"},
		{Name: "Google", Address: "stun.l.google.com:19302", Kind: "仅映射"},
		{Name: "Cloudflare", Address: "stun.cloudflare.com:3478", Kind: "仅映射"},
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

// backtraceCityTargets contains the reference targets for return-path
// classification. The target set is configuration data, not probe logic.
var backtraceCityTargets = map[string][]Endpoint{
	"beijing": {
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierTelecom, "ipv4"), Address: "219.141.136.12", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierUnicom, "ipv4"), Address: "202.106.50.1", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierMobile, "ipv4"), Address: "221.179.155.161", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierTelecom, "ipv6"), Address: "bj-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierUnicom, "ipv6"), Address: "bj-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("beijing", BacktraceCarrierMobile, "ipv6"), Address: "bj-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	},
	"guangzhou": {
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierTelecom, "ipv4"), Address: "58.60.188.222", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierUnicom, "ipv4"), Address: "210.21.196.6", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierMobile, "ipv4"), Address: "120.196.165.24", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierTelecom, "ipv6"), Address: "gd-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierUnicom, "ipv6"), Address: "gd-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("guangzhou", BacktraceCarrierMobile, "ipv6"), Address: "gd-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	},
	"shanghai": {
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierTelecom, "ipv4"), Address: "202.96.209.133", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierUnicom, "ipv4"), Address: "210.22.97.1", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierMobile, "ipv4"), Address: "211.136.112.200", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierTelecom, "ipv6"), Address: "sh-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierUnicom, "ipv6"), Address: "sh-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("shanghai", BacktraceCarrierMobile, "ipv6"), Address: "sh-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	},
	"chengdu": {
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierTelecom, "ipv4"), Address: "61.139.2.69", Kind: BacktraceCarrierTelecom},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierUnicom, "ipv4"), Address: "119.6.6.6", Kind: BacktraceCarrierUnicom},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierMobile, "ipv4"), Address: "211.137.96.205", Kind: BacktraceCarrierMobile},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierTelecom, "ipv6"), Address: "sc-ct-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierTelecom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierUnicom, "ipv6"), Address: "sc-cu-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: backtraceTargetNameKey("chengdu", BacktraceCarrierMobile, "ipv6"), Address: "sc-cm-v6.ip.zstaticcdn.com", Kind: BacktraceCarrierMobile, Family: IPVersion6},
	},
}

func backtraceTargetNameKey(city, carrier, family string) string {
	return "probe.backtrace.target." + city + "." + carrier + "." + family
}

// backtraceCityOrder fixes display and selection order.
var backtraceCityOrder = [...]string{"beijing", "guangzhou", "shanghai", "chengdu"}

// BacktraceCityOrder returns the canonical display and selection order.
// The returned slice is a copy, so callers cannot mutate the catalog.
func BacktraceCityOrder() []string {
	return append([]string(nil), backtraceCityOrder[:]...)
}

var defaultBacktraceCities = []string{"beijing", "guangzhou"}

// BacktraceTargetsFor aggregates targets for the requested cities.
func BacktraceTargetsFor(cities []string) []Endpoint {
	var targets []Endpoint
	for _, city := range backtraceCityOrder {
		if !contains(cities, city) {
			continue
		}
		targets = append(targets, backtraceCityTargets[city]...)
	}
	return targets
}

// ValidBacktraceCarrier reports whether a backtrace target has one of the
// machine carrier identities understood by the probe.
func ValidBacktraceCarrier(carrier string) bool {
	switch carrier {
	case BacktraceCarrierTelecom, BacktraceCarrierUnicom, BacktraceCarrierMobile:
		return true
	default:
		return false
	}
}

// ValidateMediaRegions validates the media region selection.
func ValidateMediaRegions(regions []string) error {
	known := map[string]bool{"global": true, "jp": true, "tw": true, "hk": true, "cn": true}
	for _, region := range regions {
		if !known[region] {
			return i18n.Errorf("err.unknownMediaRegion", region)
		}
	}
	return nil
}

// ParseBacktraceCities parses city names; all selects every city.
func ParseBacktraceCities(raw string) ([]string, error) {
	items := ParseList(raw)
	if len(items) == 0 {
		return append([]string(nil), defaultBacktraceCities...), nil
	}
	if contains(items, "all") {
		if len(items) > 1 {
			return nil, i18n.Errorf("err.cityAllCombo")
		}
		return BacktraceCityOrder(), nil
	}
	for _, item := range items {
		if _, ok := backtraceCityTargets[item]; !ok {
			return nil, i18n.Errorf("err.unknownCity", item)
		}
	}
	return items, nil
}
