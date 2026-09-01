package config

import (
	"fmt"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

// ModuleConcurrency describes the amount of interference a module can cause
// when it runs alongside another module. Runner reads descriptor.Concurrency
// directly; keeping the value here avoids a second module table in runner.
type ModuleConcurrency string

const (
	ModuleConcurrencyExclusive ModuleConcurrency = "exclusive"
	ModuleConcurrencyProbe     ModuleConcurrency = "probe"
)

// EstimateMode identifies the generic shape of a module's runtime estimate.
// Fixed estimates use the descriptor's Estimate value; the remaining modes
// derive their duration from the runtime settings that control the probe.
// Probe owns the complete estimate calculation, including workload-specific
// plans whose descriptor keeps the ordinary fixed default.
type EstimateMode string

const (
	EstimateModeFixed      EstimateMode = "fixed"
	EstimateModeTwoContext EstimateMode = "two_context"
	EstimateModeCPU        EstimateMode = "cpu"
	EstimateModeMemory     EstimateMode = "memory"
	EstimateModeDisk       EstimateMode = "disk"
	EstimateModeDNS        EstimateMode = "dns"
	EstimateModeLatency    EstimateMode = "latency"
	EstimateModeSpeed      EstimateMode = "speed"
	EstimateModeRoute      EstimateMode = "route"
)

// ModuleDescriptor is the canonical description of a module.
//
// Every cross-cutting module property belongs here: selection order and
// profile membership, exposure policy, scheduler class, methodology and the
// metadata used by list/doctor/score integrations. Probe constructors live in
// the probe package's typed built-in list; config only owns module metadata.
type ModuleDescriptor struct {
	ID string

	// ProfileStandard marks the default 19-module shortcut. The full profile is
	// the complete descriptor list; --only changes the selected set at runtime
	// and does not define a third profile or descriptor category.
	ProfileStandard bool

	Exposure      Exposure
	NeedsEgressIP bool
	Concurrency   ModuleConcurrency
	// RetryOnInterference marks benchmark modules whose result may be retried
	// once when the host snapshot shows concurrent-load interference. The
	// runner consumes this descriptor field rather than maintaining a module
	// ID policy of its own.
	RetryOnInterference bool

	Methodology model.Methodology

	// RequiredTools lists tools relevant to the module.  The route probes have a
	// single execution contract: NextTrace Tiny. This metadata is also consumed by
	// doctor and the wrapper dependency planner.
	RequiredTools []string

	TitleKey       string
	DescriptionKey string
	// PrivacyNoticeKey is an optional stable presentation key for modules whose
	// external client or service has an independent privacy/data-processing
	// policy. Empty means that the module needs no additional notice.
	PrivacyNoticeKey string
	// WizardGroup and WizardQuestionKey describe the optional interactive
	// switch for costly/privacy-sensitive modules. Empty means the module is
	// always included by the wizard and is not individually asked about.
	WizardGroup       string
	WizardQuestionKey string

	// Estimate is a rough standalone duration used by lightweight integrations.
	// probe.EstimateFor remains authoritative for runtime-sensitive estimates
	// (attempt counts, target counts and benchmark durations).
	Estimate     time.Duration
	EstimateMode EstimateMode
}

// moduleDescriptors is the single ordered registry.  Keep this order stable:
// it is both the full-profile order and the order shown by the CLI/report.
var moduleDescriptors = []ModuleDescriptor{
	moduleDescriptor("system", true, ExposureLocal, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "inventory", Label: "methodology.inventory", Engine: "OS/runtime inspection", Profile: "probe.system.profile", ComparisonScope: "probe.system.comparison_scope"},
		nil, time.Second),
	moduleDescriptor("network", false, ExposureThirdParty, true, ModuleConcurrencyProbe,
		model.Methodology{Kind: "provider-assessment", Label: "methodology.provider-assessment", Engine: "multi-provider IP intelligence", Profile: "probe.network.profile", ComparisonScope: "probe.network.comparison_scope"},
		nil, 5*time.Second, "ipquality", "wizard.askIPQuality"),
	moduleDescriptor("bgp", true, ExposurePublic, true, ModuleConcurrencyProbe,
		model.Methodology{Kind: "provider-assessment", Label: "methodology.provider-assessment", Engine: "RouteViews current RIB API", Profile: "probe.bgp.profile", ComparisonScope: "probe.bgp.comparison_scope"},
		nil, 4*time.Second),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("cpu", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "sysbench", Profile: "probe.cpu.profile", ComparisonScope: "probe.cpu.comparison_scope"},
		[]string{"sysbench"}, 3*time.Second, EstimateModeCPU)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("zstd", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "zstd", Profile: "probe.zstd.profile", ComparisonScope: "probe.zstd.comparison_scope"},
		[]string{"zstd"}, 25*time.Second, EstimateModeTwoContext)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("npb", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "NASA NPB-OMP", Profile: "probe.npb.profile", ComparisonScope: "probe.npb.comparison_scope"},
		[]string{"npb-ep", "npb-ft"}, 60*time.Second, EstimateModeTwoContext)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("memory", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "STREAM", Profile: "probe.memory.stream.profile", ComparisonScope: "probe.memory.comparison_scope"},
		[]string{"stream"}, 5*time.Second, EstimateModeMemory)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("crypto", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "OpenSSL speed", Profile: "probe.crypto.profile", ComparisonScope: "probe.crypto.comparison_scope"},
		[]string{"openssl"}, 45*time.Second, EstimateModeTwoContext)),
	withRetryOnInterference(moduleDescriptorWithEstimateMode("disk", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "fio", Profile: "probe.disk.profile", ComparisonScope: "probe.disk.comparison_scope"},
		[]string{"fio"}, 8*time.Second, EstimateModeDisk)),
	moduleDescriptorWithEstimateMode("dns", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "DNS/UDP", Profile: "probe.dns.profile", ComparisonScope: "probe.dns.comparison_scope"},
		nil, 8*time.Second, EstimateModeDNS),
	moduleDescriptorWithEstimateMode("latency", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "TCP connect", Profile: "probe.latency.profile", ComparisonScope: "probe.latency.comparison_scope"},
		[]string{"ping"}, 15*time.Second, EstimateModeLatency),
	moduleDescriptorWithEstimateMode("speed", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "methodology.standard-benchmark", Engine: "iperf3", Profile: "probe.speed.profile", ComparisonScope: "probe.speed.comparison_scope"},
		[]string{"iperf3"}, 30*time.Second, EstimateModeSpeed, "throughput", "wizard.askThroughput"),
	moduleDescriptor("ports", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "TCP connect", Profile: "probe.ports.profile", ComparisonScope: "probe.ports.comparison_scope"},
		nil, 4*time.Second),
	moduleDescriptor("nat", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "STUN (RFC 5389/5780)", Profile: "probe.nat.profile", ComparisonScope: "probe.nat.comparison_scope"},
		nil, 12*time.Second),
	moduleDescriptor("blacklist", true, ExposurePublic, true, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "DNSBL over DNS A lookup", Profile: "probe.blacklist.profile", ComparisonScope: "probe.blacklist.comparison_scope"},
		nil, 10*time.Second, "blacklist", "wizard.askBlacklist"),
	moduleDescriptor("apps", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "native TCP connect", Profile: "probe.apps.profile", ComparisonScope: "probe.apps.comparison_scope"},
		nil, 8*time.Second),
	moduleDescriptor("cnspeed", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "HTTP download against speedtest.cn nodes", Profile: "probe.cnspeed.profile", ComparisonScope: "probe.cnspeed.comparison_scope"},
		nil, 40*time.Second, "throughput", "wizard.askThroughput"),
	moduleDescriptorWithPrivacyNotice("ookla", false, ExposureThirdParty, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "official Ookla Speedtest CLI", Profile: "probe.ookla.profile", ComparisonScope: "probe.ookla.comparison_scope"},
		[]string{"speedtest"}, 90*time.Second,
		"message.notice.ooklaPrivacy"),
	moduleDescriptor("media", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "heuristic", Label: "methodology.heuristic", Engine: "public HTTP evidence", Profile: "probe.media.profile", ComparisonScope: "probe.media.comparison_scope"},
		nil, 10*time.Second, "media", "wizard.askMedia"),
	moduleDescriptorWithEstimateMode("route", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "methodology.protocol-measurement", Engine: "NextTrace Tiny", Profile: "probe.route.profile", ComparisonScope: "probe.route.comparison_scope"},
		[]string{"nexttrace-tiny"}, 36*time.Second, EstimateModeRoute, "routing", "wizard.askRouting"),
	moduleDescriptor("backtrace", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "heuristic", Label: "methodology.heuristic", Engine: "probe.backtrace.methodology.engine", Profile: "probe.backtrace.profile", ComparisonScope: "probe.backtrace.comparison_scope"},
		[]string{"nexttrace-tiny"}, 30*time.Second, "routing", "wizard.askRouting"),
}

