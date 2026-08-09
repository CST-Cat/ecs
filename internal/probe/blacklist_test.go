package probe

import (
	"net"
	"strings"
	"testing"
)

func TestReverseIPv4(t *testing.T) {
	prefix, ok := reverseIPv4(net.ParseIP("203.0.113.45"))
	if !ok || prefix != "45.113.0.203" {
		t.Fatalf("reverseIPv4 = %q, %v", prefix, ok)
	}
	// DNSBL 规范约定的测试地址。
	if prefix, _ := reverseIPv4(net.ParseIP("127.0.0.2")); prefix != "2.0.0.127" {
		t.Fatalf("测试地址反转 = %q", prefix)
	}
	// 绝大多数 DNSBL 不支持 IPv6，必须明确拒绝而不是拼出无意义的查询。
	if _, ok := reverseIPv4(net.ParseIP("2001:db8::1")); ok {
		t.Fatal("IPv6 不应被接受")
	}
	if _, ok := reverseIPv4(nil); ok {
		t.Fatal("nil 不应被接受")
	}
}

// 拒绝码绝不能被当成命中——这是本模块最容易出的误报。
//
// 实测：经 1.1.1.1 查询 zen.spamhaus.org 会返回 127.255.255.254，
// 那是"拒绝来自公共解析器的查询"，不是"该 IP 已被收录"。
// 若按"有 127.x 应答即命中"实现，所有用 Cloudflare DNS 的机器都会被误报进 Spamhaus。
func TestClassifyDNSBLCodesRejectsRefusalCodes(t *testing.T) {
	cases := []struct {
		name      string
		addresses []string
		want      dnsblOutcome
	}{
		{"无记录即未收录", nil, dnsblClean},
		{"空列表即未收录", []string{}, dnsblClean},
		{"标准命中码", []string{"127.0.0.2"}, dnsblListed},
		{"多个命中码", []string{"127.0.0.2", "127.0.0.4"}, dnsblListed},
		{"DroneBL 的 127.0.0.1 也是命中", []string{"127.0.0.1"}, dnsblListed},
		{"Spamhaus 公共解析器拒绝码", []string{"127.255.255.254"}, dnsblRefused},
		{"Spamhaus 配额超限码", []string{"127.255.255.255"}, dnsblRefused},
		{"Spamhaus 格式错误码", []string{"127.255.255.252"}, dnsblRefused},
		{"全部为拒绝码", []string{"127.255.255.254", "127.255.255.255"}, dnsblRefused},
	}
	for _, testCase := range cases {
		got, detail := classifyDNSBLCodes(testCase.addresses)
		if got != testCase.want {
			t.Errorf("%s: classifyDNSBLCodes(%v) = %q, want %q",
				testCase.name, testCase.addresses, got, testCase.want)
		}
		if got == dnsblRefused && detail == "" {
			t.Errorf("%s: 拒绝码必须附带说明，否则读者会以为是命中", testCase.name)
		}
	}

	// 混合情况：只要有一个真实命中码，就应判为命中而不是被拒。
	if got, _ := classifyDNSBLCodes([]string{"127.255.255.254", "127.0.0.2"}); got != dnsblListed {
		t.Fatalf("混合应答含真实命中码时 = %q, want %q", got, dnsblListed)
	}
}

func TestDNSBLZoneListIsWellFormed(t *testing.T) {
	zones := dnsblZones()
	if len(zones) < 10 {
		t.Fatalf("黑名单清单过短：%d", len(zones))
	}
	seen := make(map[string]bool)
	for _, zone := range zones {
		if zone.Zone == "" || zone.Name == "" || zone.Purpose == "" {
			t.Fatalf("清单项缺少字段：%+v", zone)
		}
		if seen[zone.Zone] {
			t.Fatalf("黑名单区域重复：%s", zone.Zone)
		}
		seen[zone.Zone] = true
		if strings.HasPrefix(zone.Zone, ".") || strings.HasSuffix(zone.Zone, ".") {
			t.Fatalf("区域名格式错误：%q", zone.Zone)
		}
	}

	// 实测淘汰的区域不得回到清单里：SORBS 已停服，hostkarma 是 karma 系统
	// （对干净地址也返回记录），收进来必然误报。
	for _, banned := range []string{
		"dnsbl.sorbs.net", "spam.dnsbl.sorbs.net",
		"hostkarma.junkemailfilter.com", "db.wpbl.info", "ubl.unsubscore.com",
	} {
		if seen[banned] {
			t.Errorf("%s 已被实测淘汰，不应重新收录", banned)
		}
	}
}

func TestDNSBLRowRankPutsHitsFirst(t *testing.T) {
	// 命中必须排在最前：那是读者最需要立刻看到的一行。
	if dnsblRowRank(string(dnsblListed)) >= dnsblRowRank(string(dnsblRefused)) {
		t.Fatal("命中应排在被拒之前")
	}
	if dnsblRowRank(string(dnsblRefused)) >= dnsblRowRank(string(dnsblFailed)) {
		t.Fatal("被拒应排在失败之前")
	}
	if dnsblRowRank(string(dnsblFailed)) >= dnsblRowRank(string(dnsblClean)) {
		t.Fatal("失败应排在干净之前")
	}
}

func TestDNSBLCountMeasurementsKeepAllOutcomesSeparate(t *testing.T) {
	measurements := dnsblCountMeasurements(2, 11, 3, 1, 17)
	want := map[string]float64{
		"dnsbl_listed_count":  2,
		"dnsbl_clean_count":   11,
		"dnsbl_refused_count": 3,
		"dnsbl_failed_count":  1,
	}
	if len(measurements) != len(want) {
		t.Fatalf("DNSBL measurements = %+v", measurements)
	}
	for _, measurement := range measurements {
		value, ok := want[measurement.Key]
		if !ok || measurement.Value != value || measurement.Display == "" || measurement.Method != "dnsbl-a-lookup-v1" {
			t.Errorf("DNSBL outcome measurement = %+v", measurement)
		}
	}
}
