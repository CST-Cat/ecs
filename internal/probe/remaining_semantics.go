package probe

import (
	"context"
	"strings"

	"ecs/internal/model"
)

type mediaSemanticProbe struct{}

func (mediaSemanticProbe) ID() string         { return "media" }
func (mediaSemanticProbe) Title() string      { return "module.media.title" }
func (mediaSemanticProbe) NeedsNetwork() bool { return true }

func (mediaSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (mediaProbe{}).Run(ctx, env)
	stabilizeMediaResult(&result)
	return result
}

func stabilizeMediaResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.media.title"
	result.Description = "probe.media.description"
	result.Methodology.Label = "methodology.heuristic"
	result.Methodology.Profile = "probe.media.profile"
	result.Methodology.ComparisonScope = "probe.media.comparison_scope"
	for index := range result.Measurements {
		if result.Measurements[index].Key == "media_unlocked" {
			result.Measurements[index].Label = "probe.media.metric.unlocked"
		} else {
			result.Measurements[index].Label = "probe.media.metric.unknown"
		}
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		table.Title = "probe.media.table." + strings.TrimPrefix(table.Key, "network.media.")
		table.Columns = []string{
			"probe.media.column.platform", "probe.media.column.verdict", "probe.media.column.region",
			"probe.media.column.evidence", "probe.media.column.strength", "probe.media.column.duration",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) < 5 {
				continue
			}
			row[1] = mediaVerdictKey(row[1])
			row[3] = "probe.media.evidence.observed"
			row[4] = mediaStrengthKey(row[4])
		}
	}
	result.Notes = []string{
		"probe.media.note.public_evidence",
		"probe.media.note.account_scope",
		"probe.media.note.unknown_semantics",
	}
	result.Summary = ""
	result.SummaryMessages = []model.Message{model.NewMessage("probe.media.summary.values", mediaMachineSummary(*result))}
}

func mediaVerdictKey(value string) string {
	switch strings.TrimSpace(value) {
	case "解锁":
		return "probe.media.verdict.unlocked"
	case "仅自制剧":
		return "probe.media.verdict.originals"
	case "不解锁":
		return "probe.media.verdict.locked"
	case "需登录":
		return "probe.media.verdict.login"
	case "受限":
		return "probe.media.verdict.restricted"
	case "不可达":
		return "probe.media.verdict.unreachable"
	default:
		return "probe.media.verdict.unknown"
	}
}

func mediaStrengthKey(value string) string {
	if strings.TrimSpace(value) == "强" {
		return "probe.media.strength.strong"
	}
	return "probe.media.strength.weak"
}

func mediaMachineSummary(result model.Result) string {
	values := make([]string, 0, len(result.Measurements))
	for _, measurement := range result.Measurements {
		values = append(values, measurement.Key+"="+measurement.Display)
	}
	return strings.Join(values, ";")
}

type routeSemanticProbe struct{}

func (routeSemanticProbe) ID() string         { return "route" }
func (routeSemanticProbe) Title() string      { return "module.route.title" }
func (routeSemanticProbe) NeedsNetwork() bool { return true }

func (routeSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (routeProbe{}).Run(ctx, env)
	stabilizeRouteResult(&result)
	return result
}

func stabilizeRouteResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.route.title"
	result.Description = "probe.route.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.route.profile"
	result.Methodology.ComparisonScope = "probe.route.comparison_scope"
	for index := range result.Fields {
		result.Fields[index].Label = "probe.route.field." + result.Fields[index].Key
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = routeMeasurementLabelKey(result.Measurements[index].Key)
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.route.summary" {
			continue
		}
		table.Title = "probe.route.table.summary"
		table.Columns = []string{
			"probe.route.column.target", "probe.route.column.target_type", "probe.route.column.status",
			"probe.route.column.probed_hops", "probe.route.column.visible_hops", "probe.route.column.timeout_hops", "probe.route.column.duration",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) == 0 {
				continue
			}
			row[2] = routeStatusKey(row[2])
		}
	}
	for index := range result.TextBlocks {
		result.TextBlocks[index].Title = "probe.route.raw_output"
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.route.source.nexttrace"
	}
	result.Notes = []string{"probe.route.note.forward_path", "probe.route.note.execution", "probe.route.note.json"}
	result.Summary = ""
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.route.summary.skipped")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.route.summary.values", routeSuccessCount(*result), routeTargetCount(*result))}
	}
}

func routeMeasurementLabelKey(key string) string {
	switch {
	case strings.HasSuffix(key, "_hop_slots"):
		return "probe.route.metric.hop_slots"
	case strings.HasSuffix(key, "_visible_hops"):
		return "probe.route.metric.visible_hops"
	case strings.HasSuffix(key, "_timeout_hops"):
		return "probe.route.metric.timeout_hops"
	case strings.HasSuffix(key, "_duration_ms"):
		return "probe.route.metric.duration"
	default:
		return "probe.route.metric.value"
	}
}

func routeStatusKey(value string) string {
	switch value {
	case "完成":
		return "probe.route.status.complete"
	case "部分/失败":
		return "probe.route.status.failed"
	case "NextTrace 解析失败":
		return "probe.route.status.parse_failed"
	case "无响应":
		return "probe.route.status.no_response"
	default:
		return value
	}
}

func routeSuccessCount(result model.Result) int {
	for _, measurement := range result.Measurements {
		if strings.HasSuffix(measurement.Key, "_visible_hops") {
			// The table is authoritative for targets; a parsed zero-hop trace still
			// counts as valid evidence, so use the status rows below instead.
			_ = measurement
		}
	}
	count := 0
	for _, table := range result.Tables {
		if table.Key != "network.route.summary" {
			continue
		}
		for _, row := range table.Rows {
			if len(row) > 2 && (row[2] == "完成" || row[2] == "无响应" ||
				row[2] == "probe.route.status.complete" || row[2] == "probe.route.status.no_response") {
				count++
			}
		}
	}
	return count
}

func routeTargetCount(result model.Result) int {
	for _, table := range result.Tables {
		if table.Key == "network.route.summary" {
			return len(table.Rows)
		}
	}
	return 0
}

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
