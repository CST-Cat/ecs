//go:build freebsd

package probe

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	freeBSDTracerouteIPv4Path = "/usr/sbin/traceroute"
	freeBSDTracerouteIPv6Path = "/usr/sbin/traceroute6"
)

func detectTraceBackend(ctx context.Context) traceBackend {
	return traceBackend{
		Name:    "freebsd-traceroute",
		Version: freeBSDTracerouteRelease(ctx),
		Adapter: traceFreeBSDTracerouteAdapter,
	}
}

func traceMaxHopsForFamily(family string) int {
	if family == "6" || family == "ipv6" {
		return 20
	}
	return routeSnapshotHops
}

func traceBackendAvailableForFamily(backend traceBackend, family string) bool {
	if backend.Adapter != traceFreeBSDTracerouteAdapter || backend.Name != "freebsd-traceroute" {
		return false
	}
	return freeBSDTracerouteExecutable(freeBSDTraceroutePathForFamily(freeBSDTraceFamily(family)))
}

func traceCommandSpecForFamily(backend traceBackend, target string, maxHops int, family string) traceCommandSpec {
	if backend.Adapter != traceFreeBSDTracerouteAdapter || backend.Name != "freebsd-traceroute" {
		return traceCommandSpec{}
	}
	path := freeBSDTraceroutePathForFamily(family)
	if path == "" {
		return traceCommandSpec{}
	}
	// These flags were verified with FreeBSD 15.1-RELEASE-p3 man pages and
	// ordinary-user loopback runs: ICMP Echo, numeric hops, one probe, and a
	// one-second wait per probe. The IPv4 and IPv6 utilities use the same
	// semantic flags but have separate base-system paths.
	return traceCommandSpec{
		Path: path,
		Args: []string{"-I", "-n", "-q", "1", "-m", strconv.Itoa(maxHops), "-w", "1", target},
	}
}

func runTraceCommandForFamily(ctx context.Context, backend traceBackend, target string, maxHops int, family string) traceCommandResult {
	selectedFamily := freeBSDTraceFamily(family)
	if !traceBackendAvailableForFamily(backend, selectedFamily) {
		path := freeBSDTraceroutePathForFamily(selectedFamily)
		if path == "" {
			return traceCommandResult{Err: fmt.Errorf("FreeBSD traceroute does not support IP family %q", family)}
		}
		return traceCommandResult{Err: fmt.Errorf("FreeBSD traceroute executable for IP family %q is unavailable: %s", family, path)}
	}
	spec := traceCommandSpecForFamily(backend, target, maxHops, selectedFamily)
	if spec.Path == "" || len(spec.Args) == 0 {
		return traceCommandResult{Err: fmt.Errorf("FreeBSD traceroute does not support IP family %q", family)}
	}
	command := newProbeCommand(ctx, spec.Path, spec.Args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	run := command.RunSeparate()
	result := traceCommandResult{Stdout: run.Stdout, Stderr: run.Stderr, Err: run.Err}
	// FreeBSD traceroute writes its route header to stderr and hop rows to
	// stdout. Keep both raw streams separately for the report, but feed the
	// parser in source order (header before rows).
	parseInput := strings.TrimSpace(sanitizeCommandOutput(run.Stderr) + "\n" + sanitizeCommandOutput(run.Stdout))
	if contextCauseError(ctx) != nil {
		return result
	}
	trace, err := parseFreeBSDTraceroute(parseInput, selectedFamily, target)
	if err != nil {
		result.ParseErr = err
		return result
	}
	result.Trace = trace
	result.Parsed = true
	return result
}

func freeBSDTraceFamily(family string) string {
	if family == "auto" {
		return "4"
	}
	return family
}

func freeBSDTraceroutePathForFamily(family string) string {
	switch family {
	case "4", "ipv4", "auto":
		return freeBSDTracerouteIPv4Path
	case "6", "ipv6":
		return freeBSDTracerouteIPv6Path
	default:
		return ""
	}
}

func freeBSDTracerouteExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func freeBSDTracerouteRelease(ctx context.Context) string {
	const fallbackRelease = "FreeBSD base-system traceroute"
	command := newProbeCommand(ctx, "/usr/bin/uname", "-r")
	run := command.RunCombined(1024)
	if run.Err != nil {
		return fallbackRelease
	}
	release := sanitizeCommandOutput(run.Combined)
	if release == "" {
		return fallbackRelease
	}
	return "FreeBSD " + release + " base-system traceroute"
}
