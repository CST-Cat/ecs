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

func TestBaselineLoadFileFailuresAndUnknownCompatibility(t *testing.T) {
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
	if _, err := LoadBaseline(write("unknown.json", unknownJSON)); err != nil {
		t.Fatalf("unknown baseline field should remain forward-compatible: %v", err)
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
