package probe

import (
	"net"
	"testing"

	"ecs/internal/config"
)

type fixtureNetworkAddr string

func (fixtureNetworkAddr) Network() string        { return "fixture" }
func (address fixtureNetworkAddr) String() string { return string(address) }

func TestNetworkCapabilityClassificationAndFamilies(t *testing.T) {
	for _, test := range []struct {
		name   string
		addr   fixtureNetworkAddr
		v4, v6 bool
	}{
		{name: "private IPv4", addr: "192.168.10.7/24", v4: true},
		{name: "global IPv4 host port", addr: "198.51.100.7:53", v4: true},
		{name: "global IPv6 zone", addr: "[2001:db8::7%eth0]:53", v6: true},
		{name: "ULA", addr: "fc00::1"},
		{name: "link local", addr: "fe80::1%eth0"},
		{name: "loopback", addr: "127.0.0.1"},
		{name: "invalid", addr: "not-an-address"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := classifyNetworkAddresses([]net.Addr{test.addr})
			if got.IPv4Usable != test.v4 || got.IPv6Usable != test.v6 {
				t.Fatalf("network capabilities = %+v", got)
			}
		})
	}

	dual := classifyNetworkAddresses([]net.Addr{
		fixtureNetworkAddr("192.168.10.7/24"),
		fixtureNetworkAddr("[2001:db8::7%eth0]:53"),
	})
	if !dual.IsDualStack() || !dual.Has(config.IPVersion4) || !dual.Has(config.IPVersion6) || dual.Has("unknown") {
		t.Fatalf("network capabilities = %+v", dual)
	}
}
