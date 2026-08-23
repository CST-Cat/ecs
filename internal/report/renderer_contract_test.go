package report

import (
	"encoding/json"
	"strings"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
)

func TestHTMLRendererEscapesUntrustedReportText(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].SummaryMessages = nil
	data.Results[0].Summary = "<script>alert(1)</script>"
	data.Results[0].Sources = append(data.Results[0].Sources, model.Source{Name: "unsafe", URL: "javascript:alert(1)"})

	html, err := HTML(data, rendererScoreFixture())
	if err != nil {
		t.Fatal(err)
	}
	output := string(html)
	if !strings.Contains(output, "<html") || strings.Contains(output, "<script>alert(1)</script>") || strings.Contains(output, "javascript:alert(1)") {
		t.Fatalf("HTML structure or escaping is invalid: %s", output)
	}
	if !strings.Contains(output, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("HTML did not escape untrusted text: %s", output)
	}
	for _, marker := range []string{"Memory Benchmark", "Structured failures", "Standard benchmark", "Composite score", "Skipped", "Interrupted by user", "Not enough leaderboard samples", "The official STREAM executable"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("HTML rich report missing %q: %s", marker, output)
		}
	}
}

func TestMarkdownRendersRichReportAndSafeLinks(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].SummaryMessages = nil
	data.Results[0].Summary = "<script>alert(1)</script>"
	data.Results[0].TextBlocks[0].Content = "raw output 192.0.2.10\n``` <payload>unsafe</payload>"
	data.Results[0].Sources = append(data.Results[0].Sources, model.Source{Name: "unsafe", URL: "javascript:alert(1)"})
	fallbackScore := rendererScoreFixture()
	fallbackScore.RankStatus = score.RankStatusUnavailable
	fallbackScore.TierLabel = ""
	output := Markdown(data, fallbackScore)
	for _, marker := range []string{"ecs VPS Benchmark Report", "Memory Benchmark", "Structured failures", "Standard benchmark", "Evidence coverage", "Composite score", "Leaderboard rank unavailable", "all sizes", "raw output 192.0.2.10", "采样", "The official STREAM executable", "```en"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("Markdown missing %q:\n%s", marker, output)
		}
	}
	if strings.Contains(output, "<script>alert(1)</script>") || strings.Contains(output, "javascript:alert(1)") || !strings.Contains(output, "&lt;script&gt;alert\\(1\\)&lt;/script&gt;") {
		t.Fatalf("Markdown safety contract failed:\n%s", output)
	}
	if strings.Count(output, "```") != 2 || !strings.Contains(output, "``\\` <payload>unsafe</payload>") {
		t.Fatalf("Markdown raw output fence was not kept safe:\n%s", output)
	}
	if !strings.Contains(output, "[kernel](https://example.test/source)") || !strings.Contains(output, "unsafe") {
		t.Fatalf("Markdown source links missing or unsafe link not rendered as text:\n%s", output)
	}
}

func TestReportLocalizesRegisteredMessageArgsAndSourceNamesWithoutMutation(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "contract", Profile: "full", Exposure: "local"},
		Results: []model.Result{{
			ID:     "network",
			Title:  "module.network.title",
			Status: model.StatusOK,
			SummaryMessages: []model.Message{model.NewMessage(
				"probe.network.summary.version",
				"4", "US", "provider/raw", "probe.network.ip_type.native", "1", "2",
			)},
			Sources: []model.Source{
				{Name: "probe.network.source_name.ipapicom", Purpose: "probe.network.source.ipapicom"},
				{Name: "provider/raw", Purpose: "raw purpose"},
			},
		}},
	}
	before, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	wantOrigin := map[i18n.Lang]string{
		i18n.LangZH: "原生 IP",
		i18n.LangEN: "Native IP",
	}
	wantPurpose := map[i18n.Lang]string{
		i18n.LangZH: "ip-api 国家、网络类型与代理字段",
		i18n.LangEN: "ip-api country, network type, and proxy fields",
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			message := renderMessage(data.Results[0].SummaryMessages[0])
			if !strings.Contains(message, "provider/raw") || !strings.Contains(message, wantOrigin[language]) || strings.Contains(message, "probe.network.ip_type.native") {
				t.Fatalf("message args were not localized/preserved: %q", message)
			}
			textOutput := Text(data, TextOptions{Color: 0, Width: 120})
			markdownOutput := Markdown(data, nil)
			htmlOutput, err := HTML(data, (*score.Report)(nil))
			if err != nil {
				t.Fatal(err)
			}
			outputs := []string{textOutput, markdownOutput, string(htmlOutput)}
			for _, output := range outputs {
				if !strings.Contains(output, wantPurpose[language]) || !strings.Contains(output, "provider/raw") || !strings.Contains(output, "raw purpose") {
					t.Fatalf("raw provider/source text was not preserved: %q", output)
				}
				if strings.Contains(output, "probe.network.source_name.ipapicom") || strings.Contains(output, "probe.network.source.ipapicom") {
					t.Fatalf("stable source key leaked: %q", output)
				}
			}
			after, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("rendering mutated canonical report")
			}
		})
	}
}

func TestResultSummaryUsesExplicitMultifamilySeparator(t *testing.T) {
	summaryMessages := []model.Message{
		model.NewMessage("probe.network.summary.version", "4", "US", "provider/raw", "probe.network.ip_type.native", "0", "0"),
		model.NewMessage("probe.network.summary.version.additional", "6", "US", "provider/raw", "probe.network.ip_type.native", "0", "0"),
	}
	single := model.Result{SummaryMessages: summaryMessages[:1]}
	dual := model.Result{SummaryMessages: summaryMessages}
	wantSingle := map[i18n.Lang]string{
		i18n.LangZH: "IPv4：US · provider/raw · 原生 IP · 数据源 0/0",
		i18n.LangEN: "IPv4: US · provider/raw · Native IP · sources 0/0",
	}
	wantDual := map[i18n.Lang]string{
		i18n.LangZH: "IPv4：US · provider/raw · 原生 IP · 数据源 0/0；IPv6：US · provider/raw · 原生 IP · 数据源 0/0",
		i18n.LangEN: "IPv4: US · provider/raw · Native IP · sources 0/0; IPv6: US · provider/raw · Native IP · sources 0/0",
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		if got := resultSummary(single); got != wantSingle[language] {
			t.Fatalf("%s single-family summary = %q, want %q", language, got, wantSingle[language])
		}
		if got := resultSummary(dual); got != wantDual[language] {
			t.Fatalf("%s dual-family summary = %q, want %q", language, got, wantDual[language])
		}
	}
}
