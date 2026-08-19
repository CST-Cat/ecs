package config

import (
	"reflect"
	"strings"
	"testing"
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
	if Exposure(99).String() != ExposureNameThirdParty {
		t.Fatal("unknown exposure enum should use the safe fallback")
	}
}

func TestExposureFilteringAndEgressSemantics(t *testing.T) {
	modules := []string{"system", "dns", "network"}
	if got := FilterModulesByExposure(modules, ExposureLocal); !reflect.DeepEqual(got, []string{"system"}) {
		t.Fatalf("local modules = %v", got)
	}
	if got := FilterModulesByExposure(modules, ExposurePublic); !reflect.DeepEqual(got, []string{"system", "dns"}) {
		t.Fatalf("public modules = %v", got)
	}
	if !AllowsModule(ExposurePublic, "dns") || AllowsModule(ExposurePublic, "network") ||
		ExposureFor("network").Level != ExposureThirdParty || !ExposureFor("network").NeedsEgressIP ||
		ExposureFor("unknown").Level != ExposureConsent {
		t.Fatal("exposure boundary or unknown-module safety is incorrect")
	}
	if !RequiresEgressIP([]string{"network"}) || RequiresEgressIP([]string{"system"}) {
		t.Fatal("egress dependency classification is incorrect")
	}
	if EgressNeedsIPIntel([]string{"network"}, ExposurePublic) || !EgressNeedsIPIntel([]string{"network"}, ExposureThirdParty) || EgressNeedsIPIntel([]string{"system"}, ExposureThirdParty) {
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
	runtime.Exposure = ExposureLocal
	return runtime
}

func TestCheckModuleExposure(t *testing.T) {
	useEnglish(t)
	if err := CheckModuleExposure([]string{"system"}, ExposureLocal); err != nil {
		t.Fatal(err)
	}
	err := CheckModuleExposure([]string{"network"}, ExposureLocal)
	if err == nil || !strings.Contains(err.Error(), "above the current --exposure local") {
		t.Fatalf("blocked module error = %v", err)
	}
}
