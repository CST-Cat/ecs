package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/failure"
	"ecs/internal/model"
	"ecs/internal/module"
	"ecs/internal/probe"
)

type Phase string

const (
	PhaseStart Phase = "start"
	PhaseDone  Phase = "done"
)

type Progress struct {
	Phase    Phase
	Index    int
	Total    int
	ID       string
	Title    string
	TitleKey string
	Result   model.Result
}

// ProgressFunc receives lifecycle events. Start events contain module metadata;
// done events also contain the completed probe result. Events are delivered
// serially by the runner even when a schedule group runs probes in parallel.
type ProgressFunc func(Progress)

// detectNetworkCapabilities is a hook rather than a direct call so runner
// integration tests can exercise all four local capability states without
// depending on the host's real interfaces. The production value performs no
// remote operation.
var detectNetworkCapabilities = probe.DetectNetworkCapabilities

// discoverEgress is injectable so runner tests can verify that the effective
// family and shared capability snapshot exist before the egress stage starts.
var discoverEgress = probe.DiscoverEgress

// runIDRandomReader is the only source for benchmark run identities. It is a
// package-local seam so failure propagation can be tested without introducing
// an identity abstraction.
var runIDRandomReader io.Reader = rand.Reader

func Run(ctx context.Context, definitions []probe.Definition, catalog module.Catalog, cfg config.Runtime, progress ProgressFunc) (model.Report, error) {
	runID, err := newRunID()
	if err != nil {
		return model.Report{}, err
	}
	started := time.Now().UTC()
	selected := selectDefinitions(definitions, cfg.Modules)
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool: model.ToolInfo{
			Name:      buildinfo.Name,
			Version:   buildinfo.Version,
			Commit:    buildinfo.Commit,
			BuildDate: buildinfo.BuildDate,
		},
		Run: model.RunInfo{
			ID:        runID,
			Profile:   cfg.Profile,
			StartedAt: started,
			Exposure:  cfg.Exposure.String(),
			IPVersion: cfg.IPVersion,
			// The runner still contains raw probe diagnostics. RedactedCopy is the
			// only boundary allowed to claim that every report string was scrubbed.
			Redacted:      false,
			Requested:     append([]string(nil), cfg.Modules...),
			OutputFormats: append([]string(nil), cfg.Formats...),
		},
		Notices: []model.Message{
			model.NewMessage("message.notice.localOnly"),
			model.NewMessage("message.notice.compareScope"),
		},
		SensitiveIPs: localInterfaceIPs(),
	}
	requested := make(map[string]bool, len(cfg.Modules))
	for _, id := range cfg.Modules {
		requested[id] = true
	}
	// Keep optional privacy/external-policy notices in descriptor order. The
	// runner deliberately does not special-case a module ID here: adding an
	// external client only requires metadata in the canonical definitions.
	for _, definition := range definitions {
		descriptor := definition.Descriptor
		if requested[descriptor.ID] && descriptor.PrivacyNoticeKey != "" {
			report.Notices = append(report.Notices, model.NewMessage(descriptor.PrivacyNoticeKey))
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
		Catalog:    catalog,
		HTTPClient: httpClient,
		UserAgent:  fmt.Sprintf("ecs/%s", buildinfo.Version),
		Network:    capabilities,
	}
	// 出口 IP 只发现一次，供 network、blacklist、bgp 共用。
	if networkModules && networkRunnable {
		env.Egress = discoverEgress(ctx, env)
	}
	for _, address := range env.Egress.ByVersion {
		if net.ParseIP(address.IP) != nil {
			report.SensitiveIPs = append(report.SensitiveIPs, address.IP)
		}
	}
	if env.Egress.Attempted {
		report.Notices = append(report.Notices, model.NewMessage("message.notice.egressShared", env.Egress.SourceName))
	}

	titles := make([]string, len(selected))
	titleKeys := make([]string, len(selected))
	for index, definition := range selected {
		titles[index] = definitionTitle(definition)
		titleKeys[index] = definition.Descriptor.TitleKey
	}
	results := make([]model.Result, len(selected))
	completed := 0

	// 结果按模块原顺序写入固定槽位，因此并行不会打乱报告顺序。
	for _, group := range planSchedule(selected) {
		if ctx.Err() != nil {
			report.Run.Canceled = true
			break
		}
		if len(group.Indices) > 1 {
			// 先统一发出开始事件，再启动 worker，进度视图不会把一个很快完成的
			// 模块误显示成尚未开始；开始事件不携带结果，完成事件携带对应结果，
			// 详细报告仍在最后渲染。
			if progress != nil {
				for _, index := range group.Indices {
					definition := selected[index]
					progress(Progress{Phase: PhaseStart, Index: index + 1, Total: len(selected), ID: definition.Descriptor.ID, Title: titles[index], TitleKey: titleKeys[index]})
				}
			}
			var wg sync.WaitGroup
			for _, index := range group.Indices {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					results[index] = runDefinition(ctx, selected[index], effectiveCfg, env, networkRunnable)
				}(index)
			}
			wg.Wait()
		} else {
			index := group.Indices[0]
			definition := selected[index]
			if progress != nil {
				progress(Progress{Phase: PhaseStart, Index: index + 1, Total: len(selected), ID: definition.Descriptor.ID, Title: titles[index], TitleKey: titleKeys[index]})
			}
			results[index] = runDefinition(ctx, definition, effectiveCfg, env, networkRunnable)
		}
		for _, index := range group.Indices {
			definition := selected[index]
			completed++
			if progress != nil {
				progress(Progress{Phase: PhaseDone, Index: index + 1, Total: len(selected), ID: definition.Descriptor.ID, Title: titles[index], TitleKey: titleKeys[index], Result: results[index]})
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
	// 内部生成边界的唯一身份 owner：探针在这里之后不再改动 Report，下游的评分、
	// 序列化和比较都信任这个结果。外部输入走 report.LoadJSON，那里有另一个 owner。
	if err := model.ValidateReportIdentity(report); err != nil {
		return model.Report{}, err
	}
	return report, nil
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

// runDefinition executes one validated module definition, uniformly handling
// offline skipping and methodology completion.
func runDefinition(ctx context.Context, definition probe.Definition, cfg config.Runtime, env probe.Environment, networkRunnable bool) model.Result {
	item := definition.Probe
	descriptor := definition.Descriptor
	canonicalTitle := definitionTitle(definition)
	needsNetwork := descriptor.Exposure > module.ExposureLocal
	var result model.Result
	if cfg.OfflineOnly() && needsNetwork {
		start := time.Now()
		result = model.NewResult(item.ID(), canonicalTitle)
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("message.runner.skip.offline")}
		result.Finish(start)
	} else if !networkRunnable && needsNetwork {
		start := time.Now()
		result = model.NewResult(item.ID(), canonicalTitle)
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("message.runner.skip.noRequestedIP")}
		result.Finish(start)
	} else {
		result = runWithConditionalRetry(ctx, item, descriptor.RetryOnInterference, env)
	}
	if result.Methodology.Label == "" {
		result.Methodology = descriptor.Methodology
	}
	if result.Evidence == nil {
		// Probes normally report their real sample denominator. This fallback
		// supplies a module-level denominator when a result has no evidence,
		// without pretending that its fields are independent observations.
		valid := 1
		if result.Status == model.StatusSkipped || result.Status == model.StatusError {
			valid = 0
		}
		result.Evidence = model.NewEvidence(valid, 1, "module")
	}
	if result.Evidence != nil {
		result.Evidence.Normalize()
	}
	result.Title = canonicalTitle
	return result
}

// definitionTitle supplies the canonical descriptor title for built-ins and
// falls back to the stable probe ID when no title key is available. runDefinition
// canonicalizes result.Title to this value after the probe completes.
func definitionTitle(definition probe.Definition) string {
	if definition.Descriptor.TitleKey != "" {
		return definition.Descriptor.TitleKey
	}
	if definition.Probe != nil {
		return definition.Probe.ID()
	}
	return ""
}

func hasNetworkModules(selected []probe.Definition) bool {
	for _, definition := range selected {
		if definition.Descriptor.Exposure > module.ExposureLocal {
			return true
		}
	}
	return false
}

func runWithConditionalRetry(ctx context.Context, item probe.Probe, retryOnInterference bool, env probe.Environment) model.Result {
	return runWithConditionalRetryHooks(ctx, item, retryOnInterference, env, probe.CaptureEnvironmentSnapshot, probe.AssessBenchmarkInterference)
}

func runWithConditionalRetryHooks(
	ctx context.Context,
	item probe.Probe,
	retryOnInterference bool,
	env probe.Environment,
	capture func() probe.EnvironmentSnapshot,
	assess func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference,
) model.Result {
	if !retryOnInterference {
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

func safeRun(ctx context.Context, item probe.Probe, env probe.Environment) (result model.Result) {
	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = model.NewResult(item.ID(), item.ID())
			result.Status = model.StatusError
			result.SummaryMessages = []model.Message{model.NewMessage("message.runner.panic")}
			result.AddFailure(failure.FromMessage("panic", item.ID(), fmt.Sprint(recovered)))
			result.Finish(start)
		}
	}()
	return item.Run(ctx, env)
}

func newRunID() (string, error) {
	var bytes [16]byte
	if _, err := io.ReadFull(runIDRandomReader, bytes[:]); err != nil {
		return "", fmt.Errorf("generate run ID: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
