// Package compare builds a safe, machine-readable comparison from two or more
// ecs reports. It never compares metrics across different methods, units,
// directions or machine parameter scopes. Human-facing labels, engine names
// and profile descriptions are deliberately excluded from the signature so
// reports rendered in different languages still match by stable machine keys.
package compare

import (
	"time"

	"ecs/internal/model"
)

const SchemaVersion = "ecs.compare/v1"

type Comparability string

const (
	Comparable          Comparability = "comparable"
	PartiallyComparable Comparability = "partially_comparable"
	NotComparable       Comparability = "not_comparable"
)

type Outcome string

const (
	OutcomeImproved    Outcome = "improved"
	OutcomeRegressed   Outcome = "regressed"
	OutcomeUnchanged   Outcome = "unchanged"
	OutcomeNoReference Outcome = "no_reference"
)

type Report struct {
	SchemaVersion string         `json:"schema_version"`
	Tool          model.ToolInfo `json:"tool"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Reference     int            `json:"reference_report"`
	Inputs        []Input        `json:"inputs"`
	Summary       Summary        `json:"summary"`
	Modules       []Module       `json:"modules"`
	Notices       []string       `json:"notices"`
}

type Input struct {
	Index       int       `json:"index"`
	Label       string    `json:"label"`
	ReportID    string    `json:"report_id"`
	ToolVersion string    `json:"tool_version"`
	Profile     string    `json:"profile"`
	StartedAt   time.Time `json:"started_at"`
	IPVersion   string    `json:"ip_version,omitempty"`
	Redacted    bool      `json:"redacted"`
}

type Summary struct {
	Comparability       Comparability `json:"comparability"`
	Reports             int           `json:"reports"`
	Modules             int           `json:"modules"`
	ComparableMetrics   int           `json:"comparable_metrics"`
	Improved            int           `json:"improved"`
	Regressed           int           `json:"regressed"`
	Unchanged           int           `json:"unchanged"`
	MetricIssues        int           `json:"metric_issues"`
	ObservedChanges     int           `json:"observed_changes"`
	StatusChanges       int           `json:"status_changes"`
	EvidenceChanges     int           `json:"evidence_changes"`
	MissingModuleValues int           `json:"missing_module_values"`
}

type Module struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Comparability  Comparability   `json:"comparability"`
	Statuses       []StatusValue   `json:"statuses"`
	Evidence       []EvidenceValue `json:"evidence"`
	Metrics        []Metric        `json:"metrics,omitempty"`
	Changes        []Observation   `json:"observed_changes,omitempty"`
	MetricIssues   []MetricIssue   `json:"metric_issues,omitempty"`
	MissingReports []int           `json:"missing_reports,omitempty"`
}

type StatusValue struct {
	Report    int          `json:"report"`
	Available bool         `json:"available"`
	Status    model.Status `json:"status,omitempty"`
}

type EvidenceValue struct {
	Report    int                 `json:"report"`
	Available bool                `json:"available"`
	Valid     int                 `json:"valid,omitempty"`
	Expected  int                 `json:"expected,omitempty"`
	Unit      string              `json:"unit,omitempty"`
	Grade     model.EvidenceGrade `json:"grade,omitempty"`
	Ratio     float64             `json:"ratio,omitempty"`
}

type Metric struct {
	Key            string            `json:"key"`
	Label          string            `json:"label"`
	Unit           string            `json:"unit,omitempty"`
	Method         string            `json:"method"`
	Parameters     map[string]string `json:"parameters"`
	ParameterScope string            `json:"parameter_scope"`
	HigherIsBetter bool              `json:"higher_is_better"`
	Values         []MetricValue     `json:"values"`
}

type MetricValue struct {
	Report                   int      `json:"report"`
	Available                bool     `json:"available"`
	Value                    float64  `json:"value,omitempty"`
	Display                  string   `json:"display,omitempty"`
	Best                     bool     `json:"best,omitempty"`
	Worst                    bool     `json:"worst,omitempty"`
	Rank                     int      `json:"rank,omitempty"`
	QualityRatio             float64  `json:"quality_ratio,omitempty"`
	PerformanceChangePercent *float64 `json:"performance_change_percent,omitempty"`
	Outcome                  Outcome  `json:"outcome,omitempty"`
}

type Observation struct {
	Key    string             `json:"key"`
	Label  string             `json:"label"`
	Source string             `json:"source"`
	Values []ObservationValue `json:"values"`
}

type ObservationValue struct {
	Report    int    `json:"report"`
	Available bool   `json:"available"`
	Value     string `json:"value,omitempty"`
}

type MetricIssue struct {
	Key     string `json:"key"`
	Label   string `json:"label,omitempty"`
	Reason  string `json:"reason"`
	Reports []int  `json:"reports,omitempty"`
}

type Options struct {
	Labels    []string
	Tool      model.ToolInfo
	Reference int
}
