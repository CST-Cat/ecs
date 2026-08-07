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
	"ecs/internal/textwidth"
)

type Terminal struct {
	out              io.Writer
	color            bool
	tty              bool
	progressTTY      bool
	progressInterval time.Duration
	progressWidth    int
}

const (
	// Save the beginning of the active progress region. Every redraw restores
	// this anchor and erases all physical rows occupied after a terminal resize.
	progressAnchorSequence    = "\x1b[1G\x1b7"
	progressRestoreSequence   = "\x1b8\x1b[1G"
	progressEraseLineSequence = "\x1b[2K"
	progressNextLineSequence  = "\x1b[1B"
)

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
	tty := isTerminal(out)
	return &Terminal{
		out:              out,
		color:            level != termcolor.LevelNone,
		tty:              tty,
		progressTTY:      progressTTYFromEnvironment(tty),
		progressInterval: time.Second,
	}
}

// ProgressView renders a small run-level progress indicator.  It deliberately
// never renders probe results: those stay in the final FullReport after every
// module has completed.  Updates are guarded because runner callbacks may be
// delivered from parallel probe workers.
type ProgressView struct {
	terminal *Terminal
	total    int
	started  time.Time

	mu         sync.Mutex
	done       map[int]struct{}
	running    map[int]string
	doneCount  int
	closed     bool
	live       bool
	liveLine   bool
	liveWidth  int
	stopTicker chan struct{}
	tickerDone chan struct{}
}

// BeginProgress starts the run-level progress state. A real TTY gets one live
// line refreshed by a timer; redirected and collected output stays silent
// unless a module fails.
func (terminal *Terminal) BeginProgress(total int) *ProgressView {
	view := &ProgressView{
		terminal: terminal,
		total:    total,
		started:  time.Now(),
		done:     make(map[int]struct{}),
		running:  make(map[int]string),
	}
	if terminal != nil && terminal.tty && terminal.progressTTY && total > 0 {
		view.live = true
		view.stopTicker = make(chan struct{})
		view.tickerDone = make(chan struct{})
		interval := terminal.progressInterval
		if interval <= 0 {
			interval = time.Second
		}
		go view.refreshLoop(interval)
	}
	return view
}

func (view *ProgressView) refreshLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(view.tickerDone)
	for {
		select {
		case <-ticker.C:
			view.refreshLive()
		case <-view.stopTicker:
			return
		}
	}
}

func (view *ProgressView) refreshLive() {
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.closed || !view.live {
		return
	}
	view.renderLiveLocked()
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
		view.renderLiveLocked()
	case runner.PhaseDone:
		_, alreadyDone := view.done[index]
		if alreadyDone {
			return
		}
		// Remove the transient title before committing an error line. Successful,
		// warning and skipped completions deliberately leave no progress history;
		// their details belong to the final report.
		view.clearLiveLineLocked()
		delete(view.running, index)
		view.done[index] = struct{}{}
		view.doneCount++
		if event.Result.Status == model.StatusError {
			view.renderErrorLocked(event.Title)
		}
		if len(view.running) > 0 {
			view.renderLiveLocked()
		}
	default:
		return
	}
}

// Stop stops the refresh loop and removes the transient live title. It is safe
// to call more than once, including while a callback or timer refresh is in
// flight. Cancellation is reported by the final report/exit code, not by an
// extra progress-history line.
func (view *ProgressView) Stop() {
	if view == nil {
		return
	}
	view.mu.Lock()
	if view.closed {
		view.mu.Unlock()
		return
	}
	view.closed = true
	stopTicker, tickerDone := view.stopTicker, view.tickerDone
	view.clearLiveLineLocked()
	view.mu.Unlock()
	if stopTicker != nil {
		close(stopTicker)
		<-tickerDone
	}
}

// EndProgress is the descriptive alias used by callers that treat progress as
// a begin/end scope. It shares Stop's idempotent shutdown behavior.
func (view *ProgressView) EndProgress() { view.Stop() }

func (view *ProgressView) renderErrorLocked(title string) {
	if view.total <= 0 {
		return
	}
	state := "error"
	if title = strings.TrimSpace(title); title != "" {
		state += ": " + title
	}
	elapsed := formatElapsed(time.Since(view.started))
	line := view.formatLineLocked(elapsed, state)
	fmt.Fprintln(view.terminal.out, line)
}

