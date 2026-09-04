package probe

import (
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

var pressureWindowMeasurementKeys = []string{
	"pretest_load_1m",
	"cpu_steal_percent_window",
	"cgroup_cpu_throttled_events_window",
	"cgroup_cpu_throttled_time_percent_window",
	"cpu_psi_some_avg10_pretest",
	"cpu_psi_some_percent_window",
	"cpu_psi_full_percent_window",
	"memory_psi_some_avg10_pretest",
	"memory_psi_some_percent_window",
	"memory_psi_full_percent_window",
	"io_psi_some_avg10_pretest",
	"io_psi_some_percent_window",
	"io_psi_full_percent_window",
	"cgroup_memory_high_events_window",
	"cgroup_memory_max_events_window",
	"cgroup_oom_events_window",
	"cgroup_oom_kill_events_window",
}

func TestPressureParsersAndCalculations(t *testing.T) {
	psi := parsePSI("some avg10=1.50 avg60=2.00 avg300=3.00 total=100\nfull avg10=0.10 avg60=0.20 avg300=0.30 total=20\n")
	if !psi.Some.Present || psi.Some.Avg10 != 1.5 || psi.Some.Avg60 != 2 || psi.Some.Avg300 != 3 || psi.Some.TotalUS != 100 {
		t.Fatalf("PSI some parse = %+v", psi.Some)
	}
	if !psi.Full.Present || psi.Full.Avg10 != 0.1 || psi.Full.Avg60 != 0.2 || psi.Full.Avg300 != 0.3 || psi.Full.TotalUS != 20 {
		t.Fatalf("PSI full parse = %+v", psi.Full)
	}
	if malformed := parsePSI("not-a-psi-line\n"); malformed.Some.Present || malformed.Full.Present {
		t.Fatalf("malformed PSI reported as present: %+v", malformed)
	}

	if counters := parseKeyValueCounters("usage_usec 10\nbad nope\nnr_periods 2\n"); counters["usage_usec"] != 10 || counters["nr_periods"] != 2 || len(counters) != 2 {
		t.Fatalf("counter parse = %v", counters)
	}
	if cpuSetCount("0-2,4,invalid,7-6") != 4 {
		t.Fatalf("cpuset count = %d", cpuSetCount("0-2,4,invalid,7-6"))
	}
	if delta, ok := counterDelta(2, 5); !ok || delta != 3 {
		t.Fatalf("counter delta = %v/%v", delta, ok)
	}
	if delta, ok := counterDelta(5, 2); ok || delta != 0 {
		t.Fatalf("counter rollback delta = %v/%v", delta, ok)
	}
	if value, ok := pressurePercent(
		psiValues{TotalUS: 100, Present: true},
		psiValues{TotalUS: 350_100, Present: true},
		time.Second,
	); !ok || value != 35 {
		t.Fatalf("pressure percentage = %v/%v", value, ok)
	}
	if value, ok := pressurePercent(psiValues{Present: true}, psiValues{Present: true, TotalUS: 2_000_000}, time.Second); !ok || value != 100 {
		t.Fatalf("pressure percentage clamp = %v/%v", value, ok)
	}
	if _, ok := pressurePercent(psiValues{TotalUS: 5, Present: true}, psiValues{TotalUS: 2, Present: true}, time.Second); ok {
		t.Fatal("PSI counter rollback produced a percentage")
	}
	if _, ok := pressurePercent(psiValues{}, psi.Some, time.Second); ok {
		t.Fatal("unavailable PSI produced a percentage")
	}
	if _, ok := pressurePercent(psi.Some, psi.Some, 0); ok {
		t.Fatal("zero-length window produced a percentage")
	}
}

func pressureWindowFixture() (EnvironmentSnapshot, EnvironmentSnapshot) {
	start := time.Unix(100, 0)
	before := EnvironmentSnapshot{
		CapturedAt: start,
		Load1:      3.25,
		LoadKnown:  true,
		CPUTimes:   cpuTimeSample{Total: 1_000, Steal: 10},
		CPUTracked: true,
		CPUStat: cgroupCPUStats{
			NrThrottled: 2, ThrottledUS: 1_000, Source: "fixture/cpu.stat", Present: true,
		},
		Memory: cgroupMemoryEvents{
			High: 1, Max: 2, OOM: 3, OOMKill: 4, Source: "fixture/memory.events", Present: true,
		},
		PSI: map[string]psiResource{
			"cpu": {
				Some:   psiValues{Avg10: 12.5, TotalUS: 100, Present: true},
				Full:   psiValues{Avg10: 1.5, TotalUS: 200, Present: true},
				Source: "fixture/cpu.pressure",
			},
			"memory": {
				Some:   psiValues{Avg10: 2.5, TotalUS: 100, Present: true},
				Full:   psiValues{Avg10: 0.5, TotalUS: 200, Present: true},
				Source: "fixture/memory.pressure",
			},
			"io": {
				Some:   psiValues{Avg10: 6.25, TotalUS: 400, Present: true},
				Full:   psiValues{Avg10: 0.25, TotalUS: 500, Present: true},
				Source: "fixture/io.pressure",
			},
		},
	}
	after := before
	after.CapturedAt = start.Add(2 * time.Second)
	after.CPUTimes = cpuTimeSample{Total: 1_200, Steal: 30}
	after.CPUStat.NrThrottled = 5
	after.CPUStat.ThrottledUS = 501_000
	after.Memory.High = 4
	after.Memory.Max = 7
	after.Memory.OOM = 8
	after.Memory.OOMKill = 10
	after.PSI = make(map[string]psiResource, len(before.PSI))
	for resource, pressure := range before.PSI {
		after.PSI[resource] = pressure
	}
	after.PSI["cpu"] = psiResource{
		Some:   psiValues{TotalUS: 500_100, Present: true},
		Full:   psiValues{TotalUS: 100_200, Present: true},
		Source: "fixture/cpu.pressure",
	}
	after.PSI["memory"] = psiResource{
		Some:   psiValues{TotalUS: 400_100, Present: true},
		Full:   psiValues{TotalUS: 300_200, Present: true},
		Source: "fixture/memory.pressure",
	}
	after.PSI["io"] = psiResource{
		Some:   psiValues{TotalUS: 600_400, Present: true},
		Full:   psiValues{TotalUS: 200_500, Present: true},
		Source: "fixture/io.pressure",
	}
	return before, after
}

func measurementsByKey(measurements []model.Measurement) map[string]model.Measurement {
	result := make(map[string]model.Measurement, len(measurements))
	for _, measurement := range measurements {
		result[measurement.Key] = measurement
	}
	return result
}

func TestPressureWindowMeasurementsUseExactRawFacts(t *testing.T) {
	before, after := pressureWindowFixture()
	measurements := BuildPressureMeasurements(before, after)
	if len(measurements) != len(pressureWindowMeasurementKeys) {
		t.Fatalf("window measurement count = %d, want %d: %+v", len(measurements), len(pressureWindowMeasurementKeys), measurements)
	}
	byKey := measurementsByKey(measurements)
	if len(byKey) != len(pressureWindowMeasurementKeys) {
		t.Fatalf("window measurement keys are not unique: %v", byKey)
	}
	want := map[string]struct {
		value   float64
		unit    string
		display string
		method  string
	}{
		"pretest_load_1m":                          {3.25, "load", "3.25", "proc-loadavg-v1"},
		"cpu_steal_percent_window":                 {10, "%", "10.00 %", "proc-stat-steal-window-v1"},
		"cgroup_cpu_throttled_events_window":       {3, "events", "3", "cgroup-cpu-stat-window-v1"},
		"cgroup_cpu_throttled_time_percent_window": {25, "%", "25.00 %", "cgroup-cpu-stat-window-v1"},
		"cpu_psi_some_avg10_pretest":               {12.5, "%", "12.50 %", "linux-psi-avg10-v1"},
		"cpu_psi_some_percent_window":              {25, "%", "25.00 %", "linux-psi-total-window-v1"},
		"cpu_psi_full_percent_window":              {5, "%", "5.00 %", "linux-psi-total-window-v1"},
		"memory_psi_some_avg10_pretest":            {2.5, "%", "2.50 %", "linux-psi-avg10-v1"},
		"memory_psi_some_percent_window":           {20, "%", "20.00 %", "linux-psi-total-window-v1"},
		"memory_psi_full_percent_window":           {15, "%", "15.00 %", "linux-psi-total-window-v1"},
		"io_psi_some_avg10_pretest":                {6.25, "%", "6.25 %", "linux-psi-avg10-v1"},
		"io_psi_some_percent_window":               {30, "%", "30.00 %", "linux-psi-total-window-v1"},
		"io_psi_full_percent_window":               {10, "%", "10.00 %", "linux-psi-total-window-v1"},
		"cgroup_memory_high_events_window":         {3, "events", "3", "cgroup-memory-events-window-v1"},
		"cgroup_memory_max_events_window":          {5, "events", "5", "cgroup-memory-events-window-v1"},
		"cgroup_oom_events_window":                 {5, "events", "5", "cgroup-memory-events-window-v1"},
		"cgroup_oom_kill_events_window":            {6, "events", "6", "cgroup-memory-events-window-v1"},
	}
	for _, key := range pressureWindowMeasurementKeys {
		measurement, ok := byKey[key]
		if !ok {
			t.Fatalf("missing window measurement %q", key)
		}
		expected := want[key]
		if measurement.Value != expected.value || measurement.Unit != expected.unit || measurement.Display.Text() != expected.display || measurement.Method != expected.method {
			t.Fatalf("measurement %q = %+v, want value=%v unit=%q display=%q method=%q", key, measurement, expected.value, expected.unit, expected.display, expected.method)
		}
		if measurement.Label != "probe.pressure.metric."+key {
			t.Fatalf("measurement %q label = %q", key, measurement.Label)
		}
		if measurement.HigherIsBetter == nil || *measurement.HigherIsBetter {
			t.Fatalf("measurement %q direction = %v, want explicit lower-is-better", key, measurement.HigherIsBetter)
		}
		if !i18n.Has(i18n.LangZH, measurement.Label) || !i18n.Has(i18n.LangEN, measurement.Label) {
			t.Fatalf("measurement %q lacks bilingual label", key)
		}
	}
}

func TestPressureWindowMeasurementsKeepZeroAndHighValues(t *testing.T) {
	start := time.Unix(200, 0)
	zeroBefore := EnvironmentSnapshot{
		CapturedAt: start, LoadKnown: true,
		CPUTimes: cpuTimeSample{Total: 1_000}, CPUTracked: true,
		CPUStat: cgroupCPUStats{Source: "fixture/cpu.stat", Present: true},
		Memory:  cgroupMemoryEvents{Source: "fixture/memory.events", Present: true},
		PSI: map[string]psiResource{
			"cpu":    {Some: psiValues{Present: true}, Full: psiValues{Present: true}},
			"memory": {Some: psiValues{Present: true}, Full: psiValues{Present: true}},
			"io":     {Some: psiValues{Present: true}, Full: psiValues{Present: true}},
		},
	}
	zeroAfter := zeroBefore
	zeroAfter.CapturedAt = start.Add(time.Second)
	zeroAfter.CPUTimes.Total = 1_100
	zeroAfter.CPUStat.NrThrottled = 0
	zeroAfter.CPUStat.ThrottledUS = 0
	zeroAfter.PSI = map[string]psiResource{
		"cpu":    {Some: psiValues{Present: true}, Full: psiValues{Present: true}},
		"memory": {Some: psiValues{Present: true}, Full: psiValues{Present: true}},
		"io":     {Some: psiValues{Present: true}, Full: psiValues{Present: true}},
	}
	zeroMeasurements := BuildPressureMeasurements(zeroBefore, zeroAfter)
	if len(zeroMeasurements) != len(pressureWindowMeasurementKeys) {
		t.Fatalf("zero measurement count = %d, want %d: %+v", len(zeroMeasurements), len(pressureWindowMeasurementKeys), zeroMeasurements)
	}
	zeroByKey := measurementsByKey(zeroMeasurements)
	for key, measurement := range zeroByKey {
		if measurement.Value != 0 {
			t.Fatalf("zero measurement %q = %+v", key, measurement)
		}
		wantDisplay := "0.00 %"
		if key == "pretest_load_1m" {
			wantDisplay = "0.00"
		} else if key == "cgroup_cpu_throttled_events_window" || key == "cgroup_memory_high_events_window" || key == "cgroup_memory_max_events_window" || key == "cgroup_oom_events_window" || key == "cgroup_oom_kill_events_window" {
			wantDisplay = "0"
		}
		if measurement.Display.Text() != wantDisplay {
			t.Fatalf("zero measurement %q display = %q, want %q", key, measurement.Display.Text(), wantDisplay)
		}
	}

	highBefore := zeroBefore
	highBefore.Load1 = 100
	highBefore.CPUTimes = cpuTimeSample{Total: 1_000, Steal: 0}
	highBefore.CPUStat.NrThrottled = 1
	highBefore.CPUStat.ThrottledUS = 0
	highBefore.Memory.High, highBefore.Memory.Max = 1, 1
	highBefore.Memory.OOM, highBefore.Memory.OOMKill = 1, 1
	highBefore.PSI = map[string]psiResource{
		"cpu":    {Some: psiValues{Avg10: 80, TotalUS: 0, Present: true}, Full: psiValues{Avg10: 70, TotalUS: 0, Present: true}},
		"memory": {Some: psiValues{Avg10: 90, TotalUS: 0, Present: true}, Full: psiValues{Avg10: 80, TotalUS: 0, Present: true}},
		"io":     {Some: psiValues{Avg10: 95, TotalUS: 0, Present: true}, Full: psiValues{Avg10: 85, TotalUS: 0, Present: true}},
	}
	highAfter := highBefore
	highAfter.CapturedAt = start.Add(time.Second)
	highAfter.CPUTimes = cpuTimeSample{Total: 1_100, Steal: 100}
	highAfter.CPUStat.NrThrottled = 6
	highAfter.CPUStat.ThrottledUS = 1_000_000
	highAfter.Memory.High, highAfter.Memory.Max = 4, 6
	highAfter.Memory.OOM, highAfter.Memory.OOMKill = 5, 7
	highAfter.PSI = map[string]psiResource{
		"cpu":    {Some: psiValues{TotalUS: 1_000_000, Present: true}, Full: psiValues{TotalUS: 1_000_000, Present: true}},
		"memory": {Some: psiValues{TotalUS: 1_000_000, Present: true}, Full: psiValues{TotalUS: 1_000_000, Present: true}},
		"io":     {Some: psiValues{TotalUS: 1_000_000, Present: true}, Full: psiValues{TotalUS: 1_000_000, Present: true}},
	}
	highByKey := measurementsByKey(BuildPressureMeasurements(highBefore, highAfter))
	for _, key := range pressureWindowMeasurementKeys {
		if _, ok := highByKey[key]; !ok {
			t.Fatalf("high-value measurement %q was filtered", key)
		}
	}
	for key, want := range map[string]float64{
		"pretest_load_1m":                          100,
		"cpu_steal_percent_window":                 100,
		"cgroup_cpu_throttled_events_window":       5,
		"cgroup_cpu_throttled_time_percent_window": 100,
		"cpu_psi_some_avg10_pretest":               80,
		"cpu_psi_some_percent_window":              100,
		"cpu_psi_full_percent_window":              100,
		"memory_psi_some_avg10_pretest":            90,
		"memory_psi_some_percent_window":           100,
		"memory_psi_full_percent_window":           100,
		"io_psi_some_avg10_pretest":                95,
		"io_psi_some_percent_window":               100,
		"io_psi_full_percent_window":               100,
		"cgroup_memory_high_events_window":         3,
		"cgroup_memory_max_events_window":          5,
		"cgroup_oom_events_window":                 4,
		"cgroup_oom_kill_events_window":            6,
	} {
		if highByKey[key].Value != want {
			t.Fatalf("high measurement %q = %v, want %v", key, highByKey[key].Value, want)
		}
	}
}

func TestPressureWindowMeasurementsRejectCounterRollback(t *testing.T) {
	start := time.Unix(300, 0)
	before := EnvironmentSnapshot{
		CapturedAt: start, Load1: 4, LoadKnown: true,
		CPUTimes: cpuTimeSample{Total: 1_000, Steal: 20}, CPUTracked: true,
		CPUStat: cgroupCPUStats{NrThrottled: 5, ThrottledUS: 500, Source: "fixture/cpu.stat", Present: true},
		Memory:  cgroupMemoryEvents{High: 5, Max: 6, OOM: 7, OOMKill: 8, Source: "fixture/memory.events", Present: true},
		PSI: map[string]psiResource{
			"cpu":    {Some: psiValues{Avg10: 1, TotalUS: 500, Present: true}, Full: psiValues{TotalUS: 600, Present: true}},
			"memory": {Some: psiValues{Avg10: 2, TotalUS: 700, Present: true}, Full: psiValues{TotalUS: 800, Present: true}},
			"io":     {Some: psiValues{Avg10: 3, TotalUS: 900, Present: true}, Full: psiValues{TotalUS: 1_000, Present: true}},
		},
	}
	after := before
	after.CapturedAt = start.Add(time.Second)
	after.CPUTimes = cpuTimeSample{Total: 900, Steal: 19}
	after.CPUStat.NrThrottled, after.CPUStat.ThrottledUS = 4, 400
	after.Memory.High, after.Memory.Max, after.Memory.OOM, after.Memory.OOMKill = 4, 5, 6, 7
	after.PSI = make(map[string]psiResource, len(before.PSI))
	for resource, pressure := range before.PSI {
		after.PSI[resource] = pressure
	}
	after.PSI["cpu"] = psiResource{Some: psiValues{TotalUS: 400, Present: true}, Full: psiValues{TotalUS: 500, Present: true}}
	after.PSI["memory"] = psiResource{Some: psiValues{Avg10: 2, TotalUS: 600, Present: true}, Full: psiValues{TotalUS: 700, Present: true}}
	after.PSI["io"] = psiResource{Some: psiValues{Avg10: 3, TotalUS: 800, Present: true}, Full: psiValues{TotalUS: 900, Present: true}}

	got := measurementsByKey(BuildPressureMeasurements(before, after))
	if got["pretest_load_1m"].Value != 4 {
		t.Fatalf("rollback fixture lost load measurement: %+v", got["pretest_load_1m"])
	}
	for _, key := range []string{
		"cpu_steal_percent_window",
		"cgroup_cpu_throttled_events_window",
		"cgroup_cpu_throttled_time_percent_window",
		"cpu_psi_some_percent_window",
		"cpu_psi_full_percent_window",
		"memory_psi_some_percent_window",
		"memory_psi_full_percent_window",
		"io_psi_some_percent_window",
		"io_psi_full_percent_window",
		"cgroup_memory_high_events_window",
		"cgroup_memory_max_events_window",
		"cgroup_oom_events_window",
		"cgroup_oom_kill_events_window",
	} {
		if _, ok := got[key]; ok {
			t.Fatalf("counter rollback generated %q: %+v", key, got[key])
		}
	}
	for _, key := range []string{
		"cpu_psi_some_avg10_pretest",
		"memory_psi_some_avg10_pretest",
		"io_psi_some_avg10_pretest",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("rollback fixture lost valid pretest measurement %q", key)
		}
	}
}

func TestPressureUnavailableInterfacesProduceNoFacts(t *testing.T) {
	if parsed := parsePSI(""); parsed.Some.Present || parsed.Full.Present || parsed.Source != "" {
		t.Fatalf("empty PSI reported as present: %+v", parsed)
	}
	if parsed := readPressure("pressure-interface-does-not-exist"); parsed.Some.Present || parsed.Full.Present || parsed.Source != "" {
		t.Fatalf("unavailable PSI reported as present: %+v", parsed)
	}
	before := EnvironmentSnapshot{CapturedAt: time.Unix(400, 0)}
	after := EnvironmentSnapshot{CapturedAt: time.Unix(401, 0)}
	if measurements := BuildPressureMeasurements(before, after); len(measurements) != 0 {
		t.Fatalf("unavailable snapshot produced measurements: %+v", measurements)
	}
}

func TestPressureMeasurementCatalogIsBilingual(t *testing.T) {
	for _, key := range pressureWindowMeasurementKeys {
		label := pressureMeasurementLabel(key)
		if !i18n.Has(i18n.LangZH, label) || !i18n.Has(i18n.LangEN, label) {
			t.Fatalf("missing bilingual pressure metric %q", label)
		}
	}
}
