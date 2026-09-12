//go:build linux

package probe

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cgroupV1Unlimited = uint64(1) << 62

func platformCPUQuota() (float64, string, bool) { return cgroupCPUQuota() }

func readCPUTimes() (cpuTimeSample, bool) { return readLinuxCPUTimes() }

func cgroupCPUQuota() (float64, string, bool) {
	var best float64
	var source string
	for _, candidate := range cgroupLimitCandidates("cpu", "cpu.max", "cpu.cfs_quota_us") {
		if candidate.v2 {
			fields := strings.Fields(readTrimmed(candidate.path, ""))
			if len(fields) != 2 || fields[0] == "max" {
				continue
			}
			quota, err1 := strconv.ParseFloat(fields[0], 64)
			period, err2 := strconv.ParseFloat(fields[1], 64)
			if err1 != nil || err2 != nil || quota <= 0 || period <= 0 {
				continue
			}
			value := quota / period
			if best == 0 || value < best {
				best, source = value, candidate.path
			}
			continue
		}
		quotaText := strings.TrimSpace(readTrimmed(candidate.path, ""))
		if quotaText == "" || strings.HasPrefix(quotaText, "-") {
			continue
		}
		quota, err := strconv.ParseFloat(quotaText, 64)
		if err != nil || quota <= 0 {
			continue
		}
		periodPath := filepath.Join(filepath.Dir(candidate.path), "cpu.cfs_period_us")
		period, err := strconv.ParseFloat(strings.TrimSpace(readTrimmed(periodPath, "")), 64)
		if err != nil || period <= 0 {
			continue
		}
		value := quota / period
		if best == 0 || value < best {
			best, source = value, candidate.path
		}
	}
	return best, source, best > 0
}

func cgroupMemoryLimitCandidates() []cgroupMemoryLimitCandidate {
	var result []cgroupMemoryLimitCandidate
	for _, candidate := range cgroupLimitCandidates("memory", "memory.max", "memory.limit_in_bytes") {
		value, ok := parseCgroupLimit(candidate.path, candidate.v2)
		if ok {
			result = append(result, cgroupMemoryLimitCandidate{limit: value, path: candidate.path, v2: candidate.v2})
		}
	}
	return result
}

func cgroupMemoryLimit() (uint64, string, bool) {
	var best uint64
	var source string
	known := false
	for _, candidate := range cgroupMemoryLimitCandidates() {
		if !known || candidate.limit < best {
			best, source, known = candidate.limit, candidate.path, true
		}
	}
	return best, source, known && best > 0
}

func parseCgroupLimit(path string, v2 bool) (uint64, bool) {
	text := strings.TrimSpace(readTrimmed(path, ""))
	if text == "" || text == "max" {
		return 0, false
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || value == 0 || (!v2 && value >= cgroupV1Unlimited) {
		return 0, false
	}
	return value, true
}

func cgroupMemoryCurrent() (uint64, string, bool) {
	for _, candidate := range cgroupCurrentCandidates("memory", "memory.current", "memory.usage_in_bytes") {
		text := strings.TrimSpace(readTrimmed(candidate.path, ""))
		value, err := strconv.ParseUint(text, 10, 64)
		if err == nil {
			return value, candidate.path, true
		}
	}
	return 0, "", false
}

type cgroupMembership struct {
	hierarchyID string
	controllers map[string]bool
	path        string
}

type cgroupMount struct {
	root, mountPoint, fsType string
	controllers              map[string]bool
}

type cgroupFileCandidate struct {
	path string
	v2   bool
}

var (
	cgroupSelfPath      = "/proc/self/cgroup"
	cgroupMountInfoPath = "/proc/self/mountinfo"
)

func readCgroupMemberships(path string) []cgroupMembership {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var result []cgroupMembership
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		controllers := make(map[string]bool)
		for _, controller := range strings.Split(parts[1], ",") {
			if controller != "" {
				controllers[controller] = true
			}
		}
		result = append(result, cgroupMembership{hierarchyID: parts[0], controllers: controllers, path: parts[2]})
	}
	return result
}

