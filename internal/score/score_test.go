package score

import (
	"math"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func reportWith(results ...model.Result) model.Report {
	return model.Report{Results: results}
}

func TestDimensionsRequireExplicitModuleOptIn(t *testing.T) {
	if err := ValidateDimensions(); err != nil {
		t.Fatal(err)
	}
	for _, dimension := range Dimensions() {
		descriptor, ok := config.ModuleDescriptorFor(dimension.ModuleID)
		if !ok || !descriptor.ScoreEnabled || descriptor.ScoreKey != dimension.Key {
			t.Fatalf("dimension %q is not explicitly enabled by its module descriptor", dimension.Key)
		}
	}
}

func benchResult(id string, status model.Status, metrics map[string]float64) model.Result {
	result := model.Result{ID: id, Status: status}
	for key, value := range metrics {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: key, Value: value, Unit: "u", Label: key,
		})
	}
	return result
}

func testBaseline() Baseline {
	return Baseline{
		Schema: BaselineSchema, Source: "test", SampleCount: 3,
		Metrics: map[string]float64{
			"cpu_single": 100, "cpu_multi": 100,
			"memory_copy": 100, "memory_write": 100,
			"disk_seq_read": 100, "disk_seq_write": 100,
			"disk_rand_read_iops": 100, "disk_rand_write_iops": 100,
			"bandwidth_download": 100, "bandwidth_upload": 100,
		},
	}
}

func expandedDiskBaseline() Baseline {
	baseline := testBaseline()
	for _, dimension := range Dimensions() {
		if dimension.Key != "disk" {
			continue
		}
		for _, metric := range dimension.Metrics {
			if strings.HasPrefix(metric.Key, "crystal_") || strings.HasPrefix(metric.Key, "fio_mixed_") || strings.HasPrefix(metric.Key, "atto_") {
				baseline.Metrics[metric.Key] = 100
			}
		}
	}
	return baseline
}

