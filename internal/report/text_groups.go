package report

import (
	"fmt"
	"sort"
	"strings"

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
	fields := dedupeFields(visibleFields(group.fields))
	measurements := dedupeMeasurements(fields, group.measurements)
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

func dedupeFields(items []model.Field) []model.Field {
	seen := make(map[string]bool, len(items))
	out := make([]model.Field, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.Key))
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		out = append(out, item)
	}
	return out
}

func visibleFields(items []model.Field) []model.Field {
	return append([]model.Field(nil), items...)
}

func dedupeMeasurements(fields []model.Field, items []model.Measurement) []model.Measurement {
	seen := make(map[string]bool, len(fields)+len(items))
	for _, field := range fields {
		key := strings.ToLower(strings.TrimSpace(field.Key))
		if key != "" {
			seen[key] = true
		}
	}
	out := make([]model.Measurement, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.Key))
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		out = append(out, item)
	}
	return out
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
		group := add(fieldGroupKey(result.ID, field.Key), fieldGroupTitle(result.ID, field.Key, result.Title))
		group.fields = append(group.fields, field)
	}
	for _, measurement := range result.Measurements {
		group := add(fieldGroupKey(result.ID, measurement.Key), measurementGroupTitle(result.ID, measurement.Key, result.Title))
		group.measurements = append(group.measurements, measurement)
	}
	for _, table := range result.Tables {
		group := add(tableGroupKey(result.ID, table.Key), tableGroupTitle(result.ID, table.Key, result.Title))
		group.tables = append(group.tables, table)
	}
	if len(groups) == 0 {
		groups = append(groups, textGroup{key: resultGroupKey(result.ID), title: fallbackGroupTitle(result.ID)})
	}
	groupOrder := func(key string) int {
		orders := map[string][]string{
			"system":  {"system.hardware", "system.storage", "system.kernel"},
			"network": {"network.egress", "network.ip", "network.risk"},
		}
		for index, name := range orders[result.ID] {
			if name == key {
				return index
			}
		}
		return len(orders[result.ID]) + 1
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groupOrder(groups[i].key) < groupOrder(groups[j].key)
	})
	return groups
}

func resultGroupKey(id string) string {
	return "result:" + strings.TrimSpace(id)
}

func fieldGroupKey(id, key string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	if id == "system" {
		if strings.HasPrefix(lower, "tcp") || strings.HasPrefix(lower, "net") || strings.Contains(lower, "ipv6") || strings.Contains(lower, "forward") || strings.Contains(lower, "syn") || strings.Contains(lower, "mtu") || strings.Contains(lower, "queue") || strings.Contains(lower, "conntrack") {
			return "system.kernel"
		}
		if strings.HasPrefix(lower, "disk") || strings.HasPrefix(lower, "swap") || strings.HasPrefix(lower, "load") || strings.Contains(lower, "uptime") || strings.HasPrefix(lower, "block") {
			return "system.storage"
		}
		return "system.hardware"
	}
	if id == "network" {
		if strings.Contains(lower, "risk") || strings.Contains(lower, "fraud") || strings.Contains(lower, "proxy") || strings.Contains(lower, "vpn") || strings.Contains(lower, "tor") {
			return "network.risk"
		}
		return "network.ip"
	}
	return resultGroupKey(id)
}

func tableGroupKey(id, key string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	if id == "system" {
		return "system.kernel"
	}
	if id == "network" {
		switch {
		case lower == "network.egress.overview":
			return "network.egress"
		case strings.HasPrefix(lower, "network.ipquality.") && (strings.HasSuffix(lower, ".scores") || strings.HasSuffix(lower, ".factors")):
			return "network.risk"
		default:
			return "network.ip"
		}
	}
	return resultGroupKey(id)
}

func fallbackGroupTitle(id string) string {
	if i18n.Current() == i18n.LangEN {
		return "Module details"
	}
	return "模块详情"
}

func fieldGroupTitle(id, key, resultTitle string) string {
	switch fieldGroupKey(id, key) {
	case "system.kernel":
		return localizedGroup("内核网络", "Kernel networking")
	case "system.storage":
		return localizedGroup("磁盘与运行状态", "Storage/runtime")
	case "system.hardware":
		return localizedGroup("操作系统与硬件", "OS/hardware")
	case "network.risk":
		return localizedGroup("风险矩阵", "Risk matrix")
	case "network.ip":
		return localizedGroup("IP 信息", "IP information")
	}
	return defaultResultGroup(id, resultTitle)
}

func measurementGroupTitle(id, key, resultTitle string) string {
	return fieldGroupTitle(id, key, resultTitle)
}

func tableGroupTitle(id, key, resultTitle string) string {
	switch tableGroupKey(id, key) {
	case "system.kernel":
		return localizedGroup("内核网络", "Kernel networking")
	case "network.risk":
		return localizedGroup("风险矩阵", "Risk matrix")
	case "network.egress":
		return localizedGroup("出口概览", "Egress overview")
	case "network.ip":
		return localizedGroup("IP 信息", "IP information")
	}
	return defaultResultGroup(id, resultTitle)
}

func defaultResultGroup(id, resultTitle string) string {
	if title := displayKey(resultTitle); title != "" {
		return title
	}
	return fallbackGroupTitle(id)
}

func localizedGroup(zh, en string) string {
	if i18n.Current() == i18n.LangEN {
		return en
	}
	return zh
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
