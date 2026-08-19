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

func TestBindBuiltinModulesAndSelectCanonicalOrder(t *testing.T) {
	builtins := bindBuiltinModules()
	descriptors := config.ModuleDescriptors()
	if len(builtins) != len(descriptors) {
		t.Fatalf("built-in binding count = %d, descriptors = %d", len(builtins), len(descriptors))
	}
	for index, binding := range builtins {
		if binding.Descriptor.ID != descriptors[index].ID || binding.Probe == nil || binding.Probe.ID() != descriptors[index].ID {
			t.Fatalf("binding[%d] = descriptor %q/probe %v, want canonical pair", index, binding.Descriptor.ID, binding.Probe)
		}
	}

	bindings := []moduleBinding{
		{Descriptor: config.ModuleDescriptor{ID: "first"}},
		{Descriptor: config.ModuleDescriptor{ID: "second"}},
		{Descriptor: config.ModuleDescriptor{ID: "third"}},
	}
	selected := selectBindings(bindings, []string{"third", "first", "third", "missing"})
	if len(selected) != 2 || selected[0].Descriptor.ID != "first" || selected[1].Descriptor.ID != "third" {
		t.Fatalf("selected bindings = %+v, want canonical first/third set", selected)
	}
}

func TestBindModuleProbesRejectsDistinctContractErrors(t *testing.T) {
	cases := []struct {
		name   string
		descr  func() []config.ModuleDescriptor
		probes func() []probe.Probe
		marker string
	}{
		{name: "empty descriptor", descr: func() []config.ModuleDescriptor { return []config.ModuleDescriptor{{}} }, probes: func() []probe.Probe { return nil }, marker: "empty ID"},
		{name: "duplicate descriptor", descr: func() []config.ModuleDescriptor { return []config.ModuleDescriptor{{ID: "local"}, {ID: "local"}} }, probes: func() []probe.Probe { return nil }, marker: "duplicate module descriptor"},
		{name: "nil probe", descr: func() []config.ModuleDescriptor { return []config.ModuleDescriptor{{ID: "local"}} }, probes: func() []probe.Probe { return []probe.Probe{nil} }, marker: "probe 0 is nil"},
		{name: "empty probe ID", descr: func() []config.ModuleDescriptor { return []config.ModuleDescriptor{{ID: "local"}} }, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{}} }, marker: "has empty ID"},
		{name: "unknown probe", descr: func() []config.ModuleDescriptor { return []config.ModuleDescriptor{{ID: "local"}} }, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{id: "other"}} }, marker: "has no module descriptor"},
		{name: "duplicate probe", descr: func() []config.ModuleDescriptor { return []config.ModuleDescriptor{{ID: "local"}} }, probes: func() []probe.Probe {
			return []probe.Probe{bindingTestProbe{id: "local"}, bindingTestProbe{id: "local"}}
		}, marker: "duplicate probe ID"},
		{name: "missing probe", descr: func() []config.ModuleDescriptor {
			return []config.ModuleDescriptor{{ID: "local"}, {ID: "remote", Exposure: config.ExposurePublic}}
		}, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{id: "local"}} }, marker: "has no probe"},
		{name: "network metadata mismatch", descr: func() []config.ModuleDescriptor {
			return []config.ModuleDescriptor{{ID: "remote", Exposure: config.ExposurePublic}}
		}, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{id: "remote"}} }, marker: "NeedsNetwork=false"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := bindModuleProbes(test.descr(), test.probes())
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("binding error = %v, want %q", err, test.marker)
			}
		})
	}
}

func TestHasNetworkModulesUsesDescriptorExposureAndProbeFallback(t *testing.T) {
	local := moduleBinding{Descriptor: config.ModuleDescriptor{ID: "local", Exposure: config.ExposureLocal}, Probe: bindingTestProbe{id: "local", network: true}}
	remote := moduleBinding{Descriptor: config.ModuleDescriptor{ID: "remote", Exposure: config.ExposurePublic}, Probe: bindingTestProbe{id: "remote", network: false}}
	fallback := moduleBinding{Probe: bindingTestProbe{id: "custom", network: true}}
	if hasNetworkModules(nil) || hasNetworkModules([]moduleBinding{local}) {
		t.Fatal("local or empty selection unexpectedly requires network")
	}
	if !hasNetworkModules([]moduleBinding{remote}) || !hasNetworkModules([]moduleBinding{fallback}) {
		t.Fatal("network descriptor/probe fallback was not detected")
	}
}
