package probe

import (
	"context"
	"testing"

	"ecs/internal/config"
)

func TestEgressFixturePropagatesIPAndIntel(t *testing.T) {
	egress := Egress{
		Attempted: true,
		ByVersion: map[string]EgressAddress{
			config.IPVersion4: {
				Version:  config.IPVersion4,
				IP:       "203.0.113.9",
				Source:   "ipapi.is",
				Intel:    ipAPIResponse{IP: "203.0.113.9"},
				HasIntel: true,
			},
		},
	}
	if ip, err := egress.IPFor(config.IPVersion4); err != nil || ip != "203.0.113.9" {
		t.Fatalf("IPFor = %q, %v", ip, err)
	}
	intel, ok := egress.IntelFor(config.IPVersion4)
	if !ok || intel.IP != "203.0.113.9" {
		t.Fatalf("IntelFor = %+v, %v", intel, ok)
	}
}

func TestDiscoverEgressFailsWithoutSTUNServers(t *testing.T) {
	address := discoverEgressForVersion(context.Background(), Environment{}, config.IPVersion4, false)
	if address.Source != "stun" || address.Err == nil {
		t.Fatalf("STUN failure = %+v", address)
	}
}
