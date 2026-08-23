package probe

import (
	"fmt"
	"strings"

	"ecs/internal/model"
)

// stabilizeStreamMemoryResult rewrites ECS-owned presentation metadata from
// stable machine identities before the result crosses the probe boundary. It
// never inspects a source-language sentence to decide meaning. Raw STREAM
// output remains untouched.
func stabilizeStreamMemoryResult(result *model.Result, allowance cpuAllowance) {
	if result == nil {
		return
	}
	result.Title = "module.memory.title"
	result.Description = "probe.memory.description"
	result.Methodology.Label = "methodology.standard-benchmark"
	result.Methodology.Profile = "probe.memory.stream.profile"
	result.Methodology.ComparisonScope = "probe.memory.comparison_scope"
	if allowance.Threads <= 1 {
		result.Description = "probe.memory.description.single_core"
		result.Methodology.Profile = "probe.memory.stream.profile.single_core"
	}

	for index := range result.Fields {
		field := &result.Fields[index]
		if key, ok := streamMemoryFieldLabelKeys[field.Key]; ok {
			field.Label = key
		}
		switch field.Key {
		case "cpu_allowance":
			field.Value = cpuAllowanceMachineValue(allowance)
		case "thread_control":
			field.Value = streamThreadControlMachineValue(allowance.Threads)
		case "rate_unit":
			field.Value = "MiB/s"
		}
	}

	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		if key := streamMemoryMeasurementLabelKey(measurement.Key); key != "" {
			measurement.Label = key
		}
	}

	for index := range result.TextBlocks {
		if index == 0 {
			result.TextBlocks[index].Title = "probe.memory.stream.raw.1t"
		} else {
			result.TextBlocks[index].Title = "probe.memory.stream.raw.nt"
		}
	}
	for index := range result.Sources {
		if strings.EqualFold(strings.TrimSpace(result.Sources[index].Name), "STREAM") {
			result.Sources[index].Purpose = "probe.memory.stream.source.purpose"
		}
	}
	for index := range result.Tables {
		stabilizeStreamMemoryTable(&result.Tables[index], allowance.Threads)
	}

	result.Notes = streamMemoryStableNotes(*result, allowance)
	summary := streamMemorySummaryTokens(*result, allowance.Threads)
	if len(summary) == 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.memory.stream.summary.none")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.memory.stream.summary.values", strings.Join(summary, " · "))}
	}
}

var streamMemoryFieldLabelKeys = map[string]string{
	"engine":            "probe.memory.stream.field.engine",
	"version":           "probe.memory.stream.field.version",
	"binary_sha256":     "probe.memory.stream.field.binary_sha256",
	"threads":           "probe.memory.stream.field.threads",
	"cpu_allowance":     "probe.memory.stream.field.cpu_allowance",
	"kernel_order":      "probe.memory.stream.field.kernel_order",
	"thread_control":    "probe.memory.stream.field.thread_control",
	"rate_unit":         "probe.memory.stream.field.rate_unit",
	"source_rate_units": "probe.memory.stream.field.source_rate_units",
}

var streamReportKernelOrder = []string{"Copy", "Triad", "Scale", "Add"}

func streamMemoryMeasurementLabelKey(key string) string {
	for _, kernel := range []string{"copy", "scale", "add", "triad"} {
		prefix := "stream_" + kernel + "_"
		switch key {
		case prefix + "1t_mib_s":
			return "probe.memory.stream.metric." + kernel + ".1t"
		case prefix + "nt_mib_s":
			return "probe.memory.stream.metric." + kernel + ".nt"
		case prefix + "scaling_ratio":
			return "probe.memory.stream.metric." + kernel + ".scaling"
		}
	}
	return ""
}

func streamThreadControlMachineValue(workers int) string {
	if workers <= 1 {
		return "OMP_NUM_THREADS=1;contexts=1T,NT;measurement=reused"
	}
	return fmt.Sprintf("OMP_NUM_THREADS=1;OMP_NUM_THREADS=%d;measurement=separate", workers)
}

