package model

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"regexp"
	"strings"
	"time"

	"ecs/internal/i18n"
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
	// SensitiveIPs is an in-memory allow-list of this machine's addresses. It is
	// never serialized; RedactedCopy uses it to remove exact local-IP occurrences
	// from otherwise non-sensitive raw output without hiding remote route hops.
	SensitiveIPs []string `json:"-"`
}

type ToolInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

type RunInfo struct {
	ID          string    `json:"id"`
	Profile     string    `json:"profile"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMS  int64     `json:"duration_ms"`
	// Exposure 是本次运行允许的最高外联级别：local、public、thirdparty、any。
	Exposure string `json:"exposure"`
	// Offline 是 Exposure == "local" 的派生值，保留给既有的报告消费方。
	Offline       bool     `json:"offline"`
	IPVersion     string   `json:"ip_version,omitempty"`
	Redacted      bool     `json:"redacted"`
	Canceled      bool     `json:"canceled,omitempty"`
	Requested     []string `json:"requested_modules"`
	OutputFormats []string `json:"output_formats"`
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
	// NumericColumns lists columns whose cells contain comparable numeric values.
	// Renderers may add a data-proportional relative bar without having to guess
	// from localized labels or display strings.  The optional direction slice is
	// aligned with NumericColumns; absent entries default to higher-is-better.
	NumericColumns        []int  `json:"numeric_columns,omitempty"`
	NumericHigherIsBetter []bool `json:"numeric_higher_is_better,omitempty"`
	// SensitiveColumns 列出需要遮盖的列索引。当前生产报告只把本机 IP
	// 所在列标记为敏感，不影响远端路径列。
	SensitiveColumns []int `json:"sensitive_columns,omitempty"`
}

type TextBlock struct {
	Title    string `json:"title,omitempty"`
	Language string `json:"language,omitempty"`
	Content  string `json:"content"`
	// Sensitive 表示正文需要按调用方约定遮盖。生产报告只用于本机 IP；
	// 远端路由原文不应置位。
	Sensitive bool `json:"sensitive,omitempty"`
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
		summary.Headline = fmt.Sprintf(i18n.T("summary.withErrors"), summary.OK, summary.Errors)
	case summary.Warnings > 0:
		summary.Status = StatusWarning
		summary.Headline = fmt.Sprintf(i18n.T("summary.withWarnings"), summary.OK, summary.Warnings)
	default:
		summary.Status = StatusOK
		summary.Headline = fmt.Sprintf(i18n.T("summary.allOK"), summary.OK)
	}
	if summary.Skipped > 0 {
		summary.Headline += fmt.Sprintf(i18n.T("summary.skipped"), summary.Skipped)
	}
	report.Summary = summary
}

func RedactedCopy(in Report, reveal bool) Report {
	out := in
	out.Notices = append([]string(nil), in.Notices...)
	out.SensitiveIPs = append([]string(nil), in.SensitiveIPs...)
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
			out.Results[i].Tables[j].NumericColumns = append([]int(nil), table.NumericColumns...)
			out.Results[i].Tables[j].NumericHigherIsBetter = append([]bool(nil), table.NumericHigherIsBetter...)
			out.Results[i].Tables[j].SensitiveColumns = append([]int(nil), table.SensitiveColumns...)
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
	if !reveal {
		redactExactLocalIPs(&out, collectSensitiveIPs(in))
	}
	// The allow-list is an internal redaction aid, not report data. Clear it
	// even for --reveal so an in-memory consumer cannot accidentally expose it.
	out.SensitiveIPs = nil
	out.Run.Redacted = !reveal
	return out
}

var (
	textIPv4Pattern = regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,3})?`)
	// Match a broad IPv6 token and let net.ParseIP/ParseCIDR decide whether it
	// is really an address.  This also covers compressed forms (including ::1)
	// and prefixes, while ordinary words containing a colon are left alone.
	textIPv6Pattern = regexp.MustCompile(`(?i)[0-9a-f:]{2,}(?:/\d{1,3})?`)
)

// MaskIPsInText 遮盖明确指定为敏感的自由文本中的所有 IP。
// 生产报告使用 maskSelectedIPsInText 按本机 IP 精确匹配，不用本函数
// 处理整段路由原文。CIDR 的前缀长度会保留。
func MaskIPsInText(text string) string {
	if text == "" {
		return text
	}
	masked := textIPv4Pattern.ReplaceAllStringFunc(text, maskIPToken)
	return textIPv6Pattern.ReplaceAllStringFunc(masked, maskIPToken)
}

func Mask(value string) string {
	value = strings.TrimSpace(value)
	if ip := net.ParseIP(value); ip != nil {
		return maskIP(ip)
	}
	if _, network, err := net.ParseCIDR(value); err == nil {
		return maskCIDR(network)
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return net.JoinHostPort(maskIP(ip), port)
		}
	}
	if value == "" {
		return value
	}
	return "hidden"
}

func maskIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.x.x", v4[0], v4[1])
	}
	if v6 := ip.To16(); v6 != nil {
		return fmt.Sprintf(
			"%x:%x:%x:%x:x:x:x:x",
			binary.BigEndian.Uint16(v6[0:2]),
			binary.BigEndian.Uint16(v6[2:4]),
			binary.BigEndian.Uint16(v6[4:6]),
			binary.BigEndian.Uint16(v6[6:8]),
		)
	}
	return "hidden"
}

func maskCIDR(network *net.IPNet) string {
	if network == nil {
		return "hidden"
	}
	ones, _ := network.Mask.Size()
	if ones < 0 {
		return "hidden"
	}
	return fmt.Sprintf("%s/%d", maskIP(network.IP), ones)
}

func maskIPToken(token string) string {
	base := token
	suffix := ""
	if slash := strings.IndexByte(token, '/'); slash >= 0 {
		base, suffix = token[:slash], token[slash:]
	}
	if ip := net.ParseIP(base); ip != nil {
		return maskIP(ip) + suffix
	}
	if _, network, err := net.ParseCIDR(token); err == nil {
		return maskCIDR(network)
	}
	return token
}

func collectSensitiveIPs(report Report) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range report.SensitiveIPs {
		addIPsFromText(result, value)
	}
	for _, item := range report.Results {
		for _, field := range item.Fields {
			if field.Sensitive {
				addIPsFromText(result, field.Value)
			}
		}
		for _, block := range item.TextBlocks {
			if block.Sensitive {
				addIPsFromText(result, block.Content)
			}
		}
		for _, table := range item.Tables {
			for _, column := range table.SensitiveColumns {
				for _, row := range table.Rows {
					if column >= 0 && column < len(row) {
						addIPsFromText(result, row[column])
					}
				}
			}
		}
	}
	return result
}

func addIPsFromText(result map[string]struct{}, value string) {
	add := func(token string) string {
		base := token
		if slash := strings.IndexByte(base, '/'); slash >= 0 {
			base = base[:slash]
		}
		if ip := net.ParseIP(base); ip != nil {
			result[ip.String()] = struct{}{}
		}
		return token
	}
	textIPv4Pattern.ReplaceAllStringFunc(value, add)
	textIPv6Pattern.ReplaceAllStringFunc(value, add)
}

func maskSelectedIPsInText(value string, selected map[string]struct{}) string {
	if value == "" || len(selected) == 0 {
		return value
	}
	maskSelected := func(token string) string {
		base := token
		suffix := ""
		if slash := strings.IndexByte(base, '/'); slash >= 0 {
			base, suffix = base[:slash], base[slash:]
		}
		ip := net.ParseIP(base)
		if ip == nil {
			return token
		}
		if _, ok := selected[ip.String()]; !ok {
			return token
		}
		return maskIP(ip) + suffix
	}
	masked := textIPv4Pattern.ReplaceAllStringFunc(value, maskSelected)
	return textIPv6Pattern.ReplaceAllStringFunc(masked, maskSelected)
}

func redactExactLocalIPs(report *Report, selected map[string]struct{}) {
	report.Summary.Headline = maskSelectedIPsInText(report.Summary.Headline, selected)
	for index := range report.Notices {
		report.Notices[index] = maskSelectedIPsInText(report.Notices[index], selected)
	}
	for resultIndex := range report.Results {
		result := &report.Results[resultIndex]
		result.Description = maskSelectedIPsInText(result.Description, selected)
		result.Summary = maskSelectedIPsInText(result.Summary, selected)
		result.Error = maskSelectedIPsInText(result.Error, selected)
		for index := range result.Notes {
			result.Notes[index] = maskSelectedIPsInText(result.Notes[index], selected)
		}
		for index := range result.Fields {
			result.Fields[index].Value = maskSelectedIPsInText(result.Fields[index].Value, selected)
		}
		for index := range result.Measurements {
			result.Measurements[index].Display = maskSelectedIPsInText(result.Measurements[index].Display, selected)
		}
		for tableIndex := range result.Tables {
			for rowIndex := range result.Tables[tableIndex].Rows {
				for columnIndex := range result.Tables[tableIndex].Rows[rowIndex] {
					cell := &result.Tables[tableIndex].Rows[rowIndex][columnIndex]
					*cell = maskSelectedIPsInText(*cell, selected)
				}
			}
		}
		for index := range result.TextBlocks {
			result.TextBlocks[index].Content = maskSelectedIPsInText(result.TextBlocks[index].Content, selected)
		}
	}
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
