package score

import (
	"math"
	"testing"

	"ecs/internal/model"
)

func reportWith(results ...model.Result) model.Report {
	return model.Report{Results: results}
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

func TestAggregateMedianHandlesEvenCount(t *testing.T) {
	if got := aggregate([]float64{10, 20, 30, 40}, AggregateMedian); math.Abs(got-25) > 0.001 {
		t.Fatalf("偶数个样本的中位数 = %v，期望 25", got)
	}
	if got := aggregate([]float64{10, 90}, AggregateMax); got != 90 {
		t.Fatalf("最大值 = %v，期望 90", got)
	}
}

// 基线由多份报告的中位数构成：一台异常机器不该把基线拽走。
func TestBuildBaselineUsesMedian(t *testing.T) {
	reports := []model.Report{
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 100})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 200})),
		reportWith(benchResult("cpu", model.StatusOK, map[string]float64{"sysbench_cpu_single_events_s": 9000})),
	}
	baseline, err := BuildBaseline(reports, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(baseline.Metrics["cpu_single"]-200) > 0.001 {
		t.Fatalf("基线应取中位数 200，得到 %v", baseline.Metrics["cpu_single"])
	}
	if baseline.SampleCount != 3 {
		t.Fatalf("样本数 = %d，期望 3", baseline.SampleCount)
	}
	// 没有任何机器测到的指标不该凭空出现在基线里。
	if _, ok := baseline.Metrics["bandwidth_download"]; ok {
		t.Fatal("未测到的指标不该进入基线")
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
