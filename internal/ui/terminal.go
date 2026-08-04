package ui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/runner"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

type Terminal struct {
	out   io.Writer
	color bool
	tty   bool
}

func New(out io.Writer, noColor bool) *Terminal {
	level := termcolor.Detect(isTerminal(out))
	if noColor {
		level = termcolor.LevelNone
	}
	return NewWithColor(out, level)
}

// NewWithColor 创建一个终端，显式指定终端报告的颜色能力。
// Header/Error 仍使用兼容的 ANSI 层次样式；完整报告由 reporter.Text 按
// termcolor.Level 渲染，因此 --color 的层级不会在摘要和正文之间分叉。
func NewWithColor(out io.Writer, level termcolor.Level) *Terminal {
	return &Terminal{out: out, color: level != termcolor.LevelNone, tty: isTerminal(out)}
}

// ProgressView renders a small run-level progress indicator.  It deliberately
// never renders probe results: those stay in the final FullReport after every
// module has completed.  Updates are guarded because runner callbacks may be
// delivered from parallel probe workers.
type ProgressView struct {
	terminal *Terminal
	total    int
	started  time.Time

	mu           sync.Mutex
	done         map[int]struct{}
	running      map[int]string
	doneCount    int
	errorCount   int
	warningCount int
	skippedCount int
	lastStatus   model.Status
	lastLineLen  int
	hasLine      bool
	closed       bool

	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

// BeginProgress starts the run-level progress display.  A zero-module run is
// intentionally silent and does not start a ticker, which also covers the
// ECS_PLAN_FILE planning path (that path returns before this is called).
func (terminal *Terminal) BeginProgress(total int) *ProgressView {
	view := &ProgressView{
		terminal: terminal,
		total:    total,
		started:  time.Now(),
		done:     make(map[int]struct{}),
		running:  make(map[int]string),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	if total <= 0 {
		close(view.stopped)
		return view
	}

	view.mu.Lock()
	view.renderLocked()
	view.mu.Unlock()
	if terminal.tty {
		go view.tick()
	} else {
		close(view.stopped)
	}
	return view
}

// Update consumes one runner event.  Completion is keyed by the stable module
// index, so duplicate callbacks cannot inflate the count.
func (view *ProgressView) Update(event runner.Progress) {
	if view == nil || event.Phase == "" {
		return
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.closed {
		return
	}
	if event.Total > 0 && view.total == 0 {
		view.total = event.Total
	}
	index := event.Index
	if index <= 0 {
		return
	}
	switch event.Phase {
	case runner.PhaseStart:
		if _, alreadyDone := view.done[index]; alreadyDone {
			// Out-of-order delivery cannot resurrect a completed module.
			return
		}
		view.running[index] = event.Title
	case runner.PhaseDone:
		_, alreadyDone := view.done[index]
		if !alreadyDone && view.terminal.tty && view.hasLine {
			// Keep the last live line visible as history before drawing the next
			// state. Duplicate completion callbacks must not create blank lines.
			fmt.Fprintln(view.terminal.out)
		}
		delete(view.running, index)
		if !alreadyDone {
			view.done[index] = struct{}{}
			view.doneCount++
			switch event.Result.Status {
			case model.StatusError:
				view.errorCount++
			case model.StatusWarning:
				view.warningCount++
			case model.StatusSkipped:
				view.skippedCount++
			}
		}
		view.lastStatus = event.Result.Status
	default:
		return
	}
	view.renderLocked()
}

// Stop ends the elapsed-time ticker and leaves the final progress line above
// the complete report.  It is safe to call more than once.
func (view *ProgressView) Stop() {
	if view == nil {
		return
	}
	view.stopOnce.Do(func() {
		close(view.stop)
		<-view.stopped
		view.mu.Lock()
		view.closed = true
		if view.doneCount < view.total {
			view.renderLocked()
		}
		if view.hasLine && view.terminal.tty {
			fmt.Fprintln(view.terminal.out)
		}
		view.mu.Unlock()
	})
}

// EndProgress is the descriptive alias used by callers that treat progress as
// a begin/end scope. It shares Stop's idempotent shutdown behavior.
func (view *ProgressView) EndProgress() { view.Stop() }

func (view *ProgressView) tick() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer close(view.stopped)
	for {
		select {
		case <-ticker.C:
			view.mu.Lock()
			if !view.closed {
				view.renderLocked()
			}
			view.mu.Unlock()
		case <-view.stop:
			return
		}
	}
}

func (view *ProgressView) renderLocked() {
	if view.total <= 0 {
		return
	}
	bar := progressBar(view.doneCount, view.total)
	state := view.stateLocked()
	elapsed := formatElapsed(time.Since(view.started))
	line := fmt.Sprintf("%s %d/%d  elapsed %s  %s", bar, view.doneCount, view.total, elapsed, state)
	if view.terminal.tty {
		if view.terminal.color {
			colored := view.terminal.style("36", bar) + " " + fmt.Sprintf("%d/%d  elapsed %s  %s", view.doneCount, view.total, elapsed, view.terminal.style(progressStateCode(state), state))
			fmt.Fprintf(view.terminal.out, "\r\x1b[2K%s", colored)
		} else {
			padding := ""
			if extra := view.lastLineLen - len(line); extra > 0 {
				padding = strings.Repeat(" ", extra)
			}
			fmt.Fprintf(view.terminal.out, "\r%s%s", line, padding)
		}
		view.lastLineLen = len(line)
		view.hasLine = true
		return
	}
	// Pipes and captured output get ordinary lines only: no carriage returns,
	// ANSI escapes, or per-probe result details.
	fmt.Fprintln(view.terminal.out, line)
}

func (view *ProgressView) stateLocked() string {
	if view.closed && view.doneCount < view.total {
		return "status: stopped"
	}
	if len(view.running) > 0 {
		indices := make([]int, 0, len(view.running))
		for index := range view.running {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		names := make([]string, 0, len(indices))
		for _, index := range indices {
			names = append(names, view.running[index])
		}
		return "running: " + strings.Join(names, ", ")
	}
	if view.doneCount >= view.total {
		if view.errorCount > 0 {
			return "status: error"
		}
		if view.warningCount > 0 {
			return "status: warning"
		}
		if view.skippedCount == view.doneCount && view.doneCount > 0 {
			return "status: skipped"
		}
		return "status: done"
	}
	switch view.lastStatus {
	case model.StatusError:
		return "status: error"
	case model.StatusWarning:
		return "status: warning"
	case model.StatusSkipped:
		return "status: skipped"
	}
	return "status: waiting"
}

func progressStateCode(state string) string {
	switch {
	case strings.HasPrefix(state, "status: error"):
		return "31"
	case strings.HasPrefix(state, "status: warning"):
		return "33"
	case strings.HasPrefix(state, "status: skipped"):
		return "2"
	case strings.HasPrefix(state, "status: done"):
		return "32"
	default:
		return "36"
	}
}

func progressBar(done, total int) string {
	const width = 20
	if total <= 0 {
		return "[--------------------]"
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	filled := done * width / total
	if done > 0 && filled == 0 {
		filled = 1
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := int(elapsed / time.Second)
	minutes := seconds / 60
	seconds %= 60
	if minutes < 60 {
		return fmt.Sprintf("%02d:%02d", minutes, seconds)
	}
	hours := minutes / 60
	minutes %= 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func (terminal *Terminal) Header(cfg config.Runtime, estimate config.Estimate) {
	terminal.line(terminal.style("1;36", "ecs") + " " + terminal.style("2", buildinfo.Version) + "  " + i18n.T("cli.tagline"))
	terminal.line(terminal.style("2", i18n.T("term.subtitle")))
	terminal.line("")
	terminal.line(fmt.Sprintf("%s %-10s  %s %d  %s %s", i18n.T("term.profileLine"), cfg.Profile, i18n.T("term.moduleCount"), len(cfg.Modules), i18n.T("term.estimate"), estimate.DurationText))
	ipVersion := cfg.IPVersion
	if ipVersion == "" {
		ipVersion = config.IPVersionAuto
	}
	terminal.line(fmt.Sprintf("%s %s", i18n.T("term.ipVersion"), ipVersion))
	networkBudget := fmt.Sprintf("%d MiB", estimate.NetworkMiB)
	if estimate.NetworkMiB < 0 {
		networkBudget = i18n.T("term.uncapped")
	}
	terminal.line(fmt.Sprintf("%s  %s %d MiB  %s %s", i18n.T("term.budget"), i18n.T("term.tempDisk"), estimate.DiskMiB, i18n.T("term.networkUsage"), networkBudget))
	for _, note := range estimate.Notes {
		terminal.line(terminal.style("33", i18n.T("term.hint")) + " " + note)
	}
	terminal.line("")
}

func (terminal *Terminal) Summary(data model.Report, files map[string]string) {
	terminal.line("")
	terminal.line(terminal.style("1", i18n.T("term.finished")) + "  " + data.Summary.Headline)
	for _, result := range data.Results {
		if len(result.Measurements) == 0 {
			continue
		}
		values := make([]string, 0, 2)
		for _, metric := range result.Measurements {
			if len(values) == 2 {
				break
			}
			values = append(values, metric.Label+" "+metric.Display)
		}
		label := methodologyLabel(result.Methodology)
		if label == "" {
			label = i18n.T("methodology.unlabeled")
		}
		terminal.line(fmt.Sprintf("  %-18s %-10s %s", result.Title, "["+label+"]", strings.Join(values, " · ")))
	}
	for _, result := range data.Results {
		if result.ID != "network" || len(result.Tables) == 0 {
			continue
		}
		terminal.line("")
		terminal.line(terminal.style("1;36", i18n.T("term.ipDetail")))
		for _, field := range result.Fields {
			if strings.HasSuffix(field.Key, "_ip_type") {
				terminal.line("  " + field.Label + "  " + terminal.style("1", field.Value))
			}
		}
		for _, table := range result.Tables {
			terminal.printTable(table)
		}
	}
	terminal.printFiles(files)
	terminal.printNoUpload()
}

// FullReport 在所有模块完成、脱敏、评分和文件写入后一次性输出完整报告。
// data 必须是已经按当前语言本地化的副本；结构化 measurements、fields 和 tables
// 由 reporter.Text 统一渲染。终端 txt 有意隐藏原始 text blocks、冗余 notes 与
// 方法学长说明，避免把实现细节混入模板正文。
func (terminal *Terminal) FullReport(data model.Report, files map[string]string, scored *score.Report, color termcolor.Level) {
	terminal.line("")
	text := reporter.Text(data, reporter.TextOptions{Color: color, Score: scored})
	_, _ = io.WriteString(terminal.out, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		terminal.line("")
	}
	terminal.printFiles(files)
	terminal.printNoUpload()
}

// Report 是 FullReport 的简短别名，供调用方按语义选择名称。
func (terminal *Terminal) Report(data model.Report, files map[string]string, scored *score.Report, color termcolor.Level) {
	terminal.FullReport(data, files, scored, color)
}

func (terminal *Terminal) printFiles(files map[string]string) {
	if len(files) == 0 {
		return
	}
	terminal.line("")
	terminal.line(terminal.style("1", i18n.T("term.localReports")))
	formats := make([]string, 0, len(files))
	for format := range files {
		formats = append(formats, format)
	}
	sort.Strings(formats)
	for _, format := range formats {
		terminal.line(fmt.Sprintf("  %-5s %s", strings.ToUpper(format), files[format]))
	}
}

func (terminal *Terminal) printNoUpload() {
	terminal.line("")
	terminal.line(terminal.style("2", i18n.T("term.noUpload")))
}

func (terminal *Terminal) printTable(table model.Table) {
	if len(table.Columns) == 0 {
		return
	}
	terminal.line("")
	if table.Title != "" {
		terminal.line(terminal.style("1", table.Title))
	}
	widths := make([]int, len(table.Columns))
	for index, value := range table.Columns {
		widths[index] = displayWidth(value)
	}
	for _, row := range table.Rows {
		for index := range table.Columns {
			if index < len(row) && displayWidth(row[index]) > widths[index] {
				widths[index] = displayWidth(row[index])
			}
		}
	}
	terminal.line(formatTableRow(table.Columns, widths))
	for _, row := range table.Rows {
		values := make([]string, len(table.Columns))
		copy(values, row)
		terminal.line(formatTableRow(values, widths))
	}
}

func formatTableRow(values []string, widths []int) string {
	var output strings.Builder
	output.WriteString("  ")
	for index, value := range values {
		if index > 0 {
			output.WriteString("  ")
		}
		output.WriteString(value)
		if index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", max(0, widths[index]-displayWidth(value))))
		}
	}
	return output.String()
}

func displayWidth(value string) int {
	width := 0
	for _, character := range value {
		if unicode.Is(unicode.Han, character) ||
			unicode.Is(unicode.Hiragana, character) ||
			unicode.Is(unicode.Katakana, character) ||
			unicode.Is(unicode.Hangul, character) ||
			(character >= 0xFF01 && character <= 0xFF60) ||
			(character >= 0xFFE0 && character <= 0xFFE6) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func (terminal *Terminal) Error(format string, values ...any) {
	terminal.line(terminal.style("31", i18n.T("cli.error")) + " " + fmt.Sprintf(format, values...))
}

func (terminal *Terminal) line(value string) {
	fmt.Fprintln(terminal.out, value)
}

func (terminal *Terminal) style(code, value string) string {
	if !terminal.color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

// methodologyLabel 优先按 kind 查译文，回落到探针写死的 Label。
func methodologyLabel(m model.Methodology) string {
	if m.Kind != "" {
		if key := "methodology." + m.Kind; i18n.Has(i18n.Current(), key) {
			return i18n.T(key)
		}
	}
	return m.Label
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
