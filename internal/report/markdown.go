package report

import (
	"fmt"

	"ecs/internal/i18n"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

// Markdown 渲染一份人类可读的 Markdown 报告。它是 report 包对外的渲染入口，
// 被其他包的渲染契约测试跨包使用；WriteFilesWithOptions 走内部实现。
func Markdown(data model.Report, scored *score.Report) string {
	return markdownReport(data, scored)
}

// markdownReport renders the machine report directly. Stable presentation
// keys are resolved at the individual output boundary; raw evidence is not
// copied through a localized report tree.
func markdownReport(data model.Report, scored *score.Report) string {
	var out strings.Builder
	out.WriteString("# " + i18n.T("report.title") + "\n\n")
	out.WriteString("> ")
	out.WriteString(statusIcon(data.Summary.Status))
	out.WriteByte(' ')
	out.WriteString(markdownMessages(data.Summary.Messages))
	out.WriteString(i18n.T("punct.sentenceEnd") + i18n.T("report.local") + "\n\n")

	out.WriteString("## " + i18n.T("report.overview") + "\n\n")
	out.WriteString("| " + i18n.T("report.item") + " | " + i18n.T("report.content") + " |\n| --- | --- |\n")
	writeMarkdownRow(&out, i18n.T("report.reportID"), data.Run.ID)
	writeMarkdownRow(&out, i18n.T("report.version"), data.Tool.Version+" ("+data.Tool.Commit+")")
	writeMarkdownRow(&out, i18n.T("report.profile"), data.Run.Profile)
	writeMarkdownRow(&out, i18n.T("report.startedAt"), data.Run.StartedAt.Format(time.RFC3339))
	writeMarkdownRow(&out, i18n.T("report.totalDuration"), formatDurationMS(data.Run.DurationMS))
	networkMode := i18n.T("report.online")
	if data.Run.Exposure == "local" {
		networkMode = i18n.T("report.offline")
	}
	writeMarkdownRow(&out, i18n.T("report.networkMode"), networkMode)
	if data.Run.Exposure != "" {
		writeMarkdownRow(&out, i18n.T("report.exposure"), data.Run.Exposure+" — "+i18n.T("exposure."+data.Run.Exposure))
	}
	if data.Run.IPVersion != "" {
		writeMarkdownRow(&out, i18n.T("report.ipVersion"), data.Run.IPVersion)
	}
	writeMarkdownRow(&out, i18n.T("report.privacy"), map[bool]string{true: i18n.T("report.redacted"), false: i18n.T("report.revealed")}[data.Run.Redacted])
	if data.Run.Canceled {
		writeMarkdownRow(&out, i18n.T("report.runState"), i18n.T("report.canceled"))
	}
	out.WriteString("\n")

	out.WriteString("## " + i18n.T("report.glance") + "\n\n")
	out.WriteString("| " + i18n.T("report.module") + " | " + i18n.T("report.scope") + " | " +
		i18n.T("report.status") + " | " + i18n.T("report.summary") + " | " + i18n.T("report.duration") + " |\n| --- | --- | --- | --- | --- |\n")
	for _, result := range data.Results {
		out.WriteString("| ")
		out.WriteString(markdownEscape(resultTitle(result)))
		out.WriteString(" | ")
		out.WriteString(markdownEscape(localizedMethodology(result.Methodology)))
		out.WriteString(" | ")
		out.WriteString(statusIcon(result.Status) + " " + statusLabel(result.Status))
		out.WriteString(" | ")
		out.WriteString(markdownMessages(result.SummaryMessages))
		out.WriteString(" | ")
		out.WriteString(formatDurationMS(result.DurationMS))
		out.WriteString(" |\n")
	}
	out.WriteString("\n")

	for _, result := range data.Results {
		out.WriteString("## ")
		out.WriteString(markdownEscape(resultTitle(result)))
		out.WriteString("\n\n")
		if result.Description != "" {
			out.WriteString(markdownEscape(displayKey(result.Description)))
			out.WriteString("\n\n")
		}
		methodology := displayMethodology(result.Methodology)
		if methodology.Label != "" {
			out.WriteString("**" + i18n.T("report.methodologyLabel") + "**" + i18n.T("punct.colon"))
			out.WriteString(markdownEscape(localizedMethodology(methodology)))
			if methodology.Engine != "" {
				out.WriteString(" · ")
				out.WriteString(markdownEscape(methodology.Engine))
			}
			if methodology.Profile != "" {
				out.WriteString(" · `")
				out.WriteString(strings.ReplaceAll(methodology.Profile, "`", "\\`"))
				out.WriteString("`")
			}
			out.WriteString("\n\n")
			if methodology.ComparisonScope != "" {
				out.WriteString("> " + i18n.T("report.comparability") + i18n.T("punct.colon"))
				out.WriteString(markdownEscape(methodology.ComparisonScope))
				out.WriteString("\n\n")
			}
		}
		out.WriteString("**")
		out.WriteString(statusIcon(result.Status) + " " + statusLabel(result.Status))
		out.WriteString("**")
		if summary := markdownMessages(result.SummaryMessages); summary != "" {
			out.WriteString(" · ")
			out.WriteString(summary)
		}
		out.WriteString(" · ")
		out.WriteString(formatDurationMS(result.DurationMS))
		out.WriteString("\n\n")
		if result.Evidence != nil {
			out.WriteString("**" + i18n.T("report.evidence") + "**" + i18n.T("punct.colon"))
			out.WriteString(markdownEscape(evidenceText(*result.Evidence)))
			out.WriteString(" · `" + termcolor.Palette{Level: termcolor.LevelNone}.Bar(result.Evidence.EvidenceRatio(), 16) + "`\n\n")
		}
		if len(result.Failures) > 0 {
			out.WriteString("### " + i18n.T("report.failures") + "\n\n")
			out.WriteString("| " + i18n.T("failure.category") + " | " + i18n.T("failure.stage") + " | " +
				i18n.T("failure.target") + " | " + i18n.T("failure.count") + " | " +
				i18n.T("failure.retryable") + " | " + i18n.T("failure.message") + " |\n")
			out.WriteString("| --- | --- | --- | ---: | --- | --- |\n")
			for _, failure := range result.Failures {
				values := []string{
					failureCategoryLabel(failure.Category), fallbackReport(failure.Stage, "—"),
					fallbackReport(failure.Target, "—"), fmt.Sprintf("%d", maxInt(failure.Count, 1)),
					failureRetryableLabel(failure.Retryable), fallbackReport(failure.Message, "—"),
				}
				out.WriteString("| ")
				for index, value := range values {
					if index > 0 {
						out.WriteString(" | ")
					}
					out.WriteString(markdownEscape(value))
				}
				out.WriteString(" |\n")
			}
			out.WriteString("\n")
		}
		renderRetryInterferenceMarkdown(&out, result)
		if len(result.Measurements) > 0 {
			out.WriteString("### " + i18n.T("report.metrics") + "\n\n")
			out.WriteString("| " + i18n.T("report.metric") + " | " + i18n.T("report.value") + " | " +
				i18n.T("report.rating") + " | " + i18n.T("report.method") + " |\n| --- | ---: | --- | --- |\n")
			for _, rawMetric := range result.Measurements {
				metric := displayMeasurement(rawMetric)
				out.WriteString("| ")
				out.WriteString(markdownEscape(metric.Label))
				out.WriteString(" | ")
				out.WriteString(markdownEscape(displayValue(metric.Display)))
				out.WriteString(" | ")
				out.WriteString(markdownEscape(fallbackReport(metric.Rating, "—")))
				out.WriteString(" | ")
				out.WriteString(markdownEscape(fallbackReport(metric.Method, "—")))
				out.WriteString(" |\n")
			}
			out.WriteString("\n")
		}
		if len(result.Fields) > 0 {
			out.WriteString("### " + i18n.T("report.details") + "\n\n")
			out.WriteString("| " + i18n.T("report.field") + " | " + i18n.T("report.content") + " |\n| --- | --- |\n")
			for _, rawField := range result.Fields {
				field := displayField(rawField)
				writeMarkdownRow(&out, field.Label, displayValue(field.Value))
			}
			out.WriteString("\n")
		}
		for _, rawTable := range result.Tables {
			table := displayTable(rawTable)
			if table.Title != "" {
				out.WriteString("### ")
				out.WriteString(markdownEscape(table.Title))
				out.WriteString("\n\n")
			}
			writeMarkdownTable(&out, table)
			out.WriteString("\n")
		}
		for _, rawBlock := range result.TextBlocks {
			block := rawBlock
			block.Title = displayTextBlockTitle(block)
			out.WriteString("<details>\n<summary>")
			out.WriteString(markdownEscape(block.Title))
			out.WriteString("</summary>\n\n")
			out.WriteString("```")
			out.WriteString(markdownLanguage(block.Language))
			out.WriteString("\n")
			out.WriteString(strings.ReplaceAll(block.Content, "```", "``\\`"))
			out.WriteString("\n```\n\n</details>\n\n")
		}
		if len(result.Notes) > 0 {
			out.WriteString("### " + i18n.T("report.notes") + "\n\n")
			for _, note := range result.Notes {
				out.WriteString("- ")
				out.WriteString(markdownEscape(displayKey(note)))
				out.WriteString("\n")
			}
			out.WriteString("\n")
		}
		if len(result.Sources) > 0 {
			out.WriteString("### " + i18n.T("report.sources") + "\n\n")
			for _, rawSource := range result.Sources {
				source := rawSource
				source.Name = displayKey(source.Name)
				source.Purpose = displayKey(source.Purpose)
				out.WriteString("- ")
				if safeURL := safeMarkdownURL(source.URL); safeURL != "" {
					out.WriteString("[")
					out.WriteString(markdownEscape(source.Name))
					out.WriteString("](")
					out.WriteString(safeURL)
					out.WriteString(")")
				} else {
					out.WriteString(markdownEscape(source.Name))
				}
				if source.Purpose != "" {
					out.WriteString(i18n.T("punct.colon"))
					out.WriteString(markdownEscape(source.Purpose))
				}
				out.WriteString("\n")
			}
			out.WriteString("\n")
		}
	}

	if scored != nil {
		writeMarkdownScore(&out, scored)
	}

	out.WriteString("## " + i18n.T("report.notices") + "\n\n")
	for _, notice := range data.Notices {
		out.WriteString("- ")
		out.WriteString(markdownMessage(notice))
		out.WriteString("\n")
	}
	out.WriteString("\n")
	out.WriteString(fmt.Sprintf("Schema: `%s` · %s: `%s %s`\n", data.SchemaVersion, i18n.T("report.generator"), data.Tool.Name, data.Tool.Version))
	return out.String()
}

func writeMarkdownRow(out *strings.Builder, label, value string) {
	out.WriteString("| ")
	out.WriteString(markdownEscape(label))
	out.WriteString(" | ")
	out.WriteString(markdownEscape(value))
	out.WriteString(" |\n")
}

func writeMarkdownTable(out *strings.Builder, table model.Table) {
	if len(table.Columns) == 0 {
		return
	}
	out.WriteString("| ")
	for index, column := range table.Columns {
		if index > 0 {
			out.WriteString(" | ")
		}
		out.WriteString(markdownEscape(displayTableColumnLabel(column)))
	}
	out.WriteString(" |\n| ")
	for index := range table.Columns {
		if index > 0 {
			out.WriteString(" | ")
		}
		out.WriteString("---")
	}
	out.WriteString(" |\n")
	rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone})
	for _, row := range rows {
		out.WriteString("| ")
		for index := range table.Columns {
			if index > 0 {
				out.WriteString(" | ")
			}
			value := ""
			if index < len(row) {
				value = row[index]
			}
			out.WriteString(markdownEscape(value))
		}
		out.WriteString(" |\n")
	}
}

