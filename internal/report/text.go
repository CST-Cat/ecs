package report

// 纯文本报告。
//
// json 给机器读，markdown 给 GitHub 和编辑器读，html 给浏览器读——都缺一个
// "在终端里直接看完、顺手贴进论坛或聊天窗"的形态。这就是 txt 的位置。
//
// 呈现上借鉴了社区里流传的体检报告排版（分区标题块、中文数字章节、紧凑的
// label:value 列），但柱状图部分是重做的：那些报告里的柱子大多不随数值变化，
// 183ms 与 113ms 画成一样长，等于把装饰当信息。这里的柱长一律按比例。
//
// 颜色按终端能力自适应（见 internal/termcolor），并且层次不单独依赖颜色：
// 密度字符在无色环境里照样把高低分出来。默认写进文件时不带转义序列，
// 需要彩色文件时用 --color always。

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

// textWidth 是报告的版面宽度。
//
// 110 列给三网回程、风险矩阵等宽表格留出可读空间，同时仍适合常见终端与
// 聊天窗口。所有输出路径都按 textwidth.Width 计算，中文、英文和 ANSI 颜色
// 序列都不会把版面撑开。
const textWidth = 110

// barWidth 是柱状图的格数。
const barWidth = 24

// TextOptions 控制纯文本报告的呈现。
type TextOptions struct {
	// Color 是颜色能力档位。写文件默认 LevelNone。
	Color termcolor.Level
	// Score 是可选的综合评分，为 nil 时不渲染评分区。
	Score *score.Report
}

// Text 渲染纯文本报告。
func Text(data model.Report, options TextOptions) string {
	renderer := &textRenderer{
		palette: termcolor.Palette{Level: options.Color},
		score:   options.Score,
	}
	return renderer.render(data)
}

type textRenderer struct {
	out     strings.Builder
	palette termcolor.Palette
	score   *score.Report
	section int
}

func (r *textRenderer) render(data model.Report) string {
	r.header(data)
	r.overview(data)
	for _, result := range data.Results {
		r.result(result)
	}
	if r.score != nil {
		r.scoreSection()
	}
	r.footer(data)
	return r.out.String()
}

func (r *textRenderer) line(format string, values ...any) {
	if len(values) == 0 {
		r.out.WriteString(format)
	} else {
		fmt.Fprintf(&r.out, format, values...)
	}
	r.out.WriteByte('\n')
}

func (r *textRenderer) blank() { r.out.WriteByte('\n') }

// header 是报告顶部的标题块。
func (r *textRenderer) header(data model.Report) {
	rule := strings.Repeat("═", textWidth)
	r.line(r.palette.Bold(rule))
	r.line(r.palette.Bold(textwidth.Center(i18n.T("report.title"), textWidth)))
	r.blank()
	meta := []string{
		i18n.T("report.startedAt") + " " + data.Run.StartedAt.Format("2006-01-02 15:04:05 MST"),
		i18n.T("report.version") + " " + fallbackReport(data.Tool.Version, "—"),
		i18n.T("report.profile") + " " + fallbackReport(data.Run.Profile, "—"),
	}
	r.centered(strings.Join(meta, "    "))

	exposure := fallbackReport(data.Run.Exposure, "local")
	second := []string{
		i18n.T("report.exposure") + " " + exposure,
		i18n.T("report.privacy") + " " + map[bool]string{true: i18n.T("report.redacted"), false: i18n.T("report.revealed")}[data.Run.Redacted],
	}
	if data.Run.IPVersion != "" {
		second = append(second, i18n.T("report.ipVersion")+" "+data.Run.IPVersion)
	}
	if data.Run.DurationMS > 0 {
		second = append(second, i18n.T("report.totalDuration")+" "+formatDurationMS(data.Run.DurationMS))
	}
	r.centered(strings.Join(second, "    "))
	r.line(r.palette.Bold(rule))
	r.blank()
}

// centered 按版面宽度折行后居中，避免长的外联级别或自定义版本把标题块撑宽。
func (r *textRenderer) centered(value string) {
	for _, line := range wrapText(value, textWidth-2) {
		r.line(textwidth.Center(line, textWidth))
	}
}

