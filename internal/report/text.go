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
	"unicode"

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
	out          strings.Builder
	palette      termcolor.Palette
	score        *score.Report
	section      int
	subsectionNo int
	version      string
	headline     string
}

func (r *textRenderer) render(data model.Report) string {
	r.version = data.Tool.Version
	r.headline = data.Summary.Headline
	r.moduleNavigation(data)
	r.header(data)
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
	r.line(r.palette.Dim(strings.Repeat("#", textWidth)))
	r.centeredStyled(i18n.T("report.title"), r.palette.AccentBold)
	r.centeredStyled("bash <(curl -sL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh)", r.palette.Info)
	r.centeredStyled("https://github.com/CST-Cat/ecs", r.palette.Info)
	reportTime := data.Run.CompletedAt
	if reportTime.IsZero() {
		reportTime = data.Run.StartedAt
	}
	r.centeredStyled(metaLabel("报告时间", reportTime.Format("2006-01-02 15:04:05 MST"), "Report time", reportTime.Format("2006-01-02 15:04:05 MST")), r.palette.Dim)
	r.centeredStyled(metaLabel("脚本版本", fallbackReport(data.Tool.Version, "—"), "Script version", fallbackReport(data.Tool.Version, "—")), r.palette.Info)
	r.centeredStyled(metaLabel("本次配置", fallbackReport(data.Run.Profile, "—"), "Profile", fallbackReport(data.Run.Profile, "—")), r.palette.Info)
	if data.Summary.Headline != "" {
		r.centeredStyled(metaLabel("报告状态", data.Summary.Headline, "Report status", data.Summary.Headline), r.statusStyle(data.Summary.Status))
	}
	exposure := fallbackReport(data.Run.Exposure, "local")
	second := []string{i18n.T("report.exposure") + " " + exposure, i18n.T("report.privacy") + " " + map[bool]string{true: i18n.T("report.redacted"), false: i18n.T("report.revealed")}[data.Run.Redacted]}
	if data.Run.IPVersion != "" {
		second = append(second, i18n.T("report.ipVersion")+" "+data.Run.IPVersion)
	}
	if data.Run.DurationMS > 0 {
		second = append(second, i18n.T("report.totalDuration")+" "+formatDurationMS(data.Run.DurationMS))
	}
	r.centeredStyled(strings.Join(second, "    "), r.palette.Dim)
	r.line(r.palette.Dim(strings.Repeat("#", textWidth)))
	r.blank()
}

func metaLabel(zh, value, en, englishValue string) string {
	if i18n.Current() == i18n.LangEN {
		return en + ": " + englishValue
	}
	return zh + "：" + value
}

// centered 按版面宽度折行后居中，避免长的外联级别或自定义版本把标题块撑宽。
func (r *textRenderer) centered(value string) {
	r.centeredStyled(value, nil)
}

func (r *textRenderer) centeredStyled(value string, style func(string) string) {
	for _, line := range wrapText(value, textWidth-2) {
		if style != nil {
			line = style(line)
		}
		r.line(textwidth.Center(line, textWidth))
	}
}

// overview 是模块状态总览。
func (r *textRenderer) overview(data model.Report) {
	// 概览是报告的前言，不占用正文的章节编号。正文的编号只表示
	// 实际输出的结果与评分，用户选了几个模块就从一数到几。
	r.prefaceTitle(i18n.T("report.glance"))
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
	allTabs := []string{}
	if i18n.Current() == i18n.LangEN {
		allTabs = []string{"Basic info", "Hardware", "IP quality", "Network quality", "Return path"}
	} else {
		allTabs = []string{"基本信息", "硬件性能", "IP质量", "网络质量", "回程路由"}
	}
	r.compactList(localizedGroup("全部", "All"), allTabs)
	r.compactList(localizedGroup("本次选择", "Selected"), moduleTitles(selected))
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
	styledLabel := r.palette.Label(label)
	prefix := "  " + styledLabel + "  "
	available := textWidth - textwidth.Width(prefix)
	if available < 1 {
		available = textWidth - 2
	}
	content := strings.Join(values, "    ")
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
	r.line(r.palette.AccentBold(title))
	r.line(r.palette.Dim(strings.Repeat("-", textWidth)))
}

