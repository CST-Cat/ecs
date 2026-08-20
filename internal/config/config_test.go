package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
)

func useEnglish(t *testing.T) {
	t.Helper()
	original := i18n.Current()
	i18n.Set(i18n.LangEN)
	t.Cleanup(func() { i18n.Set(original) })
}

func validRuntime(t *testing.T) Runtime {
	t.Helper()
	runtime, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func requireError(t *testing.T, err error, marker string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), marker) {
		t.Fatalf("error = %v, want marker %q", err, marker)
	}
}

func TestDefaultsAndProfiles(t *testing.T) {
	useEnglish(t)
	for _, test := range []struct {
		input, want string
	}{
		{input: "", want: ProfileStandard},
		{input: ProfileStandard, want: ProfileStandard},
		{input: ProfileFull, want: ProfileFull},
	} {
		runtime, err := Defaults(test.input)
		if err != nil || runtime.Profile != test.want || len(runtime.Modules) == 0 || !reflect.DeepEqual(runtime.Formats, []string{"json", "md", "html"}) {
			t.Fatalf("%s defaults = %+v, %v", test.input, runtime, err)
		}
	}
	standard, _ := Defaults(ProfileStandard)
	full, _ := Defaults(ProfileFull)
	if len(full.Modules) <= len(standard.Modules) || contains(standard.Modules, "network") || !contains(full.Modules, "network") {
		t.Fatalf("profile module sets = standard:%v full:%v", standard.Modules, full.Modules)
	}
	_, err := Defaults("unknown")
	requireError(t, err, "unknown profile")
}

func TestLoadFileDiagnostics(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name    string
		content string
		path    string
		markers []string
	}{
		{name: "read failure", path: filepath.Join(t.TempDir(), "missing.json"), markers: []string{"read config file", "missing.json"}},
		{name: "valid", content: `{"profile":"full","cpu_time":"2s"}`},
		{name: "syntax", content: `{"profile":`, markers: []string{"parse config file", "unexpected EOF"}},
		{name: "unknown field", content: `{"not_a_field":true}`, markers: []string{"unknown field \"not_a_field\""}},
		{name: "second object", content: `{} {}`, markers: []string{"exactly one JSON object"}},
		{name: "trailing text", content: `{} trailing`, markers: []string{"invalid trailing content"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := test.path
			if path == "" {
				path = filepath.Join(t.TempDir(), "config.json")
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			file, err := LoadFile(path)
			if len(test.markers) > 0 {
				for _, marker := range test.markers {
					requireError(t, err, marker)
				}
				return
			}
			if err != nil || file.Profile != ProfileFull || file.CPUTime != "2s" {
				t.Fatalf("loaded file = %+v, %v", file, err)
			}
		})
	}
}

