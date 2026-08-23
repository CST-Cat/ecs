package report

// 纯文本报告。
//
// json 给机器读，markdown 给 GitHub 和编辑器读，html 给浏览器读——都缺一个
// "在终端里直接看完、顺手贴进论坛或聊天窗"的形态。这就是 txt 的位置。
//
// 呈现上借鉴了社区里流传的体检报告排版（分区标题块、中文数字章节、紧凑的
// label:value 列），但柱状图部分是重做的：那些报告里的柱子大多不随数值变化，
// 183ms 与 113ms 画成一样长，等于把装饰当信息。柱子按同一指标组的范围绘制；
// 普通跨度保持线性，跨数量级时改用最小值相对的对数刻度，避免短柱全部挤成一格。
//
// 颜色按终端能力自适应（见 internal/termcolor），并且层次不单独依赖颜色：
// 密度字符在无色环境里照样把高低分出来。默认写进文件时不带转义序列，
// 需要彩色文件时用 --color always。

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

// textWidth 是文件型 txt 报告的默认版面宽度。
//
// 110 列给三网回程、风险矩阵等宽表格留出可读空间，同时仍适合常见终端与
// 聊天窗口。所有输出路径都按 textwidth.Width 计算，中文、英文和 ANSI 颜色
// 序列都不会把版面撑开。
const textWidth = 110

// barWidth 是柱状图的格数。
const barWidth = 24

const (
	minimumTextWidth = 20
	maximumTextWidth = 160
)

// TextOptions 控制纯文本报告的呈现。
type TextOptions struct {
	// Color 是颜色能力档位。写文件默认 LevelNone。
	Color termcolor.Level
	// Score 是可选的综合评分，为 nil 时不渲染评分区。
	Score *score.Report
	// Compact 只用于交互式终端摘要。文件型 txt 默认保留方法学、原始
	// TextBlocks、说明和来源，避免四种报告格式之间发生信息丢失。
	Compact bool
	// Width 是可见终端列数。0 保留文件报告的 110 列默认值；
	// 交互式终端应传入实际宽度，让表格、折行和柱图一起自适应。
	Width int
}

// Text 渲染纯文本报告。渲染器在各字段边界解析稳定 key，不复制或改写整份报告。
func Text(data model.Report, options TextOptions) string {
	return textReport(data, options)
}

func textReport(data model.Report, options TextOptions) string {
	data = terminalSafeCopy(data)
	options.Score = terminalSafeCopy(options.Score)
	renderer := &textRenderer{
		palette: termcolor.Palette{Level: options.Color},
		score:   options.Score,
		compact: options.Compact,
		width:   normalizeTextWidth(options.Width),
	}
	return renderer.render(data)
}

type textRenderer struct {
	out          strings.Builder
	palette      termcolor.Palette
	score        *score.Report
	compact      bool
	width        int
	section      int
	subsectionNo int
	version      string
	headline     string
}

func normalizeTextWidth(width int) int {
	if width <= 0 {
		return textWidth
	}
	if width < minimumTextWidth {
		return minimumTextWidth
	}
	if width > maximumTextWidth {
		return maximumTextWidth
	}
	return width
}

// adaptiveBarWidth 保留宽终端的信息密度，并在窄终端为标签、
// 数值和评级腾出空间。长度变化不改变比例或颜色语义。
func adaptiveBarWidth(reportWidth, preferred int) int {
	if preferred <= 0 {
		return 0
	}
	limit := preferred
	switch {
	case reportWidth < 48:
		limit = 4
	case reportWidth < 72:
		limit = 8
	case reportWidth < 96:
		limit = 14
	}
	return minInt(preferred, limit)
}

