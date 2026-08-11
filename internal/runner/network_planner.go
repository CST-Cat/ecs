package runner

import (
	"strings"

	"ecs/internal/config"
	"ecs/internal/probe"
)

type networkPlan struct {
	effectiveIPVersion string
	networkRunnable    bool
}

// planNetwork converts the raw user request into the family that network
// probes may use in this run. Auto narrows only when the host is single-stack;
// dual-stack remains auto so probes may keep separate IPv4 and IPv6 rows. An
// unavailable explicit family is never replaced by the other family.
func planNetwork(requested string, capabilities probe.NetworkCapabilities) networkPlan {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		requested = config.IPVersionAuto
	}
	switch requested {
	case config.IPVersion4:
		return networkPlan{effectiveIPVersion: config.IPVersion4, networkRunnable: capabilities.IPv4Usable}
	case config.IPVersion6:
		return networkPlan{effectiveIPVersion: config.IPVersion6, networkRunnable: capabilities.IPv6Usable}
	case config.IPVersionAuto:
		switch {
		case capabilities.IsDualStack():
			return networkPlan{effectiveIPVersion: config.IPVersionAuto, networkRunnable: true}
		case capabilities.IPv4Usable:
			return networkPlan{effectiveIPVersion: config.IPVersion4, networkRunnable: true}
		case capabilities.IPv6Usable:
			return networkPlan{effectiveIPVersion: config.IPVersion6, networkRunnable: true}
		default:
			return networkPlan{effectiveIPVersion: config.IPVersionAuto, networkRunnable: false}
		}
	default:
		// Config validation rejects unknown values before runner.Run. Preserve
		// one if a direct caller bypasses validation; never silently broaden it.
		return networkPlan{effectiveIPVersion: requested, networkRunnable: false}
	}
}