func unescapeMountInfo(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+3 < len(value) {
			if n, err := strconv.ParseUint(value[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(value[i])
	}
	return b.String()
}

func readCgroupMounts(path string) []cgroupMount {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var result []cgroupMount
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || separator+3 >= len(fields) {
			continue
		}
		fsType := fields[separator+1]
		if fsType != "cgroup" && fsType != "cgroup2" {
			continue
		}
		mount := cgroupMount{root: unescapeMountInfo(fields[3]), mountPoint: unescapeMountInfo(fields[4]), fsType: fsType, controllers: map[string]bool{}}
		if fsType == "cgroup" {
			for _, options := range []string{fields[5], fields[separator+3]} {
				for _, option := range strings.Split(options, ",") {
					mount.controllers[option] = true
				}
			}
		}
		result = append(result, mount)
	}
	return result
}

func mountMatchesController(mount cgroupMount, controller string) bool {
	return mount.fsType == "cgroup2" || mount.controllers[controller]
}

func validCgroupPath(value string) bool {
	if value == "" || !filepath.IsAbs(value) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(value), "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func mapCgroupMember(mount cgroupMount, memberPath string) string {
	if !validCgroupPath(mount.root) || !validCgroupPath(mount.mountPoint) || !validCgroupPath(memberPath) {
		return ""
	}
	root := filepath.Clean(mount.root)
	member := filepath.Clean(memberPath)
	if root != "/" && member != root && !strings.HasPrefix(member, root+string(filepath.Separator)) {
		return ""
	}
	if root != "/" {
		member = strings.TrimPrefix(member, root)
	}
	return filepath.Join(mount.mountPoint, member)
}

func visibleCgroupPaths(mount cgroupMount, memberPath string) []string {
	leaf := mapCgroupMember(mount, memberPath)
	if leaf == "" {
		return nil
	}
	mountPoint := filepath.Clean(mount.mountPoint)
	if leaf != mountPoint && !strings.HasPrefix(leaf, mountPoint+string(filepath.Separator)) {
		return nil
	}
	var result []string
	for current := filepath.Clean(leaf); ; current = filepath.Dir(current) {
		result = append(result, current)
		if current == mountPoint {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
	return result
}

type cgroupPathSet struct {
	paths []string
	v2    bool
}

func resolveCgroupPathSets(controller string) []cgroupPathSet {
	members := readCgroupMemberships(cgroupSelfPath)
	mounts := readCgroupMounts(cgroupMountInfoPath)
	var result []cgroupPathSet
	for _, mount := range mounts {
		if !mountMatchesController(mount, controller) {
			continue
		}
		for _, member := range members {
			if mount.fsType == "cgroup2" {
				if member.hierarchyID != "0" || len(member.controllers) != 0 {
					continue
				}
			} else if !member.controllers[controller] {
				continue
			}
			paths := visibleCgroupPaths(mount, member.path)
			if len(paths) == 0 {
				continue
			}
			result = append(result, cgroupPathSet{paths: paths, v2: mount.fsType == "cgroup2"})
		}
	}
	return result
}

func v1MemoryLimitPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	result := []string{paths[0]}
	for i := 1; i < len(paths); i++ {
		if strings.TrimSpace(readTrimmed(filepath.Join(paths[i], "memory.use_hierarchy"), "")) != "1" {
			break
		}
		result = append(result, paths[i])
	}
	return result
}

func cgroupLimitCandidates(controller, v2File, v1File string) []cgroupFileCandidate {
	var result []cgroupFileCandidate
	for _, set := range resolveCgroupPathSets(controller) {
		paths := set.paths
		if !set.v2 && controller == "memory" {
			paths = v1MemoryLimitPaths(paths)
		}
		for _, path := range paths {
			if set.v2 && v2File != "" {
				result = append(result, cgroupFileCandidate{path: filepath.Join(path, v2File), v2: true})
			}
			if !set.v2 && v1File != "" {
				result = append(result, cgroupFileCandidate{path: filepath.Join(path, v1File), v2: false})
			}
		}
	}
	return result
}

func cgroupCurrentCandidates(controller, v2File, v1File string) []cgroupFileCandidate {
	var result []cgroupFileCandidate
	for _, set := range resolveCgroupPathSets(controller) {
		path := set.paths[0]
		if set.v2 && v2File != "" {
			result = append(result, cgroupFileCandidate{path: filepath.Join(path, v2File), v2: true})
		}
		if !set.v2 && v1File != "" {
			result = append(result, cgroupFileCandidate{path: filepath.Join(path, v1File), v2: false})
		}
	}
	return result
}

func readLinuxCPUTimes() (cpuTimeSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimeSample{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var sample cpuTimeSample
		for index, field := range fields[1:] {
			value, parseErr := strconv.ParseUint(field, 10, 64)
			if parseErr != nil {
				continue
			}
			sample.Total += value
			if index == 7 {
				sample.Steal = value
			}
		}
		if sample.Total == 0 {
			return cpuTimeSample{}, false
		}
		return sample, true
	}
	return cpuTimeSample{}, false
}
