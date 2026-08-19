package score

import (
	"fmt"
	"strings"
	"testing"
)

func TestDetectOutlierStates(t *testing.T) {
	makeSubmission := func(id string, vcpu int, cpu, memory float64) Submission {
		return Submission{ID: id, Host: HostSpec{VCPU: vcpu}, Metrics: map[string]float64{
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
		name          string
		submissions   []Submission
		wantOutliers  int
		wantUndecided bool
		markers       []string
	}{
		{name: "too few samples", submissions: normal[:2], wantUndecided: true, markers: []string{"4–7 vCPU", "仅 2 个样本"}},
		{name: "ordinary spread", submissions: normal},
		{name: "zero dispersion", submissions: flat, wantUndecided: true, markers: []string{"样本离散度为零"}},
		{name: "metric and tier outliers", submissions: spikes, wantOutliers: 2, wantUndecided: true, markers: []string{"8–15 vCPU"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := DetectOutliers(test.submissions)
			if len(got.Outliers) != test.wantOutliers {
				t.Fatalf("outliers = %+v, want %d", got.Outliers, test.wantOutliers)
			}
			if test.wantUndecided != (len(got.Undecidable) > 0) {
				t.Fatalf("undecidable = %v, want present=%v", got.Undecidable, test.wantUndecided)
			}
			for _, marker := range test.markers {
				found := false
				for _, notice := range got.Undecidable {
					if strings.Contains(notice, marker) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("undecidable = %v, missing %q", got.Undecidable, marker)
				}
			}
			if test.name != "metric and tier outliers" {
				return
			}
			seen := make(map[string]bool)
			for _, outlier := range got.Outliers {
				if outlier.SubmissionID != "spike" || outlier.SampleCount != 9 || outlier.TierLabel != "4–7 vCPU" || outlier.Ratio <= 1 || !strings.Contains(outlier.Describe(), outlier.MetricKey) {
					t.Fatalf("outlier = %+v, description=%q", outlier, outlier.Describe())
				}
				seen[outlier.MetricKey] = true
			}
			if !seen["cpu_single"] || !seen["memory_copy"] {
				t.Fatalf("outlier metric keys = %v", seen)
			}
		})
	}
}
