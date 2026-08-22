package probe

import (
	"context"
	"strconv"
	"strings"

	"ecs/internal/model"
)

type networkSemanticProbe struct{}

func (networkSemanticProbe) ID() string         { return "network" }
func (networkSemanticProbe) Title() string      { return "module.network.title" }
func (networkSemanticProbe) NeedsNetwork() bool { return true }

func (networkSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (networkProbe{}).Run(ctx, env)
	stabilizeNetworkResult(&result)
	return result
}

func stabilizeNetworkResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.network.title"
	result.Description = "probe.network.description"
	result.Methodology.Label = "methodology.provider-assessment"
	result.Methodology.Profile = "probe.network.profile"
	result.Methodology.ComparisonScope = "probe.network.comparison_scope"
	for index := range result.Fields {
		field := &result.Fields[index]
		field.Label = networkFieldLabelKey(field.Key)
		if strings.HasSuffix(field.Key, "_ip_type") {
			field.Value = networkIPTypeKey(field.Value)
		}
	}
	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		measurement.Label = "probe.network.metric.risk_score"
		measurement.Rating = networkRiskKey(measurement.Rating)
	}
	for index := range result.Tables {
		stabilizeNetworkTable(&result.Tables[index])
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = networkSourcePurposeKey(result.Sources[index].Name)
	}
	if result.Status == model.StatusWarning {
		result.Notes = []string{
			"probe.network.note.third_party",
			"probe.network.note.no_upload",
			"probe.network.note.source_semantics",
			"probe.network.note.origin_scope",
			"probe.network.note.partial_sources",
		}
	} else {
		result.Notes = []string{
			"probe.network.note.third_party",
			"probe.network.note.no_upload",
			"probe.network.note.source_semantics",
			"probe.network.note.origin_scope",
		}
	}
	result.Summary = ""
	result.SummaryMessages = []model.Message{
		model.NewMessage("probe.network.summary.values", networkMachineSummary(*result)),
	}
}

func networkFieldLabelKey(key string) string {
	switch {
	case key == "ip_version_mode":
		return "probe.network.field.ip_version_mode"
	case strings.HasSuffix(key, "_lookup_error"):
		return "probe.network.field.lookup_error"
	case strings.HasSuffix(key, "_asn"):
		return "probe.network.field.asn"
	case strings.HasSuffix(key, "_route"):
		return "probe.network.field.route"
	case strings.HasSuffix(key, "_location"):
		return "probe.network.field.location"
	case strings.HasSuffix(key, "_owner"):
		return "probe.network.field.owner"
	case strings.HasSuffix(key, "_ip_type"):
		return "probe.network.field.ip_type"
	case strings.HasPrefix(key, "ipv"):
		return "probe.network.field.egress"
	default:
		return "probe.network.field.value"
	}
}

func networkIPTypeKey(value string) string {
	switch {
	case strings.HasPrefix(value, "原生 IP"):
		return "probe.network.ip_type.native"
	case strings.HasPrefix(value, "广播 IP"):
		return "probe.network.ip_type.broadcast"
	case strings.Contains(value, "无法判定"), strings.Contains(value, "类型未判定"):
		return "probe.network.ip_type.unknown"
	default:
		return value
	}
}

func networkRiskKey(value string) string {
	switch strings.TrimSpace(value) {
	case "极低", "Very Low":
		return "probe.network.risk.very_low"
	case "低", "Low":
		return "probe.network.risk.low"
	case "中", "Medium":
		return "probe.network.risk.medium"
	case "注意", "疑似", "Suspicious":
		return "probe.network.risk.suspicious"
	case "高", "High":
		return "probe.network.risk.high"
	case "极高", "Very High":
		return "probe.network.risk.very_high"
	default:
		return value
	}
}

func networkCellKey(value string) string {
	switch strings.TrimSpace(value) {
	case "是", "Yes":
		return "probe.network.boolean.yes"
	case "否", "No":
		return "probe.network.boolean.no"
	case "未返回", "unknown", "Unknown":
		return "probe.network.boolean.unknown"
	case "成功", "正常":
		return "probe.network.status.ok"
	case "失败":
		return "probe.network.status.failed"
	case "部分", "部分成功":
		return "probe.network.status.partial"
	case "未启用":
		return "probe.network.status.disabled"
	case "原生 IP":
		return "probe.network.ip_type.native"
	case "广播 IP":
		return "probe.network.ip_type.broadcast"
	case "无法判定", "类型未判定":
		return "probe.network.ip_type.unknown"
	default:
		return value
	}
}