func (r *textRenderer) subsection(title string) {
	r.line("  %s", r.palette.AccentBold(title))
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

func (r *textRenderer) indentedStyled(value string, style func(string) string) {
	indent := "  "
	for _, line := range wrapText(value, textWidth-textwidth.Width(indent)) {
		styled := indent + line
		if style != nil {
			styled = style(styled)
		}
		r.line(styled)
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
		r.matrixScoreSummary(dimension)
		// 分项指标缩进一级，读者能看到维度分是怎么来的。
		for _, metric := range dimension.Metrics {
			if matrixKindForMeasurement(metric.Key) != "" {
				continue
			}
			r.line("      %s  %s  %s",
				textwidth.Pad(textwidth.Truncate(metricLabel(metric), 22), 22),
				r.palette.Dim(textwidth.PadLeft(formatFloat(metric.Value)+" "+metric.Unit, 20)),
				r.palette.Dim(fmt.Sprintf(i18n.T("score.ofBaseline"), metric.Ratio*100)))
		}
		if len(dimension.MissingMetrics) > 0 {
			r.indented(fmt.Sprintf(i18n.T("score.missingMetrics"), i18n.T("score.dimension."+dimension.Key), len(dimension.MissingMetrics), compactMissingMetrics(dimension)))
		}
	}
	r.blank()
	if !r.score.Complete {
		r.indented(localizedGroup("评分状态：未覆盖全部维度", "Score status: not all dimensions ran"))
	}
	if r.score.BaselineSample > 0 {
		r.indented(localizedGroup(
			fmt.Sprintf("评分基线：%s（样本 %d 台）", baselineSourceLabel(r.score.BaselineSource), r.score.BaselineSample),
			fmt.Sprintf("Scoring baseline: %s (%d sample hosts)", baselineSourceLabel(r.score.BaselineSource), r.score.BaselineSample),
		))
	}
	r.blank()
}

// matrixScoreSummary keeps the score section readable when a disk result has
// dozens of Crystal/ATTO cells.  The score still uses every cell; only the
// textual explanation is grouped by the same subgroup boundaries used by the
// calculation.
func (r *textRenderer) matrixScoreSummary(dimension score.DimensionScore) {
	counts := matrixScoreCounts(dimension)
	if len(counts) == 0 {
		return
	}
	parts := make([]string, 0, len(counts))
	for _, kind := range []diskMatrixKind{matrixCrystal, matrixMixed, matrixATTO} {
		if count := counts[kind]; count > 0 {
			itemCount := fmt.Sprintf("%d", count)
			if i18n.Current() == i18n.LangZH {
				itemCount += " 项"
			} else {
				itemCount += " items"
			}
			parts = append(parts, matrixKindLabel(kind)+" "+itemCount)
		}
	}
	if len(parts) > 0 {
		separator := " · "
		r.line("      %s", r.palette.Dim(strings.Join(parts, separator)))
	}
}

func matrixScoreCounts(dimension score.DimensionScore) map[diskMatrixKind]int {
	counts := make(map[diskMatrixKind]int)
	for _, group := range dimension.Groups {
		if kind := matrixKindForGroup(group.Key); kind != "" && group.MetricCount > 0 {
			counts[kind] = group.MetricCount
		}
	}
	// Old score JSON may not carry Groups.  Recover a useful count from the
	// metric list in that case, while preferring the calculation's explicit
	// subgroup count when it is present.
	for _, metric := range dimension.Metrics {
		if kind := matrixKindForMeasurement(metric.Key); kind != "" {
			if _, ok := counts[kind]; !ok {
				counts[kind]++
			}
		}
	}
	return counts
}

func compactMissingMetrics(dimension score.DimensionScore) string {
	nonMatrix := make([]string, 0, len(dimension.MissingMetrics))
	counts := make(map[diskMatrixKind]int)
	for _, key := range dimension.MissingMetrics {
		if kind := matrixKindForMeasurement(key); kind != "" {
			counts[kind]++
			continue
		}
		nonMatrix = append(nonMatrix, key)
	}
	for _, kind := range []diskMatrixKind{matrixCrystal, matrixMixed, matrixATTO} {
		if count := counts[kind]; count > 0 {
			label := matrixKindLabel(kind)
			if i18n.Current() == i18n.LangZH {
				label += fmt.Sprintf(" %d 项", count)
			} else {
				label += fmt.Sprintf(" (%d)", count)
			}
			nonMatrix = append(nonMatrix, label)
		}
	}
	if len(nonMatrix) == 0 {
		return "—"
	}
	return strings.Join(nonMatrix, ", ")
}

// statusStyle colors a complete status row as one unit.  Unknown values are
// intentionally dim rather than guessed as success or failure.
func (r *textRenderer) statusStyle(status model.Status) func(string) string {
	switch status {
	case model.StatusOK:
		return r.palette.SuccessBold
	case model.StatusWarning:
		return r.palette.WarningBold
	case model.StatusError:
		return r.palette.ErrorBold
	case model.StatusSkipped:
		return r.palette.Dim
	default:
		return r.palette.Dim
	}
}

// semanticValue applies a color only to recognizable status words.  Numeric
// values and bars remain untouched so a dense report does not become a
// rainbow of independently colored cells.
func (r *textRenderer) semanticValue(value string) string {
	if strings.Contains(value, "\x1b") {
		return value
	}
	if semanticDim(value) {
		return r.palette.Dim(value)
	}
	tone, ok := semanticTone(value)
	if !ok {
		return value
	}
	return r.palette.Tone(value, tone)
}

func semanticDim(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, token := range []string{
		"跳过", "未知", "未测", "未测试", "未覆盖",
		"skipped", "unknown", "untested", "not tested", "not run", "not covered", "n/a",
	} {
		if containsSemanticToken(lower, token) {
			return true
		}
	}
	return false
}

func semanticTone(value string) (termcolor.Tone, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return termcolor.ToneLabel, false
	}
	// Check negative states first: "不可用" also contains the success word
	// "可用", and "not available" contains "available".
	for _, token := range []string{
		"错误", "失败", "不可用", "高风险", "阻止", "拒绝", "异常",
		"fatal", "error", "failed", "failure", "unavailable", "not available", "not enabled", "not ready", "not ok", "unhealthy", "high risk", "blocked", "denied", "critical",
	} {
		if containsSemanticToken(lower, token) {
			return termcolor.ToneError, true
		}
	}
	for _, token := range []string{
		"警告", "部分", "中风险", "需留意", "注意", "待定",
		"warning", "partial", "partially", "medium risk", "attention", "degraded", "caution",
	} {
		if containsSemanticToken(lower, token) {
			return termcolor.ToneWarning, true
		}
	}
	for _, token := range []string{
		"成功", "完成", "通过", "正常", "可用", "已启用", "解锁", "低风险",
		"ok", "success", "completed", "complete", "passed", "healthy", "available", "enabled", "unlocked", "low risk", "ready", "true",
	} {
		if containsSemanticToken(lower, token) {
			return termcolor.ToneSuccess, true
		}
	}
	return termcolor.ToneLabel, false
}

