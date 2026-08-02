package score

// 评分基线。
//
// 基线决定了分数的含义：分项分是"实测 / 基线 × 1000"，换一份基线，同一台机器
// 的分数就完全不同。因此基线不是算法里的魔数，而是一份带来源说明和样本数的
// 数据——它必须能随实测样本更新，也必须让读者看得到当前用的是哪一份。
//
// 内置基线只是让首次运行有分可算的起点。真正可信的基线来自跨机器的实测样本，
// 用 `ecs baseline` 从多份报告聚合生成，再用 --score-baseline 传入。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

// BaselineSchema 是基线文件的格式标识。
const BaselineSchema = "ecs.baseline/v1"

// Baseline 是一份评分基线。
type Baseline struct {
	Schema string `json:"schema"`
	// Source 说明这份基线是怎么来的，直接呈现在报告里。
	Source string `json:"source"`
	// SampleCount 是聚合时的样本机器数。1 意味着这只是一台机器的快照，
	// 报告需要据此提醒读者分数的参考价值有限。
	SampleCount int       `json:"sample_count"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	// Metrics 是全局基线，也是分档样本不足时的回落值。
	Metrics map[string]float64 `json:"metrics"`
	// Tiers 是按 vCPU 分档的基线。样本少时为空，评分自动用全局值。
	Tiers []Tier `json:"tiers,omitempty"`
}

// builtinBaseline 是内置基线。
//
// 数值取自单台开发机（AMD Ryzen 7 PRO 4750U，16 逻辑核，NVMe，tmpfs 上的
// fio）的一次实测，**不是 VPS 的典型值**：这台机器的磁盘与内存带宽显著高于
// 常见云主机，因此多数 VPS 用它当基线会得到远低于 1000 的分。
//
// 保留它是为了让首次运行就有可算的分，而不是主张它是"标准机器"。分数的横向
// 意义要等基线换成跨机器聚合的样本之后才成立——报告里会一并显示样本数，
// 样本为 1 时明确提示。
func builtinBaseline() Baseline {
	return Baseline{
		Schema:      BaselineSchema,
		Source:      "builtinSingleHost",
		SampleCount: 1,
		Metrics: map[string]float64{
			"cpu_single": 785.85,
			"cpu_multi":  6152.04,

			"memory_copy":  6389.17,
			"memory_write": 25719.54,

			"disk_seq_read":        6912.04,
			"disk_seq_write":       2309.85,
			"disk_rand_read_iops":  451266.5,
			"disk_rand_write_iops": 367767.62,

			// 带宽基线按千兆链路取整：公共 iperf3 节点的实测值受对端负载影响
			// 很大，用一个整数刻度比用某次抽样更好解释。
			"bandwidth_download": 1000,
			"bandwidth_upload":   1000,
		},
	}
}

// DefaultBaseline 返回内置基线的副本。
func DefaultBaseline() Baseline {
	base := builtinBaseline()
	metrics := make(map[string]float64, len(base.Metrics))
	for key, value := range base.Metrics {
		metrics[key] = value
	}
	base.Metrics = metrics
	return base
}

// IsBuiltin 报告这份基线是否仍是内置的单机快照。
func (b Baseline) IsBuiltin() bool { return b.Source == "builtinSingleHost" }

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
	if err := decoder.Decode(&baseline); err != nil {
		return baseline, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return baseline, fmt.Errorf("baseline file must contain exactly one JSON object")
	}
	if baseline.Schema != BaselineSchema {
		return baseline, fmt.Errorf("unsupported baseline schema %q, expected %q", baseline.Schema, BaselineSchema)
	}
	if len(baseline.Metrics) == 0 {
		return baseline, fmt.Errorf("baseline file contains no metrics")
	}
	for key, value := range baseline.Metrics {
		if value <= 0 {
			return baseline, fmt.Errorf("baseline metric %q must be positive", key)
		}
	}
	return baseline, nil
}

// BuildBaseline 从多份报告聚合出一份基线。
//
// 每个指标取样本的中位数而不是平均：一台异常快或异常慢的机器不该把基线拽走。
// 只有至少一台机器测到的指标才会进入基线——凭空补齐会让缺失伪装成数据。
func BuildBaseline(reports []model.Report, source string) (Baseline, error) {
	if len(reports) == 0 {
		return Baseline{}, i18n.Errorf("err.baselineNoReports")
	}
	samples := make(map[string][]float64)
	// 同一批样本同时按档位归类，分档基线与全局基线一次算完。
	byTier := make(map[int]map[string][]float64)
	for _, report := range reports {
		values := collectMeasurements(report)
		ran := make(map[string]bool, len(report.Results))
		for _, result := range report.Results {
			if result.Status != model.StatusSkipped {
				ran[result.ID] = true
			}
		}
		tierKey := TierKeyFor(hostVCPU(values))
		for _, dimension := range Dimensions() {
			if !ran[dimension.ModuleID] {
				continue
			}
			for _, metric := range dimension.Metrics {
				value, _, _, ok := resolveMetric(metric, values)
				if !ok || value <= 0 {
					continue
				}
				samples[metric.Key] = append(samples[metric.Key], value)
				if tierKey > 0 {
					if byTier[tierKey] == nil {
						byTier[tierKey] = make(map[string][]float64)
					}
					byTier[tierKey][metric.Key] = append(byTier[tierKey][metric.Key], value)
				}
			}
		}
	}
	if len(samples) == 0 {
		return Baseline{}, i18n.Errorf("err.baselineNoMetrics")
	}
	metrics := make(map[string]float64, len(samples))
	for key, values := range samples {
		sort.Float64s(values)
		metrics[key] = aggregate(values, AggregateMedian)
	}
	if source == "" {
		source = fmt.Sprintf("aggregated from %d reports", len(reports))
	}
	return Baseline{
		Schema:      BaselineSchema,
		Source:      source,
		SampleCount: len(reports),
		GeneratedAt: time.Now().UTC(),
		Metrics:     metrics,
		Tiers:       buildTiers(byTier),
	}, nil
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
		for _, dimension := range Dimensions() {
			for _, metric := range dimension.Metrics {
				if value, _, _, ok := resolveMetric(metric, values); ok && value > 0 {
					counts[metric.Key]++
				}
			}
		}
	}
	return counts
}