func stabilizeNetworkTable(table *model.Table) {
	if table == nil {
		return
	}
	switch {
	case table.Key == "network.egress.overview":
		table.Title = "probe.network.table.overview"
		table.Columns = []string{
			"probe.network.column.ip_family", "probe.network.column.network_type", "probe.network.column.datacenter",
			"probe.network.column.proxy", "probe.network.column.vpn", "probe.network.column.tor",
			"probe.network.column.abuse", "probe.network.column.duration",
		}
	case strings.HasSuffix(table.Key, ".types"):
		table.Title = "probe.network.table.ipquality.types"
		table.Columns = []string{
			"probe.network.column.source", "probe.network.column.usage", "probe.network.column.company",
			"probe.network.column.country", "probe.network.column.channel",
		}
	case strings.HasSuffix(table.Key, ".scores"):
		table.Title = "probe.network.table.ipquality.scores"
		table.Columns = []string{
			"probe.network.column.source", "probe.network.column.raw_value", "probe.network.column.risk",
			"probe.network.column.visualization", "probe.network.column.definition", "probe.network.column.bucket",
			"probe.network.column.channel",
		}
	case strings.HasSuffix(table.Key, ".factors"):
		table.Title = "probe.network.table.ipquality.factors"
		columns := []string{"probe.network.column.factor"}
		for _, column := range table.ColumnKeys[1:] {
			columns = append(columns, "probe.network.column.source."+column)
		}
		table.Columns = columns
	case strings.HasSuffix(table.Key, ".sources"):
		table.Title = "probe.network.table.ipquality.sources"
		table.Columns = []string{
			"probe.network.column.source", "probe.network.column.status", "probe.network.column.channel", "probe.network.column.duration",
		}
	default:
		return
	}
	for rowIndex := range table.Rows {
		row := table.Rows[rowIndex]
		for columnIndex := range row {
			if strings.HasSuffix(table.Key, ".factors") && columnIndex == 0 {
				row[columnIndex] = networkFactorKey(row[columnIndex])
				continue
			}
			if strings.HasSuffix(table.Key, ".scores") && columnIndex == 5 {
				row[columnIndex] = "probe.network.score_band." + networkSourceID(row[0])
				continue
			}
			row[columnIndex] = networkCellKey(row[columnIndex])
		}
	}
}

func networkFactorKey(value string) string {
	switch strings.TrimSpace(value) {
	case "国家/地区":
		return "probe.network.factor.country"
	case "代理":
		return "probe.network.factor.proxy"
	case "Tor":
		return "probe.network.factor.tor"
	case "VPN":
		return "probe.network.factor.vpn"
	case "机房":
		return "probe.network.factor.datacenter"
	case "滥用":
		return "probe.network.factor.abuse"
	case "机器人":
		return "probe.network.factor.robot"
	default:
		return value
	}
}

func networkSourceID(value string) string {
	for _, item := range map[string]string{
		"MaxMind": "maxmind", "IPinfo": "ipinfo", "ipregistry": "ipregistry", "ipapi": "ipapi",
		"IP2Location": "ip2location", "AbuseIPDB": "abuseipdb", "Scamalytics": "scamalytics",
		"IPQS": "ipqs", "DB-IP": "dbip", "ipdata": "ipdata", "IPWHOIS": "ipwhois",
		"ip-api": "ipapicom", "ip.sb": "ipsb",
	} {
		if value == item {
			return item
		}
	}
	return strings.ToLower(strings.ReplaceAll(value, " ", "_"))
}

func networkSourcePurposeKey(name string) string {
	if strings.EqualFold(name, "ipapi.is") {
		return "probe.network.source.ipapi"
	}
	if strings.EqualFold(name, "RouteViews") {
		return "probe.network.source.routeviews"
	}
	return "probe.network.source.provider"
}

func networkMachineSummary(result model.Result) string {
	parts := make([]string, 0, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value >= 0 {
			parts = append(parts, measurement.Key+"="+measurement.Display)
		}
		if len(parts) >= 6 {
			break
		}
	}
	if len(parts) == 0 {
		return strconv.Itoa(len(result.Tables)) + " tables"
	}
	return strings.Join(parts, ";")
}

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
