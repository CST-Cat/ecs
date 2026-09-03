package probe

import (
	"fmt"
	"strings"
	"time"

	"ecs/internal/model"
	"ecs/internal/module"
)

// Definition keeps a module's immutable metadata and its concrete executor
// together at the canonical composition boundary.
type Definition struct {
	Descriptor module.Descriptor
	Probe      Probe
}

// BuiltinDefinitions returns the canonical built-in module definitions in
// execution order.  The slice and all descriptor reference fields are owned
// by the caller; no mutable registry is retained between calls.
func BuiltinDefinitions() []Definition {
	definitions := []Definition{
		{Descriptor: moduleDescriptor("system", true, module.ExposureLocal, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "inventory", Label: "methodology.inventory", Engine: "OS/runtime inspection", Profile: "probe.system.profile", ComparisonScope: "probe.system.comparison_scope"},
			nil, time.Second), Probe: systemProbe{}},
		{Descriptor: moduleDescriptor("network", false, module.ExposureThirdParty, true, module.ConcurrencyProbe,
			model.Methodology{Kind: "provider-assessment", Label: "methodology.provider-assessment", Engine: "multi-provider IP intelligence", Profile: "probe.network.profile", ComparisonScope: "probe.network.comparison_scope"},
			nil, 5*time.Second, "ipquality", "wizard.askIPQuality"), Probe: networkProbe{}},
		{Descriptor: moduleDescriptor("bgp", true, module.ExposurePublic, true, module.ConcurrencyProbe,
			model.Methodology{Kind: "provider-assessment", Label: "methodology.provider-assessment", Engine: "RouteViews current RIB API", Profile: "probe.bgp.profile", ComparisonScope: "probe.bgp.comparison_scope"},
			nil, 4*time.Second), Probe: bgpProbe{}},
		{Descriptor: withRetryOnInterference(moduleDescriptorWithEstimateMode("cpu", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "sysbench", Profile: "probe.cpu.profile", ComparisonScope: "probe.cpu.comparison_scope"},
			[]string{"sysbench"}, 3*time.Second, module.EstimateModeCPU)), Probe: cpuProbe{}},
		{Descriptor: withRetryOnInterference(moduleDescriptorWithEstimateMode("zstd", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "zstd", Profile: "probe.zstd.profile", ComparisonScope: "probe.zstd.comparison_scope"},
			[]string{"zstd"}, 25*time.Second, module.EstimateModeTwoContext)), Probe: zstdProbe{}},
		{Descriptor: withRetryOnInterference(moduleDescriptorWithEstimateMode("npb", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "NASA NPB-OMP", Profile: "probe.npb.profile", ComparisonScope: "probe.npb.comparison_scope"},
			[]string{"npb-ep", "npb-ft"}, 60*time.Second, module.EstimateModeTwoContext)), Probe: npbProbe{}},
		{Descriptor: withRetryOnInterference(moduleDescriptorWithEstimateMode("memory", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "STREAM", Profile: "probe.memory.stream.profile", ComparisonScope: "probe.memory.comparison_scope"},
			[]string{"stream"}, 5*time.Second, module.EstimateModeMemory)), Probe: memoryProbe{}},
		{Descriptor: withRetryOnInterference(moduleDescriptorWithEstimateMode("crypto", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "OpenSSL speed", Profile: "probe.crypto.profile", ComparisonScope: "probe.crypto.comparison_scope"},
			[]string{"openssl"}, 45*time.Second, module.EstimateModeTwoContext)), Probe: cryptoProbe{}},
		{Descriptor: withRetryOnInterference(moduleDescriptorWithEstimateMode("disk", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "fio", Profile: "probe.disk.profile", ComparisonScope: "probe.disk.comparison_scope"},
			[]string{"fio"}, 8*time.Second, module.EstimateModeDisk)), Probe: diskProbe{}},
		{Descriptor: moduleDescriptorWithEstimateMode("dns", true, module.ExposurePublic, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "DNS/UDP", Profile: "probe.dns.profile", ComparisonScope: "probe.dns.comparison_scope"},
			nil, 8*time.Second, module.EstimateModeDNS), Probe: dnsProbe{}},
		{Descriptor: moduleDescriptorWithEstimateMode("latency", true, module.ExposurePublic, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "TCP connect", Profile: "probe.latency.profile", ComparisonScope: "probe.latency.comparison_scope"},
			[]string{"ping"}, 15*time.Second, module.EstimateModeLatency), Probe: latencyProbe{}},
		{Descriptor: moduleDescriptorWithEstimateMode("speed", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "iperf3", Profile: "probe.speed.profile", ComparisonScope: "probe.speed.comparison_scope"},
			[]string{"iperf3"}, 30*time.Second, module.EstimateModeSpeed, "throughput", "wizard.askThroughput"), Probe: speedProbe{}},
		{Descriptor: moduleDescriptor("ports", true, module.ExposurePublic, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "TCP connect", Profile: "probe.ports.profile", ComparisonScope: "probe.ports.comparison_scope"},
			nil, 4*time.Second), Probe: portsProbe{}},
		{Descriptor: moduleDescriptor("nat", true, module.ExposurePublic, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "STUN (RFC 5389/5780)", Profile: "probe.nat.profile", ComparisonScope: "probe.nat.comparison_scope"},
			nil, 12*time.Second), Probe: natProbe{}},
		{Descriptor: moduleDescriptor("blacklist", true, module.ExposurePublic, true, module.ConcurrencyProbe,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "DNSBL over DNS A lookup", Profile: "probe.blacklist.profile", ComparisonScope: "probe.blacklist.comparison_scope"},
			nil, 10*time.Second, "blacklist", "wizard.askBlacklist"), Probe: blacklistProbe{}},
		{Descriptor: moduleDescriptor("apps", true, module.ExposurePublic, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "native TCP connect", Profile: "probe.apps.profile", ComparisonScope: "probe.apps.comparison_scope"},
			nil, 8*time.Second), Probe: appsProbe{}},
		{Descriptor: moduleDescriptor("cnspeed", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "HTTP download against speedtest.cn nodes", Profile: "probe.cnspeed.profile", ComparisonScope: "probe.cnspeed.comparison_scope"},
			nil, 40*time.Second, "throughput", "wizard.askThroughput"), Probe: cnSpeedProbe{}},
		{Descriptor: moduleDescriptorWithPrivacyNotice("ookla", false, module.ExposureThirdParty, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "official Ookla Speedtest CLI", Profile: "probe.ookla.profile", ComparisonScope: "probe.ookla.comparison_scope"},
			[]string{"speedtest"}, 90*time.Second, "message.notice.ooklaPrivacy"), Probe: ooklaProbe{}},
		{Descriptor: moduleDescriptor("media", true, module.ExposurePublic, false, module.ConcurrencyProbe,
			model.Methodology{Kind: "heuristic", Label: "methodology.heuristic", Engine: "public HTTP evidence", Profile: "probe.media.profile", ComparisonScope: "probe.media.comparison_scope"},
			nil, 10*time.Second, "media", "wizard.askMedia"), Probe: mediaProbe{}},
		{Descriptor: moduleDescriptorWithEstimateMode("route", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "NextTrace Tiny", Profile: "probe.route.profile", ComparisonScope: "probe.route.comparison_scope"},
			[]string{"nexttrace-tiny"}, 36*time.Second, module.EstimateModeRoute, "routing", "wizard.askRouting"), Probe: routeProbe{}},
		{Descriptor: moduleDescriptor("backtrace", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
			model.Methodology{Kind: "heuristic", Label: "methodology.heuristic", Engine: "probe.backtrace.methodology.engine", Profile: "probe.backtrace.profile", ComparisonScope: "probe.backtrace.comparison_scope"},
			[]string{"nexttrace-tiny"}, 30*time.Second, "routing", "wizard.askRouting"), Probe: backtraceProbe{}},
	}

	if _, err := validateDefinitions(definitions); err != nil {
		panic(fmt.Sprintf("invalid built-in probe definitions: %v", err))
	}
	return copyDefinitions(definitions)
}

