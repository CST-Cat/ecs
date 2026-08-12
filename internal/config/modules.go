package config

import (
	"fmt"
	"sync"
	"time"

	"ecs/internal/model"
)

// ModuleConcurrency describes the amount of interference a module can cause
// when it runs alongside another module.  Runner maps these values to its
// private scheduler type; keeping the value here avoids a second module table
// in runner.
type ModuleConcurrency string

// ModuleFactory is the late-bound constructor used by the probe package.
// Keeping the function's return type as any avoids a config↔probe import cycle;
// probe registers concrete constructors at package runtime and verifies the
// result implements its Probe interface.
type ModuleFactory func() any

// ModuleEstimate is a late-bound runtime estimate supplied by the package that
// owns the workload.  It exists for the same reason as ModuleFactory: probe
// imports config, so config cannot ask probe how long a job plan takes.  Without
// it the duration would have to be duplicated here and would silently drift
// from the plan that actually runs.
type ModuleEstimate func(Runtime) time.Duration

const (
	ModuleConcurrencyExclusive ModuleConcurrency = "exclusive"
	ModuleConcurrencyProbe     ModuleConcurrency = "probe"

	// Short aliases are kept for callers that prefer the class terminology used
	// by runner's scheduler.
	ModuleClassExclusive = ModuleConcurrencyExclusive
	ModuleClassProbe     = ModuleConcurrencyProbe
)

// EstimateMode identifies how a module's runtime estimate is calculated.
// Fixed estimates use the descriptor's Estimate value; the remaining modes
// derive their duration from the runtime settings that control the probe.
// Keeping this policy in the descriptor registry prevents estimate callers
// from having to maintain a second module-ID switch.
type EstimateMode string

const (
	EstimateModeFixed   EstimateMode = "fixed"
	EstimateModeCPU     EstimateMode = "cpu"
	EstimateModeMemory  EstimateMode = "memory"
	EstimateModeDisk    EstimateMode = "disk"
	EstimateModeDNS     EstimateMode = "dns"
	EstimateModeLatency EstimateMode = "latency"
	EstimateModeSpeed   EstimateMode = "speed"
	EstimateModeRoute   EstimateMode = "route"
)

// ModuleDescriptor is the canonical description of a module.
//
// Every cross-cutting module property belongs here: selection order and
// profile membership, exposure policy, scheduler class, methodology and the
// metadata used by list/doctor/score integrations.  Probe constructors are
// late-bound through ProbeFactory so config remains independent of probe while
// the descriptor still exposes the complete module contract at runtime.
type ModuleDescriptor struct {
	ID string
	// ProbeFactory is populated when package probe registers its concrete
	// implementation. It is nil when callers inspect config in isolation.
	ProbeFactory ModuleFactory

	// Profile membership is explicit instead of inferred from an exclusion
	// list. ProfileFull is true for every default module (21 today), while
	// ProfileStandard marks the default 19-module shortcut. Explicit-only
	// modules, if introduced, remain reachable through --only without entering
	// either preset.
	ProfileStandard     bool
	ProfileFull         bool
	ProfileExplicitOnly bool

	Exposure Exposure
	// NeedsNetwork distinguishes a module that actively touches a remote
	// endpoint from local inventory/benchmark work.  It is derived from the
	// exposure level for the built-in catalog and remains explicit metadata for
	// callers that need to gate offline execution.
	NeedsNetwork  bool
	NeedsEgressIP bool
	Concurrency   ModuleConcurrency

	Methodology model.Methodology

	// Score metadata is intentionally descriptive.  The score package owns
	// metric definitions and algorithms; these fields let callers tell which
	// modules contribute a score without introducing a score/config cycle.
	ScoreEnabled bool
	ScoreKey     string

	// RequiredTools lists tools relevant to the module.  The route probes have a
	// single execution contract: NextTrace Tiny. This metadata is also consumed by
	// doctor and the wrapper dependency planner.
	RequiredTools []string

	TitleKey       string
	DescriptionKey string
	// PrivacyNotice is an optional report-level notice for modules whose
	// external client or service has an independent privacy/data-processing
	// policy.  Empty means that the module needs no additional notice.
	PrivacyNotice string
	// WizardGroup and WizardQuestionKey describe the optional interactive
	// switch for costly/privacy-sensitive modules. Empty means the module is
	// always included by the wizard and is not individually asked about.
	WizardGroup       string
	WizardQuestionKey string
	WizardDefaultOn   bool

	// Estimate is a rough standalone duration used by lightweight integrations.
	// Config.EstimateFor remains authoritative for runtime-sensitive estimates
	// (attempt counts, target counts and benchmark durations).
	Estimate     time.Duration
	EstimateMode EstimateMode
}

