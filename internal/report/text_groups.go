package report

import (
	"fmt"
	"sort"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/textwidth"
)

type textGroup struct {
	key          string
	title        string
	fields       []model.Field
	measurements []model.Measurement
	tables       []model.Table
}

func (r *textRenderer) renderGroup(group textGroup) {
	r.subsectionNo++
	heading := fmt.Sprintf("%s、%s", chineseNumeral(r.subsectionNo), group.title)
	if i18n.Current() == i18n.LangEN {
		heading = fmt.Sprintf("%d. %s", r.subsectionNo, group.title)
	}
	r.line(r.palette.AccentBold("  " + heading))
	r.line(r.palette.Dim("  " + strings.Repeat("-", maxInt(0, r.width-2))))
	fields := group.fields
	measurements := group.measurements
	if len(fields) > 0 {
		r.fields(fields)
	}
	measurements = visibleMeasurements(model.Result{Measurements: measurements, Tables: group.tables})
	if len(measurements) > 0 {
		r.measurements(measurements)
	}
	for _, table := range group.tables {
		r.resultTable(table)
	}
}

func textGroups(result model.Result) []textGroup {
	groups := make([]textGroup, 0, 4)
	indexes := make(map[string]int)
	add := func(key, title string) *textGroup {
		if index, ok := indexes[key]; ok {
			return &groups[index]
		}
		groups = append(groups, textGroup{key: key, title: title})
		index := len(groups) - 1
		indexes[key] = index
		return &groups[index]
	}
	for _, field := range result.Fields {
		key := fieldGroupKey(result.ID, field.Key)
		group := add(key, groupTitleForKey(key, result.Title))
		group.fields = append(group.fields, field)
	}
	for _, measurement := range result.Measurements {
		key := fieldGroupKey(result.ID, measurement.Key)
		group := add(key, groupTitleForKey(key, result.Title))
		group.measurements = append(group.measurements, measurement)
	}
	for _, table := range result.Tables {
		group := add(tableGroupKey(result.ID, table.Key), tableGroupTitle(result.ID, table.Key, result.Title))
		group.tables = append(group.tables, table)
	}
	if len(groups) == 0 {
		groups = append(groups, textGroup{key: resultGroupKey(result.ID), title: fallbackGroupTitle()})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return textGroupOrder(result.ID, groups[i].key) < textGroupOrder(result.ID, groups[j].key)
	})
	return groups
}

func textGroupOrder(id, key string) int {
	switch id {
	case "system":
		switch key {
		case "system.hardware":
			return 0
		case "system.storage":
			return 1
		case "system.kernel":
			return 2
		default:
			return 3
		}
	case "network":
		switch key {
		case "network.egress":
			return 0
		case "network.ip":
			return 1
		case "network.risk":
			return 2
		default:
			return 3
		}
	default:
		return 0
	}
}

func resultGroupKey(id string) string {
	return "result:" + strings.TrimSpace(id)
}

func fieldGroupKey(id, key string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch id {
	case "system":
		return systemFieldGroupKey(lower)
	case "network":
		switch lower {
		case "fraud_record", "risk_level", "risk_score":
			return "network.risk"
		}
		if knownRiskMeasurementKey(lower) {
			return "network.risk"
		}
		return "network.ip"
	}
	return resultGroupKey(id)
}

func tableGroupKey(id, key string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch id {
	case "system":
		switch lower {
		case "system.kernel.network_parameters", "system.pressure.cgroup":
			return "system.kernel"
		default:
			return "system.hardware"
		}
	case "network":
		switch {
		case lower == "network.egress.overview":
			return "network.egress"
		case knownNetworkRiskTableKey(lower):
			return "network.risk"
		default:
			return "network.ip"
		}
	}
	return resultGroupKey(id)
}

func systemFieldGroupKey(key string) string {
	switch key {
	case "swap", "disk_device", "disk_mount", "disk_total", "disk_used", "disk_available", "disk_usage_percent",
		"disk_total_bytes", "disk_used_bytes", "disk_free_bytes", "uptime_seconds", "load", "block_devices":
		return "system.storage"
	case "tcp_congestion", "qdisc", "bbr_status", "tcp_congestion_control", "tcp_available_congestion",
		"tcp_rmem_max_bytes", "tcp_single_flow_window_limit_150ms_mbps":
		return "system.kernel"
	default:
		return "system.hardware"
	}
}

func knownRiskMeasurementKey(key string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "_")
	if len(parts) != 4 || (parts[0] != "ipv4" && parts[0] != "ipv6") ||
		parts[2] != "risk" || parts[3] != "score" {
		return false
	}
	return config.IsIPQualitySource(parts[1])
}

func knownNetworkRiskTableKey(key string) bool {
	switch key {
	case "network.ipquality.ipv4.scores", "network.ipquality.ipv4.factors",
		"network.ipquality.ipv6.scores", "network.ipquality.ipv6.factors":
		return true
	default:
		return false
	}
}

func fallbackGroupTitle() string {
	return i18n.T("report.group.moduleDetails")
}

func groupTitleForKey(key, resultTitle string) string {
	switch key {
	case "system.kernel":
		return i18n.T("report.group.system.kernel")
	case "system.storage":
		return i18n.T("report.group.system.storage")
	case "system.hardware":
		return i18n.T("report.group.system.hardware")
	case "network.risk":
		return i18n.T("report.group.network.risk")
	case "network.ip":
		return i18n.T("report.group.network.ip")
	}
	return defaultResultGroup(resultTitle)
}

func tableGroupTitle(id, key, resultTitle string) string {
	switch tableGroupKey(id, key) {
	case "system.hardware":
		return i18n.T("report.group.system.hardware")
	case "system.kernel":
		return i18n.T("report.group.system.kernel")
	case "network.risk":
		return i18n.T("report.group.network.risk")
	case "network.egress":
		return i18n.T("report.group.network.egress")
	case "network.ip":
		return i18n.T("report.group.network.ip")
	}
	return defaultResultGroup(resultTitle)
}

func defaultResultGroup(resultTitle string) string {
	if title := displayKey(resultTitle); title != "" {
		return title
	}
	return fallbackGroupTitle()
}

// fields 渲染 label: value 列表。
func (r *textRenderer) fields(items []model.Field) {
	labelLimit := minInt(28, maxInt(6, r.width/3))
	width := 0
	for _, rawItem := range items {
		item := displayField(rawItem)
		width = maxInt(width, textwidth.Width(item.Label))
	}
	width = minInt(width, labelLimit)
	for _, rawItem := range items {
		item := displayField(rawItem)
		value := displayValue(item.Value)
		label := textwidth.Pad(textwidth.Truncate(item.Label, labelLimit), width) + i18n.T("punct.colon")
		prefix := "  " + r.palette.Label(label) + "  "
		available := r.width - textwidth.Width(prefix)
		if available < 1 {
			available = maxInt(1, r.width-2)
		}
		valueLines := wrapText(value, available)
		if len(valueLines) == 0 {
			valueLines = []string{""}
		}
		for index, valueLine := range valueLines {
			linePrefix := prefix
			if index > 0 {
				linePrefix = strings.Repeat(" ", textwidth.Width(prefix))
			}
			renderedValue := r.styledValue(valueLine, item.Value)
			r.line(linePrefix + renderedValue)
		}
	}
	r.blank()
}
