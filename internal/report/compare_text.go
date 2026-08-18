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

const comparisonBarWidth = 12

// ComparisonText renders a comparison using the same semantic palette,
// Unicode density bars and width-aware table implementation as the normal txt
// report.  The layout grows from a paired table to a matrix, then to ranked
// vertical blocks when column count would stop being readable.
func ComparisonText(data comparison.Report, color termcolor.Level) string {
	data = terminalSafeCopy(data)
	r := &comparisonTextRenderer{
		textRenderer: textRenderer{palette: termcolor.Palette{Level: color}, width: textWidth},
		data:         data,
		layout:       comparisonLayoutFor(len(data.Inputs)),
	}
	return r.render()
}

type comparisonTextRenderer struct {
	textRenderer
	data   comparison.Report
	layout comparisonLayout
}

func (r *comparisonTextRenderer) render() string {
	r.headerBlock()
	r.summaryBlock()
	r.inputBlock()
	for _, module := range r.data.Modules {
		r.moduleBlock(module)
	}
	r.noticeBlock()
	return r.out.String()
}

func (r *comparisonTextRenderer) headerBlock() {
	r.line(r.palette.Dim(strings.Repeat("#", textWidth)))
	r.centeredStyled(i18n.T("compare.title"), r.palette.AccentBold)
	r.centeredStyled(i18n.T("compare.subtitle"), r.palette.Info)
	r.centeredStyled(
		i18n.T("compare.generatedAt")+i18n.T("punct.colon")+r.data.GeneratedAt.Format("2006-01-02 15:04:05 MST"),
		r.palette.Dim,
	)
	if r.data.Reference >= 0 && r.data.Reference < len(r.data.Inputs) {
		r.centeredStyled(
			i18n.T("compare.reference")+i18n.T("punct.colon")+r.data.Inputs[r.data.Reference].Label,
			r.palette.InfoBold,
		)
	}
	r.line(r.palette.Dim(strings.Repeat("#", textWidth)))
	r.blank()
}

func (r *comparisonTextRenderer) summaryBlock() {
	r.prefaceTitle(i18n.T("report.overview"))
	rows := [][]string{
		{i18n.T("compare.comparability"), r.styleComparability(r.data.Summary.Comparability)},
		{i18n.T("compare.reports"), fmt.Sprintf("%d", r.data.Summary.Reports)},
		{i18n.T("compare.modules"), fmt.Sprintf("%d", r.data.Summary.Modules)},
		{i18n.T("compare.metrics"), fmt.Sprintf("%d", r.data.Summary.ComparableMetrics)},
		{i18n.T("compare.improved"), r.palette.SuccessBold(fmt.Sprintf("▲ %d", r.data.Summary.Improved))},
		{i18n.T("compare.regressed"), r.palette.ErrorBold(fmt.Sprintf("▼ %d", r.data.Summary.Regressed))},
		{i18n.T("compare.unchanged"), fmt.Sprintf("= %d", r.data.Summary.Unchanged)},
		{i18n.T("compare.metricIssues"), r.palette.Warning(fmt.Sprintf("%d", r.data.Summary.MetricIssues))},
		{i18n.T("compare.observedChanges"), fmt.Sprintf("%d", r.data.Summary.ObservedChanges)},
		{i18n.T("compare.statusChanges"), fmt.Sprintf("%d", r.data.Summary.StatusChanges)},
		{i18n.T("compare.evidenceChanges"), fmt.Sprintf("%d", r.data.Summary.EvidenceChanges)},
	}
	// 跨版本时才占一行。同版本是常态，为常态加一行只会稀释真正的信息。
	if schemas := r.data.SchemaVersions(); len(schemas) > 1 {
		rows = append(rows, []string{
			i18n.T("compare.schemaVersions"),
			r.palette.Warning(strings.Join(schemas, " · ")),
		})
	}
	r.table([]string{i18n.T("report.item"), i18n.T("report.content")}, rows, map[int]bool{1: true})
	r.indentedStyled(i18n.T("compare.legend"), r.palette.Dim)
	r.blank()
}

