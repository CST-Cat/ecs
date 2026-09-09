//go:build linux

package probe

import "testing"

func routeToolMissingTestApplies(*testing.T) bool { return true }

func backtraceToolMissingTestApplies(*testing.T) bool { return true }

func backtraceFixtureTestApplies(*testing.T) bool { return true }
