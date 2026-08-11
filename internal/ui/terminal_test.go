package ui

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ecs/internal/model"
	"ecs/internal/runner"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
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
		SchemaVersion: "ecs.report/v2",
		Run:           model.RunInfo{Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
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
	} else if progressText != "" {
		t.Fatalf("successful non-TTY progress should stay silent: %q", progressText)
	} else if strings.Count(output.String(), "最终报告") != 1 {
		t.Fatalf("complete report should be emitted once after progress: %q", output.String())
	}
}

func TestProgressViewNonTTYWritesOnlyErrorsAndKeepsFailedTitle(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	progress := terminal.BeginProgress(2)
	if output.Len() != 0 {
		t.Fatalf("non-TTY begin should not write a live placeholder: %q", output.String())
	}
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 2, Title: "one"})
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 2, Total: 2, Title: "two"})
	if output.Len() != 0 {
		t.Fatalf("non-TTY starts should not create history lines: %q", output.String())
	}
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 2, Title: "one", Result: model.Result{Status: model.StatusError}})
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 2, Title: "one", Result: model.Result{Status: model.StatusError}})
	if lines := strings.Count(output.String(), "\n"); lines != 1 {
		t.Fatalf("one failed module should add one history line, got %d: %q", lines, output.String())
	}
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 2, Total: 2, Title: "two", Result: model.Result{Status: model.StatusWarning}})
	progress.Stop()
	text := output.String()
	if lines := strings.Count(text, "\n"); lines != 1 {
		t.Fatalf("warning, duplicate and stop events should not add lines, got %d: %q", lines, text)
	}
	if !strings.Contains(text, "error: one") {
		t.Fatalf("error history lost the failed module title: %q", text)
	}
	for _, unwanted := range []string{"warning", "waiting", "skipped", "status: done", "status: stopped"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("progress retained unwanted state %q: %q", unwanted, text)
		}
	}
}

func TestProgressViewStopsPartialRunWithoutHanging(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.tty = true
	terminal.progressTTY = true
	terminal.progressInterval = time.Millisecond
	progress := terminal.BeginProgress(2)
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 2, Title: "disk"})
	done := make(chan struct{})
	go func() {
		progress.Stop()
		progress.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("live progress Stop did not return")
	}
	text := output.String()
	if strings.Contains(text, "status: stopped") || strings.Contains(text, "waiting") || strings.Contains(text, "warning") {
		t.Fatalf("partial progress should not commit a status line: %q", text)
	}
	if strings.ContainsAny(text, "\r\n") {
		t.Fatalf("stopping live progress must not commit a line: %q", text)
	}
}

func TestProgressViewTTYRefreshesElapsedWithoutTickNewlines(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelBasic)
	// The buffer stands in for the byte sink after production capability checks;
	// the real constructor enables this path only for an actual TTY.
	terminal.tty = true
	terminal.progressTTY = true
	terminal.progressInterval = time.Millisecond
	progress := terminal.BeginProgress(1)
	if output.Len() != 0 {
		t.Fatalf("begin should not emit a placeholder: %q", output.String())
	}
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 1, Title: "disk"})
	progress.mu.Lock()
	beforeTick := output.String()
	progress.mu.Unlock()
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		progress.mu.Lock()
		current := output.String()
		progress.mu.Unlock()
		if strings.Contains(current, progressRestoreSequence) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TTY ticker did not refresh: %q", current)
		}
		time.Sleep(time.Millisecond)
	}
	progress.mu.Lock()
	afterTick := output.String()
	progress.mu.Unlock()
	tickOutput := afterTick[len(beforeTick):]
	if strings.ContainsAny(tickOutput, "\r\n") {
		t.Fatalf("TTY tick wrote a line terminator: %q", tickOutput)
	}
	if !strings.Contains(tickOutput, progressRestoreSequence) || !strings.Contains(tickOutput, progressEraseLineSequence) || !strings.Contains(tickOutput, progressAnchorSequence) {
		t.Fatalf("TTY tick did not restore and clear the live region: %q", tickOutput)
	}
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 1, Title: "disk", Result: model.Result{Status: model.StatusOK}})
	progress.EndProgress()
	text := output.String()
	if strings.ContainsAny(text, "\r\n") || !strings.Contains(text, progressAnchorSequence) || !strings.Contains(text, progressRestoreSequence) || !strings.Contains(text, "disk") || strings.Contains(text, "waiting") || strings.Contains(text, "warning") || strings.Contains(text, "status:") {
		t.Fatalf("TTY progress should refresh only the transient module title: %q", text)
	}
}

