package probe

import "testing"

func TestBuiltinDefinitionsHaveUniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	definitions := BuiltinDefinitions()
	for _, definition := range definitions {
		if definition.Probe == nil || definition.Probe.ID() == "" || seen[definition.Probe.ID()] || definition.Descriptor.ID != definition.Probe.ID() {
			t.Fatalf("invalid or duplicate builtin definition: %#v", definition)
		}
		seen[definition.Probe.ID()] = true
	}
}
