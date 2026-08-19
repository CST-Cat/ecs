package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsProvideStandardAndFullProfiles(t *testing.T) {
	standard, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	full, err := Defaults(ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	if standard.Profile != ProfileStandard || full.Profile != ProfileFull {
		t.Fatalf("profiles = %q/%q", standard.Profile, full.Profile)
	}
	if len(standard.Modules) == 0 || len(full.Modules) <= len(standard.Modules) {
		t.Fatalf("profile module sets = %d/%d", len(standard.Modules), len(full.Modules))
	}
	if contains(standard.Modules, "network") || !contains(full.Modules, "network") {
		t.Fatalf("profile-specific module selection = standard:%v full:%v", standard.Modules, full.Modules)
	}
}

func TestLoadFileAndApplyOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"exposure":"local","reveal":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFile(&runtime, file); err != nil {
		t.Fatal(err)
	}
	if runtime.Exposure != ExposureLocal || !runtime.Reveal {
		t.Fatalf("applied overrides = exposure %v, reveal %v", runtime.Exposure, runtime.Reveal)
	}
}

func TestValidateRejectsUnknownModule(t *testing.T) {
	runtime, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Modules = []string{"not-a-module"}
	if err := Validate(runtime); err == nil {
		t.Fatal("unknown module should be rejected")
	}
}
