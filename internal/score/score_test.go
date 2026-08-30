package score

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func scoreReportFixture() model.Report {
	started := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	report := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "ecs-test"},
		Run: model.RunInfo{
			ID: "score-fixture", Profile: "standard", StartedAt: started,
		},
		Results: []model.Result{{
			ID: "system", Status: model.StatusOK,
			Fields: []model.Field{
				{Key: "cloud_provider", Label: "Provider", Value: model.RawValue("fixture-cloud")},
				{Key: "cloud_region", Label: "Region", Value: model.RawValue("fixture-region")},
				{Key: "virtualization", Label: "Virtualization", Value: model.RawValue("kvm")},
				{Key: "cpu_model", Label: "CPU", Value: model.RawValue("Fixture CPU")},
				{Key: "arch", Label: "Arch", Value: model.RawValue("amd64")},
			},
			Measurements: []model.Measurement{
				{Key: "logical_cpus", Value: 4},
				{Key: "memory_total_bytes", Value: 8 * (1 << 30)},
			},
		}},
	}
	for _, dimension := range Dimensions() {
		result := model.Result{ID: dimension.ModuleID, Status: model.StatusOK}
		for _, metric := range dimension.Metrics {
			value := 100.0
			switch dimension.Key {
			case "cpu":
				if metric.Key == "cpu_single" {
					value = 150
				} else {
					value = 300
				}
			case "disk":
				if metric.Group == "baseline" || metric.Group == "mixed" {
					value = 200
				}
			case "memory", "bandwidth":
				// Prefix metrics are added below so their median is meaningful.
				continue
			}
			key := metric.MeasurementKey
			if key == "" {
				key = metric.Prefix + "fixture" + metric.Suffix
			}
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: key, Label: metric.Key, Value: value, Unit: metric.Key, Method: "fixture-v1",
			})
		}
		switch dimension.Key {
		case "memory":
			result.Measurements = append(result.Measurements,
				prefixMeasurements("stream_copy_", "_mib_s", []float64{100, 140})...)
			result.Measurements = append(result.Measurements,
				prefixMeasurements("stream_scale_", "_mib_s", []float64{80, 100, 120})...)
			result.Measurements = append(result.Measurements,
				prefixMeasurements("stream_add_", "_mib_s", []float64{90})...)
			result.Measurements = append(result.Measurements,
				prefixMeasurements("stream_triad_", "_mib_s", []float64{110})...)
		case "bandwidth":
			result.Measurements = append(result.Measurements,
				prefixMeasurements("iperf3_target_", "_download_mbps", []float64{100, 200})...)
			result.Measurements = append(result.Measurements,
				prefixMeasurements("iperf3_target_", "_upload_mbps", []float64{80, 120, 160})...)
		}
		if dimension.Key == "cpu" {
			result.Fields = []model.Field{{Key: "version", Label: "Version", Value: model.RawValue("sysbench 1.0.20")}}
		}
		if dimension.Key == "disk" {
			result.Fields = []model.Field{{Key: "version", Label: "Version", Value: model.RawValue("fio-3.35")}}
		}
		if dimension.Key == "bandwidth" {
			result.Fields = []model.Field{{Key: "version", Label: "Version", Value: model.RawValue("iperf 3.12")}}
		}
		report.Results = append(report.Results, result)
	}
	return report
}

func prefixMeasurements(prefix, suffix string, values []float64) []model.Measurement {
	measurements := make([]model.Measurement, 0, len(values))
	for index, value := range values {
		measurements = append(measurements, model.Measurement{
			Key: fmt.Sprintf("%s%02d%s", prefix, index+1, suffix), Value: value,
			Unit: "fixture", Method: "fixture-v1",
		})
	}
	return measurements
}

func baselineFixture() Baseline {
	metrics := make(map[string]float64)
	for _, dimension := range Dimensions() {
		for _, metric := range dimension.Metrics {
			metrics[metric.Key] = 100
		}
	}
	return Baseline{
		Schema: BaselineSchema, Source: "fixture", SampleCount: 10,
		Metrics: metrics, RankMinSamples: DefaultRankMinSamples,
	}
}

func setReportMeasurement(report *model.Report, key string, value float64) bool {
	for resultIndex := range report.Results {
		for measurementIndex := range report.Results[resultIndex].Measurements {
			if report.Results[resultIndex].Measurements[measurementIndex].Key == key {
				report.Results[resultIndex].Measurements[measurementIndex].Value = value
				return true
			}
		}
	}
	return false
}

func dimensionScore(report *Report, key string) DimensionScore {
	for _, dimension := range report.Dimensions {
		if dimension.Key == key {
			return dimension
		}
	}
	return DimensionScore{}
}

