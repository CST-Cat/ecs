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
	Index int    `json:"index"`
	Label string `json:"label"`
	// SchemaVersion 是这份输入报告自身声明的 schema。各输入允许不同——
	// compare 跨版本时按尽力比较处理，因此消费方需要知道每一份的出处。
	SchemaVersion string    `json:"schema_version"`
	ReportID      string    `json:"report_id"`
	ToolVersion   string    `json:"tool_version"`
	Profile       string    `json:"profile"`
	StartedAt     time.Time `json:"started_at"`
	IPVersion     string    `json:"ip_version,omitempty"`
	Redacted      bool      `json:"redacted"`
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
	// Differences 说明签名里究竟哪一项不一致。只在因签名分歧而拒绝比较时填充，
	// 且只列出确实不同的分量——相同的分量不是用户在找的东西。
	//
	// 没有它的时候，"method 或参数口径不一致"这个结论是不可行动的：本项目约定
	// 工作负载语义变了就升 measurement.method，因此跨版本报告经常撞上这一条，
	// 而用户看不出到底是 method 变了、某个参数变了，还是单位换了。
	Differences []Difference `json:"differences,omitempty"`
}

// Difference 是一个具名的签名分量，以及各报告在这一项上的取值。
type Difference struct {
	// Field 取 unit / method / direction / kind / parameter:<名字>。
	Field  string            `json:"field"`
	Values []DifferenceValue `json:"values"`
}

type DifferenceValue struct {
	Report int `json:"report"`
	// Value 为空表示这份报告没有这一项——参数键在各报告间可增可减，
	// "缺这个键"本身就是一种差异，必须能表达。
	Value string `json:"value,omitempty"`
}

type Options struct {
	Labels    []string
	Tool      model.ToolInfo
	Reference int
}
