package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
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
	for _, marker := range []string{"Memory Performance", "Structured failures", "Standard benchmark", "Composite score", "Skipped", "Interrupted by user", "Not enough leaderboard samples", "The official STREAM executable"} {
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
	for _, marker := range []string{"ecs VPS Benchmark Report", "Memory Performance", "Structured failures", "Standard benchmark", "Evidence coverage", "Composite score", "Leaderboard rank unavailable", "all sizes", "raw output 192.0.2.10", "采样", "The official STREAM executable", "```en"} {
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
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "value-render", Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusWarning},
		Results: []model.Result{{
			ID: "value", Title: "Value presentation", Status: model.StatusWarning,
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
	}
}

func TestStructuredFailureRendersOneOperationalDiagnosisAcrossFormats(t *testing.T) {
	const diagnostic = "structured-failure-diagnostic"
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "failure-render", Profile: "standard", Exposure: "local"},
		Summary:       model.Summary{Status: model.StatusError, Errors: 1, Messages: []model.Message{model.NewMessage("message.summary.withErrors", 0, 1)}},
		Results: []model.Result{{
			ID: "fixture", Title: "Fixture", Status: model.StatusError,
			SummaryMessages: []model.Message{model.NewMessage("message.result.failed")},
			Failures:        []model.Failure{{Category: model.FailureConnectionRefused, Stage: "connect", Target: "fixture", Message: diagnostic}},
		}},
	}
	outputs := map[string]string{
		"text":     Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120}),
		"markdown": Markdown(data, nil),
	}
	html, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs["html"] = string(html)
	for format, output := range outputs {
		if got := strings.Count(output, diagnostic); got != 1 {
			t.Errorf("%s rendered operational diagnosis %d times, want once:\n%s", format, got, output)
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

func TestRendererStatusPresentationUsesExplicitValueVariants(t *testing.T) {
	renderer := &textRenderer{palette: termcolor.Palette{Level: termcolor.LevelBasic}}
	for _, raw := range []string{"failed", "available", "blocked", "true", "失败", "可用"} {
		if got := renderer.styledValue(raw, model.RawValue(raw)); got != raw {
			t.Fatalf("raw value %q was styled: %q", raw, got)
		}
		if got := reportValueClass(model.RawValue(raw)); got != "" {
			t.Fatalf("raw value %q received HTML class %q", raw, got)
		}
	}
	status := model.KeyValue("probe.network.status.failed")
	if got := renderer.styledValue("Failed", status); got == "Failed" || !strings.Contains(got, "\x1b") {
		t.Fatalf("explicit status key was not styled: %q", got)
	}
	if got := reportValueClass(status); got != "cell-bad" {
		t.Fatalf("explicit status key HTML class = %q, want cell-bad", got)
	}
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID: "field-preservation", Title: "Field preservation", Status: model.StatusOK,
			Fields: []model.Field{{Key: "command_args", Label: "Command arguments", Value: model.RawValue("--target example")}},
		}},
	}
	if output := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120}); !strings.Contains(output, "--target example") {
		t.Fatalf("implementation-looking field was hidden or changed: %s", output)
	}
}

