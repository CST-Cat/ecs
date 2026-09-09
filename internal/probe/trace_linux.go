//go:build linux

package probe

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"ecs/internal/config"
)

func detectTraceBackend(ctx context.Context) traceBackend {
	path, err := LookupTool(traceNextTraceEngineName)
	if err != nil {
		return traceBackend{Name: traceNextTraceEngineName, Adapter: traceNextTraceAdapter}
	}
	return traceBackend{
		Name:    traceNextTraceEngineName,
		Path:    path,
		Version: commandVersion(ctx, path),
		Adapter: traceNextTraceAdapter,
	}
}

func traceMaxHopsForFamily(string) int { return routeSnapshotHops }

func traceBackendAvailable(backend traceBackend) bool {
	return backend.Adapter == traceNextTraceAdapter && backend.Path != ""
}

func traceBackendAvailableForFamily(backend traceBackend, family string) bool {
	spec := traceCommandSpecForFamily(backend, "<target>", routeSnapshotHops, family)
	return traceBackendAvailable(backend) && spec.Path != "" && len(spec.Args) > 0
}

func traceCommandSpecForFamily(backend traceBackend, target string, maxHops int, family string) traceCommandSpec {
	if backend.Adapter != traceNextTraceAdapter || backend.Name != traceNextTraceEngineName {
		return traceCommandSpec{}
	}
	args := nextTraceCommandArgsForFamily(target, maxHops, family)
	if len(args) == 0 {
		return traceCommandSpec{}
	}
	return traceCommandSpec{Path: backend.Path, Args: args}
}

// nextTraceCommandArgsForFamily is the single canonical argv constructor for
// the frozen Linux NextTrace backend. It deliberately has no route/backtrace
// descriptor dependency.
func nextTraceCommandArgsForFamily(target string, maxHops int, family string) []string {
	hops := strconv.Itoa(maxHops)
	familyArg := ""
	if family == config.IPVersion4 || family == config.IPVersion6 {
		familyArg = "-" + family
	}
	args := []string{"--no-color", "--json", "-M", "--max-hops", hops, "--queries", "1", "--parallel-requests", "1", "--timeout", "1000"}
	if familyArg != "" {
		args = append([]string{familyArg}, args...)
	}
	return append(args, target)
}

func runTraceCommandForFamily(ctx context.Context, backend traceBackend, target string, maxHops int, family string) traceCommandResult {
	if !traceBackendAvailableForFamily(backend, family) {
		return traceCommandResult{Err: fmt.Errorf("trace backend is unavailable: %s", backend.Name)}
	}
	spec := traceCommandSpecForFamily(backend, target, maxHops, family)
	if spec.Path == "" || len(spec.Args) == 0 {
		return traceCommandResult{Err: fmt.Errorf("unsupported trace backend: %s", backend.Name)}
	}
	command := newProbeCommand(ctx, spec.Path, spec.Args...)
	command.Env = append(os.Environ(), "NO_COLOR=1", "LC_ALL=C", "LANG=C")
	run := command.RunSeparate()
	result := traceCommandResult{Stdout: run.Stdout, Stderr: run.Stderr, Err: run.Err}
	if contextCauseError(ctx) != nil {
		return result
	}
	trace, err := parseNextTraceCanonical(sanitizeCommandOutput(run.Stdout), family, target)
	if err != nil {
		result.ParseErr = err
		return result
	}
	result.Trace = trace
	result.Parsed = true
	return result
}