// overview 是模块状态总览。
func (r *textRenderer) overview(data model.Report) {
	// 概览是报告的前言，不占用正文的章节编号。正文的编号只表示
	// 实际输出的结果与评分，用户选了几个模块就从一数到几。
	r.prefaceTitle(i18n.T("report.overview"))
	r.moduleNavigation(data)
	if data.Summary.Headline != "" {
		r.indented(statusIcon(data.Summary.Status)+" "+data.Summary.Headline, true)
		r.blank()
	}
	rows := make([][]string, 0, len(data.Results))
	for _, result := range data.Results {
		rows = append(rows, []string{
			resultTitle(result),
			localizedMethodology(result.Methodology),
			statusIcon(result.Status) + " " + statusLabel(result.Status),
			result.Summary,
			formatDurationMS(result.DurationMS),
		})
	}
	r.table([]string{
		i18n.T("report.module"), i18n.T("report.scope"),
		i18n.T("report.status"), i18n.T("report.summary"), i18n.T("report.duration"),
	}, rows, map[int]bool{4: true})
	r.blank()
}

var reportModuleIDs = []string{
	"system", "network", "bgp", "cpu", "memory", "disk", "dns", "latency", "speed",
	"ports", "nat", "blacklist", "apps", "cnspeed", "ookla", "media", "route", "backtrace",
}

// moduleNavigation 显示完整模块目录以及本次报告实际选择的模块。Run.Requested
// 为空时从结果回退，兼容旧 JSON 报告。
func (r *textRenderer) moduleNavigation(data model.Report) {
	selected := append([]string(nil), data.Run.Requested...)
	if len(selected) == 0 {
		for _, result := range data.Results {
			selected = append(selected, result.ID)
		}
	}
	r.compactList(i18n.T("report.allModules"), moduleTitles(reportModuleIDs))
	r.compactList(i18n.T("report.selectedModules"), moduleTitles(selected))
	r.blank()
}

func moduleTitles(ids []string) []string {
	titles := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		key := "module." + id + ".title"
		if i18n.Has(i18n.Current(), key) {
			titles = append(titles, i18n.T(key))
		} else {
			titles = append(titles, id)
		}
	}
	return titles
}

// compactList 把长模块目录折成带悬挂缩进的紧凑列表。
func (r *textRenderer) compactList(label string, values []string) {
	prefix := "  " + label + "  "
	available := textWidth - textwidth.Width(prefix)
	if available < 1 {
		available = textWidth - 2
	}
	content := strings.Join(values, " · ")
	lines := wrapText(content, available)
	indent := strings.Repeat(" ", textwidth.Width(prefix))
	for index, line := range lines {
		if index == 0 {
			r.line(prefix + line)
		} else {
			r.line(indent + line)
		}
	}
}

// prefaceTitle 是不参与章节编号的标题块（目前用于模块状态总览）。
func (r *textRenderer) prefaceTitle(title string) {
	r.line(r.palette.Bold(title))
	r.line(r.palette.Dim(strings.Repeat("─", textWidth)))
}

func (r *textRenderer) subsection(title string) {
	r.line("  %s", r.palette.Bold(title))
}

// indented 把模块描述、口径和错误按版面宽度折行，避免长字段把 110 列撑开。
func (r *textRenderer) indented(value string, emphasize ...bool) {
	indent := "  "
	for _, line := range wrapText(value, textWidth-textwidth.Width(indent)) {
		if len(emphasize) > 0 && emphasize[0] {
			r.line(r.palette.Bold(indent + line))
		} else {
			r.line(indent + line)
		}
	}
}

