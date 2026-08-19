package runner

import (
	"fmt"

	"ecs/internal/config"
	"ecs/internal/probe"
)

// moduleBinding is the runner's execution view of one module. Config owns the
// descriptor and probe owns the implementation; runner joins the two once at
// the boundary before selecting work.
type moduleBinding struct {
	Descriptor config.ModuleDescriptor
	Probe      probe.Probe
}

// bindBuiltinModules joins the canonical config descriptors with the typed
// built-in probes. The returned slice always follows descriptor order.
func bindBuiltinModules() []moduleBinding {
	bindings, err := bindModuleProbes(config.ModuleDescriptors(), probe.Builtins())
	if err != nil {
		panic(fmt.Sprintf("bind built-in modules: %v", err))
	}
	return bindings
}

// bindModuleProbes joins descriptors and probes for the runner. Keeping the
// inputs explicit makes the binding contract testable with small custom Probe
// implementations without introducing a runtime registry or test hook into
// either config or probe.
func bindModuleProbes(descriptors []config.ModuleDescriptor, probes []probe.Probe) ([]moduleBinding, error) {
	descriptorIDs := make(map[string]struct{}, len(descriptors))
	for index, descriptor := range descriptors {
		if descriptor.ID == "" {
			return nil, fmt.Errorf("module descriptor %d has empty ID", index)
		}
		if _, exists := descriptorIDs[descriptor.ID]; exists {
			return nil, fmt.Errorf("duplicate module descriptor %q", descriptor.ID)
		}
		descriptorIDs[descriptor.ID] = struct{}{}
	}

	probesByID := make(map[string]probe.Probe, len(probes))
	for index, item := range probes {
		if item == nil {
			return nil, fmt.Errorf("probe %d is nil", index)
		}
		id := item.ID()
		if id == "" {
			return nil, fmt.Errorf("probe %d has empty ID", index)
		}
		if _, known := descriptorIDs[id]; !known {
			return nil, fmt.Errorf("probe %q has no module descriptor", id)
		}
		if _, duplicate := probesByID[id]; duplicate {
			return nil, fmt.Errorf("duplicate probe ID %q", id)
		}
		probesByID[id] = item
	}

	bindings := make([]moduleBinding, 0, len(descriptors))
	for _, descriptor := range descriptors {
		item, ok := probesByID[descriptor.ID]
		if !ok {
			return nil, fmt.Errorf("module %q has no probe", descriptor.ID)
		}
		expectedNetwork := descriptor.Exposure > config.ExposureLocal
		if item.NeedsNetwork() != expectedNetwork {
			return nil, fmt.Errorf("probe %q NeedsNetwork=%v disagrees with descriptor exposure network=%v", descriptor.ID, item.NeedsNetwork(), expectedNetwork)
		}
		bindings = append(bindings, moduleBinding{Descriptor: descriptor, Probe: item})
	}
	return bindings, nil
}

// selectBindings filters an already validated canonical binding list. The
// requested IDs are a set; iteration over bindings preserves canonical order.
func selectBindings(bindings []moduleBinding, ids []string) []moduleBinding {
	requested := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}
	selected := make([]moduleBinding, 0, len(ids))
	for _, binding := range bindings {
		if _, ok := requested[binding.Descriptor.ID]; ok {
			selected = append(selected, binding)
		}
	}
	return selected
}

// bindingForProbe is the explicit fallback for tests that invoke runOne with
// a custom Probe. Production execution uses validated moduleBinding values.
func bindingForProbe(item probe.Probe) moduleBinding {
	if item == nil {
		return moduleBinding{}
	}
	if descriptor, ok := config.ModuleDescriptorFor(item.ID()); ok {
		return moduleBinding{Descriptor: descriptor, Probe: item}
	}
	return moduleBinding{Probe: item}
}
