package probe

import (
	"strings"
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

func TestBuiltinsExposeStableTitleKeys(t *testing.T) {
	for _, item := range Builtins() {
		if item.Title() == "" || !strings.HasPrefix(item.Title(), "module.") {
			t.Fatalf("probe %q exposes non-key title %q", item.ID(), item.Title())
		}
	}
}
