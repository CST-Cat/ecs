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
//   - 分项分 = 实测值 / 基线值 × 1000，一步除法，读者能手算验证；
//   - 只累加真正跑过的维度，覆盖度随分数一起呈现，缺的维度不按 0 也不按满分；
//   - 权重均等，不替用户假设用途；
//   - 基线是可替换的数据，不是写死在算法里的常数——它决定了分数的含义，
//     必须能随实测样本更新。
//
// 不做百分位：那需要一个跨机器的样本库，而 ecs 不上传任何数据，编一个百分位
// 就是凭空捏造。

import (
	"math"
	"sort"
	"strings"

	"ecs/internal/model"
)

// Dimension 是一个评分维度。
type Dimension struct {
	// Key 是稳定的机器标识，用于基线文件与报告字段。
	Key string
	// ModuleID 指出该维度依赖哪个模块，模块没跑则该维度缺失。
	ModuleID string
	// Metrics 是构成该维度的指标。维度分取各指标分的算术平均。
	Metrics []Metric
}

// Metric 是维度下的一项指标。
type Metric struct {
	// Key 是基线文件里的键。
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
	return []Dimension{
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
				// 用 mbw 的 memcpy 口径而不是 sysbench 的多线程读：后者在本机
				// 实测 329 GiB/s，那是缓存命中而非内存带宽，拿它当基线会让所有
				// 缓存较小的机器凭空吃亏。
				{Key: "memory_copy", MeasurementKey: "mbw_memcpy_mib_s", HigherIsBetter: true},
				{Key: "memory_write", MeasurementKey: "sysbench_memory_write_single_mib_s", HigherIsBetter: true},
			},
		},
		{
			Key:      "disk",
			ModuleID: "disk",
			Metrics: []Metric{
				// 顺序吞吐与 4K 随机 IOPS 是两类完全不同的负载，缺一不可：
				// 只看顺序会让机械盘阵列显得够用，只看随机会埋没大文件场景。
				{Key: "disk_seq_read", MeasurementKey: "fio_sequential_read_mib_s", HigherIsBetter: true},
				{Key: "disk_seq_write", MeasurementKey: "fio_sequential_write_mib_s", HigherIsBetter: true},
				{Key: "disk_rand_read_iops", MeasurementKey: "fio_random_read_4k_iops", HigherIsBetter: true},
				{Key: "disk_rand_write_iops", MeasurementKey: "fio_random_write_4k_iops", HigherIsBetter: true},
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
}

// FullScale 是分项满分刻度：实测值等于基线时得这个分。
//
// 取 1000 而不是 100，是为了让超出基线的机器有清晰的表达空间——分数不封顶，
// 跑出 2000 就是基线的两倍，含义直白。
const FullScale = 1000

// MetricScore 是一项指标的得分。
type MetricScore struct {
	Key      string  `json:"key"`
	Label    string  `json:"label,omitempty"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Baseline float64 `json:"baseline"`
	Score    float64 `json:"score"`
	// Ratio 是实测相对基线的倍率，柱状图与色阶按它取档。
	Ratio float64 `json:"ratio"`
}

// DimensionScore 是一个维度的得分。
type DimensionScore struct {
	Key     string        `json:"key"`
	Score   float64       `json:"score"`
	Ratio   float64       `json:"ratio"`
	Metrics []MetricScore `json:"metrics"`
	// Missing 为真表示该维度没有可用数据，未计入总分。
	Missing bool `json:"missing"`
	// MissingReason 说明缺失原因，报告里如实呈现而不是留白。
	MissingReason string `json:"missing_reason,omitempty"`
}

// Report 是一次运行的完整评分。
type Report struct {
	// Total 是已覆盖维度的加权平均分；权重均等，因此就是算术平均。
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
}

// Compute 按基线为一份报告算分。
//
// 没有任何维度可算时返回 nil：与其给一个 0 分，不如明确表示"这次运行不产生
// 评分"——quick 档或 --only network 本来就不该有综合分。
func Compute(data model.Report, baseline Baseline) *Report {
	values := collectMeasurements(data)
	ran := make(map[string]bool, len(data.Results))
	for _, result := range data.Results {
		if result.Status != model.StatusSkipped {
			ran[result.ID] = true
		}
	}

	out := Report{
		BaselineSource: baseline.Source,
		BaselineSample: baseline.SampleCount,
	}
	var totals []float64
	for _, dimension := range Dimensions() {
		out.Possible++
		scored := scoreDimension(dimension, values, baseline, ran[dimension.ModuleID])
		out.Dimensions = append(out.Dimensions, scored)
		if !scored.Missing {
			out.Covered++
			totals = append(totals, scored.Score)
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
	out.Complete = out.Covered == out.Possible
	return &out
}

func scoreDimension(dimension Dimension, values map[string]measured, baseline Baseline, moduleRan bool) DimensionScore {
	scored := DimensionScore{Key: dimension.Key}
	if !moduleRan {
		scored.Missing = true
		scored.MissingReason = "moduleNotRun"
		return scored
	}
	var sum float64
	for _, metric := range dimension.Metrics {
		base, ok := baseline.Metrics[metric.Key]
		if !ok || base <= 0 {
			continue
		}
		value, unit, label, found := resolveMetric(metric, values)
		if !found || value <= 0 {
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
			Key: metric.Key, Label: label, Value: value, Unit: unit,
			Baseline: base, Ratio: ratio, Score: ratio * FullScale,
		}
		scored.Metrics = append(scored.Metrics, item)
		sum += item.Score
	}
	if len(scored.Metrics) == 0 {
		scored.Missing = true
		scored.MissingReason = "noComparableMetric"
		return scored
	}
	scored.Score = sum / float64(len(scored.Metrics))
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
