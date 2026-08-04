package score

// 排行榜提交格式。
//
// 完整报告不适合入库：一份 full 报告三千多行、上百 KB，一千份就是上百 MB，
// 而且里面有出口 IP、主机名、反向解析、逐跳路由——那些字段对排行榜毫无用处，
// 却足以定位一台具体机器。
//
// 所以提交是**另一种东西**，不是报告的压缩版：它只带评分需要的数值和用于分组
// 的机器规格，字段是白名单而不是黑名单。加字段要显式改这里，不会因为报告新增
// 了什么就悄悄多带出去——这个方向的默认值必须是"不带"。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

// SubmissionSchema 是提交文件的格式标识。
const SubmissionSchema = "ecs.submission/v1"

// Submission 是一份可公开入库的跑分记录。
type Submission struct {
	Schema string `json:"schema"`
	// ID 由机器规格与测量值派生，用于查重；不含任何可定位信息。
	ID    string    `json:"id"`
	Host  HostSpec  `json:"host"`
	Tool  ToolSpec  `json:"tool"`
	RanAt time.Time `json:"ran_at"`
	// Metrics 是评分用的原始实测值，键与 Dimensions() 的 Metric.Key 对应。
	Metrics map[string]float64 `json:"metrics"`
	// Profile 记录用户选中的模块预设；各档位使用相同的 full-depth 基准口径，
	// 因此分组时应按实际模块覆盖与机器规格解释，而不是按采样窗口区分。
	Profile string `json:"profile"`
	// Note 是提交者的自由备注，长度受限，用于说明特殊情况。
	Note string `json:"note,omitempty"`
}