// scoreSection 渲染综合评分。
//
// 覆盖度与基线来源和分数并排呈现：一个不说明基于什么、覆盖了多少的分数，
// 读者无法判断它值多少。
func (r *textRenderer) scoreSection() {
	r.sectionTitle(i18n.T("score.title"), "")
	total := r.score.Total
	ratio := total / score.FullScale
	coverage := fmt.Sprintf(i18n.T("score.coverage"), r.score.Covered, r.score.Possible)

	r.line("  %s  %s   %s",
		textwidth.Pad(i18n.T("score.total"), 12),
		r.palette.WrapRatio(fmt.Sprintf("%7.0f", total), ratio),
		r.palette.Dim(coverage))
	r.blank()

	for _, dimension := range r.score.Dimensions {
		label := textwidth.Pad(i18n.T("score.dimension."+dimension.Key), 12)
		if dimension.Missing {
			reason := i18n.T("score.missing." + dimension.MissingReason)
			r.line("  %s  %s", label, r.palette.Dim(reason))
			continue
		}
		r.line("  %s  %s  %s", label,
			r.palette.Bar(dimension.Ratio, barWidth),
			r.palette.WrapRatio(fmt.Sprintf("%7.0f", dimension.Score), dimension.Ratio))
		// 分项指标缩进一级，读者能看到维度分是怎么来的。
		for _, metric := range dimension.Metrics {
			r.line("      %s  %s  %s",
				textwidth.Pad(textwidth.Truncate(metricLabel(metric), 22), 22),
				r.palette.Dim(textwidth.PadLeft(formatFloat(metric.Value)+" "+metric.Unit, 20)),
				r.palette.Dim(fmt.Sprintf(i18n.T("score.ofBaseline"), metric.Ratio*100)))
		}
		if len(dimension.MissingMetrics) > 0 {
			r.note(fmt.Sprintf(i18n.T("score.missingMetrics"), i18n.T("score.dimension."+dimension.Key), len(dimension.MissingMetrics), strings.Join(dimension.MissingMetrics, ", ")))
		}
	}
	r.blank()
	if !r.score.Complete {
		r.note(i18n.T("score.incompleteWarning"))
	}
	if r.score.BaselineSample <= 1 {
		r.note(i18n.T("score.singleSampleWarning"))
	}
	r.note(fmt.Sprintf(i18n.T("score.baselineLine"),
		baselineSourceLabel(r.score.BaselineSource), r.score.BaselineSample))
	r.note(i18n.T("score.weightingNote"))
	// 档位决定了这个分数在跟谁比，必须说清楚。
	if r.score.TierLabel != "" {
		r.note(fmt.Sprintf(i18n.T("score.tierLine"), r.score.HostVCPU, r.score.TierLabel))
	} else if r.score.HostVCPU > 0 {
		r.note(fmt.Sprintf(i18n.T("score.tierFallbackLine"), r.score.HostVCPU))
	}
	r.blank()
}

// metricLabel 优先用 i18n 文案：measurement 的 label 是探针产出时的语言，
// 换语言重新导出时会与报告其余部分不一致。
func metricLabel(metric score.MetricScore) string {
	if key := "score.metric." + metric.Key; i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	if strings.HasPrefix(metric.Key, "crystal_") {
		return i18n.T("score.metric.crystal") + " · " + strings.TrimPrefix(metric.Key, "crystal_")
	}
	if strings.HasPrefix(metric.Key, "fio_mixed_") {
		return i18n.T("score.metric.fio_mixed") + " · " + strings.TrimPrefix(metric.Key, "fio_mixed_")
	}
	if strings.HasPrefix(metric.Key, "atto_") {
		return i18n.T("score.metric.atto") + " · " + strings.TrimPrefix(metric.Key, "atto_")
	}
	return metric.Label
}

func baselineSourceLabel(source string) string {
	if key := "score.baseline." + source; i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	return source
}

// result 渲染单个模块。
func (r *textRenderer) result(result model.Result) {
	r.sectionTitle(resultTitle(result), localizedMethodology(result.Methodology))
	if result.Description != "" {
		r.indented(result.Description)
	}
	// 状态不能依赖 Summary 是否存在：跳过、空结果和仅有错误的结果也必须
	// 明确显示状态，完整报告不能让读者靠章节标题猜测执行结果。
	status := statusIcon(result.Status) + " " + statusLabel(result.Status)
	if result.Summary != "" {
		status += " · " + result.Summary
	}
	r.indented(status, true)
	if result.Error != "" {
		r.indented(i18n.T("report.errorPrefix")+i18n.T("punct.colon")+result.Error, true)
	}
	if result.Methodology.Kind != "" || result.Methodology.Label != "" ||
		result.Methodology.Engine != "" || result.Methodology.Profile != "" ||
		result.Methodology.ComparisonScope != "" {
		parts := make([]string, 0, 5)
		if result.Methodology.Kind != "" {
			parts = append(parts, result.Methodology.Kind)
		}
		if label := localizedMethodology(result.Methodology); label != "" {
			parts = append(parts, label)
		}
		if result.Methodology.Engine != "" {
			parts = append(parts, result.Methodology.Engine)
		}
		if result.Methodology.Profile != "" {
			parts = append(parts, result.Methodology.Profile)
		}
		if result.Methodology.ComparisonScope != "" {
			parts = append(parts, i18n.T("report.comparability")+i18n.T("punct.colon")+result.Methodology.ComparisonScope)
		}
		r.indented(i18n.T("report.methodologyLabel") + i18n.T("punct.colon") + strings.Join(parts, " · "))
	}
	r.blank()

	if len(result.Measurements) > 0 {
		r.subsection(i18n.T("report.metrics"))
		r.measurements(result.Measurements)
	}
	if len(result.Fields) > 0 {
		r.subsection(i18n.T("report.details"))
		r.fields(result.Fields)
	}
	for _, table := range result.Tables {
		r.resultTable(table)
	}
	for _, block := range result.TextBlocks {
		r.textBlock(block)
	}
	if len(result.Notes) > 0 {
		r.subsection(i18n.T("report.notes"))
	}
	for _, note := range result.Notes {
		if note == "" {
			continue
		}
		r.note(note)
	}
	if len(result.Sources) > 0 {
		r.subsection(i18n.T("report.sources"))
	}
	for _, source := range result.Sources {
		parts := []string{source.Name}
		if source.URL != "" {
			parts = append(parts, source.URL)
		}
		if source.Purpose != "" {
			parts = append(parts, source.Purpose)
		}
		r.indented(strings.Join(parts, " · "))
	}
	r.blank()
}