func (r *comparisonTextRenderer) inputBlock() {
	r.prefaceTitle(i18n.T("compare.inputReports"))
	rows := make([][]string, 0, len(r.data.Inputs))
	for _, input := range r.data.Inputs {
		label := comparisonInputLabel(r.data, input.Index)
		if input.Index == r.data.Reference {
			label = r.palette.InfoBold("◆ " + label)
		}
		started := "—"
		if !input.StartedAt.IsZero() {
			started = input.StartedAt.Format("2006-01-02 15:04 MST")
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", input.Index+1), label, fallbackReport(input.Profile, "—"),
			fallbackReport(input.ToolVersion, "—"), started, fallbackReport(input.ReportID, "—"),
		})
	}
	r.table([]string{"#", i18n.T("compare.report"), i18n.T("report.profile"), i18n.T("report.version"), i18n.T("report.startedAt"), i18n.T("report.reportID")}, rows, map[int]bool{0: true})
	r.blank()
}

func (r *comparisonTextRenderer) moduleBlock(module comparison.Module) {
	r.sectionTitle(comparisonModuleTitle(module), comparisonLabel(string(module.Comparability)))
	r.statusEvidence(module)
	if len(module.Metrics) > 0 {
		r.subsection(i18n.T("compare.performance"))
		r.parameterScopes(module.Metrics)
		r.metrics(module.Metrics)
	}
	if len(module.Changes) > 0 {
		r.subsection(i18n.T("compare.discreteChanges"))
		r.observations(module.Changes)
	}
	if len(module.MetricIssues) > 0 {
		r.subsection(i18n.T("compare.methodIssues"))
		r.issues(module.MetricIssues)
	}
	if len(module.Metrics) == 0 && len(module.Changes) == 0 && len(module.MetricIssues) == 0 {
		r.indentedStyled(i18n.T("compare.noChanges"), r.palette.Dim)
	}
	r.blank()
}