func (r *textRenderer) render(data model.Report) string {
	r.version = data.Tool.Version
	r.headline = reportHeadline(data.Summary)
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

// line 输出一行已经渲染好的文本。签名刻意不带变参：译文和工具输出里的 '%'
// 因此不可能被当成格式动词。需要格式化的调用点用 linef。
func (r *textRenderer) line(value string) {
	// Every specialized layout path budgets against r.width. Keep this final
	// guard for unusually long localized/status values so a resized terminal
	// never receives a physical line wider than the requested viewport.
	if r.width > 0 && textwidth.Width(value) > r.width {
		value = textwidth.Truncate(value, r.width)
	}
	r.out.WriteString(value)
	r.out.WriteByte('\n')
}

func (r *textRenderer) linef(format string, values ...any) {
	r.line(fmt.Sprintf(format, values...))
}

func (r *textRenderer) blank() { r.out.WriteByte('\n') }

// header 是报告顶部的标题块。
func (r *textRenderer) header(data model.Report) {
	r.line(r.palette.Dim(strings.Repeat("#", r.width)))
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
	if headline := reportHeadline(data.Summary); headline != "" {
		r.centeredStyled(metaLabel("报告状态", headline, "Report status", headline), r.statusStyle(data.Summary.Status))
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
	r.line(r.palette.Dim(strings.Repeat("#", r.width)))
	r.blank()
}

func metaLabel(zh, value, en, englishValue string) string {
	if i18n.Current() == i18n.LangEN {
		return en + ": " + englishValue
	}
	return zh + "：" + value
}

func (r *textRenderer) centeredStyled(value string, style func(string) string) {
	for _, line := range wrapText(value, r.width-2) {
		if style != nil {
			line = style(line)
		}
		r.line(textwidth.Center(line, r.width))
	}
}

// moduleNavigation 显示完整模块目录以及本次报告实际选择的模块。Run.Requested
// 为空时从结果字段补出当前模块标题。
func (r *textRenderer) moduleNavigation(data model.Report) {
	selected := append([]string(nil), data.Run.Requested...)
	if len(selected) == 0 {
		for _, result := range data.Results {
			selected = append(selected, result.ID)
		}
	}
	var allTabs []string
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
		if descriptor, ok := config.ModuleDescriptorFor(id); ok && descriptor.TitleKey != "" {
			key = descriptor.TitleKey
		}
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
	available := r.width - textwidth.Width(prefix)
	if available < 1 {
		available = r.width - 2
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
	r.line(r.palette.Dim(strings.Repeat("-", r.width)))
}

func (r *textRenderer) subsection(title string) {
	r.linef("  %s", r.palette.AccentBold(title))
}

// indented 把模块描述、口径和错误按版面宽度折行，避免长字段把 110 列撑开。
func (r *textRenderer) indented(value string, emphasize ...bool) {
	indent := "  "
	for _, line := range wrapText(value, r.width-textwidth.Width(indent)) {
		if len(emphasize) > 0 && emphasize[0] {
			r.line(r.palette.Bold(indent + line))
		} else {
			r.line(indent + line)
		}
	}
}

func (r *textRenderer) indentedStyled(value string, style func(string) string) {
	indent := "  "
	for _, line := range wrapText(value, r.width-textwidth.Width(indent)) {
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

	r.linef("  %s  %s   %s",
		textwidth.Pad(i18n.T("score.total"), 12),
		r.palette.WrapRatio(fmt.Sprintf("%7.0f", total), ratio),
		r.palette.Dim(coverage))
	r.blank()

	for _, dimension := range r.score.Dimensions {
		label := textwidth.Pad(i18n.T("score.dimension."+dimension.Key), 12)
		if dimension.Missing {
			reason := i18n.T("score.missing." + dimension.MissingReason)
			r.linef("  %s  %s", label, r.palette.Dim(reason))
			continue
		}
		r.linef("  %s  %s  %s", label,
			r.palette.Bar(dimension.Ratio, adaptiveBarWidth(r.width, barWidth)),
			r.palette.WrapRatio(fmt.Sprintf("%7.0f", dimension.Score), dimension.Ratio))
		r.matrixScoreSummary(dimension)
		// 分项指标缩进一级，读者能看到维度分是怎么来的。
		for _, metric := range dimension.Metrics {
			if matrixKindForMeasurement(metric.Key) != "" {
				continue
			}
			r.linef("      %s  %s  %s",
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
		r.indented(fmt.Sprintf(i18n.T("score.baselineLine"), baselineSourceLabel(r.score.BaselineSource), r.score.BaselineSample))
	}
	if r.score.RankStatus != "" || r.score.RankSamples > 0 || r.score.BaselineSample > 0 {
		switch r.score.EffectiveRankStatus() {
		case score.RankStatusAvailable:
			r.indented(fmt.Sprintf(i18n.T("score.rank.available"), r.score.TopPercent, r.score.EffectiveRankSamples()))
		case score.RankStatusInsufficient:
			r.indented(fmt.Sprintf(i18n.T("score.rank.insufficient"), r.score.EffectiveRankSamples(), r.score.EffectiveRankMinSamples()))
		default:
			r.indented(i18n.T("score.rank.unavailable"))
		}
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
		r.linef("      %s", r.palette.Dim(strings.Join(parts, separator)))
	}
}

func matrixScoreCounts(dimension score.DimensionScore) map[diskMatrixKind]int {
	counts := make(map[diskMatrixKind]int)
	for _, group := range dimension.Groups {
		if kind := matrixKindForGroup(group.Key); kind != "" && group.MetricCount > 0 {
			counts[kind] = group.MetricCount
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

// semanticValue applies a color only to recognizable status words. Input data
// has already crossed terminalSafeCopy, so any escape here was generated by
// this renderer's palette and must not be nested inside another SGR sequence.
// Numeric
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
		r.indented(displayReportText(result.Description))
	}
	// 状态不能依赖 Summary 是否存在：跳过、空结果和仅有错误的结果也必须
	// 明确显示状态，完整报告不能让读者靠章节标题猜测执行结果。
	status := statusIcon(result.Status) + " " + statusLabel(result.Status)
	if summary := resultSummary(result); summary != "" && strings.TrimSpace(summary) != strings.TrimSpace(r.headline) {
		status += " · " + summary
	}
	if result.Status != model.StatusOK || result.Error != "" {
		r.indentedStyled(status, r.statusStyle(result.Status))
	}
	if result.Error != "" {
		r.indentedStyled(i18n.T("report.errorPrefix")+i18n.T("punct.colon")+textwidth.Truncate(displayReportText(result.Error), maxInt(1, r.width-10)), r.palette.ErrorBold)
	}
	r.resultEvidenceCoverage(result.Evidence)
	r.resultFailures(result.Failures)
	r.renderRetryInterference(result)
	for _, group := range textGroups(result) {
		r.renderGroup(group)
	}
	if !r.compact {
		r.resultEvidence(result)
	}
	r.blank()
}

func (r *textRenderer) resultFailures(failures []model.Failure) {
	if len(failures) == 0 {
		return
	}
	r.subsection(i18n.T("report.failures"))
	rows := make([][]string, 0, len(failures))
	for _, failure := range failures {
		rows = append(rows, []string{
			failureCategoryLabel(failure.Category),
			fallbackReport(failure.Stage, "—"),
			fallbackReport(failure.Target, "—"),
			strconv.Itoa(maxInt(failure.Count, 1)),
			failureRetryableLabel(failure.Retryable),
			fallbackReport(failure.Message, "—"),
		})
	}
	r.table([]string{
		i18n.T("failure.category"), i18n.T("failure.stage"), i18n.T("failure.target"),
		i18n.T("failure.count"), i18n.T("failure.retryable"), i18n.T("failure.message"),
	}, rows, map[int]bool{3: true})
	r.blank()
}

func (r *textRenderer) resultEvidenceCoverage(evidence *model.Evidence) {
	if evidence == nil {
		return
	}
	ratio := evidence.EvidenceRatio()
	label := evidenceText(*evidence)
	line := fmt.Sprintf("%s%s%s", i18n.T("report.evidence"), i18n.T("punct.colon"), label)
	style := r.palette.WarningBold
	if evidence.Expected <= 0 {
		style = r.palette.Dim
	} else if evidence.Valid <= 0 {
		style = r.palette.ErrorBold
	} else if evidence.Valid >= evidence.Expected && evidence.Expected > 0 {
		style = r.palette.SuccessBold
	}
	// Keep the semantic label and the proportional bar as sibling ANSI spans.
	// Nesting a colored bar inside another style would reset the outer color on
	// basic and 256-color terminals after the first filled cell.
	prefix := "  " + style(line)
	barSize := minInt(16, adaptiveBarWidth(r.width, 16))
	if available := r.width - textwidth.Width(prefix) - 2; available < barSize {
		barSize = maxInt(0, available)
	}
	if barSize >= 4 {
		r.linef("%s  %s", prefix, r.palette.Bar(ratio, barSize))
	} else {
		r.indentedStyled(line, style)
	}
}

func evidenceText(evidence model.Evidence) string {
	unit := evidence.Unit
	unitKey := "evidence.unit." + unit
	if i18n.Current() == i18n.LangEN && evidence.Expected != 1 {
		unitKey = "evidence.units." + unit
	}
	if unit != "" && i18n.Has(i18n.Current(), unitKey) {
		unit = i18n.T(unitKey)
	}
	count := fmt.Sprintf("%d/%d", evidence.Valid, evidence.Expected)
	if unit != "" {
		count += " " + unit
	}
	if evidence.Expected <= 0 {
		return count + " · " + i18n.T("evidence.notPlanned")
	}
	state := i18n.T("evidence." + string(evidence.EffectiveGrade()))
	return fmt.Sprintf("%s · %.0f%% · %s", count, evidence.EvidenceRatio()*100, state)
}

// resultEvidence is rendered in file txt output but intentionally omitted from
// the interactive terminal summary.  JSON remains the lossless machine
// artifact; this keeps txt equally useful when a user only has one human-
// readable file to inspect.
func (r *textRenderer) resultEvidence(result model.Result) {
	if result.Description != "" && result.Status == model.StatusOK {
		r.subsection(i18n.T("report.description"))
		r.indented(displayReportText(result.Description))
	}
	methodology := displayMethodology(result.Methodology)
	if methodology.Kind != "" || methodology.Label != "" || methodology.Engine != "" || methodology.Profile != "" || methodology.ComparisonScope != "" {
		r.subsection(i18n.T("report.methodologyLabel"))
		if label := localizedMethodology(methodology); label != "" {
			r.indented(label)
		}
		if methodology.Engine != "" {
			r.indented(metaLabel("引擎", methodology.Engine, "Engine", methodology.Engine))
		}
		if methodology.Profile != "" {
			r.indented(metaLabel("参数/工作负载", methodology.Profile, "Profile/workload", methodology.Profile))
		}
		if methodology.ComparisonScope != "" {
			r.indented(metaLabel(i18n.T("report.comparability"), methodology.ComparisonScope, "Comparable scope", methodology.ComparisonScope))
		}
	}
	if len(result.TextBlocks) > 0 {
		r.subsection(i18n.T("report.rawOutput"))
		for _, block := range result.TextBlocks {
			r.textBlock(block)
		}
	}
	if len(result.Notes) > 0 {
		r.subsection(i18n.T("report.notes"))
		for _, note := range result.Notes {
			r.note(displayReportText(note))
		}
		r.blank()
	}
	if len(result.Sources) > 0 {
		r.subsection(i18n.T("report.sources"))
		bannerSource := -1
		for index, rawSource := range result.Sources {
			source := rawSource
			source.Name = displayReportText(source.Name)
			source.Purpose = displayReportText(source.Purpose)
			if strings.TrimSpace(source.URL) != "" {
				bannerSource = index
				break
			}
		}
		for index, rawSource := range result.Sources {
			source := rawSource
			source.Name = displayReportText(source.Name)
			source.Purpose = displayReportText(source.Purpose)
			if index == bannerSource {
				// The banner already carries this source URL. Keep its name and
				// purpose in the evidence section without printing the URL twice.
				source.URL = ""
			}
			value := source.Name
			if source.URL != "" {
				if value != "" {
					value += " "
				}
				value += source.URL
			}
			if source.Purpose != "" {
				value += i18n.T("punct.colon") + source.Purpose
			}
			r.indented(value)
		}
		r.blank()
	}
}

func (r *textRenderer) moduleBanner(result model.Result) {
	r.line(r.palette.Dim(strings.Repeat("*", r.width)))
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
	r.line(r.palette.Dim(strings.Repeat("-", r.width)))
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
		"参数模板", "命令参数", "命令行",
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
		if strings.HasPrefix(lower, "disk") || strings.HasPrefix(lower, "swap") || strings.HasPrefix(lower, "load") || strings.Contains(lower, "uptime") || strings.HasPrefix(lower, "block") {
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
	lower := strings.ToLower(displayReportText(title))
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
		"cpu": {"CPU 测评", "CPU benchmark"}, "zstd": {"zstd 压缩测评", "zstd benchmark"},
		"npb": {"NPB EP + FT 测评", "NPB EP + FT benchmark"}, "memory": {"内存测评", "Memory benchmark"},
		"crypto": {"密码学测评", "Cryptography benchmark"}, "disk": {"磁盘测评", "Disk benchmark"},
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
	labelLimit := minInt(30, maxInt(6, r.width*3/10))
	valueLimit := minInt(26, maxInt(6, r.width/4))
	labelWidth, valueWidth := 0, 0
	for _, rawItem := range items {
		item := displayMeasurement(rawItem)
		labelWidth = maxInt(labelWidth, textwidth.Width(item.Label))
		valueWidth = maxInt(valueWidth, textwidth.Width(item.Display))
	}
	labelWidth = minInt(labelWidth, labelLimit)
	valueWidth = minInt(valueWidth, valueLimit)
	for _, rawItem := range items {
		item := displayMeasurement(rawItem)
		label := textwidth.Pad(textwidth.Truncate(item.Label, labelLimit), labelWidth) + i18n.T("punct.colon")
		valueLines := wrapText(item.Display, valueWidth)
		if len(valueLines) == 0 {
			valueLines = []string{""}
		}
		base := "  " + r.palette.Label(label) + "  " + r.semanticValue(textwidth.PadLeft(valueLines[0], valueWidth))
		rating := ""
		if item.Rating != "" {
			rating = "  " + r.semanticValue(textwidth.Truncate(item.Rating, 20))
		}
		separateRating := false
		bar := ""
		if group, ok := groups[item.Key]; ok {
			available := r.width - textwidth.Width(base) - 2
			barSize := minInt(adaptiveBarWidth(r.width, barWidth), maxInt(0, available-textwidth.Width(rating)))
			if rating != "" && barSize < 4 {
				separateRating = true
				rating = ""
				barSize = minInt(adaptiveBarWidth(r.width, barWidth), maxInt(0, available))
			}
			if barSize >= 2 {
				bar = "  " + r.palette.BarRelativeRange(comparableValue(item, group), group.min, group.max, barSize)
			}
		} else if textwidth.Width(base)+textwidth.Width(rating) > r.width {
			separateRating = rating != ""
			rating = ""
		}
		base += bar + rating
		for index, valueLine := range valueLines {
			if index == 0 {
				r.line(base)
			} else {
				r.linef("      %s", r.semanticValue(valueLine))
			}
		}
		if separateRating {
			r.indentedStyled(item.Rating, r.semanticValue)
		}
		// Workload identifiers are part of the evidence contract. The compact
		// terminal view hides them, while file txt retains every method so it can
		// be audited against the structured JSON artifact.
		if !r.compact && item.Method != "" {
			prefix := "      " + i18n.T("report.method") + i18n.T("punct.colon")
			available := maxInt(1, r.width-textwidth.Width(prefix))
			lines := wrapText(item.Method, available)
			for index, methodLine := range lines {
				if index == 0 {
					r.linef("%s%s", r.palette.Dim(prefix), r.palette.Dim(methodLine))
				} else {
					r.linef("%s%s", strings.Repeat(" ", textwidth.Width(prefix)), r.palette.Dim(methodLine))
				}
			}
		}
	}
	r.blank()
}

// comparableGroup 是一组可以互相比较的指标。
type comparableGroup struct {
	max     float64
	min     float64
	inverse bool
}

// riskScoreMeasurement identifies the 0–100 risk scores whose magnitude is
// useful to show directly.  A high risk score is worse, but a longer bar still
// communicates a higher observed risk; only latency/utilization-style metrics
// should invert their values to visualize "higher is better".
func riskScoreMeasurement(item model.Measurement) bool {
	if strings.TrimSpace(item.Unit) != "/100" {
		return false
	}
	key := strings.ToLower(item.Key)
	label := strings.ToLower(item.Label)
	return strings.Contains(key, "risk") || strings.Contains(key, "风险") ||
		strings.Contains(label, "risk") || strings.Contains(label, "风险")
}

// comparisonSemantic prevents unrelated measurements that happen to share a
// unit from borrowing one another's scale (for example memory usage and packet
// loss are both percentages, but neither is comparable to the other).
func comparisonSemantic(item model.Measurement) string {
	if riskScoreMeasurement(item) {
		return "risk"
	}
	// The unit alone is insufficient: Crystal, ATTO, mixed and baseline fio
	// throughput all use MiB/s, but their workloads are not one comparison set.
	if semantic := matrixMeasurementSemantic(item.Key); semantic != "" {
		return semantic
	}
	key := strings.ToLower(item.Key)
	lower := strings.ToLower(item.Key + " " + item.Label)
	// Percentile and interval qualifiers are separate metrics even when their
	// units and quality direction match. Keeping them in distinct buckets makes
	// every bar compare like with like across targets instead of, for example,
	// scaling a DNS P50 against a DNS P95 or an iperf interval minimum against
	// the final whole-run throughput.
	switch {
	case strings.Contains(key, "iperf3_") && strings.Contains(key, "_interval_min_"):
		return "iperf-interval-min"
	case strings.Contains(key, "iperf3_") && strings.Contains(key, "_interval_p50_"):
		return "iperf-interval-p50"
	case strings.Contains(key, "iperf3_") && strings.HasSuffix(key, "_mbps"):
		return "iperf-throughput"
	case strings.HasPrefix(key, "dns_resolver_") && strings.Contains(key, "_p50_"):
		return "dns-p50"
	case key == "best_dns_median_ms":
		return "dns-p50"
	case strings.HasPrefix(key, "dns_resolver_") && strings.Contains(key, "_p95_"):
		return "dns-p95"
	case strings.HasPrefix(key, "tcp_target_") && strings.Contains(key, "_p50_"):
		return "tcp-p50"
	case key == "best_tcp_median_ms":
		return "tcp-p50"
	case strings.HasPrefix(key, "tcp_target_") && strings.Contains(key, "_p95_"):
		return "tcp-p95"
	case strings.HasPrefix(key, "route_target_") && strings.HasSuffix(key, "_hop_slots"):
		return "route-hop-slots"
	case strings.HasPrefix(key, "route_target_") && strings.HasSuffix(key, "_visible_hops"):
		return "route-visible-hops"
	case strings.HasPrefix(key, "route_target_") && strings.HasSuffix(key, "_timeout_hops"):
		return "route-timeout-hops"
	}
	switch strings.TrimSpace(item.Unit) {
	case "%":
		switch {
		case strings.Contains(lower, "loss"), strings.Contains(lower, "丢包"):
			return "loss"
		case strings.Contains(lower, "steal"):
			return "cpu-steal"
		case strings.Contains(lower, "usage"), strings.Contains(lower, "使用率"):
			return "usage"
		case strings.Contains(lower, "percentage_used"), strings.Contains(lower, "已用寿命"):
			return "device-life"
		default:
			return "percent"
		}
	case "ms":
		if strings.Contains(lower, "jitter") || strings.Contains(lower, "抖动") {
			return "jitter"
		}
		return "latency"
	case "项":
		switch {
		case strings.Contains(lower, "blacklist"), strings.Contains(lower, "listed"), strings.Contains(lower, "名单"):
			return "blacklist"
		case strings.Contains(lower, "bgp"), strings.Contains(lower, "observed"), strings.Contains(lower, "可观测"):
			return "coverage"
		case strings.Contains(lower, "reachable"), strings.Contains(lower, "可达"):
			return "reachability"
		}
	case "bytes":
		switch {
		case strings.Contains(lower, "memory"), strings.Contains(lower, "内存"):
			return "memory-capacity"
		case strings.Contains(lower, "disk"), strings.Contains(lower, "磁盘"):
			return "disk-capacity"
		case strings.Contains(lower, "swap"), strings.Contains(lower, "交换"):
			return "swap-capacity"
		}
	}
	return ""
}

func matrixMeasurementSemantic(key string) string {
	switch matrixKindForMeasurement(key) {
	case matrixCrystal:
		return "matrix-crystal"
	case matrixATTO:
		return "matrix-atto"
	case matrixMixed:
		return "matrix-mixed"
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.HasPrefix(lower, "fio_mount_"):
		return "disk-mount"
	case strings.HasPrefix(lower, "fio_"):
		return "disk-baseline"
	case strings.HasPrefix(lower, "sysbench_cpu_"):
		return "cpu"
	default:
		return ""
	}
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
		if item.Unit == "" || item.Value < 0 || item.HigherIsBetter == nil ||
			math.IsNaN(item.Value) || math.IsInf(item.Value, 0) {
			continue
		}
		semantic := comparisonSemantic(item)
		direction := boolKey(*item.HigherIsBetter)
		if semantic == "risk" {
			// Keep risk scores in their own magnitude-based bucket instead of
			// mixing them with any other /100 metric that is lower-is-better.
			direction = "risk"
		}
		name := item.Unit + "|" + direction + "|" + semantic
		entry, ok := buckets[name]
		if !ok {
			entry = &bucket{inverse: !*item.HigherIsBetter && semantic != "risk"}
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
		minimum := 0.0
		for _, value := range entry.values {
			converted := value
			if entry.inverse && value > 0 {
				converted = 1 / value
			}
			if converted > maximum {
				maximum = converted
			}
			if converted > 0 && (minimum == 0 || converted < minimum) {
				minimum = converted
			}
		}
		if maximum <= 0 {
			// Keep a real all-zero group visible.  It renders as an empty
			// magnitude bar (or a full quality bar for lower-is-better
			// metrics) instead of looking like missing data.
			maximum = 1
		}
		for _, key := range entry.keys {
			out[key] = comparableGroup{max: maximum, min: minimum, inverse: entry.inverse}
		}
	}
	return out
}

// comparableValue 把"越小越好"的指标翻转，使柱长始终表示"越长越好"。
func comparableValue(item model.Measurement, group comparableGroup) float64 {
	if group.inverse {
		if item.Value <= 0 {
			// Zero latency/loss is the best attainable value; avoid 1/0 while
			// retaining a visible full-quality bar.
			return group.max
		}
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
	labelLimit := minInt(28, maxInt(6, r.width/3))
	width := 0
	for _, rawItem := range items {
		item := displayField(rawItem)
		width = maxInt(width, textwidth.Width(item.Label))
	}
	width = minInt(width, labelLimit)
	for _, rawItem := range items {
		item := displayField(rawItem)
		value := item.Value
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
	table = displayTable(table)
	table = normalizeMatrixTable(table)
	table = visibleTableColumns(table)
	if table.Title != "" {
		title := table.Title
		if kind := matrixKindForTable(title); kind == matrixCrystal || kind == matrixATTO {
			title += i18n.T("punct.colon")
		}
		r.indentedStyled(title, r.palette.AccentBold)
	}
	cellBarWidth := adaptiveBarWidth(r.width, 8)
	if tableNeedsStackedLayout(r.width, len(table.Columns)) {
		// A bar inside every stacked value adds noise and makes ANSI-aware
		// wrapping harder. Preserve the exact numeric text in this layout.
		cellBarWidth = 0
	}
	r.table(table.Columns, tableRowsWithBars(table, r.palette, cellBarWidth), nil)
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
	if table.ColumnKeys != nil {
		out.ColumnKeys = make([]string, 0, len(keep))
	}
	for index, original := range keep {
		columnMap[original] = index
		out.Columns = append(out.Columns, table.Columns[original])
		if table.ColumnKeys != nil {
			if original < len(table.ColumnKeys) {
				out.ColumnKeys = append(out.ColumnKeys, table.ColumnKeys[original])
			} else {
				// Keep the parallel shape even for malformed legacy input. Such
				// an empty key is not a usable identity, but it is safer than
				// shifting the remaining column keys onto the wrong columns.
				out.ColumnKeys = append(out.ColumnKeys, "")
			}
		}
	}
	if table.RowIdentity != "" {
		out.RowIdentity = ""
		for original, key := range table.ColumnKeys {
			if key == table.RowIdentity {
				if _, ok := columnMap[original]; ok {
					out.RowIdentity = key
				}
				break
			}
		}
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
		if kind := matrixKindForTable(displayReportText(table.Title)); kind != "" && len(table.Rows) > 0 {
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

func tableRowsWithBars(table model.Table, palette termcolor.Palette, requestedBarWidth ...int) [][]string {
	if len(table.NumericColumns) == 0 || len(table.Rows) == 0 {
		return table.Rows
	}
	// A table column is not necessarily a metric group.  Crystal/ATTO, for
	// example, put read and write throughput in adjacent columns; calculating a
	// maximum independently for each column makes a 4.63 MiB/s read hit a full
	// bar when that column has no faster read, even though a 1000 MiB/s write in
	// the same table is orders of magnitude faster.  Group columns by metric and
	// unit first, then scale every member against the group's range.  The shared
	// helper keeps compact ranges linear and switches wide ranges to a
	// min-relative logarithmic scale, while the table itself remains the scope
	// so values from different matrices never borrow a scale.
	stats := make(map[tableBarGroup]tableBarStats, len(table.NumericColumns))
	valueWidths := make(map[int]int, len(table.NumericColumns))
	semantics := make(map[int]string, len(table.NumericColumns))
	directions := make(map[int]bool, len(table.NumericColumns))
	for index, column := range table.NumericColumns {
		higher := true
		if index < len(table.NumericHigherIsBetter) {
			higher = table.NumericHigherIsBetter[index]
		}
		if riskNumericColumn(table, column) {
			// Risk magnitude is intentionally drawn directly: a larger
			// 0–100 score gets a longer warning bar even though the quality
			// direction metadata is lower-is-better.
			higher = true
		}
		semantics[column] = tableBarSemantic(table, column)
		directions[column] = higher
		for _, row := range table.Rows {
			if column >= len(row) {
				continue
			}
			value, unit, ok := numericCell(row[column])
			if !ok || value < 0 {
				continue
			}
			group := tableBarGroup{semantic: semantics[column], unit: unit, higher: higher}
			valueWidths[column] = maxInt(valueWidths[column], textwidth.Width(row[column]))
			if value == 0 {
				entry := stats[group]
				entry.zero = true
				stats[group] = entry
			}
			if !higher && value > 0 {
				value = 1 / value
			}
			if value > 0 {
				entry := stats[group]
				if value > entry.max {
					entry.max = value
				}
				if entry.min == 0 || value < entry.min {
					entry.min = value
				}
				stats[group] = entry
			}
		}
	}
	for group, entry := range stats {
		if entry.max <= 0 && entry.zero {
			// A real all-zero column still gets a visible semantic bar: empty
			// for magnitude metrics, full quality for lower-is-better metrics.
			entry.max = 1
		}
		stats[group] = entry
	}
	cellBarWidth := 8
	if len(requestedBarWidth) > 0 {
		cellBarWidth = maxInt(0, requestedBarWidth[0])
	}
	rows := make([][]string, len(table.Rows))
	for rowIndex, original := range table.Rows {
		rows[rowIndex] = append([]string(nil), original...)
		for _, column := range table.NumericColumns {
			if column >= len(rows[rowIndex]) {
				continue
			}
			value, unit, ok := numericCell(rows[rowIndex][column])
			if !ok || value < 0 {
				continue
			}
			higher := directions[column]
			group := tableBarGroup{semantic: semantics[column], unit: unit, higher: higher}
			entry, ok := stats[group]
			if !ok || entry.max <= 0 {
				continue
			}
			if !higher && value == 0 {
				value = entry.max
			} else if !higher {
				value = 1 / value
			}
			// Reserve one stable value field before the bar.  Without this
			// padding, a 1 MiB/s row starts its bar earlier than a 1000 MiB/s
			// row and the visual column drifts with digit count or unit text.
			cell := textwidth.Pad(rows[rowIndex][column], valueWidths[column])
			if cellBarWidth > 0 {
				rows[rowIndex][column] = cell + " " + palette.BarRelativeRange(value, entry.min, entry.max, cellBarWidth)
			}
		}
	}
	return rows
}

// tableBarGroup is the scale identity for a numeric table cell.  Unit is
// taken from the cell rather than inferred solely from the heading, so a
// malformed/mixed-unit table cannot silently compare MiB/s with IOPS.  Higher
// is part of the identity because a lower-is-better latency column uses the
// reciprocal magnitude.
type tableBarGroup struct {
	semantic string
	unit     string
	higher   bool
}

type tableBarStats struct {
	max  float64
	min  float64
	zero bool
}

// tableBarSemantic canonicalizes only the direction decoration shared by
// matrix columns.  More specific metric qualifiers (for example average vs
// P95 latency) stay in the key and therefore do not get a misleading common
// scale.  Direction-only headings such as "读"/"写" intentionally return an
// empty semantic; the cell unit then becomes the safe grouping boundary.
func tableBarSemantic(table model.Table, column int) string {
	if column < 0 || column >= len(table.Columns) {
		return ""
	}
	heading := strings.ToLower(strings.TrimSpace(table.Columns[column]))
	// Direction words are often attached to the metric token (读吞吐,
	// upload bandwidth), so remove them before splitting English-style
	// separators.  The direction is not a metric: read/write and upload/download
	// throughput should share one scale, while qualifiers such as P50/P95 stay.
	for _, direction := range []string{
		"读取", "写入", "上行", "下行", "发送", "接收", "上传", "下载", "读", "写",
	} {
		heading = strings.ReplaceAll(heading, direction, " ")
	}
	for _, separator := range []string{"/", "_", "-", "(", ")", ":", "·"} {
		heading = strings.ReplaceAll(heading, separator, " ")
	}
	fields := strings.Fields(heading)
	kept := fields[:0]
	for _, field := range fields {
		// Do not remove substrings ("thread" contains "read").
		if field == "read" || field == "write" || field == "upload" || field == "download" ||
			field == "send" || field == "receive" || field == "sent" || field == "received" ||
			field == "inbound" || field == "outbound" || field == "tx" || field == "rx" ||
			field == "r" || field == "w" {
			continue
		}
		kept = append(kept, field)
	}
	if len(kept) == 0 {
		return ""
	}
	joined := strings.Join(kept, " ")
	switch {
	case strings.Contains(joined, "iops"):
		return "iops"
	case strings.Contains(joined, "吞吐"), strings.Contains(joined, "throughput"),
		strings.Contains(joined, "bandwidth"), strings.Contains(joined, "带宽"):
		return "throughput"
	case joined == "合计", joined == "total":
		// Mixed/ATTO tables may call the sum simply "合计".  Its unit keeps
		// it separate from any same-table non-throughput column.
		return ""
	default:
		return joined
	}
}

func riskNumericColumn(table model.Table, column int) bool {
	if column < 0 || column >= len(table.Columns) {
		return false
	}
	heading := strings.ToLower(table.Columns[column])
	if !strings.Contains(heading, "risk") && !strings.Contains(heading, "风险") {
		return false
	}
	for _, row := range table.Rows {
		if column < len(row) && strings.Contains(strings.ToLower(row[column]), "/100") {
			return true
		}
	}
	return false
}

func numericCellValue(cell string) (float64, bool) {
	value, _, ok := numericCell(cell)
	return value, ok
}

// numericCell returns the leading number and the unit token that follows it.
// Keeping the unit with the parsed value lets table bars share a scale across
// read/write columns without ever comparing unlike units.
func numericCell(cell string) (float64, string, bool) {
	fields := strings.Fields(strings.TrimSpace(cell))
	if len(fields) == 0 || fields[0] == "—" || fields[0] == "-" {
		return 0, "", false
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, "", false
	}
	unit := ""
	if len(fields) > 1 {
		// Generated reports use one token (MiB/s, IOPS, ms, %, /100).  Only
		// the first suffix token is part of the unit; trailing prose should
		// not accidentally create a second scale for an otherwise equal cell.
		unit = strings.ToLower(strings.TrimSpace(fields[1]))
	}
	return value, unit, true
}

// table 渲染一张对齐的表格。
//
// 不画竖线边框：中英混排下竖线对齐一旦错位就特别刺眼，而列间距足够分隔数据。
// 只在表头下画一条横线。
func (r *textRenderer) table(columns []string, rows [][]string, rightAlign map[int]bool) {
	if len(columns) == 0 {
		return
	}
	if tableNeedsStackedLayout(r.width, len(columns)) {
		r.stackedTable(columns, rows)
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
	shrinkColumns(widths, r.width-2-2*(len(columns)-1))

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
	r.line(r.palette.Dim("  " + strings.Repeat("─", maxInt(0, minInt(total-2, r.width-2)))))

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

func tableNeedsStackedLayout(width, columns int) bool {
	if columns < 2 {
		return false
	}
	if width <= 0 {
		width = textWidth
	}
	// Two spaces indent, four useful columns per cell, and two spaces between
	// cells. Below this point a horizontal row would collapse into ellipses.
	return width < 2+columns*4+(columns-1)*2
}

func (r *textRenderer) stackedTable(columns []string, rows [][]string) {
	valueWidth := maxInt(1, r.width-4)
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			r.line(r.palette.Dim("  " + strings.Repeat("·", maxInt(1, minInt(8, r.width-2)))))
		}
		for columnIndex, column := range columns {
			value := ""
			if columnIndex < len(row) {
				value = row[columnIndex]
			}
			r.indentedStyled(column+i18n.T("punct.colon"), r.palette.LabelBold)
			for _, line := range wrapText(value, valueWidth) {
				r.linef("    %s", r.semanticValue(line))
			}
		}
	}
}

// shrinkColumns 在总宽超限时从最宽的列开始收缩。
func shrinkColumns(widths []int, budget int) {
	if budget <= 0 {
		return
	}
	minimum := 4
	if budget < len(widths)*minimum && len(widths) > 0 {
		minimum = maxInt(1, budget/len(widths))
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
		// 窄终端上允许列收缩到 4 格；极窄视口再均匀收缩，
		// 以保证整行不溢出。省略号会明确表示被压缩的单元格。
		if index < 0 || widths[index] <= minimum {
			return
		}
		widths[index]--
		total--
	}
}

// textBlock 渲染原文块，逐行缩进以免与正文混淆。
func (r *textRenderer) textBlock(block model.TextBlock) {
	if title := displayTextBlockTitle(block); title != "" {
		r.indented(title, true)
	}
	for _, line := range strings.Split(strings.TrimRight(block.Content, "\n"), "\n") {
		for _, wrapped := range wrapText(line, maxInt(1, r.width-6)) {
			r.linef("    %s", wrapped)
		}
	}
	r.blank()
}

func (r *textRenderer) note(text string) {
	// 说明文字按版面宽度折行，超长的一行在窄终端里会被硬折得难读。
	//
	// 只有第一行带项目符号，续行对齐缩进：每行都带 "·" 会让一条折了行的说明
	// 看起来像三条独立说明，而说明里本来就常含逗号和顿号，读者无从分辨。
	for index, line := range wrapText(text, maxInt(1, r.width-6)) {
		if index == 0 {
			r.linef("  %s %s", r.palette.Dim("·"), r.palette.Dim(line))
			continue
		}
		r.linef("    %s", r.palette.Dim(line))
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
	for _, line := range wrapText(heading, r.width) {
		r.line(r.palette.AccentBold(line))
	}
	r.line(r.palette.Dim(strings.Repeat("-", r.width)))
}

func (r *textRenderer) footer(data model.Report) {
	if !r.compact && len(data.Notices) > 0 {
		r.sectionTitle(i18n.T("report.notices"), "")
		for _, notice := range data.Notices {
			r.note(renderMessage(notice))
		}
		r.blank()
	}
	r.line(r.palette.Dim(strings.Repeat("#", r.width)))
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
