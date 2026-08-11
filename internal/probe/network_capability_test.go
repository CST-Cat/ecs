package probe

import (
	"net"
	"reflect"
	"testing"
)

func testNetworkAddress(t *testing.T, cidr string) net.Addr {
	t.Helper()
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	network.IP = ip
	return network
}

func TestNetworkAddressClassification(t *testing.T) {
	cases := []struct {
		name string
		cidr string
		want NetworkCapabilities
	}{
		{name: "public IPv4", cidr: "198.51.100.7/24", want: NetworkCapabilities{IPv4Usable: true}},
		{name: "RFC1918 IPv4", cidr: "192.168.10.7/24", want: NetworkCapabilities{IPv4Usable: true}},
		{name: "IPv4 loopback", cidr: "127.0.0.1/8", want: NetworkCapabilities{}},
		{name: "IPv4 link-local", cidr: "169.254.10.7/16", want: NetworkCapabilities{}},
		{name: "public IPv6", cidr: "2001:db8::7/64", want: NetworkCapabilities{IPv6Usable: true}},
		{name: "IPv6 ULA", cidr: "fd12:3456::7/64", want: NetworkCapabilities{}},
		{name: "IPv6 link-local", cidr: "fe80::7/64", want: NetworkCapabilities{}},
		{name: "IPv6 loopback", cidr: "::1/128", want: NetworkCapabilities{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := classifyNetworkAddresses([]net.Addr{testNetworkAddress(t, testCase.cidr)})
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("classifyNetworkAddresses(%s) = %+v, want %+v", testCase.cidr, got, testCase.want)
			}
		})
	}
}

func TestNetworkAddressClassificationCombinesFamilies(t *testing.T) {
	addresses := []net.Addr{
		testNetworkAddress(t, "192.168.10.7/24"),
		testNetworkAddress(t, "fd12:3456::7/64"),
		testNetworkAddress(t, "2001:db8::7/64"),
	}
	want := NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}
	if got := classifyNetworkAddresses(addresses); !reflect.DeepEqual(got, want) {
		t.Fatalf("combined capabilities = %+v, want %+v", got, want)
	}
}

func TestNetworkAddressHelpersArePureAndFamilySpecific(t *testing.T) {
	addresses := []net.Addr{
		testNetworkAddress(t, "192.168.10.7/24"),
		testNetworkAddress(t, "2001:db8::7/64"),
	}
	if !hasUsableIPv4(addresses) {
		t.Fatal("RFC1918 IPv4 should be accepted as a possible NAT source")
	}
	if !hasGlobalUnicastIPv6(addresses) {
		t.Fatal("global IPv6 should be accepted")
	}
	if hasUsableIPv4([]net.Addr{testNetworkAddress(t, "2001:db8::7/64")}) {
		t.Fatal("IPv6 address must not satisfy IPv4 helper")
	}
	if hasGlobalUnicastIPv6([]net.Addr{testNetworkAddress(t, "192.168.10.7/24")}) {
		t.Fatal("IPv4 address must not satisfy IPv6 helper")
	}
}
