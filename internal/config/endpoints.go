package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
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
			return nil, fmt.Errorf("端点 %q 缺少地址", item)
		}
		if requirePort {
			host, port, err := splitHostPort(address)
			if err != nil || host == "" || port == "" {
				return nil, fmt.Errorf("端点 %q 必须是 host:port 形式", address)
			}
			if !validRouteTarget(host) {
				return nil, fmt.Errorf("端点主机 %q 不是安全的 IP 或主机名", host)
			}
		} else if !validRouteTarget(address) {
			return nil, fmt.Errorf("端点 %q 不是安全的 IP 或主机名", address)
		}
		if seen[address] {
			return nil, fmt.Errorf("端点 %q 重复", address)
		}
		seen[address] = true
		if name == "" {
			name = address
		}
		endpoints = append(endpoints, Endpoint{
			Name: name, Address: address,
			Family: inferEndpointFamily(address, requirePort),
		})
	}
	return endpoints, nil
}

// inferEndpointFamily records facts that can be established without DNS.  A
// literal is unambiguous; the backtrace target list also uses a -v6 hostname
// convention so an IPv6-only hostname is not accidentally routed over IPv4.
func inferEndpointFamily(address string, requirePort bool) string {
	host := address
	if !requirePort {
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
			if ip.To4() != nil {
				return IPVersion4
			}
			return IPVersion6
		}
	} else if parsedHost, _, err := splitHostPort(address); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return IPVersion4
		}
		return IPVersion6
	}
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
	for _, item := range items {
		name, spec := "", item
		if key, value, ok := strings.Cut(item, "="); ok {
			name, spec = strings.TrimSpace(key), strings.TrimSpace(value)
		}
		host, ports, ok := splitLastColon(spec)
		if !ok {
			return nil, fmt.Errorf("iperf3 节点 %q 必须是 host:port 或 host:start-end 形式", item)
		}
		if !validRouteTarget(host) {
			return nil, fmt.Errorf("iperf3 节点主机 %q 不是安全的 IP 或主机名", host)
		}
		startText, endText, hasRange := strings.Cut(ports, "-")
		start, err := strconv.Atoi(strings.TrimSpace(startText))
		if err != nil || start < 1 || start > 65535 {
			return nil, fmt.Errorf("iperf3 节点 %q 起始端口无效", item)
		}
		end := start
		if hasRange {
			end, err = strconv.Atoi(strings.TrimSpace(endText))
			if err != nil || end < start || end > 65535 {
				return nil, fmt.Errorf("iperf3 节点 %q 端口范围无效", item)
			}
		}
		if name == "" {
			name = host
		}
		targets = append(targets, IPerfEndpoint{
			Name: name, Host: host, PortStart: start, PortEnd: end,
			Location: "命令行指定", Networks: "IPv4", Region: "custom",
		})
	}
	return targets, nil
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

// splitHostPort 在不依赖 net 包语义的前提下切出主机与端口。
//
// 需要兼容 IPv6 字面量的方括号写法，因此按最后一个冒号切分。
func splitHostPort(address string) (string, string, error) {
	host, port, ok := splitLastColon(address)
	if !ok {
		return "", "", fmt.Errorf("缺少端口")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return "", "", fmt.Errorf("端口无效")
	}
	return host, port, nil
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
