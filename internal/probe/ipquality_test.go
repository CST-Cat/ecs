package probe

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseProbabilityScore(t *testing.T) {
	tests := []struct {
		input string
		want  float64
		ok    bool
	}{
		{"0.0047 (Very Low)", 0.47, true},
		{"12.5%", 12.5, true},
		{"1.2 (invalid)", 0, false},
		{"", 0, false},
	}
	for _, test := range tests {
		value := parseProbabilityScore(test.input)
		if !test.ok {
			if value != nil {
				t.Fatalf("parseProbabilityScore(%q) = %v, want nil", test.input, *value)
			}
			continue
		}
		if value == nil || math.Abs(*value-test.want) > 0.0001 {
			t.Fatalf("parseProbabilityScore(%q) = %v, want %v", test.input, value, test.want)
		}
	}
}

func TestProviderSpecificRiskBands(t *testing.T) {
	score := func(value float64) *float64 { return &value }
	if got := riskIP2Location(score(33)); got != "中等" {
		t.Fatalf("IP2Location 33 = %q", got)
	}
	if got := riskScamalytics(score(90)); got != "极高" {
		t.Fatalf("Scamalytics 90 = %q", got)
	}
	if got := riskAbuseIPDB(score(75)); got != "高" {
		t.Fatalf("AbuseIPDB 75 = %q", got)
	}
	if got := riskIPQS(score(85)); got != "高" {
		t.Fatalf("IPQS 85 = %q", got)
	}
}

func TestRequiredQualityRowsAreNeverSilentlyDropped(t *testing.T) {
	bundle := ipQualityBundle{
		Version:  "4",
		Findings: map[string]qualityFinding{},
	}
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		bundle.Findings[id] = qualityFinding{
			ID:      id,
			Name:    qualitySourceLabels[id],
			Enabled: false,
		}
	}
	typeTable := bundle.typeTable()
	if len(typeTable.Rows) != len(typeSourceOrder) {
		t.Fatalf("type rows = %d, want %d", len(typeTable.Rows), len(typeSourceOrder))
	}
	scoreTable := bundle.scoreTable()
	if len(scoreTable.Rows) != len(scoreSourceOrder) {
		t.Fatalf("score rows = %d, want %d", len(scoreTable.Rows), len(scoreSourceOrder))
	}
	factorTable := bundle.factorTable()
	if len(factorTable.Columns) != len(factorSourceOrder)+1 || len(factorTable.Rows) != 7 {
		t.Fatalf("factor table = %d columns, %d rows", len(factorTable.Columns), len(factorTable.Rows))
	}
	for _, row := range scoreTable.Rows {
		if len(row) < 2 || row[1] != "未启用" {
			t.Fatalf("disabled score row = %v", row)
		}
	}
}

func TestDBIPMappingIsExplicitlyMarkedDerived(t *testing.T) {
	finding := newFinding("dbip")
	setDBIPRisk(&finding, "medium")
	bundle := ipQualityBundle{
		Version:  "4",
		Findings: map[string]qualityFinding{"dbip": finding},
	}
	for _, id := range scoreSourceOrder {
		if _, ok := bundle.Findings[id]; !ok {
			bundle.Findings[id] = qualityFinding{ID: id, Enabled: false}
		}
	}
	table := bundle.scoreTable()
	var dbipRow []string
	for _, row := range table.Rows {
		if row[0] == "DB-IP" {
			dbipRow = row
			break
		}
	}
	if len(dbipRow) == 0 || !strings.Contains(dbipRow[1], "*") || !strings.Contains(dbipRow[4], "映射") {
		t.Fatalf("DB-IP row does not disclose mapping: %v", dbipRow)
	}
}

func TestNetworkTypeNormalization(t *testing.T) {
	tests := map[string]string{
		"isp":                             "家宽",
		"hosting":                         "机房",
		"Data Center/Web Hosting/Transit": "机房",
		"business":                        "商业",
		"unknown-new-value":               "其他",
	}
	for input, want := range tests {
		if got := normalizeNetworkType(input); got != want {
			t.Fatalf("normalizeNetworkType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQualitySourceSelection(t *testing.T) {
	if !qualitySourceEnabled([]string{"all"}, "ipqs") {
		t.Fatal("all should enable IPQS")
	}
	if qualitySourceEnabled([]string{"none"}, "ipqs") {
		t.Fatal("none should disable IPQS")
	}
	if !qualitySourceEnabled([]string{"ipapi", "dbip"}, "dbip") {
		t.Fatal("explicit DB-IP should be enabled")
	}
}

func TestFindingEvidenceDoesNotTreatZeroValueStructAsSuccess(t *testing.T) {
	if findingHasEvidence(qualityFinding{}) {
		t.Fatal("empty provider response must not count as evidence")
	}
	if !findingHasEvidence(qualityFinding{Country: "US"}) {
		t.Fatal("country should count as evidence")
	}
	if !findingHasEvidence(qualityFinding{Proxy: knownSignal(false)}) {
		t.Fatal("an explicit false signal should count as evidence")
	}
}

func TestProviderRequestRejectsCrossHostRedirectAndDoesNotLeakSecret(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirected.Store(true)
		writer.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	_, _, err := requestBytes(
		context.Background(),
		source.Client(),
		"ecs/test",
		source.URL+"?key=super-secret",
		map[string]string{"Authorization": "Bearer super-secret"},
		1024,
	)
	if err == nil {
		t.Fatal("expected cross-host redirect error")
	}
	if redirected.Load() {
		t.Fatal("redirect target was contacted")
	}
	if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), source.URL) {
		t.Fatalf("error leaked request details: %v", err)
	}
}

func TestProviderRequestAllowsOnlyExplicitKeylessRedirectHost(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirected.Store(true)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"success":true}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}

	body, _, err := requestBytesAllowingRedirectHosts(
		context.Background(),
		source.Client(),
		"ecs/test",
		source.URL,
		nil,
		1024,
		[]string{targetURL.Hostname()},
	)
	if err != nil {
		t.Fatalf("explicitly allowed redirect failed: %v", err)
	}
	if !redirected.Load() || !strings.Contains(string(body), `"success":true`) {
		t.Fatalf("allowed redirect did not return target response: %q", body)
	}
}

