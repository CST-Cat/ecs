//go:build freebsd

package probe

import (
	"context"
	"strings"
)

const (
	freeBSDKenvPath     = "/bin/kenv"
	freeBSDIfconfigPath = "/sbin/ifconfig"
	freeBSDGeomPath     = "/sbin/geom"
)

func collectHardwareInventory() hardwareInventory {
	return collectFreeBSDHardwareInventory()
}

// collectFreeBSDHardwareInventory reads only non-identifying SMBIOS facts from
// FreeBSD's kenv interface.  Serial numbers, UUIDs and MAC addresses are
// intentionally excluded just as they are on Linux.
func collectFreeBSDHardwareInventory() hardwareInventory {
	return hardwareInventory{
		SystemVendor:   readFreeBSDKenvValue("smbios.system.maker"),
		ProductName:    readFreeBSDKenvValue("smbios.system.product"),
		ProductVersion: readFreeBSDKenvValue("smbios.system.version"),
		BoardVendor:    readFreeBSDKenvValue("smbios.planar.maker"),
		BoardName:      readFreeBSDKenvValue("smbios.planar.product"),
		BoardVersion:   readFreeBSDKenvValue("smbios.planar.version"),
		BIOSVendor:     readFreeBSDKenvValue("smbios.bios.vendor"),
		BIOSVersion:    readFreeBSDKenvValue("smbios.bios.version"),
		BIOSDate:       readFreeBSDKenvValue("smbios.bios.reldate"),
		NICs:           collectFreeBSDNICs(),
		BlockDevices:   collectFreeBSDBlockDevices(),
	}
}

func readFreeBSDKenvValue(key string) string {
	value := commandOutput(context.Background(), freeBSDKenvPath, key)
	if value == "" {
		return "unknown"
	}
	return value
}

func collectFreeBSDNICs() []string {
	output := commandOutput(context.Background(), freeBSDIfconfigPath, "-l")
	var result []string
	for _, name := range strings.Fields(output) {
		if strings.HasPrefix(name, "lo") {
			continue
		}
		// The interface name is a real base-system observation.  Link speed is
		// intentionally omitted because ifconfig's media text is driver-specific
		// and should not be guessed into a common schema.
		result = append(result, name)
	}
	return result
}

func collectFreeBSDBlockDevices() []string {
	output := commandOutput(context.Background(), freeBSDGeomPath, "disk", "list")
	return parseFreeBSDBlockDevices(output)
}

func parseFreeBSDBlockDevices(output string) []string {
	var result []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Geom name:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "Geom name:"))
		if name != "" {
			result = append(result, name)
		}
	}
	return result
}
