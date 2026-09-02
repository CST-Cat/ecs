package config

import (
	"reflect"
	"testing"
)

func TestModuleCatalogAdaptersUseExplicitOrderAndProfileSemantics(t *testing.T) {
	catalog := testModuleCatalog()
	descriptors := ModuleDescriptors(catalog)
	wantOrder := []string{"system", "network", "dns"}
	if len(descriptors) != len(wantOrder) || !reflect.DeepEqual(ModuleIDs(catalog), wantOrder) {
		t.Fatalf("descriptor/order = %v/%v, want %v", descriptors, ModuleIDs(catalog), wantOrder)
	}
	for index, descriptor := range descriptors {
		if descriptor.ID != wantOrder[index] {
			t.Fatalf("descriptor[%d] = %q, want %q", index, descriptor.ID, wantOrder[index])
		}
		if found, ok := ModuleDescriptorFor(catalog, descriptor.ID); !ok || found.ID != descriptor.ID {
			t.Fatalf("descriptor lookup failed for %q", descriptor.ID)
		}
	}
	if _, ok := ModuleDescriptorFor(catalog, "missing"); ok || ModulesForProfile(catalog, "missing") != nil {
		t.Fatal("unknown descriptor/profile should not resolve")
	}
	standard := ModulesForProfile(catalog, ProfileStandard)
	full := ModulesForProfile(catalog, ProfileFull)
	if !reflect.DeepEqual(standard, []string{"system", "dns"}) || !reflect.DeepEqual(full, wantOrder) {
		t.Fatalf("profile module sets = standard:%v full:%v", standard, full)
	}
}

func TestModuleCatalogAdaptersReturnDefensiveCopies(t *testing.T) {
	catalog := testModuleCatalog()
	ids := ModuleIDs(catalog)
	ids[0] = "mutated"
	if !reflect.DeepEqual(ModuleIDs(catalog), []string{"system", "network", "dns"}) {
		t.Fatal("ModuleIDs returned mutable catalog storage")
	}
	descriptors := ModuleDescriptors(catalog)
	descriptors[0].RequiredTools[0] = "mutated"
	fresh, ok := ModuleDescriptorFor(catalog, "system")
	if !ok || fresh.RequiredTools[0] != "sysinfo" {
		t.Fatal("descriptor metadata leaked through an adapter return value")
	}
}
