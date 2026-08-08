package probe

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// hardwareInventory is the read-only part of HardwareQuality that is useful
// even when a VPS hides DMI or does not have privileged helper tools.  It
// intentionally excludes serial numbers and MAC addresses: those are not
// needed to judge a machine and are unnecessarily identifying.
type hardwareInventory struct {
	SystemVendor   string
	ProductName    string
	ProductVersion string
	BoardVendor    string
	BoardName      string
	BoardVersion   string
	BIOSVendor     string
	BIOSVersion    string
	BIOSDate       string
	GPUs           []string
	NICs           []string
	BlockDevices   []string
}

func collectHardwareInventory() hardwareInventory {
	inventory := hardwareInventory{
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
	return inventory
}

func readHardwareValue(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	value := strings.TrimSpace(string(data))
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "unknown") {
		return "unknown"
	}
	return value
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
		// Partitions duplicate the parent device in a compact inventory; fio
		// still tests the selected filesystem path separately.
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

func parseKeyValueFile(path string) map[string]string {
	values := make(map[string]string)
	file, err := os.Open(path)
	if err != nil {
		return values
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func readLinkBase(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func formatHardwareBytes(value uint64) string {
	const unit = uint64(1024)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	amount := float64(value)
	index := 0
	for amount >= float64(unit) && index < len(units)-1 {
		amount /= float64(unit)
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d B", value)
	}
	return fmt.Sprintf("%.1f %s", amount, units[index])
}
