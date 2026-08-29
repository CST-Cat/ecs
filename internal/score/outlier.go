package score

// 离群值检测。
//
// 一个跑分库迟早会收到不合常理的数值：跑在空载物理机上的"VPS"、开了写缓存的
// 磁盘测试、手改过的数字。这些若混进基线，会把所有人的分数一起带偏。
//
// 判据不能用预设阈值——我们没有先验分布，编一个"磁盘超过 X MB/s 就是假的"
// 只会在新硬件面前出丑。这里用 MAD（中位数绝对偏差）的 modified z-score：
// 它完全从样本自身推出尺度，样本越多越准，而且中位数与 MAD 都对极端值稳健，
// 不会出现"离群值把判定离群的标准本身撑大"的循环。
//
// 更重要的是**样本不足时明确拒绝判定**：五台机器算不出可信的离散度，此时
// 报"样本不足"比给一个似是而非的结论诚实得多。
//
// 离群不等于造假：可能是新一代硬件、特殊配置或本来就该被记录的极端案例。
// 因此这里只标记与解释，是否收录由维护者决定。

import (
	"fmt"
	"math"
	"sort"
)

// minOutlierSamples 是做离群判定所需的最少样本数。
//
// 少于这个数时 MAD 本身就没有意义：4 个样本里有 1 个偏离，中位数和 MAD 都会
// 被它显著影响，判出来的"离群"只是噪声。
const minOutlierSamples = 8

// outlierThreshold 是 modified z-score 的判定门槛。
//
// 3.5 是这个统计量的通行取值（Iglewicz & Hoaglin）。对正态分布约对应
// 万分之几的误报率，对长尾分布更宽容——跑分数据本来就右偏，宁可漏报不要误伤。
const outlierThreshold = 3.5

// madScale 把 MAD 换算成可与标准差比较的尺度（正态分布下 MAD ≈ 0.6745σ）。
const madScale = 0.6745

// OutlierSample 是离群检测所需的一条评分样本。
//
// 报告与提交都投影成这个值后，检测器不需要知道输入 artifact 的类型。
type OutlierSample struct {
	ID      string
	VCPU    int
	Metrics map[string]float64
}

// OutlierSample returns the value projection used by DetectOutliers.
func (s Submission) OutlierSample() OutlierSample {
	return OutlierSample{ID: s.SampleID, VCPU: s.Host.VCPU, Metrics: s.Metrics}
}

// Outlier 是一条离群记录。
type Outlier struct {
	// SubmissionID 是被标记样本的稳定 sample identity。
	SubmissionID string
	// MetricKey 是出问题的指标。
	MetricKey string
	// Value 是该提交的实测值。
	Value float64
	// Median 与 SampleCount 说明它是跟谁比出来的。
	Median      float64
	SampleCount int
	// TierLabel 指出比较发生在哪一档。
	TierLabel string
	// ZScore 是 modified z-score，正值偏高、负值偏低。
	ZScore float64
	// Ratio 是相对中位数的倍率，比 z 值更直观。
	Ratio float64
}

// Describe 给出一句可读的说明。
func (o Outlier) Describe() string {
	direction := "高"
	if o.ZScore < 0 {
		direction = "低"
	}
	return fmt.Sprintf("%s 的 %s 比同档（%s，%d 个样本）中位数%s %.1f 倍（z=%.1f）",
		o.SubmissionID, o.MetricKey, o.TierLabel, o.SampleCount, direction, o.Ratio, math.Abs(o.ZScore))
}

// OutlierReport 是一次检测的完整结果。
type OutlierReport struct {
	Outliers []Outlier
	// Undecidable 记录因样本不足而无法判定的档位与指标。
	//
	// 单独列出来而不是静默跳过：读者需要知道"没报离群"是因为确实没有，
	// 还是因为压根没查。
	Undecidable []string
}

// DetectOutliers 在同档同指标内做离群检测。
//
// 分档比较是必须的：32 核机器的多线程分数本来就该远高于 2 核，放在一起比
// 会把所有大机器都标成离群。
func DetectOutliers(samples []OutlierSample) OutlierReport {
	type group struct {
		values map[string][]float64
		ids    map[string][]string
	}
	groups := make(map[int]*group)
	for _, sample := range samples {
		key := TierKeyFor(sample.VCPU)
		entry, ok := groups[key]
		if !ok {
			entry = &group{values: make(map[string][]float64), ids: make(map[string][]string)}
			groups[key] = entry
		}
		for metricKey, value := range sample.Metrics {
			entry.values[metricKey] = append(entry.values[metricKey], value)
			entry.ids[metricKey] = append(entry.ids[metricKey], sample.ID)
		}
	}

	var report OutlierReport
	tierKeys := make([]int, 0, len(groups))
	for key := range groups {
		tierKeys = append(tierKeys, key)
	}
	sort.Ints(tierKeys)

	for _, tierKey := range tierKeys {
		entry := groups[tierKey]
		metricKeys := make([]string, 0, len(entry.values))
		for key := range entry.values {
			metricKeys = append(metricKeys, key)
		}
		sort.Strings(metricKeys)

		for _, metricKey := range metricKeys {
			values := entry.values[metricKey]
			if len(values) < minOutlierSamples {
				report.Undecidable = append(report.Undecidable, fmt.Sprintf(
					"%s / %s：仅 %d 个样本，需要 %d 个才能判定",
					TierLabel(tierKey), metricKey, len(values), minOutlierSamples))
				continue
			}
			median, mad := medianAndMAD(values)
			if mad <= 0 {
				// 所有样本几乎相同：没有离散度可言，任何偏离都会算出无穷大的 z 值。
				report.Undecidable = append(report.Undecidable, fmt.Sprintf(
					"%s / %s：样本离散度为零，无法判定", TierLabel(tierKey), metricKey))
				continue
			}
			for index, value := range values {
				z := madScale * (value - median) / mad
				if math.Abs(z) <= outlierThreshold {
					continue
				}
				ratio := value / median
				if ratio < 1 && ratio > 0 {
					ratio = median / value
				}
				report.Outliers = append(report.Outliers, Outlier{
					SubmissionID: entry.ids[metricKey][index],
					MetricKey:    metricKey,
					Value:        value,
					Median:       median,
					SampleCount:  len(values),
					TierLabel:    TierLabel(tierKey),
					ZScore:       z,
					Ratio:        ratio,
				})
			}
		}
	}
	sort.Slice(report.Outliers, func(i, j int) bool {
		if math.Abs(report.Outliers[i].ZScore) != math.Abs(report.Outliers[j].ZScore) {
			return math.Abs(report.Outliers[i].ZScore) > math.Abs(report.Outliers[j].ZScore)
		}
		return report.Outliers[i].SubmissionID < report.Outliers[j].SubmissionID
	})
	return report
}

// medianAndMAD 返回中位数与中位数绝对偏差。
func medianAndMAD(values []float64) (float64, float64) {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	median := aggregate(sorted, AggregateMedian)

	deviations := make([]float64, len(sorted))
	for index, value := range sorted {
		deviations[index] = math.Abs(value - median)
	}
	sort.Float64s(deviations)
	return median, aggregate(deviations, AggregateMedian)
}