func containsSemanticToken(value, token string) bool {
	if strings.IndexFunc(token, func(r rune) bool { return r > unicode.MaxASCII }) >= 0 {
		return strings.Contains(value, token)
	}
	if strings.ContainsAny(token, " -_/") {
		return strings.Contains(value, token)
	}
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if field == token {
			return true
		}
	}
	return false
}

type diskMatrixKind string

const (
	matrixCrystal diskMatrixKind = "crystal"
	matrixMixed   diskMatrixKind = "mixed"
	matrixATTO    diskMatrixKind = "atto"
)

func matrixKindLabel(kind diskMatrixKind) string {
	switch kind {
	case matrixCrystal:
		return i18n.T("score.metric.crystal")
	case matrixMixed:
		return i18n.T("score.metric.fio_mixed")
	case matrixATTO:
		return i18n.T("score.metric.atto")
	default:
		return string(kind)
	}
}

func matrixKindForGroup(key string) diskMatrixKind {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case string(matrixCrystal):
		return matrixCrystal
	case string(matrixMixed), "fio_mixed":
		return matrixMixed
	case string(matrixATTO):
		return matrixATTO
	default:
		return matrixKindForMeasurement(key)
	}
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
	r.section++
	r.subsectionNo = 0
	r.moduleBanner(result)
	if result.Status != model.StatusOK && result.Description != "" {
		r.indented(result.Description)
	}
	// 状态不能依赖 Summary 是否存在：跳过、空结果和仅有错误的结果也必须
	// 明确显示状态，完整报告不能让读者靠章节标题猜测执行结果。
	status := statusIcon(result.Status) + " " + statusLabel(result.Status)
	if result.Summary != "" && strings.TrimSpace(result.Summary) != strings.TrimSpace(r.headline) {
		status += " · " + result.Summary
	}
	if result.Status != model.StatusOK || result.Error != "" {
		r.indentedStyled(status, r.statusStyle(result.Status))
	}
	if result.Error != "" {
		r.indentedStyled(i18n.T("report.errorPrefix")+i18n.T("punct.colon")+textwidth.Truncate(result.Error, textWidth-10), r.palette.ErrorBold)
	}
	for _, group := range textGroups(result) {
		r.renderGroup(group)
	}
	r.blank()
}

