package probe

import (
	"context"
	"strings"

	"ecs/internal/model"
)

// The throughput probes deliberately keep their command runners and parsers
// unchanged. These adapters only replace presentation-owned values after the
// measurement is complete; command output and failure messages remain raw
// evidence.

type speedSemanticProbe struct{}

func (speedSemanticProbe) ID() string         { return "speed" }
func (speedSemanticProbe) Title() string      { return "module.speed.title" }
func (speedSemanticProbe) NeedsNetwork() bool { return true }

func (speedSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (speedProbe{}).Run(ctx, env)
	stabilizeSpeedResult(&result)
	return result
}

func stabilizeSpeedResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.speed.title"
	result.Description = "probe.speed.description"
	result.Methodology.Label = "methodology.standard-benchmark"
	result.Methodology.Profile = "probe.speed.profile"
	result.Methodology.ComparisonScope = "probe.speed.comparison_scope"
	for index := range result.Fields {
		result.Fields[index].Label = "probe.speed.field." + result.Fields[index].Key
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = speedMeasurementLabelKey(result.Measurements[index].Key)
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		switch table.Key {
		case "network.iperf3.results":
			table.Title = "probe.speed.table.results"
			table.Columns = []string{
				"probe.speed.column.provider", "probe.speed.column.location", "probe.speed.column.protocol",
				"probe.speed.column.upload", "probe.speed.column.download", "probe.speed.column.udp_loss",
				"probe.speed.column.udp_jitter", "probe.speed.column.port", "probe.speed.column.status",
			}
		case "network.iperf3.stability":
			table.Title = "probe.speed.table.stability"
			table.Columns = []string{
				"probe.speed.column.provider", "probe.speed.column.protocol", "probe.speed.column.direction",
				"probe.speed.column.minimum", "probe.speed.column.p50", "probe.speed.column.cv",
				"probe.speed.column.retransmits", "probe.speed.column.interval",
			}
		default:
			continue
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) == 0 {
				continue
			}
			switch row[len(row)-1] {
			case "完成":
				row[len(row)-1] = "probe.speed.status.complete"
			case "失败":
				row[len(row)-1] = "probe.speed.status.failed"
			case "部分":
				row[len(row)-1] = "probe.speed.status.partial"
			}
		}
	}
	for index := range result.Sources {
		if strings.EqualFold(result.Sources[index].Name, "iperf3") {
			result.Sources[index].Purpose = "probe.speed.source.iperf3"
		} else {
			result.Sources[index].Purpose = "probe.speed.source.registry"
		}
	}
	result.Notes = []string{
		"probe.speed.note.active_traffic",
		"probe.speed.note.public_nodes",
		"probe.speed.note.comparison",
		"probe.speed.note.raw_values",
		"probe.speed.note.udp_scope",
	}
	result.Summary = ""
	switch {
	case firstFailureAt(result, "tool_lookup") != nil:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.speed.summary.tool_missing")}
	case len(result.Measurements) == 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.speed.summary.none")}
	default:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.speed.summary.values", speedMachineSummary(*result))}
	}
}

func speedMeasurementLabelKey(key string) string {
	switch {
	case strings.HasSuffix(key, "_upload_mbps"):
		return "probe.speed.metric.upload"
	case strings.HasSuffix(key, "_download_mbps"):
		return "probe.speed.metric.download"
	case strings.HasSuffix(key, "_udp_loss_percent"):
		return "probe.speed.metric.udp_loss"
	case strings.HasSuffix(key, "_udp_jitter_ms"):
		return "probe.speed.metric.udp_jitter"
	case strings.HasSuffix(key, "_retransmits"):
		return "probe.speed.metric.retransmits"
	case strings.HasSuffix(key, "_interval_min_mbps"):
		return "probe.speed.metric.interval_min"
	case strings.HasSuffix(key, "_interval_p50_mbps"):
		return "probe.speed.metric.interval_p50"
	case strings.HasSuffix(key, "_interval_cv_percent"):
		return "probe.speed.metric.interval_cv"
	default:
		return "probe.speed.metric.value"
	}
}

func speedMachineSummary(result model.Result) string {
	parts := make([]string, 0, 4)
	for _, measurement := range result.Measurements {
		if measurement.Value <= 0 {
			continue
		}
		switch {
		case strings.HasSuffix(measurement.Key, "_upload_mbps"):
			parts = append(parts, "upload="+measurement.Display)
		case strings.HasSuffix(measurement.Key, "_download_mbps"):
			parts = append(parts, "download="+measurement.Display)
		}
		if len(parts) >= 4 {
			break
		}
	}
	return strings.Join(parts, ";")
}

type cnSpeedSemanticProbe struct{}

func (cnSpeedSemanticProbe) ID() string         { return "cnspeed" }
func (cnSpeedSemanticProbe) Title() string      { return "module.cnspeed.title" }
func (cnSpeedSemanticProbe) NeedsNetwork() bool { return true }

func (cnSpeedSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (cnSpeedProbe{}).Run(ctx, env)
	stabilizeCNSpeedResult(&result)
	return result
}

func stabilizeCNSpeedResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.cnspeed.title"
	result.Description = "probe.cnspeed.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.cnspeed.profile"
	result.Methodology.ComparisonScope = "probe.cnspeed.comparison_scope"
	for index := range result.Fields {
		field := &result.Fields[index]
		field.Label = "probe.cnspeed.field." + field.Key
		if field.Key == "node_list" {
			field.Value = "speedtest.cn-CN-ID@audited-commit"
		}
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = "probe.cnspeed.metric.download"
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.cnspeed.nodes" {
			continue
		}
		table.Title = "probe.cnspeed.table.nodes"
		table.Columns = []string{
			"probe.cnspeed.column.carrier", "probe.cnspeed.column.node", "probe.cnspeed.column.location",
			"probe.cnspeed.column.latency", "probe.cnspeed.column.download", "probe.cnspeed.column.transferred",
			"probe.cnspeed.column.status",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) == 0 {
				continue
			}
			row[0] = carrierMachineKey(row[0])
			if row[len(row)-1] == "完成" {
				row[len(row)-1] = "probe.cnspeed.status.complete"
			} else {
				row[len(row)-1] = "probe.cnspeed.status.failed"
			}
		}
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.cnspeed.source.nodes"
	}
	result.Notes = []string{
		"probe.cnspeed.note.pinned_nodes",
		"probe.cnspeed.note.address_safety",
		"probe.cnspeed.note.selection",
		"probe.cnspeed.note.scope",
		"probe.cnspeed.note.ookla_registry",
	}
	result.Summary = ""
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.cnspeed.summary.skipped")}
	} else if len(result.Measurements) == 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.cnspeed.summary.none")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.cnspeed.summary.values", cnSpeedMachineSummary(*result))}
	}
}

func carrierMachineKey(value string) string {
	switch strings.TrimSpace(value) {
	case "电信", "China Telecom", "telecom":
		return "probe.cnspeed.carrier.telecom"
	case "联通", "China Unicom", "unicom":
		return "probe.cnspeed.carrier.unicom"
	case "移动", "China Mobile", "mobile":
		return "probe.cnspeed.carrier.mobile"
	default:
		return value
	}
}

func cnSpeedMachineSummary(result model.Result) string {
	parts := make([]string, 0, len(result.Measurements))
	for _, measurement := range result.Measurements {
		parts = append(parts, measurement.Key+"="+measurement.Display)
	}
	return strings.Join(parts, ";")
}

type ooklaSemanticProbe struct{}

func (ooklaSemanticProbe) ID() string         { return "ookla" }
func (ooklaSemanticProbe) Title() string      { return "module.ookla.title" }
func (ooklaSemanticProbe) NeedsNetwork() bool { return true }

func (ooklaSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (ooklaProbe{}).Run(ctx, env)
	stabilizeOoklaResult(&result)
	return result
}

func stabilizeOoklaResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.ookla.title"
	result.Description = "probe.ookla.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Engine = "ookla-speedtest-cli"
	result.Methodology.Profile = "probe.ookla.profile"
	result.Methodology.ComparisonScope = "probe.ookla.comparison_scope"
	for index := range result.Fields {
		field := &result.Fields[index]
		switch {
		case strings.HasPrefix(field.Key, "isp_"):
			field.Label = "probe.ookla.field.isp"
		case strings.HasPrefix(field.Key, "external_ip_"):
			field.Label = "probe.ookla.field.external_ip"
		case strings.HasPrefix(field.Key, "error_"):
			field.Label = "probe.ookla.field.error"
		default:
			field.Label = "probe.ookla.field." + field.Key
		}
		if field.Key == "server_selection" {
			if strings.Contains(field.Value, "自动") {
				field.Value = "automatic"
			} else {
				field.Value = "configured"
			}
		}
		if field.Key == "skip_reason" {
			if strings.Contains(field.Value, "外联") {
				field.Value = "exposure_denied"
			} else {
				field.Value = "tool_unavailable"
			}
		}
		if field.Key == "next_step" {
			if field.Value == "请提高 --exposure 后重跑模块。" {
				field.Value = "rerun_with_more_exposure"
			} else {
				field.Value = "install_official_client"
			}
		}
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = ooklaMeasurementLabelKey(result.Measurements[index].Key)
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.ookla.results" {
			continue
		}
		table.Title = "probe.ookla.table.results"
		table.Columns = []string{
			"probe.ookla.column.carrier", "probe.ookla.column.server", "probe.ookla.column.latency",
			"probe.ookla.column.download", "probe.ookla.column.upload", "probe.ookla.column.loss", "probe.ookla.column.status",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) == 0 {
				continue
			}
			row[0] = carrierMachineKey(row[0])
			switch row[len(row)-1] {
			case "完成":
				row[len(row)-1] = "probe.ookla.status.complete"
			case "部分完成":
				row[len(row)-1] = "probe.ookla.status.partial"
			case "未解析":
				row[len(row)-1] = "probe.ookla.status.unparsed"
			}
		}
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.ookla.source.engine"
	}
	result.Notes = []string{
		"probe.ookla.note.external_service",
		"probe.ookla.note.no_raw_json",
		"probe.ookla.note.traffic",
	}
	result.Summary = ""
	switch {
	case result.Status == model.StatusSkipped:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.skipped")}
	case len(result.Measurements) == 0 && len(result.Tables) > 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.no_metric")}
	case len(result.Measurements) == 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.no_result")}
	default:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.values", ooklaMachineSummary(*result))}
	}
}

func ooklaMeasurementLabelKey(key string) string {
	switch {
	case strings.HasSuffix(key, "_latency_ms"):
		return "probe.ookla.metric.latency"
	case strings.HasSuffix(key, "_download_mbps"):
		return "probe.ookla.metric.download"
	case strings.HasSuffix(key, "_upload_mbps"):
		return "probe.ookla.metric.upload"
	case strings.HasSuffix(key, "_packet_loss_percent"):
		return "probe.ookla.metric.loss"
	default:
		return "probe.ookla.metric.value"
	}
}

func ooklaMachineSummary(result model.Result) string {
	parts := make([]string, 0, 4)
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			parts = append(parts, measurement.Key+"="+measurement.Display)
		}
		if len(parts) >= 4 {
			break
		}
	}
	return strings.Join(parts, ";")
}
