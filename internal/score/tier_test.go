package score

import (
	"math"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func tierSubmission(id string, vcpu int, metrics map[string]float64) Submission {
	return Submission{
		Schema: SubmissionSchema, ID: id,
		Host:    HostSpec{VCPU: vcpu, MemoryGiB: float64(vcpu * 2), CPUModel: "test"},
		Tool:    ToolSpec{ECS: "test"},
		RanAt:   time.Unix(0, 0).UTC(),
		Metrics: metrics,
	}
}

func TestTierKeyForUsesCommonCloudSizes(t *testing.T) {
	cases := map[int]int{1: 1, 2: 2, 3: 2, 4: 4, 6: 4, 8: 8, 15: 8, 16: 16, 31: 16, 32: 32, 48: 32, 64: 64, 128: 64}
	for vcpu, want := range cases {
		if got := TierKeyFor(vcpu); got != want {
			t.Errorf("TierKeyFor(%d) = %d，期望 %d", vcpu, got, want)
		}
	}
	if TierKeyFor(0) != 0 {
		t.Error("未知核数不该归档")
	}
}

func TestTierLabelReadsAsRange(t *testing.T) {
	if got := TierLabel(2); got != "2–3 vCPU" {
		t.Errorf("TierLabel(2) = %q", got)
	}
	if got := TierLabel(64); got != "64+ vCPU" {
		t.Errorf("最后一档应无上界：%q", got)
	}
}

// 分档的核心价值：多线程分数几乎正比于核数，全局基线会让小机器永远不及格。
func TestTieredBaselineIsFairToSmallHosts(t *testing.T) {
	var reports []Submission
	// 两档各 6 台，多线程性能随核数线性缩放。
	for index := 0; index < 6; index++ {
		reports = append(reports,
			tierSubmission("s"+string(rune('a'+index)), 2, map[string]float64{"cpu_multi": 1500}),
			tierSubmission("l"+string(rune('a'+index)), 16, map[string]float64{"cpu_multi": 12000}),
		)
	}
	baseline, err := BuildBaseline(submissionsToReports(reports), "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Tiers) != 2 {
		t.Fatalf("应当生成两个档位，得到 %d 个", len(baseline.Tiers))
	}

	// 2 核机器应当与同档比，而不是被 16 核拉低。
	small, tierMin, samples := baseline.MetricsForHost(2)
	if tierMin != 2 {
		t.Fatalf("2 核应落在 2 核档，得到 %d", tierMin)
	}
	if samples != 6 {
		t.Fatalf("档位样本数 = %d，期望 6", samples)
	}
	if math.Abs(small["cpu_multi"]-1500) > 1 {
		t.Fatalf("2 核档基线 = %v，期望约 1500", small["cpu_multi"])
	}
	large, _, _ := baseline.MetricsForHost(16)
	if math.Abs(large["cpu_multi"]-12000) > 1 {
		t.Fatalf("16 核档基线 = %v，期望约 12000", large["cpu_multi"])
	}
	// 全局基线介于两者之间——正是它对两端都不公平的原因。
	if baseline.Metrics["cpu_multi"] <= 1500 || baseline.Metrics["cpu_multi"] >= 12000 {
		t.Fatalf("全局基线应落在两档之间，得到 %v", baseline.Metrics["cpu_multi"])
	}
}

// 样本不足的档位必须回落到全局基线，而不是用三台机器的中位数评判所有人。
func TestSparseTierFallsBackToGlobal(t *testing.T) {
	var subs []Submission
	for index := 0; index < 3; index++ {
		subs = append(subs, tierSubmission("x"+string(rune('a'+index)), 32, map[string]float64{"cpu_multi": 30000}))
	}
	for index := 0; index < 6; index++ {
		subs = append(subs, tierSubmission("y"+string(rune('a'+index)), 2, map[string]float64{"cpu_multi": 1500}))
	}
	baseline, err := BuildBaseline(submissionsToReports(subs), "test")
	if err != nil {
		t.Fatal(err)
	}
	_, tierMin, _ := baseline.MetricsForHost(32)
	if tierMin != 0 {
		t.Fatalf("32 核档只有 3 个样本，应回落到全局（tierMin=0），得到 %d", tierMin)
	}
	_, tierMin, _ = baseline.MetricsForHost(2)
	if tierMin != 2 {
		t.Fatalf("2 核档有 6 个样本，应当启用，得到 %d", tierMin)
	}
}

// 档位里缺的指标要用全局值兜底，不该让整档不可用。
func TestTierInheritsMissingMetricsFromGlobal(t *testing.T) {
	var subs []Submission
	for index := 0; index < 6; index++ {
		subs = append(subs, tierSubmission("a"+string(rune('a'+index)), 4, map[string]float64{"cpu_multi": 3000}))
	}
	// 另一档提供了带宽数据，4 核档没有。
	for index := 0; index < 6; index++ {
		subs = append(subs, tierSubmission("b"+string(rune('a'+index)), 16, map[string]float64{
			"cpu_multi": 12000, "bandwidth_download": 900,
		}))
	}
	baseline, _ := BuildBaseline(submissionsToReports(subs), "test")
	metrics, tierMin, _ := baseline.MetricsForHost(4)
	if tierMin != 4 {
		t.Fatalf("应使用 4 核档，得到 %d", tierMin)
	}
	if _, ok := metrics["bandwidth_download"]; !ok {
		t.Error("档位缺失的指标应从全局基线兜底")
	}
}

// 离群检测必须命中被篡改/异常的样本，且不误伤同档正常样本。
func TestDetectOutliersFindsInflatedSample(t *testing.T) {
	var subs []Submission
	for index := 0; index < 12; index++ {
		subs = append(subs, tierSubmission("n"+string(rune('a'+index)), 8, map[string]float64{
			"disk_seq_read": 2000 + float64(index)*20,
		}))
	}
	subs = append(subs, tierSubmission("BAD", 8, map[string]float64{"disk_seq_read": 26000}))

	report := DetectOutliers(subs)
	if len(report.Outliers) != 1 {
		t.Fatalf("应当只标记一条离群，得到 %d 条：%+v", len(report.Outliers), report.Outliers)
	}
	if report.Outliers[0].SubmissionID != "BAD" {
		t.Fatalf("标记了错误的样本：%s", report.Outliers[0].SubmissionID)
	}
	if !strings.Contains(report.Outliers[0].Describe(), "8–15 vCPU") {
		t.Errorf("说明里应指出比较发生在哪一档：%s", report.Outliers[0].Describe())
	}
}

// 样本不足时必须明确拒绝判定，而不是给一个似是而非的结论。
func TestDetectOutliersRefusesOnSmallSamples(t *testing.T) {
	var subs []Submission
	for index := 0; index < 4; index++ {
		subs = append(subs, tierSubmission("s"+string(rune('a'+index)), 4, map[string]float64{"cpu_multi": 3000}))
	}
	subs = append(subs, tierSubmission("WILD", 4, map[string]float64{"cpu_multi": 999999}))

	report := DetectOutliers(subs)
	if len(report.Outliers) != 0 {
		t.Fatalf("样本不足时不该判定离群，得到 %+v", report.Outliers)
	}
	if len(report.Undecidable) == 0 {
		t.Fatal("应当明确记录无法判定，而不是静默跳过")
	}
	if !strings.Contains(strings.Join(report.Undecidable, " "), "样本") {
		t.Errorf("无法判定的原因应当说清楚：%v", report.Undecidable)
	}
}

// 离群检测按档分组：32 核的多线程分数天然远高于 2 核，混在一起会把大机器全标成离群。
func TestDetectOutliersComparesWithinTier(t *testing.T) {
	var subs []Submission
	for index := 0; index < 10; index++ {
		subs = append(subs, tierSubmission("s"+string(rune('a'+index)), 2, map[string]float64{"cpu_multi": 1500}))
		subs = append(subs, tierSubmission("l"+string(rune('a'+index)), 32, map[string]float64{"cpu_multi": 24000}))
	}
	report := DetectOutliers(subs)
	if len(report.Outliers) != 0 {
		t.Fatalf("跨档差异不该被当成离群：%+v", report.Outliers)
	}
}

// 所有样本相同时离散度为零，任何偏离都会算出无穷大的 z 值——必须拒绝判定。
func TestDetectOutliersHandlesZeroSpread(t *testing.T) {
	var subs []Submission
	for index := 0; index < 10; index++ {
		subs = append(subs, tierSubmission("s"+string(rune('a'+index)), 4, map[string]float64{"cpu_multi": 3000}))
	}
	report := DetectOutliers(subs)
	for _, outlier := range report.Outliers {
		if math.IsInf(outlier.ZScore, 0) || math.IsNaN(outlier.ZScore) {
			t.Fatalf("z 值不该是无穷或 NaN：%+v", outlier)
		}
	}
	if len(report.Undecidable) == 0 {
		t.Error("零离散度应当被记为无法判定")
	}
}

func TestMedianAndMADAreRobust(t *testing.T) {
	values := []float64{10, 10, 10, 10, 10, 1000}
	median, mad := medianAndMAD(values)
	if median != 10 {
		t.Errorf("中位数应不受单个极端值影响：%v", median)
	}
	if mad != 0 {
		t.Errorf("五个相同值加一个极端值，MAD 应为 0：%v", mad)
	}
}

// submissionsToReports 把提交批量转成最小报告，供聚合测试使用。
func submissionsToReports(items []Submission) []model.Report {
	reports := make([]model.Report, 0, len(items))
	for _, item := range items {
		reports = append(reports, item.AsReport())
	}
	return reports
}
