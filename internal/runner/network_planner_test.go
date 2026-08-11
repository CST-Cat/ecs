package runner

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/probe"
)

func TestPlanNetworkCoversRequestedFamiliesAndCapabilities(t *testing.T) {
	cases := []struct {
		name         string
		requested    string
		capabilities probe.NetworkCapabilities
		effective    string
		runnable     bool
	}{
		{name: "auto no capability", requested: config.IPVersionAuto, capabilities: probe.NetworkCapabilities{}, effective: config.IPVersionAuto},
		{name: "auto IPv4 only", requested: config.IPVersionAuto, capabilities: probe.NetworkCapabilities{IPv4Usable: true}, effective: config.IPVersion4, runnable: true},
		{name: "auto IPv6 only", requested: config.IPVersionAuto, capabilities: probe.NetworkCapabilities{IPv6Usable: true}, effective: config.IPVersion6, runnable: true},
		{name: "auto dual stack", requested: config.IPVersionAuto, capabilities: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, effective: config.IPVersionAuto, runnable: true},
		{name: "explicit IPv4 no capability", requested: config.IPVersion4, capabilities: probe.NetworkCapabilities{}, effective: config.IPVersion4},
		{name: "explicit IPv4 IPv4 only", requested: config.IPVersion4, capabilities: probe.NetworkCapabilities{IPv4Usable: true}, effective: config.IPVersion4, runnable: true},
		{name: "explicit IPv4 IPv6 only", requested: config.IPVersion4, capabilities: probe.NetworkCapabilities{IPv6Usable: true}, effective: config.IPVersion4},
		{name: "explicit IPv4 dual stack", requested: config.IPVersion4, capabilities: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, effective: config.IPVersion4, runnable: true},
		{name: "explicit IPv6 no capability", requested: config.IPVersion6, capabilities: probe.NetworkCapabilities{}, effective: config.IPVersion6},
		{name: "explicit IPv6 IPv4 only", requested: config.IPVersion6, capabilities: probe.NetworkCapabilities{IPv4Usable: true}, effective: config.IPVersion6},
		{name: "explicit IPv6 IPv6 only", requested: config.IPVersion6, capabilities: probe.NetworkCapabilities{IPv6Usable: true}, effective: config.IPVersion6, runnable: true},
		{name: "explicit IPv6 dual stack", requested: config.IPVersion6, capabilities: probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}, effective: config.IPVersion6, runnable: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := planNetwork(testCase.requested, testCase.capabilities)
			if got.effectiveIPVersion != testCase.effective || got.networkRunnable != testCase.runnable {
				t.Fatalf("planNetwork(%q, %+v) = %+v, want effective=%q runnable=%v", testCase.requested, testCase.capabilities, got, testCase.effective, testCase.runnable)
			}
		})
	}
}

func TestPlanNetworkDoesNotInventIPVersionNone(t *testing.T) {
	for _, requested := range []string{config.IPVersionAuto, config.IPVersion4, config.IPVersion6} {
		plan := planNetwork(requested, probe.NetworkCapabilities{})
		if plan.effectiveIPVersion == "none" || plan.effectiveIPVersion == "0" {
			t.Fatalf("planNetwork(%q) invented a sentinel family: %+v", requested, plan)
		}
	}
}
