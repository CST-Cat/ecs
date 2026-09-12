//go:build linux

package probe

func platformCloudIdentityPaths() []string {
	return []string{"/run/cloud-init/instance-data.json", "/var/lib/cloud/instance/instance-data.json"}
}

func discoverDMICloudProvider() string {
	return cloudProviderFromDMI(
		readHardwareValue("/sys/class/dmi/id/sys_vendor"),
		readHardwareValue("/sys/class/dmi/id/product_name"),
		readHardwareValue("/sys/class/dmi/id/board_vendor"),
	)
}