func TestApplyFileCopiesAllMeaningfulOverrides(t *testing.T) {
	useEnglish(t)
	example := ExampleFile()
	if example.Profile != ProfileStandard || example.Output == "" || example.Reveal == nil || !reflect.DeepEqual(example.Formats, []string{"json", "md", "html"}) {
		t.Fatalf("ExampleFile = %+v", example)
	}
	runtime := validRuntime(t)
	reveal, noColor, diskMulti := true, true, true
	diskMiB, dnsAttempts, latencyAttempts, speedThreads := 128, 3, 4, 2
	file := File{
		Only: []string{"network", "system"}, Skip: []string{"system"}, Exposure: ExposureNameAny,
		Reveal: &reveal, IPVersion: IPVersion6, IPQualitySources: []string{"IPINFO", "all", "ipinfo"},
		Formats: []string{"json", "md"}, Output: "./custom", NoColor: &noColor,
		CPUTime: "2s", DiskMiB: &diskMiB, DiskPath: "/tmp/fixture", DiskMulti: &diskMulti,
		DiskMatrixMode: DiskMatrixFixed, IPerfDuration: "3s",
		IPerfTargets: []IPerfEndpoint{{Name: "edge", Host: "example.com", PortStart: 5201, PortEnd: 5202}},
		HTTPTimeout:  "20s", DNSAttempts: &dnsAttempts, LatencyAttempts: &latencyAttempts, SpeedThreads: &speedThreads,
		DNSResolvers:     []Endpoint{{Name: "dns", Address: "1.1.1.1:53"}},
		LatencyTargets:   []Endpoint{{Name: "web", Address: "example.com:443"}},
		RouteTargets:     []Endpoint{{Name: "route", Address: "1.1.1.1"}},
		BacktraceTargets: []Endpoint{{Name: "trace", Address: "1.1.1.1"}},
		STUNServers:      []Endpoint{{Name: "stun", Address: "stun.example.com:3478"}},
		MediaRegions:     []string{"JP", "global", "jp"},
		OoklaServers:     []OoklaServer{{Carrier: "电信", ID: 1}},
	}
	if err := ApplyFile(&runtime, file); err != nil {
		t.Fatal(err)
	}
	expected := validRuntime(t)
	expected.Exposure = ExposureConsent
	expected.Reveal, expected.IPVersion = true, IPVersion6
	expected.IPQualitySources = []string{"ipinfo", "all"}
	expected.Formats, expected.Output, expected.NoColor = []string{"json", "md"}, "./custom", true
	expected.CPUTime, expected.DiskMiB, expected.DiskPath, expected.DiskMulti = 2*time.Second, diskMiB, "/tmp/fixture", true
	expected.DiskMatrixMode, expected.IPerfDuration = DiskMatrixFixed, 3*time.Second
	expected.IPerfTargets = []IPerfEndpoint{{Name: "edge", Host: "example.com", PortStart: 5201, PortEnd: 5202}}
	expected.HTTPTimeout, expected.DNSAttempts, expected.LatencyAttempts, expected.SpeedThreads = 20*time.Second, dnsAttempts, latencyAttempts, speedThreads
	expected.DNSResolvers = []Endpoint{{Name: "dns", Address: "1.1.1.1:53"}}
	expected.LatencyTargets = []Endpoint{{Name: "web", Address: "example.com:443"}}
	expected.RouteTargets = []Endpoint{{Name: "route", Address: "1.1.1.1"}}
	expected.BacktraceTargets = []Endpoint{{Name: "trace", Address: "1.1.1.1"}}
	expected.STUNServers = []Endpoint{{Name: "stun", Address: "stun.example.com:3478"}}
	expected.MediaRegions = []string{"jp", "global"}
	expected.OoklaServers = []OoklaServer{{Carrier: "电信", ID: 1}}
	expected.Modules = []string{"network"}
	if !reflect.DeepEqual(runtime, expected) {
		t.Fatalf("applied runtime differs from expected:\n got:  %+v\n want: %+v", runtime, expected)
	}
	file.IPerfTargets[0].Name = "mutated"
	if runtime.IPerfTargets[0].Name != "edge" {
		t.Fatal("ApplyFile retained caller-owned endpoint storage")
	}
}

func TestApplyFileDiagnostics(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name   string
		file   File
		marker string
	}{
		{name: "exposure", file: File{Exposure: "invalid"}, marker: "unknown exposure level"},
		{name: "cpu duration", file: File{CPUTime: "later"}, marker: "cpu_time:"},
		{name: "disk matrix", file: File{DiskMatrixMode: "burst"}, marker: "unknown disk matrix mode"},
		{name: "iperf duration", file: File{IPerfDuration: "later"}, marker: "iperf_duration:"},
		{name: "HTTP timeout", file: File{HTTPTimeout: "later"}, marker: "http_timeout:"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntime(t)
			requireError(t, ApplyFile(&runtime, test.file), test.marker)
		})
	}
}

