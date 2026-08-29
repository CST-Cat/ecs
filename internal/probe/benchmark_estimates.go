package probe

import (
	"fmt"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

// EstimateFor owns the complete user-facing runtime estimate.  Keeping the
// orchestration with the probe workload plans means allowance-sensitive
// benchmarks and the fio plan cannot drift from the work that actually runs.
func EstimateFor(runtime config.Runtime) config.Estimate {
	workers := detectCPUAllowance().Threads
	estimate := config.Estimate{DiskMiB: runtime.DiskMiB}
	if hasModule(runtime, "speed") {
		estimate.NetworkMiB = -1
		estimate.Notes = append(estimate.Notes,
			fmt.Sprintf(i18n.T("estimate.speed"),
				len(runtime.IPerfTargets), runtime.IPerfDuration, runtime.SpeedThreads))
	}
	if !hasModule(runtime, "disk") {
		estimate.DiskMiB = 0
	}
	typical := estimateTypicalDuration(runtime, workers)
	estimate.DurationText = durationEstimateText(typical*3/5, typical*2)
	if runtime.OfflineOnly() {
		estimate.NetworkMiB = 0
		estimate.Notes = append(estimate.Notes, i18n.T("estimate.offline"))
	}
	if hasModule(runtime, "route") {
		estimate.Notes = append(estimate.Notes, i18n.T("estimate.route"))
	}
	return estimate
}

func hasModule(runtime config.Runtime, id string) bool {
	for _, module := range runtime.Modules {
		if module == id {
			return true
		}
	}
	return false
}

func estimateTypicalDuration(runtime config.Runtime, workers int) time.Duration {
	var total time.Duration
	for _, module := range runtime.Modules {
		descriptor, ok := config.ModuleDescriptorFor(module)
		if !ok {
			continue
		}
		if runtime.OfflineOnly() && descriptor.Exposure > config.ExposureLocal {
			total += 100 * time.Millisecond
			continue
		}
		total += estimateModuleDuration(runtime, descriptor, workers)
	}
	if total < 5*time.Second {
		return 5 * time.Second
	}
	return total
}

func estimateModuleDuration(runtime config.Runtime, descriptor config.ModuleDescriptor, workers int) time.Duration {
	switch descriptor.EstimateMode {
	case config.EstimateModeCPU:
		return cpuBenchmarkEstimate(runtime, workers)
	case config.EstimateModeMemory:
		return streamBenchmarkEstimate(runtime, workers)
	case config.EstimateModeDisk:
		// Add startup and --enghelp discovery around the complete fio plan.
		return FIOPlanDuration() + 10*time.Second
	case config.EstimateModeDNS:
		return time.Duration(runtime.DNSAttempts) * time.Second
	case config.EstimateModeLatency:
		return time.Duration(runtime.LatencyAttempts) * 1500 * time.Millisecond
	case config.EstimateModeSpeed:
		// Each target/family row runs forward, reverse, and one UDP sample.
		families := 1
		if runtime.IPVersion == "" || runtime.IPVersion == config.IPVersionAuto {
			families = 2
		}
		perRow := 2*runtime.IPerfDuration + config.IPerfUDPDuration
		return time.Duration(len(runtime.IPerfTargets)*families) * perRow
	case config.EstimateModeRoute:
		return time.Duration(len(runtime.RouteTargets)) * 12 * time.Second
	case config.EstimateModeTwoContext:
		return twoContextBenchmarkEstimate(descriptor.Estimate, workers)
	case config.EstimateModeFixed:
		return descriptor.Estimate
	default:
		// ValidateModuleDescriptors rejects unknown modes. Treat a descriptor
		// assembled by an external caller defensively as a fixed estimate.
		return descriptor.Estimate
	}
}

func durationEstimateText(lower, upper time.Duration) string {
	if lower < 5*time.Second {
		lower = 5 * time.Second
	}
	if upper < lower {
		upper = lower
	}
	if upper < time.Minute {
		lowerSeconds := int((lower.Seconds()+4)/5) * 5
		upperSeconds := int((upper.Seconds()+4)/5) * 5
		return fmt.Sprintf(i18n.T("estimate.seconds"), lowerSeconds, upperSeconds)
	}
	lowerMinutes := int((lower + time.Minute - 1) / time.Minute)
	upperMinutes := int((upper + time.Minute - 1) / time.Minute)
	if lowerMinutes < 1 {
		lowerMinutes = 1
	}
	if upperMinutes <= lowerMinutes {
		upperMinutes = lowerMinutes + 1
	}
	return fmt.Sprintf(i18n.T("estimate.minutes"), lowerMinutes, upperMinutes)
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
