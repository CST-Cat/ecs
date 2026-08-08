package score

// 综合评分。
//
// 评分的价值在于横向对比多台机器，但它也最容易变成谎言：把 CPU 分和磁盘分
// 相加本身就没有物理意义，而参考的样例报告里出现过 "总分 3867 = CPU N/A +
// GPU N/A + 内存 2850 + 磁盘 1017"——两项缺失却照给总分，读者无从知道这个
// 数字代表什么。
//
// 因此这里的每条规则都是为了让分数可复核：
//
//   - 分项分 = 实测值 / 排行榜参考均值 × 1000，一步除法，读者能手算验证；
//   - 只累加真正跑过的维度，覆盖度随分数一起呈现，缺的维度不按 0 也不按满分；
//   - 权重均等，不替用户假设用途；
//   - 排行榜参考是可替换的数据，不是写死在算法里的常数——它决定了分数的含义，
//     必须能随实测样本更新。
//
// 排名只使用参考文件明确保存的样本分数分布；样本不足时不显示百分位，
// 不从主机数目推造排名。

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"ecs/internal/config"
	"ecs/internal/model"
)

// Dimension 是一个评分维度。
type Dimension struct {
	// Key 是稳定的机器标识，用于基线文件与报告字段。
	Key string
	// ModuleID 指出该维度依赖哪个模块，模块没跑则该维度缺失。
	ModuleID string
	// Metrics 是构成该维度的指标。相同 Group 先取算术平均，Group 之间再等权平均。
	Metrics []Metric
}

// Metric 是维度下的一项指标。
type Metric struct {
	// Key 是当前排行榜参考文件里的指标键。
	Key string
	// MeasurementKey 直接匹配一个 measurement 的 key。
	MeasurementKey string
	// Prefix 与 Suffix 用于匹配一组动态命名的 measurement（如按节点索引拼出的
	// iperf3_target_01_ipv4_download_mbps），匹配到的值按 Aggregate 归并。
	Prefix string
	Suffix string
	// Aggregate 指定多值归并方式，仅在 Prefix/Suffix 匹配时使用。
	Aggregate Aggregation
	// HigherIsBetter 决定分数方向。延迟类指标越小越好，得分要反过来算。
	HigherIsBetter bool
	// Group controls weighting inside a dimension. Metrics in the same group
	// are averaged first, so a wide matrix such as ATTO cannot outweigh CPU or
	// bandwidth merely because it has more cells. Empty means the metric is its
	// own group.
	Group string
	// Optional marks a metric that may be absent from a partially populated
	// current baseline. A missing value is reported explicitly and never treated
	// as zero; newly aggregated baselines score it normally.
	Optional bool
}

// Aggregation 是多值归并方式。
type Aggregation int

const (
	// AggregateMedian 取中位数：单个节点繁忙或限速不该主导整个维度。
	AggregateMedian Aggregation = iota
	// AggregateMax 取最大值，用于"能达到的上限"类指标。
	AggregateMax
)

