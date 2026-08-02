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
	cfg, err := Defaults(ProfileQuick)
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

func TestParseOoklaServersAndValidation(t *testing.T) {
	servers, err := ParseOoklaServerList("telecom=123,联通=456,mobile=789")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 3 || servers[0].Carrier != "电信" || servers[2].Carrier != "移动" {
		t.Fatalf("servers = %+v", servers)
	}
	cfg, err := Defaults(ProfileQuick)
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
	cfg, err := Defaults(ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.BacktraceTargets[0].Family = "5"
	if err := Validate(cfg); err == nil {
		t.Fatal("unknown endpoint family should be rejected")
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
