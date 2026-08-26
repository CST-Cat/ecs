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
	"ecs/internal/termcolor"
)

func retryRendererFixture() model.Report {
	start := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run: model.RunInfo{
			ID: "retry-render", Profile: "standard", StartedAt: start, CompletedAt: start.Add(time.Second),
			DurationMS: 1000, Exposure: "local", Redacted: true,
		},
		Summary: model.Summary{Status: model.StatusWarning, Warnings: 1, Messages: []model.Message{model.NewMessage("message.summary.withWarnings", 0, 1)}},
		Results: []model.Result{{
			ID: "cpu", Title: "module.cpu.title", Status: model.StatusWarning, StartedAt: start,
			DurationMS: 700, SummaryMessages: []model.Message{model.NewMessage("probe.cpu.summary.single", "100 events/s")},
			Measurements: []model.Measurement{{
				Key: "cpu_steal_percent_window", Label: "probe.pressure.metric.cpu_steal_percent_window",
				Value: 7.5, Unit: "%", Display: "7.50 %", Method: "proc-stat-steal-window-v1",
			}},
			Interference: &model.Interference{
				Detected: true, Score: 5,
				Reasons: []model.Message{model.NewMessage("probe.pressure.reason.cpu_steal_high", "7.50")},
				Measurements: []model.Measurement{{
					Key: "cpu_steal_percent_window", Label: "probe.pressure.metric.cpu_steal_percent_window",
					Value: 7.5, Unit: "%", Display: "7.50 %", Method: "proc-stat-steal-window-v1",
				}},
			},
			Retry: &model.RetryInfo{
				Triggered: true, SelectedAttempt: 2,
				SelectionRule:  model.NewMessage("probe.retry.selection_rule.interference_score"),
				TriggerReasons: []model.Message{model.NewMessage("probe.pressure.reason.cpu_steal_high", "7.50")},
				Attempts: []model.RetryAttempt{
					{
						Number: 1, Status: model.StatusWarning, DurationMS: 300,
						Evidence: model.NewEvidence(1, 1, "run"),
						Interference: model.Interference{
							Detected: true, Score: 5,
							Reasons: []model.Message{model.NewMessage("probe.pressure.reason.cpu_steal_high", "7.50")},
						},
					},
					{
						Number: 2, Status: model.StatusOK, DurationMS: 400,
						Evidence:     model.NewEvidence(1, 1, "run"),
						Interference: model.Interference{Score: 1},
					},
				},
			},
			TextBlocks: []model.TextBlock{
				{Title: "probe.cpu.raw.single", Language: "text", Content: "<raw>& first", Attempt: 1},
				{Title: "probe.cpu.raw.multi", Language: "text", Content: "<raw>& second", Attempt: 2},
			},
		}},
	}
}

func TestRetryInterferenceRenderersLocalizeStructuredFactsWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		language       i18n.Lang
		markers        []string
		stablePrefixes []string
		rejectHan      bool
	}{
		{
			language: i18n.LangZH,
			markers: []string{
				"测试窗口资源干扰", "测试窗口 CPU steal 7.50%", "自动复测判定",
				"先排除无有效证据", "第 1 轮 · sysbench 单线程原始输出", "第 2 轮 · sysbench 多线程原始输出",
				"采用", "保留复核", "proc-stat-steal-window-v1",
			},
			stablePrefixes: []string{"probe.pressure.", "probe.retry.", "report.retry.", "report.interference.", "report.attempt."},
		},
		{
			language: i18n.LangEN,
			markers: []string{
				"Test-window interference", "CPU steal during the test reached 7.50%", "Automatic retry decision",
				"Exclude attempts without valid evidence", "Attempt 1 · Raw single-thread sysbench output", "Attempt 2 · Raw multi-thread sysbench output",
				"Selected", "Retained for review", "proc-stat-steal-window-v1",
			},
			stablePrefixes: []string{"probe.pressure.", "probe.retry.", "report.retry.", "report.interference.", "report.attempt."},
			rejectHan:      true,
		},
	} {
		t.Run(string(test.language), func(t *testing.T) {
			originalLanguage := i18n.Current()
			t.Cleanup(func() { i18n.Set(originalLanguage) })
			i18n.Set(test.language)
			data := retryRendererFixture()
			before, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}

			outputs := map[string]string{
				"text":     Text(data, TextOptions{Color: termcolor.LevelNone}),
				"markdown": Markdown(data, nil),
			}
			html, err := HTML(data, nil)
			if err != nil {
				t.Fatal(err)
			}
			outputs["html"] = string(html)
			for format, output := range outputs {
				for _, marker := range test.markers {
					if !strings.Contains(output, marker) {
						t.Errorf("%s output missing %q:\n%s", format, marker, output)
					}
				}
				for _, prefix := range test.stablePrefixes {
					if strings.Contains(output, prefix) {
						t.Errorf("%s output leaked stable key prefix %q:\n%s", format, prefix, output)
					}
				}
				if test.rejectHan {
					for _, character := range output {
						if unicode.Is(unicode.Han, character) {
							t.Errorf("%s English output leaked Han character %q:\n%s", format, character, output)
							break
						}
					}
				}
			}
			if !strings.Contains(outputs["text"], "<raw>& first") || !strings.Contains(outputs["markdown"], "<raw>& first") || !strings.Contains(outputs["html"], "&lt;raw&gt;&amp; first") {
				t.Errorf("raw evidence was not preserved safely: text=%q markdown=%q html=%q", outputs["text"], outputs["markdown"], outputs["html"])
			}
			after, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("renderer mutated canonical report")
			}
		})
	}
}

func TestCompactTextKeepsRetryDecision(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	output := Text(retryRendererFixture(), TextOptions{Color: termcolor.LevelNone, Compact: true, Width: 80})
	if !strings.Contains(output, "Automatic retry: selected attempt 2") {
		t.Fatalf("compact text omitted retry decision:\n%s", output)
	}
	if strings.Contains(output, "<raw>& first") || strings.Contains(output, "Selection rule") {
		t.Fatalf("compact text rendered full retry evidence:\n%s", output)
	}
}
