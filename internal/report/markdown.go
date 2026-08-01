package report

import (
	"fmt"

	"ecs/internal/i18n"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ecs/internal/model"
)

func Markdown(data model.Report) string {
	var out strings.Builder
	out.WriteString("# " + i18n.T("report.title") + "\n\n")
	out.WriteString("> ")
	out.WriteString(statusIcon(data.Summary.Status))
	out.WriteByte(' ')
	out.WriteString(data.Summary.Headline)
	out.WriteString(i18n.T("punct.sentenceEnd") + i18n.T("report.local") + "\n\n")

	out.WriteString("## " + i18n.T("report.overview") + "\n\n")
	out.WriteString("| " + i18n.T("report.item") + " | " + i18n.T("report.content") + " |\n| --- | --- |\n")
	writeMarkdownRow(&out, i18n.T("report.reportID"), data.Run.ID)
	writeMarkdownRow(&out, i18n.T("report.version"), data.Tool.Version+" ("+data.Tool.Commit+")")
	writeMarkdownRow(&out, i18n.T("report.profile"), data.Run.Profile)
	writeMarkdownRow(&out, i18n.T("report.startedAt"), data.Run.StartedAt.Format(time.RFC3339))
	writeMarkdownRow(&out, i18n.T("report.totalDuration"), formatDurationMS(data.Run.DurationMS))
	writeMarkdownRow(&out, i18n.T("report.networkMode"), map[bool]string{true: i18n.T("report.offline"), false: i18n.T("report.online")}[data.Run.Offline])
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
		out.WriteString(markdownEscape(result.Title))
		out.WriteString(" | ")
		out.WriteString(markdownEscape(localizedMethodology(result.Methodology)))
		out.WriteString(" | ")
		out.WriteString(statusIcon(result.Status) + " " + statusLabel(result.Status))
		out.WriteString(" | ")
		out.WriteString(markdownEscape(result.Summary))
		out.WriteString(" | ")
		out.WriteString(formatDurationMS(result.DurationMS))
		out.WriteString(" |\n")
	}
	out.WriteString("\n")

	for _, result := range data.Results {
		out.WriteString("## ")
		out.WriteString(markdownEscape(result.Title))
		out.WriteString("\n\n")
		if result.Description != "" {
			out.WriteString(markdownEscape(result.Description))
			out.WriteString("\n\n")
		}
		if result.Methodology.Label != "" {
			out.WriteString("**" + i18n.T("report.methodologyLabel") + "**" + i18n.T("punct.colon"))
			out.WriteString(markdownEscape(result.Methodology.Label))
			if result.Methodology.Engine != "" {
				out.WriteString(" · ")
				out.WriteString(markdownEscape(result.Methodology.Engine))
			}
			if result.Methodology.Profile != "" {
				out.WriteString(" · `")
				out.WriteString(strings.ReplaceAll(result.Methodology.Profile, "`", "\\`"))
				out.WriteString("`")
			}
			out.WriteString("\n\n")
			if result.Methodology.ComparisonScope != "" {
				out.WriteString("> " + i18n.T("report.comparability") + i18n.T("punct.colon"))
				out.WriteString(markdownEscape(result.Methodology.ComparisonScope))
				out.WriteString("\n\n")
			}
		}
		out.WriteString("**")
		out.WriteString(statusIcon(result.Status) + " " + statusLabel(result.Status))
		out.WriteString("**")
		if result.Summary != "" {
			out.WriteString(" · ")
			out.WriteString(markdownEscape(result.Summary))
		}
		out.WriteString(" · ")
		out.WriteString(formatDurationMS(result.DurationMS))
		out.WriteString("\n\n")
		if result.Error != "" {
			out.WriteString("> " + i18n.T("report.errorPrefix") + i18n.T("punct.colon"))
			out.WriteString(markdownEscape(result.Error))
			out.WriteString("\n\n")
		}
		if len(result.Measurements) > 0 {
			out.WriteString("### " + i18n.T("report.metrics") + "\n\n")
			out.WriteString("| " + i18n.T("report.metric") + " | " + i18n.T("report.value") + " | " +
				i18n.T("report.rating") + " | " + i18n.T("report.method") + " |\n| --- | ---: | --- | --- |\n")
			for _, metric := range result.Measurements {
				out.WriteString("| ")
				out.WriteString(markdownEscape(metric.Label))
				out.WriteString(" | ")
				out.WriteString(markdownEscape(metric.Display))
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
			for _, field := range result.Fields {
				writeMarkdownRow(&out, field.Label, field.Value)
			}
			out.WriteString("\n")
		}
		for _, table := range result.Tables {
			if table.Title != "" {
				out.WriteString("### ")
				out.WriteString(markdownEscape(table.Title))
				out.WriteString("\n\n")
			}
			writeMarkdownTable(&out, table)
			out.WriteString("\n")
		}
		for _, block := range result.TextBlocks {
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
				out.WriteString(markdownEscape(note))
				out.WriteString("\n")
			}
			out.WriteString("\n")
		}
		if len(result.Sources) > 0 {
			out.WriteString("### " + i18n.T("report.sources") + "\n\n")
			for _, source := range result.Sources {
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

	out.WriteString("## " + i18n.T("report.notices") + "\n\n")
	for _, notice := range data.Notices {
		out.WriteString("- ")
		out.WriteString(markdownEscape(notice))
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
		out.WriteString(markdownEscape(column))
	}
	out.WriteString(" |\n| ")
	for index := range table.Columns {
		if index > 0 {
			out.WriteString(" | ")
		}
		out.WriteString("---")
	}
	out.WriteString(" |\n")
	for _, row := range table.Rows {
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

func markdownEscape(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
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
	if m.Kind != "" {
		if key := "methodology." + m.Kind; i18n.Has(i18n.Current(), key) {
			return i18n.T(key)
		}
	}
	if m.Label != "" {
		return m.Label
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