// Dimensions 定义全部评分维度。
//
// 指标是精选的而非全收：一个维度里塞进十个高度相关的指标，等于给其中某个
// 侧面偷偷加权。每项都注明了为什么选它。
func Dimensions() []Dimension {
	dimensions := []Dimension{
		{
			Key:      "cpu",
			ModuleID: "cpu",
			Metrics: []Metric{
				// 单线程反映核心本身的强弱，多线程反映实际可用的并行度。
				// 两者都要：只看多线程会让高核低频的机器显得强于实际体验。
				{Key: "cpu_single", MeasurementKey: "sysbench_cpu_single_events_s", HigherIsBetter: true},
				{Key: "cpu_multi", MeasurementKey: "sysbench_cpu_multi_events_s", HigherIsBetter: true},
			},
		},
		{
			Key:      "memory",
			ModuleID: "memory",
			Metrics: []Metric{
				// STREAM is the current memory backend. Each kernel is its own
				// equal-weight subgroup, and the 1T/NT values are explicitly
				// aggregated by median so neither thread context silently dominates.
				{Key: "memory_copy", Prefix: "stream_copy_", Suffix: "_mib_s", Aggregate: AggregateMedian, HigherIsBetter: true, Group: "copy", Optional: true},
				{Key: "memory_scale", Prefix: "stream_scale_", Suffix: "_mib_s", Aggregate: AggregateMedian, HigherIsBetter: true, Group: "scale", Optional: true},
				{Key: "memory_add", Prefix: "stream_add_", Suffix: "_mib_s", Aggregate: AggregateMedian, HigherIsBetter: true, Group: "add", Optional: true},
				{Key: "memory_triad", Prefix: "stream_triad_", Suffix: "_mib_s", Aggregate: AggregateMedian, HigherIsBetter: true, Group: "triad", Optional: true},
			},
		},
		{
			Key:      "disk",
			ModuleID: "disk",
			Metrics: []Metric{
				// 顺序吞吐、4K 随机 IOPS、50/50 混合读写和 Crystal/ATTO
				// 矩阵覆盖不同负载，缺一不可：只看顺序会让机械盘阵列显得够用，
				// 只看随机会埋没大文件场景。各组等权，宽矩阵不会按单元数放大。
				{Key: "disk_seq_read", MeasurementKey: "fio_sequential_read_mib_s", HigherIsBetter: true, Group: "baseline"},
				{Key: "disk_seq_write", MeasurementKey: "fio_sequential_write_mib_s", HigherIsBetter: true, Group: "baseline"},
				{Key: "disk_rand_read_iops", MeasurementKey: "fio_random_read_4k_iops", HigherIsBetter: true, Group: "baseline"},
				{Key: "disk_rand_write_iops", MeasurementKey: "fio_random_write_4k_iops", HigherIsBetter: true, Group: "baseline"},
			},
		},
		{
			Key:      "bandwidth",
			ModuleID: "speed",
			Metrics: []Metric{
				// iperf3 的 key 按节点序号拼出，节点集会随档位和配置变化，
				// 因此按前缀匹配后取中位数：某个公共节点繁忙不该拖垮整个维度。
				{Key: "bandwidth_download", Prefix: "iperf3_target_", Suffix: "_download_mbps", Aggregate: AggregateMedian, HigherIsBetter: true},
				{Key: "bandwidth_upload", Prefix: "iperf3_target_", Suffix: "_upload_mbps", Aggregate: AggregateMedian, HigherIsBetter: true},
			},
		},
	}
	for index := range dimensions {
		if dimensions[index].Key != "disk" {
			continue
		}
		dimensions[index].Metrics = append(dimensions[index].Metrics, crystalScoreMetrics()...)
		dimensions[index].Metrics = append(dimensions[index].Metrics, mixedScoreMetrics()...)
		dimensions[index].Metrics = append(dimensions[index].Metrics, attoScoreMetrics()...)
	}

	// A module only participates in the leaderboard when its descriptor opts in
	// explicitly.  This keeps ordinary diagnostic modules report-only by
	// default; adding a new score dimension therefore requires an intentional
	// descriptor change as well as metric definitions here.
	filtered := dimensions[:0]
	for _, dimension := range dimensions {
		descriptor, ok := config.ModuleDescriptorFor(dimension.ModuleID)
		if !ok || !descriptor.ScoreEnabled || descriptor.ScoreKey != dimension.Key {
			continue
		}
		filtered = append(filtered, dimension)
	}
	return filtered
}