func TestRawValuesRemainNeutralAcrossTextAndHTML(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	const stableKey = "probe.network.status.failed"
	rawValues := []string{
		"failed-provider.example",
		"available-host",
		"trueNAS",
		"blocked.example",
		"failed",
		"available",
		"blocked",
		"true",
	}
	fields := make([]model.Field, 0, len(rawValues))
	measurements := make([]model.Measurement, 0, len(rawValues))
	rows := make([][]model.Value, 0, len(rawValues)+1)
	for index, raw := range rawValues {
		fields = append(fields, model.Field{Key: fmt.Sprintf("raw_field_%d", index), Label: fmt.Sprintf("Raw field %d", index), Value: model.RawValue(raw)})
		measurements = append(measurements, model.Measurement{Key: fmt.Sprintf("raw_measurement_%d", index), Label: fmt.Sprintf("Raw measurement %d", index), Display: model.RawValue(raw)})
		rows = append(rows, []model.Value{model.RawValue(raw)})
	}
	rows = append(rows, []model.Value{model.KeyValue(stableKey)})
	table := model.Table{
		Key:     "raw-value-attack",
		Title:   "Raw value attack",
		Columns: []model.TableColumn{{Key: "value", Label: "Value"}},
		Rows:    rows,
	}
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "raw-value-attack", Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID:           "raw-value-attack",
			Title:        "Raw value attack",
			Status:       model.StatusOK,
			Measurements: measurements,
			Fields:       fields,
			Tables:       []model.Table{table},
		}},
	}

	plain := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120})
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("non-colored text output contains ANSI escape: %q", plain)
	}
	colored := Text(data, TextOptions{Color: termcolor.LevelBasic, Width: 120})
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored text output did not contain ANSI styling: %q", colored)
	}
	for _, raw := range rawValues {
		assertTextTokenStyle(t, plain, raw, false)
		assertTextTokenStyle(t, colored, raw, false)
	}
	translated := i18n.T(stableKey)
	assertTextTokenStyle(t, plain, translated, false)
	assertTextTokenStyle(t, colored, translated, true)

	rowsForHTML := htmlTableRows(displayTable(table))
	if len(rowsForHTML) != len(rawValues)+1 {
		t.Fatalf("HTML table row count = %d, want %d", len(rowsForHTML), len(rawValues)+1)
	}
	for index, raw := range rawValues {
		if got := rowsForHTML[index][0]; got.Value != raw || got.Class != "" {
			t.Errorf("raw HTML row %q = %#v, want neutral exact display", raw, got)
		}
	}
	if got := rowsForHTML[len(rawValues)][0]; got.Value != translated || got.Class != "cell-bad" {
		t.Fatalf("stable HTML row = %#v, want translated cell-bad value", got)
	}
	htmlOutput, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	htmlText := string(htmlOutput)
	for _, raw := range rawValues {
		if !strings.Contains(htmlText, `<td class="">`+raw+`</td>`) {
			t.Errorf("HTML did not preserve neutral raw value %q: %s", raw, htmlText)
		}
	}
	if !strings.Contains(htmlText, `<td class="cell-bad">`+translated+`</td>`) {
		t.Fatalf("HTML did not render stable key as translated cell-bad value: %s", htmlText)
	}
}

func assertTextTokenStyle(t *testing.T, output, token string, wantStyled bool) {
	t.Helper()
	found := false
	for offset := 0; offset < len(output); {
		relative := strings.Index(output[offset:], token)
		if relative < 0 {
			break
		}
		index := offset + relative
		if ansiActiveAt(output[:index]) != wantStyled {
			t.Fatalf("text token %q styled=%v, want %v in %q", token, ansiActiveAt(output[:index]), wantStyled, output[index:minInt(len(output), index+len(token)+8)])
		}
		found = true
		offset = index + len(token)
	}
	if !found {
		t.Fatalf("text output did not contain token %q: %q", token, output)
	}
}

func ansiActiveAt(value string) bool {
	active := false
	for index := 0; index < len(value); {
		if value[index] != '\x1b' || index+1 >= len(value) || value[index+1] != '[' {
			index++
			continue
		}
		relativeEnd := strings.IndexByte(value[index+2:], 'm')
		if relativeEnd < 0 {
			break
		}
		sequence := value[index+2 : index+2+relativeEnd]
		active = sequence != "0"
		index += relativeEnd + 3
	}
	return active
}

func TestTextGroupsUseStableMachineKeys(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	result := model.Result{
		ID:    "network",
		Title: "Risk report",
		Tables: []model.Table{
			{Key: "network.egress.overview", Title: "not risk"},
			{Key: "network.ipquality.ipv4.scores", Title: "overview"},
		},
	}
	groups := textGroups(result)
	if len(groups) != 2 || groups[0].key != "network.egress" || groups[1].key != "network.risk" {
		t.Fatalf("groups = %#v, want stable egress then risk grouping", groups)
	}
}

