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
		{Name: "北京电信", Address: "219.141.136.12", Kind: "电信"},
		{Name: "北京联通", Address: "202.106.50.1", Kind: "联通"},
		{Name: "北京移动", Address: "221.179.155.161", Kind: "移动"},
		{Name: "北京电信 IPv6", Address: "bj-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "北京联通 IPv6", Address: "bj-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "北京移动 IPv6", Address: "bj-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
	"guangzhou": {
		{Name: "广州电信", Address: "58.60.188.222", Kind: "电信"},
		{Name: "广州联通", Address: "210.21.196.6", Kind: "联通"},
		{Name: "广州移动", Address: "120.196.165.24", Kind: "移动"},
		{Name: "广州电信 IPv6", Address: "gd-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "广州联通 IPv6", Address: "gd-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "广州移动 IPv6", Address: "gd-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
	"shanghai": {
		{Name: "上海电信", Address: "202.96.209.133", Kind: "电信"},
		{Name: "上海联通", Address: "210.22.97.1", Kind: "联通"},
		{Name: "上海移动", Address: "211.136.112.200", Kind: "移动"},
		{Name: "上海电信 IPv6", Address: "sh-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "上海联通 IPv6", Address: "sh-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "上海移动 IPv6", Address: "sh-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
	"chengdu": {
		{Name: "成都电信", Address: "61.139.2.69", Kind: "电信"},
		{Name: "成都联通", Address: "119.6.6.6", Kind: "联通"},
		{Name: "成都移动", Address: "211.137.96.205", Kind: "移动"},
		{Name: "成都电信 IPv6", Address: "sc-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "成都联通 IPv6", Address: "sc-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "成都移动 IPv6", Address: "sc-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
}

// BacktraceCityOrder fixes display and selection order.
var BacktraceCityOrder = []string{"beijing", "guangzhou", "shanghai", "chengdu"}

var defaultBacktraceCities = []string{"beijing", "guangzhou"}

// BacktraceTargetsFor aggregates targets for the requested cities.
func BacktraceTargetsFor(cities []string) []Endpoint {
	var targets []Endpoint
	for _, city := range BacktraceCityOrder {
		if !contains(cities, city) {
			continue
		}
		targets = append(targets, backtraceCityTargets[city]...)
	}
	return targets
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
		return defaultBacktraceCities, nil
	}
	if contains(items, "all") {
		if len(items) > 1 {
			return nil, i18n.Errorf("err.cityAllCombo")
		}
		return append([]string(nil), BacktraceCityOrder...), nil
	}
	for _, item := range items {
		if _, ok := backtraceCityTargets[item]; !ok {
			return nil, i18n.Errorf("err.unknownCity", item)
		}
	}
	return items, nil
}
