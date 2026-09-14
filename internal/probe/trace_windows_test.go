//go:build windows

package probe

import (
	"context"
	"testing"
)

func TestWindowsTraceBoundaryDoesNotExposeACommandBackend(t *testing.T) {
	backend := detectTraceBackend(context.Background())
	if backend.Adapter != traceWindowsUnsupportedAdapter || traceBackendAvailable(backend) || traceBackendAvailableForFamily(backend, "4") || traceBackendAvailableForFamily(backend, "6") {
		t.Fatalf("Windows trace backend unexpectedly available: %#v", backend)
	}
	if spec := traceCommandSpecForFamily(backend, "127.0.0.1", routeSnapshotHops, "4"); spec.Path != "" || len(spec.Args) != 0 {
		t.Fatalf("Windows trace command spec = %#v, want empty", spec)
	}
	run := runTraceCommandForFamily(context.Background(), backend, "127.0.0.1", routeSnapshotHops, "4")
	if run.Err == nil || run.Err.Error() != windowsUnsupportedMethodology {
		t.Fatalf("Windows trace command result = %#v", run)
	}
}
