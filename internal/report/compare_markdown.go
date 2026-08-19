package report

import (
	"fmt"
	"sort"
	"strings"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

// ComparisonMarkdown renders an adaptive, self-contained comparison.  Best
// values are bold even in renderers that ignore emoji or HTML styling, and
// density bars preserve the visual ranking in plain Markdown source.
func ComparisonMarkdown(data comparison.Report) string {
	var out strings.Builder
	out.WriteString("# " + i18n.T("compare.title") + "\n\n")
	out.WriteString("> " + markdownEscape(i18n.T("compare.subtitle")) + "\n\n")

	out.WriteString("## " + i18n.T("report.overview") + "\n\n")
	out.WriteString("| " + i18n.T("report.item") + " | " + i18n.T("report.content") + " |\n| --- | ---: |\n")
	comparisonMarkdownRow(&out, i18n.T("compare.comparability"), comparisonLabel(string(data.Summary.Comparability)))
	comparisonMarkdownRow(&out, i18n.T("compare.reports"), fmt.Sprintf("%d", data.Summary.Reports))
	comparisonMarkdownRow(&out, i18n.T("compare.modules"), fmt.Sprintf("%d", data.Summary.Modules))
	comparisonMarkdownRow(&out, i18n.T("compare.metrics"), fmt.Sprintf("%d", data.Summary.ComparableMetrics))
	comparisonMarkdownRow(&out, i18n.T("compare.improved"), fmt.Sprintf("▲ **%d**", data.Summary.Improved))
	comparisonMarkdownRow(&out, i18n.T("compare.regressed"), fmt.Sprintf("▼ **%d**", data.Summary.Regressed))
	comparisonMarkdownRow(&out, i18n.T("compare.unchanged"), fmt.Sprintf("= %d", data.Summary.Unchanged))
	comparisonMarkdownRow(&out, i18n.T("compare.metricIssues"), fmt.Sprintf("%d", data.Summary.MetricIssues))
	comparisonMarkdownRow(&out, i18n.T("compare.statusChanges"), fmt.Sprintf("%d", data.Summary.StatusChanges))
	comparisonMarkdownRow(&out, i18n.T("compare.evidenceChanges"), fmt.Sprintf("%d", data.Summary.EvidenceChanges))
	if schemas := data.SchemaVersions(); len(schemas) > 1 {
		comparisonMarkdownRow(&out, i18n.T("compare.schemaVersions"), markdownEscape(strings.Join(schemas, " · ")))
	}
	out.WriteString("\n> " + markdownEscape(i18n.T("compare.legend")) + "\n\n")

	out.WriteString("## " + i18n.T("compare.inputReports") + "\n\n")
	out.WriteString("| # | " + i18n.T("compare.report") + " | " + i18n.T("report.profile") + " | " + i18n.T("report.version") + " | " + i18n.T("report.startedAt") + " |\n")
	out.WriteString("| ---: | --- | --- | --- | --- |\n")
	for _, input := range data.Inputs {
		label := markdownEscape(comparisonInputLabel(data, input.Index))
		if input.Index == data.Reference {
			label = "**◆ " + label + "**"
		}
		started := "—"
		if !input.StartedAt.IsZero() {
			started = input.StartedAt.Format("2006-01-02 15:04 MST")
		}
		fmt.Fprintf(&out, "| %d | %s | %s | %s | %s |\n",
			input.Index+1, label, markdownEscape(fallbackReport(input.Profile, "—")),
			markdownEscape(fallbackReport(input.ToolVersion, "—")), markdownEscape(started))
	}
	out.WriteString("\n")

	layout := comparisonLayoutFor(len(data.Inputs))
	for _, module := range data.Modules {
		out.WriteString("## " + markdownEscape(comparisonModuleTitle(module)) + "\n\n")
		out.WriteString("**" + i18n.T("compare.comparability") + "**" + i18n.T("punct.colon") + markdownEscape(comparisonLabel(string(module.Comparability))) + "\n\n")
		writeComparisonMarkdownStatus(&out, data, module, layout)
		if len(module.Metrics) > 0 {
			out.WriteString("### " + i18n.T("compare.performance") + "\n\n")
			writeComparisonMarkdownMetrics(&out, data, module.Metrics, layout)
		}
		if len(module.Changes) > 0 {
			out.WriteString("### " + i18n.T("compare.discreteChanges") + "\n\n")
			writeComparisonMarkdownObservations(&out, data, module.Changes, layout)
		}
		if len(module.MetricIssues) > 0 {
			out.WriteString("### " + i18n.T("compare.methodIssues") + "\n\n")
			out.WriteString("| " + i18n.T("report.metric") + " | " + i18n.T("compare.methodIssues") + " | " + i18n.T("compare.reports") + " |\n| --- | --- | --- |\n")
			for _, issue := range module.MetricIssues {
				labels := make([]string, 0, len(issue.Reports))
				for _, index := range issue.Reports {
					labels = append(labels, comparisonInput(data, index).Label)
				}
				fmt.Fprintf(&out, "| %s | ⚠ %s | %s |\n",
					markdownEscape(fallbackReport(issue.Label, issue.Key)),
					markdownEscape(comparisonIssueLabel(issue.Reason)),
					markdownEscape(strings.Join(labels, ", ")))
			}
			out.WriteString("\n")
			// 原因码只说"口径不一致"。这一段说清楚不一致在哪，报告标签后带上
			// 产出它的 ecs 版本。差异相同的指标合并成一组。
			if groups := comparisonDifferenceGroups(module.MetricIssues); len(groups) > 0 {
				out.WriteString("**" + markdownEscape(i18n.T("compare.differences")) + "**\n\n")
				for _, group := range groups {
					fmt.Fprintf(&out, "- %s\n", markdownEscape(strings.Join(group.Metrics, i18n.T("punct.listSep"))))
					for _, difference := range group.Differences {
						fmt.Fprintf(&out, "  - %s%s%s\n",
							markdownEscape(comparisonDifferenceLabel(difference.Field)),
							markdownEscape(i18n.T("punct.colon")),
							markdownEscape(comparisonDifferenceLine(data, difference)))
					}
				}
				out.WriteString("\n")
			}
		}
		if len(module.Metrics) == 0 && len(module.Changes) == 0 && len(module.MetricIssues) == 0 {
			out.WriteString("_" + markdownEscape(i18n.T("compare.noChanges")) + "_\n\n")
		}
	}

	out.WriteString("## " + i18n.T("report.notices") + "\n\n")
	for _, notice := range data.Notices {
		out.WriteString("- " + markdownEscape(localizeComparisonNotice(notice)) + "\n")
	}
	out.WriteString("\n")
	out.WriteString(fmt.Sprintf("Schema: `%s` · %s: `%s %s`\n", data.SchemaVersion, i18n.T("report.generator"), markdownEscape(data.Tool.Name), markdownEscape(data.Tool.Version)))
	return out.String()
}

func comparisonMarkdownRow(out *strings.Builder, label, value string) {
	out.WriteString("| " + markdownEscape(label) + " | " + value + " |\n")
}

func writeComparisonMarkdownStatus(out *strings.Builder, data comparison.Report, module comparison.Module, layout comparisonLayout) {
	out.WriteString("### " + i18n.T("compare.statusEvidence") + "\n\n")
	if layout == comparisonMany {
		out.WriteString("| " + i18n.T("compare.report") + " | " + i18n.T("report.status") + " | " + i18n.T("report.evidence") + " |\n| --- | --- | --- |\n")
		for index := range data.Inputs {
			fmt.Fprintf(out, "| %s | %s | %s |\n",
				markdownEscape(comparisonInputLabel(data, index)),
				comparisonMarkdownStatusValue(module, index),
				comparisonMarkdownEvidenceValue(module, index, true))
		}
		out.WriteString("\n")
		return
	}
	out.WriteString("| " + i18n.T("report.item"))
	for index := range data.Inputs {
		out.WriteString(" | " + markdownEscape(comparisonInputLabel(data, index)))
	}
	out.WriteString(" |\n| ---")
	for range data.Inputs {
		out.WriteString(" | ---")
	}
	out.WriteString(" |\n| " + i18n.T("report.status"))
	for index := range data.Inputs {
		out.WriteString(" | " + comparisonMarkdownStatusValue(module, index))
	}
	out.WriteString(" |\n| " + i18n.T("report.evidence"))
	for index := range data.Inputs {
		out.WriteString(" | " + comparisonMarkdownEvidenceValue(module, index, false))
	}
	out.WriteString(" |\n\n")
}

func writeComparisonMarkdownMetrics(out *strings.Builder, data comparison.Report, metrics []comparison.Metric, layout comparisonLayout) {
	if layout == comparisonMany {
		for _, metric := range metrics {
			out.WriteString("#### " + markdownEscape(metric.Label) + "\n\n")
			out.WriteString("`" + strings.ReplaceAll(metric.Method, "`", "\\`") + "`")
			if metric.ParameterScope != "" {
				out.WriteString(" · " + markdownEscape(metric.ParameterScope))
			}
			out.WriteString("\n\n")
			out.WriteString("| " + i18n.T("compare.rank") + " | " + i18n.T("compare.report") + " | " + i18n.T("compare.value") + " | " + i18n.T("compare.change") + " | |\n")
			out.WriteString("| ---: | --- | ---: | --- | --- |\n")
			values := append([]comparison.MetricValue(nil), metric.Values...)
			sort.SliceStable(values, func(left, right int) bool {
				if !values[left].Available {
					return false
				}
				if !values[right].Available {
					return true
				}
				return values[left].Rank < values[right].Rank
			})
			for _, value := range values {
				rank := "—"
				if value.Available {
					rank = fmt.Sprintf("%d", value.Rank)
				}
				fmt.Fprintf(out, "| %s | %s | %s | %s | `%s` |\n",
					rank, markdownEscape(comparisonInputLabel(data, value.Report)),
					comparisonMarkdownMetricDisplay(metric, value),
					comparisonMarkdownChange(value, value.Report == data.Reference),
					termcolor.Palette{Level: termcolor.LevelNone}.Bar(value.QualityRatio, 18))
			}
			out.WriteString("\n")
		}
		return
	}
	out.WriteString("| " + i18n.T("report.metric"))
	for index := range data.Inputs {
		out.WriteString(" | " + markdownEscape(comparisonInputLabel(data, index)))
	}
	if layout == comparisonPair {
		out.WriteString(" | " + i18n.T("compare.change"))
	}
	out.WriteString(" |\n| ---")
	for range data.Inputs {
		out.WriteString(" | ---:")
	}
	if layout == comparisonPair {
		out.WriteString(" | ---")
	}
	out.WriteString(" |\n")
	for _, metric := range metrics {
		metricCell := markdownEscape(metric.Label) + "<br><sub><code>" + markdownEscape(metric.Method) + "</code></sub>"
		out.WriteString("| " + metricCell)
		for index := range data.Inputs {
			value := comparison.MetricValue{Report: index}
			if index < len(metric.Values) {
				value = metric.Values[index]
			}
			barWidth := 10
			if layout == comparisonMatrix {
				barWidth = 6
			}
			out.WriteString(" | " + comparisonMarkdownMetricDisplay(metric, value) + "<br>`" + termcolor.Palette{Level: termcolor.LevelNone}.Bar(value.QualityRatio, barWidth) + "`")
		}
		if layout == comparisonPair {
			candidateIndex := 1
			if data.Reference == 1 {
				candidateIndex = 0
			}
			candidate := comparison.MetricValue{Report: candidateIndex}
			if candidateIndex < len(metric.Values) {
				candidate = metric.Values[candidateIndex]
			}
			out.WriteString(" | " + comparisonMarkdownChange(candidate, false))
		}
		out.WriteString(" |\n")
	}
	out.WriteString("\n")
}

func writeComparisonMarkdownObservations(out *strings.Builder, data comparison.Report, observations []comparison.Observation, layout comparisonLayout) {
	if layout != comparisonMany {
		out.WriteString("| " + i18n.T("report.field"))
		for index := range data.Inputs {
			out.WriteString(" | " + markdownEscape(comparisonInputLabel(data, index)))
		}
		out.WriteString(" |\n| ---")
		for range data.Inputs {
			out.WriteString(" | ---")
		}
		out.WriteString(" |\n")
		for _, observation := range observations {
			out.WriteString("| " + markdownEscape(observation.Label))
			for index := range data.Inputs {
				value := "—"
				if index < len(observation.Values) && observation.Values[index].Available {
					value = observation.Values[index].Value
				}
				out.WriteString(" | " + markdownEscape(value))
			}
			out.WriteString(" |\n")
		}
		out.WriteString("\n")
		return
	}
	for _, observation := range observations {
		out.WriteString("#### " + markdownEscape(observation.Label) + "\n\n")
		out.WriteString("| " + i18n.T("compare.report") + " | " + i18n.T("compare.value") + " |\n| --- | --- |\n")
		for _, value := range observation.Values {
			display := "—"
			if value.Available {
				display = value.Value
			}
			fmt.Fprintf(out, "| %s | %s |\n", markdownEscape(comparisonInputLabel(data, value.Report)), markdownEscape(display))
		}
		out.WriteString("\n")
	}
}

func comparisonMarkdownMetricDisplay(metric comparison.Metric, value comparison.MetricValue) string {
	if !value.Available {
		return "—"
	}
	display := markdownEscape(comparisonMetricDisplay(metric, value))
	switch {
	case value.Best:
		return "**★ " + display + "**"
	case value.Outcome == comparison.OutcomeImproved:
		return "▲ " + display
	case value.Outcome == comparison.OutcomeRegressed:
		return "▼ " + display
	case value.Worst:
		return "▽ " + display
	default:
		return display
	}
}

func comparisonMarkdownChange(value comparison.MetricValue, reference bool) string {
	display := markdownEscape(comparisonChange(value, reference))
	switch value.Outcome {
	case comparison.OutcomeImproved:
		return "**▲ " + display + "**"
	case comparison.OutcomeRegressed:
		return "**▼ " + display + "**"
	case comparison.OutcomeUnchanged:
		if reference {
			return "◆ " + display
		}
		return "= " + display
	default:
		return display
	}
}

func comparisonMarkdownStatusValue(module comparison.Module, index int) string {
	if index < 0 || index >= len(module.Statuses) || !module.Statuses[index].Available {
		return "—"
	}
	status := module.Statuses[index].Status
	return statusIcon(status) + " " + markdownEscape(statusLabel(status))
}

func comparisonMarkdownEvidenceValue(module comparison.Module, index int, withBar bool) string {
	if index < 0 || index >= len(module.Evidence) || !module.Evidence[index].Available {
		return "—"
	}
	evidence := module.Evidence[index]
	display := markdownEscape(fmt.Sprintf("%d/%d %s", evidence.Valid, evidence.Expected, comparisonEvidenceGrade(evidence.Grade)))
	if withBar {
		display += "<br>`" + termcolor.Palette{Level: termcolor.LevelNone}.Bar(evidence.Ratio, 12) + "`"
	}
	if evidence.Grade == model.EvidenceInsufficient {
		return "**! " + display + "**"
	}
	return display
}
