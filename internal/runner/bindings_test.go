package runner

import (
	"context"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/module"
	"ecs/internal/probe"
)

type bindingTestProbe struct {
	id string
}

func (p bindingTestProbe) ID() string                                          { return p.id }
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
		{Descriptor: module.Descriptor{ID: "first"}},
		{Descriptor: module.Descriptor{ID: "second"}},
		{Descriptor: module.Descriptor{ID: "third"}},
	}
	selected := selectBindings(bindings, []string{"third", "first", "third", "missing"})
	if len(selected) != 2 || selected[0].Descriptor.ID != "first" || selected[1].Descriptor.ID != "third" {
		t.Fatalf("selected bindings = %+v, want canonical first/third set", selected)
	}
}

func TestBindModuleProbesRejectsDistinctContractErrors(t *testing.T) {
	cases := []struct {
		name   string
		descr  func() []module.Descriptor
		probes func() []probe.Probe
		marker string
	}{
		{name: "empty descriptor", descr: func() []module.Descriptor { return []module.Descriptor{{}} }, probes: func() []probe.Probe { return nil }, marker: "empty ID"},
		{name: "duplicate descriptor", descr: func() []module.Descriptor { return []module.Descriptor{{ID: "local"}, {ID: "local"}} }, probes: func() []probe.Probe { return nil }, marker: "duplicate module descriptor"},
		{name: "nil probe", descr: func() []module.Descriptor { return []module.Descriptor{{ID: "local"}} }, probes: func() []probe.Probe { return []probe.Probe{nil} }, marker: "probe 0 is nil"},
		{name: "empty probe ID", descr: func() []module.Descriptor { return []module.Descriptor{{ID: "local"}} }, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{}} }, marker: "has empty ID"},
		{name: "unknown probe", descr: func() []module.Descriptor { return []module.Descriptor{{ID: "local"}} }, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{id: "other"}} }, marker: "has no module descriptor"},
		{name: "duplicate probe", descr: func() []module.Descriptor { return []module.Descriptor{{ID: "local"}} }, probes: func() []probe.Probe {
			return []probe.Probe{bindingTestProbe{id: "local"}, bindingTestProbe{id: "local"}}
		}, marker: "duplicate probe ID"},
		{name: "missing probe", descr: func() []module.Descriptor {
			return []module.Descriptor{{ID: "local"}, {ID: "remote", Exposure: module.ExposurePublic}}
		}, probes: func() []probe.Probe { return []probe.Probe{bindingTestProbe{id: "local"}} }, marker: "has no probe"},
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

func TestHasNetworkModulesUsesDescriptorExposure(t *testing.T) {
	local := moduleBinding{Descriptor: module.Descriptor{ID: "local", Exposure: module.ExposureLocal}, Probe: bindingTestProbe{id: "local"}}
	remote := moduleBinding{Descriptor: module.Descriptor{ID: "remote", Exposure: module.ExposurePublic}, Probe: bindingTestProbe{id: "remote"}}
	fallback := moduleBinding{Probe: bindingTestProbe{id: "custom"}}
	if hasNetworkModules(nil) || hasNetworkModules([]moduleBinding{local}) {
		t.Fatal("local or empty selection unexpectedly requires network")
	}
	if !hasNetworkModules([]moduleBinding{remote}) || hasNetworkModules([]moduleBinding{fallback}) {
		t.Fatal("network detection did not use descriptor metadata exclusively")
	}
}
