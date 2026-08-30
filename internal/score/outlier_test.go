package score

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestDetectOutlierStates(t *testing.T) {
	makeSubmission := func(id string, vcpu int, cpu, memory float64) Submission {
		return Submission{ID: id, SampleID: id, Host: HostSpec{VCPU: vcpu}, Metrics: map[string]float64{
			"cpu_single": cpu, "memory_copy": memory,
		}}
	}
	normal := make([]Submission, 0, 8)
	for index := 0; index < 8; index++ {
		normal = append(normal, makeSubmission(fmt.Sprintf("normal-%d", index), 4, 100+float64(index), 200+float64(index)))
	}
	flat := make([]Submission, 0, 9)
	for index := 0; index < 9; index++ {
		flat = append(flat, makeSubmission(fmt.Sprintf("flat-%d", index), 4, 100, 200))
	}
	flat[len(flat)-1].Metrics["cpu_single"] = 300
	spikes := append([]Submission(nil), normal...)
	spikes = append(spikes, makeSubmission("spike", 4, 10000, 1))
	spikes = append(spikes,
		makeSubmission("large-1", 8, 500, 600),
		makeSubmission("large-2", 8, 510, 610),
	)

	cases := []struct {
		name            string
		submissions     []Submission
		wantOutliers    int
		wantUndecidable []Undecidable
	}{
		{
			name:        "too few samples",
			submissions: normal[:2],
			wantUndecidable: []Undecidable{
				{TierMinVCPU: 4, MetricKey: "cpu_single", SampleCount: 2, Required: 8, Reason: UndecidableInsufficientSamples},
				{TierMinVCPU: 4, MetricKey: "memory_copy", SampleCount: 2, Required: 8, Reason: UndecidableInsufficientSamples},
			},
		},
		{name: "ordinary spread", submissions: normal},
		{
			name:        "zero dispersion",
			submissions: flat,
			wantUndecidable: []Undecidable{
				{TierMinVCPU: 4, MetricKey: "cpu_single", SampleCount: 9, Required: 8, Reason: UndecidableZeroDispersion},
				{TierMinVCPU: 4, MetricKey: "memory_copy", SampleCount: 9, Required: 8, Reason: UndecidableZeroDispersion},
			},
		},
		{
			name:         "metric and tier outliers",
			submissions:  spikes,
			wantOutliers: 2,
			wantUndecidable: []Undecidable{
				{TierMinVCPU: 8, MetricKey: "cpu_single", SampleCount: 2, Required: 8, Reason: UndecidableInsufficientSamples},
				{TierMinVCPU: 8, MetricKey: "memory_copy", SampleCount: 2, Required: 8, Reason: UndecidableInsufficientSamples},
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			samples := make([]OutlierSample, 0, len(test.submissions))
			for _, submission := range test.submissions {
				samples = append(samples, submission.OutlierSample())
			}
			got := DetectOutliers(samples)
			if len(got.Outliers) != test.wantOutliers {
				t.Fatalf("outliers = %+v, want %d", got.Outliers, test.wantOutliers)
			}
			if !reflect.DeepEqual(got.Undecidable, test.wantUndecidable) {
				t.Fatalf("undecidable facts = %+v, want %+v", got.Undecidable, test.wantUndecidable)
			}
			if test.name != "metric and tier outliers" {
				return
			}
			seen := make(map[string]bool)
			for _, outlier := range got.Outliers {
				if outlier.SampleID != "spike" || outlier.SampleCount != 9 || outlier.TierMinVCPU != 4 || outlier.Ratio <= 1 {
					t.Fatalf("outlier facts = %+v", outlier)
				}
				seen[outlier.MetricKey] = true
			}
			if !seen["cpu_single"] || !seen["memory_copy"] {
				t.Fatalf("outlier metric keys = %v", seen)
			}
		})
	}
}

func TestDetectOutliersIsRepresentationInvariant(t *testing.T) {
	values := []float64{98, 99, 100, 101, 102, 103, 104, 105, 1000}
	reportSamples := make([]OutlierSample, 0, len(values))
	submissionSamples := make([]OutlierSample, 0, len(values))
	for index, value := range values {
		var multi *float64
		if index == 0 {
			multiValue := 3400.0
			multi = &multiValue
		}
		report := outlierReportFixture(fmt.Sprintf("representation-%d", index), value, multi)
		submission, err := BuildSubmission(report, SubmissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		reportSample, err := OutlierSampleFromReport(report)
		if err != nil {
			t.Fatal(err)
		}
		reportSamples = append(reportSamples, reportSample)
		submissionSamples = append(submissionSamples, submission.OutlierSample())
	}

	fromReports := DetectOutliers(reportSamples)
	fromSubmissions := DetectOutliers(submissionSamples)
	if !reflect.DeepEqual(fromReports, fromSubmissions) {
		t.Fatalf("report/submission outlier results differ:\nreports=%+v\nsubmissions=%+v", fromReports, fromSubmissions)
	}
	if len(fromReports.Outliers) == 0 || len(fromReports.Undecidable) == 0 {
		t.Fatalf("fixture did not cover both outlier states: %+v", fromReports)
	}
	for _, outlier := range fromReports.Outliers {
		if outlier.TierMinVCPU != 4 || outlier.MetricKey != "cpu_single" || outlier.SampleCount != len(values) {
			t.Fatalf("outlier grouping = %+v", outlier)
		}
	}
	wantUndecidable := Undecidable{
		TierMinVCPU: 4, MetricKey: "cpu_multi", SampleCount: 1, Required: 8, Reason: UndecidableInsufficientSamples,
	}
	if len(fromReports.Undecidable) != 1 || fromReports.Undecidable[0] != wantUndecidable {
		t.Fatalf("undecidable facts = %+v, want %+v", fromReports.Undecidable, wantUndecidable)
	}
}

func TestDetectOutliersMixedRepresentationsAreInvariant(t *testing.T) {
	values := []float64{98, 99, 100, 101, 102, 103, 104, 105, 1000}
	allReportSamples := make([]OutlierSample, 0, len(values))
	mixedSamples := make([]OutlierSample, 0, len(values))
	for index, value := range values {
		var multi *float64
		if index == 0 {
			multiValue := 3400.0
			multi = &multiValue
		}
		report := outlierReportFixture(fmt.Sprintf("mixed-%d", index), value, multi)
		submission, err := BuildSubmission(report, SubmissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		reportSample, err := OutlierSampleFromReport(report)
		if err != nil {
			t.Fatal(err)
		}
		allReportSamples = append(allReportSamples, reportSample)
		if index%2 == 0 {
			mixedSamples = append(mixedSamples, reportSample)
		} else {
			mixedSamples = append(mixedSamples, submission.OutlierSample())
		}
	}
	want := DetectOutliers(allReportSamples)
	got := DetectOutliers(mixedSamples)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed report/submission outlier results differ:\nwant=%+v\ngot=%+v", want, got)
	}
}

func outlierReportFixture(runID string, single float64, multi *float64) model.Report {
	report := model.Report{
		Tool: model.ToolInfo{Version: "test"},
		Run:  model.RunInfo{ID: runID, Profile: "full", StartedAt: time.Unix(1700000000, 0).UTC()},
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
				Measurements: []model.Measurement{{Key: "sysbench_cpu_single_events_s", Value: single}},
			},
		},
	}
	if multi != nil {
		report.Results[1].Measurements = append(report.Results[1].Measurements,
			model.Measurement{Key: "sysbench_cpu_multi_events_s", Value: *multi})
	}
	return report
}