// CatalogFromDefinitions validates a definition set and derives its immutable
// descriptor catalog. The caller supplies the definitions explicitly so this
// boundary cannot silently fall back to built-in module state.
func CatalogFromDefinitions(definitions []Definition) (module.Catalog, error) {
	return validateDefinitions(definitions)
}

func validateDefinitions(definitions []Definition) (module.Catalog, error) {
	descriptors := make([]module.Descriptor, len(definitions))
	for index, definition := range definitions {
		descriptors[index] = definition.Descriptor
	}
	catalog, err := module.NewCatalog(descriptors)
	if err != nil {
		return module.Catalog{}, err
	}
	seenProbes := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		if definition.Probe == nil {
			return module.Catalog{}, fmt.Errorf("definition %d has nil probe", index)
		}
		probeID := definition.Probe.ID()
		if strings.TrimSpace(probeID) == "" {
			return module.Catalog{}, fmt.Errorf("definition %d has empty probe ID", index)
		}
		if _, exists := seenProbes[probeID]; exists {
			return module.Catalog{}, fmt.Errorf("duplicate probe ID %q", probeID)
		}
		if definition.Descriptor.ID != probeID {
			return module.Catalog{}, fmt.Errorf("definition %d descriptor ID %q does not match probe ID %q", index, definition.Descriptor.ID, probeID)
		}
		seenProbes[probeID] = struct{}{}
	}
	return catalog, nil
}

