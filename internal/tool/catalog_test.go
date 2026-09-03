package tool

import (
	"reflect"
	"strings"
	"testing"
)

func validDefinition(id string) Definition {
	return Definition{
		ID:         id,
		PurposeKey: "doctor.purpose.fixture",
		Verification: VerificationPolicy{
			Kind:      VerificationCommand,
			Arguments: []string{"--version"},
		},
		Doctor:  DoctorPolicy{Standard: true, Required: true, Order: 0},
		Staging: StagingPolicy{Category: StagingArchive},
	}
}

func TestBuiltinCatalogHasCanonicalOrderAndFacts(t *testing.T) {
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
		kind     VerificationKind
		args     []string
		version  string
		label    string
		variant  NPBVariant
		required bool
		order    int
		staging  StagingCategory
		source   StagingSource
		purpose  string
	}{
		"sysbench":       {VerificationCommand, []string{"--version"}, "", "", NPBVariantNone, true, 0, StagingArchive, StagingSourceNone, "doctor.purpose.sysbench"},
		"zstd":           {VerificationPinnedZstd, []string{"--version"}, "1.5.7", "zstd 1.5.7", NPBVariantNone, true, 1, StagingZstdCorpus, StagingSourceNone, "doctor.purpose.zstd"},
		"npb-ep":         {VerificationNPB, nil, "3.4.4", "NPB 3.4.4 EP (Class A verified at run)", NPBVariantEP, true, 2, StagingArchive, StagingSourceNone, "doctor.purpose.npbEP"},
		"npb-ft":         {VerificationNPB, nil, "3.4.4", "NPB 3.4.4 FT (Class A verified at run)", NPBVariantFT, true, 3, StagingArchive, StagingSourceNone, "doctor.purpose.npbFT"},
		"openssl":        {VerificationPinnedOpenSSL, []string{"version"}, "3.5.7", "OpenSSL 3.5.7", NPBVariantNone, true, 4, StagingArchive, StagingSourceNone, "doctor.purpose.openssl"},
		"stream":         {VerificationOfficialStream, nil, "", "official STREAM", NPBVariantNone, true, 7, StagingArchive, StagingSourceNone, "doctor.purpose.stream"},
		"fio":            {VerificationCommand, []string{"--version"}, "", "", NPBVariantNone, true, 5, StagingArchive, StagingSourceNone, "doctor.purpose.fio"},
		"iperf3":         {VerificationCommand, []string{"--version"}, "", "", NPBVariantNone, true, 6, StagingArchive, StagingSourceNone, "doctor.purpose.iperf3"},
		"nexttrace-tiny": {VerificationCommand, []string{"--version"}, "", "", NPBVariantNone, false, 8, StagingNextTrace, StagingSourceNextTraceArchitecture, "doctor.purpose.nexttrace"},
		"ping":           {VerificationCommand, []string{"-V"}, "", "", NPBVariantNone, false, 9, StagingArchive, StagingSourceNone, "doctor.purpose.ping"},
		"speedtest":      {VerificationCommand, []string{"--version"}, "", "", NPBVariantNone, false, 10, StagingOokla, StagingSourceOoklaSignedPackage, "doctor.purpose.speedtest"},
	}
	for _, definition := range catalog.Definitions() {
		facts, ok := want[definition.ID]
		if !ok {
			t.Fatalf("unexpected builtin tool %q", definition.ID)
		}
		if definition.PurposeKey != facts.purpose {
			t.Fatalf("purpose key for %q = %q, want %q", definition.ID, definition.PurposeKey, facts.purpose)
		}
		if !definition.Doctor.Standard || definition.Doctor.Required != facts.required || definition.Doctor.Order != facts.order {
			t.Fatalf("doctor facts for %q = %+v, want standard/required/order true/%t/%d", definition.ID, definition.Doctor, facts.required, facts.order)
		}
		verification := definition.Verification
		if verification.Kind != facts.kind || !reflect.DeepEqual(verification.Arguments, facts.args) || verification.ExpectedVersion != facts.version || verification.SuccessLabel != facts.label || verification.NPBVariant != facts.variant {
			t.Fatalf("verification facts for %q = %+v, want kind=%q args=%v version=%q label=%q variant=%q", definition.ID, verification, facts.kind, facts.args, facts.version, facts.label, facts.variant)
		}
		if definition.Staging.Category != facts.staging || definition.Staging.Source != facts.source {
			t.Fatalf("staging facts for %q = %+v, want %q/%q", definition.ID, definition.Staging, facts.staging, facts.source)
		}
	}
	if got := catalog.DoctorDefinitions(); len(got) != len(wantIDs) || got[5].ID != "fio" || got[6].ID != "iperf3" || got[7].ID != "stream" {
		t.Fatalf("legacy doctor order = %v, want fio/iperf3/stream at indexes 5/6/7", definitionIDs(got))
	}
}

