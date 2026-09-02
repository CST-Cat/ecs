package config

import (
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/module"
)

// moduleDescriptors is the single ordered registry.  Keep this order stable:
// it is both the full-profile order and the order shown by the CLI/report.
var moduleDescriptors = []module.Descriptor{
	moduleDescriptor("system", true, module.ExposureLocal, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "inventory", Label: "methodology.inventory", Engine: "OS/runtime inspection", Profile: "probe.system.profile", ComparisonScope: "probe.system.comparison_scope"},
		nil, time.Second),
	moduleDescriptor("network", false, module.ExposureThirdParty, true, module.ConcurrencyProbe,
		model.Methodology{Kind: "provider-assessment", Label: "methodology.provider-assessment", Engine: "multi-provider IP intelligence", Profile: "probe.network.profile", ComparisonScope: "probe.network.comparison_scope"},
		nil, 5*time.Second, "ipquality", "wizard.askIPQuality"),
	moduleDescriptor("bgp", true, module.ExposurePublic, true, module.ConcurrencyProbe,
		model.Methodology{Kind: "provider-assessment", Label: "methodology.provider-assessment", Engine: "RouteViews current RIB API", Profile: "probe.bgp.profile", ComparisonScope: "probe.bgp.comparison_scope"},
		nil, 4*time.Second),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("cpu", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "sysbench", Profile: "probe.cpu.profile", ComparisonScope: "probe.cpu.comparison_scope"},
		[]string{"sysbench"}, 3*time.Second, module.EstimateModeCPU)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("zstd", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "zstd", Profile: "probe.zstd.profile", ComparisonScope: "probe.zstd.comparison_scope"},
		[]string{"zstd"}, 25*time.Second, module.EstimateModeTwoContext)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("npb", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "NASA NPB-OMP", Profile: "probe.npb.profile", ComparisonScope: "probe.npb.comparison_scope"},
		[]string{"npb-ep", "npb-ft"}, 60*time.Second, module.EstimateModeTwoContext)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("memory", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "STREAM", Profile: "probe.memory.stream.profile", ComparisonScope: "probe.memory.comparison_scope"},
		[]string{"stream"}, 5*time.Second, module.EstimateModeMemory)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("crypto", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "OpenSSL speed", Profile: "probe.crypto.profile", ComparisonScope: "probe.crypto.comparison_scope"},
		[]string{"openssl"}, 45*time.Second, module.EstimateModeTwoContext)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("disk", true, module.ExposureLocal, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "fio", Profile: "probe.disk.profile", ComparisonScope: "probe.disk.comparison_scope"},
		[]string{"fio"}, 8*time.Second, module.EstimateModeDisk)),
	moduleDescriptorWithEstimateMode("dns", true, module.ExposurePublic, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "DNS/UDP", Profile: "probe.dns.profile", ComparisonScope: "probe.dns.comparison_scope"},
		nil, 8*time.Second, module.EstimateModeDNS),
	moduleDescriptorWithEstimateMode("latency", true, module.ExposurePublic, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "TCP connect", Profile: "probe.latency.profile", ComparisonScope: "probe.latency.comparison_scope"},
		[]string{"ping"}, 15*time.Second, module.EstimateModeLatency),
	moduleDescriptorWithEstimateMode("speed", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "iperf3", Profile: "probe.speed.profile", ComparisonScope: "probe.speed.comparison_scope"},
		[]string{"iperf3"}, 30*time.Second, module.EstimateModeSpeed, "throughput", "wizard.askThroughput"),
	moduleDescriptor("ports", true, module.ExposurePublic, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "TCP connect", Profile: "probe.ports.profile", ComparisonScope: "probe.ports.comparison_scope"},
		nil, 4*time.Second),
	moduleDescriptor("nat", true, module.ExposurePublic, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "STUN (RFC 5389/5780)", Profile: "probe.nat.profile", ComparisonScope: "probe.nat.comparison_scope"},
		nil, 12*time.Second),
	moduleDescriptor("blacklist", true, module.ExposurePublic, true, module.ConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "DNSBL over DNS A lookup", Profile: "probe.blacklist.profile", ComparisonScope: "probe.blacklist.comparison_scope"},
		nil, 10*time.Second, "blacklist", "wizard.askBlacklist"),
	moduleDescriptor("apps", true, module.ExposurePublic, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "native TCP connect", Profile: "probe.apps.profile", ComparisonScope: "probe.apps.comparison_scope"},
		nil, 8*time.Second),
	moduleDescriptor("cnspeed", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "HTTP download against speedtest.cn nodes", Profile: "probe.cnspeed.profile", ComparisonScope: "probe.cnspeed.comparison_scope"},
		nil, 40*time.Second, "throughput", "wizard.askThroughput"),
	moduleDescriptorWithPrivacyNotice("ookla", false, module.ExposureThirdParty, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "official Ookla Speedtest CLI", Profile: "probe.ookla.profile", ComparisonScope: "probe.ookla.comparison_scope"},
		[]string{"speedtest"}, 90*time.Second,
		"message.notice.ooklaPrivacy"),
	moduleDescriptor("media", true, module.ExposurePublic, false, module.ConcurrencyProbe,
		model.Methodology{Kind: "heuristic", Label: "methodology.heuristic", Engine: "public HTTP evidence", Profile: "probe.media.profile", ComparisonScope: "probe.media.comparison_scope"},
		nil, 10*time.Second, "media", "wizard.askMedia"),
	moduleDescriptorWithEstimateMode("route", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "NextTrace Tiny", Profile: "probe.route.profile", ComparisonScope: "probe.route.comparison_scope"},
		[]string{"nexttrace-tiny"}, 36*time.Second, module.EstimateModeRoute, "routing", "wizard.askRouting"),
	moduleDescriptor("backtrace", true, module.ExposurePublic, false, module.ConcurrencyExclusive,
		model.Methodology{Kind: "heuristic", Label: "methodology.heuristic", Engine: "probe.backtrace.methodology.engine", Profile: "probe.backtrace.profile", ComparisonScope: "probe.backtrace.comparison_scope"},
		[]string{"nexttrace-tiny"}, 30*time.Second, "routing", "wizard.askRouting"),
}

