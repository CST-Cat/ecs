package model

import (
	"strings"
	"testing"
)

func TestValidateReportIdentity(t *testing.T) {
	tests := []struct {
		name    string
		report  Report
		wantErr string
	}{
		{
			name:    "empty result ID",
			report:  Report{Results: []Result{{ID: ""}}},
			wantErr: "empty result ID",
		},
		{
			name:    "whitespace result ID",
			report:  Report{Results: []Result{{ID: " \t"}}},
			wantErr: "empty result ID",
		},
		{
			name: "duplicate result ID",
			report: Report{Results: []Result{
				{ID: "cpu"}, {ID: "cpu"},
			}},
			wantErr: `duplicate result ID "cpu"`,
		},
		{
			name: "empty measurement key",
			report: Report{Results: []Result{{
				ID: "cpu", Measurements: []Measurement{{Key: ""}},
			}}},
			wantErr: "empty measurement key",
		},
		{
			name: "whitespace measurement key",
			report: Report{Results: []Result{{
				ID: "cpu", Measurements: []Measurement{{Key: "\n\t"}},
			}}},
			wantErr: "empty measurement key",
		},
		{
			name: "duplicate measurement key in owner",
			report: Report{Results: []Result{{
				ID: "cpu", Measurements: []Measurement{{Key: "events"}, {Key: "events"}},
			}}},
			wantErr: `duplicate measurement key "events"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateReportIdentity(test.report)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateReportIdentity error = %v, want %q", err, test.wantErr)
			}
		})
	}

	t.Run("same key under different result owners is allowed", func(t *testing.T) {
		report := Report{Results: []Result{
			{ID: "cpu", Measurements: []Measurement{{Key: "events"}}},
			{ID: "system", Measurements: []Measurement{{Key: "events"}}},
		}}
		if err := ValidateReportIdentity(report); err != nil {
			t.Fatalf("cross-owner measurement key rejected: %v", err)
		}
	})
}