func TestBuiltinDefinitionsReturnFreshCallerOwnedValues(t *testing.T) {
	first := BuiltinDefinitions()
	if len(first) < 2 || len(first[1].Verification.Arguments) == 0 {
		t.Fatal("builtin definitions missing nested verification arguments")
	}
	first[1].ID = "changed"
	first[1].Verification.Arguments[0] = "changed"

	second := BuiltinDefinitions()
	if len(second) < 2 || second[1].ID != "zstd" || !reflect.DeepEqual(second[1].Verification.Arguments, []string{"--version"}) {
		t.Fatalf("fresh builtin definitions changed through prior result: %+v", second[1])
	}
}

func definitionIDs(definitions []Definition) []string {
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, definition.ID)
	}
	return ids
}

func TestNewCatalogRejectsInvalidDefinitions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{"empty ID", func(definition *Definition) { definition.ID = " " }, "empty ID"},
		{"noncanonical ID", func(definition *Definition) { definition.ID = "Fixture_Tool" }, "noncanonical"},
		{"missing purpose", func(definition *Definition) { definition.PurposeKey = " \t" }, "purpose key"},
		{"invalid verification kind", func(definition *Definition) { definition.Verification.Kind = "future" }, "verification kind"},
		{"missing command args", func(definition *Definition) { definition.Verification.Arguments = nil }, "no arguments"},
		{"blank command arg", func(definition *Definition) { definition.Verification.Arguments = []string{" "} }, "blank verification argument"},
		{"command has pin facts", func(definition *Definition) { definition.Verification.ExpectedVersion = "1" }, "incomplete policy"},
		{"pinned missing version", func(definition *Definition) {
			definition.Verification.Kind = VerificationPinnedZstd
			definition.Verification.ExpectedVersion = ""
			definition.Verification.SuccessLabel = "zstd"
		}, "pinned verification"},
		{"NPB missing variant", func(definition *Definition) {
			definition.Verification.Kind = VerificationNPB
			definition.Verification.Arguments = nil
			definition.Verification.SuccessLabel = "NPB"
		}, "NPB verification"},
		{"stream missing label", func(definition *Definition) {
			definition.Verification.Kind = VerificationOfficialStream
			definition.Verification.Arguments = nil
		}, "STREAM verification"},
		{"required outside standard", func(definition *Definition) {
			definition.Doctor.Standard = false
			definition.Doctor.Required = true
			definition.Doctor.Order = -1
		}, "not in standard doctor"},
		{"negative doctor order", func(definition *Definition) { definition.Doctor.Order = -1 }, "negative doctor order"},
		{"nonstandard doctor order", func(definition *Definition) {
			definition.Doctor.Standard = false
			definition.Doctor.Required = false
			definition.Doctor.Order = 0
		}, "outside standard doctor"},
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
	next := validDefinition("run-next")
	next.Doctor.Order = 1
	if _, err := NewCatalog([]Definition{validDefinition("run"), next}); err != nil {
		t.Fatal(err)
	}
	second := validDefinition("next")
	if _, err := NewCatalog([]Definition{validDefinition("first"), second}); err == nil {
		t.Fatal("duplicate doctor order unexpectedly accepted")
	}
}

func TestCatalogPreservesOrderAndDefensiveCopies(t *testing.T) {
	first := validDefinition("first")
	second := validDefinition("second")
	second.Doctor.Order = 1
	second.Doctor.Required = false
	input := []Definition{first, second}
	catalog, err := NewCatalog(input)
	if err != nil {
		t.Fatal(err)
	}

	input[0].ID = "changed-input"
	input[0].Verification.Arguments[0] = "changed-input"
	definitions := catalog.Definitions()
	definitions[0].ID = "changed-output"
	definitions[0].Verification.Arguments[0] = "changed-output"
	ordered := catalog.DoctorDefinitions()
	ordered[0].Verification.Arguments[0] = "changed-doctor"
	lookup, ok := catalog.Lookup("first")
	if !ok {
		t.Fatal("first lookup failed")
	}
	lookup.Verification.Arguments[0] = "changed-lookup"
	ids := catalog.IDs()
	ids[0] = "changed-ids"

	fresh, ok := catalog.Lookup("first")
	if !ok || fresh.ID != "first" || !reflect.DeepEqual(fresh.Verification.Arguments, []string{"--version"}) {
		t.Fatalf("catalog changed through input/output references: %+v", fresh)
	}
	if got, want := catalog.IDs(), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog IDs = %v, want %v", got, want)
	}
	unknown, ok := catalog.Lookup("missing")
	if ok || unknown.ID != "" || unknown.Verification.Kind != "" {
		t.Fatalf("unknown lookup = %+v/%t, want zero/false", unknown, ok)
	}
}

func TestBuiltinCatalogReturnsFreshCallerOwnedValues(t *testing.T) {
	first, err := BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	returned := first.Definitions()
	returned[1].Verification.Arguments[0] = "changed"
	returned[1].ID = "changed"
	second, err := BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	fresh, ok := second.Lookup("zstd")
	if !ok || fresh.ID != "zstd" || !reflect.DeepEqual(fresh.Verification.Arguments, []string{"--version"}) {
		t.Fatalf("fresh builtin catalog changed through prior result: %+v", fresh)
	}
}
