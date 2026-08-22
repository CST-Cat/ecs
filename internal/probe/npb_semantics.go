package probe

import (
	"context"
	"fmt"
	"strings"

	"ecs/internal/model"
)

// npbSemanticProbe keeps the NPB executor/parser unchanged while replacing
// ECS-owned presentation text before the result crosses the probe boundary.
type npbSemanticProbe struct{}

func (npbSemanticProbe) ID() string         { return "npb" }
func (npbSemanticProbe) Title() string      { return "module.npb.title" }
func (npbSemanticProbe) NeedsNetwork() bool { return false }

func (npbSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (npbProbe{}).Run(ctx, env)
	stabilizeNPBResult(&result, detectCPUAllowance())
	return result
}

func stabilizeNPBResult(result *model.Result, allowance cpuAllowance) {
	if result == nil {
		return
	}
	result.Title = "module.npb.title"
	result.Description = "probe.npb.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "NAS Parallel Benchmarks OpenMP",
		Profile:         "probe.npb.profile",
		ComparisonScope: "probe.npb.comparison_scope",
	}

	for index := range result.Fields {
		field := &result.Fields[index]
		field.Label = "probe.npb.field." + field.Key
		if field.Key == "cpu_allowance" {
			field.Value = cpuAllowanceMachineValue(allowance)
		}
	}
	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		measurement.Label = "probe.npb.metric." + measurement.Key
	}
	for index := range result.TextBlocks {
		result.TextBlocks[index].Title = "probe.npb.raw_output"
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.npb.source.purpose"
	}
	for index := range result.Tables {
		stabilizeNPBTable(&result.Tables[index], allowance.Threads)
	}

	result.Notes = stableNPBNotes(*result, allowance)
	result.Summary = ""
	if summary := npbMachineSummary(*result, allowance.Threads); summary != "" {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.npb.summary.values", summary)}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.npb.summary.none")}
	}
}

func stabilizeNPBTable(table *model.Table, workers int) {
	if table == nil || table.Key != "benchmark.npb.results" {
		return
	}
	table.Title = "probe.npb.table.title"
	table.Columns = []string{
		"probe.npb.column.benchmark",
		"probe.npb.column.workload",
		"probe.npb.column.context",
		"probe.npb.column.mops_total",
		"probe.npb.column.mops_per_thread",
		"probe.npb.column.elapsed",
		"probe.npb.column.scaling",
		"probe.npb.column.verification",
	}
	seen := make(map[string]int, 2)
	for rowIndex := range table.Rows {
		row := table.Rows[rowIndex]
		if len(row) < 8 {
			continue
		}
		benchmark := strings.ToUpper(strings.TrimSpace(row[0]))
		seen[benchmark]++
		switch benchmark {
		case "EP":
			row[1] = "probe.npb.workload.ep"
		case "FT":
			row[1] = "probe.npb.workload.ft"
		default:
			row[1] = "probe.npb.workload.unknown"
		}
		if seen[benchmark] == 1 {
			row[2] = "1T"
		} else if workers <= 1 {
			row[2] = "NT(1T-reused)"
		} else {
			row[2] = fmt.Sprintf("NT(%dT)", workers)
		}
		if strings.TrimSpace(row[3]) == "" || row[3] == "—" {
			row[7] = "probe.npb.verification.failed"
		} else {
			row[7] = "probe.npb.verification.successful"
		}
		if workers <= 1 {
			row[6] = "na"
		} else if strings.TrimSpace(row[6]) == "" || row[6] == "—" {
			row[6] = "unavailable"
		}
	}
}

func stableNPBNotes(result model.Result, allowance cpuAllowance) []string {
	notes := make([]string, 0, 7)
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.npb.note.single_core")
	} else {
		notes = append(notes, "probe.npb.note.separate_runs")
	}
	notes = append(notes,
		"probe.npb.note.workloads",
		"probe.npb.note.verification",
		"probe.npb.note.no_composite_score",
	)
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.npb.note.quota_limited")
	}
	for _, failure := range result.Failures {
		switch failure.Stage {
		case "tool_lookup":
			notes = append(notes, "probe.npb.note.tool_missing")
		case "benchmark_run":
			notes = append(notes, "probe.npb.note.run_failure")
		}
	}
	seen := make(map[string]bool, len(notes))
	out := notes[:0]
	for _, note := range notes {
		if seen[note] {
			continue
		}
		seen[note] = true
		out = append(out, note)
	}
	return out
}

func npbMachineSummary(result model.Result, workers int) string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display
		}
	}
	parts := make([]string, 0, 6)
	for _, benchmark := range []string{"ep", "ft"} {
		upper := strings.ToUpper(benchmark)
		if value := values["npb_"+benchmark+"_1t_mops"]; value != "" {
			parts = append(parts, upper+":1T="+value)
		}
		if workers > 1 {
			if value := values["npb_"+benchmark+"_nt_mops"]; value != "" {
				parts = append(parts, fmt.Sprintf("%s:NT(%dT)=%s", upper, workers, value))
			}
			if value := values["npb_"+benchmark+"_scaling_ratio"]; value != "" {
				parts = append(parts, upper+":scaling="+value)
			}
		}
	}
	return strings.Join(parts, ";")
}
