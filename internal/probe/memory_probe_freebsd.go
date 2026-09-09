//go:build freebsd

package probe

import "context"

func platformUsesMemoryAvailableFallback() bool { return false }

func collectPlatformMemoryUsageSnapshot() memoryUsageSnapshot {
	return freeBSDMemoryUsage(freeBSDSystemValues(context.Background()))
}

func collectPlatformMemoryFacilities() (memoryFacility, memoryFacility) {
	return memoryFacility{Evidence: "unavailable on FreeBSD: no Linux balloon interface"}, memoryFacility{Evidence: "unavailable on FreeBSD: no Linux KSM interface"}
}