func (r *textRenderer) moduleBanner(result model.Result) {
	r.line(r.palette.Dim(strings.Repeat("*", textWidth)))
	r.centeredStyled(resultTitle(result), r.palette.AccentBold)
	source := "https://github.com/CST-Cat/ecs"
	for _, candidate := range result.Sources {
		if strings.TrimSpace(candidate.URL) != "" {
			source = candidate.URL
			break
		}
	}
	r.centeredStyled(source, r.palette.Info)
	command := "bash <(curl -sL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh)"
	if result.ID != "" {
		command += " --only " + result.ID
	}
	r.centeredStyled(command, r.palette.Info)
	when := result.StartedAt
	version := fallbackReport(r.version, "ecs")
	if when.IsZero() {
		r.centeredStyled(version, r.palette.Dim)
	} else {
		timestamp := when.Format("2006-01-02 15:04:05 MST")
		r.centeredStyled(timestamp+" · "+version, r.palette.Dim)
	}
	r.line(r.palette.Dim(strings.Repeat("-", textWidth)))
}

type textGroup struct {
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
	r.line(r.palette.Dim("  " + strings.Repeat("-", textWidth-2)))
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
	seen := make(map[string]bool, len(items)*2)
	out := make([]model.Field, 0, len(items))
	for _, item := range items {
		keys := []string{strings.ToLower(strings.TrimSpace(item.Key)), strings.ToLower(strings.TrimSpace(item.Label))}
		duplicate := false
		for _, key := range keys {
			if key != "" && seen[key] {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		for _, key := range keys {
			if key != "" {
				seen[key] = true
			}
		}
		out = append(out, item)
	}
	return out
}

func visibleFields(items []model.Field) []model.Field {
	out := make([]model.Field, 0, len(items))
	for _, item := range items {
		if isImplementationText(item.Key + " " + item.Label) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func isImplementationText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, token := range []string{
		"参数模板", "命令参数", "mbw 参数", "命令行",
		"command", "commandline", "command_line", "cmd", "args", "argument", "arguments",
		"parameter", "parameters", "template", "cli",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func dedupeMeasurements(fields []model.Field, items []model.Measurement) []model.Measurement {
	seen := make(map[string]bool, len(fields)+len(items))
	for _, field := range fields {
		for _, key := range []string{field.Key, field.Label} {
			key = strings.ToLower(strings.TrimSpace(key))
			if key != "" {
				seen[key] = true
			}
		}
	}
	out := make([]model.Measurement, 0, len(items))
	for _, item := range items {
		keys := []string{strings.ToLower(strings.TrimSpace(item.Key)), strings.ToLower(strings.TrimSpace(item.Label))}
		duplicate := false
		for _, key := range keys {
			if key != "" && seen[key] {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		for _, key := range keys {
			if key != "" {
				seen[key] = true
			}
		}
		out = append(out, item)
	}
	return out
}

func textGroups(result model.Result) []textGroup {
	groups := make([]textGroup, 0, 4)
	indexes := make(map[string]int)
	add := func(title string) *textGroup {
		if index, ok := indexes[title]; ok {
			return &groups[index]
		}
		groups = append(groups, textGroup{title: title})
		index := len(groups) - 1
		indexes[title] = index
		return &groups[index]
	}
	for _, field := range result.Fields {
		group := add(fieldGroupTitle(result.ID, field.Key, result.Title))
		group.fields = append(group.fields, field)
	}
	for _, measurement := range result.Measurements {
		group := add(measurementGroupTitle(result.ID, measurement.Key, measurement.Label, result.Title))
		group.measurements = append(group.measurements, measurement)
	}
	for _, table := range result.Tables {
		group := add(tableGroupTitle(result.ID, table.Title, result.Title))
		group.tables = append(group.tables, table)
	}
	if len(groups) == 0 {
		groups = append(groups, textGroup{title: fallbackGroupTitle(result.ID)})
	}
	groupOrder := func(title string) int {
		orders := map[string][]string{
			"system":  {localizedGroup("操作系统与硬件", "OS/hardware"), localizedGroup("磁盘与运行状态", "Storage/runtime"), localizedGroup("内核网络", "Kernel networking")},
			"network": {localizedGroup("出口概览", "Egress overview"), localizedGroup("IP 信息", "IP information"), localizedGroup("风险矩阵", "Risk matrix")},
		}
		for index, name := range orders[result.ID] {
			if name == title {
				return index
			}
		}
		return len(orders[result.ID]) + 1
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groupOrder(groups[i].title) < groupOrder(groups[j].title)
	})
	return groups
}

func fallbackGroupTitle(id string) string {
	if i18n.Current() == i18n.LangEN {
		return "Module details"
	}
	return "模块详情"
}

func fieldGroupTitle(id, key, resultTitle string) string {
	lower := strings.ToLower(key)
	if id == "system" {
		if strings.HasPrefix(lower, "tcp") || strings.HasPrefix(lower, "net") || strings.Contains(lower, "ipv6") || strings.Contains(lower, "forward") || strings.Contains(lower, "syn") || strings.Contains(lower, "mtu") || strings.Contains(lower, "queue") || strings.Contains(lower, "conntrack") {
			return localizedGroup("内核网络", "Kernel networking")
		}
		if strings.HasPrefix(lower, "disk") || strings.HasPrefix(lower, "swap") || strings.HasPrefix(lower, "load") || strings.Contains(lower, "uptime") || strings.Contains(lower, "temperature") || strings.Contains(lower, "smart") || strings.HasPrefix(lower, "block") {
			return localizedGroup("磁盘与运行状态", "Storage/runtime")
		}
		return localizedGroup("操作系统与硬件", "OS/hardware")
	}
	if id == "network" {
		if strings.Contains(lower, "risk") || strings.Contains(lower, "fraud") || strings.Contains(lower, "proxy") || strings.Contains(lower, "vpn") || strings.Contains(lower, "tor") {
			return localizedGroup("风险矩阵", "Risk matrix")
		}
		return localizedGroup("IP 信息", "IP information")
	}
	return defaultResultGroup(id, resultTitle)
}

func measurementGroupTitle(id, key, label, resultTitle string) string {
	return fieldGroupTitle(id, key+" "+label, resultTitle)
}

func tableGroupTitle(id, title, resultTitle string) string {
	lower := strings.ToLower(title)
	if id == "system" {
		return localizedGroup("内核网络", "Kernel networking")
	}
	if id == "network" {
		switch {
		case strings.Contains(lower, "风险"), strings.Contains(lower, "risk"):
			return localizedGroup("风险矩阵", "Risk matrix")
		case strings.Contains(lower, "出口"), strings.Contains(lower, "overview"):
			return localizedGroup("出口概览", "Egress overview")
		default:
			return localizedGroup("IP 信息", "IP information")
		}
	}
	return defaultResultGroup(id, resultTitle)
}

func defaultResultGroup(id, resultTitle string) string {
	titles := map[string][2]string{
		"cpu": {"CPU 测评", "CPU benchmark"}, "memory": {"内存测评", "Memory benchmark"}, "disk": {"磁盘测评", "Disk benchmark"},
		"dns": {"DNS 结果", "DNS results"}, "latency": {"延迟结果", "Latency results"}, "speed": {"吞吐结果", "Throughput results"},
		"route": {"路由结果", "Route results"}, "backtrace": {"回程结果", "Return path results"},
	}
	if title, ok := titles[id]; ok {
		return localizedGroup(title[0], title[1])
	}
	return fallbackGroupTitle(id)
}

func localizedGroup(zh, en string) string {
	if i18n.Current() == i18n.LangEN {
		return en
	}
	return zh
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
		label := textwidth.Pad(textwidth.Truncate(item.Label, 30), labelWidth) + i18n.T("punct.colon")
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
			rating = "  " + r.semanticValue(textwidth.Truncate(item.Rating, 20))
		}
		base := "  " + r.palette.Label(label) + "  " + r.semanticValue(textwidth.PadLeft(valueLines[0], valueWidth)) + bar + rating
		for index, valueLine := range valueLines {
			if index == 0 {
				r.line(base)
			} else {
				r.line("      %s", r.semanticValue(valueLine))
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
		label := textwidth.Pad(textwidth.Truncate(item.Label, 28), width) + i18n.T("punct.colon")
		prefix := "  " + r.palette.Label(label) + "  "
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
			displayValue := r.semanticValue(valueLine)
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
	table = normalizeMatrixTable(table)
	table = visibleTableColumns(table)
	if table.Title != "" {
		title := table.Title
		if kind := matrixKindForTable(title); kind == matrixCrystal || kind == matrixATTO {
			title += i18n.T("punct.colon")
		}
		r.indentedStyled(title, r.palette.AccentBold)
	}
	r.table(table.Columns, tableRowsWithBars(table, r.palette), nil)
	r.blank()
}

func visibleTableColumns(table model.Table) model.Table {
	if len(table.Columns) == 0 {
		return table
	}
	keep := make([]int, 0, len(table.Columns))
	for index, column := range table.Columns {
		if !isExplanatoryColumn(column) {
			keep = append(keep, index)
		}
	}
	if len(keep) == 0 || len(keep) == len(table.Columns) {
		return table
	}
	columnMap := make(map[int]int, len(keep))
	out := table
	out.Columns = make([]string, 0, len(keep))
	for index, original := range keep {
		columnMap[original] = index
		out.Columns = append(out.Columns, table.Columns[original])
	}
	out.Rows = make([][]string, len(table.Rows))
	for rowIndex, row := range table.Rows {
		filtered := make([]string, 0, len(keep))
		for _, original := range keep {
			if original < len(row) {
				filtered = append(filtered, row[original])
			} else {
				filtered = append(filtered, "")
			}
		}
		out.Rows[rowIndex] = filtered
	}
	out.NumericColumns = nil
	out.NumericHigherIsBetter = nil
	for index, original := range table.NumericColumns {
		if mapped, ok := columnMap[original]; ok {
			out.NumericColumns = append(out.NumericColumns, mapped)
			if index < len(table.NumericHigherIsBetter) {
				out.NumericHigherIsBetter = append(out.NumericHigherIsBetter, table.NumericHigherIsBetter[index])
			}
		}
	}
	out.SensitiveColumns = nil
	for _, original := range table.SensitiveColumns {
		if mapped, ok := columnMap[original]; ok {
			out.SensitiveColumns = append(out.SensitiveColumns, mapped)
		}
	}
	return out
}

func isExplanatoryColumn(column string) bool {
	lower := strings.ToLower(strings.TrimSpace(column))
	for _, token := range []string{
		"为什么值得看", "指标口径", "分段规则", "备注", "说明", "解释",
		"why", "rationale", "definition", "segment", "note", "comment", "description", "guidance",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func normalizeMatrixTable(table model.Table) model.Table {
	kind := matrixKindForTable(table.Title)
	if kind == "" {
		return table
	}
	if kind == matrixCrystal {
		table.Title = "Crystal"
	}
	if kind == matrixATTO {
		table.Title = "ATTO"
	}
	table.Columns = append([]string(nil), table.Columns...)
	columns := []string{}
	switch kind {
	case matrixCrystal, matrixATTO:
		columns = []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"}
	case matrixMixed:
		columns = []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "合计"}
	}
	for index := range table.Columns {
		if index < len(columns) {
			table.Columns[index] = columns[index]
		}
	}
	return table
}

func matrixKindForTable(title string) diskMatrixKind {
	lower := strings.ToLower(strings.TrimSpace(title))
	switch {
	case lower == string(matrixCrystal), strings.HasPrefix(lower, string(matrixCrystal)+" "):
		return matrixCrystal
	case lower == string(matrixATTO), strings.HasPrefix(lower, string(matrixATTO)+" "):
		return matrixATTO
	case strings.Contains(lower, "混合"), strings.Contains(lower, "mixed"):
		return matrixMixed
	default:
		return ""
	}
}

func visibleMeasurements(result model.Result) []model.Measurement {
	tables := make(map[diskMatrixKind]bool)
	for _, table := range result.Tables {
		if kind := matrixKindForTable(table.Title); kind != "" && len(table.Rows) > 0 {
			tables[kind] = true
		}
	}
	items := make([]model.Measurement, 0, len(result.Measurements))
	for _, item := range result.Measurements {
		kind := matrixKindForMeasurement(item.Key)
		if kind == "" {
			items = append(items, item)
			continue
		}
		if tables[kind] {
			continue
		}
		// A legacy JSON report may carry the matrix cells without the newer
		// table.  Keep the value, but replace the internal key-like label with
		// a compact workload label.
		item.Label = matrixMeasurementLabel(kind, item.Key)
		items = append(items, item)
	}
	return items
}

func matrixKindForMeasurement(key string) diskMatrixKind {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.HasPrefix(lower, "crystal_"):
		return matrixCrystal
	case strings.HasPrefix(lower, "atto_"):
		return matrixATTO
	case strings.HasPrefix(lower, "fio_mixed_"):
		return matrixMixed
	default:
		return ""
	}
}

func matrixMeasurementLabel(kind diskMatrixKind, key string) string {
	stem := strings.ToLower(strings.TrimSpace(key))
	switch kind {
	case matrixCrystal:
		stem = strings.TrimPrefix(stem, "crystal_")
	case matrixATTO:
		stem = strings.TrimPrefix(stem, "atto_")
	case matrixMixed:
		stem = strings.TrimPrefix(stem, "fio_mixed_")
	}
	direction, metric := "", ""
	for _, suffix := range []struct {
		name      string
		direction string
		metric    string
	}{
		{name: "_read_mib_s", direction: "read", metric: "throughput"},
		{name: "_write_mib_s", direction: "write", metric: "throughput"},
		{name: "_read_iops", direction: "read", metric: "IOPS"},
		{name: "_write_iops", direction: "write", metric: "IOPS"},
	} {
		if strings.HasSuffix(stem, suffix.name) {
			stem = strings.TrimSuffix(stem, suffix.name)
			direction, metric = suffix.direction, suffix.metric
			break
		}
	}
	if stem == "" {
		stem = strings.TrimSpace(key)
	}
	block := strings.ToUpper(strings.ReplaceAll(stem, "_", "/"))
	if kind == matrixCrystal {
		// Crystal's workload key is already in the compact RND4K/Q1 form
		// after replacing the separator.
	} else if kind == matrixMixed {
		block = strings.ReplaceAll(block, "/", " ")
	}
	if i18n.Current() == i18n.LangEN {
		if kind == matrixMixed {
			return "Mixed " + block + " " + direction + " " + strings.ToLower(metric)
		}
		return block + " " + direction + " " + strings.ToLower(metric)
	}
	zhDirection := map[string]string{"read": "读", "write": "写"}[direction]
	zhMetric := metric
	if metric == "throughput" {
		zhMetric = "吞吐"
	} else if metric == "IOPS" {
		zhMetric = " IOPS"
	}
	if kind == matrixMixed {
		return block + " 混合" + zhDirection + zhMetric
	}
	return block + " " + zhDirection + zhMetric
}

func tableRowsWithBars(table model.Table, palette termcolor.Palette) [][]string {
	if len(table.NumericColumns) == 0 || len(table.Rows) == 0 {
		return table.Rows
	}
	maxValues := make(map[int]float64, len(table.NumericColumns))
	directions := make(map[int]bool, len(table.NumericColumns))
	valueWidths := make(map[int]int, len(table.NumericColumns))
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
			valueWidths[column] = maxInt(valueWidths[column], textwidth.Width(row[column]))
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
			// Reserve one stable value field before the bar.  Without this
			// padding, a 1 MiB/s row starts its bar earlier than a 1000 MiB/s
			// row and the visual column drifts with digit count or unit text.
			cell := textwidth.Pad(rows[rowIndex][column], valueWidths[column])
			rows[rowIndex][column] = cell + " " + palette.BarRelative(value, maxValues[column], 8)
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
	r.line(r.palette.LabelBold(strings.TrimRight(head.String(), " ")))

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
				line.WriteString(r.semanticValue(textwidth.PadLeft(cell, widths[index])))
			} else {
				line.WriteString(r.semanticValue(textwidth.Pad(cell, widths[index])))
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
		r.line(r.palette.AccentBold(line))
	}
	r.line(r.palette.Dim(strings.Repeat("-", textWidth)))
}

func (r *textRenderer) footer(data model.Report) {
	r.line(r.palette.Dim(strings.Repeat("#", textWidth)))
	r.indentedStyled(i18n.T("report.generator")+" "+data.Tool.Name+" "+data.Tool.Version, r.palette.Dim)
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
