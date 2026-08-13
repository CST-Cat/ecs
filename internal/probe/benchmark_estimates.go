package probe

import (
	"time"

	"ecs/internal/config"
)

// The descriptor defaults describe the ordinary two-context plan. Register
// allowance-aware estimates here (config cannot import probe) so a one-core
// host is not told to expect work that is deliberately no longer executed.
func init() {
	_ = config.RegisterModuleEstimate("cpu", func(runtime config.Runtime) time.Duration {
		return cpuBenchmarkEstimate(runtime, detectCPUAllowance().Threads)
	})
	_ = config.RegisterModuleEstimate("zstd", func(config.Runtime) time.Duration {
		return twoContextBenchmarkEstimate(25*time.Second, detectCPUAllowance().Threads)
	})
	_ = config.RegisterModuleEstimate("npb", func(config.Runtime) time.Duration {
		return twoContextBenchmarkEstimate(60*time.Second, detectCPUAllowance().Threads)
	})
	_ = config.RegisterModuleEstimate("memory", func(runtime config.Runtime) time.Duration {
		return streamBenchmarkEstimate(runtime, detectCPUAllowance().Threads)
	})
	_ = config.RegisterModuleEstimate("crypto", func(config.Runtime) time.Duration {
		return twoContextBenchmarkEstimate(45*time.Second, detectCPUAllowance().Threads)
	})
}

func cpuBenchmarkEstimate(runtime config.Runtime, workers int) time.Duration {
	return time.Duration(len(distinctBenchmarkThreadCounts(workers)))*runtime.CPUTime + time.Second
}

func streamBenchmarkEstimate(runtime config.Runtime, workers int) time.Duration {
	return time.Duration(len(distinctBenchmarkThreadCounts(workers)))*2*runtime.CPUTime + time.Second
}

func twoContextBenchmarkEstimate(ordinary time.Duration, workers int) time.Duration {
	if len(distinctBenchmarkThreadCounts(workers)) == 1 {
		return ordinary / 2
	}
	return ordinary
}
