package tool

import (
	"reflect"
	"regexp"
	"testing"
)

var canonicalToolID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func TestBuiltinDefinitionsHaveCanonicalOrderAndFacts(t *testing.T) {
	want := []Definition{
		{ID: "sysbench"},
		{ID: "zstd"},
		{ID: "npb-ep"},
		{ID: "npb-ft"},
		{ID: "openssl"},
		{ID: "stream"},
		{ID: "fio"},
		{ID: "iperf3"},
		{ID: "nexttrace-tiny"},
		{ID: "ping"},
		{ID: "speedtest", ExternalService: "ookla"},
	}
	got := BuiltinDefinitions()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("builtin definitions = %+v, want %+v", got, want)
	}

	seen := make(map[string]struct{}, len(got))
	for index, definition := range got {
		if !canonicalToolID.MatchString(definition.ID) {
			t.Errorf("builtin definition[%d] has noncanonical ID %q", index, definition.ID)
		}
		if _, exists := seen[definition.ID]; exists {
			t.Errorf("builtin definition ID %q is duplicated", definition.ID)
		}
		seen[definition.ID] = struct{}{}
		if definition.ExternalService != "" && definition.ExternalService != "ookla" {
			t.Errorf("builtin tool %q has invalid external service %q", definition.ID, definition.ExternalService)
		}
	}
}

func TestLookupBuiltinPreservesToolFacts(t *testing.T) {
	definition, ok := LookupBuiltin("speedtest")
	if !ok || definition != (Definition{ID: "speedtest", ExternalService: "ookla"}) {
		t.Fatalf("speedtest lookup = %+v/%t", definition, ok)
	}
	if definition, ok := LookupBuiltin("missing"); ok || definition != (Definition{}) {
		t.Fatalf("unknown lookup = %+v/%t, want zero/false", definition, ok)
	}
}