// markdownEscape 转义单元格与正文里的不可信文本。
//
// 除了 HTML 实体与表格分隔符，还要转义链接语法：报告里的公司名、ISP 名与
// 错误信息来自第三方 API，未转义的 `[文本](URL)` 会在渲染后变成可点击链接。
func markdownEscape(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	for _, character := range []string{"[", "]", "(", ")"} {
		value = strings.ReplaceAll(value, character, "\\"+character)
	}
	return value
}

func markdownEscapeMessageArg(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	for _, character := range []string{"*", "_", "`", "~"} {
		value = strings.ReplaceAll(value, character, "\\"+character)
	}
	return markdownEscape(value)
}

// markdownMessage resolves a structured message and escapes only its dynamic
// arguments. Localized format strings are trusted presentation text; report
// data supplied through Message.Args is not.
func markdownMessage(message model.Message) string {
	if message.Key == "" {
		return ""
	}
	format := i18n.T(message.Key)
	if format == message.Key || len(message.Args) == 0 {
		return format
	}
	args := make([]any, len(message.Args))
	for index, arg := range message.Args {
		args[index] = markdownEscapeMessageArg(renderMessageArg(message.Key, index, arg))
	}
	return fmt.Sprintf(format, args...)
}

func markdownMessages(messages []model.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(markdownMessage(message))
	}
	return builder.String()
}

