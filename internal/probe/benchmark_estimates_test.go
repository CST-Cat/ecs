package probe

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/module"
)

func TestEstimateSpeedNoteIsLocalized(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	runtime := config.Runtime{
		Modules:       []string{"speed"},
		Exposure:      module.ExposureThirdParty,
		IPerfTargets:  []config.IPerfEndpoint{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		IPerfDuration: 7 * time.Second,
		SpeedThreads:  11,
	}
	for _, test := range []struct {
		name        string
		language    i18n.Lang
		containsHan bool
	}{
		{name: "English", language: i18n.LangEN, containsHan: false},
		{name: "Chinese", language: i18n.LangZH, containsHan: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			i18n.Set(test.language)
			notes := EstimateFor(runtime).Notes
			if len(notes) != 1 {
				t.Fatalf("estimate notes = %#v, want one speed note", notes)
			}
			note := notes[0]
			if want := fmt.Sprintf(i18n.T("estimate.speed"), len(runtime.IPerfTargets), runtime.IPerfDuration, runtime.SpeedThreads); note != want {
				t.Fatalf("speed note = %q, want localized template %q", note, want)
			}
			if !strings.Contains(note, "iperf3") {
				t.Fatalf("speed note = %q, want iperf3", note)
			}
			if got := containsHan(note); got != test.containsHan {
				t.Fatalf("speed note Han characters = %v, want %v: %q", got, test.containsHan, note)
			}
		})
	}
}

func TestEstimatePlansAndPublicSummary(t *testing.T) {
	base := config.Runtime{
		CPUTime:         5 * time.Second,
		DNSAttempts:     3,
		LatencyAttempts: 2,
		IPerfDuration:   5 * time.Second,
		IPerfTargets:    []config.IPerfEndpoint{{Name: "a"}, {Name: "b"}},
		SpeedThreads:    4,
		IPVersion:       config.IPVersionAuto,
		RouteTargets:    []config.Endpoint{{Name: "a"}, {Name: "b"}},
	}
	descriptor := func(id string) module.Descriptor {
		t.Helper()
		value, ok := config.ModuleDescriptorFor(BuiltinCatalog(), id)
		if !ok {
			t.Fatalf("descriptor %q missing", id)
		}
		return value
	}
	ordinary := descriptor("system").Estimate
	cases := []struct {
		name    string
		id      string
		workers int
		runtime config.Runtime
		want    time.Duration
	}{
		{name: "cpu", id: "cpu", workers: 1, runtime: base, want: 6 * time.Second},
		{name: "memory", id: "memory", workers: 4, runtime: base, want: 21 * time.Second},
		{name: "disk", id: "disk", workers: 4, runtime: base, want: FIOPlanDuration() + 10*time.Second},
		{name: "dns", id: "dns", workers: 4, runtime: base, want: 3 * time.Second},
		{name: "latency", id: "latency", workers: 4, runtime: base, want: 3 * time.Second},
		{name: "speed auto", id: "speed", workers: 4, runtime: base, want: 60 * time.Second},
		{name: "speed v4", id: "speed", workers: 4, runtime: func() config.Runtime { value := base; value.IPVersion = config.IPVersion4; return value }(), want: 30 * time.Second},
		{name: "route", id: "route", workers: 4, runtime: base, want: 24 * time.Second},
		{name: "fixed ordinary", id: "system", workers: 4, runtime: base, want: ordinary},
		{name: "two-context zstd", id: "zstd", workers: 1, runtime: base, want: descriptor("zstd").Estimate / 2},
		{name: "two-context npb", id: "npb", workers: 1, runtime: base, want: descriptor("npb").Estimate / 2},
		{name: "two-context crypto", id: "crypto", workers: 1, runtime: base, want: descriptor("crypto").Estimate / 2},
		{name: "defensive default", id: "future", workers: 4, runtime: base, want: 7 * time.Second},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var item module.Descriptor
			if test.id == "future" {
				item = module.Descriptor{ID: test.id, Estimate: 7 * time.Second, EstimateMode: "future"}
			} else {
				item = descriptor(test.id)
			}
			if got := estimateModuleDuration(test.runtime, item, test.workers); got != test.want {
				t.Fatalf("estimate = %s, want %s", got, test.want)
			}
		})
	}
	if got := cpuBenchmarkEstimate(base, 4); got != 11*time.Second || streamBenchmarkEstimate(base, 1) != 11*time.Second || twoContextBenchmarkEstimate(20*time.Second, 1) != 10*time.Second || twoContextBenchmarkEstimate(ordinary, 4) != ordinary {
		t.Fatalf("worker-sensitive estimates = cpu %s/memory %s/fixed %s", cpuBenchmarkEstimate(base, 4), streamBenchmarkEstimate(base, 1), twoContextBenchmarkEstimate(20*time.Second, 1))
	}

	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangZH)
	online := base
	online.Modules = []string{"speed"}
	online.Exposure = module.ExposureThirdParty
	onlineEstimate := EstimateFor(online)
	if onlineEstimate.NetworkMiB != -1 || onlineEstimate.DiskMiB != 0 || onlineEstimate.DurationText == "" || !strings.Contains(strings.Join(onlineEstimate.Notes, " "), "iperf3") {
		t.Fatalf("online estimate = %+v", onlineEstimate)
	}
	offline := online
	offline.Exposure = module.ExposureLocal
	offlineEstimate := EstimateFor(offline)
	if offlineEstimate.NetworkMiB != 0 || !strings.Contains(strings.Join(offlineEstimate.Notes, " "), "离线") {
		t.Fatalf("offline estimate = %+v", offlineEstimate)
	}
	routeEstimate := EstimateFor(config.Runtime{Modules: []string{"route"}, Exposure: module.ExposureThirdParty, RouteTargets: base.RouteTargets})
	if !strings.Contains(strings.Join(routeEstimate.Notes, " "), "路由") || routeEstimate.DiskMiB != 0 {
		t.Fatalf("route estimate = %+v", routeEstimate)
	}
	disk := online
	disk.Modules = []string{"disk"}
	disk.DiskMiB = 123
	diskEstimate := EstimateFor(disk)
	if diskEstimate.DiskMiB != 123 || diskEstimate.NetworkMiB != 0 {
		t.Fatalf("disk estimate = %+v", diskEstimate)
	}
	if !strings.Contains(durationEstimateText(5*time.Second, 30*time.Second), "秒") || !strings.Contains(durationEstimateText(time.Minute, 2*time.Minute), "分钟") {
		t.Fatal("duration estimate text did not select seconds/minutes forms")
	}
}
