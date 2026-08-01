package probe

import (
	"strings"
	"testing"
)

// 样本取自 ip-api.com 对本机出口的真实响应。
func TestParseIPAPIComFields(t *testing.T) {
	finding := newFinding("ipapicom")
	var response struct {
		Status      string `json:"status"`
		CountryCode string `json:"countryCode"`
		Mobile      *bool  `json:"mobile"`
		Proxy       *bool  `json:"proxy"`
		Hosting     *bool  `json:"hosting"`
	}
	if err := decodeJSON([]byte(`{"status":"success","countryCode":"US","mobile":false,"proxy":true,"hosting":true}`), &response); err != nil {
		t.Fatal(err)
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Proxy = pointerSignal(response.Proxy)
	finding.Server = pointerSignal(response.Hosting)
	if finding.Country != "US" {
		t.Fatalf("国家 = %q", finding.Country)
	}
	// proxy/hosting 是 ip-api 免密版最有价值的两个字段。
	if !finding.Proxy.Known || !finding.Proxy.Value {
		t.Fatalf("proxy 信号 = %+v", finding.Proxy)
	}
	if !finding.Server.Known || !finding.Server.Value {
		t.Fatalf("hosting 信号 = %+v", finding.Server)
	}
	// 字段缺失时必须是"未知"而不是 false——false 会被当成"确认不是代理"。
	var empty struct {
		Proxy *bool `json:"proxy"`
	}
	if err := decodeJSON([]byte(`{}`), &empty); err != nil {
		t.Fatal(err)
	}
	if pointerSignal(empty.Proxy).Known {
		t.Fatal("字段缺失必须表示为未知，不能当成 false")
	}
}

func TestRiskVirusTotalUsesVoteSemantics(t *testing.T) {
	score := func(v float64) *float64 { return &v }
	// VirusTotal 是厂商投票占比，语义与欺诈分完全不同：
	// 有一两家标记就值得留意，阈值必须远低于欺诈分类的库。
	cases := map[float64]string{0: "低", 1.5: "需留意", 5: "高", 25: "极高"}
	for value, want := range cases {
		if got := riskVirusTotal(score(value)); got != want {
			t.Errorf("riskVirusTotal(%g) = %q, want %q", value, got, want)
		}
	}
	if riskVirusTotal(nil) != "" {
		t.Fatal("无分值时不应给出等级")
	}
	// 同样是 5 分，在欺诈分口径下应当是"低"，在 VT 口径下是"高"——
	// 这正是不能把不同来源的分值合并的原因。
	if riskIPQS(score(5)) == riskVirusTotal(score(5)) {
		t.Fatal("不同口径的同一数值不应得到相同等级")
	}
}

func TestRiskGenericHundred(t *testing.T) {
	score := func(v float64) *float64 { return &v }
	for value, want := range map[float64]string{0: "低", 30: "中等", 60: "高", 90: "极高"} {
		if got := riskGenericHundred(score(value)); got != want {
			t.Errorf("riskGenericHundred(%g) = %q, want %q", value, got, want)
		}
	}
}

func TestNewSourcesAreRegisteredEverywhere(t *testing.T) {
	// 新增数据源必须同时进入顺序表与标签表，否则表格里会出现空名或整行消失。
	for _, id := range []string{"ipapicom", "ipsb", "virustotal", "ipgeolocation", "bigdatacloud", "getipintel"} {
		found := false
		for _, existing := range qualitySourceOrder {
			if existing == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q 未加入 qualitySourceOrder", id)
		}
		if qualitySourceLabels[id] == "" {
			t.Errorf("%q 缺少显示名", id)
		}
	}
	// 有分值的源必须出现在评分表里，并且有分段规则说明。
	for _, id := range []string{"virustotal", "ipgeolocation", "getipintel"} {
		if scoreBands(id) == "—" {
			t.Errorf("%q 缺少分段规则说明", id)
		}
	}
}

func TestCompactMessage(t *testing.T) {
	if got := compactMessage("  "); got != "查询未成功" {
		t.Fatalf("空消息 = %q", got)
	}
	long := strings.Repeat("x", 300)
	if got := compactMessage(long); len(got) > 130 {
		t.Fatalf("长消息未截断：%d", len(got))
	}
}