func TestDimensionsAndComputeFullFixture(t *testing.T) {
	if err := ValidateDimensions(); err != nil {
		t.Fatalf("score descriptor contract = %v", err)
	}
	dimensions := Dimensions()
	seen := make(map[string]bool, len(dimensions))
	for _, dimension := range dimensions {
		if dimension.Key == "" || dimension.ModuleID == "" || len(dimension.Metrics) == 0 || seen[dimension.Key] {
			t.Fatalf("invalid or duplicate score dimension: %+v", dimension)
		}
		seen[dimension.Key] = true
		for _, metric := range dimension.Metrics {
			if metric.Key == "" {
				t.Fatalf("unexpected score metric contract: %+v", metric)
			}
		}
	}
	if embedded := EmbeddedBaseline(); embedded.Schema != BaselineSchema || embedded.Source == "" || len(embedded.Metrics) != 0 {
		t.Fatalf("embedded baseline = %+v", embedded)
	}
	if Compute(scoreReportFixture(), EmbeddedBaseline()) != nil {
		t.Fatal("empty embedded baseline unexpectedly produced a score")
	}

	got := Compute(scoreReportFixture(), baselineFixture())
	if got == nil {
		t.Fatal("full report did not produce a score")
	}
	if got.Covered != len(dimensions) || got.Possible != len(dimensions) || !got.Complete {
		t.Fatalf("coverage = %d/%d complete=%v", got.Covered, got.Possible, got.Complete)
	}
	if got.Total != 1537.5 || got.Ratio != 1.5375 {
		t.Fatalf("total/ratio = %v/%v, want 1537.5/1.5375", got.Total, got.Ratio)
	}
	if cpu := dimensionScore(got, "cpu"); cpu.Score != 2250 || len(cpu.Metrics) != 2 {
		t.Fatalf("CPU score = %+v", cpu)
	}
	if memory := dimensionScore(got, "memory"); memory.Score != 1050 || len(memory.Groups) != 4 {
		t.Fatalf("memory median/groups = %+v", memory)
	}
	disk := dimensionScore(got, "disk")
	if disk.Score != 1500 || len(disk.Groups) != 4 {
		t.Fatalf("disk equal-weight groups = %+v", disk)
	}
	for _, group := range disk.Groups {
		if group.Key == "atto" && group.Score != 1000 {
			t.Fatalf("wide ATTO group changed disk weighting: %+v", group)
		}
	}
	if bandwidth := dimensionScore(got, "bandwidth"); bandwidth.Score != 1350 || len(bandwidth.Metrics) != 2 {
		t.Fatalf("bandwidth median = %+v", bandwidth)
	}
}

func TestComputeMissingAndInvalidValues(t *testing.T) {
	baseline := baselineFixture()
	cases := []struct {
		name       string
		mutate     func(*model.Report)
		wantKey    string
		wantReason string
	}{
		{name: "module not run", mutate: func(report *model.Report) { findResult(report, "memory").Status = model.StatusSkipped }, wantKey: "memory", wantReason: "moduleNotRun"},
		{name: "ran without comparable metric", mutate: func(report *model.Report) { findResult(report, "memory").Measurements = nil }, wantKey: "memory", wantReason: "noComparableMetric"},
		{name: "NaN values", mutate: func(report *model.Report) { setCPUValues(report, math.NaN()) }, wantKey: "cpu", wantReason: "noComparableMetric"},
		{name: "zero values", mutate: func(report *model.Report) { setCPUValues(report, 0) }, wantKey: "cpu", wantReason: "noComparableMetric"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			report := scoreReportFixture()
			test.mutate(&report)
			got := Compute(report, baseline)
			if got == nil || dimensionScore(got, test.wantKey).MissingReason != test.wantReason {
				t.Fatalf("%s missing result = %+v", test.wantKey, got)
			}
		})
	}
	warningError := scoreReportFixture()
	findResult(&warningError, "cpu").Status = model.StatusWarning
	findResult(&warningError, "disk").Status = model.StatusError
	if got := Compute(warningError, baseline); got == nil || dimensionScore(got, "cpu").Missing || dimensionScore(got, "disk").Missing {
		t.Fatalf("warning/error modules should still score: %+v", got)
	}
	duplicate := scoreReportFixture()
	cpu := findResult(&duplicate, "cpu")
	cpu.Measurements = append(cpu.Measurements, model.Measurement{Key: "sysbench_cpu_single_events_s", Value: 999})
	gotDuplicate := Compute(duplicate, baseline)
	if gotDuplicate != nil {
		t.Fatalf("duplicate measurement report was accepted: %+v", gotDuplicate)
	}
	duplicateResultID := scoreReportFixture()
	duplicateResultID.Results = append(duplicateResultID.Results, model.Result{ID: "cpu", Status: model.StatusOK})
	if gotDuplicate := Compute(duplicateResultID, baseline); gotDuplicate != nil {
		t.Fatalf("duplicate result ID report was accepted: %+v", gotDuplicate)
	}
	if got := Compute(model.Report{Results: []model.Result{{ID: "system", Status: model.StatusOK}}}, baseline); got != nil {
		t.Fatal("report with no scoreable dimensions should return nil")
	}
}

