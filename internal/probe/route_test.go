package probe

import "testing"

func TestNextTraceOutputParsesVisibleRouteHop(t *testing.T) {
	output := `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}]]}`
	hops := extractTraceHops(routeEngineTiny, output)
	if len(hops) != 1 || hops[0] != "203.0.113.1" {
		t.Fatalf("NextTrace hops = %v", hops)
	}
	if got := routeHopCount(routeEngineTiny, output); got != 1 {
		t.Fatalf("visible route hops = %d", got)
	}
}
