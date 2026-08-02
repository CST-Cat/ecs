package probe

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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
	RAID           string
	Temperatures   []string
	SMART          []string
}

func collectHardwareInventory(ctx context.Context) hardwareInventory {
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
		RAID:           collectRAID(),
		Temperatures:   collectTemperatures(),
		SMART:          collectSMARTHealth(ctx),
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

// collectTemperatures reads the kernel's read-only thermal and hwmon views.
// Values are deliberately reported as labels and degrees only; no device
// serials or other persistent identifiers are involved.
func collectTemperatures() []string {
	var result []string
	seen := make(map[string]bool)
	appendTemperature := func(label, raw string) {
		value, ok := formatTemperature(raw)
		if !ok {
			return
		}
		item := strings.TrimSpace(label) + "=" + value
		if strings.HasPrefix(item, "=") || seen[item] {
			return
		}
		seen[item] = true
		result = append(result, item)
	}

	thermalZones, _ := filepath.Glob("/sys/class/thermal/thermal_zone[0-9]*")
	sort.Strings(thermalZones)
	for _, zone := range thermalZones {
		label := readHardwareValue(filepath.Join(zone, "type"))
		if label == "unknown" {
			label = filepath.Base(zone)
		}
		appendTemperature(label, readHardwareValue(filepath.Join(zone, "temp")))
	}

	hwmonDirs, _ := filepath.Glob("/sys/class/hwmon/hwmon[0-9]*")
	sort.Strings(hwmonDirs)
	for _, dir := range hwmonDirs {
		name := readHardwareValue(filepath.Join(dir, "name"))
		if name == "unknown" {
			name = filepath.Base(dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			file := entry.Name()
			if !strings.HasPrefix(file, "temp") || !strings.HasSuffix(file, "_input") {
				continue
			}
			prefix := strings.TrimSuffix(file, "_input")
			label := readHardwareValue(filepath.Join(dir, prefix+"_label"))
			if label == "unknown" {
				label = prefix
			}
			appendTemperature(name+"/"+label, readHardwareValue(filepath.Join(dir, file)))
		}
	}
	return result
}

func formatTemperature(raw string) (string, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return "", false
	}
	// Linux thermal and hwmon files normally use millidegrees Celsius, but a
	// few drivers expose degrees directly.
	if value > 1000 || value < -1000 {
		value /= 1000
	}
	if value < -100 || value > 200 {
		return "", false
	}
	return fmt.Sprintf("%.1f °C", value), true
}

// collectSMARTHealth inspects each whole block device when smartctl is
// available.  It is intentionally best-effort: VPS virtio disks commonly do
// not expose SMART, and a missing/unsupported device is not a system failure.
func collectSMARTHealth(ctx context.Context) []string {
	var result []string
	for _, name := range collectBlockDeviceNames() {
		device := "/dev/" + name
		deviceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, ok := readSMART(deviceCtx, device)
		cancel()
		if !ok {
			continue
		}
		result = append(result, formatSMARTSummary(info))
	}
	return result
}

func formatSMARTSummary(info smartInfo) string {
	parts := []string{strings.TrimPrefix(info.Device, "/dev/")}
	if info.ModelName != "" {
		parts = append(parts, "model="+info.ModelName)
	}
	if info.Passed != nil {
		status := "fail"
		if *info.Passed {
			status = "pass"
		}
		parts = append(parts, "health="+status)
	}
	if info.Temperature != nil {
		parts = append(parts, fmt.Sprintf("temp=%d °C", *info.Temperature))
	}
	if info.PowerOnHrs != nil {
		parts = append(parts, fmt.Sprintf("power_on=%d h", *info.PowerOnHrs))
	}
	if info.PercentUsed != nil {
		parts = append(parts, fmt.Sprintf("used=%d%%", *info.PercentUsed))
	}
	return strings.Join(parts, " ")
}

func collectRAID() string {
	data, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return "unknown"
	}
	var arrays []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "md") && fields[1] == ":" {
			arrays = append(arrays, fields[0]+" "+fields[2])
		}
	}
	if len(arrays) == 0 {
		return "none detected"
	}
	return strings.Join(arrays, ", ")
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
