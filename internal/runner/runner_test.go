package runner

import (
	"context"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestOfflineNetworkProbeIsSkipped(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network"}
	cfg.Offline = true
	report := Run(context.Background(), cfg, nil)
	if len(report.Results) != 1 || report.Results[0].Status != model.StatusSkipped {
		t.Fatalf("results = %+v", report.Results)
	}
	if report.Summary.Skipped != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Results[0].Methodology.Label != "第三方评估" {
		t.Fatalf("methodology = %+v", report.Results[0].Methodology)
	}
}
