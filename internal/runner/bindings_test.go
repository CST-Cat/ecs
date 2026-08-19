package runner

import (
	"context"
	"strings"
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

func bindingTestDescriptor(id string, exposure config.Exposure) config.ModuleDescriptor {
	return config.ModuleDescriptor{ID: id, Exposure: exposure}
}

func TestBindBuiltinModulesFollowsCanonicalDescriptorOrder(t *testing.T) {
	bindings := bindBuiltinModules()
	descriptors := config.ModuleDescriptors()
	if len(bindings) != len(descriptors) {
		t.Fatalf("binding count = %d, descriptor count = %d", len(bindings), len(descriptors))
	}
	for index, binding := range bindings {
		if binding.Descriptor.ID != descriptors[index].ID {
			t.Errorf("binding[%d] descriptor = %q, want %q", index, binding.Descriptor.ID, descriptors[index].ID)
		}
		if binding.Probe == nil || binding.Probe.ID() != binding.Descriptor.ID {
			t.Errorf("binding[%d] probe = %#v, want ID %q", index, binding.Probe, binding.Descriptor.ID)
		}
		if binding.Probe.NeedsNetwork() != (binding.Descriptor.Exposure > config.ExposureLocal) {
			t.Errorf("binding[%d] network policy disagrees: probe=%v descriptor=%v", index, binding.Probe.NeedsNetwork(), binding.Descriptor.Exposure > config.ExposureLocal)
		}
	}
}

func TestBindModuleProbesRejectsInvalidJoins(t *testing.T) {
	descriptors := []config.ModuleDescriptor{
		bindingTestDescriptor("local", config.ExposureLocal),
		bindingTestDescriptor("public", config.ExposurePublic),
	}
	valid := []probe.Probe{
		bindingTestProbe{id: "local"},
		bindingTestProbe{id: "public", network: true},
	}
	tests := []struct {
		name   string
		probes []probe.Probe
		want   string
	}{
		{name: "missing", probes: valid[:1], want: `module "public" has no probe`},
		{name: "duplicate", probes: append(append([]probe.Probe(nil), valid...), bindingTestProbe{id: "local"}), want: `duplicate probe ID "local"`},
		{name: "unknown", probes: append(append([]probe.Probe(nil), valid...), bindingTestProbe{id: "other"}), want: `probe "other" has no module descriptor`},
		{name: "network policy", probes: []probe.Probe{bindingTestProbe{id: "local", network: true}, valid[1]}, want: `probe "local" NeedsNetwork=true`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := bindModuleProbes(descriptors, testCase.probes)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("bindModuleProbes() error = %v, want substring %q", err, testCase.want)
			}
		})
	}
}

func TestSelectBindingsPreservesCanonicalOrder(t *testing.T) {
	descriptors := []config.ModuleDescriptor{
		bindingTestDescriptor("first", config.ExposureLocal),
		bindingTestDescriptor("second", config.ExposureLocal),
		bindingTestDescriptor("third", config.ExposureLocal),
	}
	probes := []probe.Probe{
		bindingTestProbe{id: "third"},
		bindingTestProbe{id: "first"},
		bindingTestProbe{id: "second"},
	}
	bindings, err := bindModuleProbes(descriptors, probes)
	if err != nil {
		t.Fatal(err)
	}
	selected := selectBindings(bindings, []string{"third", "first"})
	if len(selected) != 2 || selected[0].Descriptor.ID != "first" || selected[1].Descriptor.ID != "third" {
		t.Fatalf("selected binding order = %#v, want first, third", selected)
	}
}