func stabilizeStreamMemoryTable(table *model.Table, workers int) {
	if table == nil {
		return
	}
	switch table.Key {
	case "memory.stream.bandwidth":
		table.Title = "probe.memory.stream.table.bandwidth"
		table.Columns = []string{
			"probe.memory.stream.column.kernel_context",
			"probe.memory.stream.column.best_rate",
			"probe.memory.stream.column.raw_unit",
			"probe.memory.stream.column.method",
			"probe.memory.stream.column.evidence",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) < 5 {
				continue
			}
			kernel, context := streamMemoryRowIdentity(rowIndex, workers)
			row[0] = kernel + " / " + context
			switch {
			case row[1] == "—" || strings.TrimSpace(row[1]) == "":
				row[4] = "probe.memory.stream.evidence.failed"
			case workers <= 1 && rowIndex%2 == 1:
				row[4] = "probe.memory.stream.evidence.reused"
			default:
				row[4] = "probe.memory.stream.evidence.best_rate"
			}
		}
	case "memory.stream.stability":
		table.Title = "probe.memory.stream.table.stability"
		table.Columns = []string{
			"probe.memory.stream.column.kernel_context",
			"probe.memory.stream.column.average_seconds",
			"probe.memory.stream.column.minimum_seconds",
			"probe.memory.stream.column.maximum_seconds",
			"probe.memory.stream.column.spread_percent",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) < 5 {
				continue
			}
			kernel, context := streamMemoryRowIdentity(rowIndex, workers)
			row[0] = kernel + " / " + context
		}
	}
}

func streamMemoryRowIdentity(rowIndex, workers int) (string, string) {
	kernelIndex := rowIndex / 2
	kernel := "STREAM"
	if kernelIndex >= 0 && kernelIndex < len(streamReportKernelOrder) {
		kernel = streamReportKernelOrder[kernelIndex]
	}
	if rowIndex%2 == 0 {
		return kernel, "1T"
	}
	if workers <= 1 {
		return kernel, "NT(1T-reused)"
	}
	return kernel, fmt.Sprintf("NT(%dT)", workers)
}

func streamMemoryStableNotes(result model.Result, allowance cpuAllowance) []string {
	notes := make([]string, 0, 7)
	if !streamMemoryHasContextMeasurements(result, "1t") {
		notes = append(notes, "probe.memory.stream.note.run_failed.1t")
	}
	if allowance.Threads > 1 && !streamMemoryHasContextMeasurements(result, "nt") {
		notes = append(notes, "probe.memory.stream.note.run_failed.nt")
	}
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.memory.stream.note.single_core")
	} else {
		notes = append(notes, "probe.memory.stream.note.separate_runs")
	}
	notes = append(notes, "probe.memory.stream.note.units_normalized")
	if streamMemorySourceUnitsDiffer(result.Fields) {
		notes = append(notes, "probe.memory.stream.note.mixed_units")
	}
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.memory.stream.note.quota_limited")
	}
	return notes
}

func streamMemoryHasContextMeasurements(result model.Result, contextName string) bool {
	suffix := "_" + contextName + "_mib_s"
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 && strings.HasPrefix(measurement.Key, "stream_") && strings.HasSuffix(measurement.Key, suffix) {
			return true
		}
	}
	return false
}

func streamMemorySourceUnitsDiffer(fields []model.Field) bool {
	for _, field := range fields {
		if field.Key != "source_rate_units" {
			continue
		}
		parts := strings.Split(field.Value, " / ")
		if len(parts) < 2 {
			return false
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		return left != "" && right != "" && left != right
	}
	return false
}

func streamMemorySummaryTokens(result model.Result, workers int) []string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display
		}
	}
	if workers <= 1 {
		tokens := make([]string, 0, 2)
		if value := values["stream_copy_1t_mib_s"]; value != "" {
			tokens = append(tokens, "Copy 1T/NT "+value)
		}
		if value := values["stream_triad_1t_mib_s"]; value != "" {
			tokens = append(tokens, "Triad 1T/NT "+value)
		}
		return tokens
	}
	tokens := make([]string, 0, 4)
	for _, item := range []struct {
		key   string
		label string
	}{
		{key: "stream_copy_1t_mib_s", label: "Copy 1T"},
		{key: "stream_copy_nt_mib_s", label: fmt.Sprintf("Copy NT(%dT)", workers)},
		{key: "stream_triad_1t_mib_s", label: "Triad 1T"},
		{key: "stream_triad_nt_mib_s", label: fmt.Sprintf("Triad NT(%dT)", workers)},
	} {
		if value := values[item.key]; value != "" {
			tokens = append(tokens, item.label+" "+value)
		}
	}
	return tokens
}
