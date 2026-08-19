package probe

import "testing"

func TestCloudIdentityParsesProviderRegionAndDMISignature(t *testing.T) {
	identity, ok := parseCloudInitIdentity([]byte(`{"v1":{"cloud_name":"oracle","availability_zone":"us-sanjose-1-ad-1"}}`))
	if !ok || identity.Provider != "oracle-cloud" || identity.Region != "us-sanjose-1" {
		t.Fatalf("cloud identity = %+v, ok=%v", identity, ok)
	}
	if got := cloudProviderFromDMI("Microsoft Corporation", "Virtual Machine"); got != "azure" {
		t.Fatalf("DMI provider = %q, want azure", got)
	}
}
