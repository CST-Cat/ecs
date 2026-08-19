package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEndpointListSupportsAddressFamilies(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name        string
		raw         string
		requirePort bool
		want        Endpoint
	}{
		{name: "named IPv4", raw: "dns=1.1.1.1:53", requirePort: true, want: Endpoint{Name: "dns", Address: "1.1.1.1:53", Family: IPVersion4}},
		{name: "unnamed IPv6", raw: "[2001:db8::1]:53", requirePort: true, want: Endpoint{Name: "[2001:db8::1]:53", Address: "[2001:db8::1]:53", Family: IPVersion6}},
		{name: "bare IPv4", raw: "192.0.2.1", want: Endpoint{Name: "192.0.2.1", Address: "192.0.2.1", Family: IPVersion4}},
		{name: "bare IPv6", raw: "2001:db8::1", want: Endpoint{Name: "2001:db8::1", Address: "2001:db8::1", Family: IPVersion6}},
		{name: "v6 hostname", raw: "edge-v6.example.com", want: Endpoint{Name: "edge-v6.example.com", Address: "edge-v6.example.com", Family: IPVersion6}},
		{name: "hostname without port", raw: "example.com", want: Endpoint{Name: "example.com", Address: "example.com"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			endpoints, err := ParseEndpointList(test.raw, test.requirePort)
			if err != nil || !reflect.DeepEqual(endpoints, []Endpoint{test.want}) {
				t.Fatalf("ParseEndpointList(%q) = %+v, %v", test.raw, endpoints, err)
			}
		})
	}
}

func TestParseEndpointListRejectsDistinctInputs(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name, raw, marker string
		requirePort       bool
	}{
		{name: "missing address", raw: "dns=", requirePort: true, marker: "has no address"},
		{name: "missing port", raw: "example.com", requirePort: true, marker: "host:port"},
		{name: "invalid port", raw: "example.com:nope", requirePort: true, marker: "host:port"},
		{name: "unsafe host", raw: "x=bad host:53", requirePort: true, marker: "not a safe IP or hostname"},
		{name: "unsafe address", raw: "bad host", marker: "not a safe IP or hostname"},
		{name: "duplicate", raw: "a=1.1.1.1,b=1.1.1.1", marker: "duplicated"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseEndpointList(test.raw, test.requirePort)
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("ParseEndpointList(%q) = %v, want %q", test.raw, err, test.marker)
			}
		})
	}
}

func TestParseIPerfTargetListSupportsForms(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name, raw string
		want      IPerfEndpoint
	}{
		{name: "named range", raw: "edge=example.com:5201-5210", want: IPerfEndpoint{Name: "edge", Host: "example.com", PortStart: 5201, PortEnd: 5210, Location: "命令行指定", Region: "custom"}},
		{name: "IPv4 single", raw: "192.0.2.1:5201", want: IPerfEndpoint{Name: "192.0.2.1", Host: "192.0.2.1", PortStart: 5201, PortEnd: 5201, Location: "命令行指定", Networks: "IPv4", Region: "custom"}},
		{name: "IPv6 range", raw: "[2001:db8::1]:5201-5202", want: IPerfEndpoint{Name: "2001:db8::1", Host: "2001:db8::1", PortStart: 5201, PortEnd: 5202, Location: "命令行指定", Networks: "IPv6", Region: "custom"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			targets, err := ParseIPerfTargetList(test.raw)
			if err != nil || !reflect.DeepEqual(targets, []IPerfEndpoint{test.want}) {
				t.Fatalf("ParseIPerfTargetList(%q) = %+v, %v", test.raw, targets, err)
			}
		})
	}
}

func TestParseIPerfTargetListRejectsDistinctInputs(t *testing.T) {
	useEnglish(t)
	for _, test := range []struct {
		raw, marker string
	}{
		{raw: "edge", marker: "host:port"},
		{raw: "bad host:5201", marker: "safe IP or hostname"},
		{raw: "example.com:nope", marker: "invalid start port"},
		{raw: "edge=example.com:5202-5201", marker: "invalid port range"},
	} {
		_, err := ParseIPerfTargetList(test.raw)
		if err == nil || !strings.Contains(err.Error(), test.marker) {
			t.Errorf("ParseIPerfTargetList(%q) = %v, want %q", test.raw, err, test.marker)
		}
	}
}
