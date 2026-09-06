package probe

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
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
	current, currentSource, currentOK := cgroupMemoryCurrent()
	return applyCgroupMemoryUsage(memory, currentSource, current, currentOK, cgroupMemoryLimitCandidates())
}

func applyCgroupMemoryUsage(memory memoryUsageSnapshot, currentSource string, current uint64, currentOK bool, limits []cgroupMemoryLimitCandidate) memoryUsageSnapshot {
	// Host MemAvailable is a useful upper bound, including an explicit zero,
	// but never substitutes for cgroup aggregate usage.
	memory.EffectiveAvailableBytes = 0
	memory.EffectiveAvailableKnown = false
	memory.EffectiveUsedBytes = 0
	memory.EffectiveUsagePercent = 0
	memory.EffectiveCurrentKnown = false
	if currentOK {
		memory.EffectiveUsedBytes = current
		memory.EffectiveCurrentKnown = true
		if memory.EffectiveTotalBytes > 0 {
			memory.EffectiveUsagePercent = float64(current) / float64(memory.EffectiveTotalBytes) * 100
		}
	}
	if len(limits) == 0 {
		return memory
	}
	available := memory.HostAvailableBytes
	usageKnown := true
	for _, limit := range limits {
		usage, ok := cgroupMemoryUsageAt(limit)
		if !ok && currentOK && filepath.Dir(limit.path) == filepath.Dir(currentSource) {
			usage, ok = current, true
		}
		if !ok {
			usageKnown = false
			continue
		}
		remaining := uint64(0)
		if usage < limit.limit {
			remaining = limit.limit - usage
		}
		if remaining < available {
			available = remaining
		}
	}
	memory.EffectiveAvailableKnown = usageKnown && memory.AvailableKnown
	if memory.EffectiveAvailableKnown {
		memory.EffectiveAvailableBytes = available
	}
	return memory
}

func cgroupMemoryUsageAt(limit cgroupMemoryLimitCandidate) (uint64, bool) {
	file := "memory.current"
	if !limit.v2 {
		file = "memory.usage_in_bytes"
	}
	text := strings.TrimSpace(readTrimmed(filepath.Join(filepath.Dir(limit.path), file), ""))
	value, err := strconv.ParseUint(text, 10, 64)
	return value, err == nil
}

func officialStreamPath() string {
	path, err := LookupTool("stream")
	if err != nil || !IsOfficialStreamBinary(path) {
		return ""
	}
	return path
}

func appendMemoryInventory(result *model.Result, memory memoryUsageSnapshot, balloon, ksm memoryFacility) {
	if result == nil {
		return
	}
	available := model.FormatBytes(memory.EffectiveAvailableBytes)
	used := model.FormatBytes(memory.EffectiveUsedBytes)
	usagePercent := fmt.Sprintf("%.1f %%", memory.EffectiveUsagePercent)
	if memory.LimitApplied && !memory.EffectiveAvailableKnown {
		available = "unavailable"
	}
	if memory.LimitApplied && !memory.EffectiveCurrentKnown {
		used = "unavailable"
		usagePercent = "unavailable"
	}
	result.Fields = append(result.Fields,
		model.Field{Key: "memory_total", Label: "probe.memory.field.total", Value: model.RawValue(model.FormatBytes(memory.EffectiveTotalBytes))},
		model.Field{Key: "memory_used", Label: "probe.memory.field.used", Value: model.RawValue(used)},
		model.Field{Key: "memory_available", Label: "probe.memory.field.available", Value: model.RawValue(available)},
		model.Field{Key: "memory_usage_percent", Label: "probe.memory.field.usage_percent", Value: model.RawValue(usagePercent)},
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