func TestMeasurementLookupPreservesModuleOwner(t *testing.T) {
	report := model.Report{Results: []model.Result{
		{ID: "cpu", Status: model.StatusOK, Measurements: []model.Measurement{{Key: "foo", Value: 100, Unit: "cpu"}}},
		{ID: "system", Status: model.StatusOK, Measurements: []model.Measurement{{Key: "foo", Value: 999, Unit: "system"}}},
	}}
	values, err := collectMeasurements(report)
	if err != nil {
		t.Fatal(err)
	}
	dimension := Dimension{
		Key:      "owner",
		ModuleID: "cpu",
		Metrics:  []Metric{{Key: "foo", MeasurementKey: "foo", HigherIsBetter: true}},
	}
	got := scoreDimension(dimension, values, Baseline{Metrics: map[string]float64{"foo": 100}}, true)
	if got.Missing || len(got.Metrics) != 1 || got.Metrics[0].Value != 100 || got.Metrics[0].Unit != "cpu" {
		t.Fatalf("module-scoped metric lookup = %+v, want cpu/foo=100", got)
	}
}

func TestScoreIgnoresSameMetricKeyFromOtherModule(t *testing.T) {
	baseline := baselineFixture()
	want := Compute(scoreReportFixture(), baseline)
	if want == nil {
		t.Fatal("baseline fixture did not score")
	}

	withOther := scoreReportFixture()
	withOther.Results = append(withOther.Results, model.Result{
		ID: "other", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "sysbench_cpu_single_events_s", Value: 999999},
			{Key: "sysbench_cpu_multi_events_s", Value: 999999},
		},
	})
	got := Compute(withOther, baseline)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("other-module CPU measurements changed score:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestScoreAndSubmissionAreOrderInvariant(t *testing.T) {
	first := scoreReportFixture()
	second := scoreReportFixture()
	for left, right := 0, len(second.Results)-1; left < right; left, right = left+1, right-1 {
		second.Results[left], second.Results[right] = second.Results[right], second.Results[left]
	}

	baseline := baselineFixture()
	firstScore := Compute(first, baseline)
	secondScore := Compute(second, baseline)
	if !reflect.DeepEqual(firstScore, secondScore) {
		t.Fatalf("result order changed score:\n first=%+v\nsecond=%+v", firstScore, secondScore)
	}
	firstSubmission, err := BuildSubmission(first, SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secondSubmission, err := BuildSubmission(second, SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstSubmission, secondSubmission) {
		t.Fatalf("result order changed submission:\n first=%+v\nsecond=%+v", firstSubmission, secondSubmission)
	}
}

func TestBuildBaselineRejectsDuplicateIdentity(t *testing.T) {
	duplicateMeasurement := scoreReportFixture()
	cpu := findResult(&duplicateMeasurement, "cpu")
	cpu.Measurements = append(cpu.Measurements, model.Measurement{Key: "sysbench_cpu_single_events_s", Value: 999})
	if _, err := BuildBaseline([]model.Report{duplicateMeasurement}, "duplicate measurement"); err == nil || !strings.Contains(err.Error(), `duplicate measurement key "sysbench_cpu_single_events_s"`) {
		t.Fatalf("duplicate measurement baseline error = %v", err)
	}

	duplicateResult := scoreReportFixture()
	duplicateResult.Results = append(duplicateResult.Results, model.Result{ID: "cpu", Status: model.StatusOK})
	if _, err := BuildBaseline([]model.Report{duplicateResult}, "duplicate result"); err == nil || !strings.Contains(err.Error(), `duplicate result ID "cpu"`) {
		t.Fatalf("duplicate result baseline error = %v", err)
	}
}

func TestScoreableMetricMembershipMatchesAcrossArtifacts(t *testing.T) {
	report := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Version: "ecs-test"},
		Run: model.RunInfo{
			ID: "scoreable-membership", Profile: "full", StartedAt: time.Unix(1700000000, 0).UTC(),
		},
		Results: []model.Result{
			{
				ID: "system", Status: model.StatusOK,
				Measurements: []model.Measurement{
					{Key: "logical_cpus", Value: 4},
					{Key: "memory_total_bytes", Value: 8 * (1 << 30)},
				},
			},
			{
				ID: "cpu", Status: model.StatusOK,
				Measurements: []model.Measurement{
					{Key: "sysbench_cpu_single_events_s", Value: 100},
					// cpu_multi is intentionally missing.
				},
			},
			{
				ID: "memory", Status: model.StatusOK,
				Measurements: []model.Measurement{
					{Key: "stream_copy_1t_mib_s", Value: 100},
					{Key: "stream_copy_nt_mib_s", Value: 300},
					{Key: "stream_scale_1t_mib_s", Value: 0},
					{Key: "stream_add_1t_mib_s", Value: -1},
					{Key: "stream_triad_1t_mib_s", Value: math.NaN()},
				},
			},
			{
				ID: "disk", Status: model.StatusSkipped,
				Measurements: []model.Measurement{{Key: "fio_sequential_read_mib_s", Value: 500}},
			},
			{
				ID: "speed", Status: model.StatusOK,
				Measurements: []model.Measurement{
					{Key: "iperf3_target_01_ipv4_download_mbps", Value: 100},
					{Key: "iperf3_target_02_ipv4_download_mbps", Value: 300},
					{Key: "iperf3_target_01_ipv4_upload_mbps", Value: math.Inf(1)},
				},
			},
		},
	}
	wantMetrics := map[string]float64{
		"cpu_single":         100,
		"memory_copy":        200,
		"bandwidth_download": 200,
	}

	baseline, err := BuildBaseline([]model.Report{report}, "membership fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline.Metrics, wantMetrics) {
		t.Fatalf("baseline metrics = %v, want %v", baseline.Metrics, wantMetrics)
	}
	wantCounts := map[string]int{
		"cpu_single":         1,
		"memory_copy":        1,
		"bandwidth_download": 1,
	}
	if counts := MetricSampleCounts([]model.Report{report}); !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("metric sample counts = %v, want %v", counts, wantCounts)
	}
	submission, err := BuildSubmission(report, SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(submission.Metrics, wantMetrics) {
		t.Fatalf("submission metrics = %v, want %v", submission.Metrics, wantMetrics)
	}
}

