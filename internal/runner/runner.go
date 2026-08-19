package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/failure"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/probe"
)

type Phase string

const (
	PhaseStart Phase = "start"
	PhaseDone  Phase = "done"
)

type Progress struct {
	Phase  Phase
	Index  int
	Total  int
	ID     string
	Title  string
	Result model.Result
}

// ProgressFunc receives lifecycle events without probe result details. Callers
// must be safe for concurrent delivery so the scheduler can expose parallel
// groups without coupling rendering to probe execution.
type ProgressFunc func(Progress)

// detectNetworkCapabilities is a hook rather than a direct call so runner
// integration tests can exercise all four local capability states without
// depending on the host's real interfaces. The production value performs no
// remote operation.
var detectNetworkCapabilities = probe.DetectNetworkCapabilities

// discoverEgress is injectable so runner tests can verify that the effective
// family and shared capability snapshot exist before the egress stage starts.
var discoverEgress = probe.DiscoverEgress

func Run(ctx context.Context, cfg config.Runtime, progress ProgressFunc) model.Report {
	started := time.Now().UTC()
	selected := selectBindings(bindBuiltinModules(), cfg.Modules)
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool: model.ToolInfo{
			Name:      buildinfo.Name,
			Version:   buildinfo.Version,
			Commit:    buildinfo.Commit,
			BuildDate: buildinfo.BuildDate,
		},
		Run: model.RunInfo{
			ID:        newRunID(),
			Profile:   cfg.Profile,
			StartedAt: started,
			Exposure:  cfg.Exposure.String(),
			Offline:   cfg.OfflineOnly(),
			IPVersion: cfg.IPVersion,
			// The runner still contains raw probe diagnostics. RedactedCopy is the
			// only boundary allowed to claim that every report string was scrubbed.
			Redacted:      false,
			Requested:     append([]string(nil), cfg.Modules...),
			OutputFormats: append([]string(nil), cfg.Formats...),
		},
		Notices: []string{
			"ecs 报告只写入本地，不会自动上传；网络探针仍会按模块访问必要的公开目标。",
			"性能结果只应在相同测试方法、版本和资源参数下比较。",
		},
		SensitiveIPs: localInterfaceIPs(),
	}
	requested := make(map[string]bool, len(cfg.Modules))
	for _, id := range cfg.Modules {
		requested[id] = true
	}
	// Keep optional privacy/external-policy notices in descriptor order.  The
	// runner deliberately does not special-case a module ID here: adding an
	// external client only requires metadata in config's registry.
	for _, descriptor := range config.ModuleDescriptors() {
		if requested[descriptor.ID] && descriptor.PrivacyNotice != "" {
			report.Notices = append(report.Notices, descriptor.PrivacyNotice)
		}
	}

	httpClient := probe.NewHTTPClient(cfg.HTTPTimeout)
	defer httpClient.CloseIdleConnections()
	effectiveCfg := cfg
	networkRunnable := true
	networkModules := !cfg.OfflineOnly() && hasNetworkModules(selected)
	capabilities := probe.NetworkCapabilities{}
	if networkModules {
		capabilities = detectNetworkCapabilities()
		plan := planNetwork(cfg.IPVersion, capabilities)
		effectiveCfg.IPVersion = plan.effectiveIPVersion
		networkRunnable = plan.networkRunnable
	}
	env := probe.Environment{
		Config:     effectiveCfg,
		HTTPClient: httpClient,
		UserAgent:  fmt.Sprintf("ecs/%s", buildinfo.Version),
		Network:    capabilities,
	}
	// 出口 IP 只发现一次，供 network、blacklist、bgp 共用：这既省掉重复请求，
	// 也让"连了哪个外部服务"在报告里只出现一处。
	if networkModules && networkRunnable {
		env.Egress = discoverEgress(ctx, env)
	}
	for _, address := range env.Egress.ByVersion {
		if net.ParseIP(address.IP) != nil {
			report.SensitiveIPs = append(report.SensitiveIPs, address.IP)
		}
	}
	if env.Egress.Attempted {
		report.Notices = append(report.Notices,
			fmt.Sprintf("出口 IP 由 %s 统一发现一次，供需要它的模块共用。", env.Egress.SourceName))
	}

	titles := make([]string, len(selected))
	for index, binding := range selected {
		titles[index] = localizedDescriptorTitle(binding.Descriptor, binding.Probe.Title())
	}
	results := make([]model.Result, len(selected))
	completed := 0

	// 结果按模块原顺序写入固定槽位，因此并行不会打乱报告顺序。
	for _, group := range planSchedule(selected) {
		if ctx.Err() != nil {
			report.Run.Canceled = true
			break
		}
		if group.Parallel {
			// 先统一发出开始事件，再启动 worker，进度视图不会把一个很快完成的
			// 模块误显示成尚未开始；回调本身不携带结果，详细报告仍在最后渲染。
			if progress != nil {
				for _, index := range group.Indices {
					binding := selected[index]
					progress(Progress{Phase: PhaseStart, Index: index + 1, Total: len(selected), ID: binding.Descriptor.ID, Title: titles[index]})
				}
			}
			var wg sync.WaitGroup
			for _, index := range group.Indices {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					results[index] = runBinding(ctx, selected[index], effectiveCfg, env, networkRunnable)
				}(index)
			}
			wg.Wait()
		} else {
			index := group.Indices[0]
			binding := selected[index]
			if progress != nil {
				progress(Progress{Phase: PhaseStart, Index: index + 1, Total: len(selected), ID: binding.Descriptor.ID, Title: titles[index]})
			}
			results[index] = runBinding(ctx, binding, effectiveCfg, env, networkRunnable)
		}
		for _, index := range group.Indices {
			binding := selected[index]
			completed++
			if progress != nil {
				progress(Progress{Phase: PhaseDone, Index: index + 1, Total: len(selected), ID: binding.Descriptor.ID, Title: titles[index], Result: results[index]})
			}
		}
		if ctx.Err() != nil {
			report.Run.Canceled = true
			break
		}
	}
	report.Results = append(report.Results, results[:completed]...)

	report.Run.CompletedAt = time.Now().UTC()
	report.Run.DurationMS = report.Run.CompletedAt.Sub(report.Run.StartedAt).Milliseconds()
	model.Summarize(&report)
	return report
}

