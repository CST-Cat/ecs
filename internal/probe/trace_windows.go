//go:build windows

package probe

import (
	"context"
	"errors"
)

// Windows route and backtrace remain behind the native NextTrace gate. The
// platform definition boundary short-circuits those modules before their
// concrete probes run; these compile-time adapters deliberately expose no
// executable, command arguments, or Unix fallback.
const traceWindowsUnsupportedAdapter = "windows-unsupported-v1"

func detectTraceBackend(context.Context) traceBackend {
	return traceBackend{
		Name:    windowsUnsupportedMethodology,
		Adapter: traceWindowsUnsupportedAdapter,
	}
}

func traceMaxHopsForFamily(string) int { return routeSnapshotHops }

func traceBackendAvailable(traceBackend) bool { return false }

func traceBackendAvailableForFamily(traceBackend, string) bool { return false }

func traceCommandSpecForFamily(traceBackend, string, int, string) traceCommandSpec {
	return traceCommandSpec{}
}

func runTraceCommandForFamily(context.Context, traceBackend, string, int, string) traceCommandResult {
	return traceCommandResult{Err: errors.New(windowsUnsupportedMethodology)}
}
