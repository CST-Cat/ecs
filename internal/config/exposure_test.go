package config

import "testing"

func TestExposureFiltersLocalAndPublicModules(t *testing.T) {
	modules := []string{"system", "dns", "network"}
	local := FilterModulesByExposure(modules, ExposureLocal)
	if len(local) != 1 || local[0] != "system" {
		t.Fatalf("local modules = %v", local)
	}
	public := FilterModulesByExposure(modules, ExposurePublic)
	if len(public) != 2 || public[0] != "system" || public[1] != "dns" {
		t.Fatalf("public modules = %v", public)
	}
	if !AllowsModule(ExposurePublic, "dns") || AllowsModule(ExposurePublic, "network") {
		t.Fatal("public exposure boundary is incorrect")
	}
}
