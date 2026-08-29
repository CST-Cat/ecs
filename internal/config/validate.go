package config

import (
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ecs/internal/i18n"
)

func Validate(runtime Runtime) error {
	knownModules := make(map[string]bool)
	for _, id := range ModuleOrder() {
		knownModules[id] = true
	}
	if len(runtime.Modules) == 0 {
		return i18n.Errorf("err.noModules")
	}
	if err := validateExposure(runtime.Exposure); err != nil {
		return err
	}
	switch runtime.IPVersion {
	case "", IPVersionAuto, IPVersion4, IPVersion6:
		// Empty is accepted for callers constructing Runtime directly; it has
		// the same meaning as auto.
	default:
		return i18n.Errorf("err.unknownIPVersion", runtime.IPVersion)
	}
	for _, id := range runtime.Modules {
		if !knownModules[id] {
			return i18n.Errorf("err.unknownModule", id)
		}
	}
	// Recheck the module set here so callers constructing Runtime directly
	// cannot bypass the external-contact limit.
	for _, id := range runtime.Modules {
		if !AllowsModule(runtime.Exposure, id) {
			info := ExposureFor(id)
			return i18n.Errorf("err.moduleAboveLimitFix", id, info.Level.String(), runtime.Exposure.String())
		}
	}
	if runtime.CPUTime < 100*time.Millisecond || runtime.CPUTime > 30*time.Second {
		return i18n.Errorf("err.cpuTimeRange")
	}
	if runtime.DiskMiB < 16 || runtime.DiskMiB > 16384 {
		return i18n.Errorf("err.diskSizeRange")
	}
	if runtime.HTTPTimeout < time.Second || runtime.HTTPTimeout > time.Minute {
		return i18n.Errorf("err.httpTimeoutRange")
	}
	if runtime.DNSAttempts < 1 || runtime.DNSAttempts > 20 || runtime.LatencyAttempts < 1 || runtime.LatencyAttempts > 20 {
		return i18n.Errorf("err.attemptsRange")
	}
	if runtime.SpeedThreads < 1 || runtime.SpeedThreads > 32 {
		return i18n.Errorf("err.threadsRange")
	}
	allowedFormats := map[string]bool{"json": true, "md": true, "html": true}
	if len(runtime.Formats) == 0 {
		return i18n.Errorf("err.noFormats")
	}
	for _, format := range runtime.Formats {
		if !allowedFormats[format] {
			return i18n.Errorf("err.unknownFormat", format)
		}
	}
	if len(runtime.IPQualitySources) == 0 {
		return i18n.Errorf("err.ipSourceEmpty")
	}
	for _, source := range runtime.IPQualitySources {
		if source != "all" && source != "none" && !IsIPQualitySource(source) {
			return i18n.Errorf("err.ipSourceUnknown", source)
		}
	}
	if len(runtime.IPQualitySources) > 1 && (contains(runtime.IPQualitySources, "all") || contains(runtime.IPQualitySources, "none")) {
		return i18n.Errorf("err.ipSourceCombo")
	}
	for _, group := range [][]Endpoint{runtime.DNSResolvers, runtime.LatencyTargets} {
		for _, endpoint := range group {
			if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
				return i18n.Errorf("err.endpointNameAddress")
			}
			if !validEndpointFamily(endpoint.Family) {
				return i18n.Errorf("err.endpointFamily", endpoint.Name)
			}
		}
	}
	for _, endpoint := range runtime.RouteTargets {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.routeNameAddress")
		}
		if !validRouteTarget(endpoint.Address) {
			return i18n.Errorf("err.routeUnsafe", endpoint.Address)
		}
		if !validEndpointFamily(endpoint.Family) {
			return i18n.Errorf("err.routeFamily", endpoint.Name)
		}
	}
	for _, endpoint := range runtime.STUNServers {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.stunNameAddress")
		}
		host, port, err := net.SplitHostPort(endpoint.Address)
		if err != nil || !validRouteTarget(host) || port == "" {
			return i18n.Errorf("err.stunHostPort", endpoint.Address)
		}
	}
	for _, endpoint := range runtime.BacktraceTargets {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.backtraceNameAddress")
		}
		if !validRouteTarget(endpoint.Address) {
			return i18n.Errorf("err.backtraceUnsafe", endpoint.Address)
		}
		if !validEndpointFamily(endpoint.Family) {
			return i18n.Errorf("err.backtraceFamily", endpoint.Name)
		}
		if !ValidBacktraceCarrier(endpoint.Kind) {
			return i18n.Errorf("err.backtraceKind", endpoint.Kind)
		}
	}
	seenOoklaCarriers := make(map[string]bool)
	for _, server := range runtime.OoklaServers {
		if server.Carrier != OoklaCarrierTelecom && server.Carrier != OoklaCarrierUnicom && server.Carrier != OoklaCarrierMobile {
			return i18n.Errorf("err.ooklaCarrierField", server.Carrier)
		}
		if server.ID < 1 || server.ID > 99999999 {
			return i18n.Errorf("err.ooklaIDField", server.Carrier)
		}
		if seenOoklaCarriers[server.Carrier] {
			return i18n.Errorf("err.ooklaDupField", server.Carrier)
		}
		seenOoklaCarriers[server.Carrier] = true
	}
	if runtime.DiskPath == "" {
		return i18n.Errorf("err.diskPathEmpty")
	}
	if _, err := ParseDiskMatrixMode(runtime.DiskMatrixMode); err != nil {
		return err
	}
	if runtime.IPerfDuration < time.Second || runtime.IPerfDuration > 30*time.Second {
		return i18n.Errorf("err.iperfDuration")
	}
	for _, endpoint := range runtime.IPerfTargets {
		if strings.TrimSpace(endpoint.Name) == "" || !validRouteTarget(endpoint.Host) {
			return i18n.Errorf("err.iperfNodeName", endpoint.Host)
		}
		if endpoint.PortStart < 1 || endpoint.PortStart > 65535 ||
			endpoint.PortEnd < endpoint.PortStart || endpoint.PortEnd > 65535 {
			return i18n.Errorf("err.iperfNodeRange", endpoint.Name)
		}
		switch endpoint.Networks {
		case "", "IPv4", "IPv6", "IPv4|IPv6":
		default:
			return i18n.Errorf("err.iperfNodeNetwork", endpoint.Name)
		}
	}
	abs, err := filepath.Abs(runtime.DiskPath)
	if err != nil {
		return i18n.Errorf("err.diskPathWrap", err)
	}
	if strings.TrimSpace(abs) == "" {
		return i18n.Errorf("err.diskPathInvalid")
	}
	return nil
}

var routeHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?\.?$`)

func validRouteTarget(value string) bool {
	value = strings.TrimSpace(value)
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return true
	}
	return len(value) <= 253 &&
		!strings.Contains(value, "..") &&
		routeHostnamePattern.MatchString(value)
}

func validEndpointFamily(value string) bool {
	return value == "" || value == IPVersion4 || value == IPVersion6
}
