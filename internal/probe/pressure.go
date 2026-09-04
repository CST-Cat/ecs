package probe

// Linux resource-pressure and cgroup diagnostics.
//
// PSI totals are sampled around a benchmark so no additional workload or
// network traffic is generated. cgroup counters are monotonic; deltas describe
// the benchmark window, while the system module also exposes cumulative facts.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
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
// resource facts.
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
		Limits:     collectResourceLimits(),
	}
	snapshot.Load1, snapshot.LoadKnown = readLoadAverage1()
	snapshot.CPUTimes, snapshot.CPUTracked = readCPUTimes()
	snapshot.CPUStat = readCgroupCPUStats()
	snapshot.Memory = readCgroupMemoryEvents()
	for _, resource := range []string{"cpu", "memory", "io"} {
		snapshot.PSI[resource] = readPressure(resource)
	}
	return snapshot
}

func collectResourceLimits() resourceLimits {
	limits := resourceLimits{CPU: detectCPUAllowance()}
	limits.CPUSet, limits.CPUSetCount, limits.CPUSetSource = readCPUSet()
	limits.MemoryLimit, limits.MemoryLimitVia, _ = cgroupMemoryLimit()
	limits.MemoryCurrent, limits.MemoryCurrentVia, _ = cgroupMemoryCurrent()
	if value, source, unlimited, ok := readCgroupLimit("memory.swap.max", "memory.memsw.limit_in_bytes"); ok {
		limits.MemorySwapLimit, limits.MemorySwapVia, limits.MemorySwapMax = value, source, unlimited
	}
	return limits
}

func currentCgroupPaths(file string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, relative := range selfCgroupPaths() {
		if relative == "" || relative == "/" {
			continue
		}
		path := filepath.Join(cgroupV2Root, relative, file)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	root := filepath.Join(cgroupV2Root, file)
	if !seen[root] {
		paths = append(paths, root)
	}
	return paths
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

func readPressure(resource string) psiResource {
	for _, path := range currentCgroupPaths(resource + ".pressure") {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parsed := parsePSI(string(data))
		if parsed.Some.Present || parsed.Full.Present {
			parsed.Source = path
			return parsed
		}
	}
	path := filepath.Join("/proc/pressure", resource)
	data, err := os.ReadFile(path)
	if err != nil {
		return psiResource{}
	}
	parsed := parsePSI(string(data))
	parsed.Source = path
	return parsed
}

func parseKeyValueCounters(data string) map[string]uint64 {
	values := make(map[string]uint64)
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			values[fields[0]] = value
		}
	}
	return values
}

func readCgroupCPUStats() cgroupCPUStats {
	for _, path := range append(currentCgroupPaths("cpu.stat"), filepath.Join(cgroupV1CPU, "cpu.stat")) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		values := parseKeyValueCounters(string(data))
		result := cgroupCPUStats{
			UsageUS: values["usage_usec"], NrPeriods: values["nr_periods"],
			NrThrottled: values["nr_throttled"], ThrottledUS: values["throttled_usec"],
			Source: path, Present: true,
		}
		if result.UsageUS == 0 && values["usage_ns"] > 0 {
			result.UsageUS = values["usage_ns"] / 1000
		}
		if result.ThrottledUS == 0 && values["throttled_time"] > 0 {
			result.ThrottledUS = values["throttled_time"] / 1000
		}
		return result
	}
	return cgroupCPUStats{}
}

func readCgroupMemoryEvents() cgroupMemoryEvents {
	for _, path := range currentCgroupPaths("memory.events") {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		values := parseKeyValueCounters(string(data))
		return cgroupMemoryEvents{
			Low: values["low"], High: values["high"], Max: values["max"],
			OOM: values["oom"], OOMKill: values["oom_kill"], OOMGroupKill: values["oom_group_kill"],
			Source: path, Present: true,
		}
	}
	for _, path := range append(cgroupCandidatePaths(cgroupV1Mem, "memory.failcnt"), cgroupCandidatePaths(cgroupV2Root, "memory.failcnt")...) {
		value, err := strconv.ParseUint(strings.TrimSpace(readTrimmed(path, "")), 10, 64)
		if err == nil {
			return cgroupMemoryEvents{FailCount: value, Source: path, Present: true}
		}
	}
	return cgroupMemoryEvents{}
}

func readCPUSet() (string, int, string) {
	for _, file := range []string{"cpuset.cpus.effective", "cpuset.cpus"} {
		for _, path := range currentCgroupPaths(file) {
			value := strings.TrimSpace(readTrimmed(path, ""))
			if value == "" {
				continue
			}
			return value, cpuSetCount(value), path
		}
	}
	return "", 0, ""
}

func cpuSetCount(value string) int {
	total := 0
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		startText, endText, ranged := strings.Cut(part, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 0 {
			continue
		}
		end := start
		if ranged {
			end, err = strconv.Atoi(endText)
			if err != nil || end < start {
				continue
			}
		}
		total += end - start + 1
	}
	return total
}

func readCgroupLimit(v2File, v1File string) (uint64, string, bool, bool) {
	for _, path := range currentCgroupPaths(v2File) {
		text := strings.TrimSpace(readTrimmed(path, ""))
		if text == "max" {
			return 0, path, true, true
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err == nil && value < cgroupV1Unlimited {
			return value, path, false, true
		}
	}
	for _, path := range cgroupCandidatePaths(cgroupV1Mem, v1File) {
		value, err := strconv.ParseUint(strings.TrimSpace(readTrimmed(path, "")), 10, 64)
		if err == nil {
			return value, path, value >= cgroupV1Unlimited, true
		}
	}
	return 0, "", false, false
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
// inferred.
func BuildPressureMeasurements(before, after EnvironmentSnapshot) []model.Measurement {
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
