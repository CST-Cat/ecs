package probe

import (
	"context"
	"strings"

	"ecs/internal/model"
)

// diskSemanticProbe keeps the fio execution and matrix calculations in their
// existing implementation while replacing ECS-owned presentation metadata at
// the probe boundary. Fio output and operational diagnostics remain raw.
type diskSemanticProbe struct{}

func (diskSemanticProbe) ID() string         { return "disk" }
func (diskSemanticProbe) Title() string      { return "module.disk.title" }
func (diskSemanticProbe) NeedsNetwork() bool { return false }

func (diskSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (diskProbe{}).Run(ctx, env)
	stabilizeDiskResult(&result)
	return result
}

func stabilizeDiskResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.disk.title"
	result.Description = "probe.disk.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "fio",
		Profile:         "probe.disk.profile",
		ComparisonScope: "probe.disk.comparison_scope",
	}
	for index := range result.Fields {
		field := &result.Fields[index]
		field.Label = "probe.disk.field." + field.Key
	}
	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		switch {
		case strings.HasPrefix(measurement.Key, "fio_mount_"):
			measurement.Label = "probe.disk.metric.mount"
		case strings.HasPrefix(measurement.Key, "fio_crystal_"), strings.HasPrefix(measurement.Key, "fio_atto_"), strings.HasPrefix(measurement.Key, "fio_mixed_"):
			measurement.Label = "probe.disk.metric.matrix"
		case strings.HasPrefix(measurement.Key, "fio_random_") && strings.HasSuffix(measurement.Key, "_p95_ms"):
			measurement.Label = "probe.disk.metric.latency_p95"
		default:
			measurement.Label = "probe.disk.metric." + measurement.Key
		}
	}
	for index := range result.TextBlocks {
		result.TextBlocks[index].Title = "probe.disk.raw_output"
	}
	for index := range result.Sources {
		if strings.EqualFold(result.Sources[index].Name, "fio") {
			result.Sources[index].Purpose = "probe.disk.source.fio"
		} else {
			result.Sources[index].Purpose = "probe.disk.source.yabs"
		}
	}
	for index := range result.Tables {
		stabilizeDiskTable(&result.Tables[index])
	}
	result.Notes = stableDiskNotes(*result)
	result.Summary = ""
	if diskSummary := diskMachineSummary(*result); diskSummary != "" {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.disk.summary.values", diskSummary)}
	} else if firstFailureAt(result, "tool_lookup") != nil {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.disk.summary.tool_missing")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.disk.summary.none")}
	}
}

func stabilizeDiskTable(table *model.Table) {
	if table == nil {
		return
	}
	hasStatus := false
	switch table.Key {
	case "disk.fio.mixed":
		table.Title = "probe.disk.table.mixed"
		table.Columns = []string{
			"probe.disk.column.block_size", "probe.disk.column.read", "probe.disk.column.read_iops",
			"probe.disk.column.write", "probe.disk.column.write_iops", "probe.disk.column.total",
		}
	case "disk.fio.crystal":
		hasStatus = true
		table.Title = "probe.disk.table.crystal"
		table.Columns = []string{
			"probe.disk.column.workload", "probe.disk.column.read", "probe.disk.column.read_iops",
			"probe.disk.column.write", "probe.disk.column.write_iops", "probe.disk.column.offset", "probe.disk.column.status",
		}
	case "disk.fio.atto":
		hasStatus = true
		table.Title = "probe.disk.table.atto"
		table.Columns = []string{
			"probe.disk.column.block_size", "probe.disk.column.read", "probe.disk.column.read_iops",
			"probe.disk.column.write", "probe.disk.column.write_iops", "probe.disk.column.runtime",
			"probe.disk.column.offset", "probe.disk.column.status",
		}
	case "disk.fio.mounts":
		hasStatus = true
		table.Title = "probe.disk.table.mounts"
		table.Columns = []string{
			"probe.disk.column.mount", "probe.disk.column.device", "probe.disk.column.filesystem",
			"probe.disk.column.write", "probe.disk.column.read_iops", "probe.disk.column.status",
		}
	default:
		return
	}
	if !hasStatus {
		return
	}
	for rowIndex := range table.Rows {
		row := table.Rows[rowIndex]
		if len(row) == 0 {
			continue
		}
		row[len(row)-1] = diskRowStatusKey(table.Key, row)
	}
}

func diskRowStatusKey(tableKey string, row []string) string {
	present := func(value string) bool {
		value = strings.TrimSpace(value)
		return value != "" && value != "—" && !strings.EqualFold(value, "n/a")
	}
	switch tableKey {
	case "disk.fio.crystal", "disk.fio.atto":
		if len(row) >= 5 && present(row[1]) && present(row[2]) && present(row[3]) && present(row[4]) {
			return "probe.disk.status.complete"
		}
		return "probe.disk.status.missing"
	case "disk.fio.mounts":
		if len(row) >= 5 && (present(row[3]) || present(row[4])) {
			return "probe.disk.status.complete"
		}
		return "probe.disk.status.missing"
	default:
		return ""
	}
}

func stableDiskNotes(result model.Result) []string {
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
			values[measurement.Key] = measurement.Display
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
