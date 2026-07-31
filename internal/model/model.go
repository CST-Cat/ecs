package model

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"strings"
	"time"
)

type Status string

const (
	StatusOK      Status = "ok"
	StatusWarning Status = "warning"
	StatusSkipped Status = "skipped"
	StatusError   Status = "error"
)

type Report struct {
	SchemaVersion string   `json:"schema_version"`
	Tool          ToolInfo `json:"tool"`
	Run           RunInfo  `json:"run"`
	Results       []Result `json:"results"`
	Summary       Summary  `json:"summary"`
	Notices       []string `json:"notices,omitempty"`
}

type ToolInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

type RunInfo struct {
	ID            string    `json:"id"`
	Profile       string    `json:"profile"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	DurationMS    int64     `json:"duration_ms"`
	Offline       bool      `json:"offline"`
	Redacted      bool      `json:"redacted"`
	Canceled      bool      `json:"canceled,omitempty"`
	Requested     []string  `json:"requested_modules"`
	OutputFormats []string  `json:"output_formats"`
}

type Summary struct {
	Status   Status `json:"status"`
	OK       int    `json:"ok"`
	Warnings int    `json:"warnings"`
	Skipped  int    `json:"skipped"`
	Errors   int    `json:"errors"`
	Headline string `json:"headline"`
}

type Result struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description,omitempty"`
	Methodology  Methodology   `json:"methodology,omitempty"`
	Status       Status        `json:"status"`
	Summary      string        `json:"summary,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	DurationMS   int64         `json:"duration_ms"`
	Fields       []Field       `json:"fields,omitempty"`
	Measurements []Measurement `json:"measurements,omitempty"`
	Tables       []Table       `json:"tables,omitempty"`
	TextBlocks   []TextBlock   `json:"text_blocks,omitempty"`
	Notes        []string      `json:"notes,omitempty"`
	Sources      []Source      `json:"sources,omitempty"`
	Error        string        `json:"error,omitempty"`
}

type Methodology struct {
	Kind            string `json:"kind,omitempty"`
	Label           string `json:"label,omitempty"`
	Engine          string `json:"engine,omitempty"`
	Profile         string `json:"profile,omitempty"`
	ComparisonScope string `json:"comparison_scope,omitempty"`
}

type Field struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Value     string `json:"value"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

type Measurement struct {
	Key            string  `json:"key"`
	Label          string  `json:"label"`
	Value          float64 `json:"value"`
	Unit           string  `json:"unit,omitempty"`
	Display        string  `json:"display"`
	Rating         string  `json:"rating,omitempty"`
	Method         string  `json:"method,omitempty"`
	HigherIsBetter *bool   `json:"higher_is_better,omitempty"`
}

type Table struct {
	Title   string     `json:"title,omitempty"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

type TextBlock struct {
	Title    string `json:"title,omitempty"`
	Language string `json:"language,omitempty"`
	Content  string `json:"content"`
}

type Source struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

func NewResult(id, title string) Result {
	return Result{
		ID:        id,
		Title:     title,
		Status:    StatusOK,
		StartedAt: time.Now().UTC(),
	}
}

func (r *Result) Finish(start time.Time) {
	r.StartedAt = start.UTC()
	r.DurationMS = time.Since(start).Milliseconds()
}

func (r *Result) Skip(reason string) {
	r.Status = StatusSkipped
	r.Summary = reason
}

func (r *Result) Fail(err error) {
	r.Status = StatusError
	r.Error = err.Error()
	r.Summary = "测试失败"
}

func Summarize(report *Report) {
	var summary Summary
	for _, result := range report.Results {
		switch result.Status {
		case StatusOK:
			summary.OK++
		case StatusWarning:
			summary.Warnings++
		case StatusSkipped:
			summary.Skipped++
		case StatusError:
			summary.Errors++
		}
	}
	switch {
	case summary.Errors > 0:
		summary.Status = StatusError
		summary.Headline = fmt.Sprintf("%d 项成功，%d 项异常", summary.OK, summary.Errors)
	case summary.Warnings > 0:
		summary.Status = StatusWarning
		summary.Headline = fmt.Sprintf("%d 项成功，%d 项需留意", summary.OK, summary.Warnings)
	default:
		summary.Status = StatusOK
		summary.Headline = fmt.Sprintf("%d 项测试完成", summary.OK)
	}
	if summary.Skipped > 0 {
		summary.Headline += fmt.Sprintf("，%d 项跳过", summary.Skipped)
	}
	report.Summary = summary
}

func RedactedCopy(in Report, reveal bool) Report {
	out := in
	out.Results = make([]Result, len(in.Results))
	for i, result := range in.Results {
		out.Results[i] = result
		out.Results[i].Fields = append([]Field(nil), result.Fields...)
		out.Results[i].Measurements = append([]Measurement(nil), result.Measurements...)
		out.Results[i].Notes = append([]string(nil), result.Notes...)
		out.Results[i].Sources = append([]Source(nil), result.Sources...)
		out.Results[i].TextBlocks = append([]TextBlock(nil), result.TextBlocks...)
		out.Results[i].Tables = make([]Table, len(result.Tables))
		for j, table := range result.Tables {
			out.Results[i].Tables[j] = table
			out.Results[i].Tables[j].Columns = append([]string(nil), table.Columns...)
			out.Results[i].Tables[j].Rows = make([][]string, len(table.Rows))
			for k, row := range table.Rows {
				out.Results[i].Tables[j].Rows[k] = append([]string(nil), row...)
			}
		}
		if !reveal {
			for j := range out.Results[i].Fields {
				if out.Results[i].Fields[j].Sensitive {
					out.Results[i].Fields[j].Value = Mask(out.Results[i].Fields[j].Value)
				}
			}
		}
	}
	out.Run.Redacted = !reveal
	return out
}

func Mask(value string) string {
	value = strings.TrimSpace(value)
	if ip := net.ParseIP(value); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
		}
		if v6 := ip.To16(); v6 != nil {
			return fmt.Sprintf(
				"%x:%x:%x::/48",
				binary.BigEndian.Uint16(v6[0:2]),
				binary.BigEndian.Uint16(v6[2:4]),
				binary.BigEndian.Uint16(v6[4:6]),
			)
		}
		return "hidden"
	}
	if value == "" {
		return value
	}
	return "hidden"
}

func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func FormatRate(value float64, unit string) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "n/a"
	}
	switch {
	case math.Abs(value) >= 1000:
		return fmt.Sprintf("%.0f %s", value, unit)
	case math.Abs(value) >= 100:
		return fmt.Sprintf("%.1f %s", value, unit)
	default:
		return fmt.Sprintf("%.2f %s", value, unit)
	}
}

func BoolPtr(value bool) *bool {
	return &value
}
