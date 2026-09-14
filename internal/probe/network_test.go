package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
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
	if got := ipAPIBooleanText(flat.IsProxy, flat.BooleanPresence.IsProxy); got != "probe.network.boolean.no" || ipAPIBooleanText(flat.IsDatacenter, flat.BooleanPresence.IsDatacenter) != "probe.network.boolean.yes" {
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
	if nested.BooleanPresence.IsDatacenter != true || ipAPIBooleanText(nested.IsVPN, nested.BooleanPresence.IsVPN) != "probe.network.boolean.unknown" {
		t.Fatal("boolean presence did not distinguish explicit false from missing")
	}
	flat.Company.AbuserScore = "0.42 (Low)"
	finding := findingFromIPAPI(flat, 5*time.Millisecond)
	if finding.Country != "US" || finding.Score == nil || *finding.Score != 42 || finding.Risk != "probe.network.risk.low" || !finding.Server.Known || !finding.Server.Value || !finding.Proxy.Known || finding.Proxy.Value {
		t.Fatalf("IP API finding = %+v", finding)
	}
	flat.Company.AbuserScore = ""
	flat.ASN.AbuserScore = "0.8 (High)"
	fallbackFinding := findingFromIPAPI(flat, 0)
	if fallbackFinding.Score == nil || *fallbackFinding.Score != 80 || fallbackFinding.ScoreKind != networkScoreKindASNAbuse {
		t.Fatalf("ASN score fallback = %+v", fallbackFinding)
	}

	if enabledIPQualitySourceCount([]string{"ipapi", "ipqs"}) != 2 || enabledIPQualitySourceCount([]string{"none"}) != 0 || !qualitySourceEnabled([]string{"all"}, "dbip") || qualitySourceEnabled([]string{"none", "ipapi"}, "ipapi") {
		t.Fatal("IP quality source selection failed")
	}
	if normalizeNetworkType("hosting") != "probe.network.network_type.datacenter" || normalizeNetworkType("new provider") != "probe.network.network_type.other" || normalizeNetworkType("") != "" || normalizeIP2LocationType("DCH/ISP") != "probe.network.network_type.datacenter" {
		t.Fatal("network type normalization failed")
	}
	if formatASNWithOrganization(64500, "Fixture") != "AS64500 Fixture" || formatASNWithOrganization(0, "") != networkMissingValue {
		t.Fatal("ASN formatting failed")
	}
	lookup := ipLookup{HasIntel: true}
	if unavailableIPField(lookup, "unknown") != networkMissingValue || unavailableIPField(ipLookup{IntelAttempted: true}, "unknown") != "probe.network.value.intel_unavailable" || unavailableIPField(ipLookup{}, "unknown") != "probe.network.value.intel_not_attempted" {
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

func TestIPVersionHTTPClientUsesDirectRequestedFamily(t *testing.T) {
	v4Listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4Listener.Close()
	v6Listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v6Listener.Close()

	timeout := 200 * time.Millisecond
	for _, test := range []struct {
		name, version string
		listener      net.Listener
		other         net.Listener
	}{
		{name: "IPv4", version: "4", listener: v4Listener, other: v6Listener},
		{name: "IPv6", version: "6", listener: v6Listener, other: v4Listener},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newIPVersionHTTPClient(timeout, test.version)
			defer client.CloseIdleConnections()
			transport, ok := client.Transport.(*http.Transport)
			if !ok || transport.Proxy != nil || client.Timeout != timeout || transport.TLSHandshakeTimeout != timeout {
				t.Fatalf("%s client transport = %#v, timeout=%s", test.name, client.Transport, client.Timeout)
			}
			connection, err := transport.DialContext(context.Background(), "tcp", test.listener.Addr().String())
			if err != nil {
				t.Fatalf("%s requested-family dial = %v", test.name, err)
			}
			accepted, err := test.listener.Accept()
			if err != nil {
				connection.Close()
				t.Fatal(err)
			}
			connection.Close()
			accepted.Close()
			if connection, err := transport.DialContext(context.Background(), "tcp", test.other.Addr().String()); err == nil {
				connection.Close()
				t.Fatalf("%s client connected to the opposite family", test.name)
			}
		})
	}
}
