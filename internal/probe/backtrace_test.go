package probe

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
)

func TestNextTraceParsesAndClassifiesRepresentativeHop(t *testing.T) {
	output := `{"Hops":[[{"Success":true,"Address":{"IP":"59.43.130.22"},"RTT":3500000,"Geo":{"asnumber":"4809","owner":"China Telecom","country":"CN","prov":"Shanghai"}}],[[]]]}`
	trace, err := parseNextTraceCanonical(output, "4", "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	details := backtraceDetailsFromTrace(trace)
	if len(details) != 2 {
		t.Fatalf("nexttrace details = %+v", details)
	}
	if details[0].IP != "59.43.130.22" || details[0].ASN != "AS4809" || details[0].Latency != "3.5 ms" || details[0].Network != "China Telecom" || details[0].Location != "CN / Shanghai" || details[0].Status != "probe.backtrace.hop.responded" || details[1].Status != "probe.backtrace.hop.no_response" || details[1].IP != "" {
		t.Fatalf("parsed hops = %+v", details)
	}

	hits := matchTraceSignatures(trace)
	best, ok := bestBacktraceHit(hits, config.BacktraceCarrierTelecom)
	if !ok || best.Signature.Code != "CN2" {
		t.Fatalf("classified route = %+v, ok=%v", best, ok)
	}
	if details[0].Network != "China Telecom" {
		t.Fatalf("raw provider network was overwritten: %+v", details[0])
	}
}

func TestNextTraceParserRejectsEmptyOutput(t *testing.T) {
	if _, err := parseNextTraceCanonical(`{"Hops":[]}`, "4", "203.0.113.1"); err == nil {
		t.Fatal("empty nexttrace output parsed successfully")
	}
}

func TestBacktraceChoosesBestCarrierAndFiniteLineReasons(t *testing.T) {
	trace := traceResult{Engine: "test", Family: "ipv4", Target: "test", Hops: []traceHop{
		{Hop: 1, Responded: true, IP: "202.97.0.1"},
		{Hop: 2, Responded: true, IP: "59.43.0.1"},
		{Hop: 3, Responded: true, IP: "219.158.0.1"},
	}}
	hits := matchTraceSignatures(trace)
	best, ok := bestBacktraceHit(hits, config.BacktraceCarrierTelecom)
	if !ok || best.Signature.Code != "CN2" || best.Hop != 2 || best.Source != signatureMatchByCIDR || backtraceLineKey(best) != backtraceLineTelecomCN2VariantUnknown {
		t.Fatalf("best backtrace hit = %+v, line=%q", best, backtraceLineKey(best))
	}
	if _, ok := bestBacktraceHit(hits, config.BacktraceCarrierMobile); ok {
		t.Fatal("foreign route was accepted for an unrelated carrier")
	}

	tests := []struct {
		name string
		row  backtraceRow
		want string
	}{
		{name: "foreign", row: backtraceRow{Target: config.Endpoint{Kind: config.BacktraceCarrierTelecom}, Hits: []backtraceHit{{Signature: routeSignature{Carrier: config.BacktraceCarrierMobile}}}, Hops: []string{"223.120.0.1"}}, want: backtraceReasonForeignOnly},
		{name: "unknown", row: backtraceRow{Target: config.Endpoint{Kind: config.BacktraceCarrierTelecom}, Hops: []string{"198.51.100.1"}}, want: backtraceReasonNoKnownSignature},
		{name: "limited", row: backtraceRow{Target: config.Endpoint{Kind: config.BacktraceCarrierTelecom}, Hops: []string{"198.51.100.1", "", ""}}, want: backtraceReasonLimitedOrFiltered},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := backtraceUnidentifiedReason(test.row); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBacktraceLineKeysCoverKnownSignatures(t *testing.T) {
	wants := map[string]string{
		"59.43.0.1":    backtraceLineTelecomCN2VariantUnknown,
		"202.97.0.1":   backtraceLineTelecomChinanet,
		"218.104.0.1":  backtraceLineUnicomNetworkUnknown,
		"218.105.0.1":  backtraceLineUnicomNetworkUnknown,
		"219.158.0.1":  backtraceLineUnicomBackbone,
		"223.120.0.1":  backtraceLineMobileCMI,
		"221.183.0.1":  backtraceLineMobileCMNET,
		"240e::1":      backtraceLineTelecomIPv6,
		"2408:8120::1": backtraceLineUnicomIPv6,
		"2408:8000::1": backtraceLineUnicomIPv6,
		"2409:8000::1": backtraceLineMobileCMNETIPv6,
	}
	for address, want := range wants {
		trace := traceResult{Engine: "test", Family: traceFamilyName(addressFamily(address)), Target: "test", Hops: []traceHop{{Hop: 1, Responded: true, IP: address}}}
		hits := matchTraceSignatures(trace)
		if len(hits) != 1 || hits[0].Signature.LineKey != want {
			t.Fatalf("%s hit = %+v, want stable key %q", address, hits, want)
		}
	}

	if hits := matchTraceSignatures(traceResult{Engine: "test", Family: "ipv6", Target: "test", Hops: []traceHop{{Hop: 1, Responded: true, IP: "2409::1"}}}); len(hits) != 0 {
		t.Fatalf("out-of-allocation 2409 address was classified: %+v", hits)
	}
}

func TestBacktraceClassificationEvidencePriorityAndCIDRBoundaries(t *testing.T) {
	trace := func(address, asn, family string) traceResult {
		return traceResult{Engine: "test", Family: family, Target: "test", Hops: []traceHop{{Hop: 1, Responded: true, IP: address, ASN: asn}}}
	}

	// A recognized hop ASN is stronger than an address allocation that points
	// at a different operator. This is both the priority contract and the
	// required same-/different-carrier evidence case.
	hits := matchTraceSignatures(trace("59.43.0.1", "AS4837", "ipv4"))
	if len(hits) != 1 || hits[0].Source != signatureMatchByASN || hits[0].Signature.Code != "169" || hits[0].Signature.Carrier != config.BacktraceCarrierUnicom {
		t.Fatalf("observed ASN did not override CN2 CIDR: %+v", hits)
	}
	if _, ok := bestBacktraceHit(hits, config.BacktraceCarrierTelecom); ok {
		t.Fatalf("different-carrier ASN was accepted for Telecom: %+v", hits)
	}
	hits = matchTraceSignatures(trace("59.43.0.1", "AS4134", "ipv4"))
	if len(hits) != 1 || hits[0].Source != signatureMatchByASN || hits[0].Signature.Code != "CHINANET" || hits[0].Signature.Carrier != config.BacktraceCarrierTelecom {
		t.Fatalf("same-carrier observed ASN was not preferred over CN2 CIDR: %+v", hits)
	}

	hits = matchTraceSignatures(trace("59.43.0.1", "", "ipv4"))
	if len(hits) != 1 || hits[0].Source != signatureMatchByCIDR || hits[0].CIDR != "59.43.0.0/16" {
		t.Fatalf("exact CIDR fallback = %+v", hits)
	}
	if hits = matchTraceSignatures(trace("59.43.0.1", "AS64500", "ipv4")); len(hits) != 0 {
		t.Fatalf("unrecognized observed ASN was contradicted by CIDR fallback: %+v", hits)
	}
	hits = matchTraceSignatures(trace("198.51.100.1", "AS9808", "ipv4"))
	if len(hits) != 1 || hits[0].Source != signatureMatchByASN || hits[0].Signature.LineKey != backtraceLineMobileNetworkUnknown || hits[0].Signature.Code != "CHINAMOBILE" {
		t.Fatalf("AS9808 was overclaimed as CMNET: %+v", hits)
	}
	hits = matchTraceSignatures(trace("198.51.100.1", "AS56048", "ipv4"))
	if len(hits) != 1 || hits[0].Source != signatureMatchByASN || hits[0].Signature.LineKey != backtraceLineMobileCMNET {
		t.Fatalf("AS56048 lost its directly supported CMNET identity: %+v", hits)
	}

	// The 210.51 allocation is intentionally not a production rule: its
	// registry object names an IDC allocation but does not establish a current
	// operator or routing identity. Both allocation edges must remain unknown.
	for _, address := range []string{"210.51.0.0", "210.51.19.255"} {
		if hits := matchTraceSignatures(trace(address, "", "ipv4")); len(hits) != 0 {
			t.Fatalf("unproven 210.51 allocation was classified at %s: %+v", address, hits)
		}
	}

	// The last tier is deliberately explicit and low confidence. A higher
	// tier suppresses it, so a coarse indicator cannot override a CIDR fact.
	synthetic := []routeSignature{
		{Code: "CIDR", LineKey: "cidr", Carrier: config.BacktraceCarrierTelecom, CIDRs: routePrefixes("203.0.113.0/24")},
		{Code: "HEURISTIC", LineKey: "heuristic", Carrier: config.BacktraceCarrierTelecom, HeuristicPrefixes: []string{"203.0."}},
	}
	hits = matchTraceSignaturesWith(trace("203.0.113.1", "", "ipv4"), synthetic)
	if len(hits) != 1 || hits[0].Source != signatureMatchByCIDR {
		t.Fatalf("CIDR did not suppress heuristic: %+v", hits)
	}
	hits = matchTraceSignaturesWith(trace("203.0.114.1", "", "ipv4"), synthetic)
	if len(hits) != 1 || hits[0].Source != signatureMatchByHeuristic {
		t.Fatalf("heuristic tier = %+v", hits)
	}

	boundaries := []struct {
		name    string
		address string
		family  string
		want    string
	}{
		{name: "CN2 lower", address: "59.43.0.0", family: "ipv4", want: backtraceLineTelecomCN2VariantUnknown},
		{name: "CN2 upper", address: "59.43.255.255", family: "ipv4", want: backtraceLineTelecomCN2VariantUnknown},
		{name: "CHINANET lower", address: "202.97.0.0", family: "ipv4", want: backtraceLineTelecomChinanet},
		{name: "CHINANET upper", address: "202.97.95.255", family: "ipv4", want: backtraceLineTelecomChinanet},
		{name: "Unicom allocation upper", address: "218.107.255.255", family: "ipv4", want: backtraceLineUnicomNetworkUnknown},
		{name: "China169 allocation upper", address: "219.158.31.255", family: "ipv4", want: backtraceLineUnicomBackbone},
		{name: "CMI allocation upper", address: "223.120.127.255", family: "ipv4", want: backtraceLineMobileCMI},
		{name: "CMNET allocation upper", address: "221.183.255.255", family: "ipv4", want: backtraceLineMobileCMNET},
		{name: "Telecom IPv6 upper", address: "240e:3fff:ffff:ffff::", family: "ipv6", want: backtraceLineTelecomIPv6},
		{name: "Unicom IPv6 upper", address: "2408:8fff:ffff:ffff::", family: "ipv6", want: backtraceLineUnicomIPv6},
		{name: "Mobile IPv6 upper", address: "2409:8fff:ffff:ffff::", family: "ipv6", want: backtraceLineMobileCMNETIPv6},
	}
	for _, test := range boundaries {
		t.Run(test.name, func(t *testing.T) {
			hits := matchTraceSignatures(trace(test.address, "", test.family))
			if len(hits) != 1 || hits[0].Source != signatureMatchByCIDR || hits[0].Signature.LineKey != test.want {
				t.Fatalf("boundary address %s = %+v, want CIDR %q", test.address, hits, test.want)
			}
		})
	}

	outside := []struct {
		name    string
		address string
		family  string
	}{
		{name: "CN2 outside", address: "59.44.0.0", family: "ipv4"},
		{name: "CHINANET HK boundary", address: "202.97.96.0", family: "ipv4"},
		{name: "Unicom outside", address: "218.108.0.0", family: "ipv4"},
		{name: "CNC allocation outside", address: "210.51.20.0", family: "ipv4"},
		{name: "China169 outside", address: "219.158.32.0", family: "ipv4"},
		{name: "CMI outside", address: "223.120.128.0", family: "ipv4"},
		{name: "CMNET outside", address: "221.184.0.0", family: "ipv4"},
		{name: "Telecom IPv6 outside", address: "240e:4000::", family: "ipv6"},
		{name: "Unicom IPv6 outside", address: "2408:9000::", family: "ipv6"},
		{name: "Mobile IPv6 outside", address: "2409:9000::", family: "ipv6"},
	}
	for _, test := range outside {
		t.Run(test.name, func(t *testing.T) {
			if hits := matchTraceSignatures(trace(test.address, "", test.family)); len(hits) != 0 {
				t.Fatalf("outside address %s was classified: %+v", test.address, hits)
			}
		})
	}
}

func TestBacktraceCN2VariantNeverUsesPathOrderAndVersionsStaySynchronized(t *testing.T) {
	trace := traceResult{Engine: "test", Family: "ipv4", Target: "test", Hops: []traceHop{
		{Hop: 1, Responded: true, IP: "202.97.0.1"},
		{Hop: 2, Responded: true, IP: "59.43.0.1"},
	}}
	hits := matchTraceSignatures(trace)
	best, ok := bestBacktraceHit(hits, config.BacktraceCarrierTelecom)
	if !ok || best.Signature.Code != "CN2" || backtraceLineKey(best) != backtraceLineTelecomCN2VariantUnknown {
		t.Fatalf("CN2 mixed path = %+v, line=%q", best, backtraceLineKey(best))
	}
	for _, hit := range hits {
		if strings.Contains(strings.ToUpper(hit.Signature.LineKey), "GIA") || strings.Contains(strings.ToUpper(hit.Signature.LineKey), "GT") {
			t.Fatalf("CN2 evidence retained a forbidden variant: %+v", hit)
		}
	}

	if !backtraceFixtureTestApplies(t) {
		t.Skip("producer version synchronization uses the Linux fixture; FreeBSD runtime uses base traceroute")
	}
	result := runBacktraceFixtureResult(t)
	if result.Methodology.Parameters["signature_set"] != "china-backbone-"+backtraceSignatureVersion || result.Measurements[0].Method != "china-backbone-signature-"+backtraceSignatureVersion {
		t.Fatalf("classification versions are not synchronized: parameters=%v measurements=%+v", result.Methodology.Parameters, result.Measurements)
	}
}

func TestChinaRouteSignaturesHaveAuditedSourceAndNoUnboundedPrefixRules(t *testing.T) {
	if len(chinaRouteSignatures) == 0 {
		t.Fatal("production route signature set is empty")
	}
	for _, signature := range chinaRouteSignatures {
		if signature.SourceURL == "" {
			t.Fatalf("signature %q has no source URL", signature.Code)
		}
		if len(signature.ASNs) == 0 && len(signature.CIDRs) == 0 && len(signature.HeuristicPrefixes) == 0 {
			t.Fatalf("signature %q has no matching fact", signature.Code)
		}
		if len(signature.HeuristicPrefixes) > 0 {
			t.Fatalf("production signature %q still uses an unreviewed coarse prefix: %v", signature.Code, signature.HeuristicPrefixes)
		}
	}
}

func addressFamily(address string) string {
	if strings.Contains(address, ":") {
		return config.IPVersion6
	}
	return config.IPVersion4
}

func TestBacktraceParserKeepsExternalFactsAndUsesEmptyCanonicalMissing(t *testing.T) {
	output := `{"Hops":[[{"Address":{"IP":"198.51.100.2"},"RTT":"12.5 ms","ASN":"64500","Network":"原始网络","Location":"原始位置"}],[{}]]}`
	trace, err := parseNextTraceCanonical(output, "4", "198.51.100.2")
	if err != nil {
		t.Fatal(err)
	}
	details := backtraceDetailsFromTrace(trace)
	if len(details) != 2 {
		t.Fatalf("details = %+v", details)
	}
	want := backtraceHop{Hop: 1, IP: "198.51.100.2", Latency: "12.5 ms", ASN: "AS64500", Network: "原始网络", Location: "原始位置", Status: "probe.backtrace.hop.responded"}
	if !reflect.DeepEqual(details[0], want) || details[1].IP != "" || details[1].Latency != "" || details[1].Network != "" || details[1].Status != "probe.backtrace.hop.no_response" {
		t.Fatalf("external/missing facts = %+v, want first %+v", details, want)
	}
	if got := backtraceCellValue(details[1].Network); got != backtraceMissingValue {
		t.Fatalf("missing table value = %q, want %q", got, backtraceMissingValue)
	}
}

func TestBacktraceClassificationCannotMutateCanonicalHopFacts(t *testing.T) {
	rtt := 4.25
	trace := traceResult{
		Engine: "nexttrace-tiny", Family: "ipv4", Target: "203.0.113.1", Adapter: traceNextTraceAdapter,
		Hops: []traceHop{{Hop: 1, Responded: true, IP: "59.43.130.22", RTTMS: &rtt}},
	}
	details := backtraceDetailsFromTrace(trace)
	if len(details) != 1 || details[0].IP != "59.43.130.22" || details[0].Latency != "4.25 ms" || details[0].Network != "" || details[0].ASN != "" || details[0].Location != "" {
		t.Fatalf("canonical facts were not copied faithfully: %+v", details)
	}
	hits := matchTraceSignatures(trace)
	if len(hits) != 1 || hits[0].Signature.Code != "CN2" || hits[0].IP != trace.Hops[0].IP {
		t.Fatalf("classification evidence = %+v, want independent CN2 hit", hits)
	}
	if trace.Hops[0].Network != "" {
		t.Fatalf("classifier polluted canonical network fact: %+v", trace.Hops[0])
	}
}

func TestBacktraceFreeBSDTraceFactsKeepUnavailableMetadataMissing(t *testing.T) {
	rtt := 1.5
	trace := traceResult{
		Engine: "freebsd-traceroute", Family: "ipv4", Target: "127.0.0.1", Adapter: traceFreeBSDTracerouteAdapter,
		Hops: []traceHop{{Hop: 1, Responded: true, IP: "59.43.0.1", RTTMS: &rtt}},
	}
	details := backtraceDetailsFromTrace(trace)
	if len(details) != 1 || details[0].Network != "" || details[0].ASN != "" || details[0].Location != "" {
		t.Fatalf("FreeBSD unavailable metadata was fabricated: %+v", details)
	}
	for _, value := range []string{details[0].Network, details[0].ASN, details[0].Location} {
		if got := backtraceCellValue(value); got != backtraceMissingValue {
			t.Fatalf("missing FreeBSD fact rendered as %q, want %q", got, backtraceMissingValue)
		}
	}
	hits := matchTraceSignatures(trace)
	if len(hits) != 1 || hits[0].Signature.Code != "CN2" {
		t.Fatalf("FreeBSD classifier evidence = %+v, want CN2 indicator", hits)
	}
}

func writeBacktraceFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, traceNextTraceEngineName)
	script := `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-V" ]; then
  echo "backtrace-fixture 1"
  exit 0
fi
target=""
for arg do target="$arg"; done
case "$target" in
identified)
	  printf '%s' '{"Hops":[[{"Address":{"IP":"59.43.130.22"},"RTT":3500000,"ASN":"4809","Geo":{"owner":"外部网","country":"外部国","prov":"外部城"}}],[{"Address":{"IP":"198.51.100.1"}}]]}'
  ;;
foreign)
  printf '%s' '{"Hops":[[{"Address":{"IP":"223.120.1.1"}}],[{"Address":{"IP":"198.51.100.2"}}]]}'
  ;;
nosignature)
  printf '%s' '{"Hops":[[{"Address":{"IP":"198.51.100.3"}}],[{"Address":{"IP":"198.51.100.4"}}]]}'
  ;;
limited)
  printf '%s' '{"Hops":[[],[],[{"Address":{"IP":"198.51.100.5"}}]]}'
  ;;
partial)
  printf '%s' '{"Hops":[[{"Address":{"IP":"59.43.130.23"}}]]}'
  exit 7
  ;;
partial_unknown)
  printf '%s' '{"Hops":[[{"Address":{"IP":"198.51.100.6"}}]]}'
  exit 8
  ;;
parse)
  printf '%s' '{"invalid":true}'
  ;;
error)
  printf '%s' 'fixture typed command error'
  exit 9
  ;;
no_response)
  printf '%s' '{"Hops":[[],[]]}'
  ;;
*)
  printf '%s' '{"Hops":[[]]}'
  ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func backtraceFixtureTargets() []config.Endpoint {
	targets := []config.Endpoint{
		{Name: "probe.backtrace.target.beijing.telecom.ipv4", Address: "identified", Kind: config.BacktraceCarrierTelecom},
		{Name: "probe.backtrace.target.guangzhou.unicom.ipv4", Address: "foreign", Kind: config.BacktraceCarrierUnicom},
		{Name: "probe.backtrace.target.shanghai.mobile.ipv6", Address: "nosignature", Kind: config.BacktraceCarrierMobile},
		{Name: "probe.backtrace.target.chengdu.telecom.ipv4", Address: "limited", Kind: config.BacktraceCarrierTelecom},
		{Name: "用户部分", Address: "partial", Kind: config.BacktraceCarrierTelecom},
		{Name: "用户未知", Address: "partial_unknown", Kind: config.BacktraceCarrierUnicom},
		{Name: "用户解析", Address: "parse", Kind: config.BacktraceCarrierMobile},
		{Name: "用户错误", Address: "error", Kind: config.BacktraceCarrierTelecom},
		{Name: "用户无响应", Address: "no_response", Kind: config.BacktraceCarrierMobile},
	}
	return targets
}

func TestBacktraceProducerEmitsDirectMachineFactsAndPreservesErrors(t *testing.T) {
	if !backtraceFixtureTestApplies(t) {
		t.Skip("NextTrace producer fixture is Linux-only; FreeBSD uses base-system traceroute")
	}
	fixturePath := writeBacktraceFixture(t)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", fixturePath); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ToolBinEnv, fixturePath)
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = backtraceFixtureTargets()
	result := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if result.Title != "module.backtrace.title" || result.Description != "probe.backtrace.description" || len(result.SummaryMessages) != 1 {
		t.Fatalf("direct result shape = %+v", result)
	}
	if result.SummaryMessages[0].Key != "probe.backtrace.summary.values" || !reflect.DeepEqual(result.SummaryMessages[0].Args, []string{"2", "9"}) {
		t.Fatalf("summary = %+v", result.SummaryMessages)
	}
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 6 || result.Evidence.Expected != 9 {
		t.Fatalf("status/evidence = %s/%+v", result.Status, result.Evidence)
	}
	if len(result.Failures) != 5 {
		t.Fatalf("failures = %+v", result.Failures)
	}
	var sawPartial, sawPartialUnknown, sawError, sawParse, sawNoResponse bool
	for _, failure := range result.Failures {
		switch {
		case failure.Target == "partial":
			sawPartial = failure.Message == "exit status 7"
		case failure.Target == "partial_unknown":
			sawPartialUnknown = failure.Message == "exit status 8"
		case failure.Target == "error":
			sawError = strings.HasPrefix(failure.Message, "exit status 9; parser diagnostic:")
		case failure.Target == "parse":
			sawParse = failure.Category == model.FailureParse && strings.Contains(failure.Message, "NextTrace output contains no hop slots")
		case failure.Target == "no_response":
			sawNoResponse = failure.Category == model.FailureUnknown && failure.Stage == "trace" && failure.Message == ""
		}
	}
	if !sawPartial || !sawPartialUnknown || !sawError || !sawParse || !sawNoResponse {
		t.Fatalf("typed/parse failures = %+v", result.Failures)
	}
	if len(result.Tables) != 2 || len(result.Tables[0].Rows) != 9 || len(result.Tables[0].Columns) != 8 {
		t.Fatalf("tables = %+v", result.Tables)
	}
	rows := result.Tables[0].Rows
	if rows[0][0].Text() != "probe.backtrace.carrier.telecom" || rows[0][1].Text() != "probe.backtrace.target.beijing.telecom.ipv4" || rows[0][2].Text() != backtraceLineTelecomCN2VariantUnknown || rows[0][5].Text() != backtraceStatusIdentified || rows[0][6].Text() != backtraceReasonSignatureMatch || rows[0][7].Text() != "probe.backtrace.evidence.asn" {
		t.Fatalf("identified row = %+v", rows[0])
	}
	if rows[1][0].Text() != "probe.backtrace.carrier.unicom" || rows[1][5].Text() != backtraceStatusUnidentified || rows[1][6].Text() != backtraceReasonForeignOnly || rows[2][0].Text() != "probe.backtrace.carrier.mobile" || rows[2][6].Text() != backtraceReasonNoKnownSignature || rows[3][6].Text() != backtraceReasonLimitedOrFiltered {
		t.Fatalf("unidentified rows = %v, %v, %v, %v", rows[1], rows[2], rows[3], rows[4])
	}
	if rows[4][2].Text() != backtraceLineTelecomCN2VariantUnknown || rows[4][5].Text() != backtraceStatusIdentified || rows[4][6].Text() != backtraceReasonSignatureMatch || rows[4][7].Text() != "probe.backtrace.evidence.cidr" || rows[5][5].Text() != backtraceStatusUnidentified || rows[5][6].Text() != backtraceReasonNoKnownSignature || rows[6][5].Text() != backtraceStatusFailed || rows[6][6].Text() != backtraceReasonParseFailed || rows[7][5].Text() != backtraceStatusFailed || rows[7][6].Text() != backtraceReasonTraceError || rows[8][5].Text() != backtraceStatusFailed || rows[8][6].Text() != backtraceReasonNoResponsiveHops {
		t.Fatalf("failure rows = %v, %v, %v", rows[4], rows[5], rows[6])
	}
	if _, ok := rows[0][0].Key(); !ok {
		t.Fatalf("backtrace carrier is not a tagged key: %#v", rows[0][0])
	}
	if target, ok := rows[0][1].Key(); !ok || target != "probe.backtrace.target.beijing.telecom.ipv4" {
		t.Fatalf("built-in backtrace target is not a tagged key: %#v", rows[0][1])
	}
	if target, ok := rows[4][1].Raw(); !ok || target != "用户部分" {
		t.Fatalf("custom backtrace target is not raw: %#v", rows[4][1])
	}
	if target, ok := result.Tables[1].Rows[0][0].Key(); !ok || target != "probe.backtrace.target.beijing.telecom.ipv4" {
		t.Fatalf("detail built-in backtrace target is not a tagged key: %#v", result.Tables[1].Rows[0][0])
	}
	if target, ok := result.Tables[1].Rows[9][0].Raw(); !ok || target != "用户部分" {
		t.Fatalf("detail custom backtrace target is not raw: %#v", result.Tables[1].Rows[9][0])
	}
	if len(result.Tables[1].Rows) != 15 {
		t.Fatalf("hop rows = %d, want partial/no-response details retained", len(result.Tables[1].Rows))
	}
	if result.Tables[1].Rows[13][8].Text() != "probe.backtrace.hop.no_response" || result.Tables[1].Rows[14][8].Text() != "probe.backtrace.hop.no_response" {
		t.Fatalf("no-response hop rows = %v, %v", result.Tables[1].Rows[13], result.Tables[1].Rows[14])
	}
	if _, ok := result.Tables[1].Rows[13][8].Key(); !ok {
		t.Fatalf("no-response hop status is not a tagged key: %#v", result.Tables[1].Rows[13][8])
	}
	if len(result.TextBlocks) != 16 {
		t.Fatalf("raw/canonical blocks = %d, want one raw and one canonical block per parsed target plus raw failures", len(result.TextBlocks))
	}
	var rawBlocks, normalizedBlocks int
	for _, block := range result.TextBlocks {
		if block.Content == "" {
			t.Fatalf("empty trace block = %+v", block)
		}
		switch block.Title {
		case backtraceRawOutputTitle:
			rawBlocks++
		case backtraceNormalizedJSONTitle:
			normalizedBlocks++
			if block.Language != "json" || !strings.Contains(block.Content, `"adapter":"nexttrace-json-v1"`) {
				t.Fatalf("canonical trace block = %+v", block)
			}
		default:
			t.Fatalf("unexpected trace block = %+v", block)
		}
	}
	if rawBlocks != 9 || normalizedBlocks != 7 {
		t.Fatalf("trace block counts = raw:%d normalized:%d", rawBlocks, normalizedBlocks)
	}
}

func TestBacktraceStatusReflectsTargetFailureMatrix(t *testing.T) {
	if !backtraceFixtureTestApplies(t) {
		t.Skip("NextTrace producer fixture is Linux-only; FreeBSD uses base-system traceroute")
	}
	fixturePath := writeBacktraceFixture(t)
	t.Setenv("PATH", fixturePath)
	t.Setenv(ToolBinEnv, fixturePath)
	allTargets := backtraceFixtureTargets()
	cases := []struct {
		name    string
		targets []config.Endpoint
		status  model.Status
	}{
		{name: "all success", targets: allTargets[:4], status: model.StatusOK},
		{name: "all partial results", targets: allTargets[4:6], status: model.StatusWarning},
		{name: "partial failure", targets: allTargets[:5], status: model.StatusWarning},
		{name: "all failure", targets: allTargets[6:], status: model.StatusError},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
			if err != nil {
				t.Fatal(err)
			}
			runtime.BacktraceTargets = test.targets
			result := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
			if result.Status != test.status {
				t.Fatalf("backtrace status = %s, want %s; failures=%+v", result.Status, test.status, result.Failures)
			}
		})
	}
}

func TestBacktraceSkipReasonsAreDistinctAndMachineOnly(t *testing.T) {
	if !backtraceToolMissingTestApplies(t) {
		// FreeBSD's base-system backend remains available regardless of PATH;
		// the no-target and no-family branches below are still deterministic.
		runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
		if err != nil {
			t.Fatal(err)
		}
		runtime.BacktraceTargets = nil
		noTargets := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
		if noTargets.SummaryMessages[0].Key != "probe.backtrace.summary.no_targets" || len(noTargets.Failures) != 0 {
			t.Fatalf("no targets = %+v", noTargets)
		}
		runtime.BacktraceTargets = []config.Endpoint{{Name: "ipv4", Address: "1.1.1.1", Kind: config.BacktraceCarrierTelecom}}
		runtime.IPVersion = config.IPVersion6
		noFamily := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
		if noFamily.SummaryMessages[0].Key != "probe.backtrace.summary.no_family_targets" || len(noFamily.Failures) != 0 {
			t.Fatalf("no family targets = %+v", noFamily)
		}
		return
	}
	oldPath := os.Getenv("PATH")
	empty := t.TempDir()
	if err := os.Setenv("PATH", empty); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ToolBinEnv, "")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = nil
	missing := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if missing.SummaryMessages[0].Key != "probe.backtrace.summary.tool_missing" || missing.Failures[0].Message != "" || missing.Notes[0] != "probe.backtrace.note.tool_missing" {
		t.Fatalf("tool missing = %+v", missing)
	}

	fixturePath := writeBacktraceFixture(t)
	if err := os.Setenv("PATH", fixturePath); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ToolBinEnv, fixturePath)
	runtime.BacktraceTargets = nil
	noTargets := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if noTargets.SummaryMessages[0].Key != "probe.backtrace.summary.no_targets" || len(noTargets.Failures) != 0 {
		t.Fatalf("no targets = %+v", noTargets)
	}
	runtime.BacktraceTargets = []config.Endpoint{{Name: "ipv4", Address: "1.1.1.1", Kind: config.BacktraceCarrierTelecom}}
	runtime.IPVersion = config.IPVersion6
	noFamily := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if noFamily.SummaryMessages[0].Key != "probe.backtrace.summary.no_family_targets" || len(noFamily.Failures) != 0 {
		t.Fatalf("no family targets = %+v", noFamily)
	}
}

func runBacktraceFixtureResult(t *testing.T) model.Result {
	result, _ := runBacktraceFixtureResultWithPath(t)
	return result
}

func runBacktraceFixtureResultWithPath(t *testing.T) (model.Result, string) {
	t.Helper()
	fixtureDirectory := writeBacktraceFixture(t)
	fixturePath := filepath.Join(fixtureDirectory, traceNextTraceEngineName)
	t.Setenv("PATH", fixtureDirectory)
	t.Setenv(ToolBinEnv, fixtureDirectory)
	runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = backtraceFixtureTargets()
	return (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime}), fixturePath
}

func TestBacktraceProducerDirectShapeContract(t *testing.T) {
	if !backtraceFixtureTestApplies(t) {
		t.Skip("NextTrace producer fixture is Linux-only; FreeBSD uses base-system traceroute")
	}
	result, _ := runBacktraceFixtureResultWithPath(t)
	if result.Methodology.Kind != "heuristic" || result.Methodology.Label != "methodology.heuristic" ||
		result.Methodology.Engine != "probe.backtrace.methodology.engine" || result.Methodology.Profile != "probe.backtrace.profile" ||
		result.Methodology.ComparisonScope != "probe.backtrace.comparison_scope" {
		t.Fatalf("methodology = %#v", result.Methodology)
	}
	assertProducerParameterScope(t, result, "ip_version", "targets", "max_hops", "signature_set", "tool_version", "adapter", "arguments")
	parameters := result.Methodology.Parameters
	if parameters["ip_version"] != config.IPVersionAuto || parameters["targets"] != comparisonParameterJSON(backtraceFixtureTargets()) || parameters["max_hops"] != strconv.Itoa(backtraceMaxHops) || parameters["signature_set"] != "china-backbone-v3" || parameters["tool_version"] != "backtrace-fixture 1" || parameters["adapter"] != traceNextTraceAdapter || parameters["arguments"] == "" {
		t.Fatalf("backtrace comparison parameters = %v", parameters)
	}
	wantFields := []model.Field{
		{Key: "engine", Label: "probe.backtrace.field.engine", Value: model.RawValue(traceNextTraceEngineName)},
		{Key: "version", Label: "probe.backtrace.field.version", Value: model.RawValue("backtrace-fixture 1")},
		{Key: "adapter", Label: "probe.backtrace.field.adapter", Value: model.RawValue(traceNextTraceAdapter)},
		{Key: "arguments", Label: "probe.backtrace.field.arguments", Value: model.RawValue(parameters["arguments"])},
	}
	if len(result.Fields) != len(wantFields) {
		t.Fatalf("fields = %#v", result.Fields)
	}
	for index, want := range wantFields {
		got := result.Fields[index]
		if got.Key != want.Key || got.Label != want.Label || (want.Value.Text() != "" && got.Value.Text() != want.Value.Text()) {
			t.Fatalf("field %d = %#v, want key/label/value %#v", index, got, want)
		}
	}
	measurement := result.Measurements[0]
	if measurement.Key != "backtrace_identified" || measurement.Label != "probe.backtrace.metric.identified" || measurement.Unit != "count" || measurement.Display.Text() != "2/9" || measurement.Method != "china-backbone-signature-v3" || measurement.Value != 2 {
		t.Fatalf("measurement = %#v", measurement)
	}
	wantSummaryColumns := []model.TableColumn{
		{Key: "provider", Label: "probe.backtrace.column.provider"}, {Key: "reference_target", Label: "probe.backtrace.column.target"}, {Key: "line", Label: "probe.backtrace.column.line"},
		{Key: "hit_hop", Label: "probe.backtrace.column.hit_hop"}, {Key: "hit_ip", Label: "probe.backtrace.column.hit_ip"}, {Key: "status", Label: "probe.backtrace.column.status"}, {Key: "reason", Label: "probe.backtrace.column.reason"}, {Key: "evidence", Label: "probe.backtrace.column.evidence"},
	}
	wantHopColumns := []model.TableColumn{
		{Key: "reference_target", Label: "probe.backtrace.column.target"}, {Key: "provider", Label: "probe.backtrace.column.provider"}, {Key: "hop", Label: "probe.backtrace.column.hop"}, {Key: "latency_ms", Label: "probe.backtrace.column.latency"},
		{Key: "ip", Label: "probe.backtrace.column.ip"}, {Key: "asn", Label: "probe.backtrace.column.asn"}, {Key: "network", Label: "probe.backtrace.column.network"}, {Key: "location", Label: "probe.backtrace.column.location"}, {Key: "status", Label: "probe.backtrace.column.status"},
	}
	if len(result.Tables) != 2 || result.Tables[0].Title != "probe.backtrace.table.summary" || !reflect.DeepEqual(result.Tables[0].Columns, wantSummaryColumns) || result.Tables[0].RowIdentity != "" || result.Tables[1].Title != "probe.backtrace.table.hops" || !reflect.DeepEqual(result.Tables[1].Columns, wantHopColumns) || result.Tables[1].RowIdentity != "" {
		t.Fatalf("table shape = %#v", result.Tables)
	}
	if len(result.Sources) != 2 || result.Sources[1].Name != "probe.backtrace.source.method.name" || result.Sources[1].Purpose != "probe.backtrace.source.method" || result.Sources[0].Name != "probe.route.source.nexttrace.name" {
		t.Fatalf("sources = %#v", result.Sources)
	}
	wantNotes := []string{"probe.backtrace.note.active_path", "probe.backtrace.note.signature_scope", "probe.backtrace.note.cn2_variant_inference", "probe.backtrace.note.ipv6_targets", "probe.backtrace.note.unidentified", "probe.backtrace.note.parse_failed"}
	if !reflect.DeepEqual(result.Notes, wantNotes) || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.backtrace.summary.values" || !reflect.DeepEqual(result.SummaryMessages[0].Args, []string{"2", "9"}) {
		t.Fatalf("notes/summary = %#v/%#v", result.Notes, result.SummaryMessages)
	}
	if len(result.TextBlocks) != 16 {
		t.Fatalf("text block count = %d, want raw and normalized trace blocks", len(result.TextBlocks))
	}
}

func TestBacktraceProductionCatalogKeysRegisteredInBothLanguages(t *testing.T) {
	keys := []string{
		"module.backtrace.title", "probe.backtrace.description", "probe.backtrace.methodology.engine", "probe.backtrace.profile", "probe.backtrace.comparison_scope",
		"probe.backtrace.summary.tool_missing", "probe.backtrace.summary.no_targets", "probe.backtrace.summary.no_family_targets", "probe.backtrace.summary.values",
		"probe.backtrace.field.engine", "probe.backtrace.field.version", "probe.backtrace.field.adapter", "probe.backtrace.field.arguments", "probe.backtrace.metric.identified",
		"probe.backtrace.table.summary", "probe.backtrace.table.hops", "probe.backtrace.column.provider", "probe.backtrace.column.target", "probe.backtrace.column.line", "probe.backtrace.column.hit_hop", "probe.backtrace.column.hit_ip", "probe.backtrace.column.status", "probe.backtrace.column.reason", "probe.backtrace.column.evidence", "probe.backtrace.column.hop", "probe.backtrace.column.latency", "probe.backtrace.column.ip", "probe.backtrace.column.asn", "probe.backtrace.column.network", "probe.backtrace.column.location",
		backtraceStatusFailed, backtraceStatusIdentified, backtraceStatusUnidentified, "probe.backtrace.hop.responded", "probe.backtrace.hop.no_response", "probe.backtrace.carrier.telecom", "probe.backtrace.carrier.unicom", "probe.backtrace.carrier.mobile", backtraceReasonSignatureMatch, backtraceReasonForeignOnly, backtraceReasonNoKnownSignature, backtraceReasonLimitedOrFiltered, backtraceReasonTraceError, backtraceReasonParseFailed, backtraceReasonNoResponsiveHops,
		backtraceLineTelecomCN2VariantUnknown, backtraceLineTelecomChinanet, backtraceLineUnicomNetworkUnknown, backtraceLineUnicomBackbone, backtraceLineUnicomCUII, backtraceLineUnicom169, backtraceLineMobileCMI, backtraceLineMobileNetworkUnknown, backtraceLineMobileCMNET, backtraceLineTelecomIPv6, backtraceLineUnicomIPv6, backtraceLineMobileCMNETIPv6, backtraceMissingValue,
		"probe.backtrace.evidence.asn", "probe.backtrace.evidence.cidr", "probe.backtrace.evidence.heuristic",
		"probe.backtrace.raw_output", "probe.backtrace.raw_stderr", "probe.backtrace.normalized_trace_json", "probe.route.raw_stderr", "probe.route.normalized_trace_json", "probe.backtrace.source.method.name", "probe.backtrace.source.method", "probe.backtrace.note.tool_missing", "probe.backtrace.note.no_targets", "probe.backtrace.note.no_family_targets", "probe.backtrace.note.parse_failed", "probe.backtrace.note.active_path", "probe.backtrace.note.signature_scope", "probe.backtrace.note.cn2_variant_inference", "probe.backtrace.note.ipv6_targets", "probe.backtrace.note.unidentified",
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		for _, key := range keys {
			if !i18n.Has(language, key) {
				t.Fatalf("%s missing production backtrace key %q", language, key)
			}
		}
		for _, key := range []string{
			"probe.backtrace.line.telecom.cn2", "probe.backtrace.line.telecom.cn2.gia", "probe.backtrace.line.telecom.cn2.gt", "probe.backtrace.line.telecom.163",
			"probe.backtrace.line.unicom.cuii_ipv6", "probe.backtrace.line.unicom.169_ipv6",
		} {
			if i18n.Has(language, key) {
				t.Fatalf("%s retained removed backtrace key %q", language, key)
			}
		}
	}
}

func TestBacktraceProducerReportRendersBilingualWithoutMutation(t *testing.T) {
	if !backtraceFixtureTestApplies(t) {
		t.Skip("NextTrace producer fixture is Linux-only; FreeBSD uses base-system traceroute")
	}
	result := runBacktraceFixtureResult(t)
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "backtrace-fixture", Commit: "backtrace"},
		Run: model.RunInfo{
			ID: "backtrace-test", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), CompletedAt: time.Unix(1, 0).UTC(), DurationMS: 1,
			Exposure: "local", Requested: []string{"backtrace"}, OutputFormats: []string{"json", "md", "html"},
		},
		Summary: model.Summary{Status: result.Status, OK: 1}, Results: []model.Result{result},
	}
	canonicalBefore, err := report.JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	rawSentinels := []string{"外部网", "外部国", "外部城", "用户部分", "用户未知", "用户解析", "用户错误", "用户无响应"}
	want := map[i18n.Lang][]string{
		i18n.LangZH: {"已识别", "未识别", "追踪失败", "命中线路", "仅命中其他", "未命中已知", "路径响应", "路由追踪引擎", "实际 ASN、精确地址范围", "规范化适配器", "命令参数", "路径结果", "已响应", "无响应", "没有响应", "中国电信", "中国联通", "中国移动", "北京电信", "电信 CN2", "变体未知", "实际 ASN", "精确 CIDR", "判定原因", "Backtrace 线路分类规则"},
		i18n.LangEN: {"Identified", "Unidentified", "Trace failed", "Signature match", "Foreign-carrier", "No known line", "Path responses", "Route-tracing engine", "Observed ASN and exact address allocations", "Canonical adapter", "Arguments", "Path result", "Responded", "No response", "No responsive", "China Telecom", "China Unicom", "China Mobile", "Beijing Telecom", "China Telecom CN2", "variant unknown", "Observed ASN", "Exact CIDR", "Decision reason", "Backtrace line classification rules"},
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		textOutput := report.Text(data, report.TextOptions{Width: 240})
		markdownOutput := report.Markdown(data, nil)
		htmlBytes, err := report.HTML(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		for format, output := range map[string]string{"text": textOutput, "markdown": markdownOutput, "html": string(htmlBytes)} {
			for _, marker := range want[language] {
				if !strings.Contains(output, marker) {
					t.Fatalf("%s %s output missing %q:\n%s", language, format, marker, output)
				}
			}
			for _, sentinel := range rawSentinels {
				if !strings.Contains(output, sentinel) {
					t.Fatalf("%s %s output lost raw sentinel %q:\n%s", language, format, sentinel, output)
				}
			}
			if strings.Contains(output, "probe.backtrace.") || strings.Contains(output, "%!") {
				t.Fatalf("%s %s output leaked stable key or format diagnostic:\n%s", language, format, output)
			}
			if language == i18n.LangEN {
				// The text renderer may insert line breaks inside a long JSON/raw
				// sentinel. Remove formatting whitespace before excluding those
				// explicitly external values from the ECS-owned Han check.
				withoutRaw := strings.Map(func(r rune) rune {
					if unicode.IsSpace(r) {
						return -1
					}
					return r
				}, output)
				for _, sentinel := range rawSentinels {
					withoutRaw = strings.ReplaceAll(withoutRaw, sentinel, "")
				}
				if backtraceTestHasHan(withoutRaw) {
					t.Fatalf("English ECS-owned %s output contains Han characters:\n%s", format, output)
				}
			}
		}
		canonicalAfter, err := report.JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(canonicalBefore, canonicalAfter) {
			t.Fatalf("backtrace %s rendering mutated canonical JSON", language)
		}
	}
}

func backtraceTestHasHan(value string) bool {
	for _, runeValue := range value {
		if unicode.Is(unicode.Han, runeValue) {
			return true
		}
	}
	return false
}

func TestBacktraceProducerPreservesLongTypedCommandError(t *testing.T) {
	if !backtraceFixtureTestApplies(t) {
		t.Skip("NextTrace producer fixture is Linux-only; FreeBSD uses base-system traceroute")
	}
	root := filepath.Join(t.TempDir(), strings.Repeat("backtrace-error-path-", 12))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, traceNextTraceEngineName)
	if err := os.WriteFile(path, []byte("#!/backtrace-interpreter-that-does-not-exist\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv(ToolBinEnv, root)
	backend := traceBackend{Name: traceNextTraceEngineName, Path: path, Adapter: traceNextTraceAdapter}
	traceRun := runTraceCommandForFamily(context.Background(), backend, "long", backtraceMaxHops, config.IPVersionAuto)
	if traceRun.Err == nil || len(traceRun.Err.Error()) <= 100 {
		t.Fatalf("fixture error = %v, want a long typed error", traceRun.Err)
	}
	runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = []config.Endpoint{{Name: "long", Address: "long", Kind: config.BacktraceCarrierTelecom}}
	result := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if len(result.Failures) != 1 || result.Failures[0].Stage != "trace" || result.Failures[0].Target != "long" || !strings.HasPrefix(result.Failures[0].Message, traceRun.Err.Error()+"; parser diagnostic:") || result.Failures[0].Category == "" {
		t.Fatalf("long typed failure = %#v, expected original %q", result.Failures, traceRun.Err.Error())
	}
}
