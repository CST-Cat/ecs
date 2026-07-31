package probe

import (
	"strings"
	"testing"
)

func TestExtractTraceHopsFromTracerouteText(t *testing.T) {
	output := strings.Join([]string{
		" 1  10.0.0.1  0.512 ms",
		" 2  * * *",
		" 3  202.97.94.1  32.118 ms",
		" 4  59.43.130.22  35.900 ms",
		"lines without a hop number are ignored",
	}, "\n")
	hops := extractTraceHops("traceroute", output)
	want := []string{"10.0.0.1", "", "202.97.94.1", "59.43.130.22"}
	if len(hops) != len(want) {
		t.Fatalf("hops = %v, want %v", hops, want)
	}
	for index, address := range want {
		if hops[index] != address {
			t.Fatalf("hop %d = %q, want %q", index+1, hops[index], address)
		}
	}
}

func TestExtractTraceHopsFromNextTraceJSON(t *testing.T) {
	output := `{"Hops":[[{"Address":"10.0.0.1"}],[{"Address":""}],[{"Address":"219.158.16.1"}]]}`
	hops := extractTraceHops("nexttrace", output)
	if len(hops) != 3 || hops[2] != "219.158.16.1" {
		t.Fatalf("nexttrace hops = %v", hops)
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

func TestBestBacktraceHitPrefersHigherQualityLine(t *testing.T) {
	// 同一条路径同时经过 163 与 CN2 时，结论应当取更优质的 CN2。
	hits := matchRouteSignatures([]string{"202.97.94.1", "59.43.130.22"})
	best, ok := bestBacktraceHit(hits)
	if !ok || best.Signature.Code != "CN2" {
		t.Fatalf("best = %+v, ok = %v", best, ok)
	}

	if _, ok := bestBacktraceHit(nil); ok {
		t.Fatal("empty hit list must not produce a verdict")
	}
}

func TestDescribeBacktraceLineMarksCN2Inference(t *testing.T) {
	// 直接进 CN2 推测为 GIA。
	direct := matchRouteSignatures([]string{"59.43.130.22", "59.43.80.1"})
	best, _ := bestBacktraceHit(direct)
	if got := describeBacktraceLine(best, direct); !strings.Contains(got, "GIA") || !strings.Contains(got, "推测") {
		t.Fatalf("direct CN2 line = %q", got)
	}

	// 先经 163 再进 CN2 推测为 GT。
	viaBackbone := matchRouteSignatures([]string{"202.97.94.1", "59.43.130.22"})
	best, _ = bestBacktraceHit(viaBackbone)
	if got := describeBacktraceLine(best, viaBackbone); !strings.Contains(got, "GT") || !strings.Contains(got, "推测") {
		t.Fatalf("via-163 CN2 line = %q", got)
	}

	// 非 CN2 线路不应带推测后缀。
	unicom := matchRouteSignatures([]string{"219.158.16.1"})
	best, _ = bestBacktraceHit(unicom)
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
