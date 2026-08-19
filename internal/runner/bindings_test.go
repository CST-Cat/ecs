package runner

import (
	"context"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/probe"
)

type bindingTestProbe struct {
	id      string
	network bool
}

func (p bindingTestProbe) ID() string                                          { return p.id }
func (p bindingTestProbe) Title() string                                       { return p.id }
func (p bindingTestProbe) NeedsNetwork() bool                                  { return p.network }
func (p bindingTestProbe) Run(context.Context, probe.Environment) model.Result { return model.Result{} }

func TestBindModuleProbesAssemblesDescriptorAndProbe(t *testing.T) {
	descriptor := config.ModuleDescriptor{ID: "local", Exposure: config.ExposureLocal}
	bindings, err := bindModuleProbes([]config.ModuleDescriptor{descriptor}, []probe.Probe{
		bindingTestProbe{id: descriptor.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].Descriptor.ID != descriptor.ID || bindings[0].Probe.ID() != descriptor.ID {
		t.Fatalf("binding = %#v, want one matching descriptor and probe", bindings)
	}
}
