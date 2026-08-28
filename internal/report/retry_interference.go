package report

import (
	"strconv"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

// reportInterferenceView is a short-lived presentation view. It deliberately
// contains strings, not model fields, so localization cannot write back into
// the canonical report or create a second serialized representation.
type reportInterferenceView struct {
	Title        string
	ScoreLabel   string
	Score        string
	Measurements []reportInterferenceMeasurementView
	ReasonsTitle string
	Reasons      []string
}

type reportInterferenceMeasurementView struct {
	Label  string
	Value  string
	Method string
}

type reportRetryView struct {
	Title               string
	SelectionRuleLabel  string
	SelectionRule       string
	TriggerReasonsLabel string
	TriggerReasons      []string
	AttemptsLabel       string
	Attempts            []reportRetryAttemptView
	Compact             string
}

type reportRetryAttemptView struct {
	Number    int
	Status    string
	Evidence  string
	Score     string
	Reasons   string
	Selection string
	Selected  bool
}

func interferencePresentation(result model.Result) *reportInterferenceView {
	if result.Interference == nil {
		return nil
	}
	view := &reportInterferenceView{
		Title:        i18n.T("report.interference.title"),
		ScoreLabel:   i18n.T("report.interference.score"),
		Score:        strconv.Itoa(result.Interference.Score),
		ReasonsTitle: i18n.T("report.interference.reasons"),
		Measurements: make([]reportInterferenceMeasurementView, 0, len(result.Interference.Measurements)),
		Reasons:      make([]string, 0, len(result.Interference.Reasons)),
	}
	for _, rawMeasurement := range result.Interference.Measurements {
		measurement := displayMeasurement(rawMeasurement)
		value := displayValue(measurement.Display)
		if value == "" {
			value = formatFloat(measurement.Value)
			if measurement.Unit != "" {
				value += " " + measurement.Unit
			}
		}
		view.Measurements = append(view.Measurements, reportInterferenceMeasurementView{
			Label:  fallbackReport(measurement.Label, i18n.T("value.none")),
			Value:  fallbackReport(value, i18n.T("value.none")),
			Method: fallbackReport(measurement.Method, i18n.T("value.none")),
		})
	}
	for _, reason := range result.Interference.Reasons {
		if rendered := renderMessage(reason); rendered != "" {
			view.Reasons = append(view.Reasons, rendered)
		}
	}
	if len(view.Reasons) == 0 {
		if result.Interference.Detected {
			view.Reasons = []string{i18n.T("report.interference.noReasons")}
		} else {
			view.Reasons = []string{i18n.T("report.interference.none")}
		}
	}
	return view
}

func retryPresentation(result model.Result) *reportRetryView {
	if result.Retry == nil {
		return nil
	}
	view := &reportRetryView{
		Title:               i18n.T("report.retry.title"),
		SelectionRuleLabel:  i18n.T("report.retry.selectionRule"),
		SelectionRule:       fallbackReport(renderMessage(result.Retry.SelectionRule), i18n.T("value.none")),
		TriggerReasonsLabel: i18n.T("report.retry.triggerReasons"),
		AttemptsLabel:       i18n.T("report.retry.attempts"),
		TriggerReasons:      make([]string, 0, len(result.Retry.TriggerReasons)),
		Attempts:            make([]reportRetryAttemptView, 0, len(result.Retry.Attempts)),
	}
	for _, reason := range result.Retry.TriggerReasons {
		if rendered := renderMessage(reason); rendered != "" {
			view.TriggerReasons = append(view.TriggerReasons, rendered)
		}
	}
	if len(view.TriggerReasons) == 0 {
		view.TriggerReasons = []string{i18n.T("value.none")}
	}
	for _, attempt := range result.Retry.Attempts {
		reasons := make([]string, 0, len(attempt.Interference.Reasons))
		for _, reason := range attempt.Interference.Reasons {
			if rendered := renderMessage(reason); rendered != "" {
				reasons = append(reasons, rendered)
			}
		}
		reasonText := strings.Join(reasons, i18n.T("punct.listSep"))
		if reasonText == "" {
			reasonText = i18n.T("value.none")
		}
		evidence := i18n.T("value.none")
		if attempt.Evidence != nil {
			evidence = evidenceText(*attempt.Evidence)
		}
		selected := attempt.Number == result.Retry.SelectedAttempt
		selection := i18n.T("report.retry.retained")
		if selected {
			selection = i18n.T("report.retry.selected")
		}
		view.Attempts = append(view.Attempts, reportRetryAttemptView{
			Number:    attempt.Number,
			Status:    statusLabel(attempt.Status),
			Evidence:  evidence,
			Score:     strconv.Itoa(attempt.Interference.Score),
			Reasons:   reasonText,
			Selection: selection,
			Selected:  selected,
		})
	}
	view.Compact = renderMessage(model.NewMessage("report.retry.compact", strconv.Itoa(result.Retry.SelectedAttempt)))
	return view
}

// displayTextBlockTitle localizes the base title first, then adds a localized
// attempt prefix for retry evidence. The input block is never modified.
func displayTextBlockTitle(block model.TextBlock) string {
	title := displayKey(block.Title)
	if title == "" && block.Attempt == 0 {
		return ""
	}
	if title == "" {
		title = i18n.T("report.rawOutput")
	}
	if block.Attempt <= 0 {
		return title
	}
	return renderMessage(model.NewMessage("report.attempt.prefix", strconv.Itoa(block.Attempt), title))
}

func (r *textRenderer) renderRetryInterference(result model.Result) {
	interference := interferencePresentation(result)
	retry := retryPresentation(result)
	if r.compact {
		if retry != nil {
			r.subsection(retry.Title)
			r.indented(retry.Compact)
			r.blank()
			return
		}
		if interference != nil {
			r.subsection(interference.Title)
			if len(interference.Reasons) > 0 {
				r.indented(interference.Reasons[0])
			}
			r.blank()
		}
		return
	}
	if interference != nil {
		r.subsection(interference.Title)
		r.indented(interference.ScoreLabel + i18n.T("punct.colon") + interference.Score)
		if len(interference.Measurements) > 0 {
			rows := make([][]string, 0, len(interference.Measurements))
			for _, measurement := range interference.Measurements {
				rows = append(rows, []string{measurement.Label, measurement.Value, measurement.Method})
			}
			r.table([]string{
				i18n.T("report.interference.metric"),
				i18n.T("report.interference.value"),
				i18n.T("report.interference.method"),
			}, rows, nil)
		}
		r.indentedStyled(interference.ReasonsTitle, r.palette.LabelBold)
		for _, reason := range interference.Reasons {
			r.indented(reason)
		}
		r.blank()
	}
	if retry == nil {
		return
	}
	r.subsection(retry.Title)
	r.indented(retry.SelectionRuleLabel + i18n.T("punct.colon") + retry.SelectionRule)
	r.indentedStyled(retry.TriggerReasonsLabel, r.palette.LabelBold)
	for _, reason := range retry.TriggerReasons {
		r.indented(reason)
	}
	r.indentedStyled(retry.AttemptsLabel, r.palette.LabelBold)
	rows := make([][]string, 0, len(retry.Attempts))
	for _, attempt := range retry.Attempts {
		rows = append(rows, []string{
			strconv.Itoa(attempt.Number), attempt.Status, attempt.Evidence,
			attempt.Score, attempt.Reasons, attempt.Selection,
		})
	}
	if len(rows) > 0 {
		r.table([]string{
			i18n.T("report.retry.attempt"), i18n.T("report.retry.status"),
			i18n.T("report.retry.evidence"), i18n.T("report.retry.score"),
			i18n.T("report.retry.reasons"), i18n.T("report.retry.selection"),
		}, rows, map[int]bool{0: true, 3: true})
	}
	r.blank()
}

func renderRetryInterferenceMarkdown(out *strings.Builder, result model.Result) {
	interference := interferencePresentation(result)
	if interference != nil {
		out.WriteString("### ")
		out.WriteString(markdownEscape(interference.Title))
		out.WriteString("\n\n")
		out.WriteString("**")
		out.WriteString(markdownEscape(interference.ScoreLabel))
		out.WriteString("**")
		out.WriteString(i18n.T("punct.colon"))
		out.WriteString(markdownEscape(interference.Score))
		out.WriteString("\n\n")
		if len(interference.Measurements) > 0 {
			out.WriteString("| ")
			out.WriteString(markdownEscape(i18n.T("report.interference.metric")))
			out.WriteString(" | ")
			out.WriteString(markdownEscape(i18n.T("report.interference.value")))
			out.WriteString(" | ")
			out.WriteString(markdownEscape(i18n.T("report.interference.method")))
			out.WriteString(" |\n| --- | --- | --- |\n")
			for _, measurement := range interference.Measurements {
				out.WriteString("| ")
				out.WriteString(markdownEscape(measurement.Label))
				out.WriteString(" | ")
				out.WriteString(markdownEscape(measurement.Value))
				out.WriteString(" | ")
				out.WriteString(markdownEscape(measurement.Method))
				out.WriteString(" |\n")
			}
			out.WriteString("\n")
		}
		out.WriteString("**")
		out.WriteString(markdownEscape(interference.ReasonsTitle))
		out.WriteString("**")
		out.WriteString(i18n.T("punct.colon"))
		out.WriteString("\n\n")
		for _, reason := range interference.Reasons {
			out.WriteString("- ")
			out.WriteString(markdownEscape(reason))
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}

	retry := retryPresentation(result)
	if retry == nil {
		return
	}
	out.WriteString("### ")
	out.WriteString(markdownEscape(retry.Title))
	out.WriteString("\n\n")
	out.WriteString("**")
	out.WriteString(markdownEscape(retry.SelectionRuleLabel))
	out.WriteString("**")
	out.WriteString(i18n.T("punct.colon"))
	out.WriteString(markdownEscape(retry.SelectionRule))
	out.WriteString("\n\n**")
	out.WriteString(markdownEscape(retry.TriggerReasonsLabel))
	out.WriteString("**")
	out.WriteString(i18n.T("punct.colon"))
	out.WriteString("\n\n")
	for _, reason := range retry.TriggerReasons {
		out.WriteString("- ")
		out.WriteString(markdownEscape(reason))
		out.WriteString("\n")
	}
	out.WriteString("\n")
	out.WriteString("#### ")
	out.WriteString(markdownEscape(retry.AttemptsLabel))
	out.WriteString("\n\n| ")
	columns := []string{
		i18n.T("report.retry.attempt"), i18n.T("report.retry.status"),
		i18n.T("report.retry.evidence"), i18n.T("report.retry.score"),
		i18n.T("report.retry.reasons"), i18n.T("report.retry.selection"),
	}
	for index, column := range columns {
		if index > 0 {
			out.WriteString(" | ")
		}
		out.WriteString(markdownEscape(column))
	}
	out.WriteString(" |\n| --- | --- | --- | ---: | --- | --- |\n")
	for _, attempt := range retry.Attempts {
		values := []string{strconv.Itoa(attempt.Number), attempt.Status, attempt.Evidence, attempt.Score, attempt.Reasons, attempt.Selection}
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