// measurements 渲染指标，并对同单位的一组画组内相对柱。
//
// 只在同单位、同方向且不止一项时画柱：把 ms 和 MiB/s 放在同一个刻度上比较
// 毫无意义，单独一项也没有"相对"可言。
func (r *textRenderer) measurements(items []model.Measurement) {
	groups := groupComparable(items)
	labelWidth, valueWidth := 0, 0
	for _, item := range items {
		labelWidth = maxInt(labelWidth, textwidth.Width(item.Label))
		valueWidth = maxInt(valueWidth, textwidth.Width(item.Display))
	}
	labelWidth = minInt(labelWidth, 30)
	valueWidth = minInt(valueWidth, 26)
	for _, item := range items {
		label := textwidth.Pad(textwidth.Truncate(item.Label, 30), labelWidth)
		valueLines := wrapText(item.Display, valueWidth)
		if len(valueLines) == 0 {
			valueLines = []string{""}
		}
		bar := ""
		if group, ok := groups[item.Key]; ok {
			bar = "  " + r.palette.BarRelative(comparableValue(item, group), group.max, barWidth)
		}
		rating := ""
		if item.Rating != "" {
			rating = "  " + r.palette.Dim(textwidth.Truncate(item.Rating, 20))
		}
		method := ""
		if item.Method != "" {
			method = "[" + item.Method + "]"
		}
		base := "  " + label + "  " + textwidth.PadLeft(valueLines[0], valueWidth) + bar + rating
		if method != "" && textwidth.Width(base)+2+textwidth.Width(method) <= textWidth {
			base += "  " + r.palette.Dim(method)
			method = ""
		}
		for index, valueLine := range valueLines {
			if index == 0 {
				r.line(base)
			} else {
				r.line("      %s", valueLine)
			}
		}
		if method != "" {
			for _, methodLine := range wrapText(method, textWidth-6) {
				r.line("      %s", r.palette.Dim(methodLine))
			}
		}
	}
	r.blank()
}

// comparableGroup 是一组可以互相比较的指标。
type comparableGroup struct {
	max     float64
	inverse bool
}

// groupComparable 找出可以画组内相对柱的指标。
func groupComparable(items []model.Measurement) map[string]comparableGroup {
	type bucket struct {
		keys    []string
		values  []float64
		inverse bool
	}
	buckets := make(map[string]*bucket)
	for _, item := range items {
		if item.Unit == "" || item.Value <= 0 || item.HigherIsBetter == nil {
			continue
		}
		name := item.Unit + "|" + boolKey(*item.HigherIsBetter)
		entry, ok := buckets[name]
		if !ok {
			entry = &bucket{inverse: !*item.HigherIsBetter}
			buckets[name] = entry
		}
		entry.keys = append(entry.keys, item.Key)
		entry.values = append(entry.values, item.Value)
	}
	out := make(map[string]comparableGroup)
	for _, entry := range buckets {
		if len(entry.keys) < 2 {
			continue
		}
		var maximum float64
		for _, value := range entry.values {
			converted := value
			if entry.inverse {
				converted = 1 / value
			}
			if converted > maximum {
				maximum = converted
			}
		}
		if maximum <= 0 {
			continue
		}
		for _, key := range entry.keys {
			out[key] = comparableGroup{max: maximum, inverse: entry.inverse}
		}
	}
	return out
}

// comparableValue 把"越小越好"的指标翻转，使柱长始终表示"越长越好"。
func comparableValue(item model.Measurement, group comparableGroup) float64 {
	if group.inverse && item.Value > 0 {
		return 1 / item.Value
	}
	return item.Value
}