func moduleDescriptor(id string, standard bool, exposure Exposure, needsEgress bool, concurrency ModuleConcurrency, methodology model.Methodology, tools []string, estimate time.Duration, wizard ...string) ModuleDescriptor {
	return moduleDescriptorWithEstimateMode(id, standard, exposure, needsEgress, concurrency, methodology, tools, estimate, EstimateModeFixed, wizard...)
}

func withRetryOnInterference(descriptor ModuleDescriptor) ModuleDescriptor {
	descriptor.RetryOnInterference = true
	return descriptor
}

// moduleDescriptorWithPrivacyNotice keeps the common descriptor constructor
// convenient while allowing modules with an independent external privacy
// policy to opt into a report-level notice.
func moduleDescriptorWithPrivacyNotice(id string, standard bool, exposure Exposure, needsEgress bool, concurrency ModuleConcurrency, methodology model.Methodology, tools []string, estimate time.Duration, noticeKey string, wizard ...string) ModuleDescriptor {
	descriptor := moduleDescriptor(id, standard, exposure, needsEgress, concurrency, methodology, tools, estimate, wizard...)
	descriptor.PrivacyNoticeKey = noticeKey
	return descriptor
}

func moduleDescriptorWithEstimateMode(id string, standard bool, exposure Exposure, needsEgress bool, concurrency ModuleConcurrency, methodology model.Methodology, tools []string, estimate time.Duration, estimateMode EstimateMode, wizard ...string) ModuleDescriptor {
	descriptor := ModuleDescriptor{
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

// ModuleDescriptors returns the canonical descriptors in execution order.
// Slices are copied so callers cannot mutate the registry through metadata.
func ModuleDescriptors() []ModuleDescriptor {
	out := make([]ModuleDescriptor, len(moduleDescriptors))
	for i, descriptor := range moduleDescriptors {
		out[i] = descriptor
		out[i].RequiredTools = append([]string(nil), descriptor.RequiredTools...)
	}
	return out
}

// ModuleDescriptorFor returns one descriptor by ID.  The returned value is a
// copy and is safe for callers to modify.
func ModuleDescriptorFor(id string) (ModuleDescriptor, bool) {
	for _, descriptor := range moduleDescriptors {
		if descriptor.ID == id {
			descriptor.RequiredTools = append([]string(nil), descriptor.RequiredTools...)
			return descriptor, true
		}
	}
	return ModuleDescriptor{}, false
}

// ModuleIDs returns all registered IDs in canonical order.
func ModuleIDs() []string {
	out := make([]string, 0, len(moduleDescriptors))
	for _, descriptor := range moduleDescriptors {
		out = append(out, descriptor.ID)
	}
	return out
}

// ValidateModuleSelection checks explicit --only/--skip IDs against the
// canonical module catalog. SelectModules intentionally remains a set
// operation and therefore assumes its inputs have already passed this
// boundary.
func ValidateModuleSelection(only, skip []string) error {
	known := make(map[string]struct{}, len(moduleDescriptors))
	for _, descriptor := range moduleDescriptors {
		known[descriptor.ID] = struct{}{}
	}
	for _, ids := range [][]string{only, skip} {
		for _, id := range ids {
			if _, ok := known[id]; !ok {
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
	var include func(ModuleDescriptor) bool
	switch profile {
	case ProfileStandard:
		include = func(descriptor ModuleDescriptor) bool { return descriptor.ProfileStandard }
	case ProfileFull:
		// Full is the complete canonical descriptor list. There is no second
		// per-module flag that can drift from this registry.
		include = func(ModuleDescriptor) bool { return true }
	default:
		return nil
	}
	out := make([]string, 0, len(moduleDescriptors))
	for _, descriptor := range moduleDescriptors {
		if include(descriptor) {
			out = append(out, descriptor.ID)
		}
	}
	return out
}

// validateModuleDescriptors checks invariants shared by config, probe and
// runner. init runs it at startup so an incompletely added module fails the
// process immediately rather than at the moment that module executes.
func validateModuleDescriptors() error {
	seen := make(map[string]bool, len(moduleDescriptors))
	for index, descriptor := range moduleDescriptors {
		if descriptor.ID == "" {
			return fmt.Errorf("module descriptor %d has empty ID", index)
		}
		if seen[descriptor.ID] {
			return fmt.Errorf("duplicate module descriptor %q", descriptor.ID)
		}
		seen[descriptor.ID] = true
		if descriptor.Concurrency != ModuleConcurrencyExclusive && descriptor.Concurrency != ModuleConcurrencyProbe {
			return fmt.Errorf("module %q has unknown concurrency class %q", descriptor.ID, descriptor.Concurrency)
		}
		switch descriptor.EstimateMode {
		case EstimateModeFixed, EstimateModeTwoContext, EstimateModeCPU, EstimateModeMemory, EstimateModeDisk,
			EstimateModeDNS, EstimateModeLatency, EstimateModeSpeed, EstimateModeRoute:
		default:
			return fmt.Errorf("module %q has unknown estimate mode %q", descriptor.ID, descriptor.EstimateMode)
		}
		if descriptor.Methodology.Kind == "" || descriptor.Methodology.Label == "" || descriptor.Methodology.Engine == "" {
			return fmt.Errorf("module %q has incomplete methodology", descriptor.ID)
		}
		if descriptor.TitleKey == "" || descriptor.DescriptionKey == "" {
			return fmt.Errorf("module %q has incomplete display metadata", descriptor.ID)
		}
		if descriptor.WizardGroup != "" && descriptor.WizardQuestionKey == "" {
			return fmt.Errorf("module %q has a wizard group without a question key", descriptor.ID)
		}
	}
	return nil
}

func init() {
	if err := validateModuleDescriptors(); err != nil {
		panic(err)
	}
}