// ValidateDimensions verifies the explicit score opt-in contract.  It is
// called by tests and can also be used by CI or tooling before generating a
// leaderboard.  The score package still owns metric definitions; descriptors
// only decide which module dimensions are allowed to enter the composite.
func ValidateDimensions() error {
	dimensions := Dimensions()
	seen := make(map[string]bool, len(dimensions))
	for _, dimension := range dimensions {
		if seen[dimension.Key] {
			return fmt.Errorf("duplicate score dimension %q", dimension.Key)
		}
		seen[dimension.Key] = true
		descriptor, ok := config.ModuleDescriptorFor(dimension.ModuleID)
		if !ok {
			return fmt.Errorf("score dimension %q references unknown module %q", dimension.Key, dimension.ModuleID)
		}
		if !descriptor.ScoreEnabled {
			return fmt.Errorf("score dimension %q is not explicitly enabled by module descriptor", dimension.Key)
		}
		if descriptor.ScoreKey != dimension.Key {
			return fmt.Errorf("score dimension %q disagrees with descriptor score key %q", dimension.Key, descriptor.ScoreKey)
		}
	}
	for _, descriptor := range config.ModuleDescriptors() {
		if !descriptor.ScoreEnabled {
			continue
		}
		if descriptor.ScoreKey == "" || !seen[descriptor.ScoreKey] {
			return fmt.Errorf("score-enabled module %q has no registered dimension %q", descriptor.ID, descriptor.ScoreKey)
		}
	}
	return nil
}

func crystalScoreMetrics() []Metric {
	workloads := []string{"rnd4k_q1", "rnd4k_q32", "seq1m_q1", "seq1m_q8"}
	var metrics []Metric
	for _, workload := range workloads {
		for _, direction := range []string{"read", "write"} {
			for _, kind := range []string{"mib_s", "iops"} {
				metrics = append(metrics, Metric{
					Key:            "crystal_" + workload + "_" + direction + "_" + kind,
					MeasurementKey: "crystal_" + workload + "_" + direction + "_" + kind,
					HigherIsBetter: true, Group: "crystal", Optional: true,
				})
			}
		}
	}
	return metrics
}

func mixedScoreMetrics() []Metric {
	blocks := []string{"4k", "64k", "512k", "1m"}
	var metrics []Metric
	for _, block := range blocks {
		for _, direction := range []string{"read", "write"} {
			metrics = append(metrics, Metric{
				Key:            "fio_mixed_" + block + "_" + direction + "_mib_s",
				MeasurementKey: "fio_mixed_" + block + "_" + direction + "_mib_s",
				HigherIsBetter: true, Group: "mixed", Optional: true,
			})
		}
	}
	return metrics
}

func attoScoreMetrics() []Metric {
	blocks := []string{"512b", "1k", "2k", "4k", "8k", "16k", "32k", "64k", "128k", "256k", "512k", "1m", "2m", "4m", "8m", "16m", "32m", "64m"}
	var metrics []Metric
	for _, block := range blocks {
		for _, direction := range []string{"read", "write"} {
			for _, kind := range []string{"mib_s", "iops"} {
				metrics = append(metrics, Metric{
					Key:            "atto_" + block + "_" + direction + "_" + kind,
					MeasurementKey: "atto_" + block + "_" + direction + "_" + kind,
					HigherIsBetter: true, Group: "atto", Optional: true,
				})
			}
		}
	}
	return metrics
}

// FullScale 是分项满分刻度：实测值等于排行榜参考均值时得这个分。
//
// 取 1000 而不是 100，是为了让超出参考均值的机器有清晰的表达空间——分数不封顶，
// 跑出 2000 就是参考均值的两倍，含义直白。
const FullScale = 1000

