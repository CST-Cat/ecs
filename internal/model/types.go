package model

import "time"

type Status string

const (
	StatusOK      Status = "ok"
	StatusWarning Status = "warning"
	StatusSkipped Status = "skipped"
	StatusError   Status = "error"
)

type Report struct {
	SchemaVersion string    `json:"schema_version"`
	Tool          ToolInfo  `json:"tool"`
	Run           RunInfo   `json:"run"`
	Results       []Result  `json:"results"`
	Summary       Summary   `json:"summary"`
	Notices       []Message `json:"notices,omitempty"`
	// SensitiveIPs is an in-memory allow-list of this machine's addresses. It
	// is never serialized; RedactedCopy uses it for exact local-IP scrubbing.
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
	// Exposure is the maximum external-contact level for this run.
	Exposure string `json:"exposure"`
	// Offline is retained as a derived compatibility field for consumers.
	Offline       bool     `json:"offline"`
	IPVersion     string   `json:"ip_version,omitempty"`
	Redacted      bool     `json:"redacted"`
	Canceled      bool     `json:"canceled,omitempty"`
	Requested     []string `json:"requested_modules"`
	OutputFormats []string `json:"output_formats"`
}

type Summary struct {
	Status   Status    `json:"status"`
	OK       int       `json:"ok"`
	Warnings int       `json:"warnings"`
	Skipped  int       `json:"skipped"`
	Errors   int       `json:"errors"`
	Messages []Message `json:"messages,omitempty"`
}

type Result struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Description     string        `json:"description,omitempty"`
	Methodology     Methodology   `json:"methodology,omitempty"`
	Status          Status        `json:"status"`
	SummaryMessages []Message     `json:"summary_messages,omitempty"`
	StartedAt       time.Time     `json:"started_at"`
	DurationMS      int64         `json:"duration_ms"`
	Fields          []Field       `json:"fields,omitempty"`
	Measurements    []Measurement `json:"measurements,omitempty"`
	Tables          []Table       `json:"tables,omitempty"`
	TextBlocks      []TextBlock   `json:"text_blocks,omitempty"`
	Notes           []string      `json:"notes,omitempty"`
	Sources         []Source      `json:"sources,omitempty"`
	Evidence        *Evidence     `json:"evidence,omitempty"`
	Failures        []Failure     `json:"failures,omitempty"`
	Interference    *Interference `json:"interference,omitempty"`
	Retry           *RetryInfo    `json:"retry,omitempty"`
	Error           string        `json:"error,omitempty"`
}

// RetryInfo records one interference-triggered benchmark retry.
type RetryInfo struct {
	Triggered       bool           `json:"triggered"`
	SelectedAttempt int            `json:"selected_attempt"`
	SelectionRule   Message        `json:"selection_rule"`
	TriggerReasons  []Message      `json:"trigger_reasons,omitempty"`
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
// attempt. Reasons carry stable Message semantics; measurements use stable
// machine keys and localized labels at the presentation boundary.
type Interference struct {
	Detected     bool          `json:"detected"`
	Score        int           `json:"score"`
	Reasons      []Message     `json:"reasons,omitempty"`
	Measurements []Measurement `json:"measurements,omitempty"`
}

// FailureCategory is a stable machine-readable explanation for an operation
// that did not produce usable evidence.
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

// Failure records a failed observation without conflating it with module
// status or evidence grade.
type Failure struct {
	Category  FailureCategory `json:"category"`
	Stage     string          `json:"stage,omitempty"`
	Target    string          `json:"target,omitempty"`
	Retryable bool            `json:"retryable"`
	Count     int             `json:"count,omitempty"`
	Message   string          `json:"message,omitempty"`
}

type Evidence struct {
	Valid    int           `json:"valid"`
	Expected int           `json:"expected"`
	Unit     string        `json:"unit,omitempty"`
	Grade    EvidenceGrade `json:"grade,omitempty"`
}

type EvidenceGrade string

const (
	EvidenceComplete     EvidenceGrade = "complete"
	EvidencePartial      EvidenceGrade = "partial"
	EvidenceInsufficient EvidenceGrade = "insufficient"
	EvidenceNotPlanned   EvidenceGrade = "not_planned"
)

type Methodology struct {
	Kind            string `json:"kind,omitempty"`
	Label           string `json:"label,omitempty"`
	Engine          string `json:"engine,omitempty"`
	Profile         string `json:"profile,omitempty"`
	ComparisonScope string `json:"comparison_scope,omitempty"`
	// Parameters is the stable machine-readable workload scope used by
	// ecs compare; human prose is never parsed for comparability.
	Parameters map[string]string `json:"parameters,omitempty"`
}

type Field struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Value     Value  `json:"value"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

type Measurement struct {
	Key            string  `json:"key"`
	Label          string  `json:"label"`
	Value          float64 `json:"value"`
	Unit           string  `json:"unit,omitempty"`
	Display        Value   `json:"display"`
	Rating         string  `json:"rating,omitempty"`
	Method         string  `json:"method,omitempty"`
	HigherIsBetter *bool   `json:"higher_is_better,omitempty"`
}

// TableColumn contains all metadata for one table column. Columns are ordered
// by their position in Table.Columns, and every row uses that same order.
type TableColumn struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Numeric        bool   `json:"numeric,omitempty"`
	HigherIsBetter bool   `json:"higher_is_better,omitempty"`
	Sensitive      bool   `json:"sensitive,omitempty"`
}

type Table struct {
	// Key is the stable machine identifier for this table schema.
	Key     string        `json:"key,omitempty"`
	Title   string        `json:"title,omitempty"`
	Columns []TableColumn `json:"columns"`
	Rows    [][]Value     `json:"rows"`
	// RowIdentity names the stable machine key of the row-identity column.
	RowIdentity string `json:"row_identity,omitempty"`
}

type TextBlock struct {
	Title     string `json:"title,omitempty"`
	Language  string `json:"language,omitempty"`
	Content   string `json:"content"`
	Sensitive bool   `json:"sensitive,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
}

type Source struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}