// moduleDescriptors is the single ordered registry.  Keep this order stable:
// it is both the full-profile order and the order shown by the CLI/report.
var moduleDescriptors = []ModuleDescriptor{
	moduleDescriptor("system", true, ExposureLocal, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "inventory", Label: "事实采集", Engine: "OS/runtime inspection", ComparisonScope: "资源快照；不是性能基准"},
		false, "", nil, time.Second),
	moduleDescriptor("network", false, ExposureThirdParty, true, ModuleConcurrencyProbe,
		model.Methodology{Kind: "provider-assessment", Label: "第三方评估", Engine: "multi-provider IP intelligence", ComparisonScope: "不同供应商分数不可直接混算"},
		false, "", nil, 5*time.Second, "ipquality", "wizard.askIPQuality"),
	moduleDescriptor("bgp", true, ExposurePublic, true, ModuleConcurrencyProbe,
		model.Methodology{Kind: "provider-assessment", Label: "第三方评估", Engine: "RouteViews current RIB API", ComparisonScope: "当前公共观测；不是私有互联全图或历史 BGP 分析"},
		false, "", nil, 4*time.Second),
	moduleDescriptorWithEstimateMode("cpu", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "sysbench", Profile: "cpu prime=20000"},
		true, "cpu", []string{"sysbench"}, 3*time.Second, EstimateModeCPU),
	moduleDescriptor("zstd", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "zstd", Profile: "Silesia v1 · level=3 · 1T/NT"},
		false, "", []string{"zstd"}, 25*time.Second),
	moduleDescriptor("npb", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "NASA NPB-OMP", Profile: "NPB 3.4.4 · EP + FT · Class A · 1T/NT"},
		false, "", []string{"npb-ep", "npb-ft"}, 60*time.Second),
	moduleDescriptorWithEstimateMode("memory", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "STREAM", Profile: "Copy/Scale/Add/Triad × 1T/NT"},
		true, "memory", []string{"stream"}, 5*time.Second, EstimateModeMemory),
	moduleDescriptor("crypto", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "OpenSSL speed", Profile: "OpenSSL 3.5.7 · AES-256-GCM/ChaCha20-Poly1305/SHA-256 · 16 KiB · 1W/NW"},
		false, "", []string{"openssl"}, 45*time.Second),
	moduleDescriptorWithEstimateMode("disk", true, ExposureLocal, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "fio", Profile: "Direct I/O"},
		true, "disk", []string{"fio"}, 8*time.Second, EstimateModeDisk),
	moduleDescriptorWithEstimateMode("dns", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "DNS/UDP", ComparisonScope: "现场诊断；不是基准分"},
		false, "", nil, 8*time.Second, EstimateModeDNS),
	moduleDescriptorWithEstimateMode("latency", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "TCP connect", ComparisonScope: "现场诊断；不是 ICMP ping"},
		false, "", []string{"ping"}, 15*time.Second, EstimateModeLatency),
	moduleDescriptorWithEstimateMode("speed", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "iperf3", Profile: "TCP multi-stream forward/reverse + UDP 50M/5s"},
		true, "bandwidth", []string{"iperf3"}, 30*time.Second, EstimateModeSpeed, "throughput", "wizard.askThroughput"),
	moduleDescriptor("ports", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "TCP connect", ComparisonScope: "可达性诊断；不是性能基准"},
		false, "", nil, 4*time.Second),
	moduleDescriptor("nat", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "STUN (RFC 5389/5780)", ComparisonScope: "当次 UDP 路径上的 NAT 行为；不代表 TCP"},
		false, "", nil, 12*time.Second),
	moduleDescriptor("blacklist", true, ExposurePublic, true, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "DNSBL over DNS A lookup", ComparisonScope: "各名单收录标准不同，不可合并计分"},
		false, "", nil, 10*time.Second, "blacklist", "wizard.askBlacklist"),
	moduleDescriptor("apps", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "native TCP connect", ComparisonScope: "网络可达性诊断；不代表服务本身可用"},
		false, "", nil, 8*time.Second),
	moduleDescriptor("cnspeed", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "HTTP download against speedtest.cn nodes", ComparisonScope: "到具体节点的 HTTP 带宽；不代表到该运营商全网"},
		false, "", nil, 40*time.Second, "throughput", "wizard.askThroughput"),
	moduleDescriptorWithPrivacyNotice("ookla", false, ExposureThirdParty, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "协议测量", Engine: "official Ookla Speedtest CLI", ComparisonScope: "外部服务测量；Ookla 的条款与数据处理独立于 ecs"},
		false, "", []string{"speedtest"}, 90*time.Second,
		"Ookla 调用外部测速客户端；Ookla 可能独立处理测量元数据，这不属于 ecs 的本地零上传保证。"),
	moduleDescriptor("media", true, ExposurePublic, false, ModuleConcurrencyProbe,
		model.Methodology{Kind: "heuristic", Label: "启发式判断", Engine: "public HTTP evidence", ComparisonScope: "不等同账号播放、注册或支付能力"},
		false, "", nil, 10*time.Second, "media", "wizard.askMedia"),
	moduleDescriptorWithEstimateMode("route", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "protocol-measurement", Label: "协议诊断", Engine: "NextTrace Tiny", ComparisonScope: "正向路径快照；不是性能基准"},
		false, "", []string{"nexttrace-tiny"}, 36*time.Second, EstimateModeRoute, "routing", "wizard.askRouting"),
	moduleDescriptor("backtrace", true, ExposurePublic, false, ModuleConcurrencyExclusive,
		model.Methodology{Kind: "heuristic", Label: "启发式判断", Engine: "NextTrace Tiny + 骨干网段特征表", ComparisonScope: "路径特征推断；不是反向抓包"},
		false, "", []string{"nexttrace-tiny"}, 30*time.Second, "routing", "wizard.askRouting"),
}

