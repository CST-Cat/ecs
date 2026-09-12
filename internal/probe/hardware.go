package probe

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hardwareInventory is the read-only part of HardwareQuality that is useful
// even when DMI or platform-specific device metadata is unavailable. It
// intentionally excludes serial numbers and MAC addresses.
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

// parseKeyValueFile and readLinkBase are small filesystem parsers shared by
// platform collectors and their deterministic tests. Platform paths remain
// in the corresponding *_linux.go or *_freebsd.go collector.
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
