package probe

import (
	"testing"

	"ecs/internal/config"
)

func TestBuiltinsHaveUniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	builtins := Builtins()
	descriptors := config.ModuleDescriptors()
	if len(builtins) != len(descriptors) {
		t.Fatalf("builtin count = %d, descriptor count = %d", len(builtins), len(descriptors))
	}
	known := make(map[string]bool, len(descriptors))
	for _, descriptor := range descriptors {
		known[descriptor.ID] = true
		method := MethodologyFor(descriptor.ID)
		if method.Kind == "" || method.Label == "" || method.Engine == "" {
			t.Fatalf("methodology for %q = %+v", descriptor.ID, method)
		}
	}
	for _, item := range builtins {
		if item == nil || item.ID() == "" || seen[item.ID()] || !known[item.ID()] {
			t.Fatalf("invalid or duplicate builtin probe: %#v", item)
		}
		seen[item.ID()] = true
	}
	if method := MethodologyFor("missing"); method.Kind != "" || method.Label != "" || method.Engine != "" {
		t.Fatalf("unknown methodology = %+v", method)
	}
}