var (
	moduleFactoriesMu sync.RWMutex
	moduleFactories   = map[string]ModuleFactory{}
	moduleEstimatesMu sync.RWMutex
	moduleEstimates   = map[string]ModuleEstimate{}
)

// RegisterModuleEstimate attaches a runtime estimate to a module.  Registration
// is idempotent and safe to call from a package init.
func RegisterModuleEstimate(id string, estimate ModuleEstimate) error {
	if estimate == nil {
		return fmt.Errorf("module %q has a nil estimate", id)
	}
	if _, ok := ModuleDescriptorFor(id); !ok {
		return fmt.Errorf("module estimate %q is not present in the descriptor registry", id)
	}
	moduleEstimatesMu.Lock()
	moduleEstimates[id] = estimate
	moduleEstimatesMu.Unlock()
	return nil
}

func moduleEstimateFor(id string) ModuleEstimate {
	moduleEstimatesMu.RLock()
	estimate := moduleEstimates[id]
	moduleEstimatesMu.RUnlock()
	return estimate
}

func moduleDescriptor(id string, standard bool, exposure Exposure, needsEgress bool, concurrency ModuleConcurrency, methodology model.Methodology, scoreEnabled bool, scoreKey string, tools []string, estimate time.Duration, wizard ...string) ModuleDescriptor {
	return moduleDescriptorWithEstimateMode(id, standard, exposure, needsEgress, concurrency, methodology, scoreEnabled, scoreKey, tools, estimate, EstimateModeFixed, wizard...)
}

// moduleDescriptorWithPrivacyNotice keeps the common descriptor constructor
// convenient while allowing modules with an independent external privacy
// policy to opt into a report-level notice.
func moduleDescriptorWithPrivacyNotice(id string, standard bool, exposure Exposure, needsEgress bool, concurrency ModuleConcurrency, methodology model.Methodology, scoreEnabled bool, scoreKey string, tools []string, estimate time.Duration, notice string, wizard ...string) ModuleDescriptor {
	descriptor := moduleDescriptor(id, standard, exposure, needsEgress, concurrency, methodology, scoreEnabled, scoreKey, tools, estimate, wizard...)
	descriptor.PrivacyNotice = notice
	return descriptor
}

func moduleDescriptorWithEstimateMode(id string, standard bool, exposure Exposure, needsEgress bool, concurrency ModuleConcurrency, methodology model.Methodology, scoreEnabled bool, scoreKey string, tools []string, estimate time.Duration, estimateMode EstimateMode, wizard ...string) ModuleDescriptor {
	descriptor := ModuleDescriptor{
		ID:              id,
		ProfileStandard: standard,
		ProfileFull:     true,
		Exposure:        exposure,
		NeedsNetwork:    exposure > ExposureLocal,
		NeedsEgressIP:   needsEgress,
		Concurrency:     concurrency,
		Methodology:     methodology,
		ScoreEnabled:    scoreEnabled,
		ScoreKey:        scoreKey,
		RequiredTools:   tools,
		TitleKey:        "module." + id + ".title",
		DescriptionKey:  "module." + id + ".desc",
		Estimate:        estimate,
		EstimateMode:    estimateMode,
	}
	if len(wizard) > 0 {
		descriptor.WizardGroup = wizard[0]
		descriptor.WizardDefaultOn = true
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
		out[i].ProbeFactory = moduleFactoryFor(descriptor.ID)
	}
	return out
}

