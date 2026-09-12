//go:build freebsd

package probe

import "testing"

func routeToolMissingTestApplies(t *testing.T) bool {
	t.Helper()
	// FreeBSD route deliberately does not use the private benchmark staging
	// directory. Its deterministic no-target producer test exercises the
	// comparison-parameter contract; backend availability is covered by the
	// dedicated fixed-path unit/integration tests.
	return false
}

func backtraceToolMissingTestApplies(t *testing.T) bool {
	t.Helper()
	// FreeBSD backtrace uses the fixed base-system traceroute paths, so an
	// empty staged tool directory cannot simulate a missing backend.
	return false
}

func backtraceFixtureTestApplies(t *testing.T) bool {
	t.Helper()
	// The producer fixture is a fake NextTrace executable. FreeBSD production
	// backtrace is intentionally fixed to the base-system traceroute backend;
	// its real execution is covered by the integration-tagged test instead.
	return false
}
