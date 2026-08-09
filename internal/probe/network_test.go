package probe

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestNormalizeIPAPIResponseSupportsCurrentFlatSchema(t *testing.T) {
	var data ipAPIResponse
	if err := json.Unmarshal([]byte(`{
		"ip":"64.23.222.10",
		"is_datacenter":true,
		"company_name":"DigitalOcean, LLC",
		"asn_num":14061,
		"asn_org":"DIGITALOCEAN-ASN",
		"cc":"US"
	}`), &data); err != nil {
		t.Fatal(err)
	}
	data = normalizeIPAPIResponse(data)
	if data.IP != "64.23.222.10" || data.ASN.ASN != 14061 || data.ASN.Organization != "DIGITALOCEAN-ASN" {
		t.Fatalf("flat ASN was not normalized: %+v", data)
	}
	if data.Company.Name != "DigitalOcean, LLC" || data.Company.Type != "hosting" || data.Location.CountryCode != "US" || data.Location.Country != "US" {
		t.Fatalf("flat company/location was not normalized: %+v", data)
	}
	finding := findingFromIPAPI(data, 0)
	if finding.Country != "US" || !finding.Server.Known || !finding.Server.Value {
		t.Fatalf("normalized flat response produced incomplete finding: %+v", finding)
	}
}

func TestIPAPIBooleanPresenceDistinguishesMissingFromFalse(t *testing.T) {
	var missing ipAPIResponse
	if err := json.Unmarshal([]byte(`{"ip":"203.0.113.10"}`), &missing); err != nil {
		t.Fatal(err)
	}
	missingFinding := findingFromIPAPI(normalizeIPAPIResponse(missing), 0)
	if findingHasEvidence(missingFinding) {
		t.Fatalf("missing optional fields were treated as evidence: %+v", missingFinding)
	}
	if got := ipAPIBooleanText(missing.IsProxy, missing.BooleanPresence.IsProxy); got != "未返回" {
		t.Fatalf("missing boolean text = %q", got)
	}

	var explicitFalse ipAPIResponse
	if err := json.Unmarshal([]byte(`{"ip":"203.0.113.10","is_proxy":false}`), &explicitFalse); err != nil {
		t.Fatal(err)
	}
	falseFinding := findingFromIPAPI(normalizeIPAPIResponse(explicitFalse), 0)
	if !falseFinding.Proxy.Known || falseFinding.Proxy.Value || !findingHasEvidence(falseFinding) {
		t.Fatalf("explicit false was not preserved as evidence: %+v", falseFinding)
	}
	if got := ipAPIBooleanText(explicitFalse.IsProxy, explicitFalse.BooleanPresence.IsProxy); got != "否" {
		t.Fatalf("explicit false text = %q", got)
	}
}

func TestNetworkFieldsFallbackToBGPWhenIPAPIUnavailable(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.IPVersion = config.IPVersion4
	cfg.IPQualitySources = []string{"ipapi"}
	env := Environment{
		Config: cfg,
		Egress: Egress{
			Attempted: true,
			ByVersion: map[string]EgressAddress{
				config.IPVersion4: {
					Version:        config.IPVersion4,
					IP:             "64.23.222.10",
					Source:         "stun",
					IntelAttempted: true,
					IntelErr:       errors.New("ipapi service unavailable"),
					BGPObservations: []routeViewsPrefix{{
						Prefix:    "64.23.192.0/19",
						OriginASN: 14061,
					}},
				},
			},
		},
	}

	result := (networkProbe{}).Run(context.Background(), env)
	fields := make(map[string]string, len(result.Fields))
	for _, field := range result.Fields {
		fields[field.Key] = field.Value
	}
	if result.Status != model.StatusWarning {
		t.Fatalf("network status = %s, want warning for unavailable ipapi", result.Status)
	}
	if fields["ipv4"] != "64.23.222.10" {
		t.Fatalf("egress IP = %q", fields["ipv4"])
	}
	if fields["ipv4_asn"] != "AS14061" {
		t.Fatalf("ASN fallback = %q, want AS14061", fields["ipv4_asn"])
	}
	if fields["ipv4_route"] != "64.23.192.0/19" {
		t.Fatalf("route fallback = %q, want BGP prefix", fields["ipv4_route"])
	}
	if fields["ipv4_location"] == "unknown" || fields["ipv4_owner"] == "" || fields["ipv4_owner"] == "unknown" {
		t.Fatalf("unavailable IP fields have misleading fallback values: location=%q owner=%q", fields["ipv4_location"], fields["ipv4_owner"])
	}
	if fields["ipv4_owner"] != "未查询（ipapi 不可用）" {
		t.Fatalf("owner semantic fallback = %q", fields["ipv4_owner"])
	}
}
