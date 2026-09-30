//go:build linux

package probe

import "testing"

func TestLinuxRouteDefinitionKeepsNextTraceMethodology(t *testing.T) {
	for _, definition := range BuiltinDefinitions() {
		if definition.Descriptor.ID == "route" {
			if definition.Descriptor.Methodology.Engine != "NextTrace Tiny" {
				t.Fatalf("Linux route methodology engine = %q, want NextTrace Tiny", definition.Descriptor.Methodology.Engine)
			}
			return
		}
	}
	t.Fatal("route definition missing")
}