func (r *comparisonTextRenderer) parameterScopes(metrics []comparison.Metric) {
	seen := make(map[string]bool)
	for _, metric := range metrics {
		scope := strings.TrimSpace(metric.ParameterScope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		r.indentedStyled(i18n.T("compare.parameters")+i18n.T("punct.colon")+scope, r.palette.Dim)
	}
	if len(seen) > 0 {
		r.blank()
	}
}

func (r *comparisonTextRenderer) statusEvidence(module comparison.Module) {
	if r.layout == comparisonMany {
		rows := make([][]string, 0, len(r.data.Inputs))
		for index := range r.data.Inputs {
			rows = append(rows, []string{
				comparisonInputLabel(r.data, index),
				r.statusValue(module, index),
				r.evidenceValue(module, index, true),
			})
		}
		r.table([]string{i18n.T("compare.report"), i18n.T("report.status"), i18n.T("report.evidence")}, rows, nil)
		r.blank()
		return
	}
	columns := []string{i18n.T("compare.statusEvidence")}
	statusRow := []string{i18n.T("report.status")}
	evidenceRow := []string{i18n.T("report.evidence")}
	for index := range r.data.Inputs {
		columns = append(columns, comparisonInputLabel(r.data, index))
		statusRow = append(statusRow, r.statusValue(module, index))
		evidenceRow = append(evidenceRow, r.evidenceValue(module, index, false))
	}
	r.table(columns, [][]string{statusRow, evidenceRow}, nil)
	r.blank()
}

func (r *comparisonTextRenderer) metrics(metrics []comparison.Metric) {
	switch r.layout {
	case comparisonPair:
		r.pairMetrics(metrics)
	case comparisonMatrix:
		r.matrixMetrics(metrics)
	default:
		r.manyMetrics(metrics)
	}
}

func (r *comparisonTextRenderer) pairMetrics(metrics []comparison.Metric) {
	columns := []string{i18n.T("report.metric")}
	for index := range r.data.Inputs {
		columns = append(columns, comparisonInputLabel(r.data, index))
	}
	columns = append(columns, i18n.T("compare.change"))
	rows := make([][]string, 0, len(metrics))
	for _, metric := range metrics {
		row := []string{metric.Label + " · " + metric.Method}
		for index := range r.data.Inputs {
			row = append(row, r.metricValue(metric, index, comparisonBarWidth))
		}
		candidateIndex := 1
		if r.data.Reference == 1 {
			candidateIndex = 0
		}
		candidate := comparison.MetricValue{Report: candidateIndex}
		if candidateIndex < len(metric.Values) {
			candidate = metric.Values[candidateIndex]
		}
		row = append(row, r.styleChange(candidate, false))
		rows = append(rows, row)
	}
	r.table(columns, rows, nil)
	r.blank()
}

func (r *comparisonTextRenderer) matrixMetrics(metrics []comparison.Metric) {
	columns := []string{i18n.T("report.metric")}
	for index := range r.data.Inputs {
		columns = append(columns, comparisonInputLabel(r.data, index))
	}
	barWidth := 7
	if len(r.data.Inputs) >= 5 {
		barWidth = 5
	}
	rows := make([][]string, 0, len(metrics))
	for _, metric := range metrics {
		row := []string{metric.Label + " · " + metric.Method}
		for index := range r.data.Inputs {
			row = append(row, r.metricValue(metric, index, barWidth))
		}
		rows = append(rows, row)
	}
	r.table(columns, rows, nil)
	r.blank()
}

func (r *comparisonTextRenderer) manyMetrics(metrics []comparison.Metric) {
	for _, metric := range metrics {
		r.indentedStyled(metric.Label+" · "+metric.Method, r.palette.LabelBold)
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
		rows := make([][]string, 0, len(values))
		for _, value := range values {
			rank := "—"
			if value.Available {
				rank = fmt.Sprintf("%d", value.Rank)
			}
			rows = append(rows, []string{
				rank,
				comparisonInputLabel(r.data, value.Report),
				r.styleMetricDisplay(metric, value),
				r.styleChange(value, value.Report == r.data.Reference),
				r.metricBar(value, 18),
			})
		}
		r.table([]string{i18n.T("compare.rank"), i18n.T("compare.report"), i18n.T("compare.value"), i18n.T("compare.change"), ""}, rows, map[int]bool{0: true, 2: true})
		r.blank()
	}
}

func (r *comparisonTextRenderer) observations(observations []comparison.Observation) {
	if r.layout != comparisonMany {
		columns := []string{i18n.T("report.field")}
		for index := range r.data.Inputs {
			columns = append(columns, comparisonInputLabel(r.data, index))
		}
		rows := make([][]string, 0, len(observations))
		for _, observation := range observations {
			row := []string{observation.Label}
			for index := range r.data.Inputs {
				row = append(row, r.observationValue(observation, index))
			}
			rows = append(rows, row)
		}
		r.table(columns, rows, nil)
		r.blank()
		return
	}
	for _, observation := range observations {
		r.indented(observation.Label, true)
		rows := make([][]string, 0, len(observation.Values))
		for _, value := range observation.Values {
			display := "—"
			if value.Available {
				display = value.Value
			}
			rows = append(rows, []string{comparisonInputLabel(r.data, value.Report), display})
		}
		r.table([]string{i18n.T("compare.report"), i18n.T("compare.value")}, rows, nil)
	}
	r.blank()
}

func (r *comparisonTextRenderer) issues(issues []comparison.MetricIssue) {
	rows := make([][]string, 0, len(issues))
	for _, issue := range issues {
		labels := make([]string, 0, len(issue.Reports))
		for _, index := range issue.Reports {
			labels = append(labels, comparisonInput(r.data, index).Label)
		}
		rows = append(rows, []string{
			fallbackReport(issue.Label, issue.Key),
			r.palette.Warning(comparisonIssueLabel(issue.Reason)),
			strings.Join(labels, ", "),
		})
	}
	r.table([]string{i18n.T("report.metric"), i18n.T("compare.methodIssues"), i18n.T("compare.reports")}, rows, nil)
	r.blank()

	// 原因码只说"口径不一致"，不说不一致在哪。这一段把差异逐项列出来，报告标签
	// 后带上产出它的 ecs 版本——"哪个版本用的哪个 method"是用户撞上这条时真正
	// 要问的问题。差异相同的指标合并成一组，避免模块级参数被逐指标刷屏。
	groups := comparisonDifferenceGroups(issues)
	if len(groups) == 0 {
		return
	}
	r.indented(i18n.T("compare.differences"), true)
	for _, group := range groups {
		r.indentedStyled("  "+strings.Join(group.Metrics, i18n.T("punct.listSep")), r.palette.LabelBold)
		for _, difference := range group.Differences {
			r.indentedStyled("    "+comparisonDifferenceLabel(difference.Field)+
				i18n.T("punct.colon")+comparisonDifferenceLine(r.data, difference), r.palette.Dim)
		}
	}
	r.blank()
}

func (r *comparisonTextRenderer) noticeBlock() {
	r.prefaceTitle(i18n.T("report.notices"))
	for _, notice := range r.data.Notices {
		r.note(notice)
	}
	r.blank()
	r.line(r.palette.Dim(fmt.Sprintf("Schema: %s · %s: %s %s", r.data.SchemaVersion, i18n.T("report.generator"), r.data.Tool.Name, r.data.Tool.Version)))
}

func (r *comparisonTextRenderer) metricValue(metric comparison.Metric, index, width int) string {
	if index < 0 || index >= len(metric.Values) {
		return r.palette.Dim("— " + r.palette.Bar(0, width))
	}
	value := metric.Values[index]
	return r.styleMetricDisplay(metric, value) + " " + r.metricBar(value, width)
}

func (r *comparisonTextRenderer) styleMetricDisplay(metric comparison.Metric, value comparison.MetricValue) string {
	if !value.Available {
		return r.palette.Dim("—")
	}
	display := comparisonMetricDisplay(metric, value)
	switch {
	case value.Best:
		return r.palette.SuccessBold("★ " + display)
	case value.Outcome == comparison.OutcomeRegressed:
		return r.palette.Error("▼ " + display)
	case value.Outcome == comparison.OutcomeImproved:
		return r.palette.Success("▲ " + display)
	case value.Worst:
		return r.palette.Warning("▽ " + display)
	default:
		return display
	}
}

func (r *comparisonTextRenderer) metricBar(value comparison.MetricValue, width int) string {
	if !value.Available {
		return r.palette.Bar(0, width)
	}
	return r.palette.Bar(value.QualityRatio, width)
}

func (r *comparisonTextRenderer) styleChange(value comparison.MetricValue, reference bool) string {
	display := comparisonChange(value, reference)
	switch value.Outcome {
	case comparison.OutcomeImproved:
		return r.palette.SuccessBold("▲ " + display)
	case comparison.OutcomeRegressed:
		return r.palette.ErrorBold("▼ " + display)
	case comparison.OutcomeUnchanged:
		if reference {
			return r.palette.Info("◆ " + display)
		}
		return "= " + display
	default:
		return r.palette.Dim(display)
	}
}

func (r *comparisonTextRenderer) statusValue(module comparison.Module, index int) string {
	if index < 0 || index >= len(module.Statuses) || !module.Statuses[index].Available {
		return r.palette.Dim("—")
	}
	status := module.Statuses[index].Status
	display := statusIcon(status) + " " + statusLabel(status)
	switch status {
	case model.StatusOK:
		return r.palette.Success(display)
	case model.StatusWarning:
		return r.palette.Warning(display)
	case model.StatusError:
		return r.palette.Error(display)
	default:
		return r.palette.Dim(display)
	}
}

func (r *comparisonTextRenderer) evidenceValue(module comparison.Module, index int, withBar bool) string {
	if index < 0 || index >= len(module.Evidence) || !module.Evidence[index].Available {
		return r.palette.Dim("—")
	}
	evidence := module.Evidence[index]
	display := fmt.Sprintf("%d/%d %s", evidence.Valid, evidence.Expected, comparisonEvidenceGrade(evidence.Grade))
	if withBar {
		display += " " + r.palette.Bar(evidence.Ratio, 12)
	}
	switch evidence.Grade {
	case model.EvidenceComplete:
		return r.palette.Success(display)
	case model.EvidencePartial:
		return r.palette.Warning(display)
	case model.EvidenceInsufficient:
		return r.palette.Error(display)
	default:
		return r.palette.Dim(display)
	}
}

func (r *comparisonTextRenderer) observationValue(observation comparison.Observation, index int) string {
	if index < 0 || index >= len(observation.Values) || !observation.Values[index].Available {
		return r.palette.Dim("—")
	}
	return observation.Values[index].Value
}

func (r *comparisonTextRenderer) styleComparability(value comparison.Comparability) string {
	display := comparisonLabel(string(value))
	switch value {
	case comparison.Comparable:
		return r.palette.SuccessBold(display)
	case comparison.PartiallyComparable:
		return r.palette.WarningBold(display)
	default:
		return r.palette.ErrorBold(display)
	}
}

func comparisonEvidenceGrade(grade model.EvidenceGrade) string {
	key := "evidence." + string(grade)
	if grade == model.EvidenceNotPlanned {
		key = "evidence.notPlanned"
	}
	translated := i18n.T(key)
	if translated == key {
		return string(grade)
	}
	return translated
}
