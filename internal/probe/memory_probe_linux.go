//go:build linux

package probe

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func platformUsesMemoryAvailableFallback() bool { return true }

func collectPlatformMemoryUsageSnapshot() memoryUsageSnapshot {
	mem := parseMemInfo("/proc/meminfo")
	limit, _, _ := cgroupMemoryLimit()
	memory := memoryUsageFromMemInfo(mem, limit)
	if !memory.LimitApplied {
		return memory
	}
	current, currentSource, currentOK := cgroupMemoryCurrent()
	return applyCgroupMemoryUsage(memory, currentSource, current, currentOK, cgroupMemoryLimitCandidates())
}

func collectPlatformMemoryFacilities() (memoryFacility, memoryFacility) {
	return detectBalloonReclaim("/sys", "/proc/vmstat"), detectKSM("/sys")
}

func applyCgroupMemoryUsage(memory memoryUsageSnapshot, currentSource string, current uint64, currentOK bool, limits []cgroupMemoryLimitCandidate) memoryUsageSnapshot {
	// Host MemAvailable is a useful upper bound, including an explicit zero,
	// but never substitutes for cgroup aggregate usage.
	memory.EffectiveAvailableBytes = 0
	memory.EffectiveAvailableKnown = false
	memory.EffectiveUsedBytes = 0
	memory.EffectiveUsagePercent = 0
	memory.EffectiveCurrentKnown = false
	if currentOK {
		memory.EffectiveUsedBytes = current
		memory.EffectiveCurrentKnown = true
		if memory.EffectiveTotalBytes > 0 {
			memory.EffectiveUsagePercent = float64(current) / float64(memory.EffectiveTotalBytes) * 100
		}
	}
	if len(limits) == 0 {
		return memory
	}
	available := memory.HostAvailableBytes
	usageKnown := true
	for _, limit := range limits {
		usage, ok := cgroupMemoryUsageAt(limit)
		if !ok && currentOK && filepath.Dir(limit.path) == filepath.Dir(currentSource) {
			usage, ok = current, true
		}
		if !ok {
			usageKnown = false
			continue
		}
		remaining := uint64(0)
		if usage < limit.limit {
			remaining = limit.limit - usage
		}
		if remaining < available {
			available = remaining
		}
	}
	memory.EffectiveAvailableKnown = usageKnown && memory.AvailableKnown
	if memory.EffectiveAvailableKnown {
		memory.EffectiveAvailableBytes = available
	}
	return memory
}

func cgroupMemoryUsageAt(limit cgroupMemoryLimitCandidate) (uint64, bool) {
	file := "memory.current"
	if !limit.v2 {
		file = "memory.usage_in_bytes"
	}
	text := strings.TrimSpace(readTrimmed(filepath.Join(filepath.Dir(limit.path), file), ""))
	value, err := strconv.ParseUint(text, 10, 64)
	return value, err == nil
}

// detectBalloonReclaim checks sysfs controls and reclaim-related vmstat
// counters. A virtio-balloon device alone is insufficient evidence.
func detectBalloonReclaim(sysfsRoot, procVMStatPath string) memoryFacility {
	roots := []string{
		filepath.Join(sysfsRoot, "class", "virtio-balloon"),
		filepath.Join(sysfsRoot, "devices", "virtual", "virtio-balloon"),
	}
	attributes := []string{"reclaim", "free_page_report", "free_page_reporting", "deflate_on_oom"}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, attribute := range attributes {
				path := filepath.Join(root, entry.Name(), attribute)
				if value, ok := readFacilityFile(path); ok {
					return memoryFacility{Available: true, Evidence: path + "=" + value}
				}
			}
		}
	}
	if evidence := balloonVMStatEvidence(procVMStatPath); evidence != "" {
		return memoryFacility{Available: true, Evidence: evidence}
	}
	return memoryFacility{Evidence: "no reclaim control or reclaim-related /proc/vmstat counter found"}
}

func balloonVMStatEvidence(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	var matches []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "balloon_") || !isUint(fields[1]) {
			continue
		}
		if strings.Contains(fields[0], "reclaim") || strings.Contains(fields[0], "migrat") || strings.Contains(fields[0], "defer") {
			matches = append(matches, fields[0]+"="+fields[1])
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return path + ": " + strings.Join(matches, ", ")
}

// detectKSM requires both the run control and pages_sharing statistic.
func detectKSM(sysfsRoot string) memoryFacility {
	root := filepath.Join(sysfsRoot, "kernel", "mm", "ksm")
	run, runOK := readFacilityFile(filepath.Join(root, "run"))
	sharing, sharingOK := readFacilityFile(filepath.Join(root, "pages_sharing"))
	if runOK && sharingOK {
		return memoryFacility{Available: true, Evidence: fmt.Sprintf("%s/run=%s; %s/pages_sharing=%s", root, run, root, sharing)}
	}
	if runOK {
		return memoryFacility{Evidence: root + "/run present but pages_sharing is unavailable"}
	}
	return memoryFacility{Evidence: root + "/run and pages_sharing are unavailable"}
}

func readFacilityFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", false
	}
	return value, true
}

func isUint(value string) bool {
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}