func TestProgressViewTTYErrorKeepsFailedAndRunningTitles(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.tty = true
	terminal.progressTTY = true
	terminal.progressInterval = time.Hour
	terminal.progressWidth = 100
	progress := terminal.BeginProgress(2)
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 2, Title: "硬盘测试"})
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 2, Total: 2, Title: "网络测试"})
	beforeError := output.Len()
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 2, Title: "硬盘测试", Result: model.Result{Status: model.StatusError}})
	delta := output.String()[beforeError:]
	progress.Stop()

	if strings.Count(delta, "\n") != 1 || !strings.Contains(delta, "error: 硬盘测试") {
		t.Fatalf("failed module did not retain its title in one error line: %q", delta)
	}
	if !strings.Contains(delta, "网络测试") {
		t.Fatalf("remaining live module title was swallowed by the error: %q", delta)
	}
	for _, unwanted := range []string{"warning", "waiting", "status:"} {
		if strings.Contains(delta, unwanted) {
			t.Fatalf("error redraw retained unwanted state %q: %q", unwanted, delta)
		}
	}
}

func TestProgressViewTTYNarrowRefreshNeverAutoWraps(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.tty = true
	terminal.progressTTY = true
	terminal.progressInterval = time.Hour
	terminal.progressWidth = 60
	progress := terminal.BeginProgress(18)
	progress.mu.Lock()
	progress.doneCount = 8
	progress.started = time.Now().Add(-136 * time.Second)
	progress.mu.Unlock()

	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 9, Total: 18, Title: "网络吞吐"})
	progress.refreshLive()
	text := output.String()
	if strings.ContainsAny(text, "\r\n") {
		progress.Stop()
		t.Fatalf("live refresh must not commit a line: %q", text)
	}
	if !strings.Contains(text, "网络吞吐") || strings.Contains(text, "elapsed") || strings.Contains(text, "running") {
		progress.Stop()
		t.Fatalf("narrow live line lost its module or retained redundant labels: %q", text)
	}
	for _, redraw := range progressRedrawPayloads(text) {
		if width := textwidth.Width(redraw); width >= terminal.progressWidth {
			progress.Stop()
			t.Fatalf("live redraw width = %d, terminal columns = %d: %q", width, terminal.progressWidth, redraw)
		}
	}
	progress.Stop()
}

func TestProgressViewTTYResizeClearsAllReflowedRows(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.tty = true
	terminal.progressTTY = true
	terminal.progressInterval = time.Hour
	terminal.progressWidth = 60
	progress := terminal.BeginProgress(18)
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 9, Total: 18, Title: "网络吞吐"})

	progress.mu.Lock()
	oldWidth := progress.liveWidth
	beforeResize := output.Len()
	progress.mu.Unlock()
	terminal.progressWidth = 20
	progress.refreshLive()
	delta := output.String()[beforeResize:]
	wantRows := progressLineRows(oldWidth, terminal.progressWidth)
	if erased := strings.Count(delta, progressEraseLineSequence); erased != wantRows {
		progress.Stop()
		t.Fatalf("resize erased %d rows, want %d: %q", erased, wantRows, delta)
	}
	if moved := strings.Count(delta, progressNextLineSequence); moved != wantRows-1 {
		progress.Stop()
		t.Fatalf("resize moved across %d rows, want %d: %q", moved, wantRows-1, delta)
	}
	payloads := progressRedrawPayloads(delta)
	if len(payloads) == 0 || textwidth.Width(payloads[len(payloads)-1]) >= terminal.progressWidth {
		progress.Stop()
		t.Fatalf("resized redraw does not fit %d columns: %q", terminal.progressWidth, delta)
	}
	progress.Stop()
}

func TestFormatProgressLinePreservesWideFormatAndFitsNarrowTTY(t *testing.T) {
	const (
		elapsed = "02:16"
		state   = "网络吞吐"
	)
	wide := formatProgressLine(8, 18, elapsed, state, 80)
	wantWide := "[########------------] 8/18  02:16  网络吞吐"
	if wide != wantWide {
		t.Fatalf("wide progress format changed:\n got %q\nwant %q", wide, wantWide)
	}

	const columns = 60
	narrow := formatProgressLine(8, 18, elapsed, state, columns)
	if width := textwidth.Width(narrow); width >= columns {
		t.Fatalf("narrow progress width = %d, must stay below %d: %q", width, columns, narrow)
	}
	if !strings.Contains(narrow, elapsed) || !strings.Contains(narrow, "网络吞吐") {
		t.Fatalf("narrow progress must retain elapsed time and current module: %q", narrow)
	}
}

func TestProgressViewDisablesTickerForCollectorCompatibility(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	// A bytes.Buffer is intentionally non-TTY; force the TTY flag to prove that
	// even a terminal-like sink never receives timer/CR refresh writes.
	terminal.tty = true
	terminal.progressInterval = time.Millisecond
	progress := terminal.BeginProgress(2)
	time.Sleep(5 * time.Millisecond)
	if output.Len() != 0 {
		t.Fatalf("ticker/placeholder output must remain empty before completion: %q", output.String())
	}
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 2, Title: "one", Result: model.Result{Status: model.StatusOK}})
	progress.Stop()
	text := output.String()
	if text != "" {
		t.Fatalf("collector-compatible progress should stay silent without errors: %q", text)
	}
}

