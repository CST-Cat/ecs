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
	"strconv"
	"strings"

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
	data = sanitizedCopy(data)
	options.Score = sanitizedCopy(options.Score)
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
	summaryText  string
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
	return min(preferred, limit)
}

func (r *textRenderer) render(data model.Report) string {
	r.version = data.Tool.Version
	r.summaryText = reportSummaryText(data.Summary)
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
	r.centeredStyled(i18n.T("report.reportTime")+i18n.T("punct.colon")+reportTime.Format("2006-01-02 15:04:05 MST"), r.palette.Dim)
	r.centeredStyled(i18n.T("report.scriptVersion")+i18n.T("punct.colon")+fallbackReport(data.Tool.Version, "—"), r.palette.Info)
	r.centeredStyled(i18n.T("report.runProfile")+i18n.T("punct.colon")+fallbackReport(data.Run.Profile, "—"), r.palette.Info)
	if summaryText := reportSummaryText(data.Summary); summaryText != "" {
		r.centeredStyled(i18n.T("report.reportStatus")+i18n.T("punct.colon")+summaryText, r.statusStyle(data.Summary.Status))
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
	allTabs := []string{
		i18n.T("report.navigation.basic"),
		i18n.T("report.navigation.hardware"),
		i18n.T("report.navigation.ipQuality"),
		i18n.T("report.navigation.networkQuality"),
		i18n.T("report.navigation.returnPath"),
	}
	r.compactList(i18n.T("report.navigation.all"), allTabs)
	r.compactList(i18n.T("report.navigation.selected"), moduleTitles(selected))
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

// result 渲染单个模块。
func (r *textRenderer) result(result model.Result) {
	r.section++
	r.subsectionNo = 0
	r.moduleBanner(result)
	if result.Status != model.StatusOK && result.Description != "" {
		r.indented(displayKey(result.Description))
	}
	// 状态不能依赖 Summary 是否存在：跳过、空结果和仅有错误的结果也必须
	// 明确显示状态，完整报告不能让读者靠章节标题猜测执行结果。
	status := statusIcon(result.Status) + " " + statusLabel(result.Status)
	if summary := resultSummary(result); summary != "" && strings.TrimSpace(summary) != strings.TrimSpace(r.summaryText) {
		status += " · " + summary
	}
	if result.Status != model.StatusOK {
		r.indentedStyled(status, r.statusStyle(result.Status))
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
			strconv.Itoa(max(failure.Count, 1)),
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
	ratio := evidence.Ratio()
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
	barSize := min(16, adaptiveBarWidth(r.width, 16))
	if available := r.width - textwidth.Width(prefix) - 2; available < barSize {
		barSize = max(0, available)
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
	state := i18n.T("evidence." + string(evidence.DerivedGrade()))
	return fmt.Sprintf("%s · %.0f%% · %s", count, evidence.Ratio()*100, state)
}

// resultEvidence is rendered in file txt output but intentionally omitted from
// the interactive terminal summary.  JSON remains the lossless machine
// artifact; this keeps txt equally useful when a user only has one human-
// readable file to inspect.
func (r *textRenderer) resultEvidence(result model.Result) {
	if result.Description != "" && result.Status == model.StatusOK {
		r.subsection(i18n.T("report.description"))
		r.indented(displayKey(result.Description))
	}
	methodology := displayMethodology(result.Methodology)
	if methodology.Kind != "" || methodology.Label != "" || methodology.Engine != "" || methodology.Profile != "" || methodology.ComparisonScope != "" {
		r.subsection(i18n.T("report.methodologyLabel"))
		if label := localizedMethodology(methodology); label != "" {
			r.indented(label)
		}
		if methodology.Engine != "" {
			r.indented(i18n.T("report.engine") + i18n.T("punct.colon") + methodology.Engine)
		}
		if methodology.Profile != "" {
			r.indented(i18n.T("report.profileWorkload") + i18n.T("punct.colon") + methodology.Profile)
		}
		if methodology.ComparisonScope != "" {
			r.indented(i18n.T("report.comparability") + i18n.T("punct.colon") + methodology.ComparisonScope)
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
			r.note(displayKey(note))
		}
		r.blank()
	}
	if len(result.Sources) > 0 {
		r.subsection(i18n.T("report.sources"))
		bannerSource := -1
		for index, rawSource := range result.Sources {
			source := rawSource
			source.Name = displayKey(source.Name)
			source.Purpose = displayKey(source.Purpose)
			if strings.TrimSpace(source.URL) != "" {
				bannerSource = index
				break
			}
		}
		for index, rawSource := range result.Sources {
			source := rawSource
			source.Name = displayKey(source.Name)
			source.Purpose = displayKey(source.Purpose)
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

// textBlock 渲染原文块，逐行缩进以免与正文混淆。
func (r *textRenderer) textBlock(block model.TextBlock) {
	if title := displayTextBlockTitle(block); title != "" {
		r.indented(title, true)
	}
	for _, line := range strings.Split(strings.TrimRight(block.Content, "\n"), "\n") {
		for _, wrapped := range wrapText(line, max(1, r.width-6)) {
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
	for index, line := range wrapText(text, max(1, r.width-6)) {
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
