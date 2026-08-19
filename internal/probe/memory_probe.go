package probe

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"ecs/internal/model"
)

type memoryProbe struct{}

func (memoryProbe) ID() string         { return "memory" }
func (memoryProbe) Title() string      { return "内存性能" }
func (memoryProbe) NeedsNetwork() bool { return false }

func (memoryProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	// /proc/meminfo 与 cgroup 的有效视图用于保留内存库存和资源边界证据。
	mem := parseMemInfo("/proc/meminfo")
	limit, _, _ := cgroupMemoryLimit()
	memory := memoryUsageFromMemInfo(mem, limit)
	if memory.LimitApplied {
		if current, _, ok := cgroupMemoryCurrent(); ok && current <= memory.EffectiveTotalBytes {
			memory.EffectiveUsedBytes = current
			memory.EffectiveAvailableBytes = memory.EffectiveTotalBytes - current
			memory.EffectiveUsagePercent = float64(current) / float64(memory.EffectiveTotalBytes) * 100
			memory.EffectiveCurrentKnown = true
		}
	}

	result := model.NewResult("memory", "内存性能")
	result.Description = "官方 STREAM 内存带宽的 Copy、Scale、Add、Triad kernel，分别运行 1T 与当前 CPU allowance 的 NT"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "STREAM",
		Profile:         "Copy/Scale/Add/Triad × 1T/NT",
		ComparisonScope: "相同 STREAM 实现、数组规模、编译选项、线程上下文与运行环境",
	}

	streamPath, streamErr := exec.LookPath("stream")
	if streamErr != nil || !IsOfficialStreamBinary(streamPath) {
		result.Status = model.StatusWarning
		result.Summary = "未找到官方 STREAM，标准内存基准未运行"
		result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: "STREAM", Count: 1, Message: result.Summary})
		if streamErr != nil {
			result.Notes = append(result.Notes, "probe.memory.stream_missing")
		} else {
			result.Notes = append(result.Notes, "probe.memory.stream_invalid")
		}
		result.Evidence = model.NewEvidence(0, len(distinctBenchmarkThreadCounts(detectCPUAllowance().Threads)), "run")
	} else {
		result = runStreamMemory(ctx, env, streamPath)
	}

	appendMemoryInventory(&result, memory, detectBalloonReclaim("/sys", "/proc/vmstat"), detectKSM("/sys"))
	result.Finish(start)
	return result
}

func appendMemoryInventory(result *model.Result, memory memoryUsageSnapshot, balloon, ksm memoryFacility) {
	result.Fields = append(result.Fields,
		model.Field{Key: "memory_total", Label: "内存总量", Value: model.FormatBytes(memory.EffectiveTotalBytes)},
		model.Field{Key: "memory_used", Label: "内存已用", Value: model.FormatBytes(memory.EffectiveUsedBytes)},
		model.Field{Key: "memory_available", Label: "内存可用", Value: model.FormatBytes(memory.EffectiveAvailableBytes)},
		model.Field{Key: "memory_usage_percent", Label: "内存使用率", Value: fmt.Sprintf("%.1f %%", memory.EffectiveUsagePercent)},
		model.Field{Key: "balloon_reclaim", Label: "Balloon reclaim", Value: balloon.Status()},
		model.Field{Key: "balloon_reclaim_available", Label: "Balloon reclaim 可用", Value: strconv.FormatBool(balloon.Available)},
		model.Field{Key: "balloon_reclaim_evidence", Label: "Balloon reclaim 证据", Value: fallback(balloon.Evidence, "none found")},
		model.Field{Key: "ksm_merging", Label: "KSM merging", Value: ksm.Status()},
		model.Field{Key: "ksm_merging_available", Label: "KSM merging 可用", Value: strconv.FormatBool(ksm.Available)},
		model.Field{Key: "ksm_merging_evidence", Label: "KSM merging 证据", Value: fallback(ksm.Evidence, "none found")},
	)
	if memory.LimitApplied {
		result.Notes = append(result.Notes, "内存总量与使用率按 cgroup 有效配额计算；/proc/meminfo 的宿主可见值仍由系统模块保留。")
		if memory.EffectiveCurrentKnown {
			result.Notes = append(result.Notes, "cgroup memory.current 提供了有效配额内的已用内存；可用值按配额减当前用量计算。")
		} else {
			result.Notes = append(result.Notes, "cgroup memory.current 不可读；有效配额内的已用/可用值按 MemAvailable 计算。")
		}
	}
	if !memory.AvailableKnown {
		result.Notes = append(result.Notes, "MemAvailable unavailable：使用 MemFree + Buffers + Cached 计算可用内存。")
	}
	if !balloon.Available {
		result.Notes = append(result.Notes, "Balloon reclaim unavailable：未找到可验证的 Linux sysfs/proc 证据。")
	}
	if !ksm.Available {
		result.Notes = append(result.Notes, "KSM merging unavailable：未找到可验证的 Linux sysfs run/pages_sharing 证据。")
	}
}
