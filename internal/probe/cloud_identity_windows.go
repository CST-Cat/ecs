//go:build windows

package probe

// Windows has no cloud-init cache layout that this local probe can assume.
// Cloud identity therefore comes only from non-identifying SMBIOS provider
// signatures, never from a network metadata endpoint.
func platformCloudIdentityPaths() []string { return nil }

func discoverDMICloudProvider() string {
	hardware := parseWindowsSMBIOSInventory(readWindowsSMBIOS())
	return cloudProviderFromDMI(hardware.SystemVendor, hardware.ProductName, hardware.BoardVendor)
}