func TestValidateReportsDistinctConfigurationErrors(t *testing.T) {
	useEnglish(t)
	for _, test := range []struct {
		name   string
		mutate func(*Runtime)
	}{
		{name: "defaults", mutate: func(*Runtime) {}},
		{name: "empty IP family", mutate: func(r *Runtime) { r.IPVersion = "" }},
	} {
		t.Run("valid "+test.name, func(t *testing.T) {
			runtime := validRuntime(t)
			test.mutate(&runtime)
			if err := Validate(runtime); err != nil {
				t.Fatalf("valid runtime rejected: %v", err)
			}
		})
	}
	cases := []struct {
		name   string
		mutate func(*Runtime)
		marker string
	}{
		{name: "no modules", mutate: func(r *Runtime) { r.Modules = nil }, marker: "at least one module must be selected"},
		{name: "IP family", mutate: func(r *Runtime) { r.IPVersion = "9" }, marker: "unknown IP family"},
		{name: "unknown module", mutate: func(r *Runtime) { r.Modules = []string{"missing"} }, marker: "unknown module"},
		{name: "exposure limit", mutate: func(r *Runtime) { r.Modules, r.Exposure = []string{"network"}, ExposureLocal }, marker: "above --exposure local"},
		{name: "CPU range", mutate: func(r *Runtime) { r.CPUTime = 0 }, marker: "CPU test duration"},
		{name: "disk range", mutate: func(r *Runtime) { r.DiskMiB = 1 }, marker: "disk test size"},
		{name: "HTTP range", mutate: func(r *Runtime) { r.HTTPTimeout = 0 }, marker: "HTTP timeout"},
		{name: "attempt range", mutate: func(r *Runtime) { r.DNSAttempts = 0 }, marker: "sample counts"},
		{name: "thread range", mutate: func(r *Runtime) { r.SpeedThreads = 0 }, marker: "speed test concurrency"},
		{name: "no formats", mutate: func(r *Runtime) { r.Formats = nil }, marker: "at least one output format"},
		{name: "unknown format", mutate: func(r *Runtime) { r.Formats = []string{"txt"} }, marker: `unknown output format "txt"`},
		{name: "IP source empty", mutate: func(r *Runtime) { r.IPQualitySources = nil }, marker: "IP quality sources must not be empty"},
		{name: "IP source unknown", mutate: func(r *Runtime) { r.IPQualitySources = []string{"vendor"} }, marker: "unknown IP quality source"},
		{name: "IP source combination", mutate: func(r *Runtime) { r.IPQualitySources = []string{"all", "ipinfo"} }, marker: "all/none cannot"},
		{name: "endpoint name", mutate: func(r *Runtime) { r.DNSResolvers = []Endpoint{{Address: "1.1.1.1:53"}} }, marker: "must have both name and address"},
		{name: "endpoint family", mutate: func(r *Runtime) { r.DNSResolvers = []Endpoint{{Name: "dns", Address: "1.1.1.1:53", Family: "9"}} }, marker: "family must be 4"},
		{name: "route name", mutate: func(r *Runtime) { r.RouteTargets = []Endpoint{{Address: "1.1.1.1"}} }, marker: "route targets must have both"},
		{name: "route unsafe", mutate: func(r *Runtime) { r.RouteTargets = []Endpoint{{Name: "route", Address: "bad host"}} }, marker: "route target"},
		{name: "route family", mutate: func(r *Runtime) { r.RouteTargets = []Endpoint{{Name: "route", Address: "1.1.1.1", Family: "9"}} }, marker: "route target \"route\" family"},
		{name: "STUN name", mutate: func(r *Runtime) { r.STUNServers = []Endpoint{{Address: "stun.example.com:3478"}} }, marker: "STUN servers must"},
		{name: "STUN address", mutate: func(r *Runtime) { r.STUNServers = []Endpoint{{Name: "stun", Address: "stun.example.com"}} }, marker: "STUN server"},
		{name: "backtrace name", mutate: func(r *Runtime) { r.BacktraceTargets = []Endpoint{{Address: "1.1.1.1"}} }, marker: "backtrace targets must"},
		{name: "backtrace unsafe", mutate: func(r *Runtime) { r.BacktraceTargets = []Endpoint{{Name: "trace", Address: "bad host"}} }, marker: "backtrace target"},
		{name: "backtrace family", mutate: func(r *Runtime) { r.BacktraceTargets = []Endpoint{{Name: "trace", Address: "1.1.1.1", Family: "9"}} }, marker: "backtrace target \"trace\" family"},
		{name: "Ookla carrier", mutate: func(r *Runtime) { r.OoklaServers = []OoklaServer{{Carrier: "other", ID: 1}} }, marker: "carrier must be"},
		{name: "Ookla ID", mutate: func(r *Runtime) { r.OoklaServers = []OoklaServer{{Carrier: "电信", ID: 0}} }, marker: "invalid ID"},
		{name: "Ookla duplicate", mutate: func(r *Runtime) {
			r.OoklaServers = []OoklaServer{{Carrier: "电信", ID: 1}, {Carrier: "电信", ID: 2}}
		}, marker: "must not configure"},
		{name: "disk path", mutate: func(r *Runtime) { r.DiskPath = "" }, marker: "disk test path must not"},
		{name: "disk matrix", mutate: func(r *Runtime) { r.DiskMatrixMode = "burst" }, marker: "unknown disk matrix mode"},
		{name: "iperf duration", mutate: func(r *Runtime) { r.IPerfDuration = 0 }, marker: "iperf3 per-direction"},
		{name: "iperf node", mutate: func(r *Runtime) {
			r.IPerfTargets = []IPerfEndpoint{{Name: "edge", Host: "bad host", PortStart: 1, PortEnd: 1}}
		}, marker: "invalid iperf3 node name"},
		{name: "iperf range", mutate: func(r *Runtime) {
			r.IPerfTargets = []IPerfEndpoint{{Name: "edge", Host: "example.com", PortStart: 2, PortEnd: 1}}
		}, marker: "invalid port range"},
		{name: "iperf network", mutate: func(r *Runtime) {
			r.IPerfTargets = []IPerfEndpoint{{Name: "edge", Host: "example.com", PortStart: 1, PortEnd: 1, Networks: "other"}}
		}, marker: "networks must"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntime(t)
			test.mutate(&runtime)
			requireError(t, Validate(runtime), test.marker)
		})
	}
}