func TestDimensionsIncludeCompleteFioMixedMatrix(t *testing.T) {
	dimensions := Dimensions()
	var disk *Dimension
	for index := range dimensions {
		if dimensions[index].Key == "disk" {
			disk = &dimensions[index]
			break
		}
	}
	if disk == nil {
		t.Fatal("disk dimension is missing")
	}
	want := make(map[string]bool)
	for _, block := range []string{"4k", "64k", "512k", "1m"} {
		for _, direction := range []string{"read", "write"} {
			want["fio_mixed_"+block+"_"+direction+"_mib_s"] = true
		}
	}
	for _, metric := range disk.Metrics {
		if !strings.HasPrefix(metric.Key, "fio_mixed_") {
			continue
		}
		if !want[metric.Key] || metric.MeasurementKey != metric.Key || metric.Group != "mixed" || !metric.HigherIsBetter {
			t.Fatalf("invalid fio mixed metric: %+v", metric)
		}
		delete(want, metric.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing fio mixed metrics: %v", want)
	}
}

func expandedMemoryBaseline() Baseline {
	baseline := testBaseline()
	baseline.Metrics["memory_write_multi"] = 100
	baseline.Metrics["memory_read"] = 100
	baseline.Metrics["memory_latency"] = 2
	return baseline
}

// 分项分必须是一步除法，读者能手算复核。
func TestScoreIsMeasuredOverBaseline(t *testing.T) {
	data := reportWith(benchResult("cpu", model.StatusOK, map[string]float64{
		"sysbench_cpu_single_events_s": 150,
		"sysbench_cpu_multi_events_s":  50,
	}))
	got := Compute(data, testBaseline())
	if got == nil {
		t.Fatal("应当算出分数")
	}
	// (150/100 + 50/100) / 2 × 1000 = 1000
	if math.Abs(got.Dimensions[0].Score-1000) > 0.001 {
		t.Fatalf("CPU 维度分 = %v，期望 1000", got.Dimensions[0].Score)
	}
}

// 缺失的维度既不能按 0 也不能按满分计入——这正是参考实现里
// "总分 = CPU N/A + GPU N/A + 内存 + 磁盘" 的错误。
func TestMissingDimensionsAreExcludedNotZeroed(t *testing.T) {
	data := reportWith(
		benchResult("cpu", model.StatusOK, map[string]float64{
			"sysbench_cpu_single_events_s": 100,
			"sysbench_cpu_multi_events_s":  100,
		}),
		model.Result{ID: "disk", Status: model.StatusSkipped},
	)
	got := Compute(data, testBaseline())
	if got == nil {
		t.Fatal("应当算出分数")
	}
	if got.Covered != 1 || got.Possible != 4 {
		t.Fatalf("覆盖度 = %d/%d，期望 1/4", got.Covered, got.Possible)
	}
	if got.Complete {
		t.Fatal("缺维度时不该标记为完整")
	}
	// 只有 CPU 计入：总分应等于 CPU 分，而不是被三个 0 稀释成 250。
	if math.Abs(got.Total-1000) > 0.001 {
		t.Fatalf("总分 = %v，期望 1000（缺失维度不参与平均）", got.Total)
	}
	for _, dimension := range got.Dimensions {
		if dimension.Key == "disk" && !dimension.Missing {
			t.Fatal("跳过的模块应标记为缺失")
		}
	}
}

// 一个维度都算不出时返回 nil：给个 0 分比不给分更误导。
func TestNoScoreWhenNothingComparable(t *testing.T) {
	data := reportWith(model.Result{ID: "dns", Status: model.StatusOK})
	if got := Compute(data, testBaseline()); got != nil {
		t.Fatalf("没有可评分维度时应返回 nil，得到 %+v", got)
	}
}

// 基线缺某项时跳过该项而不是当成 0 分。
func TestMetricWithoutBaselineIsSkipped(t *testing.T) {
	baseline := testBaseline()
	delete(baseline.Metrics, "cpu_multi")
	data := reportWith(benchResult("cpu", model.StatusOK, map[string]float64{
		"sysbench_cpu_single_events_s": 200,
		"sysbench_cpu_multi_events_s":  50,
	}))
	got := Compute(data, baseline)
	if len(got.Dimensions[0].Metrics) != 1 {
		t.Fatalf("只有一项有基线，应只计一项：%+v", got.Dimensions[0].Metrics)
	}
	if math.Abs(got.Dimensions[0].Score-2000) > 0.001 {
		t.Fatalf("维度分 = %v，期望 2000", got.Dimensions[0].Score)
	}
}

// 分数不封顶：跑赢基线一倍就该是两倍分，截断会抹掉真实差距。
func TestScoreIsNotCapped(t *testing.T) {
	data := reportWith(benchResult("cpu", model.StatusOK, map[string]float64{
		"sysbench_cpu_single_events_s": 500,
		"sysbench_cpu_multi_events_s":  500,
	}))
	got := Compute(data, testBaseline())
	if got.Total < 4999 {
		t.Fatalf("总分 = %v，期望约 5000（不封顶）", got.Total)
	}
}

// 按前缀聚合的带宽维度取中位数：某个公共节点繁忙不该拖垮整个维度。
func TestBandwidthAggregatesByMedian(t *testing.T) {
	data := reportWith(benchResult("speed", model.StatusOK, map[string]float64{
		"iperf3_target_01_ipv4_download_mbps": 100,
		"iperf3_target_02_ipv4_download_mbps": 200,
		"iperf3_target_03_ipv4_download_mbps": 10, // 繁忙节点
		"iperf3_target_01_ipv4_upload_mbps":   100,
	}))
	got := Compute(data, testBaseline())
	var bandwidth *DimensionScore
	for index := range got.Dimensions {
		if got.Dimensions[index].Key == "bandwidth" {
			bandwidth = &got.Dimensions[index]
		}
	}
	if bandwidth == nil || bandwidth.Missing {
		t.Fatal("带宽维度应当有分")
	}
	for _, metric := range bandwidth.Metrics {
		if metric.Key == "bandwidth_download" && math.Abs(metric.Value-100) > 0.001 {
			t.Fatalf("下载取中位数应为 100，得到 %v", metric.Value)
		}
	}
}

func TestMemoryExpansionUsesEqualSubgroupsAndLatencyDirection(t *testing.T) {
	data := reportWith(benchResult("memory", model.StatusOK, map[string]float64{
		"mbw_memcpy_mib_s":                        100,
		"sysbench_memory_write_single_mib_s":      100,
		"sysbench_memory_write_multi_mib_s":       300,
		"sysbench_memory_read_single_mib_s":       200,
		"sysbench_memory_read_multi_mib_s":        400,
		"sysbench_memory_write_single_latency_ms": 2,
		"sysbench_memory_write_multi_latency_ms":  1,
		"sysbench_memory_read_single_latency_ms":  2,
		"sysbench_memory_read_multi_latency_ms":   1,
	}))
	got := Compute(data, expandedMemoryBaseline())
	if got == nil {
		t.Fatal("memory expansion should produce a score")
	}
	var memory *DimensionScore
	for index := range got.Dimensions {
		if got.Dimensions[index].Key == "memory" {
			memory = &got.Dimensions[index]
			break
		}
	}
	if memory == nil || memory.Missing {
		t.Fatalf("memory dimension missing: %+v", got)
	}
	if len(memory.Groups) != 4 {
		t.Fatalf("memory groups = %+v, want copy/write/read/latency", memory.Groups)
	}
	groupScores := make(map[string]float64)
	for _, group := range memory.Groups {
		groupScores[group.Key] = group.Score
	}
	for group, want := range map[string]float64{"copy": 1000, "write": 2000, "read": 3000, "latency": 1333.3333333333333} {
		if math.Abs(groupScores[group]-want) > 0.001 {
			t.Fatalf("memory group %s = %v, want %v", group, groupScores[group], want)
		}
	}
	wantMemoryScore := (1000 + 2000 + 3000 + 1333.3333333333333) / 4
	if math.Abs(memory.Score-wantMemoryScore) > 0.001 {
		t.Fatalf("memory score = %v, want equal subgroup average %v", memory.Score, wantMemoryScore)
	}
}

func TestDiskCrystalAndATTOUseEqualSubgroups(t *testing.T) {
	values := map[string]float64{
		"fio_sequential_read_mib_s": 100, "fio_sequential_write_mib_s": 100,
		"fio_random_read_4k_iops": 100, "fio_random_write_4k_iops": 100,
	}
	for _, dimension := range Dimensions() {
		if dimension.Key != "disk" {
			continue
		}
		for _, metric := range dimension.Metrics {
			switch {
			case strings.HasPrefix(metric.Key, "crystal_"):
				values[metric.MeasurementKey] = 100
			case strings.HasPrefix(metric.Key, "fio_mixed_"):
				values[metric.MeasurementKey] = 100
			case strings.HasPrefix(metric.Key, "atto_"):
				values[metric.MeasurementKey] = 200
			}
		}
	}
	got := Compute(reportWith(benchResult("disk", model.StatusOK, values)), expandedDiskBaseline())
	if got == nil {
		t.Fatal("disk expansion should produce a score")
	}
	var disk *DimensionScore
	for index := range got.Dimensions {
		if got.Dimensions[index].Key == "disk" {
			disk = &got.Dimensions[index]
			break
		}
	}
	if disk == nil || disk.Missing || len(disk.Groups) != 4 {
		t.Fatalf("disk groups = %+v", disk)
	}
	groups := make(map[string]GroupScore)
	for _, group := range disk.Groups {
		groups[group.Key] = group
	}
	if groups["legacy"].MetricCount != 4 || groups["crystal"].MetricCount != 16 || groups["mixed"].MetricCount != 8 || groups["atto"].MetricCount != 72 {
		t.Fatalf("matrix cells were not grouped: %+v", groups)
	}
	if math.Abs(disk.Score-1250) > 0.001 {
		t.Fatalf("disk score = %v, want equal legacy/mixed/Crystal/ATTO average", disk.Score)
	}
}

func TestOldBaselinesExposeMissingExpandedMetrics(t *testing.T) {
	data := reportWith(benchResult("memory", model.StatusOK, map[string]float64{
		"mbw_memcpy_mib_s":                   100,
		"sysbench_memory_write_single_mib_s": 100,
	}))
	got := Compute(data, testBaseline())
	if got == nil {
		t.Fatal("legacy memory metrics should still score")
	}
	var memory *DimensionScore
	for index := range got.Dimensions {
		if got.Dimensions[index].Key == "memory" {
			memory = &got.Dimensions[index]
			break
		}
	}
	if memory == nil || memory.Missing || len(memory.MissingMetrics) < 3 {
		t.Fatalf("missing expanded metrics were not explicit: %+v", memory)
	}
	if got.Complete {
		t.Fatal("a legacy baseline without expanded metrics must not claim complete coverage")
	}
	for _, metric := range memory.Metrics {
		if metric.Score == 0 || metric.Baseline == 0 {
			t.Fatalf("missing metrics must not be represented as zero scores: %+v", metric)
		}
	}
}

func TestAggregateMedianHandlesEvenCount(t *testing.T) {
	if got := aggregate([]float64{10, 20, 30, 40}, AggregateMedian); math.Abs(got-25) > 0.001 {
		t.Fatalf("偶数个样本的中位数 = %v，期望 25", got)
	}
	if got := aggregate([]float64{10, 90}, AggregateMax); got != 90 {
		t.Fatalf("最大值 = %v，期望 90", got)
	}
}

// 排行榜参考由多份报告的算术平均构成；离群检测在提交流程中单独完成。
func TestBuildBaselineUsesMean(t *testing.T) {
	reports := []model.Report{
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 100})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 200})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 9000})),
	}
	baseline, err := BuildBaseline(reports, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(baseline.Metrics["cpu_single"]-3100) > 0.001 {
		t.Fatalf("参考应取算术平均 3100，得到 %v", baseline.Metrics["cpu_single"])
	}
	if baseline.SampleCount != 3 {
		t.Fatalf("样本数 = %d，期望 3", baseline.SampleCount)
	}
	// 没有任何机器测到的指标不该凭空出现在基线里。
	if _, ok := baseline.Metrics["bandwidth_download"]; ok {
		t.Fatal("未测到的指标不该进入基线")
	}
}

