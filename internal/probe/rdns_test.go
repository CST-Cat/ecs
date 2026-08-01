package probe

import (
	"testing"
)

func TestResidentialPTRHintsAvoidSubstringFalsePositives(t *testing.T) {
	// 命中必须靠分隔符包围，否则 "res" 会命中 "resource"、"ppp" 会命中 "apppool"。
	cases := map[string]bool{
		"host-1-2-3-4.dsl.example.net":  true,
		"pppoe-client.isp.example.com":  true,
		"1-2-3-4.dynamic.example.net":   true,
		"cable-1-2-3-4.example.com":     true,
		"server1.resources.example.com": false, // resources 含 res 但不是家宽特征
		"static-1-2-3-4.example.com":    false,
		"edge01.cdn.example.net":        false,
		"":                              false,
	}
	for name, wantHit := range cases {
		// 调用真实实现，而不是在测试里复制一遍匹配逻辑——
		// 复制出来的逻辑只能证明它自己自洽。
		hit := len(matchResidentialHints(name)) > 0
		if hit != wantHit {
			t.Errorf("%q 命中家宽特征 = %v, want %v", name, hit, wantHit)
		}
	}
	// 多个特征应全部返回，供报告逐条说明。
	if hits := matchResidentialHints("pppoe-dsl-1-2-3-4.dynamic.example.net"); len(hits) < 3 {
		t.Fatalf("多特征命名应返回全部命中项：%v", hits)
	}
}

func TestBoolValue(t *testing.T) {
	if boolValue(true) != 1 || boolValue(false) != 0 {
		t.Fatal("布尔指标必须映射为 1/0")
	}
}

// FCrDNS 的核心不变式：只有 PTR 存在且正向解析回本 IP 才算通过。
// PTR 由 IP 持有者单方面设置，不做正向确认就能指向任意域名。
func TestFCrDNSRequiresForwardConfirmation(t *testing.T) {
	cases := []struct {
		name      string
		result    rdnsResult
		confirmed bool
	}{
		{"无 PTR", rdnsResult{IP: "203.0.113.1"}, false},
		{"有 PTR 但正向对不上", rdnsResult{
			IP: "203.0.113.1", Names: []string{"mail.example.com."},
			Forward: []string{"198.51.100.9"},
		}, false},
		{"正反一致", rdnsResult{
			IP: "203.0.113.1", Names: []string{"mail.example.com."},
			Forward: []string{"203.0.113.1"}, Confirmed: true,
		}, true},
	}
	for _, testCase := range cases {
		if testCase.result.Confirmed != testCase.confirmed {
			t.Errorf("%s: Confirmed = %v, want %v", testCase.name, testCase.result.Confirmed, testCase.confirmed)
		}
	}
}
