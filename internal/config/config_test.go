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
	if cfg.IPVersion != IPVersionAuto {
		t.Fatalf("default IP version = %q", cfg.IPVersion)
	}
	if !contains(cfg.Modules, "route") || !contains(cfg.Modules, "speed") {
		t.Fatalf("standard modules = %v", cfg.Modules)
	}
	if cfg.IPerfDuration != 15*time.Second || len(cfg.IPerfTargets) != 7 {
		t.Fatalf("standard iperf settings = %s, targets=%d; want full-depth 15s/7 nodes", cfg.IPerfDuration, len(cfg.IPerfTargets))
	}
	if cfg.CPUTime != 15*time.Second {
		t.Fatalf("standard cpu time = %s, want full-depth 15s", cfg.CPUTime)
	}
	selected := SelectModules(cfg.Modules, []string{"disk", "system", "disk"}, []string{"disk"})
	if want := []string{"system"}; !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected = %v, want %v", selected, want)
	}
}

func TestProfilesOnlyChangeModulePreset(t *testing.T) {
	full, err := Defaults(ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int{
		ProfileStandard: 16,
		ProfileFull:     18,
	}
	for _, profile := range []string{ProfileStandard, ProfileFull} {
		cfg, err := Defaults(profile)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Modules) != wantCounts[profile] {
			t.Fatalf("%s module count = %d, want %d", profile, len(cfg.Modules), wantCounts[profile])
		}
		if cfg.CPUTime != full.CPUTime || cfg.DiskMiB != full.DiskMiB ||
			cfg.DNSAttempts != full.DNSAttempts || cfg.LatencyAttempts != full.LatencyAttempts ||
			cfg.SpeedThreads != full.SpeedThreads || cfg.IPerfDuration != full.IPerfDuration ||
			!reflect.DeepEqual(cfg.IPerfTargets, full.IPerfTargets) {
			t.Fatalf("%s benchmark defaults differ from full depth: %+v", profile, cfg)
		}
	}
}

func TestOnlyCanSelectModulesOutsideProfile(t *testing.T) {
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name string
		only []string
		want []string
	}{
		{name: "cnspeed and disk", only: []string{"cnspeed", "disk"}, want: []string{"disk", "cnspeed"}},
		{name: "ookla", only: []string{"ookla"}, want: []string{"ookla"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SelectModules(cfg.Modules, testCase.only, nil); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("SelectModules(%v) = %v, want %v", testCase.only, got, testCase.want)
			}
			runtime := cfg
			if err := ApplyFile(&runtime, File{Only: testCase.only}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtime.Modules, testCase.want) {
				t.Fatalf("ApplyFile only=%v modules = %v, want %v", testCase.only, runtime.Modules, testCase.want)
			}
		})
	}
	// --skip still filters an explicitly selected module after --only.
	if got := SelectModules(cfg.Modules, []string{"cnspeed", "disk"}, []string{"disk"}); !reflect.DeepEqual(got, []string{"cnspeed"}) {
		t.Fatalf("SelectModules skip = %v, want [cnspeed]", got)
	}
}

func TestIPVersionSelection(t *testing.T) {
	cases := []struct {
		mode string
		want []string
	}{
		{IPVersionAuto, []string{IPVersion4, IPVersion6}},
		{IPVersion4, []string{IPVersion4}},
		{IPVersion6, []string{IPVersion6}},
		{"", []string{IPVersion4, IPVersion6}},
	}
	for _, testCase := range cases {
		if got := IPVersions(testCase.mode); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("IPVersions(%q) = %v, want %v", testCase.mode, got, testCase.want)
		}
	}
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{IPVersionAuto, IPVersion4, IPVersion6} {
		cfg.IPVersion = mode
		if err := Validate(cfg); err != nil {
			t.Errorf("valid IP version %q rejected: %v", mode, err)
		}
	}
	cfg.IPVersion = "5"
	if err := Validate(cfg); err == nil {
		t.Fatal("unknown IP version should be rejected")
	}
}

func TestLoadFileRejectsUnknownAndTrailingJSON(t *testing.T) {
	directory := t.TempDir()
	unknown := filepath.Join(directory, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"profile":"standard","typo":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(unknown); err == nil {
		t.Fatal("expected unknown field error")
	}
	trailing := filepath.Join(directory, "trailing.json")
	if err := os.WriteFile(trailing, []byte(`{"profile":"standard"} {"profile":"full"}`), 0o600); err != nil {
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
	cfg, err := Defaults(ProfileStandard)
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

func TestParseOoklaServersAndValidation(t *testing.T) {
	servers, err := ParseOoklaServerList("telecom=123,联通=456,mobile=789")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 3 || servers[0].Carrier != "电信" || servers[2].Carrier != "移动" {
		t.Fatalf("servers = %+v", servers)
	}
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OoklaServers = servers
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid Ookla servers rejected: %v", err)
	}
	cfg.OoklaServers[1].ID = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("invalid Ookla server ID should be rejected")
	}
}

func TestParseEndpointListInfersFamily(t *testing.T) {
	endpoints, err := ParseEndpointList("v4=1.1.1.1,v6=[2001:db8::1],host=example-v6.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if endpoints[0].Family != IPVersion4 || endpoints[1].Family != IPVersion6 || endpoints[2].Family != IPVersion6 {
		t.Fatalf("inferred endpoint families = %+v", endpoints)
	}
	if _, err := ParseEndpointList("bad=example", false); err != nil {
		t.Fatalf("hostname should remain valid: %v", err)
	}
}

func TestValidateRejectsUnknownEndpointFamily(t *testing.T) {
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.BacktraceTargets[0].Family = "5"
	if err := Validate(cfg); err == nil {
		t.Fatal("unknown endpoint family should be rejected")
	}
}

func TestIPQualitySourceValidation(t *testing.T) {
	cfg, err := Defaults(ProfileStandard)
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