func boolKey(value bool) string {
	if value {
		return "up"
	}
	return "down"
}

// fields 渲染 label: value 列表。
func (r *textRenderer) fields(items []model.Field) {
	width := 0
	for _, item := range items {
		width = maxInt(width, textwidth.Width(item.Label))
	}
	width = minInt(width, 28)
	for _, item := range items {
		value := item.Value
		prefix := "  " + r.palette.Dim(textwidth.Pad(textwidth.Truncate(item.Label, 28), width)) + "  "
		available := textWidth - textwidth.Width(prefix)
		if available < 1 {
			available = textWidth - 2
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
			displayValue := valueLine
			switch strings.ToLower(strings.TrimSpace(valueLine)) {
			case "available", "true":
				displayValue = r.palette.WrapRatio(valueLine, 1)
			case "unavailable", "false":
				displayValue = r.palette.WrapRatio(valueLine, 0)
			}
			r.line(linePrefix + displayValue)
		}
	}
	r.blank()
}

// resultTable 渲染带边框的表格。
func (r *textRenderer) resultTable(table model.Table) {
	if table.Title != "" {
		r.indented(table.Title, true)
	}
	r.table(table.Columns, tableRowsWithBars(table, r.palette), nil)
	r.blank()
}

func tableRowsWithBars(table model.Table, palette termcolor.Palette) [][]string {
	if len(table.NumericColumns) == 0 || len(table.Rows) == 0 {
		return table.Rows
	}
	maxValues := make(map[int]float64, len(table.NumericColumns))
	directions := make(map[int]bool, len(table.NumericColumns))
	for index, column := range table.NumericColumns {
		higher := true
		if index < len(table.NumericHigherIsBetter) {
			higher = table.NumericHigherIsBetter[index]
		}
		directions[column] = higher
		for _, row := range table.Rows {
			if column >= len(row) {
				continue
			}
			value, ok := numericCellValue(row[column])
			if !ok || value <= 0 {
				continue
			}
			if !higher {
				value = 1 / value
			}
			if value > maxValues[column] {
				maxValues[column] = value
			}
		}
	}
	rows := make([][]string, len(table.Rows))
	for rowIndex, original := range table.Rows {
		rows[rowIndex] = append([]string(nil), original...)
		for _, column := range table.NumericColumns {
			if column >= len(rows[rowIndex]) || maxValues[column] <= 0 {
				continue
			}
			value, ok := numericCellValue(rows[rowIndex][column])
			if !ok || value <= 0 {
				continue
			}
			if !directions[column] {
				value = 1 / value
			}
			rows[rowIndex][column] += " " + palette.BarRelative(value, maxValues[column], 8)
		}
	}
	return rows
}