func TestBuildBaselineStoresScoreDistributionAndRanks(t *testing.T) {
	reports := []model.Report{
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 100})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 200})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 300})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 400})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 500})),
	}
	baseline, err := BuildBaseline(reports, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.ScoreSamples) != 5 {
		t.Fatalf("score distribution length = %d, want 5", len(baseline.ScoreSamples))
	}
	if baseline.RankMinSamples != DefaultRankMinSamples {
		t.Fatalf("rank threshold = %d, want %d", baseline.RankMinSamples, DefaultRankMinSamples)
	}
	got := Compute(reports[3], baseline)
	if got == nil {
		t.Fatal("expected score for the fourth report")
	}
	if got.RankStatus != RankStatusAvailable || got.RankSamples != 5 {
		t.Fatalf("rank status/samples = %q/%d, want available/5", got.RankStatus, got.RankSamples)
	}
	if math.Abs(got.TopPercent-40) > 0.001 {
		t.Fatalf("top percent = %v, want 40", got.TopPercent)
	}
}

func TestComputeDoesNotInventRankForSparseOrLegacyReference(t *testing.T) {
	reports := []model.Report{
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 100})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 200})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 300})),
	}
	baseline, err := BuildBaseline(reports, "sparse")
	if err != nil {
		t.Fatal(err)
	}
	got := Compute(reports[0], baseline)
	if got == nil || got.RankStatus != RankStatusInsufficient || got.RankSamples != 3 || got.RankMinSamples != DefaultRankMinSamples {
		t.Fatalf("sparse rank = %+v", got)
	}
	legacy := testBaseline()
	legacyScore := Compute(reports[0], legacy)
	if legacyScore == nil || legacyScore.RankStatus != RankStatusInsufficient || legacyScore.RankSamples != 3 || legacyScore.TopPercent != 0 {
		t.Fatalf("legacy sparse rank should be insufficient without distribution: %+v", legacyScore)
	}
	legacyLarge := legacy
	legacyLarge.SampleCount = 10
	legacyLargeScore := Compute(reports[0], legacyLarge)
	if legacyLargeScore == nil || legacyLargeScore.RankStatus != RankStatusUnavailable || legacyLargeScore.TopPercent != 0 {
		t.Fatalf("legacy rank should be unavailable without distribution: %+v", legacyLargeScore)
	}
}

