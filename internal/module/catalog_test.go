package module

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func validDescriptor(id string) Descriptor {
	return Descriptor{
		ID:              id,
		ProfileStandard: true,
		Exposure:        ExposurePublic,
		NeedsEgressIP:   true,
		Concurrency:     ConcurrencyProbe,
		Methodology: model.Methodology{
			Kind:            "protocol-measurement",
			Label:           "methodology.protocol-measurement",
			Engine:          "fixture",
			Profile:         "probe.fixture.profile",
			ComparisonScope: "probe.fixture.comparison_scope",
			Parameters:      map[string]string{"workload": "fixture"},
		},
		RequiredTools:     []string{"fixture-tool"},
		TitleKey:          "module." + id + ".title",
		DescriptionKey:    "module." + id + ".desc",
		PrivacyNoticeKey:  "message.notice.fixture",
		WizardGroup:       "fixture",
		WizardQuestionKey: "wizard.askFixture",
		Estimate:          time.Second,
		EstimateMode:      EstimateModeFixed,
	}
}

func TestNewCatalogRejectsInvalidDescriptors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Descriptor)
		marker string
	}{
		{name: "empty ID", mutate: func(descriptor *Descriptor) { descriptor.ID = "" }, marker: "empty ID"},
		{name: "whitespace ID", mutate: func(descriptor *Descriptor) { descriptor.ID = " \t" }, marker: "empty ID"},
		{name: "surrounding ID whitespace", mutate: func(descriptor *Descriptor) { descriptor.ID = " fixture " }, marker: "surrounding whitespace"},
		{name: "invalid exposure", mutate: func(descriptor *Descriptor) { descriptor.Exposure = Exposure(-1) }, marker: "unknown exposure"},
		{name: "invalid concurrency", mutate: func(descriptor *Descriptor) { descriptor.Concurrency = Concurrency("future") }, marker: "unknown concurrency"},
		{name: "invalid estimate mode", mutate: func(descriptor *Descriptor) { descriptor.EstimateMode = EstimateMode("future") }, marker: "unknown estimate mode"},
		{name: "negative estimate", mutate: func(descriptor *Descriptor) { descriptor.Estimate = -time.Nanosecond }, marker: "negative estimate"},
		{name: "missing methodology kind", mutate: func(descriptor *Descriptor) { descriptor.Methodology.Kind = "" }, marker: "incomplete methodology"},
		{name: "missing methodology label", mutate: func(descriptor *Descriptor) { descriptor.Methodology.Label = " \t" }, marker: "incomplete methodology"},
		{name: "missing methodology engine", mutate: func(descriptor *Descriptor) { descriptor.Methodology.Engine = "" }, marker: "incomplete methodology"},
		{name: "missing methodology profile", mutate: func(descriptor *Descriptor) { descriptor.Methodology.Profile = "" }, marker: "incomplete methodology"},
		{name: "missing comparison scope", mutate: func(descriptor *Descriptor) { descriptor.Methodology.ComparisonScope = " \t" }, marker: "incomplete methodology"},
		{name: "missing title key", mutate: func(descriptor *Descriptor) { descriptor.TitleKey = "" }, marker: "incomplete display metadata"},
		{name: "missing description key", mutate: func(descriptor *Descriptor) { descriptor.DescriptionKey = " \t" }, marker: "incomplete display metadata"},
		{name: "wizard group without question", mutate: func(descriptor *Descriptor) { descriptor.WizardQuestionKey = "" }, marker: "unpaired wizard metadata"},
		{name: "wizard question without group", mutate: func(descriptor *Descriptor) { descriptor.WizardGroup = "" }, marker: "unpaired wizard metadata"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			descriptor := validDescriptor("fixture")
			test.mutate(&descriptor)
			if _, err := NewCatalog([]Descriptor{descriptor}); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("NewCatalog error = %v, want marker %q", err, test.marker)
			}
		})
	}
}

func TestNewCatalogRejectsDuplicateAndWhitespaceVariantIDs(t *testing.T) {
	duplicate := []Descriptor{validDescriptor("run"), validDescriptor("run")}
	if _, err := NewCatalog(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate module descriptor") {
		t.Fatalf("duplicate ID error = %v", err)
	}
	whitespaceVariant := []Descriptor{validDescriptor("run"), validDescriptor(" run ")}
	if _, err := NewCatalog(whitespaceVariant); err == nil || !strings.Contains(err.Error(), "surrounding whitespace") {
		t.Fatalf("whitespace variant error = %v, want explicit identity rejection", err)
	}
}

