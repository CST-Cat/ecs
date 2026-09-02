package config

import (
	"reflect"
	"testing"
)

func TestModuleCatalogUsesExplicitOrderAndProfileSemantics(t *testing.T) {
	catalog := testModuleCatalog()
	descriptors := catalog.Descriptors()
	wantOrder := []string{"system", "network", "dns"}
	if len(descriptors) != len(wantOrder) || !reflect.DeepEqual(catalog.IDs(), wantOrder) {
		t.Fatalf("descriptor/order = %v/%v, want %v", descriptors, catalog.IDs(), wantOrder)
	}
	for index, descriptor := range descriptors {
		if descriptor.ID != wantOrder[index] {
			t.Fatalf("descriptor[%d] = %q, want %q", index, descriptor.ID, wantOrder[index])
		}
		if found, ok := catalog.Lookup(descriptor.ID); !ok || found.ID != descriptor.ID {
			t.Fatalf("descriptor lookup failed for %q", descriptor.ID)
		}
	}
	if _, ok := catalog.Lookup("missing"); ok || ModulesForProfile(catalog, "missing") != nil {
		t.Fatal("unknown descriptor/profile should not resolve")
	}
	standard := ModulesForProfile(catalog, ProfileStandard)
	full := ModulesForProfile(catalog, ProfileFull)
	if !reflect.DeepEqual(standard, []string{"system", "dns"}) || !reflect.DeepEqual(full, wantOrder) {
		t.Fatalf("profile module sets = standard:%v full:%v", standard, full)
	}
}

func TestModuleCatalogReturnsDefensiveCopies(t *testing.T) {
	catalog := testModuleCatalog()
	ids := catalog.IDs()
	ids[0] = "mutated"
	if !reflect.DeepEqual(catalog.IDs(), []string{"system", "network", "dns"}) {
		t.Fatal("catalog IDs returned mutable catalog storage")
	}
	descriptors := catalog.Descriptors()
	descriptors[0].RequiredTools[0] = "mutated"
	fresh, ok := catalog.Lookup("system")
	if !ok || fresh.RequiredTools[0] != "sysinfo" {
		t.Fatal("descriptor metadata leaked through an adapter return value")
	}
}
