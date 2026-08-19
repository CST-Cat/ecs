package runner

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/probe"
)

func TestPlanNetworkCapabilityClasses(t *testing.T) {
	cases := []struct {
		name       string
		requested  string
		capability probe.NetworkCapabilities
		wantFamily string
		wantRun    bool
	}{
		{name: "empty dual", capability: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, wantFamily: config.IPVersionAuto, wantRun: true},
		{name: "auto v4 only", requested: config.IPVersionAuto, capability: probe.NetworkCapabilities{IPv4Usable: true}, wantFamily: config.IPVersion4, wantRun: true},
		{name: "auto v6 only", requested: config.IPVersionAuto, capability: probe.NetworkCapabilities{IPv6Usable: true}, wantFamily: config.IPVersion6, wantRun: true},
		{name: "auto none", requested: config.IPVersionAuto, wantFamily: config.IPVersionAuto},
		{name: "explicit v4 available", requested: config.IPVersion4, capability: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, wantFamily: config.IPVersion4, wantRun: true},
		{name: "explicit v4 unavailable", requested: config.IPVersion4, capability: probe.NetworkCapabilities{IPv6Usable: true}, wantFamily: config.IPVersion4},
		{name: "explicit v6 available", requested: config.IPVersion6, capability: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, wantFamily: config.IPVersion6, wantRun: true},
		{name: "explicit v6 unavailable", requested: config.IPVersion6, capability: probe.NetworkCapabilities{IPv4Usable: true}, wantFamily: config.IPVersion6},
		{name: "unknown stays narrow", requested: "v9", capability: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, wantFamily: "v9"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := planNetwork(test.requested, test.capability)
			if got.effectiveIPVersion != test.wantFamily || got.networkRunnable != test.wantRun {
				t.Fatalf("plan = %+v, want family=%q runnable=%v", got, test.wantFamily, test.wantRun)
			}
		})
	}
}
