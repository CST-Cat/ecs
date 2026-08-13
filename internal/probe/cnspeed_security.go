package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ecs/internal/config"
)

type cnIPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type cnDialContextFunc func(context.Context, string, string) (net.Conn, error)

var cnBlockedPrefixes = []netip.Prefix{
	// IPv4 special-use, local, documentation and benchmarking networks.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// IPv6 local/special-use, transition, documentation and benchmark ranges.
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// validateCNNodeURL rejects URL forms that can escape the intended HTTP(S)
// target model. Literal special-use addresses are rejected immediately;
// hostname results are checked again at the actual dial boundary.
func validateCNNodeURL(raw string) (*url.URL, error) {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return nil, fmt.Errorf("节点 URL 为空或含首尾空白")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("节点 URL 无效: %w", err)
	}
	if parsed.Opaque != "" || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("节点 URL 必须是绝对 HTTP(S) URL")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, fmt.Errorf("节点 URL 仅允许 HTTP(S)")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("节点 URL 不允许用户信息或片段")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("节点 URL 端口无效")
		}
	}
	if address, err := netip.ParseAddr(parsed.Hostname()); err == nil && !cnPublicAddress(address) {
		return nil, fmt.Errorf("节点 URL 指向非公网地址")
	}
	return parsed, nil
}

func cnPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range cnBlockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func newCNSpeedHTTPClient(timeout time.Duration, ipVersion string, resolver cnIPResolver, dial cnDialContextFunc) *http.Client {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dial == nil {
		dialer := &net.Dialer{Timeout: timeout}
		dial = dialer.DialContext
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialTLSContext = nil
	transport.DialContext = cnSafeDialContext(ipVersion, resolver, dial)
	transport.MaxIdleConns = 16
	transport.MaxIdleConnsPerHost = 8
	transport.IdleConnTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = timeout
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("重定向次数超过上限")
			}
			if _, err := validateCNNodeURL(request.URL.String()); err != nil {
				return fmt.Errorf("拒绝不安全的重定向: %w", err)
			}
			return nil
		},
	}
}

func cnSafeDialContext(ipVersion string, resolver cnIPResolver, dial cnDialContextFunc) cnDialContextFunc {
	return func(ctx context.Context, _ string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("节点地址无效: %w", err)
		}
		resolved, err := cnResolvePublicAddresses(ctx, resolver, host, ipVersion)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, candidate := range resolved {
			network := "tcp6"
			if candidate.Is4() {
				network = "tcp4"
			}
			connection, dialErr := dial(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, fmt.Errorf("公网节点连接失败: %w", lastErr)
	}
}

func cnResolvePublicAddresses(ctx context.Context, resolver cnIPResolver, host, ipVersion string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(host, ".")
	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if !cnAddressMatchesVersion(literal, ipVersion) {
			return nil, fmt.Errorf("节点地址与所选 IP 版本不匹配")
		}
		if !cnPublicAddress(literal) {
			return nil, fmt.Errorf("拒绝连接非公网节点地址")
		}
		return []netip.Addr{literal}, nil
	}

	lookupNetwork := "ip"
	if ipVersion == config.IPVersion4 {
		lookupNetwork = "ip4"
	} else if ipVersion == config.IPVersion6 {
		lookupNetwork = "ip6"
	}
	addresses, err := resolver.LookupNetIP(ctx, lookupNetwork, host)
	if err != nil {
		return nil, fmt.Errorf("节点 DNS 解析失败: %w", err)
	}
	public := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if cnAddressMatchesVersion(address, ipVersion) && cnPublicAddress(address) {
			public = append(public, address)
		}
	}
	if len(public) == 0 {
		return nil, fmt.Errorf("节点 DNS 未返回允许的公网地址")
	}
	return public, nil
}

func cnAddressMatchesVersion(address netip.Addr, ipVersion string) bool {
	switch ipVersion {
	case config.IPVersion4:
		return address.Is4()
	case config.IPVersion6:
		return address.Is6()
	default:
		return true
	}
}