func localInterfaceIPs() []string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(addresses))
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		value := ip.String()
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// runBinding executes one validated module binding, uniformly handling
// offline skipping and methodology completion.
func runBinding(ctx context.Context, binding moduleBinding, cfg config.Runtime, env probe.Environment, networkRunnable bool) model.Result {
	item := binding.Probe
	descriptor := binding.Descriptor
	hasDescriptor := descriptor.ID != ""
	canonicalTitle := item.Title()
	needsNetwork := item.NeedsNetwork()
	if hasDescriptor {
		// Descriptor metadata is authoritative for cross-cutting runner policy;
		// keep the report title in the canonical source language. The terminal
		// progress title is localized separately by localizedTitle, and report
		// renderers localize this canonical title only for human-facing output.
		canonicalTitle = canonicalDescriptorTitle(descriptor, canonicalTitle)
		needsNetwork = descriptor.Exposure > config.ExposureLocal
	}
	var result model.Result
	if cfg.OfflineOnly() && needsNetwork {
		start := time.Now()
		result = model.NewResult(item.ID(), item.Title())
		result.Skip("离线模式")
		result.Finish(start)
	} else if !networkRunnable && needsNetwork {
		start := time.Now()
		result = model.NewResult(item.ID(), item.Title())
		result.Skip("未检测到用户请求的可用 IPv4/IPv6 出站能力")
		result.Finish(start)
	} else {
		result = runWithConditionalRetry(ctx, item, env)
	}
	if result.Methodology.Label == "" {
		if hasDescriptor {
			result.Methodology = descriptor.Methodology
		} else {
			result.Methodology = probe.MethodologyFor(item.ID())
		}
	}
	result.Methodology.Parameters = comparisonParameters(item.ID(), cfg, result)
	if result.Evidence == nil {
		// Probes normally report their real sample denominator.  This fallback
		// keeps panic, offline and legacy/custom probe results explicit without
		// pretending that their fields are independent observations.
		valid := 1
		if result.Status == model.StatusSkipped || result.Status == model.StatusError {
			valid = 0
		}
		result.Evidence = model.NewEvidence(valid, 1, "module")
	}
	failure.EnsureResult(&result)
	result.Title = canonicalTitle
	return result
}