func moduleDescriptor(id string, standard bool, exposure module.Exposure, needsEgress bool, concurrency module.Concurrency, methodology model.Methodology, tools []string, estimate time.Duration, wizard ...string) module.Descriptor {
	return moduleDescriptorWithEstimateMode(id, standard, exposure, needsEgress, concurrency, methodology, tools, estimate, module.EstimateModeFixed, wizard...)
}

func withRetryOnInterference(descriptor module.Descriptor) module.Descriptor {
	descriptor.RetryOnInterference = true
	return descriptor
}

// moduleDescriptorWithPrivacyNotice keeps the common descriptor constructor
// convenient while allowing modules with an independent external privacy
// policy to opt into a report-level notice.
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

// ModuleCatalog validates the retained built-in descriptor data through the
// low-level module constructor. There is no config init hook: callers receive
// the immutable catalog value and its defensive-copy read methods.
func ModuleCatalog() module.Catalog {
	catalog, err := module.NewCatalog(moduleDescriptors)
	if err != nil {
		panic(err)
	}
	return catalog
}

// ModuleDescriptors returns the canonical descriptors in execution order.
// The module catalog detaches all reference-typed metadata before returning.
func ModuleDescriptors() []module.Descriptor {
	return ModuleCatalog().Descriptors()
}

// ModuleDescriptorFor returns one descriptor by ID. The returned descriptor is
// a defensive copy owned by the module catalog.
func ModuleDescriptorFor(id string) (module.Descriptor, bool) {
	return ModuleCatalog().Lookup(id)
}

// ModuleIDs returns all descriptor IDs in canonical order.
func ModuleIDs() []string {
	return ModuleCatalog().IDs()
}

// ValidateModuleSelection checks explicit --only/--skip IDs against the
// canonical module catalog. SelectModules intentionally remains a set
// operation and therefore assumes its inputs have already passed this
// boundary.
func ValidateModuleSelection(only, skip []string) error {
	catalog := ModuleCatalog()
	for _, ids := range [][]string{only, skip} {
		for _, id := range ids {
			if _, ok := catalog.Lookup(id); !ok {
				return i18n.Errorf("err.unknownModule", id)
			}
		}
	}
	return nil
}

// ModulesForProfile returns the module IDs selected by a profile in canonical
// order.  Unknown profiles return nil; Defaults remains responsible for
// producing the localized validation error used by the CLI.
func ModulesForProfile(profile string) []string {
	catalog := ModuleCatalog()
	switch profile {
	case ProfileStandard:
		return catalog.StandardIDs()
	case ProfileFull:
		return catalog.IDs()
	default:
		return nil
	}
}
