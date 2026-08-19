package model

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"reflect"
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
	Evidence     *Evidence     `json:"evidence,omitempty"`
	Failures     []Failure     `json:"failures,omitempty"`
	Retry        *RetryInfo    `json:"retry,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// RetryInfo records a single interference-triggered benchmark retry. The
// selected attempt is chosen by environment interference, never by whichever
// benchmark score is higher.
type RetryInfo struct {
	Triggered       bool           `json:"triggered"`
	SelectedAttempt int            `json:"selected_attempt"`
	SelectionRule   string         `json:"selection_rule"`
	TriggerReasons  []string       `json:"trigger_reasons,omitempty"`
	Attempts        []RetryAttempt `json:"attempts"`
}

type RetryAttempt struct {
	Number       int           `json:"number"`
	Status       Status        `json:"status"`
	DurationMS   int64         `json:"duration_ms"`
	Evidence     *Evidence     `json:"evidence,omitempty"`
	Interference Interference  `json:"interference"`
	Measurements []Measurement `json:"measurements,omitempty"`
}

// Interference is a structured environment assessment for one benchmark
// attempt. Measurements contain load, steal, PSI and cgroup counter deltas
// using stable keys; Reasons are concise human-readable trigger explanations.
type Interference struct {
	Detected     bool          `json:"detected"`
	Score        int           `json:"score"`
	Reasons      []string      `json:"reasons,omitempty"`
	Measurements []Measurement `json:"measurements,omitempty"`
}

// FailureCategory is a stable machine-readable explanation for an operation
// that did not produce usable evidence.  Human-readable command output stays
// in Message; consumers should branch on Category rather than parsing it.
type FailureCategory string

const (
	FailureTimeout            FailureCategory = "timeout"
	FailureDNS                FailureCategory = "dns_error"
	FailureConnectionRefused  FailureCategory = "connection_refused"
	FailureNetworkUnreachable FailureCategory = "network_unreachable"
	FailureRateLimited        FailureCategory = "rate_limited"
	FailureHTTPRejected       FailureCategory = "http_rejected"
	FailureTLS                FailureCategory = "tls_error"
	FailureParse              FailureCategory = "parse_error"
	FailureToolMissing        FailureCategory = "tool_missing"
	FailurePermissionDenied   FailureCategory = "permission_denied"
	FailureUnsupported        FailureCategory = "unsupported"
	FailureCanceled           FailureCategory = "cancelled"
	FailureUnknown            FailureCategory = "unknown"
)

// Failure records a failed observation without conflating it with the module
// status or evidence grade. Count collapses repeated identical sample failures
// so a DNS or latency module cannot bloat a report with one row per attempt.
type Failure struct {
	Category  FailureCategory `json:"category"`
	Stage     string          `json:"stage,omitempty"`
	Target    string          `json:"target,omitempty"`
	Retryable bool            `json:"retryable"`
	Count     int             `json:"count,omitempty"`
	Message   string          `json:"message,omitempty"`
}

// AddFailure appends or coalesces a structured failure. Machine dimensions
// form the identity; Message remains diagnostic context and is not parsed by
// downstream consumers.
func (r *Result) AddFailure(failure Failure) {
	if failure.Category == "" {
		failure.Category = FailureUnknown
	}
	if failure.Count <= 0 {
		failure.Count = 1
	}
	for index := range r.Failures {
		current := &r.Failures[index]
		if current.Category == failure.Category && current.Stage == failure.Stage &&
			current.Target == failure.Target && current.Retryable == failure.Retryable &&
			current.Message == failure.Message {
			current.Count += failure.Count
			return
		}
	}
	r.Failures = append(r.Failures, failure)
}

// Evidence records how much of a module's planned observation set produced a
// usable verdict.  It is deliberately separate from Status: a module may
// finish with a warning while still returning every planned sample, or may be
// marked OK with only part of an optional observation set available.
type Evidence struct {
	Valid    int           `json:"valid"`
	Expected int           `json:"expected"`
	Unit     string        `json:"unit,omitempty"`
	Grade    EvidenceGrade `json:"grade,omitempty"`
}

// EvidenceGrade describes whether the planned observation set is sufficient;
// it says nothing about whether the observed value itself is good or bad.
type EvidenceGrade string

const (
	EvidenceComplete     EvidenceGrade = "complete"
	EvidencePartial      EvidenceGrade = "partial"
	EvidenceInsufficient EvidenceGrade = "insufficient"
	EvidenceNotPlanned   EvidenceGrade = "not_planned"
)

// DerivedGrade computes the canonical grade from normalized counters instead
// of trusting a stale or manually edited serialized label.
func (e Evidence) DerivedGrade() EvidenceGrade {
	switch {
	case e.Expected <= 0:
		return EvidenceNotPlanned
	case e.Valid >= e.Expected:
		return EvidenceComplete
	case e.Valid > 0:
		return EvidencePartial
	default:
		return EvidenceInsufficient
	}
}

// EffectiveGrade deliberately ignores a stale serialized grade when counters
// disagree, keeping valid/expected as the single source of truth.
func (e Evidence) EffectiveGrade() EvidenceGrade { return e.DerivedGrade() }

// Normalize clamps malformed counters and refreshes the derived grade.
func (e *Evidence) Normalize() {
	if e == nil {
		return
	}
	if e.Valid < 0 {
		e.Valid = 0
	}
	if e.Expected < 0 {
		e.Expected = 0
	}
	if e.Expected == 0 {
		e.Valid = 0
	} else if e.Valid > e.Expected {
		e.Valid = e.Expected
	}
	e.Grade = e.DerivedGrade()
}

// EvidenceRatio returns a renderer-safe coverage ratio in [0, 1].
func (e Evidence) EvidenceRatio() float64 {
	if e.Expected <= 0 || e.Valid <= 0 {
		return 0
	}
	valid := e.Valid
	if valid > e.Expected {
		valid = e.Expected
	}
	return float64(valid) / float64(e.Expected)
}

// NewEvidence normalizes counters at the probe boundary so malformed or
// partially cancelled runs cannot emit negative coverage.
func NewEvidence(valid, expected int, unit string) *Evidence {
	evidence := &Evidence{Valid: valid, Expected: expected, Unit: unit}
	evidence.Normalize()
	return evidence
}

type Methodology struct {
	Kind            string `json:"kind,omitempty"`
	Label           string `json:"label,omitempty"`
	Engine          string `json:"engine,omitempty"`
	Profile         string `json:"profile,omitempty"`
	ComparisonScope string `json:"comparison_scope,omitempty"`
	// Parameters is the stable, machine-readable workload scope used by
	// ecs compare. Human prose in Profile/ComparisonScope is never parsed to
	// decide whether two numbers are like-for-like.
	Parameters map[string]string `json:"parameters,omitempty"`
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
		summary.Headline = fmt.Sprintf(i18n.TL(i18n.LangZH, "summary.withErrors"), summary.OK, summary.Errors)
	case summary.Warnings > 0:
		summary.Status = StatusWarning
		summary.Headline = fmt.Sprintf(i18n.TL(i18n.LangZH, "summary.withWarnings"), summary.OK, summary.Warnings)
	default:
		summary.Status = StatusOK
		summary.Headline = fmt.Sprintf(i18n.TL(i18n.LangZH, "summary.allOK"), summary.OK)
	}
	if summary.Skipped > 0 {
		summary.Headline += fmt.Sprintf(i18n.TL(i18n.LangZH, "summary.skipped"), summary.Skipped)
	}
	report.Summary = summary
}

func RedactedCopy(in Report, reveal bool) Report {
	out := in
	out.Notices = append([]string(nil), in.Notices...)
	out.SensitiveIPs = append([]string(nil), in.SensitiveIPs...)
	out.Run.Requested = append([]string(nil), in.Run.Requested...)
	out.Run.OutputFormats = append([]string(nil), in.Run.OutputFormats...)
	out.Results = make([]Result, len(in.Results))
	for i, result := range in.Results {
		out.Results[i] = result
		out.Results[i].Methodology.Parameters = cloneStringMap(result.Methodology.Parameters)
		if result.Evidence != nil {
			evidence := *result.Evidence
			out.Results[i].Evidence = &evidence
		}
		out.Results[i].Fields = append([]Field(nil), result.Fields...)
		out.Results[i].Measurements = append([]Measurement(nil), result.Measurements...)
		out.Results[i].Failures = append([]Failure(nil), result.Failures...)
		if result.Retry != nil {
			retry := *result.Retry
			retry.TriggerReasons = append([]string(nil), result.Retry.TriggerReasons...)
			retry.Attempts = make([]RetryAttempt, len(result.Retry.Attempts))
			for attemptIndex, attempt := range result.Retry.Attempts {
				retry.Attempts[attemptIndex] = attempt
				retry.Attempts[attemptIndex].Measurements = append([]Measurement(nil), attempt.Measurements...)
				retry.Attempts[attemptIndex].Interference.Reasons = append([]string(nil), attempt.Interference.Reasons...)
				retry.Attempts[attemptIndex].Interference.Measurements = append([]Measurement(nil), attempt.Interference.Measurements...)
				if attempt.Evidence != nil {
					evidence := *attempt.Evidence
					retry.Attempts[attemptIndex].Evidence = &evidence
				}
			}
			out.Results[i].Retry = &retry
		}
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

// maskIP 遮盖一个地址。
//
// IPv4 保留前两段（/16）：足以看出运营商与大致归属，又不能定位到具体主机。
//
// IPv6 只保留前两组（/32）。这里刻意比 IPv4 更激进：VPS 提供商普遍按 /64
// 给每台机器分配一整个子网，保留前四组等于把实例本身指认出来，遮盖形同虚设。
// /32 通常对应分配给运营商的大块，与 IPv4 的 /16 是同一量级的信息。
func maskIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.x.x", v4[0], v4[1])
	}
	if v6 := ip.To16(); v6 != nil {
		return fmt.Sprintf(
			"%x:%x:x:x:x:x:x:x",
			binary.BigEndian.Uint16(v6[0:2]),
			binary.BigEndian.Uint16(v6[2:4]),
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
	if report == nil || len(selected) == 0 {
		return
	}
	redactStringValues(reflect.ValueOf(report).Elem(), selected)
}

// redactStringValues walks every exported string value in the report schema.
// Keeping redaction at the schema boundary prevents a newly added diagnostic
// field from silently bypassing the local-IP promise. Map keys are stable
// machine identifiers and are deliberately preserved; map string values are
// redacted like ordinary fields.
func redactStringValues(value reflect.Value, selected map[string]struct{}) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			redactStringValues(value.Elem(), selected)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.CanSet() {
				redactStringValues(field, selected)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			redactStringValues(value.Index(index), selected)
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key()
			item := iterator.Value()
			if item.Kind() == reflect.String {
				masked := reflect.New(item.Type()).Elem()
				masked.SetString(maskSelectedIPsInText(item.String(), selected))
				value.SetMapIndex(key, masked)
			}
		}
	case reflect.String:
		value.SetString(maskSelectedIPsInText(value.String(), selected))
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
