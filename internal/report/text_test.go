package report

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

func textSampleReport() model.Report {
	data := sampleReport()
	data.Run.Canceled = true
	data.Run.Requested = []string{"system", "memory", "disk", "skipped"}
	data.Summary = model.Summary{
		Status: model.StatusError, OK: 1, Warnings: 1, Skipped: 1, Errors: 1,
		Messages: []model.Message{model.NewMessage("message.summary.withErrors", 1, 1), model.NewMessage("message.summary.skipped", 1)},
	}
	start := data.Run.StartedAt
	memory := model.Result{
		ID: "memory", Title: "module.memory.title", Description: "内存带宽测量", Status: model.StatusWarning,
		StartedAt: start, DurationMS: 1500, SummaryMessages: []model.Message{model.NewMessage("message.summary.withWarnings", 1, 1)},
		Methodology: model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "stream", Profile: "standard"},
		Fields:      []model.Field{{Key: "memory_state", Label: "状态", Value: model.RawValue("需留意")}},
		Measurements: []model.Measurement{
			{Key: "copy", Label: "复制", Value: 100, Unit: "MiB/s", Display: model.RawValue("100 MiB/s"), Rating: "成功", Method: "stream-v1", HigherIsBetter: model.BoolPtr(true)},
			{Key: "scale", Label: "缩放", Value: 80, Unit: "MiB/s", Display: model.RawValue("80 MiB/s"), Rating: "需留意", Method: "stream-v1", HigherIsBetter: model.BoolPtr(true)},
		},
		Tables: []model.Table{{
			Key: "memory.samples", Title: "采样", Columns: []model.TableColumn{
				{Key: "name", Label: "项目"}, {Key: "value", Label: "数值", Numeric: true, HigherIsBetter: true},
			},
			Rows: [][]model.Value{{model.RawValue("复制"), model.RawValue("100")}, {model.RawValue("缩放"), model.RawValue("80")}},
		}},
		Evidence: model.NewEvidence(1, 1, "sample"),
	}
	disk := model.Result{
		ID: "disk", Title: "module.disk.title", Description: "磁盘测试失败", Status: model.StatusError,
		StartedAt: start, DurationMS: 2,
		Methodology:  model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "fio"},
		Measurements: []model.Measurement{{Key: "disk", Label: "吞吐", Value: 0, Unit: "MiB/s", Display: model.RawValue("0 MiB/s"), HigherIsBetter: model.BoolPtr(true)}},
		Evidence:     model.NewEvidence(0, 1, "sample"),
		Failures:     []model.Failure{{Category: model.FailurePermissionDenied, Stage: "open", Target: "/dev/test", Retryable: false, Message: "permission denied"}},
	}
	skipped := model.Result{
		ID: "skipped", Title: "可选检查", Status: model.StatusSkipped, SummaryMessages: []model.Message{model.NewMessage("message.runner.skip.offline")},
		StartedAt: start, Evidence: model.NewEvidence(0, 0, "sample"),
	}
	data.Results[0].Evidence = model.NewEvidence(1, 2, "sample")
	disk.Failures = append(disk.Failures, model.Failure{Category: "mystery"})
	data.Results = append(data.Results, memory, disk, skipped)
	return data
}

func rendererScoreFixture() *score.Report {
	return &score.Report{
		Total: 850, Ratio: 0.85, Covered: 1, Possible: 2, Complete: false,
		BaselineSource: "global", BaselineSample: 3, HostVCPU: 4, TierLabel: "4 vCPU",
		RankStatus: score.RankStatusInsufficient, RankSamples: 3, RankMinSamples: 5,
		Dimensions: []score.DimensionScore{
			{Key: "cpu", Score: 850, Ratio: 0.85, Metrics: []score.MetricScore{{Key: "cpu_single", Label: "Single", Value: 850, Unit: "events/s", Baseline: 1000, Score: 850, Ratio: 0.85}}, Groups: []score.GroupScore{{Key: "cpu", Score: 850, Ratio: 0.85, MetricCount: 1}}},
			{Key: "memory", Missing: true, MissingReason: "module", MissingMetrics: []string{"memory_copy"}},
		},
	}
}

func TestResultTitleUsesCanonicalResultTitle(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		for _, result := range []model.Result{
			{ID: "memory", Title: "module.memory.title"},
			{ID: "disk", Title: "module.disk.title"},
		} {
			if got, want := resultTitle(result), i18n.T(result.Title); got != want {
				t.Errorf("%s result %s title = %q, want canonical %q", language, result.ID, got, want)
			}
		}
	}
}

