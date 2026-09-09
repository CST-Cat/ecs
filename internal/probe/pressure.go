package probe

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

type psiValues struct {
	Avg10, Avg60, Avg300 float64
	TotalUS              uint64
	Present              bool
}

type psiResource struct {
	Some, Full psiValues
	Source     string
}

type cgroupCPUStats struct {
	UsageUS, NrPeriods, NrThrottled, ThrottledUS uint64
	Source                                       string
	Present                                      bool
}

type cgroupMemoryEvents struct {
	Low, High, Max, OOM, OOMKill, OOMGroupKill, FailCount uint64
	Source                                                string
	Present                                               bool
}

type resourceLimits struct {
	CPU              cpuAllowance
	CPUSet           string
	CPUSetCount      int
	CPUSetSource     string
	MemoryLimit      uint64
	MemoryLimitVia   string
	MemoryCurrent    uint64
	MemoryCurrentVia string
	MemorySwapLimit  uint64
	MemorySwapVia    string
	MemorySwapMax    bool
}

// EnvironmentSnapshot contains one point-in-time set of locally sampled
// resource facts. Platform files populate it; this file only defines the
// portable shape and arithmetic.
type EnvironmentSnapshot struct {
	CapturedAt time.Time
	Load1      float64
	LoadKnown  bool
	CPUTimes   cpuTimeSample
	CPUTracked bool
	CPUStat    cgroupCPUStats
	Memory     cgroupMemoryEvents
	PSI        map[string]psiResource
	Limits     resourceLimits
}

// CaptureEnvironmentSnapshot performs read-only local sampling.
func CaptureEnvironmentSnapshot() EnvironmentSnapshot {
	snapshot := EnvironmentSnapshot{
		CapturedAt: time.Now(),
		PSI:        make(map[string]psiResource, 3),
	}
	capturePlatformEnvironment(&snapshot)
	return snapshot
}

func parsePSI(data string) psiResource {
	var result psiResource
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		values := psiValues{}
		for _, field := range fields[1:] {
			key, raw, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "avg10":
				values.Avg10, _ = strconv.ParseFloat(raw, 64)
			case "avg60":
				values.Avg60, _ = strconv.ParseFloat(raw, 64)
			case "avg300":
				values.Avg300, _ = strconv.ParseFloat(raw, 64)
			case "total":
				values.TotalUS, _ = strconv.ParseUint(raw, 10, 64)
			}
		}
		values.Present = true
		switch fields[0] {
		case "some":
			result.Some = values
		case "full":
			result.Full = values
		}
	}
	return result
}

func counterDelta(before, after uint64) (uint64, bool) {
	if after < before {
		return 0, false
	}
	return after - before, true
}

func pressurePercent(before, after psiValues, elapsed time.Duration) (float64, bool) {
	elapsedUS := elapsed.Microseconds()
	if !before.Present || !after.Present || elapsedUS <= 0 {
		return 0, false
	}
	delta, ok := counterDelta(before.TotalUS, after.TotalUS)
	if !ok {
		return 0, false
	}
	percent := float64(delta) / float64(elapsedUS) * 100
	return math.Max(0, math.Min(100, percent)), true
}

func environmentMeasurement(key, label string, value float64, unit, display, method string) model.Measurement {
	return model.Measurement{
		Key: key, Label: label, Value: value, Unit: unit, Display: model.RawValue(display),
		Method: method, HigherIsBetter: model.BoolPtr(false),
	}
}

func pressureMeasurementLabel(key string) string {
	return "probe.pressure.metric." + key
}

// BuildPressureMeasurements computes ordinary measurements from two resource
// snapshots. Missing or non-monotonic counters are omitted rather than
// inferred. FreeBSD returns no Linux pressure facts from its platform hook.
func BuildPressureMeasurements(before, after EnvironmentSnapshot) []model.Measurement {
	if !platformPressureFactsAvailable() {
		return nil
	}
	elapsed := after.CapturedAt.Sub(before.CapturedAt)
	measurements := make([]model.Measurement, 0, 17)
	add := func(key string, value float64, unit, display, method string) {
		measurements = append(measurements, environmentMeasurement(key, pressureMeasurementLabel(key), value, unit, display, method))
	}
	if before.LoadKnown {
		add("pretest_load_1m", before.Load1, "load", fmt.Sprintf("%.2f", before.Load1), "proc-loadavg-v1")
	}
	if before.CPUTracked && after.CPUTracked {
		if steal, ok := stealPercent(before.CPUTimes, after.CPUTimes); ok {
			add("cpu_steal_percent_window", steal, "%", fmt.Sprintf("%.2f %%", steal), "proc-stat-steal-window-v1")
		}
	}
	if before.CPUStat.Present && after.CPUStat.Present && before.CPUStat.Source == after.CPUStat.Source {
		if events, ok := counterDelta(before.CPUStat.NrThrottled, after.CPUStat.NrThrottled); ok {
			add("cgroup_cpu_throttled_events_window", float64(events), "events", strconv.FormatUint(events, 10), "cgroup-cpu-stat-window-v1")
			throttledUS, timeOK := counterDelta(before.CPUStat.ThrottledUS, after.CPUStat.ThrottledUS)
			if timeOK && elapsed.Microseconds() > 0 {
				percent := float64(throttledUS) / float64(elapsed.Microseconds()) * 100
				add("cgroup_cpu_throttled_time_percent_window", percent, "%", fmt.Sprintf("%.2f %%", percent), "cgroup-cpu-stat-window-v1")
			}
		}
	}
	for _, resource := range []string{"cpu", "memory", "io"} {
		pre := before.PSI[resource]
		post := after.PSI[resource]
		if pre.Some.Present {
			key := resource + "_psi_some_avg10_pretest"
			add(key, pre.Some.Avg10, "%", fmt.Sprintf("%.2f %%", pre.Some.Avg10), "linux-psi-avg10-v1")
		}
		if percent, ok := pressurePercent(pre.Some, post.Some, elapsed); ok {
			key := resource + "_psi_some_percent_window"
			add(key, percent, "%", fmt.Sprintf("%.2f %%", percent), "linux-psi-total-window-v1")
		}
		if percent, ok := pressurePercent(pre.Full, post.Full, elapsed); ok {
			key := resource + "_psi_full_percent_window"
			add(key, percent, "%", fmt.Sprintf("%.2f %%", percent), "linux-psi-total-window-v1")
		}
	}
	if before.Memory.Present && after.Memory.Present && before.Memory.Source == after.Memory.Source {
		for _, event := range []struct {
			key           string
			before, after uint64
		}{
			{"cgroup_memory_high_events_window", before.Memory.High, after.Memory.High},
			{"cgroup_memory_max_events_window", before.Memory.Max, after.Memory.Max},
			{"cgroup_oom_events_window", before.Memory.OOM, after.Memory.OOM},
			{"cgroup_oom_kill_events_window", before.Memory.OOMKill, after.Memory.OOMKill},
		} {
			if delta, ok := counterDelta(event.before, event.after); ok {
				add(event.key, float64(delta), "events", strconv.FormatUint(delta, 10), "cgroup-memory-events-window-v1")
			}
		}
	}
	return measurements
}
