package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
	"ecs/internal/module"
)

type definitionTestProbe struct{ id string }

func (probe definitionTestProbe) ID() string { return probe.id }

func (probe definitionTestProbe) Run(context.Context, Environment) model.Result {
	return model.Result{ID: probe.id}
}

func testCatalog() module.Catalog {
	catalog, err := CatalogFromDefinitions(BuiltinDefinitions())
	if err != nil {
		panic(err)
	}
	return catalog
}

func testDefinition(id string) Definition {
	return Definition{
		Descriptor: module.Descriptor{
			ID: id, Exposure: module.ExposureLocal, Concurrency: module.ConcurrencyProbe,
			Methodology: model.Methodology{
				Kind: "fixture", Label: "fixture.label", Engine: "fixture.engine",
				Profile: "fixture.profile", ComparisonScope: "fixture.scope",
			},
			TitleKey: "module." + id + ".title", DescriptionKey: "module." + id + ".desc",
			Estimate: time.Second, EstimateMode: module.EstimateModeFixed,
		},
		Probe: definitionTestProbe{id: id},
	}
}

func TestBuiltinDefinitionsAreCompleteAndCanonical(t *testing.T) {
	wantIDs := []string{
		"system", "network", "bgp", "cpu", "zstd", "npb", "memory", "crypto", "disk", "dns", "latency",
		"speed", "ports", "nat", "blacklist", "apps", "cnspeed", "ookla", "media", "route", "backtrace",
	}
	definitions := BuiltinDefinitions()
	if len(definitions) != len(wantIDs) {
		t.Fatalf("builtin definition count = %d, want %d", len(definitions), len(wantIDs))
	}
	seen := make(map[string]bool, len(definitions))
	for index, definition := range definitions {
		if definition.Descriptor.ID != wantIDs[index] {
			t.Fatalf("definition[%d] ID = %q, want %q", index, definition.Descriptor.ID, wantIDs[index])
		}
		if definition.Descriptor.ID == "" || seen[definition.Descriptor.ID] {
			t.Fatalf("definition[%d] has empty or duplicate ID %q", index, definition.Descriptor.ID)
		}
		seen[definition.Descriptor.ID] = true
		if definition.Probe == nil || strings.TrimSpace(definition.Probe.ID()) == "" {
			t.Fatalf("definition[%d] has invalid probe %#v", index, definition.Probe)
		}
		if definition.Probe.ID() != definition.Descriptor.ID {
			t.Fatalf("definition[%d] descriptor/probe IDs = %q/%q", index, definition.Descriptor.ID, definition.Probe.ID())
		}
	}

	catalog := testCatalog()
	if got := catalog.IDs(); !equalStrings(got, wantIDs) {
		t.Fatalf("builtin catalog IDs = %v, want %v", got, wantIDs)
	}
	for _, id := range wantIDs {
		if _, ok := catalog.Lookup(id); !ok {
			t.Fatalf("builtin catalog missing %q", id)
		}
	}
}

func TestBuiltinCatalogStandardIDsAreCanonical(t *testing.T) {
	wantIDs := []string{
		"system", "bgp", "cpu", "zstd", "npb", "memory", "crypto", "disk", "dns", "latency",
		"speed", "ports", "nat", "blacklist", "apps", "cnspeed", "media", "route", "backtrace",
	}
	if got := testCatalog().StandardIDs(); !equalStrings(got, wantIDs) {
		t.Fatalf("builtin catalog standard IDs = %v, want %v", got, wantIDs)
	}
}

