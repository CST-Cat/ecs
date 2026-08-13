package probe

import (
	"bufio"
	"math"
	"os"
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
	cgroupV2Root = "/sys/fs/cgroup"
	cgroupV1CPU  = "/sys/fs/cgroup/cpu"
	cgroupV1Mem  = "/sys/fs/cgroup/memory"

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

// describeCPUAllowance 给出可读的可用 CPU 描述，用于报告字段。
func describeCPUAllowance(a cpuAllowance) string {
	if !a.Limited() {
		return strconv.Itoa(a.Visible) + " 逻辑核（无 cgroup 配额限制）"
	}
	return strconv.FormatFloat(a.Quota, 'f', 2, 64) + " 核配额 / " +
		strconv.Itoa(a.Visible) + " 逻辑核可见（" + a.Source + "）"
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
	if quota, ok := cgroupV2CPUQuota(); ok {
		return quota, "cgroup v2 cpu.max", true
	}
	if quota, ok := cgroupV1CPUQuota(); ok {
		return quota, "cgroup v1 cpu.cfs_quota_us", true
	}
	return 0, "", false
}

// cgroupV2CPUQuota 解析 cpu.max，格式为 "<quota|max> <period>"。
func cgroupV2CPUQuota() (float64, bool) {
	for _, path := range cgroupCandidatePaths(cgroupV2Root, "cpu.max") {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) != 2 || fields[0] == "max" {
			continue
		}
		quota, quotaErr := strconv.ParseFloat(fields[0], 64)
		period, periodErr := strconv.ParseFloat(fields[1], 64)
		if quotaErr != nil || periodErr != nil || quota <= 0 || period <= 0 {
			continue
		}
		return quota / period, true
	}
	return 0, false
}

// cgroupV1CPUQuota 读取 cpu.cfs_quota_us 与 cpu.cfs_period_us，配额为 -1 表示无限制。
func cgroupV1CPUQuota() (float64, bool) {
	for _, base := range []string{cgroupV1CPU, cgroupV2Root} {
		for _, quotaPath := range cgroupCandidatePaths(base, "cpu.cfs_quota_us") {
			quotaText := strings.TrimSpace(readTrimmed(quotaPath, ""))
			if quotaText == "" || strings.HasPrefix(quotaText, "-") {
				continue
			}
			quota, err := strconv.ParseFloat(quotaText, 64)
			if err != nil || quota <= 0 {
				continue
			}
			periodPath := strings.TrimSuffix(quotaPath, "cpu.cfs_quota_us") + "cpu.cfs_period_us"
			period, periodErr := strconv.ParseFloat(readTrimmed(periodPath, ""), 64)
			if periodErr != nil || period <= 0 {
				period = 100000
			}
			return quota / period, true
		}
	}
	return 0, false
}

// cgroupMemoryLimit 返回 cgroup 内存上限；0 表示没有限制或无法读取。
func cgroupMemoryLimit() (uint64, string, bool) {
	for _, candidate := range []struct {
		base string
		file string
		via  string
	}{
		{cgroupV2Root, "memory.max", "cgroup v2 memory.max"},
		{cgroupV1Mem, "memory.limit_in_bytes", "cgroup v1 memory.limit_in_bytes"},
		{cgroupV2Root, "memory.limit_in_bytes", "cgroup v1 memory.limit_in_bytes"},
	} {
		for _, path := range cgroupCandidatePaths(candidate.base, candidate.file) {
			text := strings.TrimSpace(readTrimmed(path, ""))
			if text == "" || text == "max" {
				continue
			}
			value, err := strconv.ParseUint(text, 10, 64)
			if err != nil || value == 0 || value >= cgroupV1Unlimited {
				continue
			}
			return value, candidate.via, true
		}
	}
	return 0, "", false
}

// cgroupMemoryCurrent returns the bytes currently charged to this cgroup.
// When /proc/meminfo exposes host memory, this lets the memory inventory report
// a real effective used/available split instead of subtracting host
// MemAvailable from a smaller container limit.
func cgroupMemoryCurrent() (uint64, string, bool) {
	for _, candidate := range []struct {
		base string
		file string
		via  string
	}{
		{cgroupV2Root, "memory.current", "cgroup v2 memory.current"},
		{cgroupV1Mem, "memory.usage_in_bytes", "cgroup v1 memory.usage_in_bytes"},
		{cgroupV2Root, "memory.usage_in_bytes", "cgroup v1 memory.usage_in_bytes"},
	} {
		for _, path := range cgroupCandidatePaths(candidate.base, candidate.file) {
			text := strings.TrimSpace(readTrimmed(path, ""))
			if text == "" {
				continue
			}
			value, err := strconv.ParseUint(text, 10, 64)
			if err != nil {
				continue
			}
			return value, candidate.via, true
		}
	}
	return 0, "", false
}

// cgroupCandidatePaths 给出一个 cgroup 控制文件的候选位置。
//
// 启用了 cgroup namespace 的容器里，挂载点根部就是该容器自身的 cgroup，直接路径
// 即可命中。没有 namespace 时（部分 LXC、宿主机上的 Docker）需要按 /proc/self/cgroup
// 里的相对路径拼接。
func cgroupCandidatePaths(base, file string) []string {
	paths := []string{base + "/" + file}
	for _, relative := range selfCgroupPaths() {
		if relative == "" || relative == "/" {
			continue
		}
		paths = append(paths, base+relative+"/"+file)
	}
	return paths
}

// selfCgroupPaths 解析 /proc/self/cgroup 中本进程所属的 cgroup 相对路径。
func selfCgroupPaths() []string {
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return nil
	}
	defer file.Close()
	seen := make(map[string]bool)
	var paths []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// 格式为 hierarchy-ID:controller-list:cgroup-path
		parts := strings.SplitN(scanner.Text(), ":", 3)
		if len(parts) != 3 {
			continue
		}
		path := strings.TrimSpace(parts[2])
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

// cpuTimeSample 是 /proc/stat 首行聚合 CPU 时间的一次采样。
type cpuTimeSample struct {
	Total uint64
	Steal uint64
	Idle  uint64
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
			switch index {
			case 3:
				sample.Idle = value
			case 7:
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
