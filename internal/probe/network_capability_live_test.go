//go:build live

package probe

import (
	"os"
	"testing"
	"time"
)

// This live check is deliberately opt-in even under the live build tag: it
// reads only local kernel state, but the four capability environments cannot
// be synthesized safely inside a running host without changing its network.
func TestLiveNetworkCapabilities(t *testing.T) {
	if os.Getenv("ECS_LIVE_NETWORK_CAPABILITY") != "1" {
		t.Skip("set ECS_LIVE_NETWORK_CAPABILITY=1 to inspect the current host locally")
	}
	start := time.Now()
	capabilities := DetectNetworkCapabilities()
	t.Logf("local network capabilities: %+v (detector=%s)", capabilities, time.Since(start).Round(time.Microsecond))
}
