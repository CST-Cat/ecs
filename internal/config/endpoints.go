package config

import (
	"net"
	"strconv"
	"strings"

	"ecs/internal/i18n"
)

// requirePort / noPort name the boolean argument shared by the address helpers
// below. A bare true/false at a call site says nothing about what it selects.
const (
	requirePort = true
	noPort      = false
)

// 命令行端点解析。
//
// 让每一项配置都能只靠命令行调节：容器与一次性排查场景下写配置文件很别扭，
// 而这些目标列表恰恰是最需要临时替换的东西（换个 DNS、换个回程目标、
// 指定自建 iperf3 节点）。

// ParseEndpointList 解析 "名称=地址" 或纯地址的逗号分隔列表。
//
// 省略名称时用地址本身作为名称，这样 `--dns-resolvers 1.1.1.1:53` 就能用，
// 不必写成 `Cloudflare=1.1.1.1:53`。
func ParseEndpointList(raw string, requirePort bool) ([]Endpoint, error) {
	items := splitTrimmed(raw)
	if len(items) == 0 {
		return nil, nil
	}
	endpoints := make([]Endpoint, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		name, address := item, item
		if key, value, ok := strings.Cut(item, "="); ok {
			name, address = strings.TrimSpace(key), strings.TrimSpace(value)
		}
		if address == "" {
			return nil, i18n.Errorf("err.endpointMissingAddress", item)
		}
		if requirePort {
			host, port, err := splitHostPort(address)
			if err != nil || host == "" || port == "" {
				return nil, i18n.Errorf("err.endpointNeedsHostPort", address)
			}
			if !validRouteTarget(host) {
				return nil, i18n.Errorf("err.endpointUnsafeHost", host)
			}
		} else if !validRouteTarget(address) {
			return nil, i18n.Errorf("err.endpointUnsafe", address)
		}
		if seen[address] {
			return nil, i18n.Errorf("err.endpointDuplicate", address)
		}
		seen[address] = true
		if name == "" {
			name = address
		}
		endpoints = append(endpoints, Endpoint{
			Name: name, Address: address,
			Family: InferEndpointFamily(address, requirePort),
		})
	}
	return endpoints, nil
}

// ParseBacktraceTargetList parses the backtrace-specific machine syntax
// `carrier:Name=host`. Carrier is deliberately explicit: the producer cannot
// infer it from a localized or user-defined target name.
func ParseBacktraceTargetList(raw string) ([]Endpoint, error) {
	items := splitTrimmed(raw)
	if len(items) == 0 {
		return nil, nil
	}
	targets := make([]Endpoint, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		carrier, specification, ok := strings.Cut(item, ":")
		carrier = strings.TrimSpace(carrier)
		specification = strings.TrimSpace(specification)
		if !ok || carrier == "" || specification == "" {
			return nil, i18n.Errorf("err.backtraceFormat", item)
		}
		if !validBacktraceCarrier(carrier) {
			return nil, i18n.Errorf("err.backtraceCarrier", item, carrier)
		}
		name, address, ok := strings.Cut(specification, "=")
		name, address = strings.TrimSpace(name), strings.TrimSpace(address)
		if !ok || name == "" || address == "" {
			return nil, i18n.Errorf("err.backtraceFormat", item)
		}
		if !validRouteTarget(address) {
			return nil, i18n.Errorf("err.backtraceUnsafe", address)
		}
		if seen[address] {
			return nil, i18n.Errorf("err.endpointDuplicate", address)
		}
		seen[address] = true
		targets = append(targets, Endpoint{
			Name: name, Address: address, Kind: carrier,
			Family: InferEndpointFamily(address, noPort),
		})
	}
	return targets, nil
}

// literalEndpointFamily records only the protocol family intrinsic to a
// literal address. Hostname conventions are deliberately excluded: callers
// may use Family to pin a hostname even when its name contains a v6 hint.
func literalEndpointFamily(address string, requirePort bool) string {
	host := address
	if requirePort {
		if parsedHost, _, err := splitHostPort(address); err == nil {
			host = parsedHost
		}
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return IPVersion4
		}
		return IPVersion6
	}
	return ""
}

