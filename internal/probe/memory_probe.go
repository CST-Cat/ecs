package probe

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"ecs/internal/model"
)

type memoryProbe struct{}

func (memoryProbe) ID() string         { return "memory" }
func (memoryProbe) Title() string      { return "module.memory.title" }
func (memoryProbe) NeedsNetwork() bool { return false }

func (memoryProbe) Run(ctx context.Context, env Environment) model.Result {
	stats := collectMemoryStats()
	allowance := detectCPUAllowance()
	if path := officialStreamPath(); path != "" {
		result := runStreamMemoryWithAllowance(ctx, env, path, allowance)
		appendMemoryInventory(&result, stats)
		return result
	}
	start := time.Now()
	result := model.NewResult("memory", "module.memory.title")
	result.Description = "probe.memory.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "STREAM",
		Profile:         "Copy/Scale/Add/Triad; 1T+NT",
		ComparisonScope: "probe.memory.comparison_scope",
	}
	appendMemoryInventory(&result, stats)
	result.Status = model.StatusWarning
	result.Summary = ""
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

func officialStreamPath() string {
	path, err := exec.LookPath("stream")
	if err != nil || !IsOfficialStreamBinary(path) {
		return ""
	}
	return path
}

func appendMemoryInventory(result *model.Result, stats memoryStats) {
	if stats.EffectiveTotal > 0 {
		result.Fields = append(result.Fields, model.Field{Key: "memory_total", Label: "probe.memory.field.total", Value: model.FormatBytes(stats.EffectiveTotal)})
	}
	if stats.EffectiveUsed > 0 || stats.EffectiveTotal > 0 {
		result.Fields = append(result.Fields, model.Field{Key: "memory_used", Label: "probe.memory.field.used", Value: model.FormatBytes(stats.EffectiveUsed)})
	}
	if stats.EffectiveAvailable > 0 || stats.EffectiveTotal > 0 {
		result.Fields = append(result.Fields, model.Field{Key: "memory_available", Label: "probe.memory.field.available", Value: model.FormatBytes(stats.EffectiveAvailable)})
	}
	if stats.UsagePercent >= 0 {
		result.Fields = append(result.Fields, model.Field{Key: "memory_usage_percent", Label: "probe.memory.field.usage_percent", Value: fmt.Sprintf("%.1f %%", stats.UsagePercent)})
	}
	if stats.CgroupLimit > 0 {
		result.Notes = append(result.Notes, "probe.memory.note.cgroup_limit")
		if stats.CgroupCurrent == 0 {
			result.Notes = append(result.Notes, "probe.memory.note.cgroup_current_unknown")
		}
	}
	if !stats.AvailableKnown && stats.HostTotal > 0 {
		result.Notes = append(result.Notes, "probe.memory.note.memavailable_fallback")
	}
	if stats.Balloon.Present {
		result.Fields = append(result.Fields,
			model.Field{Key: "balloon_present", Label: "probe.memory.field.balloon_present", Value: fmt.Sprintf("%t", stats.Balloon.Present)},
			model.Field{Key: "balloon_actual_bytes", Label: "probe.memory.field.balloon_actual", Value: model.FormatBytes(stats.Balloon.Actual)},
			model.Field{Key: "balloon_current_bytes", Label: "probe.memory.field.balloon_current", Value: model.FormatBytes(stats.Balloon.Current)},
		)
	} else {
		result.Notes = append(result.Notes, "probe.memory.note.balloon_unavailable")
	}
	if stats.KSM.Present {
		result.Fields = append(result.Fields,
			model.Field{Key: "ksm_pages_shared", Label: "probe.memory.field.ksm_pages_shared", Value: fmt.Sprintf("%d", stats.KSM.PagesShared)},
			model.Field{Key: "ksm_pages_sharing", Label: "probe.memory.field.ksm_pages_sharing", Value: fmt.Sprintf("%d", stats.KSM.PagesSharing)},
			model.Field{Key: "ksm_saved_bytes_estimate", Label: "probe.memory.field.ksm_saved_bytes", Value: model.FormatBytes(stats.KSM.SavedBytes)},
		)
	} else {
		result.Notes = append(result.Notes, "probe.memory.note.ksm_unavailable")
	}
}
