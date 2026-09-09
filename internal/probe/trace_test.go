package probe

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// These normal-response fixtures were captured from the real FreeBSD
// 15.1-RELEASE-p3 arm64 VM used by this phase. The parser edge cases below are
// deterministic variants of this documented grammar and are not claimed as
// additional network captures.
const (
	freeBSDTracerouteIPv4Fixture = "traceroute to 127.0.0.1 (127.0.0.1), 12 hops max, 48 byte packets\n 1  127.0.0.1  7.334 ms"
	freeBSDTracerouteIPv6Fixture = "traceroute6 to ::1 (::1) from ::1, 20 hops max, 20 byte packets\n 1  ::1  11.640 ms"
	// These two fixtures were captured from the same FreeBSD 15.1-RELEASE-p3
	// arm64 VM. The first reached the configured three-hop limit; the second
	// returned three consecutive timeout slots for a documentation IPv6 target.
	freeBSDTracerouteIPv4MaxHopsFixture = `traceroute to 192.0.2.1 (192.0.2.1), 3 hops max, 48 byte packets
 1  10.0.2.2  3.403 ms
 2  140.91.234.242  1.916 ms
 3  69.31.63.194  1.470 ms`
	freeBSDTracerouteIPv6NoResponseFixture = `traceroute6 to 2001:db8::1 (2001:db8::1) from fec0::5054:ff:fe12:3456, 3 hops max, 20 byte packets
 1  *
 2  *
 3  *`
)

func TestCanonicalTraceJSONKeepsFactsAndUnknowns(t *testing.T) {
	rtt := 1.25
	trace := traceResult{
		Engine: "freebsd-traceroute", Family: "ipv4", Target: "127.0.0.1", Adapter: traceFreeBSDTracerouteAdapter,
		Hops: []traceHop{
			{Hop: 1, Responded: true, IP: "127.0.0.1", RTTMS: &rtt},
			{Hop: 2, Responded: false},
		},
	}
	data, err := trace.canonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	hops, ok := decoded["hops"].([]any)
	if !ok || len(hops) != 2 {
		t.Fatalf("canonical hops = %#v", decoded["hops"])
	}
	if _, ok := hops[1].(map[string]any)["rtt_ms"]; ok {
		t.Fatal("unknown RTT serialized as a field")
	}
	if strings.Contains(string(data), `"rtt_ms":0`) {
		t.Fatalf("canonical JSON fabricated zero RTT: %s", data)
	}
	if slots, visible, timeouts, ok := traceHopSummary(trace); !ok || slots != 2 || visible != 1 || timeouts != 1 {
		t.Fatalf("canonical summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
	}
}

func TestNextTraceCanonicalAdapter(t *testing.T) {
	output := `{"Hops":[[{"Success":true,"Address":{"IP":"203.0.113.1"},"RTT":3500000,"Geo":{"asnumber":"64500","owner":"Example Network","country":"US","prov":"California"}}],[[]]]}`
	trace, err := parseNextTraceCanonical(output, "4", "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	wantRTT := 3.5
	want := traceResult{
		Engine: "nexttrace-tiny", Family: "ipv4", Target: "203.0.113.1", Adapter: traceNextTraceAdapter,
		Hops: []traceHop{
			{Hop: 1, Responded: true, IP: "203.0.113.1", RTTMS: &wantRTT, ASN: "AS64500", Network: "Example Network", Location: "US / California"},
			{Hop: 2, Responded: false},
		},
	}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("canonical NextTrace = %#v, want %#v", trace, want)
	}
	if _, err := parseNextTraceCanonical(`{"Hops":[]}`, "4", "203.0.113.1"); err == nil {
		t.Fatal("empty NextTrace hops parsed successfully")
	}
}

func TestFreeBSDTracerouteParserRealFixtures(t *testing.T) {
	for _, test := range []struct {
		name, output, family, target string
		wantIP                       string
		wantRTT                      float64
	}{
		{name: "IPv4", output: freeBSDTracerouteIPv4Fixture, family: "4", target: "127.0.0.1", wantIP: "127.0.0.1", wantRTT: 7.334},
		{name: "IPv6", output: freeBSDTracerouteIPv6Fixture, family: "6", target: "::1", wantIP: "::1", wantRTT: 11.640},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace, err := parseFreeBSDTraceroute(test.output, test.family, test.target)
			if err != nil {
				t.Fatal(err)
			}
			if trace.Engine != "freebsd-traceroute" || trace.Adapter != traceFreeBSDTracerouteAdapter || trace.Family != traceFamilyName(test.family) || trace.Target != test.target || len(trace.Hops) != 1 {
				t.Fatalf("trace metadata = %#v", trace)
			}
			hop := trace.Hops[0]
			if !hop.Responded || hop.IP != test.wantIP || hop.RTTMS == nil || *hop.RTTMS != test.wantRTT {
				t.Fatalf("trace hop = %#v", hop)
			}
		})
	}
}