func TestListSelectionAndIPVersionHelpers(t *testing.T) {
	if got := ParseList(" A, a, b,, "); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("ParseList = %v", got)
	}
	if got := SelectModules([]string{"dns", "system"}, []string{"network", "missing", "system", "network"}, nil); !reflect.DeepEqual(got, []string{"system", "network"}) {
		t.Fatalf("SelectModules = %v", got)
	}
	if got := SelectModules([]string{"dns", "system"}, nil, []string{"dns"}); !reflect.DeepEqual(got, []string{"system"}) {
		t.Fatalf("SelectModules skip = %v", got)
	}
	for _, test := range []struct {
		mode string
		want []string
	}{
		{mode: "auto", want: []string{"4", "6"}},
		{mode: "4", want: []string{"4"}},
		{mode: "6", want: []string{"6"}},
		{mode: "other", want: []string{"4", "6"}},
	} {
		if got := IPVersions(test.mode); !reflect.DeepEqual(got, test.want) {
			t.Errorf("IPVersions(%q) = %v", test.mode, got)
		}
	}
	if !AllowsIPVersion("auto", "4") || AllowsIPVersion("4", "6") {
		t.Fatal("IP family allowance is incorrect")
	}
}

func TestParseDiskMatrixMode(t *testing.T) {
	useEnglish(t)
	for _, test := range []struct {
		raw, want, marker string
	}{
		{raw: "", want: DiskMatrixTime},
		{raw: "time", want: DiskMatrixTime},
		{raw: "fixed", want: DiskMatrixFixed},
		{raw: "burst", marker: "unknown disk matrix mode"},
	} {
		got, err := ParseDiskMatrixMode(test.raw)
		if test.marker != "" {
			requireError(t, err, test.marker)
		} else if err != nil || got != test.want {
			t.Fatalf("ParseDiskMatrixMode(%q) = %q, %v", test.raw, got, err)
		}
	}
}
