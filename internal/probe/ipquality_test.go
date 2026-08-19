package probe

import (
	"testing"
	"time"
)

func TestIPQualityProviderFixtureProducesMeasurement(t *testing.T) {
	var data ipAPIResponse
	data.Location.CountryCode = "US"
	data.ASN.Type = "hosting"
	data.Company.Type = "business"
	data.Company.AbuserScore = "0.42 (Low)"
	data.BooleanPresence.IsProxy = true
	data.IsProxy = true
	finding := findingFromIPAPI(data, 5*time.Millisecond)
	bundle := ipQualityBundle{Version: "4", Findings: map[string]qualityFinding{"ipapi": finding}}
	measurements := bundle.measurements()
	if finding.Country != "US" || finding.Usage != "机房" || !finding.Proxy.Known || !finding.Proxy.Value {
		t.Fatalf("provider finding = %+v", finding)
	}
	if len(measurements) != 1 || measurements[0].Key != "ipv4_ipapi_risk_score" || measurements[0].Value != 42 {
		t.Fatalf("provider measurements = %+v", measurements)
	}
}

func TestIPQualityRejectsMalformedProviderResponse(t *testing.T) {
	if finding := parseIPregistryJSON([]byte(`{"location":`)); finding.Err == nil {
		t.Fatal("malformed provider response unexpectedly parsed")
	}
}
