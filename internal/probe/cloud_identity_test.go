package probe

import "testing"

func TestParseCloudInitIdentityWhitelist(t *testing.T) {
	identity, ok := parseCloudInitIdentity([]byte(`{
  "v1": {
    "cloud_name": "oracle",
    "platform": "oracle",
    "region": null,
    "availability_zone": "us-sanjose-1-ad-1",
    "instance_id": "ocid1.instance.secret",
    "subplatform": "metadata (http://169.254.169.254/opc/v2/)"
  }
}`))
	if !ok || identity.Provider != "oracle-cloud" || identity.Region != "us-sanjose-1" {
		t.Fatalf("cloud identity = %+v, ok=%v", identity, ok)
	}
}

func TestParseCloudInitIdentityPrefersRegion(t *testing.T) {
	identity, ok := parseCloudInitIdentity([]byte(`{
  "v1": {"cloud_name":"aws", "region":"us-east-1", "availability_zone":"us-east-1b"}
}`))
	if !ok || identity.Provider != "aws" || identity.Region != "us-east-1" {
		t.Fatalf("cloud identity = %+v, ok=%v", identity, ok)
	}
}

func TestCloudProviderFromDMIUsesExplicitSignatures(t *testing.T) {
	cases := []struct {
		name     string
		values   []string
		provider string
	}{
		{name: "vultr", values: []string{"Vultr", "Vultr VM"}, provider: "vultr"},
		{name: "azure", values: []string{"Microsoft Corporation", "Virtual Machine"}, provider: "azure"},
		{name: "generic kvm", values: []string{"QEMU", "KVM Virtual Machine"}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := cloudProviderFromDMI(item.values...); got != item.provider {
				t.Fatalf("provider = %q, want %q", got, item.provider)
			}
		})
	}
}

func TestNormalizeCloudProviderCanonicalNames(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{input: "oracle", want: "oracle-cloud"},
		{input: "OCI", want: "oracle-cloud"},
		{input: "Google Compute Engine", want: "gcp"},
	} {
		if got := normalizeCloudProvider(item.input); got != item.want {
			t.Errorf("normalizeCloudProvider(%q) = %q, want %q", item.input, got, item.want)
		}
	}
}

func TestTrimAvailabilityDomain(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{input: "us-sanjose-1-ad-1", want: "us-sanjose-1"},
		{input: "us-sanjose-1-ad-foo", want: "us-sanjose-1-ad-foo"},
		{input: "us-east-1a", want: "us-east-1a"},
	} {
		if got := trimAvailabilityDomain(item.input); got != item.want {
			t.Errorf("trimAvailabilityDomain(%q) = %q, want %q", item.input, got, item.want)
		}
	}
}
