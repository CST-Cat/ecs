package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"ecs/internal/i18n"
)

func LoadFile(path string) (File, error) {
	var cfg File
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, i18n.Errorf("err.configRead", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, i18n.Errorf("err.configParse", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return cfg, i18n.Errorf("err.configSingle")
		}
		return cfg, i18n.Errorf("err.configTrailing", err)
	}
	return cfg, nil
}

func ApplyFile(runtime *Runtime, file File) error {
	if file.Exposure != "" {
		level, err := ParseExposure(file.Exposure)
		if err != nil {
			return err
		}
		runtime.Exposure = level
	}
	if file.Reveal != nil {
		runtime.Reveal = *file.Reveal
	}
	if file.IPVersion != "" {
		runtime.IPVersion = strings.ToLower(strings.TrimSpace(file.IPVersion))
	}
	if len(file.IPQualitySources) > 0 {
		runtime.IPQualitySources = normalizeList(file.IPQualitySources)
	}
	if file.NoColor != nil {
		runtime.NoColor = *file.NoColor
	}
	if len(file.Formats) > 0 {
		runtime.Formats = append([]string(nil), file.Formats...)
	}
	if file.Output != "" {
		runtime.Output = file.Output
	}
	if file.CPUTime != "" {
		value, err := time.ParseDuration(file.CPUTime)
		if err != nil {
			return fmt.Errorf("cpu_time: %w", err)
		}
		runtime.CPUTime = value
	}
	if file.DiskMiB != nil {
		runtime.DiskMiB = *file.DiskMiB
	}
	if file.DiskPath != "" {
		runtime.DiskPath = file.DiskPath
	}
	if file.DiskMulti != nil {
		runtime.DiskMulti = *file.DiskMulti
	}
	if file.DiskMatrixMode != "" {
		mode, err := ParseDiskMatrixMode(file.DiskMatrixMode)
		if err != nil {
			return err
		}
		runtime.DiskMatrixMode = mode
	}
	if file.IPerfDuration != "" {
		value, err := time.ParseDuration(file.IPerfDuration)
		if err != nil {
			return fmt.Errorf("iperf_duration: %w", err)
		}
		runtime.IPerfDuration = value
	}
	if len(file.IPerfTargets) > 0 {
		runtime.IPerfTargets = append([]IPerfEndpoint(nil), file.IPerfTargets...)
	}
	if file.HTTPTimeout != "" {
		value, err := time.ParseDuration(file.HTTPTimeout)
		if err != nil {
			return fmt.Errorf("http_timeout: %w", err)
		}
		runtime.HTTPTimeout = value
	}
	if file.DNSAttempts != nil {
		runtime.DNSAttempts = *file.DNSAttempts
	}
	if file.LatencyAttempts != nil {
		runtime.LatencyAttempts = *file.LatencyAttempts
	}
	if file.SpeedThreads != nil {
		runtime.SpeedThreads = *file.SpeedThreads
	}
	if len(file.DNSResolvers) > 0 {
		runtime.DNSResolvers = append([]Endpoint(nil), file.DNSResolvers...)
	}
	if len(file.LatencyTargets) > 0 {
		runtime.LatencyTargets = append([]Endpoint(nil), file.LatencyTargets...)
	}
	if len(file.RouteTargets) > 0 {
		runtime.RouteTargets = append([]Endpoint(nil), file.RouteTargets...)
	}
	if len(file.BacktraceTargets) > 0 {
		runtime.BacktraceTargets = append([]Endpoint(nil), file.BacktraceTargets...)
	}
	if len(file.STUNServers) > 0 {
		runtime.STUNServers = append([]Endpoint(nil), file.STUNServers...)
	}
	if len(file.MediaRegions) > 0 {
		runtime.MediaRegions = normalizeList(file.MediaRegions)
	}
	if len(file.OoklaServers) > 0 {
		runtime.OoklaServers = append([]OoklaServer(nil), file.OoklaServers...)
	}
	// An explicit file allowlist is independent of the profile preset: callers
	// may select any registered module, then remove entries with skip.
	if err := ValidateModuleSelection(file.Only, file.Skip); err != nil {
		return err
	}
	runtime.Modules = SelectModules(runtime.Modules, file.Only, file.Skip)
	return nil
}

func SelectModules(base, only, skip []string) []string {
	selected := make(map[string]bool)
	if len(only) > 0 {
		for _, id := range only {
			selected[id] = true
		}
	} else {
		for _, id := range base {
			selected[id] = true
		}
	}
	for _, id := range skip {
		delete(selected, id)
	}
	out := make([]string, 0, len(selected))
	for _, id := range ModuleIDs() {
		if selected[id] {
			out = append(out, id)
		}
	}
	return out
}

func ParseList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// ParseOoklaServerList parses carrier=server-id pairs. IDs are not fetched
// from the network because the official catalogue is volatile.
func ParseOoklaServerList(raw string) ([]OoklaServer, error) {
	var result []OoklaServer
	seen := make(map[string]bool)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, i18n.Errorf("err.ooklaFormat")
		}
		carrier := normalizeOoklaCarrier(parts[0])
		if carrier == "" {
			return nil, i18n.Errorf("err.ooklaCarrier", parts[0])
		}
		id, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || id < 1 || id > 99999999 {
			return nil, i18n.Errorf("err.ooklaIDInvalid", parts[1])
		}
		if seen[carrier] {
			return nil, i18n.Errorf("err.ooklaDuplicate", carrier)
		}
		seen[carrier] = true
		result = append(result, OoklaServer{Carrier: carrier, ID: id})
	}
	return result, nil
}

func normalizeOoklaCarrier(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "电信", "telecom", "ct", "chinatelecom":
		return OoklaCarrierTelecom
	case "联通", "unicom", "cu", "chinaunicom":
		return OoklaCarrierUnicom
	case "移动", "mobile", "cm", "chinamobile":
		return OoklaCarrierMobile
	default:
		return ""
	}
}

func ExampleFile() File {
	reveal := false
	return File{
		Profile:          ProfileStandard,
		Exposure:         DefaultExposure,
		Reveal:           &reveal,
		IPVersion:        IPVersionAuto,
		IPQualitySources: []string{"all"},
		Formats:          []string{"json", "md", "html"},
		Output:           "./reports",
		DiskPath:         ".",
		IPerfDuration:    "5s",
		HTTPTimeout:      "10s",
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func normalizeList(items []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
