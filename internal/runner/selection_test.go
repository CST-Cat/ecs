package runner

import (
	"context"
	"testing"

	"ecs/internal/model"
	"ecs/internal/module"
	"ecs/internal/probe"
)

type selectionTestProbe struct{ id string }

func (item selectionTestProbe) ID() string { return item.id }

func (item selectionTestProbe) Run(_ context.Context, _ probe.Environment) model.Result {
	return model.Result{ID: item.id}
}

func TestSelectDefinitionsPreservesCanonicalOrderAndDeduplicatesRequests(t *testing.T) {
	definitions := []probe.Definition{
		{Descriptor: module.Descriptor{ID: "first"}, Probe: selectionTestProbe{id: "first"}},
		{Descriptor: module.Descriptor{ID: "second"}, Probe: selectionTestProbe{id: "second"}},
		{Descriptor: module.Descriptor{ID: "third"}, Probe: selectionTestProbe{id: "third"}},
	}
	selected := selectDefinitions(definitions, []string{"third", "first", "third", "missing"})
	if len(selected) != 2 || selected[0].Descriptor.ID != "first" || selected[1].Descriptor.ID != "third" {
		t.Fatalf("selected definitions = %+v, want canonical first/third set", selected)
	}
}

func TestHasNetworkModulesUsesDescriptorExposure(t *testing.T) {
	local := probe.Definition{Descriptor: module.Descriptor{ID: "local", Exposure: module.ExposureLocal}, Probe: selectionTestProbe{id: "local"}}
	remote := probe.Definition{Descriptor: module.Descriptor{ID: "remote", Exposure: module.ExposurePublic}, Probe: selectionTestProbe{id: "remote"}}
	fallback := probe.Definition{Probe: selectionTestProbe{id: "custom"}}
	if hasNetworkModules(nil) || hasNetworkModules([]probe.Definition{local}) {
		t.Fatal("local or empty selection unexpectedly requires network")
	}
	if !hasNetworkModules([]probe.Definition{remote}) || hasNetworkModules([]probe.Definition{fallback}) {
		t.Fatal("network detection did not use descriptor metadata exclusively")
	}
}