func TestRendererContractAcrossLanguagesLayoutsAndColors(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	data := textSampleReport()
	scored := rendererScoreFixture()
	canonical, err := JSON(data)
	if err != nil {
		t.Fatalf("canonical report JSON: %v", err)
	}

	type textMode struct {
		name    string
		width   int
		compact bool
	}
	modes := []textMode{
		{name: "wide", width: 120},
		{name: "wide-compact", width: 120, compact: true},
		{name: "narrow", width: 30},
		{name: "narrow-compact", width: 30, compact: true},
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		language := language
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)

			markdownBefore := Markdown(data, scored)
			htmlBeforeBytes, err := HTML(data, scored)
			if err != nil {
				t.Fatalf("HTML before Text: %v", err)
			}
			htmlBefore := string(htmlBeforeBytes)
			assertRendererContractContent(t, "Markdown", markdownBefore)
			assertRendererContractContent(t, "HTML", htmlBefore)

			for _, mode := range modes {
				mode := mode
				t.Run(mode.name, func(t *testing.T) {
					plain := Text(data, TextOptions{
						Color: termcolor.LevelNone, Compact: mode.compact, Width: mode.width, Score: scored,
					})
					colored := Text(data, TextOptions{
						Color: termcolor.LevelBasic, Compact: mode.compact, Width: mode.width, Score: scored,
					})

					assertRendererContractContent(t, "Text", plain)
					assertRendererContractContent(t, "colored Text", colored)
					assertTextWidthContract(t, plain, mode.width)
					assertTextWidthContract(t, colored, mode.width)
					if strings.Contains(plain, "\x1b") {
						t.Fatal("color-off Text contains ANSI escape")
					}
					if !strings.Contains(colored, "\x1b[") {
						t.Fatal("color-on Text contains no ANSI styling")
					}
					if got := stripANSI(colored); got != plain {
						t.Fatalf("color changed Text content beyond presentation:\nplain=%q\ncolored=%q", plain, colored)
					}

					if mode.compact {
						if strings.Contains(plain, "raw output 192.0.2.10") || strings.Contains(plain, "sysbench-v1") {
							t.Fatalf("compact Text leaked file-only evidence details:\n%s", plain)
						}
					} else if !strings.Contains(plain, "raw output 192.0.2.10") || !strings.Contains(plain, "sysbench-v1") {
						t.Fatalf("full Text lost evidence details:\n%s", plain)
					}

					gotJSON, err := JSON(data)
					if err != nil {
						t.Fatalf("machine JSON after %s Text: %v", mode.name, err)
					}
					if !bytes.Equal(gotJSON, canonical) {
						t.Fatalf("%s Text changed machine facts:\nbefore=%s\nafter=%s", mode.name, canonical, gotJSON)
					}
				})
			}

			markdownAfter := Markdown(data, scored)
			if markdownAfter != markdownBefore {
				t.Fatalf("Text rendering changed Markdown output:\nbefore=%s\nafter=%s", markdownBefore, markdownAfter)
			}
			htmlAfterBytes, err := HTML(data, scored)
			if err != nil {
				t.Fatalf("HTML after Text: %v", err)
			}
			if string(htmlAfterBytes) != htmlBefore {
				t.Fatalf("Text rendering changed HTML output:\nbefore=%s\nafter=%s", htmlBefore, htmlAfterBytes)
			}
		})
	}
}

func assertRendererContractContent(t *testing.T, format, output string) {
	t.Helper()
	markers := []string{
		i18n.T("probe.system.field.hostname"),
		"token",
		i18n.T("probe.system.pressure.column.resource"),
		"780",
		i18n.T("probe.network.status.ok"),
		i18n.T("report.evidence"),
		"1/2",
		"50%",
		i18n.T("report.failures"),
		i18n.T("failure.timeout"),
		"api.example",
		"raw timeout",
		i18n.T("score.title"),
		"850",
		i18n.T("score.dimension.cpu"),
	}
	for _, marker := range markers {
		if !strings.Contains(output, marker) {
			t.Fatalf("%s renderer lost contract marker %q:\n%s", format, marker, output)
		}
	}
}

func assertTextWidthContract(t *testing.T, output string, requestedWidth int) {
	t.Helper()
	wantWidth := normalizeTextWidth(requestedWidth)
	for lineNumber, line := range strings.Split(output, "\n") {
		if got := textwidth.Width(line); got > wantWidth {
			t.Fatalf("Text line %d width=%d exceeds requested width=%d: %q", lineNumber+1, got, wantWidth, line)
		}
	}
}

func stripANSI(value string) string {
	var output strings.Builder
	for index := 0; index < len(value); {
		if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '[' {
			index += 2
			for index < len(value) {
				character := value[index]
				index++
				if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
					break
				}
			}
			continue
		}
		output.WriteByte(value[index])
		index++
	}
	return output.String()
}
