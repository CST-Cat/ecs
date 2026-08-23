package ui

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/runner"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

func TestProgressViewIsSilentOnSuccessAndReportsErrorsOnce(t *testing.T) {
	var output bytes.Buffer
	view := NewWithColor(&output, termcolor.LevelNone).BeginProgress(2)

	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			view.Update(runner.Progress{Phase: runner.PhaseDone, Index: 1, Total: 2, Result: model.Result{Status: model.StatusOK}})
		}()
	}
	group.Wait()
	view.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 2, Title: "late start"})
	if output.Len() != 0 {
		t.Fatalf("successful/duplicate progress was not silent: %q", output.String())
	}
	view.Update(runner.Progress{Phase: runner.PhaseDone, Index: 2, Total: 2, Title: "disk", Result: model.Result{Status: model.StatusError}})
	if view.doneCount != 2 || strings.Count(output.String(), "error: disk") != 1 {
		t.Fatalf("progress state/output = %d/%q", view.doneCount, output.String())
	}
	view.Stop()
	view.Stop()
	before := output.String()
	view.Update(runner.Progress{Phase: runner.PhaseDone, Index: 3, Title: "late", Result: model.Result{Status: model.StatusError}})
	if view.doneCount != 2 || output.String() != before {
		t.Fatalf("closed progress accepted an update: %d/%q", view.doneCount, output.String())
	}
}

func TestTerminalGlueAndProgressPolicy(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.Header(config.Runtime{Profile: "standard", IPVersion: config.IPVersionAuto, Modules: []string{"system"}}, config.Estimate{DurationText: "1s", DiskMiB: 2, NetworkMiB: -1})
	report := model.Report{Summary: model.Summary{Messages: []model.Message{model.NewMessage("message.summary.allOK", 1)}}}
	terminal.FullReport(report, map[string]string{"md": "/tmp/z-report.md", "json": "/tmp/a-report.json"}, nil, termcolor.LevelNone)
	terminal.Error("failed: %s", "fixture")
	text := output.String()
	for _, marker := range []string{"standard", "1s", "1 tests completed", "a-report.json", "z-report.md", "fixture", i18n.T("term.noUpload")} {
		if !strings.Contains(text, marker) {
			t.Errorf("terminal output missing %q: %q", marker, text)
		}
	}
	if strings.Index(text, "a-report.json") > strings.Index(text, "z-report.md") {
		t.Fatal("local report files were not sorted")
	}

	for _, test := range []struct {
		name, term, mode string
		stdoutTTY, ci    bool
		want             bool
	}{
		{name: "not tty", term: "xterm", want: false},
		{name: "ci", term: "xterm", stdoutTTY: true, ci: true, want: false},
		{name: "plain", term: "xterm", stdoutTTY: true, mode: "plain", want: false},
		{name: "dumb", term: "dumb", stdoutTTY: true, want: false},
		{name: "live", term: "xterm", stdoutTTY: true, want: true},
	} {
		if got := progressTTYAllowed(test.stdoutTTY, test.term, test.ci, test.mode); got != test.want {
			t.Errorf("%s progress policy = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestLiveProgressErasesAndFitsNarrowTerminals(t *testing.T) {
	if got := formatProgressLine(1, 2, "00:01", "中文状态", 12); textwidth.Width(got) > 11 {
		t.Fatalf("narrow progress line width = %d/%q", textwidth.Width(got), got)
	}
	var output bytes.Buffer
	terminal := &Terminal{out: &output, tty: true, progressTTY: true, progressInterval: time.Hour, progressWidth: 12}
	view := terminal.BeginProgress(1)
	view.Update(runner.Progress{Phase: runner.PhaseStart, Index: 1, Total: 1, Title: "中文状态"})
	view.Stop()
	if !strings.Contains(output.String(), progressAnchorSequence) || !strings.Contains(output.String(), progressEraseLineSequence) {
		t.Fatalf("live progress did not render/erase: %q", output.String())
	}
}
