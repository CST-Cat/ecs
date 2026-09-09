//go:build linux

package probe

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Linux resource-pressure and cgroup diagnostics stay behind this build
// boundary. PSI totals are sampled around a benchmark; cgroup counters are
// monotonic and are interpreted as deltas by the shared arithmetic.
func capturePlatformEnvironment(snapshot *EnvironmentSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Limits = collectResourceLimits()
	snapshot.Load1, snapshot.LoadKnown = readLoadAverage1()
	snapshot.CPUTimes, snapshot.CPUTracked = readCPUTimes()
	snapshot.CPUStat = readCgroupCPUStats()
	snapshot.Memory = readCgroupMemoryEvents()
	for _, resource := range []string{"cpu", "memory", "io"} {
		snapshot.PSI[resource] = readPressure(resource)
	}
}

func platformPressureFactsAvailable() bool { return true }

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

func currentCgroupPaths(controller, file string) []string {
	paths := make([]string, 0)
	for _, candidate := range cgroupCurrentCandidates(controller, file, file) {
		paths = append(paths, candidate.path)
	}
	return paths
}

func readPressure(resource string) psiResource {
	for _, path := range currentCgroupPaths(resource, resource+".pressure") {
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
	for _, path := range currentCgroupPaths("cpu", "cpu.stat") {
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
	for _, candidate := range cgroupCurrentCandidates("memory", "memory.events", "memory.failcnt") {
		path := candidate.path
		if candidate.v2 {
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
		value, err := strconv.ParseUint(strings.TrimSpace(readTrimmed(path, "")), 10, 64)
		if err == nil {
			return cgroupMemoryEvents{FailCount: value, Source: path, Present: true}
		}
	}
	return cgroupMemoryEvents{}
}

func readCPUSet() (string, int, string) {
	for _, file := range []string{"cpuset.cpus.effective", "cpuset.cpus"} {
		for _, path := range currentCgroupPaths("cpuset", file) {
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
	var best uint64
	var source, unlimitedSource string
	finiteKnown, unlimitedKnown := false, false
	for _, candidate := range cgroupLimitCandidates("memory", v2File, v1File) {
		text := strings.TrimSpace(readTrimmed(candidate.path, ""))
		if text == "max" || (!candidate.v2 && text != "" && parseUintAtLeast(text, cgroupV1Unlimited)) {
			if !unlimitedKnown {
				unlimitedSource, unlimitedKnown = candidate.path, true
			}
			continue
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			continue
		}
		if !finiteKnown || value < best {
			best, source, finiteKnown = value, candidate.path, true
		}
	}
	if finiteKnown {
		return best, source, false, true
	}
	if unlimitedKnown {
		return 0, unlimitedSource, true, true
	}
	return 0, "", false, false
}

func parseUintAtLeast(text string, minimum uint64) bool {
	value, err := strconv.ParseUint(text, 10, 64)
	return err == nil && value >= minimum
}
