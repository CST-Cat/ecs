//go:build windows

package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ecs/internal/config"
)

const windowsTraceFixtureEnv = "ECS_WINDOWS_TRACE_FIXTURE"

// TestMain lets the test binary act as a real Windows PE fixture without a
// shell script. The fixture branch runs before testing parses command-line
// flags, so the canonical NextTrace argv reaches this executable unchanged.
func TestMain(m *testing.M) {
	if os.Getenv(windowsTraceFixtureEnv) == "1" {
		windowsTraceFixture(os.Args[1:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func windowsTraceFixture(args []string) {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-V") {
		_, _ = fmt.Fprintln(os.Stdout, "fixture-nexttrace 1")
		return
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "fixture received no arguments")
		os.Exit(2)
	}
	payload := struct {
		Hops        [][]map[string]any `json:"Hops"`
		FixtureArgs []string           `json:"fixture_args"`
	}{
		Hops: [][]map[string]any{
			{{"Address": map[string]string{"IP": "203.0.113.1"}}},
		},
		FixtureArgs: args,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_, _ = os.Stdout.Write(append(data, '\n'))
}

func TestWindowsTraceUsesStagedCanonicalNextTraceAdapter(t *testing.T) {
	stagedDirectory, stagedPath := stageWindowsTraceFixture(t)
	t.Setenv(ToolBinEnv, stagedDirectory)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(windowsTraceFixtureEnv, "1")

	backend := detectTraceBackend(context.Background())
	if backend.Name != traceNextTraceEngineName || backend.Adapter != traceNextTraceAdapter || backend.Path != stagedPath || backend.Version != "fixture-nexttrace 1" {
		t.Fatalf("staged Windows trace backend = %#v", backend)
	}
	if !traceBackendAvailable(backend) || !traceBackendAvailableForFamily(backend, config.IPVersion4) || !traceBackendAvailableForFamily(backend, config.IPVersion6) {
		t.Fatalf("staged Windows trace backend is not available for both families: %#v", backend)
	}

	target := "target with spaces &|<>"
	for _, test := range []struct {
		name   string
		family string
		maxHop int
	}{
		{name: "IPv4", family: config.IPVersion4, maxHop: routeSnapshotHops},
		{name: "IPv6", family: config.IPVersion6, maxHop: backtraceMaxHops},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := traceCommandSpecForFamily(backend, target, test.maxHop, test.family)
			wantArgs := nextTraceCommandArgsForFamily(target, test.maxHop, test.family)
			if spec.Path != stagedPath || !reflect.DeepEqual(spec.Args, wantArgs) {
				t.Fatalf("Windows trace spec = %#v, want path %q and canonical args %v", spec, stagedPath, wantArgs)
			}
			run := runTraceCommandForFamily(context.Background(), backend, target, test.maxHop, test.family)
			if run.Err != nil || run.ParseErr != nil || !run.Parsed {
				t.Fatalf("Windows canonical trace run = %#v", run)
			}
			if run.Trace.Engine != traceNextTraceEngineName || run.Trace.Adapter != traceNextTraceAdapter || run.Trace.Family != traceFamilyName(test.family) || run.Trace.Target != target {
				t.Fatalf("Windows canonical trace facts = %#v", run.Trace)
			}
			var fixture struct {
				FixtureArgs []string `json:"fixture_args"`
			}
			if err := json.Unmarshal(run.Stdout, &fixture); err != nil {
				t.Fatalf("fixture output JSON = %v; output=%q", err, run.Stdout)
			}
			if !reflect.DeepEqual(fixture.FixtureArgs, wantArgs) {
				t.Fatalf("Windows executor argv = %v, want canonical %v", fixture.FixtureArgs, wantArgs)
			}
			if strings.Contains(string(run.Stdout), "cmd.exe") || strings.Contains(string(run.Stdout), "powershell") {
				t.Fatalf("Windows trace fixture observed a shell executable in argv: %q", run.Stdout)
			}
		})
	}
}

func TestWindowsTraceMissingStagedToolFailsClosed(t *testing.T) {
	hostDirectory := t.TempDir()
	copyWindowsTraceFixture(t, hostDirectory, traceNextTraceEngineName+".exe")
	stagedDirectory := t.TempDir()
	t.Setenv(ToolBinEnv, stagedDirectory)
	t.Setenv("PATH", hostDirectory)

	if _, err := LookupTool(traceNextTraceEngineName); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing staged NextTrace lookup = %v, want exec.ErrNotFound", err)
	}
	backend := detectTraceBackend(context.Background())
	if backend.Path != "" || backend.Adapter != traceNextTraceAdapter || traceBackendAvailable(backend) || traceBackendAvailableForFamily(backend, config.IPVersion4) {
		t.Fatalf("missing staged Windows trace backend = %#v", backend)
	}
	run := runTraceCommandForFamily(context.Background(), backend, "203.0.113.1", routeSnapshotHops, config.IPVersion4)
	if run.Err == nil || run.Parsed || run.ParseErr != nil || !strings.Contains(run.Err.Error(), "unavailable") {
		t.Fatalf("missing staged Windows trace run = %#v", run)
	}
}

func TestWindowsTraceTamperedStagedToolDoesNotUseHostPath(t *testing.T) {
	stagedDirectory := t.TempDir()
	stagedPath := filepath.Join(stagedDirectory, traceNextTraceEngineName+".exe")
	if err := os.WriteFile(stagedPath, []byte("not a Windows executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	hostDirectory := t.TempDir()
	copyWindowsTraceFixture(t, hostDirectory, traceNextTraceEngineName+".exe")
	t.Setenv(ToolBinEnv, stagedDirectory)
	t.Setenv("PATH", hostDirectory)

	backend := detectTraceBackend(context.Background())
	if backend.Path != stagedPath {
		t.Fatalf("tampered staged path = %q, want %q", backend.Path, stagedPath)
	}
	run := runTraceCommandForFamily(context.Background(), backend, "203.0.113.1", routeSnapshotHops, config.IPVersion4)
	if run.Err == nil || run.Parsed || run.Trace.Hops != nil {
		t.Fatalf("tampered staged Windows trace unexpectedly succeeded: %#v", run)
	}
}

func TestWindowsTraceUsesProductionExecutorCancellation(t *testing.T) {
	stagedDirectory, _ := stageWindowsTraceFixture(t)
	t.Setenv(ToolBinEnv, stagedDirectory)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(windowsTraceFixtureEnv, "1")
	backend := detectTraceBackend(context.Background())

	cause := errors.New("Windows trace cancellation fixture")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	run := runTraceCommandForFamily(ctx, backend, "203.0.113.1", routeSnapshotHops, config.IPVersion4)
	if !errors.Is(run.Err, cause) || run.Parsed || run.ParseErr != nil {
		t.Fatalf("cancelled Windows trace = %#v, want cancellation before parse", run)
	}
}

func stageWindowsTraceFixture(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	path := copyWindowsTraceFixture(t, directory, traceNextTraceEngineName+".exe")
	return directory, path
}

func copyWindowsTraceFixture(t *testing.T, directory, name string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
