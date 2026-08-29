package score

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func validBaselineFixture() Baseline {
	return Baseline{
		Schema: BaselineSchema, Source: "fixture", SampleCount: 5,
		Metrics: map[string]float64{"cpu_single": 100},
		Tiers: []Tier{{
			VCPUMin: 4, SampleCount: 5, Metrics: map[string]float64{"cpu_single": 100},
			MetricSampleCounts: map[string]int{"cpu_single": 5},
		}},
		ScoreSamples:   []float64{100},
		RankMinSamples: DefaultRankMinSamples,
	}
}

func TestBaselineRankThresholdContract(t *testing.T) {
	cases := []struct {
		name    string
		rankMin int
		wantMin int
	}{
		{name: "zero uses default", rankMin: 0, wantMin: DefaultRankMinSamples},
		{name: "explicit value is preserved", rankMin: 7, wantMin: 7},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			baseline := Baseline{RankMinSamples: test.rankMin}
			if got := baseline.RankThreshold(); got != test.wantMin {
				t.Fatalf("rank threshold = %d, want %d", got, test.wantMin)
			}
		})
	}
}

func TestBuildBaselineAndRoundTrip(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	reports := []model.Report{scoreReportFixture(), scoreReportFixture()}
	if !setReportMeasurement(&reports[0], "sysbench_cpu_single_events_s", 150) || !setReportMeasurement(&reports[1], "sysbench_cpu_single_events_s", 250) {
		t.Fatal("fixture CPU measurement missing")
	}
	baseline, err := BuildBaseline(reports, "fixture source")
	if err != nil {
		t.Fatal(err)
	}
	defaultSource, err := BuildBaseline(reports, "")
	if err != nil || defaultSource.Source != "aggregated from 2 reports" {
		t.Fatalf("default baseline source = %q, err=%v", defaultSource.Source, err)
	}
	if baseline.Schema != BaselineSchema || baseline.Source != "fixture source" || baseline.SampleCount != 2 || baseline.GeneratedAt.IsZero() || baseline.GeneratedAt.Location() != time.UTC {
		t.Fatalf("baseline metadata = %+v", baseline)
	}
	if baseline.Metrics["cpu_single"] != 200 || baseline.Metrics["memory_copy"] != 120 {
		t.Fatalf("baseline means = cpu=%v memory=%v", baseline.Metrics["cpu_single"], baseline.Metrics["memory_copy"])
	}
	if len(baseline.Tiers) != 1 || baseline.Tiers[0].VCPUMin != 4 || baseline.Tiers[0].SampleCount != 2 || baseline.Tiers[0].MetricSampleCounts["cpu_single"] != 2 {
		t.Fatalf("baseline tier = %+v", baseline.Tiers)
	}
	if len(baseline.ScoreSamples) != 2 || baseline.RankThreshold() != DefaultRankMinSamples {
		t.Fatalf("baseline score distribution = %v threshold=%d", baseline.ScoreSamples, baseline.RankThreshold())
	}
	counts := MetricSampleCounts(reports)
	if counts["cpu_single"] != 2 || counts["memory_copy"] != 2 {
		t.Fatalf("metric sample counts = %v", counts)
	}

	encoded, err := baseline.Encode()
	if err != nil || len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		t.Fatalf("baseline encode = %v", err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Schema != baseline.Schema || loaded.Source != baseline.Source || loaded.SampleCount != baseline.SampleCount || loaded.Metrics["cpu_single"] != 200 || len(loaded.ScoreSamples) != 2 {
		t.Fatalf("round-trip baseline = %+v", loaded)
	}
	if _, err := BuildBaseline(nil, "fixture"); err == nil || !strings.Contains(err.Error(), "no usable report files") {
		t.Fatalf("empty baseline reports error = %v", err)
	}
	if _, err := BuildBaseline([]model.Report{{Results: []model.Result{{ID: "system", Status: model.StatusOK}}}}, "fixture"); err == nil || !strings.Contains(err.Error(), "no scoreable measurements") {
		t.Fatalf("unscorable baseline error = %v", err)
	}
}

func TestBuildBaselineTracksTierAndMetricSampleCounts(t *testing.T) {
	makeReport := func(runID string, vcpu int, single, multi float64, includeMulti bool) model.Report {
		report := scoreReportFixture()
		report.Run.ID = runID
		for resultIndex := range report.Results {
			result := &report.Results[resultIndex]
			if result.ID == "system" {
				for measurementIndex := range result.Measurements {
					if result.Measurements[measurementIndex].Key == "logical_cpus" {
						result.Measurements[measurementIndex].Value = float64(vcpu)
					}
				}
			}
			if result.ID != "cpu" {
				continue
			}
			filtered := result.Measurements[:0]
			for _, measurement := range result.Measurements {
				switch measurement.Key {
				case "sysbench_cpu_single_events_s":
					measurement.Value = single
				case "sysbench_cpu_multi_events_s":
					if !includeMulti {
						continue
					}
					measurement.Value = multi
				}
				filtered = append(filtered, measurement)
			}
			result.Measurements = filtered
		}
		return report
	}
	reports := []model.Report{
		makeReport("tier-4-a", 4, 100, 200, true),
		makeReport("tier-4-b", 4, 300, 0, false),
		makeReport("tier-8-a", 8, 500, 400, true),
	}
	baseline, err := BuildBaseline(reports, "tier fixture")
	if err != nil {
		t.Fatal(err)
	}
	if baseline.SampleCount != 3 || baseline.Metrics["cpu_single"] != 300 || baseline.Metrics["cpu_multi"] != 300 {
		t.Fatalf("global baseline = %+v", baseline)
	}
	if counts := MetricSampleCounts(reports); counts["cpu_single"] != 3 || counts["cpu_multi"] != 2 {
		t.Fatalf("global metric sample counts = %v", counts)
	}
	if len(baseline.Tiers) != 2 {
		t.Fatalf("tier baseline = %+v", baseline.Tiers)
	}
	tier4, tier8 := baseline.Tiers[0], baseline.Tiers[1]
	if tier4.VCPUMin != 4 || tier4.SampleCount != 2 || tier4.Metrics["cpu_single"] != 200 ||
		tier4.MetricSampleCounts["cpu_single"] != 2 || tier4.MetricSampleCounts["cpu_multi"] != 1 {
		t.Fatalf("4-vCPU tier = %+v", tier4)
	}
	if tier8.VCPUMin != 8 || tier8.SampleCount != 1 || tier8.Metrics["cpu_single"] != 500 ||
		tier8.MetricSampleCounts["cpu_single"] != 1 || tier8.MetricSampleCounts["cpu_multi"] != 1 {
		t.Fatalf("8-vCPU tier = %+v", tier8)
	}
}

func TestMetricSampleCountsExcludesInvalidResolvedValues(t *testing.T) {
	skipped := scoreReportFixture()
	findResult(&skipped, "cpu").Status = model.StatusSkipped
	invalid := scoreReportFixture()
	if !setReportMeasurement(&invalid, "sysbench_cpu_single_events_s", math.Inf(1)) {
		t.Fatal("fixture CPU measurement missing")
	}
	valid := scoreReportFixture()
	baseline, err := BuildBaseline([]model.Report{skipped, invalid, valid}, "validity fixture")
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Metrics["cpu_single"] != 150 {
		t.Fatalf("invalid CPU values entered baseline: %v", baseline.Metrics["cpu_single"])
	}
	counts := MetricSampleCounts([]model.Report{skipped, invalid, valid})
	if counts["cpu_single"] != 1 {
		t.Fatalf("invalid CPU values entered sample counts: %v", counts)
	}
}

func TestBaselineValidationDiagnostics(t *testing.T) {
	cases := []struct {
		name       string
		allowEmpty bool
		mutate     func(*Baseline)
		marker     string
	}{
		{name: "schema", mutate: func(b *Baseline) { b.Schema = "other/v1" }, marker: "unsupported baseline schema"},
		{name: "source", mutate: func(b *Baseline) { b.Source = "" }, marker: "source must be non-empty"},
		{name: "sample count", mutate: func(b *Baseline) { b.SampleCount = -1 }, marker: "sample_count must not be negative"},
		{name: "empty metrics", mutate: func(b *Baseline) { b.Metrics = nil }, marker: "contains no metrics"},
		{name: "metrics need samples", mutate: func(b *Baseline) { b.SampleCount = 0 }, marker: "positive sample_count"},
		{name: "empty baseline tiers", allowEmpty: true, mutate: func(b *Baseline) { b.Metrics = nil }, marker: "must not contain tiers"},
		{name: "rank threshold", mutate: func(b *Baseline) { b.RankMinSamples = -1 }, marker: "rank_min_samples must not be negative"},
		{name: "metric finite positive", mutate: func(b *Baseline) { b.Metrics["cpu_single"] = 0 }, marker: "must be positive and finite"},
		{name: "score sample finite", mutate: func(b *Baseline) { b.ScoreSamples = []float64{math.NaN()} }, marker: "score_samples must contain finite"},
		{name: "score sample count", mutate: func(b *Baseline) { b.ScoreSamples = []float64{1, 2, 3, 4, 5, 6} }, marker: "cannot exceed sample_count"},
		{name: "tier key", mutate: func(b *Baseline) { b.Tiers[0].VCPUMin = 3 }, marker: "invalid vcpu_min"},
		{name: "duplicate tier", mutate: func(b *Baseline) { b.Tiers = append(b.Tiers, b.Tiers[0]) }, marker: "duplicate tier"},
		{name: "tier sample count", mutate: func(b *Baseline) { b.Tiers[0].SampleCount = 0 }, marker: "tier 4 sample_count must be positive"},
		{name: "tier metrics", mutate: func(b *Baseline) { b.Tiers[0].Metrics = nil }, marker: "tier 4 contains no metrics"},
		{name: "tier metric counts missing", mutate: func(b *Baseline) { b.Tiers[0].MetricSampleCounts = nil }, marker: "metric_sample_counts is required"},
		{name: "tier metric counts shape", mutate: func(b *Baseline) { b.Tiers[0].MetricSampleCounts = map[string]int{} }, marker: "must cover exactly its metrics"},
		{name: "tier metric count value", mutate: func(b *Baseline) { b.Tiers[0].MetricSampleCounts["cpu_single"] = 0 }, marker: "has invalid sample count"},
		{name: "tier metric finite positive", mutate: func(b *Baseline) { b.Tiers[0].Metrics["cpu_single"] = 0 }, marker: "tier 4 metric"},
		{name: "tier totals", mutate: func(b *Baseline) {
			b.Tiers = append(b.Tiers, Tier{VCPUMin: 8, SampleCount: 5, Metrics: map[string]float64{"cpu_single": 100}, MetricSampleCounts: map[string]int{"cpu_single": 5}})
		}, marker: "tier sample counts cannot exceed"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			baseline := validBaselineFixture()
			test.mutate(&baseline)
			if err := validateBaseline(baseline, test.allowEmpty); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("validation error = %v, want %q", err, test.marker)
			}
		})
	}
	if err := validateBaseline(validBaselineFixture(), false); err != nil {
		t.Fatalf("valid baseline rejected: %v", err)
	}
}

