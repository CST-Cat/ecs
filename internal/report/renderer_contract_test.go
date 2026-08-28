package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

func TestHTMLRendererEscapesUntrustedReportText(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].Fields = append(data.Results[0].Fields, model.Field{Key: "unsafe", Label: "unsafe", Value: model.RawValue("<field>unsafe</field>")})
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

func TestFieldValueDisplayPreservesExplicitVariant(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	const key = "probe.network.status.ok"
	if got := displayValue(model.RawValue(key)); got != key {
		t.Fatalf("raw field value was translated: got %q, want %q", got, key)
	}
	if got, want := displayValue(model.KeyValue(key)), i18n.T(key); got != want {
		t.Fatalf("key field value was not translated: got %q, want %q", got, want)
	}
}

func TestMeasurementDisplayPreservesExplicitVariant(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	const key = "probe.network.status.ok"
	raw := displayMeasurement(model.Measurement{Display: model.RawValue(key)})
	if got := displayValue(raw.Display); got != key {
		t.Fatalf("raw measurement display was translated: got %q, want %q", got, key)
	}
	keyMeasurement := displayMeasurement(model.Measurement{Display: model.KeyValue(key)})
	if got, want := displayValue(keyMeasurement.Display), i18n.T(key); got != want {
		t.Fatalf("key measurement display was not translated: got %q, want %q", got, want)
	}
}

func TestTableCellDisplayPreservesExplicitVariantAndCanonicalInput(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	const key = "probe.network.status.ok"
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: "table", Profile: "test", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID: "table", Title: "Table", Status: model.StatusOK,
			Tables: []model.Table{{
				Key: "table.values", Title: "Values",
				Columns: []model.TableColumn{{Key: "key_value", Label: "Key value"}, {Key: "raw_value", Label: "Raw value"}},
				Rows:    [][]model.Value{{model.KeyValue(key), model.RawValue(key)}},
			}},
		}},
	}
	canonical, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	textOutput := Text(data, TextOptions{Color: termcolor.LevelNone})
	markdownOutput := Markdown(data, nil)
	htmlOutput, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	translated := i18n.T(key)
	for name, output := range map[string]string{
		"text": string(textOutput), "markdown": markdownOutput, "html": string(htmlOutput),
	} {
		if !strings.Contains(output, key) || !strings.Contains(output, translated) {
			t.Fatalf("%s table output did not preserve raw/key display: %s", name, output)
		}
	}
	after, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, after) {
		t.Fatalf("table rendering changed canonical report:\nbefore=%s\nafter=%s", canonical, after)
	}
	if raw, ok := data.Results[0].Tables[0].Rows[0][1].Raw(); !ok || raw != key {
		t.Fatalf("raw table cell changed variant or text: %q, %v", raw, ok)
	}
	if stableKey, ok := data.Results[0].Tables[0].Rows[0][0].Key(); !ok || stableKey != key {
		t.Fatalf("key table cell changed variant or text: %q, %v", stableKey, ok)
	}
}

func TestMarkdownRendersRichReportAndSafeLinks(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := textSampleReport()
	data.Results[0].Fields = append(data.Results[0].Fields, model.Field{Key: "unsafe", Label: "unsafe", Value: model.RawValue("<field>unsafe</field>")})
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

func TestReportRendersNetworkSummaryKeyArgAndKeepsOtherMessageArgsRaw(t *testing.T) {
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
	wantPurpose := map[i18n.Lang]string{
		i18n.LangZH: "ip-api 国家、网络类型与代理字段",
		i18n.LangEN: "ip-api country, network type, and proxy fields",
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			message := renderMessage(data.Results[0].SummaryMessages[0])
			if !strings.Contains(message, "provider/raw") || !strings.Contains(message, i18n.T("probe.network.ip_type.native")) || strings.Contains(message, "probe.network.ip_type.native") {
				t.Fatalf("network summary key arg was not rendered explicitly: %q", message)
			}
			plain := renderMessage(model.NewMessage("message.summary.withWarnings", "probe.network.ip_type.native", "1"))
			if !strings.Contains(plain, "probe.network.ip_type.native") || strings.Contains(plain, i18n.T("probe.network.ip_type.native")) {
				t.Fatalf("ordinary message arg was translated: %q", plain)
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

func TestValuePresentationKeepsRawAndTranslatesKeyAcrossRenderers(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	const rawKey = "probe.network.ip_type.native"
	const errorKey = "probe.network.status.ok"
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "value-render", Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusWarning},
		Results: []model.Result{{
			ID: "value", Title: "Value presentation", Status: model.StatusWarning,
			Error: errorKey,
			Fields: []model.Field{
				{Key: "raw", Label: "Raw", Value: model.RawValue(rawKey)},
				{Key: "key", Label: "Key", Value: model.KeyValue(rawKey)},
			},
		}},
	}
	outputs := map[string]string{
		"text":     string(Text(data, TextOptions{Color: termcolor.LevelNone})),
		"markdown": Markdown(data, nil),
	}
	html, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs["html"] = string(html)
	translated := i18n.T(rawKey)
	for format, output := range outputs {
		if !strings.Contains(output, rawKey) {
			t.Errorf("%s renderer translated raw Value %q: %s", format, rawKey, output)
		}
		if !strings.Contains(output, translated) {
			t.Errorf("%s renderer did not translate Key Value %q to %q: %s", format, rawKey, translated, output)
		}
		if !strings.Contains(output, errorKey) {
			t.Errorf("%s renderer translated raw Result.Error %q: %s", format, errorKey, output)
		}
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
