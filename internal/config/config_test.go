package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDefaultsAndModuleSelection(t *testing.T) {
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != ProfileStandard {
		t.Fatalf("profile = %q", cfg.Profile)
	}
	if !contains(cfg.Modules, "route") || !contains(cfg.Modules, "speed") {
		t.Fatalf("standard modules = %v", cfg.Modules)
	}
	if cfg.IPerfDuration != 10*time.Second || len(cfg.IPerfTargets) == 0 {
		t.Fatalf("standard iperf settings = %s, targets=%d", cfg.IPerfDuration, len(cfg.IPerfTargets))
	}
	// 采样窗口必须达到 sysbench 的通行时长，短窗口会在突发性能机型上测到 burst credit。
	if cfg.CPUTime < 10*time.Second {
		t.Fatalf("standard cpu time = %s, want at least 10s", cfg.CPUTime)
	}
	selected := SelectModules(cfg.Modules, []string{"disk", "system", "disk"}, []string{"disk"})
	if want := []string{"system"}; !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected = %v, want %v", selected, want)
	}
}

func TestLoadFileRejectsUnknownAndTrailingJSON(t *testing.T) {
	directory := t.TempDir()
	unknown := filepath.Join(directory, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"profile":"quick","typo":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(unknown); err == nil {
		t.Fatal("expected unknown field error")
	}
	trailing := filepath.Join(directory, "trailing.json")
	if err := os.WriteFile(trailing, []byte(`{"profile":"quick"} {"profile":"full"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(trailing); err == nil {
		t.Fatal("expected trailing JSON error")
	}
}

func TestFileCanSetIPerfDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iperf.json")
	if err := os.WriteFile(path, []byte(`{"iperf_duration":"7s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFile(&cfg, file); err != nil {
		t.Fatal(err)
	}
	if cfg.IPerfDuration != 7*time.Second {
		t.Fatalf("iperf duration = %s", cfg.IPerfDuration)
	}
}

func TestEstimateOnlyCountsSelectedPressureModules(t *testing.T) {
	cfg, err := Defaults(ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"system", "network"}
	estimate := EstimateFor(cfg)
	if estimate.DiskMiB != 0 || estimate.NetworkMiB != 0 {
		t.Fatalf("estimate = %+v", estimate)
	}
	if estimate.DurationText == "" {
		t.Fatal("duration estimate is empty")
	}
	cfg.Modules = []string{"disk", "speed"}
	estimate = EstimateFor(cfg)
	if estimate.DiskMiB != cfg.DiskMiB || estimate.NetworkMiB != -1 || len(estimate.Notes) == 0 {
		t.Fatalf("iperf estimate = %+v", estimate)
	}
}

func TestValidateRejectsUnknownModule(t *testing.T) {
	cfg, err := Defaults(ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"not-a-module"}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error")
	}
	cfg.Modules = []string{"disk"}
	cfg.RouteTargets = []Endpoint{{Name: "unsafe", Address: "--output=/tmp/x"}}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected unsafe route target validation error")
	}
}

func TestIPQualitySourceValidation(t *testing.T) {
	cfg, err := Defaults(ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.IPQualitySources, []string{"all"}) {
		t.Fatalf("default IP quality sources = %v", cfg.IPQualitySources)
	}
	cfg.IPQualitySources = []string{"ipapi", "abuseipdb", "ipqs"}
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid IP sources rejected: %v", err)
	}
	cfg.IPQualitySources = []string{"all", "ipapi"}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected all combination error")
	}
	cfg.IPQualitySources = []string{"invented"}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected unknown IP source error")
	}
}
