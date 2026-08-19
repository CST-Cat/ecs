package probe

import "testing"

func TestNextTraceParsesAndClassifiesRepresentativeHop(t *testing.T) {
	output := `{"Hops":[[{"Success":true,"Address":{"IP":"59.43.130.22"},"RTT":3500000,"Geo":{"asnumber":"4809","owner":"China Telecom","country":"CN","prov":"Shanghai"}}]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 1 {
		t.Fatalf("nexttrace details = %+v, ok=%v", details, ok)
	}
	if details[0].IP != "59.43.130.22" || details[0].ASN != "AS4809" {
		t.Fatalf("parsed hop = %+v", details[0])
	}

	hits := matchRouteSignatures([]string{details[0].IP})
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
