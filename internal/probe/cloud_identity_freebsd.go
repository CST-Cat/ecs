//go:build freebsd

package probe

func platformCloudIdentityPaths() []string {
	return []string{"/run/cloud-init/instance-data.json", "/var/lib/cloud/instance/instance-data.json"}
}

func discoverDMICloudProvider() string {
	return cloudProviderFromDMI(
		readFreeBSDKenvValue("smbios.system.maker"),
		readFreeBSDKenvValue("smbios.system.product"),
		readFreeBSDKenvValue("smbios.planar.maker"),
	)
}