func numericCellValue(cell string) (float64, bool) {
	fields := strings.Fields(strings.TrimSpace(cell))
	if len(fields) == 0 || fields[0] == "—" || fields[0] == "-" {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// table 渲染一张对齐的表格。
//
// 不画竖线边框：中英混排下竖线对齐一旦错位就特别刺眼，而列间距足够分隔数据。
// 只在表头下画一条横线。
func (r *textRenderer) table(columns []string, rows [][]string, rightAlign map[int]bool) {
	if len(columns) == 0 {
		return
	}
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = textwidth.Width(column)
	}
	for _, row := range rows {
		for index, cell := range row {
			if index < len(widths) {
				widths[index] = maxInt(widths[index], textwidth.Width(cell))
			}
		}
	}
	// 总宽超出版面时按比例压缩最宽的列，而不是让表格折行。
	shrinkColumns(widths, textWidth-2-2*(len(columns)-1))

	var head strings.Builder
	head.WriteString("  ")
	for index, column := range columns {
		if index > 0 {
			head.WriteString("  ")
		}
		head.WriteString(textwidth.Pad(textwidth.Truncate(column, widths[index]), widths[index]))
	}
	r.line(r.palette.Bold(strings.TrimRight(head.String(), " ")))

	total := 2
	for index, width := range widths {
		total += width
		if index > 0 {
			total += 2
		}
	}
	r.line(r.palette.Dim("  " + strings.Repeat("─", minInt(total-2, textWidth-2))))

	for _, row := range rows {
		var line strings.Builder
		line.WriteString("  ")
		for index := range columns {
			cell := ""
			if index < len(row) {
				cell = row[index]
			}
			if index > 0 {
				line.WriteString("  ")
			}
			cell = textwidth.Truncate(cell, widths[index])
			if rightAlign[index] {
				line.WriteString(textwidth.PadLeft(cell, widths[index]))
			} else {
				line.WriteString(textwidth.Pad(cell, widths[index]))
			}
		}
		r.line(strings.TrimRight(line.String(), " "))
	}
}

// shrinkColumns 在总宽超限时从最宽的列开始收缩。
func shrinkColumns(widths []int, budget int) {
	if budget <= 0 {
		return
	}
	total := 0
	for _, width := range widths {
		total += width
	}
	for total > budget {
		widest, index := 0, -1
		for position, width := range widths {
			if width > widest {
				widest, index = width, position
			}
		}
		// 收不动了就放手：强行压到 8 列以下会让每个单元格都只剩省略号。
		if index < 0 || widths[index] <= 8 {
			return
		}
		widths[index]--
		total--
	}
}

// textBlock 渲染原文块，逐行缩进以免与正文混淆。
func (r *textRenderer) textBlock(block model.TextBlock) {
	if block.Title != "" {
		r.indented(block.Title, true)
	}
	for _, line := range strings.Split(strings.TrimRight(block.Content, "\n"), "\n") {
		for _, wrapped := range wrapText(line, textWidth-6) {
			r.line("    %s", wrapped)
		}
	}
	r.blank()
}

func (r *textRenderer) note(text string) {
	// 说明文字按版面宽度折行，超长的一行在窄终端里会被硬折得难读。
	for _, line := range wrapText(text, textWidth-6) {
		r.line("  %s %s", r.palette.Dim("·"), r.palette.Dim(line))
	}
}

func (r *textRenderer) sectionTitle(title, scope string) {
	r.section++
	heading := fmt.Sprintf("%s、%s", chineseNumeral(r.section), title)
	if i18n.Current() == i18n.LangEN {
		heading = fmt.Sprintf("%d. %s", r.section, title)
	}
	if scope != "" {
		heading += "  [" + scope + "]"
	}
	for _, line := range wrapText(heading, textWidth) {
		r.line(r.palette.Bold(line))
	}
	r.line(r.palette.Dim(strings.Repeat("─", textWidth)))
}

func (r *textRenderer) footer(data model.Report) {
	r.line(r.palette.Dim(strings.Repeat("═", textWidth)))
	for _, notice := range data.Notices {
		r.note(notice)
	}
	r.indented(i18n.T("report.generator") + " " + data.Tool.Name + " " + data.Tool.Version)
}

// chineseNumeral 把章节号转成中文数字。
func chineseNumeral(value int) string {
	digits := []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	switch {
	case value <= 0:
		return "零"
	case value < 10:
		return digits[value]
	case value < 20:
		if value == 10 {
			return "十"
		}
		return "十" + digits[value-10]
	case value < 100:
		tens := value / 10
		rest := value % 10
		out := digits[tens] + "十"
		if rest > 0 {
			out += digits[rest]
		}
		return out
	default:
		return fmt.Sprintf("%d", value)
	}
}

// wrapText 按显示宽度折行，尽量在空格处断开。
func wrapText(text string, width int) []string {
	if width <= 0 || textwidth.Width(text) <= width {
		return []string{text}
	}
	var lines []string
	var current strings.Builder
	used := 0
	lastSpace := -1
	flush := func() {
		if current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
			used, lastSpace = 0, -1
		}
	}
	for _, character := range text {
		size := textwidth.RuneWidth(character)
		if used+size > width {
			// 有空格可断就从空格断，否则硬断——中文没有词间空格，硬断是正常的。
			if lastSpace > 0 {
				text := current.String()
				lines = append(lines, strings.TrimRight(text[:lastSpace], " "))
				rest := strings.TrimLeft(text[lastSpace:], " ")
				current.Reset()
				current.WriteString(rest)
				used = textwidth.Width(rest)
				lastSpace = -1
			} else {
				flush()
			}
		}
		if character == ' ' {
			lastSpace = current.Len()
		}
		current.WriteRune(character)
		used += size
	}
	flush()
	if len(lines) == 0 {
		return []string{text}
	}
	return lines
}

func formatFloat(value float64) string {
	switch {
	case value >= 1000:
		return fmt.Sprintf("%.0f", value)
	case value >= 10:
		return fmt.Sprintf("%.1f", value)
	default:
		return fmt.Sprintf("%.2f", value)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sortedKeys 用于稳定遍历，避免 map 顺序让输出在两次运行间抖动。
func sortedKeys(values map[string]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
