package config

import "testing"

func TestModuleDescriptorsAreCanonical(t *testing.T) {
	descriptors := ModuleDescriptors()
	if len(descriptors) == 0 || len(descriptors) != len(ModuleOrder) {
		t.Fatalf("descriptor/order sizes = %d/%d", len(descriptors), len(ModuleOrder))
	}
	for index, descriptor := range descriptors {
		if descriptor.ID == "" || descriptor.ID != ModuleOrder[index] {
			t.Fatalf("descriptor[%d] = %q, order = %q", index, descriptor.ID, ModuleOrder[index])
		}
		if _, ok := ModuleDescriptorFor(descriptor.ID); !ok {
			t.Fatalf("descriptor lookup failed for %q", descriptor.ID)
		}
	}
	if err := ValidateModuleDescriptors(); err != nil {
		t.Fatal(err)
	}
	standard := ModulesForProfile(ProfileStandard)
	full := ModulesForProfile(ProfileFull)
	if len(standard) == 0 || len(full) != len(descriptors) || contains(standard, "network") || !contains(full, "network") {
		t.Fatalf("profile module sets = standard:%v full:%v", standard, full)
	}
}
