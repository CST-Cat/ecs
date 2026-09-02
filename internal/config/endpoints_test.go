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

func TestParseEndpointListRequiresExecutableHostPort(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "missing port", raw: "example.com", want: false},
		{name: "non-numeric port", raw: "example.com:notaport", want: false},
		{name: "zero port", raw: "example.com:0", want: false},
		{name: "port too large", raw: "example.com:65536", want: false},
		{name: "hostname", raw: "example.com:53", want: true},
		{name: "bracketed IPv6", raw: "[2001:db8::1]:53", want: true},
		// net.Dialer and net.SplitHostPort require brackets around an IPv6
		// literal when a port is present; the parser must enforce that same
		// executable representation.
		{name: "unbracketed IPv6", raw: "2001:db8::1:53", want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			endpoints, err := ParseEndpointList(test.raw, true)
			if test.want {
				if err != nil || len(endpoints) != 1 {
					t.Fatalf("ParseEndpointList(%q) = %+v, %v, want one endpoint", test.raw, endpoints, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseEndpointList(%q) = %+v, nil, want rejection", test.raw, endpoints)
			}
		})
	}
}

func TestValidateRejectsInvalidRuntimeEndpointHostPorts(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name   string
		mutate func(*Runtime)
	}{
		{name: "DNS missing port", mutate: func(runtime *Runtime) {
			runtime.DNSResolvers = []Endpoint{{Name: "dns", Address: "example.com"}}
		}},
		{name: "DNS non-numeric port", mutate: func(runtime *Runtime) {
			runtime.DNSResolvers = []Endpoint{{Name: "dns", Address: "example.com:notaport"}}
		}},
		{name: "DNS zero port", mutate: func(runtime *Runtime) {
			runtime.DNSResolvers = []Endpoint{{Name: "dns", Address: "example.com:0"}}
		}},
		{name: "DNS port too large", mutate: func(runtime *Runtime) {
			runtime.DNSResolvers = []Endpoint{{Name: "dns", Address: "example.com:65536"}}
		}},
		{name: "DNS unbracketed IPv6", mutate: func(runtime *Runtime) {
			runtime.DNSResolvers = []Endpoint{{Name: "dns", Address: "2001:db8::1:53"}}
		}},
		{name: "DNS unsafe host", mutate: func(runtime *Runtime) {
			runtime.DNSResolvers = []Endpoint{{Name: "dns", Address: "bad host:53"}}
		}},
		{name: "latency missing port", mutate: func(runtime *Runtime) {
			runtime.LatencyTargets = []Endpoint{{Name: "latency", Address: "example.com"}}
		}},
		{name: "latency non-numeric port", mutate: func(runtime *Runtime) {
			runtime.LatencyTargets = []Endpoint{{Name: "latency", Address: "example.com:notaport"}}
		}},
		{name: "latency port too large", mutate: func(runtime *Runtime) {
			runtime.LatencyTargets = []Endpoint{{Name: "latency", Address: "example.com:70000"}}
		}},
		{name: "STUN non-numeric port", mutate: func(runtime *Runtime) {
			runtime.STUNServers = []Endpoint{{Name: "stun", Address: "stun.example.com:notaport"}}
		}},
		{name: "STUN port too large", mutate: func(runtime *Runtime) {
			runtime.STUNServers = []Endpoint{{Name: "stun", Address: "stun.example.com:70000"}}
		}},
		{name: "STUN unsafe host", mutate: func(runtime *Runtime) {
			runtime.STUNServers = []Endpoint{{Name: "stun", Address: "bad host:3478"}}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntime(t)
			test.mutate(&runtime)
			if err := Validate(testModuleCatalog(), runtime); err == nil {
				t.Fatalf("Validate invalid endpoint runtime = nil, want rejection")
			}
		})
	}
}

func TestValidateAcceptsExecutableEndpointHostPorts(t *testing.T) {
	useEnglish(t)
	runtime := validRuntime(t)
	runtime.DNSResolvers = []Endpoint{
		{Name: "hostname", Address: "example.com:53"},
		{Name: "IPv4", Address: "192.0.2.1:53"},
		{Name: "IPv6", Address: "[2001:db8::1]:53"},
	}
	runtime.LatencyTargets = []Endpoint{
		{Name: "latency hostname", Address: "example.com:443"},
		{Name: "latency IPv4", Address: "192.0.2.1:443"},
		{Name: "latency IPv6", Address: "[2001:db8::1]:443"},
	}
	runtime.STUNServers = []Endpoint{
		{Name: "stun hostname", Address: "stun.example.com:3478"},
		{Name: "stun IPv4", Address: "192.0.2.2:3478"},
		{Name: "stun IPv6", Address: "[2001:db8::2]:3478"},
	}
	if err := Validate(testModuleCatalog(), runtime); err != nil {
		t.Fatalf("Validate executable endpoint runtime = %v, want nil", err)
	}
}

