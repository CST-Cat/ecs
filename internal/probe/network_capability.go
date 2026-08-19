package probe

import (
	"net"
	"strings"
	"sync"

	"ecs/internal/config"
)

const (
	// These are literal addresses on purpose. A UDP connect selects a local
	// route and source address in the kernel; it does not send a packet or
	// wait for a peer response.
	ipv4CapabilityAddress = "1.1.1.1:53"
	ipv6CapabilityAddress = "[2606:4700:4700::1111]:53"
)

// NetworkCapabilities is the local, per-run snapshot of the IP families that
// can be selected before any network probe starts. It deliberately describes
// local route selection, not reachability of a remote service.
//
// A private IPv4 address is useful here: a host behind NAT still needs its
// IPv4 network modules to run. IPv6 is intentionally stricter because ULA,
// link-local and loopback addresses do not provide a public IPv6 egress path.
type NetworkCapabilities struct {
	IPv4Usable bool
	IPv6Usable bool
}

// Has reports whether a concrete config IP family is available in the
// snapshot. Unknown and auto modes are not concrete families.
func (c NetworkCapabilities) Has(version string) bool {
	switch version {
	case config.IPVersion4:
		return c.IPv4Usable
	case config.IPVersion6:
		return c.IPv6Usable
	default:
		return false
	}
}

// IsDualStack reports whether both concrete families are available.
func (c NetworkCapabilities) IsDualStack() bool {
	return c.IPv4Usable && c.IPv6Usable
}

// DetectNetworkCapabilities enumerates usable addresses and then asks the
// kernel to select a route with one literal UDP endpoint per family. Both
// family checks run concurrently. UDP connect performs no DNS, HTTP, ICMP or
// remote-response wait, so a missing route fails locally without a serial
// timeout for each protocol family.
func DetectNetworkCapabilities() NetworkCapabilities {
	addresses, err := localInterfaceAddresses()
	if err != nil {
		return NetworkCapabilities{}
	}
	candidates := classifyNetworkAddresses(addresses)
	var capabilities NetworkCapabilities
	var waitGroup sync.WaitGroup
	if candidates.IPv4Usable {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			capabilities.IPv4Usable = udpRouteAvailable("udp4", ipv4CapabilityAddress)
		}()
	}
	if candidates.IPv6Usable {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			capabilities.IPv6Usable = udpRouteAvailable("udp6", ipv6CapabilityAddress)
		}()
	}
	waitGroup.Wait()
	return capabilities
}

func udpRouteAvailable(network, address string) bool {
	connection, err := net.Dial(network, address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func localInterfaceAddresses() ([]net.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	addresses := make([]net.Addr, 0)
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 {
			continue
		}
		interfaceAddresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		addresses = append(addresses, interfaceAddresses...)
	}
	return addresses, nil
}

// classifyNetworkAddresses is pure: it only classifies the addresses passed
// by the caller and never consults the host or performs any I/O.
func classifyNetworkAddresses(addresses []net.Addr) NetworkCapabilities {
	capabilities := NetworkCapabilities{}
	for _, address := range addresses {
		ip := ipFromNetworkAddress(address)
		if ip == nil {
			continue
		}
		capabilities.IPv4Usable = capabilities.IPv4Usable || isUsableIPv4(ip)
		capabilities.IPv6Usable = capabilities.IPv6Usable || isUsableIPv6(ip)
		if capabilities.IsDualStack() {
			break
		}
	}
	return capabilities
}

func isUsableIPv4(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func isUsableIPv6(ip net.IP) bool {
	if ip == nil || ip.To4() != nil {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsPrivate()
}

func ipFromNetworkAddress(address net.Addr) net.IP {
	if address == nil {
		return nil
	}
	raw := strings.TrimSpace(address.String())
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	raw = strings.Trim(raw, "[]")
	if slash := strings.IndexByte(raw, '/'); slash >= 0 {
		raw = raw[:slash]
	}
	if percent := strings.LastIndexByte(raw, '%'); percent >= 0 {
		raw = raw[:percent]
	}
	return net.ParseIP(raw)
}
