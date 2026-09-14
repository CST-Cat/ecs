//go:build windows

package probe

import "testing"

func routeToolMissingTestApplies(t *testing.T) bool {
	t.Helper()
	// Native Windows route remains platform-unsupported until the NextTrace
	// Windows gate passes; a missing staged executable is not its semantics.
	return false
}

func backtraceToolMissingTestApplies(t *testing.T) bool {
	t.Helper()
	// Native Windows backtrace remains platform-unsupported until the NextTrace
	// Windows gate passes; a missing staged executable is not its semantics.
	return false
}

func backtraceFixtureTestApplies(t *testing.T) bool {
	t.Helper()
	// The Linux fixture supplies a fake NextTrace executable and is not a
	// Windows runtime substitute while the native backend is unsupported.
	return false
}
