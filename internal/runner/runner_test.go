package runner

import (
	"context"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/probe"
)

type runnerTestProbe struct {
	runs int
}

func (*runnerTestProbe) ID() string         { return "test-probe" }
func (*runnerTestProbe) Title() string      { return "test probe" }
func (*runnerTestProbe) NeedsNetwork() bool { return false }
func (p *runnerTestProbe) Run(context.Context, probe.Environment) model.Result {
	p.runs++
	return model.Result{ID: p.ID(), Status: model.StatusOK, Summary: "ok"}
}

func TestRunOneExecutesProbeAndCompletesResult(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	item := &runnerTestProbe{}
	result := runOne(context.Background(), item, cfg, probe.Environment{}, true)
	if item.runs != 1 {
		t.Fatalf("probe runs = %d, want 1", item.runs)
	}
	if result.ID != item.ID() || result.Status != model.StatusOK || result.Summary != "ok" {
		t.Fatalf("result = %+v, want successful probe result", result)
	}
	if result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 1 {
		t.Fatalf("result evidence = %+v, want one completed observation", result.Evidence)
	}
}
