package config

import (
	"reflect"
	"slices"
	"testing"
)

func TestModuleDescriptorsAreCanonicalAndComplete(t *testing.T) {
	descriptors := ModuleDescriptors()
	order := ModuleIDs()
	if len(descriptors) == 0 || len(descriptors) != len(order) || len(ModuleIDs()) != len(descriptors) {
		t.Fatalf("descriptor/order sizes = %d/%d", len(descriptors), len(order))
	}
	if err := validateModuleDescriptors(); err != nil {
		t.Fatal(err)
	}
	for index, descriptor := range descriptors {
		if descriptor.ID == "" || descriptor.ID != order[index] {
			t.Fatalf("descriptor[%d] = %q, order = %q", index, descriptor.ID, order[index])
		}
		if _, ok := ModuleDescriptorFor(descriptor.ID); !ok {
			t.Fatalf("descriptor lookup failed for %q", descriptor.ID)
		}
	}
	if _, ok := ModuleDescriptorFor("missing"); ok || ModulesForProfile("missing") != nil {
		t.Fatal("unknown descriptor/profile should not resolve")
	}
	standard := ModulesForProfile(ProfileStandard)
	full := ModulesForProfile(ProfileFull)
	if len(standard) == 0 || len(full) != len(descriptors) || slices.Contains(standard, "network") || !slices.Contains(full, "network") {
		t.Fatalf("profile module sets = standard:%v full:%v", standard, full)
	}
}

func TestModuleIDsReturnsCopy(t *testing.T) {
	original := ModuleIDs()
	if len(original) == 0 {
		t.Fatal("module order must not be empty")
	}
	mutated := append([]string(nil), original...)
	mutated[0] = "mutated"
	got := ModuleIDs()
	if !reflect.DeepEqual(got, original) || reflect.DeepEqual(got, mutated) {
		t.Fatalf("ModuleIDs returned mutable canonical data: %v", got)
	}
}

func TestModuleDescriptorCopiesAreSafe(t *testing.T) {
	descriptors := ModuleDescriptors()
	index := -1
	for candidate, descriptor := range descriptors {
		if len(descriptor.RequiredTools) > 0 {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatal("expected a descriptor with required tools")
	}
	descriptors[index].RequiredTools[0] = "mutated"
	fresh, ok := ModuleDescriptorFor(descriptors[index].ID)
	if !ok || fresh.RequiredTools[0] == "mutated" {
		t.Fatal("descriptor metadata leaked through a returned slice")
	}
}

func TestModuleDescriptorsDeclareInterferenceRetryPolicy(t *testing.T) {
	wantRetry := map[string]bool{
		"cpu":    true,
		"zstd":   true,
		"npb":    true,
		"memory": true,
		"crypto": true,
		"disk":   true,
	}
	for _, descriptor := range ModuleDescriptors() {
		want := wantRetry[descriptor.ID]
		if descriptor.RetryOnInterference != want {
			t.Errorf("module %q RetryOnInterference=%v, want %v", descriptor.ID, descriptor.RetryOnInterference, want)
		}
	}
}

// TestModuleDescriptorsDetachReferenceFields covers every reference-typed field
// of a returned descriptor. RequiredTools was already copied; Methodology
// carries a Parameters map that a caller could otherwise use to write into the
// canonical registry.
func TestModuleDescriptorsDetachReferenceFields(t *testing.T) {
	first := ModuleDescriptors()[0]
	first.RequiredTools = append(first.RequiredTools, "injected")
	if first.Methodology.Parameters == nil {
		first.Methodology.Parameters = map[string]string{}
	}
	first.Methodology.Parameters["injected"] = "1"

	second := ModuleDescriptors()[0]
	if slices.Contains(second.RequiredTools, "injected") {
		t.Error("RequiredTools is shared with the registry")
	}
	if _, ok := second.Methodology.Parameters["injected"]; ok {
		t.Error("Methodology.Parameters is shared with the registry")
	}
	if single, ok := ModuleDescriptorFor(second.ID); ok {
		if slices.Contains(single.RequiredTools, "injected") {
			t.Error("ModuleDescriptorFor shares RequiredTools with the registry")
		}
		if _, ok := single.Methodology.Parameters["injected"]; ok {
			t.Error("ModuleDescriptorFor shares Methodology.Parameters with the registry")
		}
	}
}
