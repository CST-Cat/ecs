package probe

import (
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestRouteSummaryArgumentsAndFailures(t *testing.T) {
	output := `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}],[[]]]}`
	slots, visible, timeouts, ok := routeHopSummary(routeEngineTiny, output)
	if !ok || slots != 2 || visible != 1 || timeouts != 1 {
		t.Fatalf("NextTrace summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
	}
	if _, _, _, ok := routeHopSummary("other", output); ok {
		t.Fatal("unsupported route engine parsed successfully")
	}
	if _, _, _, ok := routeHopSummary(routeEngineTiny, "{}"); ok {
		t.Fatal("empty route output parsed successfully")
	}
	if args := routeCommandArgsForFamily(routeEngine{Name: routeEngineTiny}, "203.0.113.1", routeSnapshotHops, config.IPVersion4); len(args) == 0 || args[0] != "-4" || args[len(args)-1] != "203.0.113.1" {
		t.Fatalf("IPv4 route args = %v", args)
	}
	if args := routeCommandArgsForFamily(routeEngine{Name: routeEngineTiny}, "2001:db8::1", routeSnapshotHops, config.IPVersion6); len(args) == 0 || args[0] != "-6" || args[len(args)-1] != "2001:db8::1" {
		t.Fatalf("IPv6 route args = %v", args)
	}
	if routeCommandArgsForFamily(routeEngine{Name: "other"}, "target", 12, config.IPVersion4) != nil {
		t.Fatal("unsupported route engine produced arguments")
	}
	if clean := sanitizeCommandOutput([]byte("\x1b[31mhop\x1b[0m\x00")); clean != "hop" || strings.ContainsRune(clean, '\x1b') {
		t.Fatalf("sanitized route output = %q", clean)
	}
}