// MetricScore 是一项指标的得分。
type MetricScore struct {
	Key      string  `json:"key"`
	Label    string  `json:"label,omitempty"`
	Group    string  `json:"group,omitempty"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Baseline float64 `json:"baseline"`
	Score    float64 `json:"score"`
	// Ratio 是实测相对基线的倍率，柱状图与色阶按它取档。
	Ratio float64 `json:"ratio"`
}

// GroupScore exposes the equal-weight subgroup calculation in the score
// report. It is especially important for the disk matrices: all cells are
// visible, but each workload subgroup contributes only once to the disk
// dimension.
type GroupScore struct {
	Key         string  `json:"key"`
	Score       float64 `json:"score"`
	Ratio       float64 `json:"ratio"`
	MetricCount int     `json:"metric_count"`
}

// DimensionScore 是一个维度的得分。
type DimensionScore struct {
	Key     string        `json:"key"`
	Score   float64       `json:"score"`
	Ratio   float64       `json:"ratio"`
	Metrics []MetricScore `json:"metrics"`
	Groups  []GroupScore  `json:"groups,omitempty"`
	// Missing 为真表示该维度没有可用数据，未计入总分。
	Missing bool `json:"missing"`
	// MissingReason 说明缺失原因，报告里如实呈现而不是留白。
	MissingReason string `json:"missing_reason,omitempty"`
	// MissingMetrics lists expected metric keys absent from the report or its
	// selected baseline. Missing values are excluded, never replaced by zero.
	MissingMetrics []string `json:"missing_metrics,omitempty"`
}

// Report 是一次运行的完整评分。
type Report struct {
	// Total 是已覆盖维度的等权平均分；维度内部按 Group 先聚合，再等权平均。
	Total float64 `json:"total"`
	// Ratio 是总分相对满分刻度的倍率，供柱状图与色阶取档。
	Ratio float64 `json:"ratio"`
	// Covered 与 Possible 一起表达覆盖度，缺维度时读者立刻看得到。
	Covered    int              `json:"covered_dimensions"`
	Possible   int              `json:"possible_dimensions"`
	Complete   bool             `json:"complete"`
	Dimensions []DimensionScore `json:"dimensions"`
	// BaselineSource 记录分数基于哪份基线，换基线后分数不可直接比。
	BaselineSource string `json:"baseline_source,omitempty"`
	BaselineSample int    `json:"baseline_sample_count,omitempty"`
	// TierVCPUMin 是实际使用的档位下界，0 表示用的是全局基线。
	TierVCPUMin int `json:"tier_vcpu_min,omitempty"`
	// TierLabel 是档位的可读名称，全局基线时为空。
	TierLabel string `json:"tier_label,omitempty"`
	// HostVCPU 是被评分机器的核数，用于说明为什么落在这一档。
	HostVCPU int `json:"host_vcpu,omitempty"`
	// RankStatus reports whether a leaderboard position is available without
	// inventing a distribution when the current reference has none. Values are
	// the RankStatus* constants below.
	RankStatus string `json:"rank_status,omitempty"`
	// TopPercent is the share of score samples at or above this report's total;
	// lower is better. It is omitted when RankStatus is not available.
	TopPercent float64 `json:"top_percent,omitempty"`
	// RankSamples is the number of scores used for TopPercent, or the available
	// distribution size when the sample is too small.
	RankSamples int `json:"rank_samples,omitempty"`
	// RankMinSamples records the threshold used by the reference. It is kept in
	// the score report so renderers can explain a sparse distribution.
	RankMinSamples int `json:"rank_min_samples,omitempty"`
}

const (
	// RankStatusAvailable means a top-percent rank was computed.
	RankStatusAvailable = "available"
	// RankStatusInsufficient means a distribution exists but has too few scores.
	RankStatusInsufficient = "insufficient"
	// RankStatusUnavailable means the current reference has no score distribution.
	RankStatusUnavailable = "unavailable"
)

// EffectiveRankStatus normalizes an omitted ranking state. It deliberately
// degrades to an unavailable rank instead of deriving one from host count.
func (r Report) EffectiveRankStatus() string {
	switch r.RankStatus {
	case RankStatusAvailable, RankStatusInsufficient, RankStatusUnavailable:
		return r.RankStatus
	default:
		if r.BaselineSample > 0 && r.BaselineSample < r.EffectiveRankMinSamples() {
			return RankStatusInsufficient
		}
		return RankStatusUnavailable
	}
}

// EffectiveRankSamples returns the score distribution size, falling back to
// the reference sample count only for explanatory rendering.
func (r Report) EffectiveRankSamples() int {
	if r.RankSamples > 0 {
		return r.RankSamples
	}
	return r.BaselineSample
}

// EffectiveRankMinSamples returns the persisted threshold or the current
// conservative default when the reference omits one.
func (r Report) EffectiveRankMinSamples() int {
	if r.RankMinSamples > 0 {
		return r.RankMinSamples
	}
	return DefaultRankMinSamples
}

// Compute 按基线为一份报告算分。
//
// 没有任何维度可算时返回 nil：与其给一个 0 分，不如明确表示"这次运行不产生
// 评分"——仅运行 network 等单一维度本来就不该有综合分。
func Compute(data model.Report, baseline Baseline) *Report {
	values := collectMeasurements(data)
	ran := make(map[string]bool, len(data.Results))
	for _, result := range data.Results {
		if result.Status != model.StatusSkipped {
			ran[result.ID] = true
		}
	}

	// 按被测机器的规格选档位：多线程分数几乎正比于核数，拿全体参考均值
	// 会让小机器永远不及格、大机器永远满分。档位样本不足时自动回落到全局。
	vcpu := hostVCPU(values)
	tierMetrics, tierMin, tierSamples := baseline.MetricsForHost(vcpu)
	scoped := baseline
	scoped.Metrics = tierMetrics

	out := Report{
		BaselineSource: baseline.Source,
		BaselineSample: tierSamples,
		TierVCPUMin:    tierMin,
		HostVCPU:       vcpu,
		RankMinSamples: baseline.RankThreshold(),
	}
	if tierMin > 0 {
		out.TierLabel = TierLabel(tierMin)
	}
	var totals []float64
	complete := true
	for _, dimension := range Dimensions() {
		out.Possible++
		scored := scoreDimension(dimension, values, scoped, ran[dimension.ModuleID])
		out.Dimensions = append(out.Dimensions, scored)
		if !scored.Missing {
			out.Covered++
			totals = append(totals, scored.Score)
		}
		if scored.Missing || len(scored.MissingMetrics) > 0 {
			complete = false
		}
	}
	if out.Covered == 0 {
		return nil
	}
	var sum float64
	for _, value := range totals {
		sum += value
	}
	out.Total = sum / float64(len(totals))
	out.Ratio = out.Total / FullScale
	out.Complete = complete && out.Covered == out.Possible
	populateRank(&out, baseline)
	return &out
}

// populateRank computes a leaderboard position only when the reference carries
// an explicit score distribution. SampleCount alone is not enough to rank.
func populateRank(out *Report, baseline Baseline) {
	out.RankSamples = len(baseline.ScoreSamples)
	if baseline.ScoreSamples == nil {
		// A reference may expose only its host count. We can explain that the
		// sample is too small, but never derive a percentage without scores.
		if baseline.SampleCount > 0 && baseline.SampleCount < baseline.RankThreshold() {
			out.RankSamples = baseline.SampleCount
			out.RankStatus = RankStatusInsufficient
			return
		}
		out.RankStatus = RankStatusUnavailable
		return
	}
	threshold := baseline.RankThreshold()
	out.RankMinSamples = threshold
	if len(baseline.ScoreSamples) < threshold {
		out.RankStatus = RankStatusInsufficient
		return
	}
	if len(baseline.ScoreSamples) == 0 {
		// A zero threshold is invalid, but keep this guard defensive for callers
		// constructing a Baseline directly rather than loading one from disk.
		out.RankStatus = RankStatusUnavailable
		return
	}
	countAtOrAbove := 0
	for _, sample := range baseline.ScoreSamples {
		if sample >= out.Total {
			countAtOrAbove++
		}
	}
	out.TopPercent = float64(countAtOrAbove) * 100 / float64(len(baseline.ScoreSamples))
	out.RankStatus = RankStatusAvailable
}

func scoreDimension(dimension Dimension, values map[string]measured, baseline Baseline, moduleRan bool) DimensionScore {
	scored := DimensionScore{Key: dimension.Key}
	if !moduleRan {
		scored.Missing = true
		scored.MissingReason = "moduleNotRun"
		return scored
	}
	groupScores := make(map[string][]float64)
	for _, metric := range dimension.Metrics {
		base, ok := baseline.Metrics[metric.Key]
		if !ok || base <= 0 {
			scored.MissingMetrics = append(scored.MissingMetrics, metric.Key)
			continue
		}
		value, unit, label, found := resolveMetric(metric, values)
		if !found || value <= 0 {
			scored.MissingMetrics = append(scored.MissingMetrics, metric.Key)
			continue
		}
		ratio := value / base
		if !metric.HigherIsBetter {
			// 越小越好的指标反过来算：基线除以实测，比基线快一倍就是两倍分。
			ratio = base / value
		}
		if math.IsInf(ratio, 0) || math.IsNaN(ratio) {
			continue
		}
		item := MetricScore{
			Key: metric.Key, Label: label, Group: metric.Group, Value: value, Unit: unit,
			Baseline: base, Ratio: ratio, Score: ratio * FullScale,
		}
		scored.Metrics = append(scored.Metrics, item)
		group := metric.Group
		if group == "" {
			group = metric.Key
		}
		groupScores[group] = append(groupScores[group], item.Score)
	}
	sort.Strings(scored.MissingMetrics)
	if len(scored.Metrics) == 0 {
		scored.Missing = true
		scored.MissingReason = "noComparableMetric"
		return scored
	}
	groupKeys := make([]string, 0, len(groupScores))
	for key := range groupScores {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	var sum float64
	for _, key := range groupKeys {
		values := groupScores[key]
		var groupSum float64
		for _, value := range values {
			groupSum += value
		}
		groupScore := groupSum / float64(len(values))
		scored.Groups = append(scored.Groups, GroupScore{
			Key: key, Score: groupScore, Ratio: groupScore / FullScale, MetricCount: len(values),
		})
		sum += groupScore
	}
	scored.Score = sum / float64(len(groupKeys))
	scored.Ratio = scored.Score / FullScale
	return scored
}

// measured 是一条实测值连同它的展示信息。
type measured struct {
	value float64
	unit  string
	label string
}

func collectMeasurements(data model.Report) map[string]measured {
	values := make(map[string]measured, 64)
	for _, result := range data.Results {
		if result.Status == model.StatusSkipped {
			continue
		}
		for _, item := range result.Measurements {
			values[item.Key] = measured{value: item.Value, unit: item.Unit, label: item.Label}
		}
	}
	return values
}

// resolveMetric 取出指标对应的实测值，必要时按前缀归并多个节点的结果。
func resolveMetric(metric Metric, values map[string]measured) (float64, string, string, bool) {
	if metric.MeasurementKey != "" {
		item, ok := values[metric.MeasurementKey]
		return item.value, item.unit, item.label, ok
	}
	if metric.Prefix == "" && metric.Suffix == "" {
		return 0, "", "", false
	}
	var matched []float64
	var unit, label string
	for key, item := range values {
		if metric.Prefix != "" && !strings.HasPrefix(key, metric.Prefix) {
			continue
		}
		if metric.Suffix != "" && !strings.HasSuffix(key, metric.Suffix) {
			continue
		}
		if item.value <= 0 {
			continue
		}
		matched = append(matched, item.value)
		unit, label = item.unit, item.label
	}
	if len(matched) == 0 {
		return 0, "", "", false
	}
	return aggregate(matched, metric.Aggregate), unit, label, true
}

func aggregate(values []float64, mode Aggregation) float64 {
	sort.Float64s(values)
	switch mode {
	case AggregateMax:
		return values[len(values)-1]
	default:
		middle := len(values) / 2
		if len(values)%2 == 1 {
			return values[middle]
		}
		return (values[middle-1] + values[middle]) / 2
	}
}
