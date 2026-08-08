package probe

// Memory inventory helpers shared by the system and memory probes.
//
// Linux exposes the facts needed here through /proc/meminfo and optional
// kernel interfaces.  Optional interfaces are deliberately treated as
// unavailable when their evidence is absent; a balloon driver or KSM is not
// inferred from a generic virtual-machine label.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// memoryUsageSnapshot keeps the host-visible values used by
// the system report and the effective values after a cgroup limit is applied.
// A container can expose host MemTotal while its cgroup limit is much smaller;
// the memory benchmark should size itself against Effective*, not the host
// number.
type memoryUsageSnapshot struct {
	HostTotalBytes     uint64
	HostUsedBytes      uint64
	HostAvailableBytes uint64
	HostUsagePercent   float64

	EffectiveTotalBytes     uint64
	EffectiveUsedBytes      uint64
	EffectiveAvailableBytes uint64
	EffectiveUsagePercent   float64
	AvailableKnown          bool
	LimitApplied            bool
	EffectiveCurrentKnown   bool
}

func memoryUsageFromMemInfo(mem map[string]uint64, limit uint64) memoryUsageSnapshot {
	result := memoryUsageSnapshot{}
	result.HostTotalBytes = mem["MemTotal"] * 1024
	if available, ok := mem["MemAvailable"]; ok && available > 0 {
		result.HostAvailableBytes = available * 1024
		result.AvailableKnown = true
	} else {
		// MemAvailable was added in Linux 3.14.  This fallback mirrors the
		// current ecs behaviour while making the evidence boundary explicit.
		result.HostAvailableBytes = (mem["MemFree"] + mem["Buffers"] + mem["Cached"]) * 1024
	}
	if result.HostTotalBytes > 0 && result.HostAvailableBytes > result.HostTotalBytes {
		result.HostAvailableBytes = result.HostTotalBytes
	}
	if result.HostTotalBytes >= result.HostAvailableBytes {
		result.HostUsedBytes = result.HostTotalBytes - result.HostAvailableBytes
	}
	if result.HostTotalBytes > 0 {
		result.HostUsagePercent = float64(result.HostUsedBytes) / float64(result.HostTotalBytes) * 100
	}

	result.EffectiveTotalBytes = result.HostTotalBytes
	if limit > 0 && (result.EffectiveTotalBytes == 0 || limit < result.EffectiveTotalBytes) {
		result.EffectiveTotalBytes = limit
		result.LimitApplied = true
	}
	result.EffectiveAvailableBytes = result.HostAvailableBytes
	if result.EffectiveAvailableBytes > result.EffectiveTotalBytes && result.EffectiveTotalBytes > 0 {
		result.EffectiveAvailableBytes = result.EffectiveTotalBytes
	}
	if result.EffectiveTotalBytes >= result.EffectiveAvailableBytes {
		result.EffectiveUsedBytes = result.EffectiveTotalBytes - result.EffectiveAvailableBytes
	}
	if result.EffectiveTotalBytes > 0 {
		result.EffectiveUsagePercent = float64(result.EffectiveUsedBytes) / float64(result.EffectiveTotalBytes) * 100
	}
	return result
}

// memoryFacility is the result of an optional-kernel-facility probe.  Evidence
// is a path and a small value summary, never a claim based on virtualization
// detection or a guessed default.
type memoryFacility struct {
	Available bool
	Evidence  string
}

func (f memoryFacility) Status() string {
	if f.Available {
		return "available"
	}
	return "unavailable"
}

// detectBalloonReclaim checks sysfs controls and reclaim-related vmstat
// counters.  A virtio-balloon device alone is insufficient evidence: older
// kernels expose inflate/deflate but no reclaim facility, so that case remains
// unavailable.
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
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "balloon_") {
			continue
		}
		if !isUint(fields[1]) {
			continue
		}
		// inflate/deflate are driver activity counters, not reclaim evidence.
		// Reclaim, migration and deferred pages are the counters that indicate
		// the kernel exposes the relevant reclaim path.
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

// detectKSM requires both the run control and pages_sharing statistic.  The
// facility is available even when run=0 (disabled), and the evidence records
// that state so readers do not confuse availability with current activity.
func detectKSM(sysfsRoot string) memoryFacility {
	root := filepath.Join(sysfsRoot, "kernel", "mm", "ksm")
	run, runOK := readFacilityFile(filepath.Join(root, "run"))
	sharing, sharingOK := readFacilityFile(filepath.Join(root, "pages_sharing"))
	if runOK && sharingOK {
		return memoryFacility{
			Available: true,
			Evidence:  fmt.Sprintf("%s/run=%s; %s/pages_sharing=%s", root, run, root, sharing),
		}
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