func TestCatalogPreservesOrderAndUsesExactLookup(t *testing.T) {
	first := validDescriptor("first")
	first.ProfileStandard = true
	second := validDescriptor("second")
	second.ProfileStandard = false
	third := validDescriptor("third")
	third.ProfileStandard = true
	catalog, err := NewCatalog([]Descriptor{first, second, third})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := catalog.IDs(), []string{"first", "second", "third"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	if got, want := catalog.StandardIDs(), []string{"first", "third"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("standard membership = %v, want %v", got, want)
	}
	if descriptor, ok := catalog.Lookup("second"); !ok || descriptor.ID != "second" {
		t.Fatalf("exact lookup = %+v/%v", descriptor, ok)
	}
	if descriptor, ok := catalog.Lookup(" second "); ok || descriptor.ID != "" {
		t.Fatalf("whitespace lookup = %+v/%v, want unknown zero value", descriptor, ok)
	}
	if metadata, ok := catalog.ExposureFor("second"); !ok || metadata.Level != second.Exposure || !metadata.NeedsEgressIP {
		t.Fatalf("exposure metadata = %+v/%v", metadata, ok)
	}
	if metadata, ok := catalog.ExposureFor("missing"); ok || metadata != (ExposureMetadata{}) {
		t.Fatalf("unknown exposure metadata = %+v/%v", metadata, ok)
	}
}

func TestCatalogDefensivelyCopiesInputAndAllOutputs(t *testing.T) {
	input := validDescriptor("fixture")
	inputTools := input.RequiredTools
	inputParameters := input.Methodology.Parameters
	catalog, err := NewCatalog([]Descriptor{input})
	if err != nil {
		t.Fatal(err)
	}

	inputTools[0] = "changed-input"
	inputParameters["workload"] = "changed-input"
	input.RequiredTools = append(input.RequiredTools, "new-input")
	input.Methodology.Parameters["another"] = "changed-input"

	returned := catalog.Descriptors()
	returned[0].ID = "changed-output"
	returned[0].RequiredTools[0] = "changed-output"
	returned[0].Methodology.Parameters["workload"] = "changed-output"
	returned[0].Methodology.Parameters["another"] = "changed-output"

	lookup, ok := catalog.Lookup("fixture")
	if !ok {
		t.Fatal("fixture lookup failed")
	}
	lookup.RequiredTools[0] = "changed-lookup"
	lookup.Methodology.Parameters["workload"] = "changed-lookup"

	ids := catalog.IDs()
	ids[0] = "changed-ids"
	standard := catalog.StandardIDs()
	standard[0] = "changed-profile"

	fresh, ok := catalog.Lookup("fixture")
	if !ok {
		t.Fatal("fresh fixture lookup failed")
	}
	want := validDescriptor("fixture")
	if !reflect.DeepEqual(fresh, want) {
		t.Fatalf("catalog changed through input/output references:\n got:  %+v\n want: %+v", fresh, want)
	}
	if got := catalog.IDs(); !reflect.DeepEqual(got, []string{"fixture"}) {
		t.Fatalf("IDs changed through returned slice: %v", got)
	}
	if got := catalog.StandardIDs(); !reflect.DeepEqual(got, []string{"fixture"}) {
		t.Fatalf("standard membership changed through returned slice: %v", got)
	}
}

func TestCatalogAcceptsEveryDeclaredEnumValue(t *testing.T) {
	for _, exposure := range []Exposure{ExposureLocal, ExposurePublic, ExposureThirdParty, ExposureConsent} {
		for _, concurrency := range []Concurrency{ConcurrencyExclusive, ConcurrencyProbe} {
			for _, estimateMode := range []EstimateMode{
				EstimateModeFixed, EstimateModeTwoContext, EstimateModeCPU, EstimateModeMemory,
				EstimateModeDisk, EstimateModeDNS, EstimateModeLatency, EstimateModeSpeed, EstimateModeRoute,
			} {
				descriptor := validDescriptor("fixture")
				descriptor.Exposure = exposure
				descriptor.Concurrency = concurrency
				descriptor.EstimateMode = estimateMode
				if _, err := NewCatalog([]Descriptor{descriptor}); err != nil {
					t.Fatalf("enum values %v/%v/%v rejected: %v", exposure, concurrency, estimateMode, err)
				}
			}
		}
	}
}
