package config

import (
	"net"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/module"
)

// ValidateFormats enforces the output formats supported by the current
// report contract. Callers are responsible for parsing and normalizing user
// input before validation.
func ValidateFormats(formats []string) error {
	if len(formats) == 0 {
		return i18n.Errorf("err.noFormats")
	}
	for _, format := range formats {
		switch format {
		case "json", "md", "html":
		default:
			return i18n.Errorf("err.unknownFormat", format)
		}
	}
	return nil
}

func Validate(catalog module.Catalog, runtime Runtime) error {
	knownModules := make(map[string]bool)
	for _, id := range ModuleIDs(catalog) {
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
		if !AllowsModule(catalog, runtime.Exposure, id) {
			info := exposureFor(catalog, id)
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
	if err := ValidateFormats(runtime.Formats); err != nil {
		return err
	}
	if len(runtime.IPQualitySources) == 0 {
		return i18n.Errorf("err.ipSourceEmpty")
	}
	for _, source := range runtime.IPQualitySources {
		if source != "all" && source != "none" && !IsIPQualitySource(source) {
			return i18n.Errorf("err.ipSourceUnknown", source)
		}
	}
	if len(runtime.IPQualitySources) > 1 && (slices.Contains(runtime.IPQualitySources, "all") || slices.Contains(runtime.IPQualitySources, "none")) {
		return i18n.Errorf("err.ipSourceCombo")
	}
	if err := ValidateMediaRegions(runtime.MediaRegions); err != nil {
		return err
	}
	for _, group := range [][]Endpoint{runtime.DNSResolvers, runtime.LatencyTargets} {
		for _, endpoint := range group {
			if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
				return i18n.Errorf("err.endpointNameAddress")
			}
			host, _, err := splitHostPort(endpoint.Address)
			if err != nil || host == "" {
				return i18n.Errorf("err.endpointNeedsHostPort", endpoint.Address)
			}
			if !validRouteTarget(host) {
				return i18n.Errorf("err.endpointUnsafeHost", host)
			}
			if !validEndpointFamily(endpoint.Family) {
				return i18n.Errorf("err.endpointFamily", endpoint.Name)
			}
			if literalFamily := literalEndpointFamily(endpoint.Address, requirePort); literalFamily != "" && endpoint.Family != "" && endpoint.Family != literalFamily {
				return i18n.Errorf("err.endpointFamilyMismatch", endpoint.Name, endpoint.Family, literalFamily)
			}
		}
	}
	if err := validateEndpointDuplicates(runtime.DNSResolvers, runtime.IPVersion, requirePort); err != nil {
		return err
	}
	if err := validateEndpointDuplicates(runtime.LatencyTargets, runtime.IPVersion, requirePort); err != nil {
		return err
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
		if literalFamily := literalEndpointFamily(endpoint.Address, noPort); literalFamily != "" && endpoint.Family != "" && endpoint.Family != literalFamily {
			return i18n.Errorf("err.endpointFamilyMismatch", endpoint.Name, endpoint.Family, literalFamily)
		}
	}
	if err := validateEndpointDuplicates(runtime.RouteTargets, runtime.IPVersion, noPort); err != nil {
		return err
	}
	for _, endpoint := range runtime.STUNServers {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.stunNameAddress")
		}
		host, _, err := splitHostPort(endpoint.Address)
		if err != nil || host == "" || !validRouteTarget(host) {
			return i18n.Errorf("err.stunHostPort", endpoint.Address)
		}
		if !validEndpointFamily(endpoint.Family) {
			return i18n.Errorf("err.endpointFamily", endpoint.Name)
		}
		if literalFamily := literalEndpointFamily(endpoint.Address, requirePort); literalFamily != "" && endpoint.Family != "" && endpoint.Family != literalFamily {
			return i18n.Errorf("err.endpointFamilyMismatch", endpoint.Name, endpoint.Family, literalFamily)
		}
	}
	if err := validateSTUNDuplicates(runtime.STUNServers); err != nil {
		return err
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
		if literalFamily := literalEndpointFamily(endpoint.Address, noPort); literalFamily != "" && endpoint.Family != "" && endpoint.Family != literalFamily {
			return i18n.Errorf("err.endpointFamilyMismatch", endpoint.Name, endpoint.Family, literalFamily)
		}
		if !validBacktraceCarrier(endpoint.Kind) {
			return i18n.Errorf("err.backtraceKind", endpoint.Kind)
		}
	}
	if err := validateEndpointDuplicates(runtime.BacktraceTargets, runtime.IPVersion, noPort); err != nil {
		return err
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
	seenIPerfTargets := make(map[string]bool, len(runtime.IPerfTargets))
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
		if literalFamily := literalEndpointFamily(endpoint.Host, noPort); literalFamily != "" && endpoint.Networks != "" && endpoint.Networks != "IPv"+literalFamily {
			return i18n.Errorf("err.endpointFamilyMismatch", endpoint.Name, endpoint.Networks, literalFamily)
		}
		key := iperfTargetKey(endpoint.Host, endpoint.PortStart, endpoint.PortEnd)
		if seenIPerfTargets[key] {
			return i18n.Errorf("err.endpointDuplicate", endpoint.Host)
		}
		seenIPerfTargets[key] = true
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

// validateEndpointDuplicates enforces the runtime endpoint contract after
// parsing and file overlays have completed. Display names are deliberately
// excluded: the address and the concrete execution family identify the work
// a probe would perform.
func validateEndpointDuplicates(endpoints []Endpoint, runtimeIPVersion string, requirePort bool) error {
	seen := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		key := normalizedEndpointAddress(endpoint.Address, requirePort) + "\x00" + endpointExecutionFamily(endpoint, runtimeIPVersion, requirePort)
		if _, exists := seen[key]; exists {
			return i18n.Errorf("err.endpointDuplicate", endpoint.Address)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateSTUNDuplicates(servers []Endpoint) error {
	seen := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		key := normalizedEndpointAddress(server.Address, requirePort)
		if _, exists := seen[key]; exists {
			return i18n.Errorf("err.endpointDuplicate", server.Address)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// normalizedEndpointAddress makes equivalent host spellings share one key.
// Port numbers are normalized too, so a direct Runtime caller cannot bypass
// the duplicate contract with a leading zero or a different hostname case.
func normalizedEndpointAddress(address string, requirePort bool) string {
	address = strings.TrimSpace(address)
	if requirePort {
		if host, port, err := splitHostPort(address); err == nil {
			return net.JoinHostPort(normalizedEndpointHost(host), port)
		}
	}
	return normalizedEndpointHost(address)
}

func normalizedEndpointHost(host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// endpointExecutionFamily mirrors the family choices made by the probes for
// the parts that can be known at validation time. An explicit family and a
// literal address are concrete; otherwise the Runtime mode (or auto) is the
// operational choice. Family is part of the key only because it can change
// which network operation is executed.
func endpointExecutionFamily(endpoint Endpoint, runtimeIPVersion string, requirePort bool) string {
	if endpoint.Family == IPVersion4 || endpoint.Family == IPVersion6 {
		return endpoint.Family
	}
	if inferred := InferEndpointFamily(endpoint.Address, requirePort); inferred != "" {
		return inferred
	}
	if runtimeIPVersion == IPVersion4 || runtimeIPVersion == IPVersion6 {
		return runtimeIPVersion
	}
	return IPVersionAuto
}
