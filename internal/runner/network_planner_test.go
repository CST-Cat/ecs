package runner

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/probe"
)

func TestPlanNetworkSelectsUsableIPv4ForAuto(t *testing.T) {
	plan := planNetwork(config.IPVersionAuto, probe.NetworkCapabilities{IPv4Usable: true})
	if plan.effectiveIPVersion != config.IPVersion4 || !plan.networkRunnable {
		t.Fatalf("planNetwork() = %+v, want runnable IPv4 plan", plan)
	}
}