// HostSpec 是用于分组比较的机器规格。
//
// 字段选择的标准是"能不能用来分组，而不是能不能定位"：核数与内存决定了机器
// 属于哪一档，CPU 型号让同型号之间可比。刻意不含出口 IP、主机名、反向解析、
// ASN、精确地理位置与磁盘序列号——那些既不影响分组，又能把机器指认出来。
type HostSpec struct {
	VCPU           int     `json:"vcpu"`
	MemoryGiB      float64 `json:"memory_gib"`
	Virtualization string  `json:"virtualization,omitempty"`
	CPUModel       string  `json:"cpu_model,omitempty"`
	Arch           string  `json:"arch,omitempty"`
	// Region 与 Provider 由提交者自报：从 IP 推断需要引入 IP 情报数据，
	// 而那正是这个格式要避开的东西。
	Region   string `json:"region,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// ToolSpec 记录产出这份数据的工具版本。
//
// 换了 ecs 版本或基准工具版本，数值口径可能变；不记版本的跑分库过几个月
// 就会变成一堆无法解释的数字。
type ToolSpec struct {
	ECS      string `json:"ecs"`
	Sysbench string `json:"sysbench,omitempty"`
	Fio      string `json:"fio,omitempty"`
	IPerf3   string `json:"iperf3,omitempty"`
}

// SubmissionOptions 是生成提交时的可选自报信息。
type SubmissionOptions struct {
	Region   string
	Provider string
	Note     string
}

// maxNoteLength 限制备注长度：这是一个跑分库，不是留言板。
const maxNoteLength = 200

// BuildSubmission 从完整报告提取一份可公开的提交。
func BuildSubmission(data model.Report, options SubmissionOptions) (Submission, error) {
	values := collectMeasurements(data)
	ran := make(map[string]bool, len(data.Results))
	for _, result := range data.Results {
		if result.Status != model.StatusSkipped {
			ran[result.ID] = true
		}
	}

	metrics := make(map[string]float64)
	for _, dimension := range Dimensions() {
		if !ran[dimension.ModuleID] {
			continue
		}
		for _, metric := range dimension.Metrics {
			if value, _, _, ok := resolveMetric(metric, values); ok && value > 0 {
				metrics[metric.Key] = value
			}
		}
	}
	if len(metrics) == 0 {
		return Submission{}, fmt.Errorf("report contains no scoreable measurements")
	}

	submission := Submission{
		Schema:  SubmissionSchema,
		Host:    extractHostSpec(data, values),
		Tool:    extractToolSpec(data),
		RanAt:   data.Run.StartedAt,
		Metrics: metrics,
		Profile: data.Run.Profile,
	}
	submission.Host.Region = sanitizeShort(options.Region, 32)
	submission.Host.Provider = sanitizeShort(options.Provider, 48)
	submission.Note = sanitizeShort(options.Note, maxNoteLength)
	submission.ID = submission.fingerprint()
	// Keep invalid records out of every export path, not just the repository
	// loader.  In particular, a report containing only CPU measurements has no
	// host specification and must fail before a file can be written.
	if err := submission.Validate(); err != nil {
		return Submission{}, err
	}
	return submission, nil
}

// extractHostSpec 只读白名单字段。
func extractHostSpec(data model.Report, values map[string]measured) HostSpec {
	spec := HostSpec{}
	if item, ok := values["logical_cpus"]; ok {
		spec.VCPU = int(item.value)
	}
	if item, ok := values["memory_total_bytes"]; ok {
		spec.MemoryGiB = roundTo(item.value/(1<<30), 2)
	}
	for _, result := range data.Results {
		if result.ID != "system" {
			continue
		}
		for _, field := range result.Fields {
			// 按 key 白名单取值，不按 label——label 会随语言变。
			switch field.Key {
			case "virtualization":
				spec.Virtualization = field.Value
			case "cpu_model":
				spec.CPUModel = field.Value
			case "arch", "architecture":
				spec.Arch = field.Value
			}
		}
	}
	return spec
}

func extractToolSpec(data model.Report) ToolSpec {
	spec := ToolSpec{ECS: data.Tool.Version}
	for _, result := range data.Results {
		for _, field := range result.Fields {
			if field.Key != "version" {
				continue
			}
			switch result.ID {
			case "cpu", "memory":
				if spec.Sysbench == "" {
					spec.Sysbench = shortVersion(field.Value)
				}
			case "disk":
				if spec.Fio == "" {
					spec.Fio = shortVersion(field.Value)
				}
			case "speed":
				if spec.IPerf3 == "" {
					spec.IPerf3 = shortVersion(field.Value)
				}
			}
		}
	}
	return spec
}

// fingerprint 由规格与测量值派生一个稳定 ID，用于查重。
//
// 不用随机数也不用时间戳：同一台机器重复提交同一份结果应当得到同一个 ID，
// CI 才查得出重复。反过来，这个 ID 也无法反推出机器身份。
func (s Submission) fingerprint() string {
	var builder strings.Builder
	builder.WriteString(s.Host.CPUModel)
	builder.WriteString("|")
	builder.WriteString(strconv.Itoa(s.Host.VCPU))
	builder.WriteString("|")
	builder.WriteString(strconv.FormatFloat(s.Host.MemoryGiB, 'f', 2, 64))
	builder.WriteString("|")
	builder.WriteString(s.Host.Virtualization)
	builder.WriteString("|")
	builder.WriteString(s.RanAt.UTC().Format(time.RFC3339))
	for _, key := range sortedMetricKeys(s.Metrics) {
		builder.WriteString("|")
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(strconv.FormatFloat(s.Metrics[key], 'f', 3, 64))
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// FileName 是这份提交在库里的建议文件名。
func (s Submission) FileName() string {
	parts := []string{s.ID}
	if s.Host.Provider != "" {
		parts = append(parts, slug(s.Host.Provider))
	}
	if s.Host.Region != "" {
		parts = append(parts, slug(s.Host.Region))
	}
	return strings.Join(parts, "-") + ".json"
}

// Encode 序列化提交。
func (s Submission) Encode() ([]byte, error) {
	content, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

// LoadSubmission 读入一份提交并校验。
func LoadSubmission(path string) (Submission, error) {
	var submission Submission
	file, err := os.Open(path)
	if err != nil {
		return submission, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 256*1024 {
		return submission, fmt.Errorf("submission exceeds the 256 KiB limit")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&submission); err != nil {
		return submission, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return submission, fmt.Errorf("submission must contain exactly one JSON object")
	}
	return submission, submission.Validate()
}

// Validate 检查一份提交是否可以入库。
//
// 校验的重点不是"数据好不好看"，而是"能不能被解释"：缺规格的记录无法分组，
// 指纹对不上的记录说明内容被手改过而没有重算，两者都会污染基线。
func (s Submission) Validate() error {
	if s.Schema != SubmissionSchema {
		return fmt.Errorf("unsupported submission schema %q, expected %q", s.Schema, SubmissionSchema)
	}
	if len(s.Metrics) == 0 {
		return fmt.Errorf("submission contains no metrics")
	}
	known := make(map[string]bool)
	for _, dimension := range Dimensions() {
		for _, metric := range dimension.Metrics {
			known[metric.Key] = true
		}
	}
	for key, value := range s.Metrics {
		if !known[key] {
			return fmt.Errorf("unknown metric %q", key)
		}
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("metric %q must be positive", key)
		}
	}
	if s.Host.VCPU <= 0 {
		return fmt.Errorf("host vcpu must be positive")
	}
	if s.Host.MemoryGiB <= 0 || math.IsNaN(s.Host.MemoryGiB) || math.IsInf(s.Host.MemoryGiB, 0) {
		return fmt.Errorf("host memory must be positive")
	}
	if s.Tool.ECS == "" {
		return fmt.Errorf("tool version is required")
	}
	if len([]rune(s.Note)) > maxNoteLength {
		return fmt.Errorf("note exceeds %d characters", maxNoteLength)
	}
	if expected := s.fingerprint(); expected != s.ID {
		return fmt.Errorf("submission id %q does not match its contents (expected %q)", s.ID, expected)
	}
	return nil
}

// AsReport 把提交转成一份最小报告，让它能直接喂给 BuildBaseline。
//
// 这样基线聚合只有一条代码路径：无论输入是完整报告还是瘦身提交，
// 走的都是同一套指标解析与中位数逻辑。
func (s Submission) AsReport() model.Report {
	byModule := make(map[string][]model.Measurement)
	for _, dimension := range Dimensions() {
		for _, metric := range dimension.Metrics {
			value, ok := s.Metrics[metric.Key]
			if !ok {
				continue
			}
			// 还原成 resolveMetric 认得的键：直接映射的用原键，
			// 聚合类的用一个合成键，反正聚合后的值就是它本身。
			key := metric.MeasurementKey
			if key == "" {
				key = metric.Prefix + "submission" + metric.Suffix
			}
			byModule[dimension.ModuleID] = append(byModule[dimension.ModuleID],
				model.Measurement{Key: key, Value: value})
		}
	}
	report := model.Report{
		Run: model.RunInfo{Profile: s.Profile, StartedAt: s.RanAt},
	}
	// 机器规格要一并还原：基线按 vCPU 分档，少了它整批提交都会落进"未知档"。
	report.Results = append(report.Results, model.Result{
		ID: "system", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "logical_cpus", Value: float64(s.Host.VCPU)},
			{Key: "memory_total_bytes", Value: s.Host.MemoryGiB * (1 << 30)},
		},
	})
	for _, id := range sortedModuleIDs(byModule) {
		report.Results = append(report.Results, model.Result{
			ID: id, Status: model.StatusOK, Measurements: byModule[id],
		})
	}
	return report
}

func sortedModuleIDs(values map[string][]model.Measurement) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMetricKeys(values map[string]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sanitizeShort 清洗自报字段：去掉控制字符并限长。
func sanitizeShort(value string, limit int) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			continue
		}
		builder.WriteRune(character)
	}
	runes := []rune(builder.String())
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return strings.TrimSpace(string(runes))
}

// slug 把自报文本转成适合做文件名的形式。
func slug(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(value) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
		case character == '-', character == '_':
			builder.WriteRune('-')
		case character == ' ' || character == '.':
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func shortVersion(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	if len([]rune(value)) > 64 {
		value = string([]rune(value)[:64])
	}
	return value
}

func roundTo(value float64, digits int) float64 {
	parsed, err := strconv.ParseFloat(strconv.FormatFloat(value, 'f', digits, 64), 64)
	if err != nil {
		return value
	}
	return parsed
}
