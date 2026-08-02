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

func HTML(data model.Report, scored *score.Report) ([]byte, error) {
	functions := template.FuncMap{
		"statusLabel": statusLabel,
		"statusIcon":  statusIcon,
		"duration":    formatDurationMS,
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
		"metricName": func(metric score.MetricScore) string {
			if key := "score.metric." + metric.Key; i18n.Has(i18n.Current(), key) {
				return i18n.T(key)
			}
			return metric.Label
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
		"missingReason":  func(key string) string { return i18n.T("score.missing." + key) },
		"baselineSource": baselineSourceLabel,
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

const htmlTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <meta name="referrer" content="no-referrer">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
  <title>ecs VPS 综合测试报告</title>
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
    @media print {
      :root { --bg: #fff; --panel: #fff; --text: #111; --muted: #555; --line: #ddd; --shadow: none; }
      main { width: 100%; margin: 0; } .hero { border-radius: 0; } section { break-inside: avoid; }
    }
  </style>
</head>
<body>
<main>
  <header class="hero">
    <h1>ecs VPS 综合测试报告</h1>
    <p>{{statusIcon .Summary.Status}} {{.Summary.Headline}} · 本地生成，零广告，零自动上传</p>
    <div class="hero-meta">
      <span class="pill">报告 {{.Run.ID}}</span>
      <span class="pill">{{.Run.Profile}}</span>
      {{if .Run.IPVersion}}<span class="pill">IP {{.Run.IPVersion}}</span>{{end}}
      <span class="pill">{{time .Run.StartedAt}}</span>
      <span class="pill">{{duration .Run.DurationMS}}</span>
      <span class="pill">{{boolText .Run.Redacted "敏感字段已遮盖" "包含完整敏感字段"}}</span>
      {{if .Run.Canceled}}<span class="pill">运行已中断 · 部分报告</span>{{end}}
    </div>
  </header>

  <div class="summary-grid">
    <div class="summary-card"><strong class="ok">{{.Summary.OK}}</strong><span>完成</span></div>
    <div class="summary-card"><strong class="warning">{{.Summary.Warnings}}</strong><span>需留意</span></div>
    <div class="summary-card"><strong class="error">{{.Summary.Errors}}</strong><span>异常</span></div>
    <div class="summary-card"><strong class="skipped">{{.Summary.Skipped}}</strong><span>跳过</span></div>
  </div>

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
      {{end}}
    {{end}}
    <div class="score-note">
      {{if not .Score.Complete}}<p>{{scoreText "score.incompleteWarning"}}</p>{{end}}
      {{if le .Score.BaselineSample 1}}<p>{{scoreText "score.singleSampleWarning"}}</p>{{end}}
      <p>{{baselineLine .Score.BaselineSource .Score.BaselineSample}}</p>
      {{if .Score.TierLabel}}<p>{{tierLine .Score.HostVCPU .Score.TierLabel}}</p>
      {{else if .Score.HostVCPU}}<p>{{tierFallbackLine .Score.HostVCPU}}</p>{{end}}
    </div>
  </div>
  {{end}}

  {{range .Results}}
  <section id="{{.ID}}">
    <div class="section-head">
      <div><h2>{{.Title}}</h2>{{if .Description}}<p class="description">{{.Description}}</p>{{end}}</div>
      <div class="badges">
        {{if .Methodology.Label}}<span class="badge method-badge">{{.Methodology.Label}}</span>{{end}}
        <span class="badge {{.Status}}">{{statusIcon .Status}} {{statusLabel .Status}}</span>
      </div>
    </div>
    {{if .Methodology.Label}}
    <div class="methodology"><strong>{{.Methodology.Label}}</strong>
      {{if .Methodology.Engine}} · {{.Methodology.Engine}}{{end}}
      {{if .Methodology.Profile}} · <code>{{.Methodology.Profile}}</code>{{end}}
      {{if .Methodology.ComparisonScope}}<div class="muted">可比范围：{{.Methodology.ComparisonScope}}</div>{{end}}
    </div>
    {{end}}
    {{if .Summary}}<p class="headline">{{.Summary}} <span class="muted">· {{duration .DurationMS}}</span></p>{{end}}
    {{if .Error}}<p class="error">错误：{{.Error}}</p>{{end}}

    {{if .Measurements}}
    <div class="metrics">
      {{range .Measurements}}
      <div class="metric">
        <div class="label">{{.Label}}{{if .Rating}} · {{.Rating}}{{end}}</div>
        <div class="value">{{.Display}}</div>
        {{if .Method}}<div class="method">{{.Method}}</div>{{end}}
      </div>
      {{end}}
    </div>
    {{end}}

    {{if .Fields}}
    <h3>详情</h3>
    <dl class="fields">{{range .Fields}}<div class="field"><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>{{end}}</dl>
    {{end}}

    {{range .Tables}}
    {{if .Title}}<h3>{{.Title}}</h3>{{end}}
    <div class="table-wrap"><table><thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
      <tbody>{{range .Rows}}<tr>{{range .}}<td class="{{valueClass .}}">{{.}}</td>{{end}}</tr>{{end}}</tbody>
    </table></div>
    {{end}}

    {{range .TextBlocks}}
    <details><summary>{{.Title}}</summary><pre><code>{{.Content}}</code></pre></details>
    {{end}}

    {{if .Notes}}<h3>说明</h3><ul>{{range .Notes}}<li>{{.}}</li>{{end}}</ul>{{end}}
    {{if .Sources}}<h3>数据来源</h3><ul>{{range .Sources}}<li>{{if .URL}}<a href="{{.URL}}" rel="noreferrer">{{.Name}}</a>{{else}}{{.Name}}{{end}}{{if .Purpose}}：{{.Purpose}}{{end}}</li>{{end}}</ul>{{end}}
  </section>
  {{end}}

  <footer>
    <p>{{range .Notices}}{{.}} · {{end}}</p>
    <p>Schema {{.SchemaVersion}} · {{.Tool.Name}} {{.Tool.Version}} · Commit {{.Tool.Commit}}</p>
  </footer>
</main>
</body>
</html>
`

func reportValueClass(value string) string {
	switch strings.TrimSpace(value) {
	case "极低", "低", "否", "成功", "原生 IP":
		return "cell-good"
	case "中等", "需留意", "可疑", "商业", "其他":
		return "cell-warn"
	case "高", "极高", "是", "机房", "失败":
		return "cell-bad"
	case "—", "未启用", "未返回":
		return "cell-muted"
	default:
		if strings.HasPrefix(value, "失败：") {
			return "cell-bad"
		}
		if strings.HasPrefix(value, "部分：") {
			return "cell-warn"
		}
		return ""
	}
}