func TestFreeBSDTracerouteParserNoResponseAndAnnotations(t *testing.T) {
	output := "traceroute to 192.0.2.1 (192.0.2.1), 12 hops max, 48 byte packets\n" +
		" 1  * * *\n" +
		" 2    *   *   *\n" +
		" 3  198.51.100.1  1.000 ms !H  2.000 ms !N  3.000 ms !P"
	trace, err := parseFreeBSDTraceroute(output, "4", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Hops) != 3 || trace.Hops[0].Responded || trace.Hops[1].Responded || !trace.Hops[2].Responded || trace.Hops[2].IP != "198.51.100.1" || trace.Hops[2].RTTMS == nil || *trace.Hops[2].RTTMS != 1 {
		t.Fatalf("no-response/annotation hops = %#v", trace.Hops)
	}
}

func TestFreeBSDTracerouteParserActualMaxHopsAndNoResponse(t *testing.T) {
	maxHops, err := parseFreeBSDTraceroute(freeBSDTracerouteIPv4MaxHopsFixture, "4", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if slots, visible, timeouts, ok := traceHopSummary(maxHops); !ok || slots != 3 || visible != 3 || timeouts != 0 {
		t.Fatalf("actual max-hop trace summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
	}
	noResponse, err := parseFreeBSDTraceroute(freeBSDTracerouteIPv6NoResponseFixture, "6", "2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	if slots, visible, timeouts, ok := traceHopSummary(noResponse); !ok || slots != 3 || visible != 0 || timeouts != 3 {
		t.Fatalf("actual no-response trace summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
	}
}

func TestFreeBSDTracerouteParserRejectsUnexpectedHostnames(t *testing.T) {
	output := "traceroute to 127.0.0.1 (127.0.0.1), 12 hops max, 48 byte packets\n 1  router.example  1.000 ms"
	if _, err := parseFreeBSDTraceroute(output, "4", "127.0.0.1"); err == nil || !strings.Contains(err.Error(), "hostname") {
		t.Fatalf("hostname output error = %v", err)
	}
}

func TestFreeBSDTracerouteParserRejectsMalformedFamilyAndOutput(t *testing.T) {
	cases := []struct {
		name, output, family, want string
	}{
		{name: "empty", output: "", family: "4", want: "header"},
		{name: "stderr only", output: "traceroute: unknown host invalid.invalid", family: "4", want: "header"},
		{name: "wrong family", output: freeBSDTracerouteIPv4Fixture, family: "6", want: "header"},
		{name: "missing RTT value", output: "traceroute to 127.0.0.1 (127.0.0.1), 12 hops max, 48 byte packets\n 1  127.0.0.1 ms", family: "4", want: "RTT"},
		{name: "non-finite RTT", output: "traceroute to 127.0.0.1 (127.0.0.1), 12 hops max, 48 byte packets\n 1  127.0.0.1 NaN ms", family: "4", want: "unexpected"},
		{name: "wrong address family", output: "traceroute6 to ::1 (::1) from ::1, 20 hops max, 20 byte packets\n 1  127.0.0.1  1.000 ms", family: "6", want: "IPv4"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseFreeBSDTraceroute(test.output, test.family, "target"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parse error = %v, want %q", err, test.want)
			}
		})
	}
}
