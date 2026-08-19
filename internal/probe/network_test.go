package probe

import (
	"encoding/json"
	"testing"
)

func TestNormalizeIPAPIResponseProducesFinding(t *testing.T) {
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
	if data.IP != "64.23.222.10" || data.ASN.ASN != 14061 || data.Company.Name != "DigitalOcean, LLC" || data.Location.CountryCode != "US" {
		t.Fatalf("normalized response = %+v", data)
	}
	finding := findingFromIPAPI(data, 0)
	if finding.Country != "US" || !finding.Server.Known || !finding.Server.Value {
		t.Fatalf("normalized response produced incomplete finding: %+v", finding)
	}
}