func TestProgressTTYCapabilityUsesOutputAndEnvironment(t *testing.T) {
	tests := []struct {
		name          string
		stdoutTTY, ci bool
		term, mode    string
		want          bool
	}{
		{name: "interactive xterm", stdoutTTY: true, term: "xterm-256color", want: true},
		{name: "curl pipe with terminal stdout", stdoutTTY: true, term: "xterm-256color", want: true},
		{name: "redirected stdout", term: "xterm-256color"},
		{name: "CI", stdoutTTY: true, term: "xterm-256color", ci: true},
		{name: "dumb terminal", stdoutTTY: true, term: "dumb"},
		{name: "explicit plain", stdoutTTY: true, term: "xterm-256color", mode: "plain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := progressTTYAllowed(test.stdoutTTY, test.term, test.ci, test.mode); got != test.want {
				t.Fatalf("progressTTYAllowed() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestProgressTTYIgnoresNOColor(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CI", "")
	t.Setenv("ECS_PROGRESS_MODE", "")
	t.Setenv("NO_COLOR", "1")
	if !progressTTYFromEnvironment(true) {
		t.Fatal("NO_COLOR must not disable live elapsed refresh")
	}
}

func TestTerminalDetectionRejectsCharacterDeviceWithoutTTYIoctl(t *testing.T) {
	file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if isTerminal(file) {
		t.Fatal("/dev/null must not enable live cursor output")
	}
}

func TestProgressViewKeepsOnlyErrorHistory(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	progress := terminal.BeginProgress(3)
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 3, Title: "one"})
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 3, Title: "one", Result: model.Result{Status: model.StatusOK}})
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 3, Title: "one", Result: model.Result{Status: model.StatusOK}})
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 2, Total: 3, Title: "two"})
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 2, Total: 3, Title: "two", Result: model.Result{Status: model.StatusWarning}})
	progress.Update(runner.Progress{Phase: runner.PhaseStart, Index: 3, Total: 3, Title: "three"})
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 3, Total: 3, Title: "three", Result: model.Result{Status: model.StatusError}})
	// Duplicate completion callbacks must not add a second error line.
	progress.Update(runner.Progress{Phase: runner.PhaseDone, Index: 3, Total: 3, Title: "three", Result: model.Result{Status: model.StatusError}})
	progress.EndProgress()

	lines := progressLines(output.String())
	if len(lines) != 1 {
		t.Fatalf("progress history lines = %d, want one error: %q", len(lines), output.String())
	}
	if !strings.Contains(lines[0], "3/3") || !strings.Contains(lines[0], "error: three") {
		t.Fatalf("error history missing count or title: %q", lines[0])
	}
	if strings.Contains(lines[0], "warning") || strings.Contains(lines[0], "waiting") {
		t.Fatalf("error history contains a discarded state: %q", lines[0])
	}
}

func progressLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	rawLines := strings.Split(text, "\n")
	lines := make([]string, len(rawLines))
	for index, line := range rawLines {
		if carriage := strings.LastIndexByte(line, '\r'); carriage >= 0 {
			line = line[carriage+1:]
		}
		lines[index] = line
	}
	return lines
}

func progressRedrawPayloads(text string) []string {
	text = strings.ReplaceAll(text, progressRestoreSequence, "\n")
	text = strings.ReplaceAll(text, progressEraseLineSequence, "")
	text = strings.ReplaceAll(text, progressNextLineSequence, "")
	text = strings.ReplaceAll(text, progressAnchorSequence, "")
	raw := strings.Split(text, "\n")
	payloads := make([]string, 0, len(raw))
	for _, value := range raw {
		if value != "" {
			payloads = append(payloads, value)
		}
	}
	return payloads
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
		SchemaVersion: "ecs.report/v2",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), Redacted: true},
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
		"指标一", "指标二", "指标三", "字段值", "表格值",
		"完整结果", "报告状态：完成", "综合评分", "/tmp/report.json", "/tmp/report.txt", "未上传任何报告",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("full report output missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"文本块内容", "备注", "事实采集", "全部完成", "engine"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("full report should hide raw/notes/methodology detail %q:\n%s", forbidden, text)
		}
	}
}

func TestFullReportUsesDetectedTerminalWidth(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelANSI256)
	terminal.progressWidth = 48
	terminal.FullReport(model.Report{
		SchemaVersion: "ecs.report/v2",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{Profile: "standard", StartedAt: time.Unix(0, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK, Headline: "completed"},
		Results: []model.Result{{
			ID: "crypto", Title: "Cryptography benchmark", Status: model.StatusOK,
			Measurements: []model.Measurement{
				{Key: "one", Label: "AES-256-GCM 1 worker", Value: 100, Unit: "MB/s", Display: "100 MB/s", HigherIsBetter: model.BoolPtr(true)},
				{Key: "all", Label: "AES-256-GCM all workers", Value: 700, Unit: "MB/s", Display: "700 MB/s", HigherIsBetter: model.BoolPtr(true)},
			},
		}},
	}, nil, nil, termcolor.LevelANSI256)
	for lineNumber, line := range strings.Split(output.String(), "\n") {
		if width := textwidth.Width(line); width > terminal.progressWidth {
			t.Fatalf("terminal line %d exceeds detected width %d: got %d: %q", lineNumber+1, terminal.progressWidth, width, line)
		}
	}
}
