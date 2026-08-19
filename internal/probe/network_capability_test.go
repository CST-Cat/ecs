package probe

import (
	"net"
	"testing"
)

func TestNetworkAddressClassificationRecognizesUsableIPv4(t *testing.T) {
	ip, network, err := net.ParseCIDR("192.168.10.7/24")
	if err != nil {
		t.Fatal(err)
	}
	network.IP = ip

	capabilities := classifyNetworkAddresses([]net.Addr{network})
	if !capabilities.IPv4Usable || capabilities.IPv6Usable {
		t.Fatalf("network capabilities = %+v", capabilities)
	}
}
