package probe

import (
	"context"
	"fmt"
	"strings"

	"ecs/internal/model"
)

type zstdSemanticProbe struct{}

func (zstdSemanticProbe) ID() string         { return "zstd" }
func (zstdSemanticProbe) Title() string      { return "module.zstd.title" }
func (zstdSemanticProbe) NeedsNetwork() bool { return false }

func (zstdSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (zstdProbe{}).Run(ctx, env)
	stabilizeZstdResult(&result, detectCPUAllowance())
	return result
}

func stabilizeZstdResult(result *model.Result, allowance cpuAllowance) {
	if result == nil {
		return
	}
	result.Title = "module.zstd.title"
	result.Description = "probe.zstd.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "zstd",
		Profile:         "probe.zstd.profile",
		ComparisonScope: "probe.zstd.comparison_scope",
	}
	for index := range result.Fields {
		field := &result.Fields[index]
		field.Label = "probe.zstd.field." + field.Key
		switch field.Key {
		case "cpu_allowance":
			field.Value = cpuAllowanceMachineValue(allowance)
		case "corpus_construction":
			field.Value = "dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml"
		}
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = "probe.zstd.metric." + result.Measurements[index].Key
	}
	for index := range result.TextBlocks {
		result.TextBlocks[index].Title = "probe.zstd.raw_output"
	}
	for index := range result.Sources {
		switch strings.ToLower(result.Sources[index].Name) {
		case "zstandard":
			result.Sources[index].Purpose = "probe.zstd.source.zstandard"
		case "silesia corpus":
			result.Sources[index].Purpose = "probe.zstd.source.silesia"
		}
	}
	for index := range result.Tables {
		stabilizeZstdTable(&result.Tables[index], allowance.Threads)
	}
	result.Notes = stableZstdNotes(*result, allowance)
	result.Summary = ""
	if summary := zstdMachineSummary(*result, allowance.Threads); summary != "" {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.zstd.summary.values", summary)}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.zstd.summary.none")}
	}
}

func stabilizeZstdTable(table *model.Table, workers int) {
	if table == nil || table.Key != "benchmark.zstd.throughput" {
		return
	}
	table.Title = "probe.zstd.table.title"
	table.Columns = []string{
		"probe.zstd.column.context",
		"probe.zstd.column.compress",
		"probe.zstd.column.decompress",
		"probe.zstd.column.compress_scaling",
		"probe.zstd.column.decompress_scaling",
		"probe.zstd.column.compress_efficiency",
		"probe.zstd.column.decompress_efficiency",
	}
	for index := range table.Rows {
		row := table.Rows[index]
		if len(row) < 7 {
			continue
		}
		if index == 0 {
			row[0] = "1T"
		} else if workers <= 1 {
			row[0] = "NT(1T-reused)"
		} else {
			row[0] = fmt.Sprintf("NT(%dT)", workers)
		}
		if workers <= 1 {
			for column := 3; column < len(row); column++ {
				row[column] = "na"
			}
		}
	}
}

func stableZstdNotes(result model.Result, allowance cpuAllowance) []string {
	notes := []string{
		"probe.zstd.note.contract",
		"probe.zstd.note.corpus",
		"probe.zstd.note.units",
		"probe.zstd.note.decompression",
		"probe.zstd.note.no_composite_score",
	}
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.zstd.note.single_core")
	} else {
		notes = append(notes, "probe.zstd.note.separate_runs")
	}
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.zstd.note.quota_limited")
	}
	for _, failure := range result.Failures {
		switch failure.Stage {
		case "tool_lookup":
			notes = append(notes, "probe.zstd.note.tool_missing")
		case "version_check":
			notes = append(notes, "probe.zstd.note.version_mismatch")
		case "corpus_verify":
			notes = append(notes, "probe.zstd.note.corpus_invalid")
		case "benchmark_run":
			notes = append(notes, "probe.zstd.note.run_failure")
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

func zstdMachineSummary(result model.Result, workers int) string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display
		}
	}
	parts := make([]string, 0, 3)
	if value := values["zstd_compress_1t_mb_s"]; value != "" {
		parts = append(parts, "compress:1T="+value)
	}
	if workers > 1 {
		if value := values["zstd_compress_nt_mb_s"]; value != "" {
			parts = append(parts, fmt.Sprintf("compress:NT(%dT)=%s", workers, value))
		}
		if value := values["zstd_compress_scaling_ratio"]; value != "" {
			parts = append(parts, "compress:scaling="+value)
		}
	}
	return strings.Join(parts, ";")
}