var markdownLanguagePattern = regexp.MustCompile(`[^A-Za-z0-9_+.-]`)

func markdownLanguage(value string) string {
	return markdownLanguagePattern.ReplaceAllString(value, "")
}

func safeMarkdownURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func statusLabel(status model.Status) string {
	switch status {
	case model.StatusOK:
		return i18n.T("status.ok")
	case model.StatusWarning:
		return i18n.T("status.warning")
	case model.StatusSkipped:
		return i18n.T("status.skipped")
	case model.StatusError:
		return i18n.T("status.error")
	default:
		return string(status)
	}
}

// localizedMethodology 把 methodology.kind 翻成当前语言。
//
// 报告里的 Label 是探针写死的中文，英文界面下改用 kind 查译文——
// kind 是稳定的机器标识，正适合做 i18n 的 key。
func localizedMethodology(m model.Methodology) string {
	m = displayMethodology(m)
	if m.Kind != "" {
		if key := "methodology." + m.Kind; i18n.Has(i18n.Current(), key) {
			return i18n.T(key)
		}
	}
	if m.Label != "" {
		return displayKey(m.Label)
	}
	return i18n.T("methodology.unlabeled")
}

func statusIcon(status model.Status) string {
	switch status {
	case model.StatusOK:
		return "✓"
	case model.StatusWarning:
		return "!"
	case model.StatusSkipped:
		return "–"
	case model.StatusError:
		return "×"
	default:
		return "·"
	}
}

