//go:build linux

package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func collectHardwareInventory() hardwareInventory { return collectLinuxHardwareInventory() }

func collectLinuxHardwareInventory() hardwareInventory {
	return hardwareInventory{
		SystemVendor:   readHardwareValue("/sys/class/dmi/id/sys_vendor"),
		ProductName:    readHardwareValue("/sys/class/dmi/id/product_name"),
		ProductVersion: readHardwareValue("/sys/class/dmi/id/product_version"),
		BoardVendor:    readHardwareValue("/sys/class/dmi/id/board_vendor"),
		BoardName:      readHardwareValue("/sys/class/dmi/id/board_name"),
		BoardVersion:   readHardwareValue("/sys/class/dmi/id/board_version"),
		BIOSVendor:     readHardwareValue("/sys/class/dmi/id/bios_vendor"),
		BIOSVersion:    readHardwareValue("/sys/class/dmi/id/bios_version"),
		BIOSDate:       readHardwareValue("/sys/class/dmi/id/bios_date"),
		GPUs:           collectGPUs(),
		NICs:           collectNICs(),
		BlockDevices:   collectBlockDevices(),
	}
}

func collectGPUs() []string {
	entries, err := filepath.Glob("/sys/class/drm/card[0-9]*")
	if err != nil {
		return nil
	}
	sort.Strings(entries)
	var result []string
	for _, entry := range entries {
		name := filepath.Base(entry)
		if len(name) <= len("card") || strings.Trim(name[len("card"):], "0123456789") != "" {
			continue
		}
		uevent := parseKeyValueFile(filepath.Join(entry, "device", "uevent"))
		vendor := firstNonEmpty(uevent["PCI_ID"], readHardwareValue(filepath.Join(entry, "device", "vendor")))
		device := firstNonEmpty(uevent["PCI_SUBSYS_ID"], readHardwareValue(filepath.Join(entry, "device", "device")))
		driver := readLinkBase(filepath.Join(entry, "device", "driver"))
		parts := []string{name}
		if vendor != "unknown" {
			parts = append(parts, "vendor="+vendor)
		}
		if device != "unknown" {
			parts = append(parts, "device="+device)
		}
		if driver != "" {
			parts = append(parts, "driver="+driver)
		}
		result = append(result, strings.Join(parts, " "))
	}
	return result
}

func collectNICs() []string {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if name == "lo" {
			continue
		}
		base := filepath.Join("/sys/class/net", name)
		state := readHardwareValue(filepath.Join(base, "operstate"))
		speed := readHardwareValue(filepath.Join(base, "speed"))
		if speed != "unknown" {
			if value, err := strconv.ParseInt(speed, 10, 64); err == nil && value > 0 {
				speed = fmt.Sprintf("%d Mbps", value)
			}
		}
		driver := readLinkBase(filepath.Join(base, "device", "driver"))
		parts := []string{name, "state=" + state}
		if speed != "unknown" && speed != "-1" {
			parts = append(parts, "speed="+speed)
		}
		if driver != "" {
			parts = append(parts, "driver="+driver)
		}
		result = append(result, strings.Join(parts, " "))
	}
	return result
}

func collectBlockDevices() []string {
	names := collectBlockDeviceNames()
	if len(names) == 0 {
		return nil
	}
	var result []string
	for _, name := range names {
		base := filepath.Join("/sys/class/block", name)
		if _, err := os.Stat(filepath.Join(base, "partition")); err == nil {
			continue
		}
		sectors, _ := strconv.ParseUint(readHardwareValue(filepath.Join(base, "size")), 10, 64)
		size := "unknown"
		if sectors > 0 && sectors <= ^uint64(0)/512 {
			size = formatHardwareBytes(sectors * 512)
		}
		rotational := readHardwareValue(filepath.Join(base, "queue", "rotational"))
		kind := "non-rotational"
		if rotational == "1" {
			kind = "rotational"
		} else if rotational == "unknown" {
			kind = "unknown-type"
		}
		model := readHardwareValue(filepath.Join(base, "device", "model"))
		vendor := readHardwareValue(filepath.Join(base, "device", "vendor"))
		parts := []string{name, size, kind}
		if vendor != "unknown" {
			parts = append(parts, "vendor="+vendor)
		}
		if model != "unknown" {
			parts = append(parts, "model="+model)
		}
		result = append(result, strings.Join(parts, " "))
	}
	return result
}

func collectBlockDeviceNames() []string {
	entries, err := os.ReadDir("/sys/class/block")
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "fd") || strings.HasPrefix(name, "sr") ||
			strings.HasPrefix(name, "dm-") {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/block", name, "partition")); err == nil {
			continue
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