func TestRankingStates(t *testing.T) {
	cases := []struct {
		name          string
		samples       []float64
		sampleCount   int
		threshold     int
		wantStatus    string
		wantSamples   int
		wantTopPct    float64
		wantMinSample int
	}{
		{name: "available distribution", samples: []float64{1000, 1500, 1600, 1700, 2000}, sampleCount: 10, threshold: 5, wantStatus: RankStatusAvailable, wantSamples: 5, wantTopPct: 60, wantMinSample: 5},
		{name: "insufficient distribution", samples: []float64{1000, 1500}, sampleCount: 10, threshold: 5, wantStatus: RankStatusInsufficient, wantSamples: 2, wantMinSample: 5},
		{name: "omitted threshold fallback", samples: []float64{1000, 1500}, sampleCount: 10, threshold: 0, wantStatus: RankStatusInsufficient, wantSamples: 2, wantMinSample: 5},
		{name: "no distribution with enough hosts", sampleCount: 10, threshold: 5, wantStatus: RankStatusUnavailable, wantSamples: 10, wantMinSample: 5},
		{name: "no distribution with few hosts", sampleCount: 2, threshold: 5, wantStatus: RankStatusInsufficient, wantSamples: 2, wantMinSample: 5},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			baseline := baselineFixture()
			baseline.ScoreSamples = test.samples
			baseline.SampleCount = test.sampleCount
			baseline.RankMinSamples = test.threshold
			got := Compute(scoreReportFixture(), baseline)
			if got == nil || got.EffectiveRankStatus() != test.wantStatus || got.EffectiveRankSamples() != test.wantSamples || got.EffectiveRankMinSamples() != test.wantMinSample {
				t.Fatalf("rank = %+v", got)
			}
			if test.wantStatus == RankStatusAvailable && got.TopPercent != test.wantTopPct {
				t.Fatalf("top percent = %v, want %v", got.TopPercent, test.wantTopPct)
			}
		})
	}
}

func findResult(report *model.Report, id string) *model.Result {
	for index := range report.Results {
		if report.Results[index].ID == id {
			return &report.Results[index]
		}
	}
	return nil
}

func setCPUValues(report *model.Report, value float64) {
	result := findResult(report, "cpu")
	for index := range result.Measurements {
		if result.Measurements[index].Key == "sysbench_cpu_single_events_s" || result.Measurements[index].Key == "sysbench_cpu_multi_events_s" {
			result.Measurements[index].Value = value
		}
	}
}
