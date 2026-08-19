package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloudIdentityLocalFixturesAndNormalization(t *testing.T) {
	for _, test := range []struct {
		name, data string
		want       cloudIdentity
		ok         bool
	}{
		{
			name: "availability domain is reduced",
			data: `{"v1":{"cloud_name":"oracle","availability_zone":"us-sanjose-1-ad-1"}}`,
			want: cloudIdentity{Provider: "oracle-cloud", Region: "us-sanjose-1"},
			ok:   true,
		},
		{
			name: "region and platform aliases",
			data: `{"v1":{"platform":"Google Cloud","region":"US-CENTRAL1"}}`,
			want: cloudIdentity{Provider: "gcp", Region: "us-central1"},
			ok:   true,
		},
		{
			name: "invalid region is dropped",
			data: `{"v1":{"cloud_name":"aws","region":"us east"}}`,
			want: cloudIdentity{Provider: "aws"},
			ok:   true,
		},
		{name: "unknown document is ignored", data: `{"v1":{"cloud_name":"bare-metal"}}`},
		{name: "malformed document is ignored", data: `{`, ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseCloudInitIdentity([]byte(test.data))
			if ok != test.ok || got != test.want {
				t.Fatalf("identity = %+v, ok=%v; want %+v, ok=%v", got, ok, test.want, test.ok)
			}
		})
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "instance-data.json")
	if err := os.WriteFile(path, []byte(`{"v1":{"cloud_name":"aws","region":"eu-west-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readCloudInitIdentity(path); !ok || got != (cloudIdentity{Provider: "aws", Region: "eu-west-1"}) {
		t.Fatalf("read cloud identity = %+v, ok=%v", got, ok)
	}
	if _, ok := readCloudInitIdentity(filepath.Join(dir, "missing.json")); ok {
		t.Fatal("missing cloud-init file must be ignored")
	}
	if got := cloudProviderFromDMI("Microsoft Corporation", "Virtual Machine"); got != "azure" {
		t.Fatalf("DMI provider = %q, want azure", got)
	}
	if got := cloudProviderFromDMI("QEMU", "Standard PC"); got != "" {
		t.Fatalf("generic virtualization identified as %q", got)
	}
}
