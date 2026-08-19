package score

import (
	"encoding/json"
	"testing"

	"ecs/internal/model"
)

func sampleSubmissionReport() model.Report {
	return model.Report{
		Tool: model.ToolInfo{Version: "ecs-test"},
		Run:  model.RunInfo{Profile: "standard"},
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
					{Key: "sysbench_cpu_single_events_s", Value: 900},
					{Key: "sysbench_cpu_multi_events_s", Value: 3400},
				},
			},
		},
	}
}

func TestSubmissionRoundTripsIntoBaseline(t *testing.T) {
	submission, err := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := submission.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Submission
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("encoded submission should validate: %v", err)
	}
	if decoded.ID != submission.ID || decoded.Host.VCPU != 4 {
		t.Fatalf("decoded submission = %+v, want same identity and host size", decoded)
	}

	baseline, err := BuildBaseline([]model.Report{decoded.AsReport()}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Metrics["cpu_single"] != 900 || baseline.Metrics["cpu_multi"] != 3400 {
		t.Fatalf("baseline metrics = %v, want submission CPU values", baseline.Metrics)
	}
}