func TestBuildBaselineAggregatesExpandedStableKeys(t *testing.T) {
	values := map[string]float64{
		"fio_sequential_read_mib_s": 100, "fio_sequential_write_mib_s": 100,
		"fio_random_read_4k_iops": 100, "fio_random_write_4k_iops": 100,
	}
	for _, dimension := range Dimensions() {
		if dimension.Key != "disk" {
			continue
		}
		for _, metric := range dimension.Metrics {
			if metric.MeasurementKey != "" && (strings.HasPrefix(metric.Key, "crystal_") || strings.HasPrefix(metric.Key, "fio_mixed_") || strings.HasPrefix(metric.Key, "atto_")) {
				values[metric.MeasurementKey] = 123
			}
		}
	}
	baseline, err := BuildBaseline([]model.Report{reportWith(benchResult("disk", model.StatusOK, values))}, "expanded")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"crystal_rnd4k_q1_read_mib_s", "crystal_seq1m_q8_write_iops", "fio_mixed_4k_read_mib_s", "atto_512b_read_mib_s", "atto_64m_write_iops"} {
		if got := baseline.Metrics[key]; got != 123 {
			t.Fatalf("expanded baseline %q = %v, want 123", key, got)
		}
	}
}

func TestBuildBaselineRejectsEmptyInput(t *testing.T) {
	if _, err := BuildBaseline(nil, ""); err == nil {
		t.Fatal("没有报告时应报错")
	}
	if _, err := BuildBaseline([]model.Report{reportWith()}, ""); err == nil {
		t.Fatal("报告里没有可评分指标时应报错")
	}
}

