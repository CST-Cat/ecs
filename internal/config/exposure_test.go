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
	if got := FilterModulesByExposure(modules, module.ExposureLocal); !reflect.DeepEqual(got, []string{"system"}) {
		t.Fatalf("local modules = %v", got)
	}
	if got := FilterModulesByExposure(modules, module.ExposurePublic); !reflect.DeepEqual(got, []string{"system", "dns"}) {
		t.Fatalf("public modules = %v", got)
	}
	if !AllowsModule(module.ExposurePublic, "dns") || AllowsModule(module.ExposurePublic, "network") ||
		exposureFor("network").Level != module.ExposureThirdParty || !exposureFor("network").NeedsEgressIP ||
		exposureFor("unknown").Level != module.ExposureConsent {
		t.Fatal("exposure boundary or unknown-module safety is incorrect")
	}
	if !RequiresEgressIP([]string{"network"}) || RequiresEgressIP([]string{"system"}) {
		t.Fatal("egress dependency classification is incorrect")
	}
	if EgressNeedsIPIntel([]string{"network"}, module.ExposurePublic) || !EgressNeedsIPIntel([]string{"network"}, module.ExposureThirdParty) || EgressNeedsIPIntel([]string{"system"}, module.ExposureThirdParty) {
		t.Fatal("IP intelligence exposure boundary is incorrect")
	}
	if !validRuntimeForExposure().OfflineOnly() {
		t.Fatal("local runtime should be offline-only")
	}
	defaultRuntime, _ := Defaults(ProfileStandard)
	if defaultRuntime.OfflineOnly() {
		t.Fatal("local runtime should be offline-only")
	}
}

func validRuntimeForExposure() Runtime {
	runtime, _ := Defaults(ProfileStandard)
	runtime.Exposure = module.ExposureLocal
	return runtime
}

func TestCheckModuleExposure(t *testing.T) {
	useEnglish(t)
	if err := CheckModuleExposure([]string{"system"}, module.ExposureLocal); err != nil {
		t.Fatal(err)
	}
	err := CheckModuleExposure([]string{"network"}, module.ExposureLocal)
	if err == nil || !strings.Contains(err.Error(), "above the current --exposure local") {
		t.Fatalf("blocked module error = %v", err)
	}
}

func TestInvalidRuntimeExposureFailsClosed(t *testing.T) {
	useEnglish(t)
	for _, invalid := range []module.Exposure{-1, 4, 99} {
		runtime := validRuntimeForExposure()
		runtime.Exposure = invalid
		if err := Validate(runtime); err == nil || !strings.Contains(err.Error(), "unknown exposure level") {
			t.Fatalf("Validate(%d) = %v, want an invalid exposure error", invalid, err)
		}
		if AllowsModule(invalid, "system") {
			t.Fatalf("invalid exposure %d allowed a module", invalid)
		}
		if got := FilterModulesByExposure([]string{"system", "dns"}, invalid); got != nil {
			t.Fatalf("invalid exposure %d filtered modules to %v", invalid, got)
		}
		if err := CheckModuleExposure([]string{"system"}, invalid); err == nil || !strings.Contains(err.Error(), "unknown exposure level") {
			t.Fatalf("CheckModuleExposure(%d) = %v, want an invalid exposure error", invalid, err)
		}
		if EgressNeedsIPIntel([]string{"network"}, invalid) {
			t.Fatalf("invalid exposure %d enabled IP intelligence", invalid)
		}
	}
}
