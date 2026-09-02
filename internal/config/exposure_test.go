package config

import (
	"reflect"
	"strings"
	"testing"

	"ecs/internal/module"
)

func TestExposureParsingAndNames(t *testing.T) {
	useEnglish(t)
	for index, name := range ExposureNames() {
		level, err := ParseExposure(name)
		if err != nil || level.String() != name || int(level) != index {
			t.Fatalf("exposure %q = %v, %v", name, level, err)
		}
	}
	_, err := ParseExposure("elsewhere")
	if err == nil || !strings.Contains(err.Error(), "unknown exposure level") {
		t.Fatalf("unknown exposure error = %v", err)
	}
}

func TestExposureFilteringAndEgressSemantics(t *testing.T) {
	modules := []string{"system", "dns", "network"}
	if got := FilterModulesByExposure(testModuleCatalog(), modules, module.ExposureLocal); !reflect.DeepEqual(got, []string{"system"}) {
		t.Fatalf("local modules = %v", got)
	}
	if got := FilterModulesByExposure(testModuleCatalog(), modules, module.ExposurePublic); !reflect.DeepEqual(got, []string{"system", "dns"}) {
		t.Fatalf("public modules = %v", got)
	}
	if !AllowsModule(testModuleCatalog(), module.ExposurePublic, "dns") || AllowsModule(testModuleCatalog(), module.ExposurePublic, "network") ||
		exposureFor(testModuleCatalog(), "network").Level != module.ExposureThirdParty || !exposureFor(testModuleCatalog(), "network").NeedsEgressIP ||
		exposureFor(testModuleCatalog(), "unknown").Level != module.ExposureConsent {
		t.Fatal("exposure boundary or unknown-module safety is incorrect")
	}
	if !RequiresEgressIP(testModuleCatalog(), []string{"network"}) || RequiresEgressIP(testModuleCatalog(), []string{"system"}) {
		t.Fatal("egress dependency classification is incorrect")
	}
	if EgressNeedsIPIntel(testModuleCatalog(), []string{"network"}, module.ExposurePublic) || !EgressNeedsIPIntel(testModuleCatalog(), []string{"network"}, module.ExposureThirdParty) || EgressNeedsIPIntel(testModuleCatalog(), []string{"system"}, module.ExposureThirdParty) {
		t.Fatal("IP intelligence exposure boundary is incorrect")
	}
	if !validRuntimeForExposure().OfflineOnly() {
		t.Fatal("local runtime should be offline-only")
	}
	defaultRuntime, _ := Defaults(testModuleCatalog(), ProfileStandard)
	if defaultRuntime.OfflineOnly() {
		t.Fatal("local runtime should be offline-only")
	}
}

func validRuntimeForExposure() Runtime {
	runtime, _ := Defaults(testModuleCatalog(), ProfileStandard)
	runtime.Exposure = module.ExposureLocal
	return runtime
}

func TestCheckModuleExposure(t *testing.T) {
	useEnglish(t)
	if err := CheckModuleExposure(testModuleCatalog(), []string{"system"}, module.ExposureLocal); err != nil {
		t.Fatal(err)
	}
	err := CheckModuleExposure(testModuleCatalog(), []string{"network"}, module.ExposureLocal)
	if err == nil || !strings.Contains(err.Error(), "above the current --exposure local") {
		t.Fatalf("blocked module error = %v", err)
	}
}

func TestInvalidRuntimeExposureFailsClosed(t *testing.T) {
	useEnglish(t)
	for _, invalid := range []module.Exposure{-1, 4, 99} {
		runtime := validRuntimeForExposure()
		runtime.Exposure = invalid
		if err := Validate(testModuleCatalog(), runtime); err == nil || !strings.Contains(err.Error(), "unknown exposure level") {
			t.Fatalf("Validate(%d) = %v, want an invalid exposure error", invalid, err)
		}
		if AllowsModule(testModuleCatalog(), invalid, "system") {
			t.Fatalf("invalid exposure %d allowed a module", invalid)
		}
		if got := FilterModulesByExposure(testModuleCatalog(), []string{"system", "dns"}, invalid); got != nil {
			t.Fatalf("invalid exposure %d filtered modules to %v", invalid, got)
		}
		if err := CheckModuleExposure(testModuleCatalog(), []string{"system"}, invalid); err == nil || !strings.Contains(err.Error(), "unknown exposure level") {
			t.Fatalf("CheckModuleExposure(testModuleCatalog(), %d) = %v, want an invalid exposure error", invalid, err)
		}
		if EgressNeedsIPIntel(testModuleCatalog(), []string{"network"}, invalid) {
			t.Fatalf("invalid exposure %d enabled IP intelligence", invalid)
		}
	}
}