// ModuleDescriptorFor returns one descriptor by ID.  The returned value is a
// copy and is safe for callers to modify.
func ModuleDescriptorFor(id string) (ModuleDescriptor, bool) {
	for _, descriptor := range moduleDescriptors {
		if descriptor.ID == id {
			descriptor.RequiredTools = append([]string(nil), descriptor.RequiredTools...)
			descriptor.ProbeFactory = moduleFactoryFor(id)
			return descriptor, true
		}
	}
	return ModuleDescriptor{}, false
}

// RegisterModuleFactory attaches a concrete implementation constructor to the
// canonical descriptor. Registration is idempotent so Registry may safely
// ensure it has happened before reading descriptors.
func RegisterModuleFactory(id string, factory ModuleFactory) error {
	if factory == nil {
		return fmt.Errorf("module %q has a nil factory", id)
	}
	if _, ok := ModuleDescriptorFor(id); !ok {
		return fmt.Errorf("module factory %q is not present in the descriptor registry", id)
	}
	moduleFactoriesMu.Lock()
	moduleFactories[id] = factory
	moduleFactoriesMu.Unlock()
	return nil
}

func moduleFactoryFor(id string) ModuleFactory {
	moduleFactoriesMu.RLock()
	factory := moduleFactories[id]
	moduleFactoriesMu.RUnlock()
	return factory
}

// ModuleIDs returns all registered IDs in canonical order.
func ModuleIDs() []string {
	out := make([]string, 0, len(moduleDescriptors))
	for _, descriptor := range moduleDescriptors {
		out = append(out, descriptor.ID)
	}
	return out
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
		include = func(descriptor ModuleDescriptor) bool { return descriptor.ProfileFull }
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

// ValidateModuleDescriptors checks invariants shared by config, probe and
// runner.  Keeping this check public lets package-level tests and integrations
// fail close to the source when a new module is added incompletely.
func ValidateModuleDescriptors() error {
	seen := make(map[string]bool, len(moduleDescriptors))
	for index, descriptor := range moduleDescriptors {
		if descriptor.ID == "" {
			return fmt.Errorf("module descriptor %d has empty ID", index)
		}
		if seen[descriptor.ID] {
			return fmt.Errorf("duplicate module descriptor %q", descriptor.ID)
		}
		seen[descriptor.ID] = true
		if !descriptor.ProfileFull && !descriptor.ProfileStandard && !descriptor.ProfileExplicitOnly {
			return fmt.Errorf("module %q belongs to no profile", descriptor.ID)
		}
		if descriptor.ProfileStandard && !descriptor.ProfileFull {
			return fmt.Errorf("standard module %q is missing from full profile", descriptor.ID)
		}
		if descriptor.Concurrency != ModuleConcurrencyExclusive && descriptor.Concurrency != ModuleConcurrencyProbe {
			return fmt.Errorf("module %q has unknown concurrency class %q", descriptor.ID, descriptor.Concurrency)
		}
		switch descriptor.EstimateMode {
		case EstimateModeFixed, EstimateModeCPU, EstimateModeMemory, EstimateModeDisk,
			EstimateModeDNS, EstimateModeLatency, EstimateModeSpeed, EstimateModeRoute:
		default:
			return fmt.Errorf("module %q has unknown estimate mode %q", descriptor.ID, descriptor.EstimateMode)
		}
		if descriptor.Methodology.Kind == "" || descriptor.Methodology.Label == "" || descriptor.Methodology.Engine == "" {
			return fmt.Errorf("module %q has incomplete methodology", descriptor.ID)
		}
		if descriptor.ScoreEnabled && descriptor.ScoreKey == "" {
			return fmt.Errorf("score-enabled module %q has empty score key", descriptor.ID)
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

// ModuleOrder is derived from the canonical descriptor registry.
var ModuleOrder = ModuleIDs()

func init() {
	if err := ValidateModuleDescriptors(); err != nil {
		panic(err)
	}
}
