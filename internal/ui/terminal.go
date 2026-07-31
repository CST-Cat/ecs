package ui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/runner"
)

type Terminal struct {
	out   io.Writer
	color bool
}

func New(out io.Writer, noColor bool) *Terminal {
	color := !noColor && os.Getenv("NO_COLOR") == "" && isTerminal(out)
	return &Terminal{out: out, color: color}
}

func (terminal *Terminal) Header(cfg config.Runtime, estimate config.Estimate) {
	terminal.line(terminal.style("1;36", "ecs") + " " + terminal.style("2", buildinfo.Version) + "  VPS 综合测试")
	terminal.line(terminal.style("2", "零广告 · 零自动上传 · 本地 JSON/Markdown/HTML"))
	terminal.line("")
	terminal.line(fmt.Sprintf("配置档 %-10s  模块 %d 项  预计 %s", cfg.Profile, len(cfg.Modules), estimate.DurationText))
	networkBudget := fmt.Sprintf("%d MiB", estimate.NetworkMiB)
	if estimate.NetworkMiB < 0 {
		networkBudget = "iperf3 按带宽计（不封顶）"
	}
	terminal.line(fmt.Sprintf("资源上限  临时磁盘 %d MiB  网络流量 %s", estimate.DiskMiB, networkBudget))
	for _, note := range estimate.Notes {
		terminal.line(terminal.style("33", "提示") + " " + note)
	}
	terminal.line("")
}

func (terminal *Terminal) Progress(event runner.Progress) {
	switch event.Phase {
	case runner.PhaseStart:
		terminal.line(fmt.Sprintf("%s [%d/%d] %s", terminal.style("36", "→"), event.Index, event.Total, event.Title))
	case runner.PhaseDone:
		icon, code := "✓", "32"
		switch event.Result.Status {
		case model.StatusWarning:
			icon, code = "!", "33"
		case model.StatusSkipped:
			icon, code = "–", "2"
		case model.StatusError:
			icon, code = "×", "31"
		}
		summary := strings.TrimSpace(event.Result.Summary)
		if summary != "" {
			summary = "  " + terminal.style("2", summary)
		}
		method := ""
		if event.Result.Methodology.Label != "" {
			method = " " + terminal.style("36", "["+event.Result.Methodology.Label+"]")
		}
		terminal.line(fmt.Sprintf("%s [%d/%d] %s%s%s", terminal.style(code, icon), event.Index, event.Total, event.Title, method, summary))
	}
}

func (terminal *Terminal) Summary(data model.Report, files map[string]string) {
	terminal.line("")
	terminal.line(terminal.style("1", "测试完成") + "  " + data.Summary.Headline)
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
		label := result.Methodology.Label
		if label == "" {
			label = "未标注"
		}
		terminal.line(fmt.Sprintf("  %-18s %-10s %s", result.Title, "["+label+"]", strings.Join(values, " · ")))
	}
	for _, result := range data.Results {
		if result.ID != "network" || len(result.Tables) == 0 {
			continue
		}
		terminal.line("")
		terminal.line(terminal.style("1;36", "IP 质量明细"))
		for _, field := range result.Fields {
			if strings.HasSuffix(field.Key, "_ip_type") {
				terminal.line("  " + field.Label + "  " + terminal.style("1", field.Value))
			}
		}
		for _, table := range result.Tables {
			terminal.printTable(table)
		}
	}
	if len(files) > 0 {
		terminal.line("")
		terminal.line(terminal.style("1", "本地报告"))
		formats := make([]string, 0, len(files))
		for format := range files {
			formats = append(formats, format)
		}
		sort.Strings(formats)
		for _, format := range formats {
			terminal.line(fmt.Sprintf("  %-5s %s", strings.ToUpper(format), files[format]))
		}
	}
	terminal.line("")
	terminal.line(terminal.style("2", "未上传任何报告；分享文件前请确认敏感字段遮盖状态。"))
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
	terminal.line(terminal.style("31", "错误") + " " + fmt.Sprintf(format, values...))
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

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