func TestTextRendersRichReportStatesAndDetails(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangZH)
	scoreReport := rendererScoreFixture()
	scoreReport.RankStatus = score.RankStatusAvailable
	scoreReport.RankSamples = 12
	scoreReport.RankMinSamples = 5
	scoreReport.TopPercent = 10.5
	output := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone, Score: scoreReport})
	for _, marker := range []string{
		"系统", "内存性能", "磁盘性能", "可选检查", "需留意", "异常", "跳过", "逻辑 CPU", "复制", "采样", "raw output 192.0.2.10",
		"api.example", "permission denied", "mystery", "证据完整度", "100% · 完整", "50% · 部分", "0% · 证据不足", "0/0 样本 · 本轮无计划样本", "评分", "排行榜参考", "排行榜前", "报告说明",
	} {
		if !strings.Contains(output, marker) {
			t.Fatalf("text report missing %q:\n%s", marker, output)
		}
	}
	if strings.Contains(output, "\x1b") {
		t.Fatal("plain text contains ANSI escapes")
	}
	if !strings.Contains(output, "█") {
		t.Fatalf("text report omitted numeric density bar:\n%s", output)
	}
	compact := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone, Compact: true, Width: 40})
	if !strings.Contains(compact, "需留意") || strings.Contains(compact, "raw output 192.0.2.10") || strings.Contains(compact, "标准基准") {
		t.Fatalf("compact text did not keep core status while omitting evidence details:\n%s", compact)
	}
}

func TestTextGroupsUseCanonicalResultTitleForDefaultGroups(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		for _, test := range []struct {
			id, title string
		}{
			{id: "memory", title: "module.memory.title"},
			{id: "disk", title: "module.disk.title"},
		} {
			result := model.Result{
				ID:    test.id,
				Title: test.title,
				Fields: []model.Field{{
					Key: "state", Label: "state", Value: model.RawValue("ready"),
				}},
			}
			groups := textGroups(result)
			if len(groups) != 1 {
				t.Fatalf("%s %s groups = %#v, want one default group", language, test.id, groups)
			}
			if got, want := groups[0].title, i18n.T(test.title); got != want {
				t.Errorf("%s %s group title = %q, want Result.Title %q", language, test.id, got, want)
			}
		}
	}
}

func TestTextRendersSparseAndNarrowLayouts(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	rich := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone, Width: 30})
	for _, line := range strings.Split(rich, "\n") {
		if textwidth.Width(line) > 30 {
			t.Fatalf("narrow text line width=%d: %q", textwidth.Width(line), line)
		}
	}
	if !strings.Contains(rich, "Memory Performance") {
		t.Fatalf("narrow report lost result title:\n%s", rich)
	}
	sparse := model.Report{
		SchemaVersion: "ecs.report/v1", Tool: model.ToolInfo{Name: "ecs", Version: "test"},
		Run:     model.RunInfo{ID: "sparse", Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary: model.Summary{Status: model.StatusSkipped, Skipped: 1, Messages: []model.Message{model.NewMessage("message.summary.skipped", 1)}},
		Results: []model.Result{{ID: "optional", Title: "Optional", Status: model.StatusSkipped}},
	}
	output := Text(sparse, TextOptions{Color: termcolor.LevelNone, Compact: true, Width: 40})
	if !strings.Contains(output, "Optional") || !strings.Contains(output, "Skipped") {
		t.Fatalf("sparse report omitted status:\n%s", output)
	}
	markdown := Markdown(sparse, nil)
	if !strings.Contains(markdown, "ecs VPS Benchmark Report") || strings.Contains(markdown, "Composite score") || strings.Contains(markdown, "Structured failures") || strings.Contains(markdown, "Raw output") {
		t.Fatalf("sparse Markdown rendered optional sections:\n%s", markdown)
	}
	i18n.Set(i18n.LangZH)
	html, err := HTML(sparse, nil)
	htmlOutput := string(html)
	if err != nil || !strings.Contains(htmlOutput, `<html lang="zh-CN">`) || !strings.Contains(htmlOutput, `<section id="optional">`) || !strings.Contains(htmlOutput, `<span class="badge skipped">`) || strings.Contains(htmlOutput, `<div class="score-card">`) || strings.Contains(htmlOutput, `<details>`) || strings.Contains(htmlOutput, `<table`) || strings.Contains(htmlOutput, `<h3>结构化失败</h3>`) || strings.Contains(htmlOutput, `<h3>Structured failures</h3>`) {
		t.Fatalf("sparse HTML rendered optional sections: %v\n%s", err, html)
	}
}
