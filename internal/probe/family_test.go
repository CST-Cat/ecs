package probe

import (
	"testing"

	"ecs/internal/config"
)

func TestIPv6FamilySelectionAddsCommandFamily(t *testing.T) {
	if got := endpointFamilies("IPv4|IPv6", true, true, config.IPVersion6); len(got) != 1 || got[0] != "IPv6" {
		t.Fatalf("IPv6 endpoint families = %v", got)
	}
	args := routeCommandArgsForFamily(routeEngine{Name: routeEngineTiny}, "2001:db8::1", 5, config.IPVersion6)
	if len(args) == 0 || args[0] != "-6" {
		t.Fatalf("IPv6 route command family = %v", args)
	}
}

func TestExplicitEndpointFamilyPropagatesToProbeSelection(t *testing.T) {
	endpoint := config.Endpoint{Name: "v6", Address: "resolver.example:443", Family: config.IPVersion6}
	if got := endpointFamily(endpoint, config.IPVersionAuto); got != config.IPVersion6 {
		t.Fatalf("endpoint family = %q", got)
	}
	families := latencyFamiliesForEndpoint(endpoint, config.IPVersionAuto, true, true)
	if len(families) != 1 || families[0] != config.IPVersion6 {
		t.Fatalf("latency families = %v", families)
	}
}
