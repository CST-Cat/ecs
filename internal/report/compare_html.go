package report

import (
	"fmt"
	"html"
	"strings"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

// ComparisonHTML produces a responsive, script-free comparison report.  The
// document chooses paired, matrix or ranked-list CSS at generation time and
// still collapses cleanly on narrow mobile terminals/browsers.
func ComparisonHTML(data comparison.Report) ([]byte, error) {
	var out strings.Builder
	layout := comparisonLayoutFor(len(data.Inputs))
	layoutClass := map[comparisonLayout]string{
		comparisonPair: "layout-pair", comparisonMatrix: "layout-matrix", comparisonMany: "layout-many",
	}[layout]
	lang := "zh-CN"
	if i18n.Current() == i18n.LangEN {
		lang = "en"
	}
	fmt.Fprintf(&out, comparisonHTMLHead, lang, html.EscapeString(i18n.T("compare.title")), layoutClass)

	out.WriteString(`<main><header class="hero">`)
	out.WriteString("<h1>" + html.EscapeString(i18n.T("compare.title")) + "</h1>")
	out.WriteString("<p>" + html.EscapeString(i18n.T("compare.subtitle")) + "</p><div class=\"hero-meta\">")
	writeComparisonHTMLPill(&out, i18n.T("compare.reports"), fmt.Sprintf("%d", data.Summary.Reports))
	writeComparisonHTMLPill(&out, i18n.T("compare.comparability"), comparisonLabel(string(data.Summary.Comparability)))
	if data.Reference >= 0 && data.Reference < len(data.Inputs) {
		writeComparisonHTMLPill(&out, i18n.T("compare.reference"), data.Inputs[data.Reference].Label)
	}
	writeComparisonHTMLPill(&out, i18n.T("compare.generatedAt"), data.GeneratedAt.Format("2006-01-02 15:04:05 MST"))
	out.WriteString("</div></header>")

	out.WriteString(`<div class="summary-grid">`)
	writeComparisonHTMLSummary(&out, fmt.Sprintf("%d", data.Summary.ComparableMetrics), i18n.T("compare.metrics"), "accent")
	writeComparisonHTMLSummary(&out, fmt.Sprintf("▲ %d", data.Summary.Improved), i18n.T("compare.improved"), "good")
	writeComparisonHTMLSummary(&out, fmt.Sprintf("▼ %d", data.Summary.Regressed), i18n.T("compare.regressed"), "bad")
	writeComparisonHTMLSummary(&out, fmt.Sprintf("%d", data.Summary.MetricIssues), i18n.T("compare.metricIssues"), "warn")
	writeComparisonHTMLSummary(&out, fmt.Sprintf("%d", data.Summary.ObservedChanges), i18n.T("compare.observedChanges"), "neutral")
	writeComparisonHTMLSummary(&out, fmt.Sprintf("%d", data.Summary.StatusChanges), i18n.T("compare.statusChanges"), "neutral")
	writeComparisonHTMLSummary(&out, fmt.Sprintf("%d", data.Summary.EvidenceChanges), i18n.T("compare.evidenceChanges"), "neutral")
	out.WriteString("</div>")

	out.WriteString(`<section><div class="section-head"><div><h2>` + html.EscapeString(i18n.T("compare.inputReports")) + `</h2></div></div><div class="input-grid">`)
	for _, input := range data.Inputs {
		class := "input-card"
		mark := ""
		if input.Index == data.Reference {
			class += " reference"
			mark = `<span class="tag reference-tag">◆ ` + html.EscapeString(i18n.T("compare.referenceMark")) + `</span>`
		}
		started := "—"
		if !input.StartedAt.IsZero() {
			started = input.StartedAt.Format("2006-01-02 15:04 MST")
		}
		fmt.Fprintf(&out, `<article class="%s"><div class="input-index">#%d</div><h3>%s</h3>%s<dl>`, class, input.Index+1, html.EscapeString(input.Label), mark)
		writeComparisonHTMLDefinition(&out, i18n.T("report.profile"), fallbackReport(input.Profile, "—"))
		writeComparisonHTMLDefinition(&out, i18n.T("report.version"), fallbackReport(input.ToolVersion, "—"))
		writeComparisonHTMLDefinition(&out, i18n.T("report.startedAt"), started)
		writeComparisonHTMLDefinition(&out, i18n.T("report.reportID"), fallbackReport(input.ReportID, "—"))
		out.WriteString("</dl></article>")
	}
	out.WriteString("</div></section>")

	for _, module := range data.Modules {
		fmt.Fprintf(&out, `<section id="module-%s"><div class="section-head"><div><h2>%s</h2></div><span class="tag comparability %s">%s</span></div>`,
			html.EscapeString(module.ID), html.EscapeString(comparisonModuleTitle(module)), html.EscapeString(string(module.Comparability)), html.EscapeString(comparisonLabel(string(module.Comparability))))
		writeComparisonHTMLStatus(&out, data, module)
		if len(module.Metrics) > 0 {
			out.WriteString("<h3>" + html.EscapeString(i18n.T("compare.performance")) + "</h3>")
			out.WriteString(`<div class="metric-list">`)
			for _, metric := range module.Metrics {
				writeComparisonHTMLMetric(&out, data, metric, layout)
			}
			out.WriteString("</div>")
		}
		if len(module.Changes) > 0 {
			out.WriteString("<h3>" + html.EscapeString(i18n.T("compare.discreteChanges")) + "</h3>")
			for _, observation := range module.Changes {
				out.WriteString(`<article class="observation-card"><div class="observation-title">` + html.EscapeString(observation.Label) + `</div><div class="observation-values">`)
				for _, value := range observation.Values {
					display := "—"
					if value.Available {
						display = value.Value
					}
					fmt.Fprintf(&out, `<div><span>%s</span><strong>%s</strong></div>`, html.EscapeString(comparisonInputLabel(data, value.Report)), html.EscapeString(display))
				}
				out.WriteString("</div></article>")
			}
		}
		if len(module.MetricIssues) > 0 {
			out.WriteString("<h3>" + html.EscapeString(i18n.T("compare.methodIssues")) + "</h3>")
			out.WriteString(`<div class="table-wrap"><table><thead><tr><th>` + html.EscapeString(i18n.T("report.metric")) + `</th><th>` + html.EscapeString(i18n.T("compare.methodIssues")) + `</th><th>` + html.EscapeString(i18n.T("compare.reports")) + `</th></tr></thead><tbody>`)
			for _, issue := range module.MetricIssues {
				labels := make([]string, 0, len(issue.Reports))
				for _, index := range issue.Reports {
					labels = append(labels, comparisonInput(data, index).Label)
				}
				fmt.Fprintf(&out, `<tr><td>%s</td><td class="warn">⚠ %s</td><td>%s</td></tr>`,
					html.EscapeString(fallbackReport(issue.Label, issue.Key)), html.EscapeString(comparisonIssueLabel(issue.Reason)), html.EscapeString(strings.Join(labels, ", ")))
			}
			out.WriteString("</tbody></table></div>")
		}
		if len(module.Metrics) == 0 && len(module.Changes) == 0 && len(module.MetricIssues) == 0 {
			out.WriteString(`<p class="muted">` + html.EscapeString(i18n.T("compare.noChanges")) + `</p>`)
		}
		out.WriteString("</section>")
	}

	out.WriteString(`<footer><p>`)
	for index, notice := range data.Notices {
		if index > 0 {
			out.WriteString(" · ")
		}
		out.WriteString(html.EscapeString(notice))
	}
	fmt.Fprintf(&out, `</p><p>Schema: %s · %s: %s %s</p></footer></main></body></html>`,
		html.EscapeString(data.SchemaVersion), html.EscapeString(i18n.T("report.generator")), html.EscapeString(data.Tool.Name), html.EscapeString(data.Tool.Version))
	return []byte(out.String()), nil
}

func writeComparisonHTMLPill(out *strings.Builder, label, value string) {
	fmt.Fprintf(out, `<span class="pill"><b>%s</b>&nbsp; %s</span>`, html.EscapeString(label), html.EscapeString(value))
}

func writeComparisonHTMLSummary(out *strings.Builder, value, label, class string) {
	fmt.Fprintf(out, `<div class="summary-card"><strong class="%s">%s</strong><span>%s</span></div>`, class, html.EscapeString(value), html.EscapeString(label))
}

func writeComparisonHTMLDefinition(out *strings.Builder, label, value string) {
	fmt.Fprintf(out, `<div><dt>%s</dt><dd>%s</dd></div>`, html.EscapeString(label), html.EscapeString(value))
}

func writeComparisonHTMLStatus(out *strings.Builder, data comparison.Report, module comparison.Module) {
	out.WriteString(`<h3>` + html.EscapeString(i18n.T("compare.statusEvidence")) + `</h3><div class="status-grid">`)
	for index := range data.Inputs {
		statusText, statusClass := "—", "muted"
		if index < len(module.Statuses) && module.Statuses[index].Available {
			status := module.Statuses[index].Status
			statusText = statusIcon(status) + " " + statusLabel(status)
			statusClass = string(status)
		}
		evidenceText := "—"
		evidenceRatio := 0.0
		evidenceClass := "muted"
		if index < len(module.Evidence) && module.Evidence[index].Available {
			evidence := module.Evidence[index]
			evidenceText = fmt.Sprintf("%d/%d · %s", evidence.Valid, evidence.Expected, comparisonEvidenceGrade(evidence.Grade))
			evidenceRatio = evidence.Ratio
			evidenceClass = comparisonEvidenceClass(evidence.Grade)
		}
		fmt.Fprintf(out, `<article class="status-card"><div class="report-name">%s</div><strong class="%s">%s</strong><div class="evidence-line %s">%s</div>%s</article>`,
			html.EscapeString(comparisonInputLabel(data, index)), html.EscapeString(statusClass), html.EscapeString(statusText),
			evidenceClass, html.EscapeString(evidenceText), comparisonHTMLBar(evidenceRatio))
	}
	out.WriteString("</div>")
}

func writeComparisonHTMLMetric(out *strings.Builder, data comparison.Report, metric comparison.Metric, layout comparisonLayout) {
	direction := "↓"
	if metric.HigherIsBetter {
		direction = "↑"
	}
	fmt.Fprintf(out, `<article class="metric-card"><div class="metric-head"><div><div class="metric-title">%s</div><div class="method"><code>%s</code>%s</div></div><span class="direction">%s</span></div><div class="metric-values">`,
		html.EscapeString(metric.Label), html.EscapeString(metric.Method), comparisonHTMLParameterScope(metric.ParameterScope), direction)
	values := append([]comparison.MetricValue(nil), metric.Values...)
	if layout == comparisonMany {
		sortComparisonValues(values)
	}
	for _, value := range values {
		class := comparisonHTMLMetricClass(value)
		star := ""
		if value.Best {
			star = "★ "
		}
		rank := "—"
		if value.Available {
			rank = fmt.Sprintf("#%d", value.Rank)
		}
		fmt.Fprintf(out, `<div class="value-card %s"><div class="value-meta"><span class="report-name">%s</span><span class="rank">%s</span></div><div class="number">%s%s</div><div class="delta">%s</div>%s</div>`,
			class, html.EscapeString(comparisonInputLabel(data, value.Report)), html.EscapeString(rank), star,
			html.EscapeString(comparisonMetricDisplay(metric, value)), html.EscapeString(comparisonChange(value, value.Report == data.Reference)), comparisonHTMLBar(value.QualityRatio))
	}
	out.WriteString("</div></article>")
}

func sortComparisonValues(values []comparison.MetricValue) {
	// Stable insertion sort keeps missing values in input order at the bottom
	// and avoids importing a second ordering abstraction into the renderer.
	for index := 1; index < len(values); index++ {
		current := values[index]
		position := index
		for position > 0 && comparisonValueBefore(current, values[position-1]) {
			values[position] = values[position-1]
			position--
		}
		values[position] = current
	}
}

func comparisonValueBefore(left, right comparison.MetricValue) bool {
	if left.Available != right.Available {
		return left.Available
	}
	if !left.Available {
		return false
	}
	return left.Rank < right.Rank
}

func comparisonHTMLParameterScope(scope string) string {
	if scope == "" {
		return ""
	}
	return ` <span class="scope">· ` + html.EscapeString(scope) + `</span>`
}

func comparisonHTMLMetricClass(value comparison.MetricValue) string {
	classes := []string{}
	if !value.Available {
		classes = append(classes, "missing")
	}
	if value.Best {
		classes = append(classes, "best")
	}
	if value.Worst {
		classes = append(classes, "worst")
	}
	if value.Outcome != "" {
		classes = append(classes, string(value.Outcome))
	}
	return strings.Join(classes, " ")
}

func comparisonEvidenceClass(grade model.EvidenceGrade) string {
	switch grade {
	case model.EvidenceComplete:
		return "good"
	case model.EvidencePartial:
		return "warn"
	case model.EvidenceInsufficient:
		return "bad"
	default:
		return "muted"
	}
}

func comparisonHTMLBar(ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	color := termcolor.Color(ratio)
	return fmt.Sprintf(`<div class="bar" aria-hidden="true"><i style="width:%.1f%%;background:#%02x%02x%02x"></i></div>`, ratio*100, color.R, color.G, color.B)
}

const comparisonHTMLHead = `<!doctype html>
<html lang="%s">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <meta name="referrer" content="no-referrer">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
  <title>%s</title>
  <style>
    :root { --bg:#f4f6fb;--panel:#fff;--panel-soft:#f8faff;--text:#182033;--muted:#65708a;--line:#dfe4ee;--accent:#315efb;--good:#087f4d;--warn:#a75f00;--bad:#bf2938;--shadow:0 10px 30px rgba(25,40,75,.08); }
    @media (prefers-color-scheme:dark) { :root { --bg:#0d1220;--panel:#151c2d;--panel-soft:#111827;--text:#edf1fb;--muted:#a0abc1;--line:#2a344a;--accent:#84a2ff;--good:#57d49b;--warn:#ffbd62;--bad:#ff7d89;--shadow:none; } }
    * { box-sizing:border-box; }
    body { margin:0;background:var(--bg);color:var(--text);font:15px/1.55 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    main { width:min(1320px,calc(100%% - 32px));margin:32px auto 64px; }
    .hero { background:linear-gradient(135deg,#243b8f,#315efb 60%%,#5d85ff);color:#fff;border-radius:22px;padding:32px;box-shadow:var(--shadow); }
    .hero h1 { margin:0 0 5px;font-size:clamp(27px,5vw,43px);letter-spacing:-.03em; }
    .hero p { margin:0;opacity:.9; }
    .hero-meta { display:flex;flex-wrap:wrap;gap:9px;margin-top:22px; }
    .pill,.tag { display:inline-flex;align-items:center;border-radius:999px;padding:5px 10px;font-size:12px; }
    .pill { background:rgba(255,255,255,.15); }
    .summary-grid { display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:13px;margin:18px 0; }
    .summary-card,section { background:var(--panel);border:1px solid var(--line);box-shadow:var(--shadow); }
    .summary-card { border-radius:16px;padding:17px; }
    .summary-card strong { display:block;font-size:26px;line-height:1.15; }
    .summary-card span { color:var(--muted);font-size:12px; }
    section { margin-top:18px;padding:24px;border-radius:18px; }
    h2 { margin:0;font-size:22px;letter-spacing:-.015em; }
    h3 { margin:24px 0 10px;color:var(--muted);font-size:13px;text-transform:uppercase;letter-spacing:.055em; }
    .section-head { display:flex;justify-content:space-between;align-items:flex-start;gap:12px; }
    .comparability { border:1px solid currentColor;font-weight:750;white-space:nowrap; }
    .comparable { color:var(--good); }.partially_comparable { color:var(--warn); }.not_comparable { color:var(--bad); }
    .input-grid,.status-grid { display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:12px;margin-top:16px; }
    .input-card,.status-card,.metric-card,.observation-card { border:1px solid var(--line);border-radius:14px;background:var(--panel-soft); }
    .input-card { position:relative;padding:16px;overflow:hidden; }
    .input-card.reference { border-color:var(--accent);box-shadow:inset 0 0 0 1px var(--accent); }
    .input-index { position:absolute;right:13px;top:10px;color:var(--muted);font-weight:800;font-size:12px; }
    .input-card h3 { margin:0 36px 7px 0;color:var(--text);font-size:16px;text-transform:none;letter-spacing:0;overflow-wrap:anywhere; }
    .reference-tag { color:var(--accent);border:1px solid currentColor;font-weight:750; }
    dl { margin:10px 0 0; }.input-card dl div { display:grid;grid-template-columns:90px 1fr;gap:10px;padding:4px 0; }
    dt { color:var(--muted); } dd { margin:0;font-weight:600;overflow-wrap:anywhere; }
    .status-card { padding:14px; }.report-name { color:var(--muted);font-size:12px;overflow-wrap:anywhere; }
    .status-card strong { display:block;margin:4px 0;font-size:16px; }.evidence-line { font-size:12px;margin:5px 0; }
    .metric-list { display:grid;gap:12px; }
    .metric-card { padding:16px; }.metric-head { display:flex;justify-content:space-between;gap:12px;align-items:flex-start; }
    .metric-title { font-size:16px;font-weight:800; }.method { color:var(--muted);font-size:11px;margin-top:3px;overflow-wrap:anywhere; }
    .direction { width:27px;height:27px;border-radius:50%%;display:grid;place-items:center;background:color-mix(in srgb,var(--accent) 12%%,transparent);color:var(--accent);font-weight:900;flex:none; }
    .metric-values { display:grid;gap:10px;margin-top:13px; }
    .layout-pair .metric-values { grid-template-columns:repeat(2,minmax(0,1fr)); }
    .layout-matrix .metric-values { grid-template-columns:repeat(auto-fit,minmax(155px,1fr)); }
    .layout-many .metric-values { grid-template-columns:1fr; }
    .value-card { min-width:0;padding:12px;border:1px solid var(--line);border-radius:11px;background:var(--panel); }
    .layout-many .value-card { display:grid;grid-template-columns:minmax(130px,.8fr) minmax(120px,.7fr) minmax(100px,.65fr) minmax(140px,1fr);align-items:center;gap:12px; }
    .value-meta { display:flex;justify-content:space-between;gap:8px; }.rank { color:var(--muted);font-size:12px;font-variant-numeric:tabular-nums; }
    .number { margin:4px 0 2px;font-size:21px;font-weight:730;font-variant-numeric:tabular-nums;overflow-wrap:anywhere; }
    .delta { color:var(--muted);font-size:12px; }.bar { height:10px;border-radius:8px;background:var(--line);overflow:hidden;margin-top:8px; }
    .bar i { display:block;height:100%%;border-radius:8px; }
    .value-card.best { border-color:var(--good);box-shadow:inset 0 0 0 1px var(--good);background:color-mix(in srgb,var(--panel) 93%%,var(--good)); }
    .value-card.best .number { color:var(--good);font-weight:880; }.value-card.regressed:not(.best) .delta { color:var(--bad);font-weight:750; }
    .value-card.improved:not(.best) .delta { color:var(--good);font-weight:750; }.value-card.worst:not(.best) .number { color:var(--warn); }
    .value-card.missing { opacity:.65; }.good,.ok { color:var(--good); }.warn,.warning { color:var(--warn); }.bad,.error { color:var(--bad); }.muted,.skipped { color:var(--muted); }.accent { color:var(--accent); }.neutral { color:var(--text); }
    .observation-card { margin-top:10px;padding:14px; }.observation-title { font-weight:800;margin-bottom:9px; }
    .observation-values { display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:8px; }
    .observation-values div { border-left:2px solid var(--line);padding-left:10px;min-width:0; }.observation-values span { display:block;color:var(--muted);font-size:11px; }.observation-values strong { display:block;overflow-wrap:anywhere; }
    .table-wrap { overflow-x:auto;border:1px solid var(--line);border-radius:12px; } table { width:100%%;border-collapse:collapse;min-width:620px; }
    th,td { padding:9px 11px;text-align:left;border-bottom:1px solid var(--line); } th { color:var(--muted);background:var(--panel-soft);font-size:12px; } tr:last-child td { border-bottom:0; }
    footer { padding:24px 4px;color:var(--muted);font-size:12px; }
    @media (max-width:760px) { main { width:calc(100%% - 20px);margin:10px auto 35px; }.hero,section { padding:18px;border-radius:14px; }.section-head { display:block; }.comparability { margin-top:8px; }.layout-pair .metric-values,.layout-matrix .metric-values { grid-template-columns:1fr; }.layout-many .value-card { display:block; }.layout-many .value-card .bar { margin-top:8px; }.input-grid,.status-grid { grid-template-columns:1fr; } }
    @media print { :root { --bg:#fff;--panel:#fff;--panel-soft:#fff;--text:#111;--muted:#555;--line:#ddd;--shadow:none; } main { width:100%%;margin:0; }.hero { border-radius:0; } section,.metric-card { break-inside:avoid; } }
  </style>
</head>
<body class="%s">`
