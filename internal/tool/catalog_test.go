package tool

import (
	"reflect"
	"strings"
	"testing"
)

func validDefinition(id string) Definition {
	return Definition{ID: id, Staging: StagingPolicy{Category: StagingArchive}}
}

func TestBuiltinCatalogHasCanonicalOrderAndStagingFacts(t *testing.T) {
	catalog, err := BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"sysbench", "zstd", "npb-ep", "npb-ft", "openssl", "stream", "fio", "iperf3",
		"nexttrace-tiny", "ping", "speedtest",
	}
	if got := catalog.IDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("builtin IDs = %v, want %v", got, wantIDs)
	}
	if !catalog.Valid() || len(catalog.Definitions()) != len(wantIDs) {
		t.Fatalf("builtin catalog validity/size = %t/%d", catalog.Valid(), len(catalog.Definitions()))
	}
	want := map[string]struct {
		staging StagingCategory
		source  StagingSource
	}{
		"sysbench":       {StagingArchive, StagingSourceNone},
		"zstd":           {StagingZstdCorpus, StagingSourceNone},
		"npb-ep":         {StagingArchive, StagingSourceNone},
		"npb-ft":         {StagingArchive, StagingSourceNone},
		"openssl":        {StagingArchive, StagingSourceNone},
		"stream":         {StagingArchive, StagingSourceNone},
		"fio":            {StagingArchive, StagingSourceNone},
		"iperf3":         {StagingArchive, StagingSourceNone},
		"nexttrace-tiny": {StagingNextTrace, StagingSourceNextTraceArchitecture},
		"ping":           {StagingArchive, StagingSourceNone},
		"speedtest":      {StagingOokla, StagingSourceOoklaSignedPackage},
	}
	for _, definition := range catalog.Definitions() {
		facts, ok := want[definition.ID]
		if !ok {
			t.Fatalf("unexpected builtin tool %q", definition.ID)
		}
		if definition.Staging.Category != facts.staging || definition.Staging.Source != facts.source {
			t.Fatalf("staging facts for %q = %+v, want %q/%q", definition.ID, definition.Staging, facts.staging, facts.source)
		}
	}
}

func TestBuiltinDefinitionsReturnFreshCallerOwnedValues(t *testing.T) {
	first := BuiltinDefinitions()
	if len(first) < 2 {
		t.Fatal("builtin definitions are incomplete")
	}
	first[1].ID = "changed"

	second := BuiltinDefinitions()
	if len(second) < 2 || second[1].ID != "zstd" {
		t.Fatalf("fresh builtin definitions changed through prior result: %+v", second[1])
	}
}

func TestNewCatalogRejectsInvalidDefinitions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{"empty ID", func(definition *Definition) { definition.ID = " " }, "empty ID"},
		{"noncanonical ID", func(definition *Definition) { definition.ID = "Fixture_Tool" }, "noncanonical"},
		{"invalid staging category", func(definition *Definition) { definition.Staging.Category = "future" }, "invalid staging"},
		{"mismatched staging source", func(definition *Definition) { definition.Staging.Source = StagingSourceOoklaSignedPackage }, "staging source"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition := validDefinition("fixture")
			test.mutate(&definition)
			if _, err := NewCatalog([]Definition{definition}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewCatalog error = %v, want %q", err, test.want)
			}
		})
	}

	if _, err := NewCatalog([]Definition{validDefinition("run"), validDefinition("run")}); err == nil || !strings.Contains(err.Error(), "duplicate tool") {
		t.Fatalf("duplicate ID error = %v", err)
	}
}

func TestCatalogPreservesOrderAndDefensiveCopies(t *testing.T) {
	first := validDefinition("first")
	second := validDefinition("second")
	input := []Definition{first, second}
	catalog, err := NewCatalog(input)
	if err != nil {
		t.Fatal(err)
	}

	input[0].ID = "changed-input"
	definitions := catalog.Definitions()
	definitions[0].ID = "changed-output"
	lookup, ok := catalog.Lookup("first")
	if !ok {
		t.Fatal("first lookup failed")
	}
	lookup.ID = "changed-lookup"
	ids := catalog.IDs()
	ids[0] = "changed-ids"

	fresh, ok := catalog.Lookup("first")
	if !ok || fresh.ID != "first" {
		t.Fatalf("catalog changed through input/output references: %+v", fresh)
	}
	if got, want := catalog.IDs(), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog IDs = %v, want %v", got, want)
	}
	unknown, ok := catalog.Lookup("missing")
	if ok || unknown.ID != "" {
		t.Fatalf("unknown lookup = %+v/%t, want zero/false", unknown, ok)
	}
}

func TestBuiltinCatalogReturnsFreshCallerOwnedValues(t *testing.T) {
	first, err := BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	returned := first.Definitions()
	returned[1].ID = "changed"

	second, err := BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	fresh, ok := second.Lookup("zstd")
	if !ok || fresh.ID != "zstd" {
		t.Fatalf("fresh builtin catalog changed through prior result: %+v", fresh)
	}
}