func TestParseIPQSPublicPage(t *testing.T) {
	const ip = "203.0.113.8"
	page := []byte(`
# Proxy Detection Test for 203.0.113.8

### **203.0.113.8** (example.test) is an IP address located in **Example City**, **Example Region**, **US** that is assigned to **Example ISP**.

| Country | City | Region | VPN | PROXY |
| --- | --- | --- | --- | --- |
| United States | Example City | Example Region | Yes | Yes |
| ISP | Organization | Hostname | ASN | TOR |
| --- | --- | --- | --- | --- |
| Example ISP | Example ISP | example.test | AS64500 | No |

### Risk Summary

This IP address (203.0.113.8) is a **proxy** connection. IPQS proxy detection scoring has identified 203.0.113.8 as a **VPN** connection. IPQS fraud scoring algorithms have rated this IP address as **high risk**, scoring 93 out of 100. IPQS has recently detected abusive behavior from this connection.
`)
	finding := parseIPQSPublicPage(page, ip)
	if finding.Err != nil {
		t.Fatalf("parseIPQSPublicPage error: %v", finding.Err)
	}
	if finding.Score == nil || *finding.Score != 93 || finding.Risk != "极高" {
		t.Fatalf("score/risk = %v/%q", finding.Score, finding.Risk)
	}
	if finding.Country != "US" {
		t.Fatalf("country = %q", finding.Country)
	}
	if !finding.Proxy.Known || !finding.Proxy.Value ||
		!finding.VPN.Known || !finding.VPN.Value ||
		!finding.Tor.Known || finding.Tor.Value ||
		!finding.Abuser.Known || !finding.Abuser.Value {
		t.Fatalf("signals = proxy=%+v vpn=%+v tor=%+v abuser=%+v", finding.Proxy, finding.VPN, finding.Tor, finding.Abuser)
	}
}

func TestParseIPQSPublicPageRejectsQuotaAndWrongTarget(t *testing.T) {
	if finding := parseIPQSPublicPage([]byte("You've reached your daily max lookups! 203.0.113.8"), "203.0.113.8"); finding.Err == nil {
		t.Fatal("quota page must not be treated as provider evidence")
	}
	if finding := parseIPQSPublicPage([]byte("scoring 10 out of 100 for 203.0.113.9"), "203.0.113.8"); finding.Err == nil {
		t.Fatal("page for a different IP must not be accepted")
	}
}

func TestParseDBIPFreeJSONIsHonestPartialEvidence(t *testing.T) {
	finding := parseDBIPFreeJSON([]byte(`{"ipAddress":"203.0.113.8","countryCode":"US"}`))
	if finding.Err != nil || finding.Country != "US" {
		t.Fatalf("free response = %+v", finding)
	}
	if finding.Score != nil {
		t.Fatalf("free DB-IP response must not invent a threat score: %v", *finding.Score)
	}
}

func TestOfficialKeylessProviderParsers(t *testing.T) {
	ipregistry := parseIPregistryJSON([]byte(`{
		"location":{"country":{"code":"US"}},
		"connection":{"type":"hosting"},
		"company":{"type":"business"},
		"security":{"is_proxy":false,"is_tor":false,"is_vpn":false,"is_cloud_provider":true}
	}`))
	if ipregistry.Err != nil || ipregistry.Country != "US" || ipregistry.Usage != "机房" ||
		!ipregistry.Proxy.Known || ipregistry.Proxy.Value ||
		!ipregistry.Server.Known || !ipregistry.Server.Value {
		t.Fatalf("ipregistry tryout parse = %+v", ipregistry)
	}

	ip2location := parseIP2LocationJSON([]byte(`{
		"country_code":"US",
		"is_proxy":false
	}`))
	if ip2location.Err != nil || ip2location.Country != "US" ||
		!ip2location.Proxy.Known || ip2location.Proxy.Value {
		t.Fatalf("IP2Location keyless parse = %+v", ip2location)
	}
	if ip2location.Score != nil || !strings.Contains(ip2location.Partial, "欺诈分") {
		t.Fatalf("IP2Location keyless response must disclose missing paid score: %+v", ip2location)
	}
}