func copyDefinitions(definitions []Definition) []Definition {
	descriptors := make([]module.Descriptor, len(definitions))
	for index, definition := range definitions {
		descriptors[index] = definition.Descriptor
	}
	catalog, err := module.NewCatalog(descriptors)
	if err != nil {
		panic(fmt.Sprintf("invalid built-in module catalog copy: %v", err))
	}
	result := make([]Definition, len(definitions))
	for index, definition := range definitions {
		descriptor, ok := catalog.Lookup(definition.Descriptor.ID)
		if !ok {
			panic(fmt.Sprintf("missing copied built-in descriptor %q", definition.Descriptor.ID))
		}
		result[index] = Definition{Descriptor: descriptor, Probe: definition.Probe}
	}
	return result
}

func moduleDescriptor(id string, standard bool, exposure module.Exposure, needsEgress bool, concurrency module.Concurrency, methodology model.Methodology, tools []string, estimate time.Duration, wizard ...string) module.Descriptor {
	return moduleDescriptorWithEstimateMode(id, standard, exposure, needsEgress, concurrency, methodology, tools, estimate, module.EstimateModeFixed, wizard...)
}

func withRetryOnInterference(descriptor module.Descriptor) module.Descriptor {
	descriptor.RetryOnInterference = true
	return descriptor
}

func moduleDescriptorWithPrivacyNotice(id string, standard bool, exposure module.Exposure, needsEgress bool, concurrency module.Concurrency, methodology model.Methodology, tools []string, estimate time.Duration, noticeKey string, wizard ...string) module.Descriptor {
	descriptor := moduleDescriptor(id, standard, exposure, needsEgress, concurrency, methodology, tools, estimate, wizard...)
	descriptor.PrivacyNoticeKey = noticeKey
	return descriptor
}

func moduleDescriptorWithEstimateMode(id string, standard bool, exposure module.Exposure, needsEgress bool, concurrency module.Concurrency, methodology model.Methodology, tools []string, estimate time.Duration, estimateMode module.EstimateMode, wizard ...string) module.Descriptor {
	descriptor := module.Descriptor{
		ID:              id,
		ProfileStandard: standard,
		Exposure:        exposure,
		NeedsEgressIP:   needsEgress,
		Concurrency:     concurrency,
		Methodology:     methodology,
		RequiredTools:   tools,
		TitleKey:        "module." + id + ".title",
		DescriptionKey:  "module." + id + ".desc",
		Estimate:        estimate,
		EstimateMode:    estimateMode,
	}
	if len(wizard) > 0 {
		descriptor.WizardGroup = wizard[0]
	}
	if len(wizard) > 1 {
		descriptor.WizardQuestionKey = wizard[1]
	}
	return descriptor
}