func formatDurationMS(milliseconds int64) string {
	duration := time.Duration(milliseconds) * time.Millisecond
	if duration < time.Second {
		return duration.Round(time.Millisecond).String()
	}
	if duration < time.Minute {
		return duration.Round(100 * time.Millisecond).String()
	}
	return duration.Round(time.Second).String()
}

func fallbackReport(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// writeMarkdownScore 渲染评分区。
//
// markdown 不能依赖 ANSI 颜色，因此层次全部由柱长与密度字符承担——这也是
// 无色档位一直要能独立表达层次的原因。
func writeMarkdownScore(out *strings.Builder, scored *score.Report) {
	plain := termcolor.Palette{Level: termcolor.LevelNone}
	out.WriteString("## " + i18n.T("score.title") + "\n\n")
	out.WriteString("**" + i18n.T("score.total") + "** " +
		formatScore(scored.Total) + " · " +
		fmt.Sprintf(i18n.T("score.coverage"), scored.Covered, scored.Possible) + "\n\n")

	out.WriteString("| " + i18n.T("report.metric") + " | " + i18n.T("report.value") + " | |\n| --- | ---: | --- |\n")
	for _, dimension := range scored.Dimensions {
		name := i18n.T("score.dimension." + dimension.Key)
		if dimension.Missing {
			out.WriteString("| " + markdownEscape(name) + " | " +
				markdownEscape(i18n.T("score.missing."+dimension.MissingReason)) + " | — |\n")
			continue
		}
		out.WriteString("| " + markdownEscape(name) + " | " + formatScore(dimension.Score) +
			" | `" + plain.Bar(dimension.Ratio, 20) + "` |\n")
		if len(dimension.MissingMetrics) > 0 {
			out.WriteString("| " + markdownEscape(name) + " | " +
				markdownEscape(fmt.Sprintf(i18n.T("score.missingMetrics"), name, len(dimension.MissingMetrics), strings.Join(dimension.MissingMetrics, ", "))) + " | — |\n")
		}
	}
	out.WriteString("\n")
	if !scored.Complete {
		out.WriteString("> " + i18n.T("score.incompleteWarning") + "\n\n")
	}
	out.WriteString("> " + i18n.T("score.weightingNote") + "\n\n")
	out.WriteString(fmt.Sprintf(i18n.T("score.baselineLine"),
		baselineSourceLabel(scored.BaselineSource), scored.BaselineSample) + "\n\n")
	if scored.RankStatus != "" || scored.RankSamples > 0 || scored.BaselineSample > 0 {
		var rankLine string
		switch scored.EffectiveRankStatus() {
		case score.RankStatusAvailable:
			rankLine = fmt.Sprintf(i18n.T("score.rank.available"), scored.TopPercent, scored.EffectiveRankSamples())
		case score.RankStatusInsufficient:
			rankLine = fmt.Sprintf(i18n.T("score.rank.insufficient"), scored.EffectiveRankSamples(), scored.EffectiveRankMinSamples())
		default:
			rankLine = i18n.T("score.rank.unavailable")
		}
		out.WriteString("> " + rankLine + "\n\n")
	}
	if scored.TierLabel != "" {
		out.WriteString(fmt.Sprintf(i18n.T("score.tierLine"), scored.HostVCPU, scored.TierLabel) + "\n\n")
	} else if scored.HostVCPU > 0 {
		out.WriteString(fmt.Sprintf(i18n.T("score.tierFallbackLine"), scored.HostVCPU) + "\n\n")
	}
}

func formatScore(value float64) string {
	return fmt.Sprintf("%.0f", value)
}
