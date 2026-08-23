package probe

import (
	"context"
	"strings"

	"ecs/internal/model"
)

type backtraceSemanticProbe struct{}

func (backtraceSemanticProbe) ID() string         { return "backtrace" }
func (backtraceSemanticProbe) Title() string      { return "module.backtrace.title" }
func (backtraceSemanticProbe) NeedsNetwork() bool { return true }

func (backtraceSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (backtraceProbe{}).Run(ctx, env)
	stabilizeBacktraceResult(&result)
	return result
}

func stabilizeBacktraceResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.backtrace.title"
	result.Description = "probe.backtrace.description"
	result.Methodology.Label = "methodology.heuristic"
	result.Methodology.Profile = "probe.backtrace.profile"
	result.Methodology.ComparisonScope = "probe.backtrace.comparison_scope"
	for index := range result.Fields {
		result.Fields[index].Label = "probe.backtrace.field." + result.Fields[index].Key
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = "probe.backtrace.metric.identified"
	}
	for index := range result.TextBlocks {
		result.TextBlocks[index].Title = "probe.backtrace.raw_output"
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.backtrace.source.method"
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		switch table.Key {
		case "network.backtrace.summary":
			table.Title = "probe.backtrace.table.summary"
			table.Columns = []string{
				"probe.backtrace.column.provider", "probe.backtrace.column.target", "probe.backtrace.column.line",
				"probe.backtrace.column.hit_hop", "probe.backtrace.column.hit_ip", "probe.backtrace.column.status",
			}
			for rowIndex := range table.Rows {
				row := table.Rows[rowIndex]
				if len(row) > 5 {
					row[5] = backtraceStatusKey(row[5])
				}
			}
		case "network.backtrace.hops":
			table.Title = "probe.backtrace.table.hops"
			table.Columns = []string{
				"probe.backtrace.column.target", "probe.backtrace.column.provider", "probe.backtrace.column.hop",
				"probe.backtrace.column.latency", "probe.backtrace.column.ip", "probe.backtrace.column.asn",
				"probe.backtrace.column.network", "probe.backtrace.column.location", "probe.backtrace.column.status",
			}
			for rowIndex := range table.Rows {
				row := table.Rows[rowIndex]
				if len(row) > 8 && row[8] == "追踪失败" {
					row[8] = "probe.backtrace.status.failed"
				}
			}
		}
	}
	result.Notes = []string{
		"probe.backtrace.note.active_path",
		"probe.backtrace.note.signature_scope",
		"probe.backtrace.note.ipv6_targets",
		"probe.backtrace.note.unidentified",
	}
	result.Summary = ""
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.skipped")}
	} else {
		identified := 0
		if len(result.Measurements) > 0 {
			identified = int(result.Measurements[0].Value)
		}
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.values", identified, backtraceTargetCount(*result))}
	}
}

func backtraceStatusKey(value string) string {
	switch {
	case value == "追踪失败":
		return "probe.backtrace.status.failed"
	case value == "已识别":
		return "probe.backtrace.status.identified"
	case value == "未识别" || strings.Contains(value, "无已知特征") || strings.Contains(value, "无响应"):
		return "probe.backtrace.status.unidentified"
	default:
		return value
	}
}

func backtraceTargetCount(result model.Result) int {
	for _, table := range result.Tables {
		if table.Key == "network.backtrace.summary" {
			return len(table.Rows)
		}
	}
	return 0
}