func TestBaselineLoadContract(t *testing.T) {
	current, err := validBaselineFixture().Encode()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(current, &payload); err != nil {
		t.Fatal(err)
	}
	var tiers []map[string]json.RawMessage
	if err := json.Unmarshal(payload["tiers"], &tiers); err != nil {
		t.Fatal(err)
	}
	delete(tiers[0], "metric_sample_counts")
	payload["tiers"], err = json.Marshal(tiers)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	malformedBaseline := validBaselineFixture()
	malformedBaseline.Tiers[0].MetricSampleCounts["cpu_single"] = 0
	malformed, err := malformedBaseline.Encode()
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{
  "schema": "ecs.baseline/v1",
  "source": "legacy fixture",
  "sample_count": 5,
  "metrics": {"cpu_single": 100},
  "tiers": [{
    "vcpu_min": 4,
    "sample_count": 5,
    "metrics": {"cpu_single": 100}
  }]
}`)
	cases := []struct {
		name    string
		content []byte
		marker  string
	}{
		{name: "current valid baseline", content: current},
		{name: "missing current required field", content: missing, marker: "metric_sample_counts is required"},
		{name: "malformed counts", content: malformed, marker: "has invalid sample count"},
		{name: "old legacy fixture", content: legacy, marker: "metric_sample_counts is required"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "baseline.json")
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, loadErr := LoadBaseline(path)
			if test.marker == "" {
				if loadErr != nil {
					t.Fatalf("current baseline load error = %v", loadErr)
				}
				return
			}
			if loadErr == nil || !strings.Contains(loadErr.Error(), test.marker) {
				t.Fatalf("load error = %v, want %q", loadErr, test.marker)
			}
		})
	}
}

func TestBaselineLoadFileFailuresAndUnknownFields(t *testing.T) {
	valid, err := validBaselineFixture().Encode()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	write := func(name string, content []byte) string {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := LoadBaseline(filepath.Join(directory, "missing.json")); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Fatalf("open error = %v", err)
	}
	if _, err := LoadBaseline(write("syntax.json", []byte(`{"schema":`))); err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("syntax error = %v", err)
	}
	twoObjects := append(append([]byte(nil), valid...), valid...)
	if _, err := LoadBaseline(write("trailing.json", twoObjects)); err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("trailing error = %v", err)
	}
	unknown := map[string]any{}
	if err := json.Unmarshal(valid, &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["future_field"] = true
	unknownJSON, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(write("unknown.json", unknownJSON)); err == nil || !strings.Contains(err.Error(), `unknown field "future_field"`) {
		t.Fatalf("unknown baseline field error = %v", err)
	}
	typo := map[string]any{}
	if err := json.Unmarshal(valid, &typo); err != nil {
		t.Fatal(err)
	}
	delete(typo, "rank_min_samples")
	typo["rank_min_sample"] = 9
	typoJSON, err := json.Marshal(typo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(write("rank-min-sample-typo.json", typoJSON)); err == nil || !strings.Contains(err.Error(), `unknown field "rank_min_sample"`) {
		t.Fatalf("rank_min_sample typo error = %v", err)
	}
	largePath := filepath.Join(directory, "large.json")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(4*1024*1024 + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := LoadBaseline(largePath); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversize error = %v", err)
	}
}
