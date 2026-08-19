package probe

import (
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestNextTraceParsesAndClassifiesRepresentativeHop(t *testing.T) {
	output := `{"Hops":[[{"Success":true,"Address":{"IP":"59.43.130.22"},"RTT":3500000,"Geo":{"asnumber":"4809","owner":"China Telecom","country":"CN","prov":"Shanghai"}}],[[]]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 2 {
		t.Fatalf("nexttrace details = %+v, ok=%v", details, ok)
	}
	if details[0].IP != "59.43.130.22" || details[0].ASN != "AS4809" || details[0].Latency != "3.5 ms" || details[0].Network != "China Telecom" || details[0].Location != "CN / Shanghai" || details[0].Status != "已响应" || details[1].Status != "无响应" {
		t.Fatalf("parsed hop = %+v", details[0])
	}

	hits := matchRouteSignatures(extractTraceHops(routeEngineTiny, output))
	best, ok := bestBacktraceHit(hits, "电信")
	if !ok || best.Signature.Code != "CN2" {
		t.Fatalf("classified route = %+v, ok=%v", best, ok)
	}
}

func TestNextTraceParserRejectsEmptyOutput(t *testing.T) {
	details, ok := extractNextTraceDetails(`{"Hops":[]}`)
	if ok || len(details) != 0 {
		t.Fatalf("empty nexttrace output = %+v, ok=%v", details, ok)
	}
}

func TestBacktraceChoosesBestCarrierAndExplainsUnknown(t *testing.T) {
	hits := matchRouteSignatures([]string{"202.97.0.1", "59.43.0.1", "219.158.0.1"})
	best, ok := bestBacktraceHit(hits, "电信")
	if !ok || best.Signature.Code != "CN2" || best.Hop != 2 || describeBacktraceLine(best, hits) != "电信 CN2 · GT（推测）" {
		t.Fatalf("best backtrace hit = %+v, line=%q", best, describeBacktraceLine(best, hits))
	}
	if _, ok := bestBacktraceHit(hits, "教育"); ok {
		t.Fatal("foreign route was accepted for an unrelated carrier")
	}
	foreign := unidentifiedBacktraceStatus(backtraceRow{Target: config.Endpoint{Kind: "电信"}, Hits: []backtraceHit{{Signature: routeSignature{Carrier: "移动"}}}, Hops: []string{"223.120.0.1"}})
	if !strings.Contains(foreign, "异网骨干") || !strings.Contains(foreign, "移动") {
		t.Fatalf("foreign backtrace status = %q", foreign)
	}
	unknown := unidentifiedBacktraceStatus(backtraceRow{Target: config.Endpoint{Kind: "电信"}, Hops: []string{"198.51.100.1"}})
	if !strings.Contains(unknown, "1 跳无已知特征") {
		t.Fatalf("unknown backtrace status = %q", unknown)
	}
	missing := unidentifiedBacktraceStatus(backtraceRow{Target: config.Endpoint{Kind: "电信"}, Hops: []string{"", ""}})
	if !strings.Contains(missing, "可能被限速") {
		t.Fatalf("missing backtrace status = %q", missing)
	}
}