// 内置基线必须覆盖全部定义的指标，否则首次运行就会有维度算不出分。
func TestBuiltinBaselineCoversEveryMetric(t *testing.T) {
	baseline := DefaultBaseline()
	for _, dimension := range Dimensions() {
		for _, metric := range dimension.Metrics {
			if metric.Optional {
				// The embedded baseline predates the Crystal/ATTO and memory
				// latency expansion. Missing optional values are surfaced by
				// Compute and populated by newly aggregated baselines.
				continue
			}
			if value, ok := baseline.Metrics[metric.Key]; !ok || value <= 0 {
				t.Errorf("内置基线缺少指标 %q", metric.Key)
			}
		}
	}
	if !baseline.IsBuiltin() {
		t.Fatal("内置基线应能被识别，报告才能提示它只是单机快照")
	}
	if baseline.SampleCount != 1 {
		t.Fatalf("内置基线样本数 = %d，应为 1", baseline.SampleCount)
	}
}

// DefaultBaseline 必须返回副本，调用方改动不能污染下一次调用。
func TestDefaultBaselineIsCopied(t *testing.T) {
	first := DefaultBaseline()
	first.Metrics["cpu_single"] = 1
	if DefaultBaseline().Metrics["cpu_single"] == 1 {
		t.Fatal("DefaultBaseline 返回了共享的 map")
	}
}

// 维度键要与 i18n 文案键对应，漏一个就会在报告里显示成裸 key。
func TestDimensionKeysAreStable(t *testing.T) {
	want := map[string]bool{"cpu": true, "memory": true, "disk": true, "bandwidth": true}
	for _, dimension := range Dimensions() {
		if !want[dimension.Key] {
			t.Errorf("未预期的维度键 %q（新增维度需同步 i18n 的 score.dimension.* 文案）", dimension.Key)
		}
		delete(want, dimension.Key)
	}
	for key := range want {
		t.Errorf("缺少维度 %q", key)
	}
}
