package probe

// Local cloud identity discovery deliberately reads only the small, non-secret
// identity fields exposed by cloud-init.  It never contacts a metadata
// endpoint and never copies the surrounding instance-data document into a
// report.  A missing or malformed file simply yields an empty identity.

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

const maxCloudInitIdentityBytes = 4 << 20

type cloudIdentity struct {
	Provider string
	Region   string
}

type cloudInitDocument struct {
	V1 struct {
		CloudName        string  `json:"cloud_name"`
		Platform         string  `json:"platform"`
		Region           *string `json:"region"`
		AvailabilityZone string  `json:"availability_zone"`
	} `json:"v1"`
}

// discoverLocalCloudIdentity reads cloud-init's local cache.  The paths cover
// the two layouts used by current Linux distributions; no network metadata
// service is queried as a fallback.
func discoverLocalCloudIdentity() cloudIdentity {
	identity := cloudIdentity{}
	for _, path := range []string{
		"/run/cloud-init/instance-data.json",
		"/var/lib/cloud/instance/instance-data.json",
	} {
		if candidate, ok := readCloudInitIdentity(path); ok {
			identity = candidate
			break
		}
	}
	if identity.Provider == "" {
		identity.Provider = discoverDMICloudProvider()
	}
	return identity
}

func discoverDMICloudProvider() string {
	return cloudProviderFromDMI(
		readHardwareValue("/sys/class/dmi/id/sys_vendor"),
		readHardwareValue("/sys/class/dmi/id/product_name"),
		readHardwareValue("/sys/class/dmi/id/board_vendor"),
	)
}

// cloudProviderFromDMI uses only explicit provider signatures.  Generic
// virtualization values such as QEMU, KVM and VMware are intentionally not
// treated as cloud providers.
func cloudProviderFromDMI(values ...string) string {
	text := strings.ToLower(strings.Join(values, " | "))
	for _, item := range []struct{ needle, provider string }{
		{needle: "digitalocean", provider: "digitalocean"},
		{needle: "digital ocean", provider: "digitalocean"},
		{needle: "vultr", provider: "vultr"},
		{needle: "hetzner", provider: "hetzner"},
		{needle: "leaseweb", provider: "leaseweb"},
		{needle: "scaleway", provider: "scaleway"},
		{needle: "upcloud", provider: "upcloud"},
		{needle: "linode", provider: "linode"},
		{needle: "akamai cloud", provider: "linode"},
		{needle: "oracle cloud", provider: "oracle-cloud"},
		{needle: "oracle corporation", provider: "oracle-cloud"},
		{needle: "amazon ec2", provider: "aws"},
		{needle: "amazon web services", provider: "aws"},
		{needle: "google compute engine", provider: "gcp"},
		{needle: "google cloud", provider: "gcp"},
		{needle: "microsoft azure", provider: "azure"},
		{needle: "aliyun", provider: "alibaba"},
		{needle: "alibaba cloud", provider: "alibaba"},
	} {
		if strings.Contains(text, item.needle) {
			return item.provider
		}
	}
	// Azure's common DMI pair is Microsoft Corporation / Virtual Machine.
	if strings.Contains(text, "microsoft corporation") && strings.Contains(text, "virtual machine") {
		return "azure"
	}
	return ""
}

func readCloudInitIdentity(path string) (cloudIdentity, bool) {
	file, err := os.Open(path)
	if err != nil {
		return cloudIdentity{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCloudInitIdentityBytes+1))
	if err != nil || len(data) > maxCloudInitIdentityBytes {
		return cloudIdentity{}, false
	}
	return parseCloudInitIdentity(data)
}

func parseCloudInitIdentity(data []byte) (cloudIdentity, bool) {
	var document cloudInitDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return cloudIdentity{}, false
	}
	provider := normalizeCloudProvider(document.V1.CloudName)
	if provider == "" {
		provider = normalizeCloudProvider(document.V1.Platform)
	}
	region := ""
	if document.V1.Region != nil {
		region = normalizeCloudRegion(*document.V1.Region)
	}
	if region == "" {
		region = normalizeCloudRegion(document.V1.AvailabilityZone)
		region = trimAvailabilityDomain(region)
	}
	return cloudIdentity{Provider: provider, Region: region}, provider != "" || region != ""
}

func normalizeCloudProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, runeValue := range value {
		if (runeValue >= 'a' && runeValue <= 'z') ||
			(runeValue >= '0' && runeValue <= '9') ||
			runeValue == ' ' || runeValue == '-' || runeValue == '_' || runeValue == '.' {
			continue
		}
		return ""
	}
	aliases := map[string]string{
		"aws":                   "aws",
		"amazon":                "aws",
		"amazon ec2":            "aws",
		"ec2":                   "aws",
		"gcp":                   "gcp",
		"google":                "gcp",
		"google cloud":          "gcp",
		"google compute engine": "gcp",
		"azure":                 "azure",
		"microsoft azure":       "azure",
		"microsoft":             "azure",
		"oracle":                "oracle-cloud",
		"oci":                   "oracle-cloud",
		"oracle cloud":          "oracle-cloud",
		"alibaba":               "alibaba",
		"aliyun":                "alibaba",
		"digitalocean":          "digitalocean",
		"digital ocean":         "digitalocean",
		"vultr":                 "vultr",
		"hetzner":               "hetzner",
		"linode":                "linode",
		"akamai":                "linode",
		"ovh":                   "ovh",
		"scaleway":              "scaleway",
		"upcloud":               "upcloud",
		"leaseweb":              "leaseweb",
	}
	return aliases[value]
}

func normalizeCloudRegion(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, runeValue := range value {
		if (runeValue >= 'a' && runeValue <= 'z') ||
			(runeValue >= '0' && runeValue <= '9') ||
			runeValue == '-' || runeValue == '_' || runeValue == '.' {
			continue
		}
		return ""
	}
	return value
}

func trimAvailabilityDomain(value string) string {
	if value == "" {
		return value
	}
	// OCI availability domains use <region>-ad-<number>; retain only the
	// region so the submission does not expose a finer-grained fault domain.
	marker := "-ad-"
	index := strings.LastIndex(value, marker)
	if index < 0 || index+len(marker) == len(value) {
		return value
	}
	for _, runeValue := range value[index+len(marker):] {
		if runeValue < '0' || runeValue > '9' {
			return value
		}
	}
	return value[:index]
}
