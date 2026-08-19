package config

import "testing"

func TestModuleDescriptorsAreCanonicalAndComplete(t *testing.T) {
	descriptors := ModuleDescriptors()
	if len(descriptors) == 0 || len(descriptors) != len(ModuleOrder) || len(ModuleIDs()) != len(descriptors) {
		t.Fatalf("descriptor/order sizes = %d/%d", len(descriptors), len(ModuleOrder))
	}
	if err := ValidateModuleDescriptors(); err != nil {
		t.Fatal(err)
	}
	for index, descriptor := range descriptors {
		if descriptor.ID == "" || descriptor.ID != ModuleOrder[index] {
			t.Fatalf("descriptor[%d] = %q, order = %q", index, descriptor.ID, ModuleOrder[index])
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
	if len(standard) == 0 || len(full) != len(descriptors) || contains(standard, "network") || !contains(full, "network") {
		t.Fatalf("profile module sets = standard:%v full:%v", standard, full)
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
