package report

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

// htmlPayload 把评分与报告一起交给模板：模板只认一个顶层数据对象。
type htmlPayload struct {
	model.Report
	Score *score.Report
}

type htmlTableCell struct {
	Value string
	Class string
}

// HTML renders the machine report directly; stable keys are resolved by the
// template functions at the individual presentation fields.
func HTML(data model.Report, scored *score.Report) ([]byte, error) {
	return htmlReport(data, scored)
}

func htmlReport(data model.Report, scored *score.Report) ([]byte, error) {
	functions := template.FuncMap{
		"t":             i18n.T,
		"display":       displayReportText,
		"headline":      reportHeadline,
		"resultSummary": resultSummary,
		"message":       renderMessage,
		"htmlLang":      reportHTMLLanguage,
		"methodology":   localizedMethodology,
		"statusLabel":   statusLabel,
		"statusIcon":    statusIcon,
		"duration":      formatDurationMS,
		"time": func(value time.Time) string {
			return value.Format("2006-01-02 15:04:05 MST")
		},
		"boolText": func(value bool, yes, no string) string {
			if value {
				return yes
			}
			return no
		},
		"valueClass": reportValueClass,
		"scoreValue": func(value float64) string { return fmt.Sprintf("%.0f", value) },
		"metricValue": func(value float64) string {
			switch {
			case value >= 1000:
				return fmt.Sprintf("%.0f", value)
			case value >= 10:
				return fmt.Sprintf("%.1f", value)
			default:
				return fmt.Sprintf("%.2f", value)
			}
		},
		"percent": func(ratio float64) string { return fmt.Sprintf("%.0f", ratio*100) },
		"evidenceText": func(value *model.Evidence) string {
			if value == nil {
				return ""
			}
			return evidenceText(*value)
		},
		"evidenceRatio": func(value *model.Evidence) float64 {
			if value == nil {
				return 0
			}
			return value.EvidenceRatio()
		},
		"evidenceColor":      evidenceHTMLColor,
		"evidenceLabelColor": evidenceHTMLLabelColor,
		"failureCategory":    failureCategoryLabel,
		"failureRetryable":   failureRetryableLabel,
		"failureCount": func(value int) int {
			if value < 1 {
				return 1
			}
			return value
		},
		// 条宽与 txt 柱状图同口径：超过基线不撑破容器，但颜色已到顶。
		"barWidth": func(ratio float64) string {
			if ratio > 1 {
				ratio = 1
			}
			if ratio < 0 {
				ratio = 0
			}
			return fmt.Sprintf("%.1f", ratio*100)
		},
		"scoreColor": func(ratio float64) template.CSS {
			color := termcolor.Color(ratio)
			return template.CSS(fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B))
		},
		"dimensionName": func(key string) string { return i18n.T("score.dimension." + key) },
		"resultTitle":   resultTitle,
		"tableRows": func(table model.Table) [][]htmlTableCell {
			return htmlTableRows(displayTable(table))
		},
		"metricName": func(metric score.MetricScore) string {
			return metricLabel(metric)
		},
		// 评分区的成句文案统一走 i18n，避免模板里留下写死的中文。
		"scoreText": func(key string) string { return i18n.T(key) },
		"ofBaseline": func(ratio float64) string {
			return fmt.Sprintf(i18n.T("score.ofBaseline"), ratio*100)
		},
		"coverageText": func(covered, possible int) string {
			return fmt.Sprintf(i18n.T("score.coverage"), covered, possible)
		},
		"tierLine": func(vcpu int, label string) string {
			return fmt.Sprintf(i18n.T("score.tierLine"), vcpu, label)
		},
		"tierFallbackLine": func(vcpu int) string {
			return fmt.Sprintf(i18n.T("score.tierFallbackLine"), vcpu)
		},
		"baselineLine": func(source string, sample int) string {
			return fmt.Sprintf(i18n.T("score.baselineLine"), baselineSourceLabel(source), sample)
		},
		"rankLine": func(scored *score.Report) string {
			if scored == nil {
				return ""
			}
			switch scored.EffectiveRankStatus() {
			case score.RankStatusAvailable:
				return fmt.Sprintf(i18n.T("score.rank.available"), scored.TopPercent, scored.EffectiveRankSamples())
			case score.RankStatusInsufficient:
				return fmt.Sprintf(i18n.T("score.rank.insufficient"), scored.EffectiveRankSamples(), scored.EffectiveRankMinSamples())
			default:
				return i18n.T("score.rank.unavailable")
			}
		},
		"missingReason": func(key string) string { return i18n.T("score.missing." + key) },
		"missingMetrics": func(dimension score.DimensionScore) string {
			if len(dimension.MissingMetrics) == 0 {
				return ""
			}
			return fmt.Sprintf(i18n.T("score.missingMetrics"), i18n.T("score.dimension."+dimension.Key), len(dimension.MissingMetrics), strings.Join(dimension.MissingMetrics, ", "))
		},
		"baselineSource":   baselineSourceLabel,
		"interferenceView": interferencePresentation,
		"retryView":        retryPresentation,
		"textBlockTitle":   displayTextBlockTitle,
	}
	parsed, err := template.New("report").Funcs(functions).Parse(htmlTemplate)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := parsed.Execute(&output, htmlPayload{Report: data, Score: scored}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func reportHTMLLanguage() string {
	if i18n.Current() == i18n.LangEN {
		return "en"
	}
	return "zh-CN"
}

const htmlTemplate = `<!doctype html>
<html lang="{{htmlLang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <meta name="referrer" content="no-referrer">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
  <title>{{t "report.title"}}</title>
  <style>
    :root {
      --bg: #f4f6fb; --panel: #ffffff; --text: #182033; --muted: #65708a;
      --line: #dfe4ee; --accent: #315efb; --ok: #0b8f55; --warn: #b26700;
      --error: #c12a38; --skip: #6d7485; --shadow: 0 10px 30px rgba(25,40,75,.08);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0d1220; --panel: #151c2d; --text: #edf1fb; --muted: #a0abc1;
        --line: #2a344a; --accent: #84a2ff; --ok: #57d49b; --warn: #ffbd62;
        --error: #ff7d89; --skip: #a4adbf; --shadow: none;
      }
    }
    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg); color: var(--text); font: 15px/1.6 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    main { width: min(1180px, calc(100% - 32px)); margin: 32px auto 64px; }
    .hero { background: linear-gradient(135deg, #243b8f, #315efb 60%, #5d85ff); color: #fff; border-radius: 22px; padding: 32px; box-shadow: var(--shadow); }
    .hero h1 { margin: 0 0 4px; font-size: clamp(26px, 5vw, 42px); letter-spacing: -.03em; }
    .hero p { margin: 0; opacity: .9; }
    .hero-meta { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 24px; }
    .pill { display: inline-flex; align-items: center; border-radius: 999px; padding: 6px 11px; background: rgba(255,255,255,.15); font-size: 13px; }
    .summary-grid, .metrics { display: grid; grid-template-columns: repeat(auto-fit,minmax(170px,1fr)); gap: 14px; }
    .summary-grid { margin: 18px 0; }
    .summary-card, section { background: var(--panel); border: 1px solid var(--line); border-radius: 18px; box-shadow: var(--shadow); }
    .summary-card { padding: 18px; }
    .summary-card strong { display: block; font-size: 27px; line-height: 1.15; }
    .summary-card span { color: var(--muted); font-size: 13px; }
    section { margin-top: 18px; padding: 24px; }
    h2 { margin: 0; font-size: 22px; letter-spacing: -.015em; }
    h3 { margin: 24px 0 10px; font-size: 15px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; }
    .section-head { display: flex; gap: 12px; justify-content: space-between; align-items: flex-start; }
    .description, .muted { color: var(--muted); }
    .description { margin: 4px 0 0; }
    .badge { white-space: nowrap; border: 1px solid currentColor; border-radius: 999px; padding: 3px 9px; font-size: 12px; font-weight: 700; }
    .badges { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 7px; }
    .method-badge { color: var(--accent); }
    .methodology { margin: 14px 0 0; padding: 11px 13px; border-left: 3px solid var(--accent); border-radius: 8px; background: color-mix(in srgb, var(--panel) 93%, var(--accent)); }
    .methodology strong { color: var(--accent); }
    .ok { color: var(--ok); } .warning { color: var(--warn); } .error { color: var(--error); } .skipped { color: var(--skip); }
    .headline { margin: 16px 0 0; font-size: 17px; font-weight: 650; }
    .evidence { display: grid; grid-template-columns: minmax(130px,auto) minmax(120px,320px); align-items: center; gap: 12px; margin-top: 12px; color: var(--muted); font-size: 13px; }
		.failure-kind { font-weight: 750; color: var(--error); }
		.retryable { color: var(--warn); font-weight: 650; }
    .metrics { margin-top: 14px; }
    .metric { padding: 15px; border: 1px solid var(--line); border-radius: 14px; background: color-mix(in srgb, var(--panel) 92%, var(--accent)); }
    .metric .label { color: var(--muted); font-size: 12px; }
    .metric .value { font-size: 24px; font-weight: 750; line-height: 1.25; margin-top: 4px; word-break: break-word; }
    .metric .method { color: var(--muted); font: 11px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace; margin-top: 7px; }
    .fields { display: grid; grid-template-columns: repeat(auto-fit,minmax(260px,1fr)); gap: 0 20px; }
    .field { display: grid; grid-template-columns: minmax(100px, .7fr) minmax(120px, 1.3fr); gap: 12px; padding: 9px 0; border-bottom: 1px solid var(--line); }
    .field dt { color: var(--muted); } .field dd { margin: 0; font-weight: 550; overflow-wrap: anywhere; }
    .table-wrap { overflow-x: auto; border: 1px solid var(--line); border-radius: 13px; }
    table { width: 100%; border-collapse: collapse; min-width: 580px; }
    th, td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--line); white-space: nowrap; }
    th { color: var(--muted); background: color-mix(in srgb, var(--panel) 92%, var(--text)); font-size: 12px; }
    tr:last-child td { border-bottom: 0; }
    .cell-good { color: var(--ok); font-weight: 700; }
    .cell-warn { color: var(--warn); font-weight: 700; }
    .cell-bad { color: var(--error); font-weight: 700; }
    .cell-muted { color: var(--muted); }
    ul { padding-left: 21px; margin-bottom: 0; }
    details { margin-top: 12px; border: 1px solid var(--line); border-radius: 13px; padding: 10px 13px; }
    summary { cursor: pointer; font-weight: 650; }
    .score-card { background: var(--panel); border: 1px solid var(--line); border-radius: 18px;
      box-shadow: var(--shadow); padding: 20px; margin: 18px 0; }
    .score-total { display: flex; align-items: baseline; gap: 14px; flex-wrap: wrap; margin-bottom: 6px; }
    .score-total strong { font-size: 40px; line-height: 1; }
    .score-total span { color: var(--muted); font-size: 13px; }
    .score-row { display: grid; grid-template-columns: 88px 1fr 72px; align-items: center;
      gap: 12px; margin: 9px 0; }
    .score-row .name { font-weight: 620; }
    .score-row .value { text-align: right; font-variant-numeric: tabular-nums; }
    /* 条形与 txt 报告用同一套色阶：低分偏橙红，接近或超过基线转青绿。
       宽度按比例，与柱状图一样不做等宽装饰。 */
    .bar { position: relative; height: 13px; border-radius: 7px; background: var(--line); overflow: hidden; }
    .bar > i { display: block; height: 100%; border-radius: 7px; }
    .score-metrics { margin: 4px 0 12px 88px; color: var(--muted); font-size: 13px; }
    .score-metrics div { display: grid; grid-template-columns: 1fr auto auto; gap: 10px; padding: 1px 0; }
    .score-metrics .num { font-variant-numeric: tabular-nums; }
    .score-missing { color: var(--muted); }
    .score-note { color: var(--muted); font-size: 13px; margin-top: 10px; }

    pre { overflow: auto; max-height: 480px; margin: 10px -2px 0; padding: 14px; border-radius: 10px; background: #090d16; color: #d9e2f5; font: 12px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }
    a { color: var(--accent); }
    footer { padding: 24px 4px; color: var(--muted); font-size: 13px; }
    @media (max-width: 560px) {
      main { width: min(1180px, calc(100% - 20px)); margin-top: 10px; }
      .hero, section { padding: 18px; border-radius: 14px; }
      .section-head { display: block; }
      .badges { justify-content: flex-start; margin-top: 9px; }
      .evidence { grid-template-columns: 1fr; gap: 6px; }
    }
    @media print {
      :root { --bg: #fff; --panel: #fff; --text: #111; --muted: #555; --line: #ddd; --shadow: none; }
      main { width: 100%; margin: 0; } .hero { border-radius: 0; } section { break-inside: avoid; }
    }
  </style>
</head>
<body>
<main>
  <header class="hero">
    <h1>{{t "report.title"}}</h1>
    <p>{{statusIcon .Summary.Status}} {{headline .Summary}} · {{t "report.local"}}</p>
    <div class="hero-meta">
      <span class="pill">{{t "report.reportID"}} {{.Run.ID}}</span>
      <span class="pill">{{t "report.profile"}}: {{.Run.Profile}}</span>
      {{if .Run.IPVersion}}<span class="pill">{{t "report.ipVersion"}}: {{.Run.IPVersion}}</span>{{end}}
      <span class="pill">{{t "report.startedAt"}}: {{time .Run.StartedAt}}</span>
      <span class="pill">{{t "report.totalDuration"}}: {{duration .Run.DurationMS}}</span>
      <span class="pill">{{boolText .Run.Redacted (t "report.redacted") (t "report.revealed")}}</span>
      {{if .Run.Canceled}}<span class="pill">{{t "report.canceled"}}</span>{{end}}
    </div>
  </header>

  <div class="summary-grid">
    <div class="summary-card"><strong class="ok">{{.Summary.OK}}</strong><span>{{t "status.ok"}}</span></div>
    <div class="summary-card"><strong class="warning">{{.Summary.Warnings}}</strong><span>{{t "status.warning"}}</span></div>
    <div class="summary-card"><strong class="error">{{.Summary.Errors}}</strong><span>{{t "status.error"}}</span></div>
    <div class="summary-card"><strong class="skipped">{{.Summary.Skipped}}</strong><span>{{t "status.skipped"}}</span></div>
  </div>

  {{range .Results}}
  <section id="{{.ID}}">
    <div class="section-head">
      <div><h2>{{resultTitle .}}</h2>{{if .Description}}<p class="description">{{display .Description}}</p>{{end}}</div>
      <div class="badges">
        {{if .Methodology.Label}}<span class="badge method-badge">{{methodology .Methodology}}</span>{{end}}
        <span class="badge {{.Status}}">{{statusIcon .Status}} {{statusLabel .Status}}</span>
      </div>
    </div>
    {{if .Methodology.Label}}
    <div class="methodology"><strong>{{methodology .Methodology}}</strong>
      {{if .Methodology.Engine}} · {{display .Methodology.Engine}}{{end}}
      {{if .Methodology.Profile}} · <code>{{display .Methodology.Profile}}</code>{{end}}
      {{if .Methodology.ComparisonScope}}<div class="muted">{{t "report.comparability"}}{{t "punct.colon"}}{{display .Methodology.ComparisonScope}}</div>{{end}}
    </div>
    {{end}}
    {{if resultSummary .}}<p class="headline">{{resultSummary .}} <span class="muted">· {{duration .DurationMS}}</span></p>{{end}}
    {{if .Error}}<p class="error">{{t "report.errorPrefix"}}{{t "punct.colon"}}{{.Error}}</p>{{end}}
    {{if .Evidence}}<div class="evidence"><strong style="color:{{evidenceLabelColor .Evidence}}">{{t "report.evidence"}}{{t "punct.colon"}}{{evidenceText .Evidence}}</strong><div class="bar"><i style="width:{{barWidth (evidenceRatio .Evidence)}}%;background:{{evidenceColor .Evidence}}"></i></div></div>{{end}}
		{{if .Failures}}
		<h3>{{t "report.failures"}}</h3>
		<div class="table-wrap"><table><thead><tr>
			<th>{{t "failure.category"}}</th><th>{{t "failure.stage"}}</th><th>{{t "failure.target"}}</th>
			<th>{{t "failure.count"}}</th><th>{{t "failure.retryable"}}</th><th>{{t "failure.message"}}</th>
		</tr></thead><tbody>{{range .Failures}}<tr>
			<td class="failure-kind">{{failureCategory .Category}}</td><td>{{.Stage}}</td><td>{{.Target}}</td>
			<td>{{failureCount .Count}}</td><td class="{{if .Retryable}}retryable{{else}}muted{{end}}">{{failureRetryable .Retryable}}</td><td>{{.Message}}</td>
		</tr>{{end}}</tbody></table></div>
		{{end}}

    {{with interferenceView .}}
    <h3>{{.Title}}</h3>
    <p><strong>{{.ScoreLabel}}{{t "punct.colon"}}</strong> {{.Score}}</p>
    {{if .Measurements}}
    <div class="table-wrap"><table><thead><tr>
      <th>{{t "report.interference.metric"}}</th><th>{{t "report.interference.value"}}</th><th>{{t "report.interference.method"}}</th>
    </tr></thead><tbody>{{range .Measurements}}<tr><td>{{.Label}}</td><td>{{.Value}}</td><td>{{.Method}}</td></tr>{{end}}</tbody></table></div>
    {{end}}
    <h3>{{.ReasonsTitle}}</h3><ul>{{range .Reasons}}<li>{{.}}</li>{{end}}</ul>
    {{end}}

    {{with retryView .}}
    <h3>{{.Title}}</h3>
    <p><strong>{{.SelectionRuleLabel}}{{t "punct.colon"}}</strong> {{.SelectionRule}}</p>
    <p><strong>{{.TriggerReasonsLabel}}{{t "punct.colon"}}</strong></p>
    <ul>{{range .TriggerReasons}}<li>{{.}}</li>{{end}}</ul>
    <h3>{{.AttemptsLabel}}</h3>
    <div class="table-wrap"><table><thead><tr>
      <th>{{t "report.retry.attempt"}}</th><th>{{t "report.retry.status"}}</th><th>{{t "report.retry.evidence"}}</th>
      <th>{{t "report.retry.score"}}</th><th>{{t "report.retry.reasons"}}</th><th>{{t "report.retry.selection"}}</th>
    </tr></thead><tbody>{{range .Attempts}}<tr>
      <td>{{.Number}}</td><td>{{.Status}}</td><td>{{.Evidence}}</td><td>{{.Score}}</td><td>{{.Reasons}}</td>
      <td class="{{if .Selected}}ok{{else}}muted{{end}}">{{.Selection}}</td>
    </tr>{{end}}</tbody></table></div>
    {{end}}

    {{if .Measurements}}
    <div class="metrics">
      {{range .Measurements}}
      <div class="metric">
        <div class="label">{{display .Label}}{{if .Rating}} · {{display .Rating}}{{end}}</div>
        <div class="value">{{display .Display}}</div>
        {{if .Method}}<div class="method">{{.Method}}</div>{{end}}
      </div>
      {{end}}
    </div>
    {{end}}

    {{if .Fields}}
    <h3>{{t "report.details"}}</h3>
    <dl class="fields">{{range .Fields}}<div class="field"><dt>{{display .Label}}</dt><dd>{{display .Value}}</dd></div>{{end}}</dl>
    {{end}}

    {{range .Tables}}
    {{if .Title}}<h3>{{display .Title}}</h3>{{end}}
    <div class="table-wrap"><table><thead><tr>{{range .Columns}}<th>{{display .}}</th>{{end}}</tr></thead>
      <tbody>{{range tableRows .}}<tr>{{range .}}<td class="{{.Class}}">{{.Value}}</td>{{end}}</tr>{{end}}</tbody>
    </table></div>
    {{end}}

    {{range .TextBlocks}}
    <details><summary>{{if textBlockTitle .}}{{textBlockTitle .}}{{else}}{{t "report.rawOutput"}}{{end}}</summary><pre><code>{{.Content}}</code></pre></details>
    {{end}}

    {{if .Notes}}<h3>{{t "report.notes"}}</h3><ul>{{range .Notes}}<li>{{display .}}</li>{{end}}</ul>{{end}}
    {{if .Sources}}<h3>{{t "report.sources"}}</h3><ul>{{range .Sources}}<li>{{if .URL}}<a href="{{.URL}}" rel="noreferrer">{{.Name}}</a>{{else}}{{.Name}}{{end}}{{if .Purpose}}{{t "punct.colon"}}{{display .Purpose}}{{end}}</li>{{end}}</ul>{{end}}
  </section>
  {{end}}

  {{if .Score}}
  <div class="score-card">
    <h2>{{scoreText "score.title"}}</h2>
    <div class="score-total">
      <strong style="color:{{scoreColor .Score.Ratio}}">{{scoreValue .Score.Total}}</strong>
      <span>{{coverageText .Score.Covered .Score.Possible}}</span>
    </div>
    {{range .Score.Dimensions}}
      {{if .Missing}}
      <div class="score-row">
        <div class="name">{{dimensionName .Key}}</div>
        <div class="score-missing" style="grid-column: 2 / span 2">{{missingReason .MissingReason}}</div>
      </div>
      {{else}}
      <div class="score-row">
        <div class="name">{{dimensionName .Key}}</div>
        <div class="bar"><i style="width:{{barWidth .Ratio}}%;background:{{scoreColor .Ratio}}"></i></div>
        <div class="value" style="color:{{scoreColor .Ratio}}">{{scoreValue .Score}}</div>
      </div>
      <div class="score-metrics">
        {{range .Metrics}}<div><span>{{metricName .}}</span><span class="num">{{metricValue .Value}} {{.Unit}}</span><span class="num">{{ofBaseline .Ratio}}</span></div>{{end}}
      </div>
      {{if .MissingMetrics}}<div class="score-missing">{{missingMetrics .}}</div>{{end}}
      {{end}}
    {{end}}
    <div class="score-note">
      {{if not .Score.Complete}}<p>{{scoreText "score.incompleteWarning"}}</p>{{end}}
      <p>{{baselineLine .Score.BaselineSource .Score.BaselineSample}}</p>
      {{if or .Score.RankStatus .Score.RankSamples .Score.BaselineSample}}<p>{{rankLine .Score}}</p>{{end}}
      {{if .Score.TierLabel}}<p>{{tierLine .Score.HostVCPU .Score.TierLabel}}</p>
      {{else if .Score.HostVCPU}}<p>{{tierFallbackLine .Score.HostVCPU}}</p>{{end}}
    </div>
  </div>
  {{end}}

  <footer>
    <p>{{range .Notices}}{{message .}} · {{end}}</p>
    <p>{{t "report.schema"}}: {{.SchemaVersion}} · {{t "report.generator"}}: {{.Tool.Name}} {{.Tool.Version}}{{if .Tool.Commit}} · {{t "report.commit"}}: {{.Tool.Commit}}{{end}}</p>
  </footer>
</main>
</body>
</html>
`

func evidenceHTMLColor(evidence *model.Evidence) template.CSS {
	if evidence == nil || evidence.Expected <= 0 {
		return template.CSS("var(--skip)")
	}
	color := termcolor.Color(evidence.EvidenceRatio())
	return template.CSS(fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B))
}

