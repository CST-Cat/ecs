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

func TestValidateRejectsDuplicateOperationalEndpoints(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name   string
		mutate func(*Runtime)
	}{
		{
			name: "DNS ignores display name and normalizes address",
			mutate: func(runtime *Runtime) {
				runtime.DNSResolvers = []Endpoint{
					{Name: "primary", Address: "Example.COM.:53"},
					{Name: "backup label", Address: "example.com:053"},
				}
			},
		},
		{
			name: "latency ignores display name and normalizes address",
			mutate: func(runtime *Runtime) {
				runtime.LatencyTargets = []Endpoint{
					{Name: "primary", Address: "Example.COM.:443"},
					{Name: "backup label", Address: "example.com:0443"},
				}
			},
		},
		{
			name: "route ignores display name and normalizes address",
			mutate: func(runtime *Runtime) {
				runtime.RouteTargets = []Endpoint{
					{Name: "primary", Address: "Example.COM."},
					{Name: "backup label", Address: "example.com"},
				}
			},
		},
		{
			name: "STUN normalizes host and port",
			mutate: func(runtime *Runtime) {
				runtime.STUNServers = []Endpoint{
					{Name: "primary", Address: "STUN.Example.COM.:3478"},
					{Name: "backup label", Address: "stun.example.com:03478"},
				}
			},
		},
		{
			name: "backtrace ignores display name and normalizes address",
			mutate: func(runtime *Runtime) {
				runtime.BacktraceTargets = []Endpoint{
					{Name: "primary", Address: "Example.COM.", Kind: BacktraceCarrierTelecom},
					{Name: "backup label", Address: "example.com", Kind: BacktraceCarrierUnicom},
				}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntime(t)
			test.mutate(&runtime)
			if err := Validate(runtime); err == nil || !strings.Contains(err.Error(), "duplicated") {
				t.Fatalf("Validate duplicate runtime = %v, want explicit duplicate error", err)
			}
		})
	}
}

func TestValidateAcceptsDistinctOperationalEndpoints(t *testing.T) {
	useEnglish(t)
	runtime := validRuntime(t)
	runtime.DNSResolvers = []Endpoint{
		{Name: "dns-a", Address: "example.com:53"},
		{Name: "dns-b", Address: "example.com:5353"},
	}
	runtime.LatencyTargets = []Endpoint{
		{Name: "latency-a", Address: "example.com:443", Family: IPVersion4},
		{Name: "latency-b", Address: "example.com:443", Family: IPVersion6},
	}
	runtime.RouteTargets = []Endpoint{
		{Name: "route-a", Address: "example.com", Family: IPVersion4},
		{Name: "route-b", Address: "example.com", Family: IPVersion6},
	}
	runtime.STUNServers = []Endpoint{
		{Name: "stun-a", Address: "stun.example.com:3478"},
		{Name: "stun-b", Address: "stun.example.com:5349"},
	}
	runtime.BacktraceTargets = []Endpoint{
		{Name: "trace-a", Address: "example.com", Kind: BacktraceCarrierTelecom, Family: IPVersion4},
		{Name: "trace-b", Address: "example.com", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
	}
	if err := Validate(runtime); err != nil {
		t.Fatalf("Validate distinct operational endpoints = %v, want nil", err)
	}
}

func TestParseIPerfTargetListSupportsForms(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name, raw string
		want      IPerfEndpoint
	}{
		{name: "named range", raw: "edge=example.com:5201-5210", want: IPerfEndpoint{Name: "edge", Host: "example.com", PortStart: 5201, PortEnd: 5210, Region: "custom"}},
		{name: "IPv4 single", raw: "192.0.2.1:5201", want: IPerfEndpoint{Name: "192.0.2.1", Host: "192.0.2.1", PortStart: 5201, PortEnd: 5201, Networks: "IPv4", Region: "custom"}},
		{name: "IPv6 range", raw: "[2001:db8::1]:5201-5202", want: IPerfEndpoint{Name: "2001:db8::1", Host: "2001:db8::1", PortStart: 5201, PortEnd: 5202, Networks: "IPv6", Region: "custom"}},
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

func TestParseIPerfTargetListRejectsDuplicateOperationalTargets(t *testing.T) {
	useEnglish(t)
	for _, raw := range []string{
		"edge-a=Example.COM:5201,edge-b=example.com.:5201",
		"edge-a=[2001:DB8::1]:5201,edge-b=[2001:db8::1]:5201",
	} {
		_, err := ParseIPerfTargetList(raw)
		if err == nil || !strings.Contains(err.Error(), "duplicated") {
			t.Errorf("ParseIPerfTargetList(%q) = %v, want duplicate error", raw, err)
		}
	}
}

func TestParseIPerfTargetListAllowsDistinctPortRanges(t *testing.T) {
	useEnglish(t)
	targets, err := ParseIPerfTargetList("edge-a=example.com:5201,edge-b=example.com:5202")
	if err != nil {
		t.Fatalf("ParseIPerfTargetList returned error: %v", err)
	}
	if len(targets) != 2 || targets[0].Name != "edge-a" || targets[0].PortStart != 5201 || targets[1].Name != "edge-b" || targets[1].PortStart != 5202 {
		t.Fatalf("ParseIPerfTargetList targets = %+v, want ordered distinct ranges", targets)
	}
}
