package score

// 按机型分档的排行榜参考。
//
// 全局平均值得到的是"平均机器"，而 4 核和 32 核混在一起比对两端都不公平：
// 多线程分数几乎正比于核数，用全体平均值作参考，小机器永远及格不了，大机器
// 永远轻松满分——两边看到的都不是有用的信息。
//
// 分档的边界不是我划的，是云厂商的实际规格分布：1、2、4、8、16、32、64+
// 这几档覆盖了绝大多数售卖规格，落在中间的（如 6 核）归入下界所在的档。
//
// 关键约束是**按指标自动退化**：同一档的 CPU 可能有足够样本，磁盘却只有一份；
// 每个指标必须各自达到 minTierSamples 才覆盖全局均值。一个由三台机器算出来的
// 指标平均值没有代表性，用它去评判别人还不如老实回落到全局参考。

import (
	"fmt"
	"sort"
)

// minTierSamples 是一个档位可用的最少样本数。
//
// 取 5 而不是 3：三个样本的平均值很容易被单台异常机器显著拉动，5 台起码有
// 更多交叉验证机会。
const minTierSamples = 5

// tierBounds 是档位下界，按云厂商常见规格划分。
var tierBounds = []int{1, 2, 4, 8, 16, 32, 64}

// Tier 是一个机型档位的排行榜参考。
type Tier struct {
	// VCPUMin 是这一档的 vCPU 下界，上界由下一档决定（最后一档无上界）。
	VCPUMin     int                `json:"vcpu_min"`
	SampleCount int                `json:"sample_count"`
	Metrics     map[string]float64 `json:"metrics"`
	// MetricSampleCounts records the independent evidence behind each mean.
	// Older baselines omit it and conservatively fall back to global metrics.
	MetricSampleCounts map[string]int `json:"metric_sample_counts,omitempty"`
}

// MinTierSamples 返回档位可用所需的最少样本数，供命令行说明用。
func MinTierSamples() int { return minTierSamples }

// TierKeyFor 返回一个 vCPU 数所属的档位下界。
func TierKeyFor(vcpu int) int {
	if vcpu <= 0 {
		return 0
	}
	key := tierBounds[0]
	for _, bound := range tierBounds {
		if vcpu >= bound {
			key = bound
			continue
		}
		break
	}
	return key
}

// TierLabel 返回档位的可读名称。
func TierLabel(vcpuMin int) string {
	for index, bound := range tierBounds {
		if bound != vcpuMin {
			continue
		}
		if index == len(tierBounds)-1 {
			return fmt.Sprintf("%d+ vCPU", bound)
		}
		if next := tierBounds[index+1]; next == bound+1 {
			return fmt.Sprintf("%d vCPU", bound)
		} else {
			return fmt.Sprintf("%d–%d vCPU", bound, tierBounds[index+1]-1)
		}
	}
	return fmt.Sprintf("%d vCPU", vcpuMin)
}

// MetricsForHost 返回适用于该机器的参考指标，以及所用档位的说明。
//
// 第二个返回值是档位下界，0 表示用的是全局参考（该档样本不足或没有分档数据）。
func (b Baseline) MetricsForHost(vcpu int) (map[string]float64, int, int) {
	key := TierKeyFor(vcpu)
	for _, tier := range b.Tiers {
		if tier.VCPUMin != key {
			continue
		}
		// 档位里缺的指标用全局值兜底：某档可能没人跑过带宽，
		// 那一项回落到全局比整档不可用要好。
		merged := make(map[string]float64, len(b.Metrics))
		for metricKey, value := range b.Metrics {
			merged[metricKey] = value
		}
		usedTierMetric := false
		for metricKey, value := range tier.Metrics {
			if tier.MetricSampleCounts[metricKey] < minTierSamples {
				continue
			}
			merged[metricKey] = value
			usedTierMetric = true
		}
		if !usedTierMetric {
			return b.Metrics, 0, b.SampleCount
		}
		return merged, tier.VCPUMin, tier.SampleCount
	}
	return b.Metrics, 0, b.SampleCount
}

// buildTiers 从按档位分好的样本里生成分档排行榜参考。
func buildTiers(samplesByTier map[int]map[string][]float64, reportCounts map[int]int) []Tier {
	keys := make([]int, 0, len(samplesByTier))
	for key := range samplesByTier {
		keys = append(keys, key)
	}
	sort.Ints(keys)

	var tiers []Tier
	for _, key := range keys {
		metrics := make(map[string]float64)
		metricCounts := make(map[string]int)
		for metricKey, values := range samplesByTier[key] {
			if len(values) == 0 {
				continue
			}
			metrics[metricKey] = arithmeticMean(values)
			metricCounts[metricKey] = len(values)
		}
		if len(metrics) == 0 {
			continue
		}
		tiers = append(tiers, Tier{
			VCPUMin: key, SampleCount: reportCounts[key], Metrics: metrics, MetricSampleCounts: metricCounts,
		})
	}
	return tiers
}