func TestValidateEndpointFamilyLiteralConsistency(t *testing.T) {
	useEnglish(t)
	groups := []struct {
		name    string
		address func(string) string
		set     func(*Runtime, Endpoint)
	}{
		{
			name: "DNS",
			address: func(value string) string {
				return value + ":53"
			},
			set: func(runtime *Runtime, endpoint Endpoint) {
				runtime.DNSResolvers = []Endpoint{endpoint}
			},
		},
		{
			name: "latency",
			address: func(value string) string {
				return value + ":443"
			},
			set: func(runtime *Runtime, endpoint Endpoint) {
				runtime.LatencyTargets = []Endpoint{endpoint}
			},
		},
		{
			name:    "route",
			address: func(value string) string { return value },
			set: func(runtime *Runtime, endpoint Endpoint) {
				runtime.RouteTargets = []Endpoint{endpoint}
			},
		},
		{
			name:    "backtrace",
			address: func(value string) string { return value },
			set: func(runtime *Runtime, endpoint Endpoint) {
				runtime.BacktraceTargets = []Endpoint{endpoint}
			},
		},
	}
	addresses := []struct {
		name, value, matching, contradictory string
	}{
		{name: "IPv4", value: "192.0.2.1", matching: IPVersion4, contradictory: IPVersion6},
		{name: "IPv6", value: "[2001:db8::1]", matching: IPVersion6, contradictory: IPVersion4},
	}
	for _, group := range groups {
		for _, address := range addresses {
			for _, test := range []struct {
				name      string
				family    string
				wantError bool
			}{
				{name: "empty family", family: ""},
				{name: "matching family", family: address.matching},
				{name: "contradictory family", family: address.contradictory, wantError: true},
			} {
				t.Run(group.name+"/"+address.name+"/"+test.name, func(t *testing.T) {
					runtime := validRuntime(t)
					endpoint := Endpoint{Name: "target", Address: group.address(address.value), Family: test.family}
					if group.name == "backtrace" {
						endpoint.Kind = BacktraceCarrierTelecom
					}
					group.set(&runtime, endpoint)
					err := Validate(testModuleCatalog(), runtime)
					if test.wantError {
						if err == nil || !strings.Contains(err.Error(), "contradict") {
							t.Fatalf("Validate(%+v) = %v, want literal family contradiction", endpoint, err)
						}
						return
					}
					if err != nil {
						t.Fatalf("Validate(%+v) = %v, want nil", endpoint, err)
					}
				})
			}
		}
		for _, family := range []string{IPVersion4, IPVersion6} {
			t.Run(group.name+"/hostname/"+family, func(t *testing.T) {
				runtime := validRuntime(t)
				group.set(&runtime, Endpoint{Name: "target", Address: group.address("edge-v6.example.com"), Family: family, Kind: BacktraceCarrierTelecom})
				if err := Validate(testModuleCatalog(), runtime); err != nil {
					t.Fatalf("Validate hostname with family %q = %v, want nil", family, err)
				}
			})
		}
	}
}

func TestValidateSTUNEndpointFamilyConsistency(t *testing.T) {
	useEnglish(t)
	for _, test := range []struct {
		name, address, family string
		wantError             string
	}{
		{name: "hostname IPv4 pin", address: "stun.example.com:3478", family: IPVersion4},
		{name: "hostname IPv6 pin", address: "stun.example.com:3478", family: IPVersion6},
		{name: "hostname automatic", address: "stun.example.com:3478"},
		{name: "IPv4 literal matching", address: "1.1.1.1:3478", family: IPVersion4},
		{name: "IPv6 literal matching", address: "[2001:db8::1]:3478", family: IPVersion6},
		{name: "IPv4 literal conflict", address: "1.1.1.1:3478", family: IPVersion6, wantError: "contradicts"},
		{name: "IPv6 literal conflict", address: "[2001:db8::1]:3478", family: IPVersion4, wantError: "contradicts"},
		{name: "invalid family", address: "stun.example.com:3478", family: "9", wantError: "family must be 4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntime(t)
			runtime.STUNServers = []Endpoint{{Name: "stun", Address: test.address, Family: test.family}}
			err := Validate(testModuleCatalog(), runtime)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate(STUN endpoint %+v) = %v, want nil", runtime.STUNServers[0], err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate(STUN endpoint %+v) = %v, want marker %q", runtime.STUNServers[0], err, test.wantError)
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
			if err := Validate(testModuleCatalog(), runtime); err == nil || !strings.Contains(err.Error(), "duplicated") {
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
	if err := Validate(testModuleCatalog(), runtime); err != nil {
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