// runOne is retained as a small test/custom-probe convenience. Production
// paths call runBinding with a descriptor already joined by bindBuiltinModules.
func runOne(ctx context.Context, item probe.Probe, cfg config.Runtime, env probe.Environment, networkRunnable bool) model.Result {
	return runBinding(ctx, bindingForProbe(item), cfg, env, networkRunnable)
}

func hasNetworkModules(selected []moduleBinding) bool {
	for _, binding := range selected {
		if binding.Descriptor.ID != "" {
			if binding.Descriptor.Exposure > config.ExposureLocal {
				return true
			}
			continue
		}
		if binding.Probe != nil && binding.Probe.NeedsNetwork() {
			return true
		}
	}
	return false
}

func runWithConditionalRetry(ctx context.Context, item probe.Probe, env probe.Environment) model.Result {
	return runWithConditionalRetryHooks(ctx, item, env, probe.CaptureEnvironmentSnapshot, probe.AssessBenchmarkInterference)
}

func runWithConditionalRetryHooks(
	ctx context.Context,
	item probe.Probe,
	env probe.Environment,
	capture func() probe.EnvironmentSnapshot,
	assess func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference,
) model.Result {
	if item.ID() != "cpu" && item.ID() != "zstd" && item.ID() != "npb" && item.ID() != "memory" && item.ID() != "crypto" && item.ID() != "disk" {
		return safeRun(ctx, item, env)
	}
	firstBefore := capture()
	first := safeRun(ctx, item, env)
	firstAfter := capture()
	firstInterference := assess(item.ID(), firstBefore, firstAfter)
	canRetry := firstInterference.Detected && ctx.Err() == nil &&
		first.Status != model.StatusError && first.Status != model.StatusSkipped &&
		first.Evidence != nil && first.Evidence.Valid > 0
	if !canRetry {
		probe.AppendInterferenceDiagnostics(&first, firstInterference)
		return first
	}

	secondBefore := capture()
	second := safeRun(ctx, item, env)
	secondAfter := capture()
	secondInterference := assess(item.ID(), secondBefore, secondAfter)
	selected := probe.FinalizeBenchmarkRetry(first, firstInterference, second, secondInterference)
	if selected.Retry != nil && selected.Retry.SelectedAttempt == 2 {
		probe.AppendInterferenceDiagnostics(&selected, secondInterference)
	} else {
		probe.AppendInterferenceDiagnostics(&selected, firstInterference)
	}
	return selected
}

// localizedTitle 按模块 ID 查标题译文。
//
// ID 是稳定的机器标识，正适合做 i18n 的 key；没有译文时保留探针自带的标题。
func localizedTitle(id, fallback string) string {
	if descriptor, ok := config.ModuleDescriptorFor(id); ok {
		return localizedDescriptorTitle(descriptor, fallback)
	}
	return fallback
}

func localizedDescriptorTitle(descriptor config.ModuleDescriptor, fallback string) string {
	if descriptor.TitleKey != "" && i18n.Has(i18n.Current(), descriptor.TitleKey) {
		return i18n.T(descriptor.TitleKey)
	}
	return fallback
}

// canonicalDescriptorTitle returns the stable source-language title stored in
// a Report. Human-facing renderers apply Localize later, so collection must not
// depend on the current UI language.
func canonicalDescriptorTitle(descriptor config.ModuleDescriptor, fallback string) string {
	if descriptor.TitleKey != "" && i18n.Has(i18n.LangZH, descriptor.TitleKey) {
		return i18n.TL(i18n.LangZH, descriptor.TitleKey)
	}
	return fallback
}

func safeRun(ctx context.Context, item probe.Probe, env probe.Environment) (result model.Result) {
	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = model.NewResult(item.ID(), item.Title())
			result.Status = model.StatusError
			result.Error = fmt.Sprintf("探针发生 panic: %v", recovered)
			result.Summary = "探针异常，已隔离并继续"
			result.AddFailure(model.Failure{
				Category: model.FailureUnknown, Stage: "panic", Target: item.ID(),
				Count: 1, Message: result.Error,
			})
			result.Finish(start)
		}
	}()
	return item.Run(ctx, env)
}

func newRunID() string {
	var bytes [6]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
