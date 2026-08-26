package report

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
)

func TestHTMLRendererEscapesUntrustedReportText(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].Fields = append(data.Results[0].Fields, model.Field{Key: "unsafe", Label: "unsafe", Value: "<field>unsafe</field>"})
	data.Results[0].SummaryMessages = []model.Message{model.NewMessage("message.summary.withWarnings", "<script>alert(1)</script>", "1")}
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
	data.Results[0].Fields = append(data.Results[0].Fields, model.Field{Key: "unsafe", Label: "unsafe", Value: "<field>unsafe</field>"})
	data.Results[0].SummaryMessages = []model.Message{model.NewMessage("message.summary.withWarnings", "<script>alert(1)</script>", "1")}
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

func TestResultSummaryDoesNotFallbackToLegacyInput(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	data := model.Result{ID: "fixture", Title: "module.system.title", Status: model.StatusWarning}
	if got := resultSummary(data); got != "" {
		t.Fatalf("empty structured summary rendered as %q", got)
	}
	var decoded model.Result
	if err := json.Unmarshal([]byte(`{"id":"fixture","title":"module.system.title","summary":"legacy prose"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if got := resultSummary(decoded); got != "" {
		t.Fatalf("legacy-only input rendered as %q", got)
	}
}

func TestLegacySummaryInputDoesNotRenderInAnyFormat(t *testing.T) {
	legacy := []byte(`{"schema_version":"ecs.report/v1","tool":{"name":"ecs","version":"fixture"},"run":{"id":"legacy","profile":"standard"},"summary":{"status":"warning","headline":"GLOBAL_LEGACY_SENTINEL"},"results":[{"id":"fixture","title":"module.system.title","status":"warning","summary":"RESULT_LEGACY_SENTINEL"}]}`)
	var report model.Report
	if err := json.Unmarshal(legacy, &report); err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		htmlOutput, err := HTML(report, nil)
		if err != nil {
			t.Fatal(err)
		}
		outputs := []string{
			Text(report, TextOptions{Color: 0, Width: 120}),
			Markdown(report, nil),
			string(htmlOutput),
		}
		for _, output := range outputs {
			if strings.Contains(output, "GLOBAL_LEGACY_SENTINEL") || strings.Contains(output, "RESULT_LEGACY_SENTINEL") {
				t.Fatalf("%s renderer revived legacy summary text: %q", language, output)
			}
		}
	}
}

func TestStructuredSummariesRenderAcrossFormatsWithoutMutation(t *testing.T) {
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "summary-contract", Profile: "standard", Exposure: "local"},
		Summary:       model.Summary{Status: model.StatusError, Errors: 1, Messages: []model.Message{model.NewMessage("message.summary.withErrors", 0, 1)}},
		Results: []model.Result{{
			ID:              "system",
			Title:           "module.system.title",
			Status:          model.StatusError,
			SummaryMessages: []model.Message{model.NewMessage("message.result.failed")},
		}},
	}
	before, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var canonical model.Report
	if err := json.Unmarshal(before, &canonical); err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		outputs := []string{
			Text(canonical, TextOptions{Color: 0, Width: 120}),
			Markdown(canonical, nil),
		}
		htmlOutput, err := HTML(canonical, nil)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, string(htmlOutput))
		wantGlobal := map[i18n.Lang]string{
			i18n.LangZH: "0 项成功，1 项异常",
			i18n.LangEN: "0 succeeded, 1 failed",
		}[language]
		wantResult := map[i18n.Lang]string{
			i18n.LangZH: "测试失败",
			i18n.LangEN: "Test failed",
		}[language]
		for _, output := range outputs {
			if !strings.Contains(output, wantGlobal) || !strings.Contains(output, wantResult) {
				t.Fatalf("%s structured summaries missing from output: want global %q and result %q, got %q", language, wantGlobal, wantResult, output)
			}
			if strings.Contains(output, "message.summary.") || strings.Contains(output, "message.result.") || strings.Contains(output, "%!") {
				t.Fatalf("%s summary output leaked key/format diagnostic: %q", language, output)
			}
			if language == i18n.LangEN {
				for _, character := range output {
					if unicode.Is(unicode.Han, character) {
						t.Fatalf("English summary output contains Han character %q: %q", character, output)
					}
				}
			}
		}
		after, err := json.Marshal(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("summary rendering mutated canonical report")
		}
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
