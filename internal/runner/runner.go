package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
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

func Run(ctx context.Context, cfg config.Runtime, progress ProgressFunc) model.Report {
	started := time.Now().UTC()
	selected := selectedProbes(cfg.Modules)
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool: model.ToolInfo{
			Name:      buildinfo.Name,
			Version:   buildinfo.Version,
			Commit:    buildinfo.Commit,
			BuildDate: buildinfo.BuildDate,
		},
		Run: model.RunInfo{
			ID:            newRunID(),
			Profile:       cfg.Profile,
			StartedAt:     started,
			Exposure:      cfg.Exposure.String(),
			Accepted:      append([]string(nil), cfg.Accepted...),
			Offline:       cfg.OfflineOnly(),
			IPVersion:     cfg.IPVersion,
			Redacted:      !cfg.Reveal,
			Requested:     append([]string(nil), cfg.Modules...),
			OutputFormats: append([]string(nil), cfg.Formats...),
		},
		Notices: []string{
			"ecs 报告只写入本地，不会自动上传；网络探针仍会按模块访问必要的公开目标。",
			"性能结果只应在相同测试方法、版本和资源参数下比较。",
		},
	}
	for _, id := range cfg.Modules {
		if id == "ookla" && config.AllowsModule(cfg.Exposure, cfg.Accepted, "ookla") {
			report.Notices = append(report.Notices,
				"Ookla 已按显式同意调用外部测速客户端；Ookla 可能独立处理测量元数据，这不属于 ecs 的本地零上传保证。")
			break
		}
	}

	httpClient := probe.NewHTTPClient(cfg.HTTPTimeout)
	defer httpClient.CloseIdleConnections()
	env := probe.Environment{
		Config:     cfg,
		HTTPClient: httpClient,
		UserAgent:  fmt.Sprintf("ecs/%s", buildinfo.Version),
	}
	// 出口 IP 只发现一次，供 network、blacklist、bgp 共用：这既省掉重复请求，
	// 也让"连了哪个外部服务"在报告里只出现一处。
	env.Egress = probe.DiscoverEgress(ctx, env)
	if env.Egress.Attempted {
		report.Notices = append(report.Notices,
			fmt.Sprintf("出口 IP 由 %s 统一发现一次，供需要它的模块共用。", env.Egress.SourceName))
	}

	ids := make([]string, len(selected))
	titles := make([]string, len(selected))
	for index, item := range selected {
		ids[index] = item.ID()
		titles[index] = localizedTitle(item.ID(), item.Title())
	}
	results := make([]model.Result, len(selected))
	completed := 0

	// 结果按模块原顺序写入固定槽位，因此并行不会打乱报告顺序。
	for _, group := range planSchedule(ids) {
		if ctx.Err() != nil {
			report.Run.Canceled = true
			break
		}
		if group.Parallel {
			// 先统一发出开始事件，再启动 worker，进度视图不会把一个很快完成的
			// 模块误显示成尚未开始；回调本身不携带结果，详细报告仍在最后渲染。
			if progress != nil {
				for _, index := range group.Indices {
					item := selected[index]
					progress(Progress{Phase: PhaseStart, Index: index + 1, Total: len(selected), ID: item.ID(), Title: titles[index]})
				}
			}
			var wg sync.WaitGroup
			for _, index := range group.Indices {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					results[index] = runOne(ctx, selected[index], cfg, env)
				}(index)
			}
			wg.Wait()
		} else {
			index := group.Indices[0]
			item := selected[index]
			if progress != nil {
				progress(Progress{Phase: PhaseStart, Index: index + 1, Total: len(selected), ID: item.ID(), Title: titles[index]})
			}
			results[index] = runOne(ctx, item, cfg, env)
		}
		for _, index := range group.Indices {
			item := selected[index]
			completed++
			if progress != nil {
				progress(Progress{Phase: PhaseDone, Index: index + 1, Total: len(selected), ID: item.ID(), Title: titles[index], Result: results[index]})
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

// runOne 执行单个探针，统一处理离线跳过与方法学补全。
func runOne(ctx context.Context, item probe.Probe, cfg config.Runtime, env probe.Environment) model.Result {
	var result model.Result
	if cfg.OfflineOnly() && item.NeedsNetwork() {
		start := time.Now()
		result = model.NewResult(item.ID(), item.Title())
		result.Skip("离线模式")
		result.Finish(start)
	} else {
		result = safeRun(ctx, item, env)
	}
	if result.Methodology.Label == "" {
		result.Methodology = probe.MethodologyFor(item.ID())
	}
	result.Title = localizedTitle(item.ID(), result.Title)
	return result
}

// localizedTitle 按模块 ID 查标题译文。
//
// ID 是稳定的机器标识，正适合做 i18n 的 key；没有译文时保留探针自带的标题。
func localizedTitle(id, fallback string) string {
	if key := "module." + id + ".title"; i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	return fallback
}

func selectedProbes(ids []string) []probe.Probe {
	requested := make(map[string]bool)
	for _, id := range ids {
		requested[id] = true
	}
	var out []probe.Probe
	for _, item := range probe.Registry() {
		if requested[item.ID()] {
			out = append(out, item)
		}
	}
	return out
}

func safeRun(ctx context.Context, item probe.Probe, env probe.Environment) (result model.Result) {
	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = model.NewResult(item.ID(), item.Title())
			result.Status = model.StatusError
			result.Error = fmt.Sprintf("探针发生 panic: %v", recovered)
			result.Summary = "探针异常，已隔离并继续"
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