func (view *ProgressView) renderLiveLocked() {
	if !view.live || view.total <= 0 || view.closed {
		return
	}
	state := view.stateLocked()
	if state == "" {
		return
	}
	elapsed := formatElapsed(time.Since(view.started))
	line := view.formatLineLocked(elapsed, state)
	if view.liveLine {
		view.clearLiveLineLocked()
	}
	_, _ = io.WriteString(view.terminal.out, progressAnchorSequence)
	_, _ = io.WriteString(view.terminal.out, line)
	view.liveLine = true
	view.liveWidth = textwidth.Width(line)
}

func (view *ProgressView) formatLineLocked(elapsed, state string) string {
	columns := 0
	if view.live && view.terminal != nil {
		columns = view.terminal.progressColumns()
	}
	return formatProgressLine(view.doneCount, view.total, elapsed, state, columns)
}

func (view *ProgressView) clearLiveLineLocked() {
	if !view.live || !view.liveLine {
		return
	}
	_, _ = io.WriteString(view.terminal.out, progressRestoreSequence)
	columns := 0
	if view.terminal != nil {
		columns = view.terminal.progressColumns()
	}
	rows := progressLineRows(view.liveWidth, columns)
	for row := 0; row < rows; row++ {
		_, _ = io.WriteString(view.terminal.out, progressEraseLineSequence)
		if row+1 < rows {
			_, _ = io.WriteString(view.terminal.out, progressNextLineSequence)
		}
	}
	if rows > 1 {
		_, _ = io.WriteString(view.terminal.out, progressRestoreSequence)
	}
	view.liveLine = false
	view.liveWidth = 0
}

func progressLineRows(width, columns int) int {
	if width <= 0 || columns <= 0 {
		return 1
	}
	return max(1, (width+columns-1)/columns)
}

func (view *ProgressView) stateLocked() string {
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
		return strings.Join(names, ", ")
	}
	return ""
}

func progressBar(done, total int) string {
	return progressBarWithWidth(done, total, 20)
}

func progressBarWithWidth(done, total, width int) string {
	if width < 1 {
		return "[]"
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
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

// formatProgressLine keeps a live redraw on one physical terminal row. A line
// that reaches the final column may trigger terminal auto-wrap, so interactive
// output deliberately leaves that column unused. Roomy terminals retain the
// full bar; narrow terminals first tighten spacing, then shrink the
// bar, and only truncate the state as a last resort so elapsed time remains
// visible.
func formatProgressLine(done, total int, elapsed, state string, terminalColumns int) string {
	bar := progressBar(done, total)
	standard := fmt.Sprintf("%s %d/%d  %s  %s", bar, done, total, elapsed, state)
	if terminalColumns <= 0 {
		return standard
	}
	maxWidth := terminalColumns - 1
	if maxWidth <= 0 {
		return ""
	}
	if textwidth.Width(standard) <= maxWidth {
		return standard
	}

	count := fmt.Sprintf("%d/%d", done, total)
	core := fmt.Sprintf("%s %s %s", count, elapsed, state)
	const minBarWidth = 5
	availableBarWidth := maxWidth - textwidth.Width(core) - 1
	if availableBarWidth >= minBarWidth+2 {
		barWidth := min(20, availableBarWidth-2)
		return progressBarWithWidth(done, total, barWidth) + " " + core
	}
	if textwidth.Width(core) <= maxWidth {
		return core
	}

	fixed := count + " " + elapsed
	if stateWidth := maxWidth - textwidth.Width(fixed) - 1; stateWidth > 0 {
		return fixed + " " + textwidth.Truncate(state, stateWidth)
	}
	if textwidth.Width(fixed) <= maxWidth {
		return fixed
	}
	return textwidth.Truncate(elapsed, maxWidth)
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
	return isTerminalFile(file)
}

func (terminal *Terminal) progressColumns() int {
	if terminal == nil {
		return 0
	}
	if terminal.progressWidth > 0 {
		return terminal.progressWidth
	}
	file, ok := terminal.out.(*os.File)
	if !ok {
		return 0
	}
	return terminalFileWidth(file)
}

// progressTTYFromEnvironment keeps live progress independent from color.
// NO_COLOR is intentionally not read here: it only changes palette output.
func progressTTYFromEnvironment(stdoutTTY bool) bool {
	return progressTTYAllowed(
		stdoutTTY,
		os.Getenv("TERM"),
		strings.TrimSpace(os.Getenv("CI")) != "",
		os.Getenv("ECS_PROGRESS_MODE"),
	)
}

func progressTTYAllowed(stdoutTTY bool, term string, ci bool, mode string) bool {
	if !stdoutTTY || ci {
		return false
	}
	if mode = strings.ToLower(strings.TrimSpace(mode)); mode == "plain" || mode == "off" || mode == "disabled" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(term)) {
	case "", "dumb", "unknown":
		return false
	default:
		return true
	}
}
