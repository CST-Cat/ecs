package probe

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

type diskProbe struct{}

func (diskProbe) ID() string { return "disk" }

func (diskProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	var result model.Result
	if path, err := exec.LookPath("fio"); err == nil {
		result = runFIODisk(ctx, env, path)
	} else {
		result = newDiskResult()
		result.Status = model.StatusWarning
		addFailure(&result, "tool_lookup", "fio", err)
		result.Evidence = model.NewEvidence(0, len(fioJobPlan()), "job")
	}

	// 多挂载盘默认关闭：多跑一块盘就多一份写入与时间，应由用户显式要求。
	if env.Config.DiskMulti {
		if fioPath, err := exec.LookPath("fio"); err == nil {
			appendMultiDiskResults(ctx, &result, env, fioPath)
		}
	}
	addComparisonParameter(result.Methodology.Parameters, "configured_file_mib", strconv.Itoa(env.Config.DiskMiB))
	addComparisonParameter(result.Methodology.Parameters, "multi_mount", strconv.FormatBool(env.Config.DiskMulti))
	finalizeDiskResult(&result)
	result.Finish(start)
	return result
}

func newDiskResult() model.Result {
	result := model.NewResult("disk", "module.disk.title")
	result.Description = "probe.disk.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "fio",
		Profile:         "probe.disk.profile",
		ComparisonScope: "probe.disk.comparison_scope",
	}
	result.Methodology.Parameters = newComparisonParameters()
	return result
}

func diskFieldLabel(key string) string {
	return "probe.disk.field." + key
}

func diskMeasurementLabel(key string) string {
	switch {
	case strings.HasPrefix(key, "fio_mount_"):
		return "probe.disk.metric.mount"
	case strings.HasPrefix(key, "fio_crystal_"), strings.HasPrefix(key, "fio_atto_"), strings.HasPrefix(key, "fio_mixed_"):
		return "probe.disk.metric.matrix"
	case strings.HasPrefix(key, "fio_random_") && strings.HasSuffix(key, "_p95_ms"):
		return "probe.disk.metric.latency_p95"
	default:
		return "probe.disk.metric." + key
	}
}

func finalizeDiskResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Notes = diskNotes(*result)
	if summary := diskMachineSummary(*result); summary != "" {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.disk.summary.values", summary)}
	} else if firstFailureAt(result, "tool_lookup") != nil {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.disk.summary.tool_missing")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.disk.summary.none")}
	}
}

func diskTableStatusKey(tableKey string, complete bool) string {
	switch tableKey {
	case "disk.fio.crystal", "disk.fio.atto", "disk.fio.mounts":
		if complete {
			return "probe.disk.status.complete"
		}
		return "probe.disk.status.missing"
	default:
		return ""
	}
}

func diskNotes(result model.Result) []string {
	notes := []string{
		"probe.disk.note.contract",
		"probe.disk.note.matrices",
		"probe.disk.note.missing_not_zero",
	}
	if result.Evidence != nil && result.Evidence.Valid < result.Evidence.Expected {
		notes = append(notes, "probe.disk.note.partial_results")
	}
	if firstFailureAt(&result, "tool_lookup") != nil {
		notes = append(notes, "probe.disk.note.tool_missing")
	}
	if result.Status == model.StatusError {
		notes = append(notes, "probe.disk.note.run_failure")
	}
	for _, table := range result.Tables {
		if table.Key == "disk.fio.mounts" {
			notes = append(notes, "probe.disk.note.multidisk")
		}
	}
	seen := make(map[string]bool, len(notes))
	out := notes[:0]
	for _, note := range notes {
		if !seen[note] {
			seen[note] = true
			out = append(out, note)
		}
	}
	return out
}

func diskMachineSummary(result model.Result) string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display.Text()
		}
	}
	parts := make([]string, 0, 4)
	for _, item := range []struct{ key, label string }{
		{key: "fio_sequential_write_mib_s", label: "write"},
		{key: "fio_sequential_read_mib_s", label: "read"},
		{key: "fio_random_read_4k_iops", label: "4K-read"},
		{key: "fio_random_write_4k_iops", label: "4K-write"},
	} {
		if value := values[item.key]; value != "" {
			parts = append(parts, item.label+"="+value)
		}
	}
	return strings.Join(parts, ";")
}
