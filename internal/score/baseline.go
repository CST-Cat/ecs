package score

// 排行榜参考与样本分布。
//
// 参考均值决定了分数的含义：分项分是"实测 / 参考均值 × 1000"，换一份参考，同一台
// 机器的分数就完全不同。因此参考不是算法里的魔数，而是一份带来源说明和样本数的
// 数据——它必须能随实测样本更新，也必须让读者看得到当前用的是哪一份。
//
// 排行榜统计来自跨机器的实测样本，用 `ecs leaderboard` 从多份报告聚合生成，再用
// --score-baseline 传入。没有当前样本时，发行包不会使用旧的硬编码参考。

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

// BaselineSchema 是当前排行榜参考文件的格式标识。
const BaselineSchema = "ecs.baseline/v1"

// DefaultRankMinSamples is the minimum number of score samples needed before
// a report may claim a leaderboard position.
const DefaultRankMinSamples = 5

// Baseline 是一份排行榜参考。
type Baseline struct {
	Schema string `json:"schema"`
	// Source 保存 ECS 稳定来源代码或用户提供的原始说明；已知代码在展示边界翻译。
	Source string `json:"source"`
	// SampleCount 是聚合时的样本机器数。1 意味着这只是一台机器的快照，
	// 报告需要据此提醒读者分数的参考价值有限。
	SampleCount int       `json:"sample_count"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	// Metrics 是全局基线，也是分档样本不足时的回落值。
	Metrics map[string]float64 `json:"metrics"`
	// Tiers 是按 vCPU 分档的基线。样本少时为空，评分自动用全局值。
	Tiers []Tier `json:"tiers,omitempty"`
	// ScoreSamples stores only aggregate scores, never host identifiers or raw
	// report fields. An empty slice means that no score distribution was built.
	ScoreSamples []float64 `json:"score_samples,omitempty"`
	// RankMinSamples records the threshold used for ranking.
	RankMinSamples int `json:"rank_min_samples,omitempty"`
}

// emptyBaseline 表示发行包当前没有可用的排行榜样本。
//
// 它故意不包含任何指标：评分器会返回 nil，报告不会把旧数据伪装成当前参考。
func emptyBaseline() Baseline {
	return Baseline{
		Schema:         BaselineSchema,
		Source:         "noCurrentBaseline",
		SampleCount:    0,
		RankMinSamples: DefaultRankMinSamples,
		Metrics:        map[string]float64{},
	}
}

// LoadBaseline 从文件读入基线。
func LoadBaseline(path string) (Baseline, error) {
	var baseline Baseline
	file, err := os.Open(path)
	if err != nil {
		return baseline, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 4*1024*1024 {
		return baseline, fmt.Errorf("baseline file exceeds the 4 MiB safety limit")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&baseline); err != nil {
		return baseline, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return baseline, fmt.Errorf("baseline file must contain exactly one JSON object")
	}
	if err := validateBaseline(baseline, false); err != nil {
		return baseline, err
	}
	return baseline, nil
}

// validateBaseline checks both global and tier evidence. allowEmptyMetrics is
// reserved for the intentionally empty embedded reference; user-provided
// baseline files must always contain at least one usable metric.
func validateBaseline(baseline Baseline, allowEmptyMetrics bool) error {
	if baseline.Schema != BaselineSchema {
		return fmt.Errorf("unsupported baseline schema %q, expected %q", baseline.Schema, BaselineSchema)
	}
	if baseline.Source == "" {
		return fmt.Errorf("baseline source must be non-empty")
	}
	if baseline.SampleCount < 0 {
		return fmt.Errorf("baseline sample_count must not be negative")
	}
	if !allowEmptyMetrics && len(baseline.Metrics) == 0 {
		return fmt.Errorf("baseline file contains no metrics")
	}
	if len(baseline.Metrics) > 0 && baseline.SampleCount == 0 {
		return fmt.Errorf("baseline with metrics must have a positive sample_count")
	}
	if len(baseline.Metrics) == 0 && (len(baseline.Tiers) > 0 || len(baseline.ScoreSamples) > 0) {
		return fmt.Errorf("empty baseline must not contain tiers or score samples")
	}
	if baseline.RankMinSamples < 0 {
		return fmt.Errorf("baseline rank_min_samples must not be negative")
	}
	if err := validatePositiveMetrics("baseline", baseline.Metrics); err != nil {
		return err
	}
	for _, value := range baseline.ScoreSamples {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("baseline score_samples must contain finite non-negative values")
		}
	}
	if len(baseline.ScoreSamples) > baseline.SampleCount {
		return fmt.Errorf("baseline score_samples cannot exceed sample_count")
	}
	seenTiers := make(map[int]bool, len(baseline.Tiers))
	tierSampleTotal := 0
	for index, tier := range baseline.Tiers {
		if TierKeyFor(tier.VCPUMin) != tier.VCPUMin || tier.VCPUMin <= 0 {
			return fmt.Errorf("baseline tier %d has invalid vcpu_min %d", index, tier.VCPUMin)
		}
		if seenTiers[tier.VCPUMin] {
			return fmt.Errorf("baseline contains duplicate tier vcpu_min %d", tier.VCPUMin)
		}
		seenTiers[tier.VCPUMin] = true
		tierSampleTotal += tier.SampleCount
		if tier.SampleCount <= 0 || tier.SampleCount > baseline.SampleCount {
			return fmt.Errorf("baseline tier %d sample_count must be positive and no greater than the global count", tier.VCPUMin)
		}
		if len(tier.Metrics) == 0 {
			return fmt.Errorf("baseline tier %d contains no metrics", tier.VCPUMin)
		}
		if err := validatePositiveMetrics(fmt.Sprintf("baseline tier %d", tier.VCPUMin), tier.Metrics); err != nil {
			return err
		}
		if tier.MetricSampleCounts == nil {
			return fmt.Errorf("baseline tier %d metric_sample_counts is required", tier.VCPUMin)
		}
		if len(tier.MetricSampleCounts) != len(tier.Metrics) {
			return fmt.Errorf("baseline tier %d metric_sample_counts must cover exactly its metrics", tier.VCPUMin)
		}
		for key := range tier.Metrics {
			count, ok := tier.MetricSampleCounts[key]
			if !ok || count <= 0 || count > tier.SampleCount {
				return fmt.Errorf("baseline tier %d metric %q has invalid sample count %d", tier.VCPUMin, key, count)
			}
		}
		for key := range tier.MetricSampleCounts {
			if _, ok := tier.Metrics[key]; !ok {
				return fmt.Errorf("baseline tier %d has sample count for unknown metric %q", tier.VCPUMin, key)
			}
		}
	}
	if tierSampleTotal > baseline.SampleCount {
		return fmt.Errorf("baseline tier sample counts cannot exceed the global sample_count")
	}
	return nil
}

func validatePositiveMetrics(scope string, metrics map[string]float64) error {
	for key, value := range metrics {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s metric %q must be positive and finite", scope, key)
		}
	}
	return nil
}

// BuildBaseline 从多份报告聚合出一份排行榜参考。
//
// 每个指标取样本的算术平均。离群检测仍在提交/CI 流程中单独完成；把两件事
// 混在一起会让参考值的定义随着一个隐藏阈值变化。只有至少一台机器测到的指标
// 才会进入参考——凭空补齐会让缺失伪装成数据。
func BuildBaseline(reports []model.Report, source string) (Baseline, error) {
	if len(reports) == 0 {
		return Baseline{}, i18n.Errorf("err.baselineNoReports")
	}
	samples := make(map[string][]float64)
	// 同一批样本同时按档位归类，分档基线与全局基线一次算完。
	byTier := make(map[int]map[string][]float64)
	tierReportCounts := make(map[int]int)
	for _, report := range reports {
		values := collectMeasurements(report)
		tierKey := TierKeyFor(hostVCPU(values))
		if tierKey > 0 {
			tierReportCounts[tierKey]++
		}
		for key, value := range scoreableMetrics(report, values) {
			samples[key] = append(samples[key], value)
			if tierKey > 0 {
				if byTier[tierKey] == nil {
					byTier[tierKey] = make(map[string][]float64)
				}
				byTier[tierKey][key] = append(byTier[tierKey][key], value)
			}
		}
	}
	if len(samples) == 0 {
		return Baseline{}, i18n.Errorf("err.baselineNoMetrics")
	}
	metrics := make(map[string]float64, len(samples))
	for key, values := range samples {
		metrics[key] = arithmeticMean(values)
	}
	if source == "" {
		source = "aggregated"
	}
	baseline := Baseline{
		Schema:         BaselineSchema,
		Source:         source,
		SampleCount:    len(reports),
		GeneratedAt:    time.Now().UTC(),
		Metrics:        metrics,
		Tiers:          buildTiers(byTier, tierReportCounts),
		RankMinSamples: DefaultRankMinSamples,
	}
	// Compute each input against the freshly built reference to retain a small,
	// privacy-preserving score distribution for leaderboard ranking. Partial
	// reports follow the same coverage semantics as ordinary rendering; only the
	// aggregate score is copied into the artifact.
	for _, report := range reports {
		if scored := Compute(report, baseline); scored != nil &&
			!math.IsNaN(scored.Total) && !math.IsInf(scored.Total, 0) {
			baseline.ScoreSamples = append(baseline.ScoreSamples, scored.Total)
		}
	}
	sort.Float64s(baseline.ScoreSamples)
	return baseline, nil
}

// RankThreshold returns the minimum distribution size for a trustworthy rank,
// falling back to the current default when the field is omitted.
func (b Baseline) RankThreshold() int {
	if b.RankMinSamples > 0 {
		return b.RankMinSamples
	}
	return DefaultRankMinSamples
}

// arithmeticMean returns the sample mean without mutating the input slice.
func arithmeticMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

// hostVCPU 从报告里取逻辑核数，用于归档。
func hostVCPU(values map[string]measured) int {
	if item, ok := values["logical_cpus"]; ok && item.value > 0 {
		return int(item.value)
	}
	return 0
}

// Encode 序列化基线，供写入文件。
func (b Baseline) Encode() ([]byte, error) {
	content, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

// MetricSampleCounts 返回每个指标的样本数，供 baseline 命令报告覆盖情况。
func MetricSampleCounts(reports []model.Report) map[string]int {
	counts := make(map[string]int)
	for _, report := range reports {
		values := collectMeasurements(report)
		for key := range scoreableMetrics(report, values) {
			counts[key]++
		}
	}
	return counts
}
