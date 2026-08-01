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

func TestNewSourcesAreRegisteredEverywhere(t *testing.T) {
	// 新增数据源必须同时进入顺序表与标签表，否则表格里会出现空名或整行消失。
	for _, id := range []string{"ipapicom", "ipsb"} {
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
	// 纯密钥源不应重新出现：没有免密兜底就等于对绝大多数用户永远失败。
	for _, id := range []string{"virustotal", "ipgeolocation", "bigdatacloud", "getipintel"} {
		if qualitySourceLabels[id] != "" {
			t.Errorf("%q 是纯密钥源，已移除，不应重新登记", id)
		}
	}
}
