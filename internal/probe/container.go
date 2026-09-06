package probe

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// 容器与虚拟化下的资源真值。
//
// runtime.NumCPU() 只反映启动时的 CPU affinity mask，不读 cgroup 的 CPU 配额。
// 在 LXC、Docker、Kubernetes 和限核 VPS 上，按 NumCPU 开线程会显著超过实际可用
// 配额，多线程基准成绩既偏低又不稳定。/proc/meminfo 同理：没有 lxcfs 时它显示的
// 是宿主机内存。这里统一读取 cgroup v2 与 v1 的限制，让基准线程数和资源快照都
// 反映容器实际拿到的份额。
//
// 所有读取失败都退回"无限制"，绝不猜测。

const (
	// cgroup v1 用一个接近 int64 上限的值表示"无限制"，各内核版本取值略有差异，
	// 统一按这个量级判定。
	cgroupV1Unlimited = uint64(1) << 62
)

// cpuAllowance 描述本进程实际可用的 CPU 份额。
type cpuAllowance struct {
	// Visible 是 runtime.NumCPU() 看到的逻辑核数。
	Visible int
	// Quota 是 cgroup 配额折算的核数；0 表示没有配额限制。
	Quota float64
	// Threads 是基准应当使用的线程数。
	Threads int
	// Source 说明配额来自哪一层，用于报告披露。
	Source string
}

// Limited 表示 cgroup 配额确实小于可见核数。
func (a cpuAllowance) Limited() bool {
	return a.Quota > 0 && a.Threads < a.Visible
}

// detectCPUAllowance 计算基准应当使用的线程数。
//
// 取 min(可见核数, ceil(cgroup 配额))：向上取整与 Go 运行时对 GOMAXPROCS 的
// 处理一致，配额 2.5 核时用 3 个线程跑满比用 2 个更接近真实上限。
func detectCPUAllowance() cpuAllowance {
	allowance := cpuAllowance{Visible: runtime.NumCPU(), Source: "runtime.NumCPU"}
	if allowance.Visible < 1 {
		allowance.Visible = 1
	}
	allowance.Threads = allowance.Visible

	if quota, source, ok := cgroupCPUQuota(); ok && quota > 0 {
		allowance.Quota = quota
		allowance.Source = source
		threads := int(math.Ceil(quota))
		if threads < 1 {
			threads = 1
		}
		if threads < allowance.Threads {
			allowance.Threads = threads
		}
	}
	// sysbench 对线程数没有硬上限，但超过几百个线程只会放大调度噪声。
	if allowance.Threads > 256 {
		allowance.Threads = 256
	}
	return allowance
}

// distinctBenchmarkThreadCounts is the physical execution plan shared by
// local 1T/NT benchmarks. A one-core allowance has only one distinct context;
// running the same 1-thread command twice adds cost and noise but no scaling
// evidence. Renderers may still expose both logical metric keys for schema and
// scoring compatibility.
func distinctBenchmarkThreadCounts(workers int) []int {
	if workers <= 1 {
		return []int{1}
	}
	return []int{1, workers}
}

// cgroupCPUQuota 返回 cgroup 配额折算的核数。
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

type cgroupMemoryLimitCandidate struct {
	limit uint64
	path  string
	v2    bool
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

// cgroupMemoryLimit returns the strictest finite memory limit visible from the
// process's cgroup.  A cgroup namespace intentionally bounds this walk at the
// namespace's mount root.
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

// cgroupMemoryCurrent returns usage charged to the current cgroup only.  It
// never falls back to a mount root or an ancestor, since those values include
// other cgroups and cannot be used as this process's leaf usage.
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
	// mountinfo uses octal escapes for space, tab, and backslash.
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
		// An ancestor's flag controls whether that ancestor constrains its
		// descendants. Once disabled, higher ancestors are not applicable.
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

// cpuTimeSample 是 /proc/stat 首行聚合 CPU 时间的一次采样。
type cpuTimeSample struct {
	Total uint64
	Steal uint64
}

// readCPUTimes 读取 /proc/stat 的聚合 CPU 行。
//
// 字段顺序为 user nice system idle iowait irq softirq steal guest guest_nice，
// steal 是索引 7。内核较旧时可能没有 steal 列，此时按 0 处理并照常返回。
func readCPUTimes() (cpuTimeSample, bool) {
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

// stealPercent 计算两次采样之间被虚拟化层偷走的 CPU 时间占比。
func stealPercent(before, after cpuTimeSample) (float64, bool) {
	if after.Total <= before.Total {
		return 0, false
	}
	totalDelta := after.Total - before.Total
	stealDelta := after.Steal - before.Steal
	if after.Steal < before.Steal {
		return 0, false
	}
	return float64(stealDelta) / float64(totalDelta) * 100, true
}

// cumulativeStealPercent 给出自开机以来的 steal 占比。
//
// 累计值比短窗口采样更能反映长期超售程度，短窗口只说明测试当刻的争抢情况。
func cumulativeStealPercent(sample cpuTimeSample) (float64, bool) {
	if sample.Total == 0 {
		return 0, false
	}
	return float64(sample.Steal) / float64(sample.Total) * 100, true
}
