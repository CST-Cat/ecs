package report

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ecs/internal/model"
)

func Markdown(data model.Report) string {
	var out strings.Builder
	out.WriteString("# ecs VPS 综合测试报告\n\n")
	out.WriteString("> ")
	out.WriteString(statusIcon(data.Summary.Status))
	out.WriteByte(' ')
	out.WriteString(data.Summary.Headline)
	out.WriteString("。报告由本地生成，未自动上传。\n\n")

	out.WriteString("## 运行概览\n\n")
	out.WriteString("| 项目 | 内容 |\n| --- | --- |\n")
	writeMarkdownRow(&out, "报告 ID", data.Run.ID)
	writeMarkdownRow(&out, "ecs 版本", data.Tool.Version+" ("+data.Tool.Commit+")")
	writeMarkdownRow(&out, "配置档", data.Run.Profile)
	writeMarkdownRow(&out, "开始时间", data.Run.StartedAt.Format(time.RFC3339))
	writeMarkdownRow(&out, "总耗时", formatDurationMS(data.Run.DurationMS))
	writeMarkdownRow(&out, "网络模式", map[bool]string{true: "离线", false: "在线"}[data.Run.Offline])
	writeMarkdownRow(&out, "隐私", map[bool]string{true: "敏感字段已遮盖", false: "包含完整敏感字段"}[data.Run.Redacted])
	if data.Run.Canceled {
		writeMarkdownRow(&out, "运行状态", "用户中断，报告包含已完成部分")
	}
	out.WriteString("\n")

	out.WriteString("## 一眼看懂\n\n")
	out.WriteString("| 模块 | 口径 | 状态 | 摘要 | 耗时 |\n| --- | --- | --- | --- | --- |\n")
	for _, result := range data.Results {
		out.WriteString("| ")
		out.WriteString(markdownEscape(result.Title))
		out.WriteString(" | ")
		out.WriteString(markdownEscape(fallbackReport(result.Methodology.Label, "未标注")))
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
			out.WriteString("**测试口径**：")
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
				out.WriteString("> 可比范围：")
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
			out.WriteString("> 错误：")
			out.WriteString(markdownEscape(result.Error))
			out.WriteString("\n\n")
		}
		if len(result.Measurements) > 0 {
			out.WriteString("### 关键指标\n\n")
			out.WriteString("| 指标 | 数值 | 评价 | 方法 |\n| --- | ---: | --- | --- |\n")
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
			out.WriteString("### 详情\n\n")
			out.WriteString("| 字段 | 内容 |\n| --- | --- |\n")
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
			out.WriteString("### 说明\n\n")
			for _, note := range result.Notes {
				out.WriteString("- ")
				out.WriteString(markdownEscape(note))
				out.WriteString("\n")
			}
			out.WriteString("\n")
		}
		if len(result.Sources) > 0 {
			out.WriteString("### 数据来源\n\n")
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
					out.WriteString("：")
					out.WriteString(markdownEscape(source.Purpose))
				}
				out.WriteString("\n")
			}
			out.WriteString("\n")
		}
	}

	out.WriteString("## 报告说明\n\n")
	for _, notice := range data.Notices {
		out.WriteString("- ")
		out.WriteString(markdownEscape(notice))
		out.WriteString("\n")
	}
	out.WriteString("\n")
	out.WriteString(fmt.Sprintf("Schema：`%s` · 生成器：`%s %s`\n", data.SchemaVersion, data.Tool.Name, data.Tool.Version))
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
		return "完成"
	case model.StatusWarning:
		return "需留意"
	case model.StatusSkipped:
		return "跳过"
	case model.StatusError:
		return "异常"
	default:
		return string(status)
	}
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
