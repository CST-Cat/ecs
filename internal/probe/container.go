package probe

import (
	"math"
	"runtime"
)

// cpuAllowance describes the execution share visible to local benchmarks.
// Linux may refine it with cgroup quota; FreeBSD uses the native runtime CPU
// count because cgroups do not exist there.
type cpuAllowance struct {
	Visible int
	Quota   float64
	Threads int
	Source  string
}

func (a cpuAllowance) Limited() bool {
	return a.Quota > 0 && a.Threads < a.Visible
}

// detectCPUAllowance computes the benchmark thread count from the platform
// quota boundary. All platform-specific quota reads live in *_linux.go or
// *_freebsd.go files.
func detectCPUAllowance() cpuAllowance {
	allowance := cpuAllowance{Visible: runtime.NumCPU(), Source: "runtime.NumCPU"}
	if allowance.Visible < 1 {
		allowance.Visible = 1
	}
	allowance.Threads = allowance.Visible

	if quota, source, ok := platformCPUQuota(); ok && quota > 0 {
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
	if allowance.Threads > 256 {
		allowance.Threads = 256
	}
	return allowance
}

// distinctBenchmarkThreadCounts is the physical execution plan shared by
// local 1T/NT benchmarks. A one-core allowance has only one distinct context;
// renderers may still expose both logical metric keys for schema compatibility.
func distinctBenchmarkThreadCounts(workers int) []int {
	if workers <= 1 {
		return []int{1}
	}
	return []int{1, workers}
}

// cgroupMemoryLimitCandidate is kept in the shared type layer because the
// memory inventory's arithmetic and tests use it, while Linux is the only
// platform that can populate candidates from a live cgroup hierarchy.
type cgroupMemoryLimitCandidate struct {
	limit uint64
	path  string
	v2    bool
}

// cpuTimeSample is a platform-neutral value used by the pressure arithmetic.
// Linux populates it from /proc/stat; FreeBSD returns an unavailable sample.
type cpuTimeSample struct {
	Total uint64
	Steal uint64
}

func stealPercent(before, after cpuTimeSample) (float64, bool) {
	if after.Total <= before.Total || after.Steal < before.Steal {
		return 0, false
	}
	return float64(after.Steal-before.Steal) / float64(after.Total-before.Total) * 100, true
}

func cumulativeStealPercent(sample cpuTimeSample) (float64, bool) {
	if sample.Total == 0 {
		return 0, false
	}
	return float64(sample.Steal) / float64(sample.Total) * 100, true
}
