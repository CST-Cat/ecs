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

func (memoryProbe) ID() string { return "memory" }

func (memoryProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	memory := collectMemoryUsageSnapshot()
	balloon := detectBalloonReclaim("/sys", "/proc/vmstat")
	ksm := detectKSM("/sys")
	allowance := detectCPUAllowance()

	if path := officialStreamPath(); path != "" {
		result := runStreamMemoryWithAllowance(ctx, env, path, allowance)
		appendMemoryInventory(&result, memory, balloon, ksm)
		result.Finish(start)
		return result
	}

	result := newMemoryResult()
	appendMemoryInventory(&result, memory, balloon, ksm)
	result.Status = model.StatusWarning
	result.SummaryMessages = []model.Message{model.NewMessage("probe.memory.stream_missing")}
	result.AddFailure(model.Failure{
		Category: model.FailureToolMissing,
		Stage:    "tool_lookup",
		Target:   "stream",
		Count:    1,
		Message:  "official STREAM executable not found",
	})
	result.Notes = append(result.Notes, "probe.memory.stream_missing")
	result.Evidence = model.NewEvidence(0, len(distinctBenchmarkThreadCounts(allowance.Threads)), "run")
	result.Finish(start)
	return result
}

func newMemoryResult() model.Result {
	result := model.NewResult("memory", "module.memory.title")
	result.Description = "probe.memory.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "STREAM",
		Profile:         "probe.memory.stream.profile",
		ComparisonScope: "probe.memory.comparison_scope",
	}
	result.Methodology.Parameters = newComparisonParameters()
	return result
}

func collectMemoryUsageSnapshot() memoryUsageSnapshot {
	mem := parseMemInfo("/proc/meminfo")
	limit, _, _ := cgroupMemoryLimit()
	memory := memoryUsageFromMemInfo(mem, limit)
	if !memory.LimitApplied {
		return memory
	}
	if current, _, ok := cgroupMemoryCurrent(); ok && current <= memory.EffectiveTotalBytes {
		memory.EffectiveUsedBytes = current
		memory.EffectiveAvailableBytes = memory.EffectiveTotalBytes - current
		if memory.EffectiveTotalBytes > 0 {
			memory.EffectiveUsagePercent = float64(current) / float64(memory.EffectiveTotalBytes) * 100
		}
		memory.EffectiveCurrentKnown = true
	}
	return memory
}

func officialStreamPath() string {
	path, err := exec.LookPath("stream")
	if err != nil || !IsOfficialStreamBinary(path) {
		return ""
	}
	return path
}

func appendMemoryInventory(result *model.Result, memory memoryUsageSnapshot, balloon, ksm memoryFacility) {
	if result == nil {
		return
	}
	result.Fields = append(result.Fields,
		model.Field{Key: "memory_total", Label: "probe.memory.field.total", Value: model.RawValue(model.FormatBytes(memory.EffectiveTotalBytes))},
		model.Field{Key: "memory_used", Label: "probe.memory.field.used", Value: model.RawValue(model.FormatBytes(memory.EffectiveUsedBytes))},
		model.Field{Key: "memory_available", Label: "probe.memory.field.available", Value: model.RawValue(model.FormatBytes(memory.EffectiveAvailableBytes))},
		model.Field{Key: "memory_usage_percent", Label: "probe.memory.field.usage_percent", Value: model.RawValue(fmt.Sprintf("%.1f %%", memory.EffectiveUsagePercent))},
		model.Field{Key: "balloon_reclaim", Label: "probe.memory.field.balloon_reclaim", Value: model.RawValue(balloon.Status())},
		model.Field{Key: "balloon_reclaim_available", Label: "probe.memory.field.balloon_reclaim_available", Value: model.RawValue(strconv.FormatBool(balloon.Available))},
		model.Field{Key: "balloon_reclaim_evidence", Label: "probe.memory.field.balloon_reclaim_evidence", Value: model.RawValue(fallback(balloon.Evidence, "none found"))},
		model.Field{Key: "ksm_merging", Label: "probe.memory.field.ksm_merging", Value: model.RawValue(ksm.Status())},
		model.Field{Key: "ksm_merging_available", Label: "probe.memory.field.ksm_merging_available", Value: model.RawValue(strconv.FormatBool(ksm.Available))},
		model.Field{Key: "ksm_merging_evidence", Label: "probe.memory.field.ksm_merging_evidence", Value: model.RawValue(fallback(ksm.Evidence, "none found"))},
	)
	if memory.LimitApplied {
		result.Notes = append(result.Notes, "probe.memory.note.cgroup_limit")
		if memory.EffectiveCurrentKnown {
			result.Notes = append(result.Notes, "probe.memory.note.cgroup_current")
		} else {
			result.Notes = append(result.Notes, "probe.memory.note.cgroup_current_unknown")
		}
	}
	if !memory.AvailableKnown {
		result.Notes = append(result.Notes, "probe.memory.note.memavailable_legacy_fallback")
	}
	if !balloon.Available {
		result.Notes = append(result.Notes, "probe.memory.note.balloon_reclaim_unavailable")
	}
	if !ksm.Available {
		result.Notes = append(result.Notes, "probe.memory.note.ksm_unavailable")
	}
}