func TestBuiltinDefinitionsReturnedStateIsDetached(t *testing.T) {
	first := BuiltinDefinitions()
	if len(first) == 0 {
		t.Fatal("BuiltinDefinitions returned no definitions")
	}
	first[0].Descriptor.ID = "changed"
	first[1].Descriptor.RequiredTools = append(first[1].Descriptor.RequiredTools, "changed")
	first[2].Descriptor.Methodology.Parameters = map[string]string{"changed": "yes"}
	first[3].Probe = definitionTestProbe{id: "changed"}

	second := BuiltinDefinitions()
	if second[0].Descriptor.ID != "system" || second[1].Descriptor.ID != "network" || second[3].Probe.ID() != "cpu" {
		t.Fatalf("mutating returned definitions changed canonical state: %+v", second[:4])
	}
	if len(second[1].Descriptor.RequiredTools) != 0 || second[2].Descriptor.Methodology.Parameters != nil {
		t.Fatalf("mutating nested returned metadata changed canonical state: %+v", second[:3])
	}
}

func TestBuiltinDefinitionsPreserveInterferenceRetryPolicy(t *testing.T) {
	wantRetry := map[string]bool{
		"cpu": true, "zstd": true, "npb": true, "memory": true, "crypto": true, "disk": true,
	}
	for _, definition := range BuiltinDefinitions() {
		if got, want := definition.Descriptor.RetryOnInterference, wantRetry[definition.Descriptor.ID]; got != want {
			t.Errorf("module %q RetryOnInterference=%v, want %v", definition.Descriptor.ID, got, want)
		}
	}
}

func TestDefinitionValidationRejectsInvalidPairs(t *testing.T) {
	cases := []struct {
		name   string
		input  []Definition
		marker string
	}{
		{name: "empty descriptor", input: []Definition{{Probe: definitionTestProbe{id: "local"}}}, marker: "empty ID"},
		{name: "duplicate descriptor", input: []Definition{testDefinition("local"), testDefinition("local")}, marker: "duplicate module descriptor"},
		{name: "nil probe", input: []Definition{{Descriptor: testDefinition("local").Descriptor}}, marker: "nil probe"},
		{name: "empty probe ID", input: []Definition{{Descriptor: testDefinition("local").Descriptor, Probe: definitionTestProbe{}}}, marker: "empty probe ID"},
		{name: "whitespace probe ID", input: []Definition{{Descriptor: testDefinition("local").Descriptor, Probe: definitionTestProbe{id: " local "}}}, marker: "does not match"},
		{name: "unknown probe", input: []Definition{{Descriptor: testDefinition("local").Descriptor, Probe: definitionTestProbe{id: "other"}}}, marker: "does not match"},
		{name: "duplicate probe", input: []Definition{
			{Descriptor: testDefinition("local").Descriptor, Probe: definitionTestProbe{id: "local"}},
			{Descriptor: testDefinition("remote").Descriptor, Probe: definitionTestProbe{id: "local"}},
		}, marker: "duplicate probe ID"},
		{name: "missing descriptor", input: []Definition{{Probe: definitionTestProbe{id: "local"}}}, marker: "empty ID"},
		{name: "missing probe", input: []Definition{{Descriptor: testDefinition("local").Descriptor}}, marker: "nil probe"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateDefinitions(test.input)
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("definition validation error = %v, want %q", err, test.marker)
			}
		})
	}
}

func TestDefinitionCopyDeepCopiesNestedDescriptorMetadata(t *testing.T) {
	input := testDefinition("fixture")
	input.Descriptor.RequiredTools = []string{"tool"}
	input.Descriptor.Methodology.Parameters = map[string]string{"key": "value"}
	copy := copyDefinitions([]Definition{input})
	copy[0].Descriptor.RequiredTools[0] = "changed"
	copy[0].Descriptor.Methodology.Parameters["key"] = "changed"
	copy[0].Descriptor.RequiredTools = append(copy[0].Descriptor.RequiredTools, "extra")

	if input.Descriptor.RequiredTools[0] != "tool" || input.Descriptor.Methodology.Parameters["key"] != "value" || len(input.Descriptor.RequiredTools) != 1 {
		t.Fatalf("definition copy shares nested metadata: input=%+v copy=%+v", input, copy[0])
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
