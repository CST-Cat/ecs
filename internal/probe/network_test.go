package probe

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestIPAPIResponseNormalizationAndNetworkHelpers(t *testing.T) {
	var flat ipAPIResponse
	if err := json.Unmarshal([]byte(`{"ip":"203.0.113.9","is_datacenter":true,"is_proxy":false,"is_tor":false,"company_name":"Fixture Hosting","asn_num":64500,"asn_org":"FIXTURE-AS","cc":"US"}`), &flat); err != nil {
		t.Fatal(err)
	}
	flat = normalizeIPAPIResponse(flat)
	if flat.ASN.ASN != 64500 || flat.ASN.Organization != "FIXTURE-AS" || flat.Company.Name != "Fixture Hosting" || flat.Company.Type != "hosting" || flat.Location.CountryCode != "US" || !flat.BooleanPresence.IsProxy || flat.IsProxy {
		t.Fatalf("flat IP API normalization = %+v", flat)
	}
	if got := ipAPIBooleanText(flat.IsProxy, flat.BooleanPresence.IsProxy); got != "否" || ipAPIBooleanText(flat.IsDatacenter, flat.BooleanPresence.IsDatacenter) != "是" {
		t.Fatalf("explicit false boolean = %q", got)
	}
	var nested ipAPIResponse
	if err := json.Unmarshal([]byte(`{"ip":"198.51.100.7","is_datacenter":false,"asn":{"asn":64501,"org":"Nested AS","type":"isp"},"company":{"name":"Nested Co","type":"business"},"location":{"country_code":"CA"}}`), &nested); err != nil {
		t.Fatal(err)
	}
	nested = normalizeIPAPIResponse(nested)
	if nested.ASN.ASN != 64501 || nested.ASN.Organization != "Nested AS" || nested.Company.Name != "Nested Co" || nested.Company.Type != "business" || nested.Location.CountryCode != "CA" {
		t.Fatalf("nested IP API normalization overwrote fields = %+v", nested)
	}
	if nested.BooleanPresence.IsDatacenter != true || ipAPIBooleanText(nested.IsVPN, nested.BooleanPresence.IsVPN) != "未返回" {
		t.Fatal("boolean presence did not distinguish explicit false from missing")
	}
	flat.Company.AbuserScore = "0.42 (Low)"
	finding := findingFromIPAPI(flat, 5*time.Millisecond)
	if finding.Country != "US" || finding.Score == nil || *finding.Score != 42 || finding.Risk != "低" || !finding.Server.Known || !finding.Server.Value || !finding.Proxy.Known || finding.Proxy.Value {
		t.Fatalf("IP API finding = %+v", finding)
	}
	flat.Company.AbuserScore = ""
	flat.ASN.AbuserScore = "0.8 (High)"
	fallbackFinding := findingFromIPAPI(flat, 0)
	if fallbackFinding.Score == nil || *fallbackFinding.Score != 80 || fallbackFinding.ScoreKind != "ASN 滥用概率" {
		t.Fatalf("ASN score fallback = %+v", fallbackFinding)
	}

	if enabledIPQualitySourceCount([]string{"ipapi", "ipqs"}) != 2 || enabledIPQualitySourceCount([]string{"none"}) != 0 || !qualitySourceEnabled([]string{"all"}, "dbip") || qualitySourceEnabled([]string{"none", "ipapi"}, "ipapi") {
		t.Fatal("IP quality source selection failed")
	}
	if normalizeNetworkType("hosting") != "机房" || normalizeNetworkType("new provider") != "其他" || normalizeNetworkType("") != "" || normalizeIP2LocationType("DCH/ISP") != "机房" {
		t.Fatal("network type normalization failed")
	}
	if formatASNWithOrganization(64500, "Fixture") != "AS64500 Fixture" || formatASNWithOrganization(0, "") != "unknown" {
		t.Fatal("ASN formatting failed")
	}
	lookup := ipLookup{HasIntel: true}
	if unavailableIPField(lookup, "unknown") != "unknown" || unavailableIPField(ipLookup{IntelAttempted: true}, "unknown") != "未查询（ipapi 不可用）" || unavailableIPField(ipLookup{}, "unknown") != "未查询（未启用 ipapi）" {
		t.Fatal("unavailable IP field states failed")
	}
	bundle := ipQualityBundle{Origin: originAssessment{UsageCountry: "US", RegisteredCountry: "CA"}, Findings: map[string]qualityFinding{"ipinfo": {Country: "DE", Enabled: true}}}
	if bundleCountry(bundle) != "US" {
		t.Fatal("origin country was not preferred")
	}
	bundle.Origin.Err = errors.New("fixture")
	if bundleCountry(bundle) != "DE" {
		t.Fatal("provider country fallback failed")
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	if proxyEnvironmentEnabled() {
		t.Fatal("empty proxy environment reported enabled")
	}
	t.Setenv("HTTPS_PROXY", "http://fixture.invalid")
	if !proxyEnvironmentEnabled() {
		t.Fatal("proxy environment was not detected")
	}
}