// InferEndpointFamily records facts that can be established without DNS.  A
// literal is unambiguous; the backtrace target list also uses a -v6 hostname
// convention so an IPv6-only hostname is not accidentally routed over IPv4.
//
// It is exported because probe needs the same answer when it picks a concrete
// family per target. Keeping one implementation means the -v6 convention is
// defined in exactly one place.
func InferEndpointFamily(address string, requirePort bool) string {
	if family := literalEndpointFamily(address, requirePort); family != "" {
		return family
	}
	host := address
	if requirePort {
		if parsedHost, _, err := splitHostPort(address); err == nil {
			host = parsedHost
		}
	}
	host = strings.Trim(host, "[]")
	if strings.Contains(strings.ToLower(host), "-v6.") {
		return IPVersion6
	}
	return ""
}

// ParseIPerfTargetList 解析 iperf3 节点列表。
//
// 格式为 `[名称=]主机:起始端口[-结束端口]`，例如：
//
//	my.iperf.example:5201
//	Home=my.iperf.example:5201-5210
//
// 省略结束端口时按单端口处理。
func ParseIPerfTargetList(raw string) ([]IPerfEndpoint, error) {
	items := splitTrimmed(raw)
	if len(items) == 0 {
		return nil, nil
	}
	targets := make([]IPerfEndpoint, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		name, spec := "", item
		if key, value, ok := strings.Cut(item, "="); ok {
			name, spec = strings.TrimSpace(key), strings.TrimSpace(value)
		}
		host, ports, ok := splitLastColon(spec)
		if !ok {
			return nil, i18n.Errorf("err.iperfNodeFormat", item)
		}
		if !validRouteTarget(host) {
			return nil, i18n.Errorf("err.iperfNodeHost", host)
		}
		startText, endText, hasRange := strings.Cut(ports, "-")
		start, err := strconv.Atoi(strings.TrimSpace(startText))
		if err != nil || start < 1 || start > 65535 {
			return nil, i18n.Errorf("err.iperfNodeStart", item)
		}
		end := start
		if hasRange {
			end, err = strconv.Atoi(strings.TrimSpace(endText))
			if err != nil || end < start || end > 65535 {
				return nil, i18n.Errorf("err.iperfNodeRange", item)
			}
		}
		key := iperfTargetKey(host, start, end)
		if seen[key] {
			return nil, i18n.Errorf("err.endpointDuplicate", host)
		}
		seen[key] = true
		if name == "" {
			name = host
		}
		networks := ""
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
			if ip.To4() != nil {
				networks = "IPv4"
			} else {
				networks = "IPv6"
			}
		}
		targets = append(targets, IPerfEndpoint{
			Name: name, Host: host, PortStart: start, PortEnd: end,
			Networks: networks, Region: "custom",
		})
	}
	return targets, nil
}

func iperfTargetKey(host string, portStart, portEnd int) string {
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(strings.TrimSpace(host), "[]"), "."))
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	return host + "\x00" + strconv.Itoa(portStart) + "\x00" + strconv.Itoa(portEnd)
}

// splitTrimmed 按逗号切分并去掉空项。
func splitTrimmed(raw string) []string {
	var items []string
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

// splitHostPort validates the executable host:port form used by network
// probes. In particular, IPv6 literals must use the bracketed form accepted
// by Go's net package (for example, [2001:db8::1]:53).
func splitHostPort(address string) (string, string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", "", i18n.Errorf("err.portMissing")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", "", i18n.Errorf("err.portInvalid")
	}
	return host, strconv.Itoa(number), nil
}

// splitLastColon 按最后一个冒号切分，并剥掉 IPv6 字面量的方括号。
func splitLastColon(address string) (string, string, bool) {
	index := strings.LastIndex(address, ":")
	if index <= 0 || index == len(address)-1 {
		return "", "", false
	}
	host := address[:index]
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return host, address[index+1:], true
}
