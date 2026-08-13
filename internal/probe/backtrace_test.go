package probe

import (
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestExtractTraceHopsFromNextTraceJSON(t *testing.T) {
	output := `{"Hops":[[{"Address":"10.0.0.1"}],[{"Address":""}],[{"Address":"219.158.16.1"}]]}`
	for _, engineName := range []string{routeEngineTiny} {
		hops := extractTraceHops(engineName, output)
		if len(hops) != 3 || hops[2] != "219.158.16.1" {
			t.Fatalf("%s hops = %v", engineName, hops)
		}
	}
}

func TestExtractNextTraceDetailsParsesOfficialNestedAddress(t *testing.T) {
	output := `{"Hops":[[{"Success":true,"Address":{"IP":"59.43.130.22","Zone":""},"RTT":3500000,"Hostname":"edge.example","Geo":{"asnumber":"4809","owner":"China Telecom","country":"CN","prov":"Shanghai","city":"Shanghai","district":"Pudong"}}],[{"Success":false,"Address":null,"RTT":0}],[{"Success":true,"Address":"2001:db8::1","RTT":"2 ms"}]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 3 {
		t.Fatalf("official details = %+v, ok=%v", details, ok)
	}
	if details[0].IP != "59.43.130.22" || details[0].Latency != "3.5 ms" || details[0].ASN != "AS4809" || details[0].Network != "China Telecom" || details[0].Location != "CN / Shanghai / Pudong" || details[0].Status != "已响应" {
		t.Fatalf("official nested detail = %+v", details[0])
	}
	if details[1].IP != "—" || details[1].Status != "无响应" {
		t.Fatalf("official timeout detail = %+v", details[1])
	}
	if details[2].IP != "2001:db8::1" || details[2].Latency != "2 ms" {
		t.Fatalf("official scalar detail = %+v", details[2])
	}
}

func TestExtractNextTraceDetailsPrioritizesGeoNetworkOverHostname(t *testing.T) {
	output := `{"Hops":[[{"Address":"203.0.113.8","Hostname":"edge.example","Geo":{"owner":"Example Carrier"}}]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 1 {
		t.Fatalf("details = %+v, ok=%v", details, ok)
	}
	if details[0].Network != "Example Carrier" {
		t.Fatalf("network = %q, want Geo.owner instead of Hostname", details[0].Network)
	}
}

func TestNextTraceParserRejectsMalformedOrUnresponsiveOutput(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantOK     bool
		wantHops   int
		wantDetail int
	}{
		{name: "malformed", output: `{"Hops":`, wantOK: false, wantHops: 0, wantDetail: 0},
		{name: "empty hops", output: `{"Hops":[]}`, wantOK: false, wantHops: 0, wantDetail: 0},
		{name: "all unresponsive", output: `{"Hops":[[{"Address":null}],[{"Address":""}],[]]}`, wantOK: true, wantHops: 0, wantDetail: 3},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			details, ok := extractNextTraceDetails(testCase.output)
			if ok != testCase.wantOK || len(details) != testCase.wantDetail {
				t.Fatalf("details = %+v, ok=%v", details, ok)
			}
			if got := routeHopCount(routeEngineTiny, testCase.output); got != testCase.wantHops {
				t.Fatalf("tiny hop count = %d, want %d", got, testCase.wantHops)
			}
		})
	}
}