func evidenceHTMLLabelColor(evidence *model.Evidence) template.CSS {
	switch {
	case evidence == nil || evidence.Expected <= 0:
		return template.CSS("var(--skip)")
	case evidence.Valid <= 0:
		return template.CSS("var(--error)")
	case evidence.Valid >= evidence.Expected:
		return template.CSS("var(--ok)")
	default:
		return template.CSS("var(--warn)")
	}
}

// reportValueClass 给表格单元格挑一个语义色。
//
// 值在渲染前已经过本地化，因此中英两种写法都要认：只列中文会让英文报告的
// 整张表退化成无色。前缀判断放在精确匹配之后，覆盖"失败：<原因>"这类带
// 后缀的取值。
func reportValueClass(value string) string {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "极低", "低", "否", "成功", "原生 IP",
		"very low", "low", "no", "success", "native ip", "available", "true", "unlocked", "passed":
		return "cell-good"
	case "中等", "需留意", "可疑", "商业", "其他",
		"elevated", "medium", "attention", "suspicious", "business", "other", "partial":
		return "cell-warn"
	case "高", "极高", "是", "机房", "失败",
		"high", "very high", "yes", "hosting", "datacenter", "failed", "failure":
		return "cell-bad"
	case "—", "未启用", "未返回",
		"not enabled", "no response", "unavailable", "false", "unknown", "n/a":
		return "cell-muted"
	}
	for _, prefix := range []string{"失败：", "failed:", "failure:"} {
		if strings.HasPrefix(strings.ToLower(trimmed), prefix) || strings.HasPrefix(trimmed, prefix) {
			return "cell-bad"
		}
	}
	for _, prefix := range []string{"部分：", "partial:"} {
		if strings.HasPrefix(strings.ToLower(trimmed), prefix) || strings.HasPrefix(trimmed, prefix) {
			return "cell-warn"
		}
	}
	return ""
}

func htmlTableRows(table model.Table) [][]htmlTableCell {
	rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone})
	result := make([][]htmlTableCell, len(rows))
	for rowIndex, row := range rows {
		result[rowIndex] = make([]htmlTableCell, len(table.Columns))
		for column := range table.Columns {
			value := ""
			if column < len(row) {
				value = row[column]
			}
			original := ""
			if column < len(table.Rows[rowIndex]) {
				original = table.Rows[rowIndex][column]
			}
			class := reportValueClass(original)
			for _, numericColumn := range table.NumericColumns {
				if numericColumn != column {
					continue
				}
				if _, ok := numericCellValue(original); !ok {
					break
				}
				density := "░"
				if strings.Contains(value, "█") {
					density = "█"
				} else if strings.Contains(value, "▓") {
					density = "▓"
				} else if strings.Contains(value, "▒") {
					density = "▒"
				}
				class = map[string]string{"█": "cell-good", "▓": "cell-good", "▒": "cell-warn", "░": "cell-bad"}[density]
				break
			}
			result[rowIndex][column] = htmlTableCell{Value: value, Class: class}
		}
	}
	return result
}
