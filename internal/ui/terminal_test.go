package ui

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"ecs/internal/model"
	"ecs/internal/runner"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

func TestProgressViewNonTTYIsConciseAndDeduplicatesConcurrentEvents(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelTrueColor)
	progress := terminal.BeginProgress(2)
	events := []runner.Progress{
		{Phase: runner.PhaseStart, Index: 1, Total: 2, Title: "disk"},
		{Phase: runner.PhaseStart, Index: 2, Total: 2, Title: "network"},
		{Phase: runner.PhaseDone, Index: 1, Total: 2, Title: "disk", Result: model.Result{Status: model.StatusOK, Summary: "private probe details"}},
		// A duplicate completion must not increase the progress count.
		{Phase: runner.PhaseDone, Index: 1, Total: 2, Title: "disk", Result: model.Result{Status: model.StatusOK}},
		{Phase: runner.PhaseDone, Index: 2, Total: 2, Title: "network", Result: model.Result{Status: model.StatusOK}},
	}
	var group sync.WaitGroup
	for _, event := range events {
		group.Add(1)
		go func(event runner.Progress) {
			defer group.Done()
			progress.Update(event)
		}(event)
	}
	group.Wait()
	progress.Stop()
	progressText := output.String()
	terminal.FullReport(model.Report{
		SchemaVersion: "ecs.report/v1",
		Run:           model.RunInfo{Profile: "quick", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK, Headline: "最终报告"},
		Results: []model.Result{{
			ID: "final", Title: "final-only", Status: model.StatusOK,
			Methodology: model.Methodology{Kind: "inventory", Label: "事实采集"},
		}},
	}, nil, nil, termcolor.LevelNone)

	if progress.doneCount != 2 {
		t.Fatalf("doneCount = %d, want 2", progress.doneCount)
	}
	if strings.Contains(progressText, "\x1b") {
		t.Fatalf("non-TTY progress contains ANSI escape: %q", progressText)
	} else if !strings.Contains(progressText, "2/2") || !strings.Contains(progressText, "elapsed ") || !strings.Contains(progressText, "status: done") {
		t.Fatalf("progress output missing final state: %q", progressText)
	} else if strings.Contains(progressText, "private probe details") {
		t.Fatalf("progress output leaked probe result details: %q", progressText)
	} else if strings.Count(output.String(), "最终报告") != 1 {
		t.Fatalf("complete report should be emitted once after progress: %q", output.String())
	}
}

func TestProgressViewStopsPartialRunWithoutHanging(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	progress := terminal.BeginProgress(2)
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 2, Title: "disk"})
	progress.Stop()
	progress.Stop()
	if text := output.String(); !strings.Contains(text, "status: stopped") {
		t.Fatalf("partial progress output missing stopped state: %q", text)
	}
}

func TestProgressViewTTYRefreshesOneLine(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelBasic)
	// A bytes.Buffer is intentionally non-TTY; force the renderer branch here
	// to verify the escape sequence without requiring a platform-specific PTY.
	terminal.tty = true
	progress := terminal.BeginProgress(1)
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 1, Title: "disk"})
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 1, Title: "disk", Result: model.Result{Status: model.StatusOK}})
	progress.EndProgress()
	text := output.String()
	if !strings.Contains(text, "\r\x1b[2K") || strings.Count(text, "\n") != 1 {
		t.Fatalf("TTY progress should refresh one line: %q", text)
	}
}

func TestNetworkSummaryPrintsDetailedTables(t *testing.T) {
	var output bytes.Buffer
	terminal := New(&output, true)
	terminal.Summary(model.Report{
		Summary: model.Summary{Headline: "完成"},
		Results: []model.Result{{
			ID:     "network",
			Title:  "网络与 IP 质量",
			Status: model.StatusOK,
			Fields: []model.Field{{
				Key:   "ipv4_ip_type",
				Label: "IPv4 IP 类型",
				Value: "原生 IP",
			}},
			Tables: []model.Table{{
				Title:   "IPv4 · 风险评分",
				Columns: []string{"数据库", "风险值"},
				Rows:    [][]string{{"IPQS", "87/100"}},
			}},
		}},
	}, nil)
	text := output.String()
	for _, expected := range []string{"IP 质量明细", "原生 IP", "IPv4 · 风险评分", "IPQS", "87/100"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("terminal output missing %q:\n%s", expected, text)
		}
	}
}

func TestDisplayWidthCountsCJKAsTwoColumns(t *testing.T) {
	if got := displayWidth("IP 风险"); got != 7 {
		t.Fatalf("displayWidth = %d, want 7", got)
	}
	if got := displayWidth("IPQS"); got != 4 {
		t.Fatalf("ASCII displayWidth = %d, want 4", got)
	}
}

func TestFullReportPrintsCompleteTextAndPaths(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.FullReport(model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{Profile: "quick", StartedAt: time.Unix(0, 0).UTC(), Redacted: true},
		Summary:       model.Summary{Status: model.StatusOK, Headline: "完成"},
		Results: []model.Result{{
			ID: "full", Title: "完整结果", Status: model.StatusOK, Summary: "全部完成",
			Methodology: model.Methodology{Kind: "inventory", Label: "事实采集", Engine: "engine"},
			Measurements: []model.Measurement{
				{Key: "one", Label: "指标一", Display: "1 unit"},
				{Key: "two", Label: "指标二", Display: "2 unit"},
				{Key: "three", Label: "指标三", Display: "3 unit"},
			},
			Fields:     []model.Field{{Key: "field", Label: "字段", Value: "字段值"}},
			Tables:     []model.Table{{Title: "表格", Columns: []string{"列"}, Rows: [][]string{{"表格值"}}}},
			TextBlocks: []model.TextBlock{{Title: "文本", Content: "文本块内容"}},
			Notes:      []string{"备注"},
		}},
	}, map[string]string{"json": "/tmp/report.json", "txt": "/tmp/report.txt"}, &score.Report{Total: 800, Covered: 1, Possible: 1}, termcolor.LevelNone)
	text := output.String()
	for _, want := range []string{
		"指标一", "指标二", "指标三", "字段值", "表格值", "文本块内容", "备注",
		"完整结果", "事实采集", "综合评分", "/tmp/report.json", "/tmp/report.txt", "未上传任何报告",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("full report output missing %q:\n%s", want, text)
		}
	}
}
