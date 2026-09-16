//go:build windows

package probe

import "testing"

func routeToolMissingTestApplies(*testing.T) bool { return true }

func backtraceToolMissingTestApplies(*testing.T) bool { return true }

func backtraceFixtureTestApplies(t *testing.T) bool {
	t.Helper()
	// The shared producer fixture is a Unix shell script. The Windows-specific
	// trace tests use a real PE test binary instead, so this fixture stays off
	// the Windows path.
	return false
}
