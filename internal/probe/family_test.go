package probe

import (
	"testing"

	"ecs/internal/config"
)

func TestIPv6FamilySelection(t *testing.T) {
	if got := endpointFamilies("IPv4|IPv6", true, true, config.IPVersion6); len(got) != 1 || got[0] != "IPv6" {
		t.Fatalf("IPv6 endpoint families = %v", got)
	}
}