func TestExtractNextTraceDetailsParsesMetadataWithoutInventingGeo(t *testing.T) {
	output := `{"Hops":[[{"Address":"59.43.130.22","RTT":"3.5 ms","ASN":4809,"ASName":"China Telecom","Location":"Shanghai"}],[{"Address":""}],[{"Address":"203.0.113.8","RTT":2.0,"Owner":"Example Net","PTR":"edge.example","country":"CN","prov":"BJ","city":"Beijing","district":"Haidian"}]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 3 {
		t.Fatalf("details = %+v, ok=%v", details, ok)
	}
	if details[0].IP != "59.43.130.22" || details[0].Latency != "3.5 ms" || details[0].ASN != "AS4809" || details[0].Network != "China Telecom" || details[0].Location != "Shanghai" {
		t.Fatalf("metadata detail = %+v", details[0])
	}
	if details[1].Status != "无响应" || details[1].IP != "—" || details[1].ASN != "—" || details[1].Location != "—" {
		t.Fatalf("unknown detail = %+v", details[1])
	}
	if details[2].IP != "203.0.113.8" || details[2].Latency != "2 ms" || details[2].ASN != "—" || details[2].Network != "Example Net" || details[2].Location != "CN / BJ / Beijing / Haidian" {
		t.Fatalf("partial metadata detail = %+v", details[2])
	}
	annotateBacktraceDetails(details)
	if details[0].Network != "China Telecom" || details[0].ASN != "AS4809" {
		t.Fatalf("existing metadata should not be overwritten: %+v", details[0])
	}
	withoutMetadata := []backtraceHop{{IP: "59.43.130.22", ASN: "—", Network: "—"}}
	annotateBacktraceDetails(withoutMetadata)
	if withoutMetadata[0].ASN != "—" || withoutMetadata[0].Network == "—" {
		t.Fatalf("route signature must not fabricate ASN, but may label line: %+v", withoutMetadata[0])
	}
}

func TestMatchRouteSignaturesIdentifiesCarrierBackbones(t *testing.T) {
	hits := matchRouteSignatures([]string{"10.0.0.1", "202.97.94.1", "59.43.130.22", "1.2.3.4"})
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Signature.Code != "163" || hits[0].Hop != 2 {
		t.Fatalf("first hit = %+v", hits[0])
	}
	if hits[1].Signature.Code != "CN2" || hits[1].Hop != 3 {
		t.Fatalf("second hit = %+v", hits[1])
	}
}

func TestMatchRouteSignaturesIdentifiesIPv6CarrierBackbones(t *testing.T) {
	hits := matchRouteSignatures([]string{"2408:8120:2::108", "2408:8000:2:70b::1", "2409:8c00::1", "240e:0:a::1"})
	if len(hits) != 4 {
		t.Fatalf("IPv6 hits = %+v", hits)
	}
	if hits[0].Signature.Code != "CUII-v6" || hits[3].Signature.Code != "CT-v6" {
		t.Fatalf("IPv6 hits = %+v", hits)
	}
}

func TestBestBacktraceHitPrefersHigherQualityLine(t *testing.T) {
	// 同一条路径同时经过 163 与 CN2 时，结论应当取更优质的 CN2。
	hits := matchRouteSignatures([]string{"202.97.94.1", "59.43.130.22"})
	best, ok := bestBacktraceHit(hits, "电信")
	if !ok || best.Signature.Code != "CN2" {
		t.Fatalf("best = %+v, ok = %v", best, ok)
	}

	if _, ok := bestBacktraceHit(nil, "电信"); ok {
		t.Fatal("empty hit list must not produce a verdict")
	}
}

func TestBestBacktraceHitNeverUsesAnotherCarrierForTargetVerdict(t *testing.T) {
	// A higher-quality foreign-carrier hit must not displace the target
	// carrier's lower-quality but relevant evidence.
	hits := matchRouteSignatures([]string{"223.120.1.1", "202.97.94.1"})
	best, ok := bestBacktraceHit(hits, "电信")
	if !ok || best.Signature.Code != "163" || best.Signature.Carrier != "电信" {
		t.Fatalf("target-carrier verdict used foreign evidence: best=%+v ok=%v", best, ok)
	}

	foreignOnly := matchRouteSignatures([]string{"223.120.1.1", "219.158.16.1"})
	if best, ok := bestBacktraceHit(foreignOnly, "电信"); ok {
		t.Fatalf("foreign-only path produced a telecom verdict: %+v", best)
	}
	status := unidentifiedBacktraceStatus(backtraceRow{
		Target: config.Endpoint{Kind: "电信"}, Hits: foreignOnly, Hops: []string{"223.120.1.1", "219.158.16.1"},
	})
	if status != "仅命中异网骨干（联通/移动），不作为电信结论" {
		t.Fatalf("foreign-only status is not explicit: %q", status)
	}
}

func TestDescribeBacktraceLineMarksCN2Inference(t *testing.T) {
	// 直接进 CN2 推测为 GIA。
	direct := matchRouteSignatures([]string{"59.43.130.22", "59.43.80.1"})
	best, _ := bestBacktraceHit(direct, "电信")
	if got := describeBacktraceLine(best, direct); !strings.Contains(got, "GIA") || !strings.Contains(got, "推测") {
		t.Fatalf("direct CN2 line = %q", got)
	}

	// 先经 163 再进 CN2 推测为 GT。
	viaBackbone := matchRouteSignatures([]string{"202.97.94.1", "59.43.130.22"})
	best, _ = bestBacktraceHit(viaBackbone, "电信")
	if got := describeBacktraceLine(best, viaBackbone); !strings.Contains(got, "GT") || !strings.Contains(got, "推测") {
		t.Fatalf("via-163 CN2 line = %q", got)
	}

	// 非 CN2 线路不应带推测后缀。
	unicom := matchRouteSignatures([]string{"219.158.16.1"})
	best, _ = bestBacktraceHit(unicom, "联通")
	if got := describeBacktraceLine(best, unicom); strings.Contains(got, "推测") {
		t.Fatalf("non-CN2 line must not be labelled as inferred: %q", got)
	}
}

func TestRouteSignaturesDoNotOverlap(t *testing.T) {
	// 前缀重叠会让匹配结果依赖表顺序，必须避免。
	for i, left := range chinaRouteSignatures {
		for j, right := range chinaRouteSignatures {
			if i == j {
				continue
			}
			if strings.HasPrefix(left.Prefix, right.Prefix) {
				t.Fatalf("signature %q overlaps %q", left.Prefix, right.Prefix)
			}
		}
	}
}
