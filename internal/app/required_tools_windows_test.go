//go:build windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// wantSelectedModuleTools is the Windows tool set for the mixed selection used
// by TestBuildExecutionPlanDerivesRequiredToolsAndExternalServices. The
// unsupported benchmark/network adapters remain selectable modules, while
// route and backtrace share one staged NextTrace executable.
func wantSelectedModuleTools() []string { return []string{"nexttrace-tiny", "zstd"} }

func wantPlanJSONRequiredTools() []string { return []string{"zstd"} }

func wantPlanJSONExternalServices() []string { return []string{"third-party-provider"} }

func TestResolveRequiredToolsKeepsWindowsBundleContract(t *testing.T) {
	application := newApplication()
	for _, test := range []struct {
		module string
		want   []string
	}{
		{module: "latency", want: nil},
		{module: "route", want: []string{"nexttrace-tiny"}},
		{module: "backtrace", want: []string{"nexttrace-tiny"}},
		{module: "cpu", want: nil},
		{module: "zstd", want: []string{"zstd"}},
		{module: "npb", want: []string{"npb-ep", "npb-ft"}},
		{module: "memory", want: []string{"stream"}},
		{module: "crypto", want: []string{"openssl"}},
		{module: "disk", want: []string{"fio"}},
		{module: "speed", want: nil},
		{module: "ookla", want: nil},
		{module: "system", want: nil},
	} {
		t.Run(test.module, func(t *testing.T) {
			descriptor, ok := application.modules.Lookup(test.module)
			if !ok {
				t.Fatalf("descriptor %q missing", test.module)
			}
			if got := resolveRequiredTools(descriptor.RequiredTools); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("resolveRequiredTools(%q) = %v, want %v", test.module, got, test.want)
			}
		})
	}
}

func TestResolveRequiredToolsStagesOnlyWindowsBundleTools(t *testing.T) {
	declared := []string{"ping", "sysbench", "iperf3", "speedtest", "nexttrace-tiny", "zstd"}
	want := []string{"nexttrace-tiny", "zstd"}
	if got := resolveRequiredTools(declared); !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveRequiredTools(%v) = %v, want %v", declared, got, want)
	}
}

func TestResolveRequiredToolsDoesNotAliasCallerStorage(t *testing.T) {
	declared := []string{"zstd", "sysbench", "fio"}
	resolved := resolveRequiredTools(declared)
	if !reflect.DeepEqual(resolved, []string{"zstd", "fio"}) {
		t.Fatalf("resolveRequiredTools(%v) = %v, want [zstd fio]", declared, resolved)
	}
	resolved[0] = "mutated"
	if declared[0] != "zstd" {
		t.Fatalf("resolver output aliases its input: declared = %v", declared)
	}
}

func TestWindowsRoutingPlansRequestNextTrace(t *testing.T) {
	for _, moduleID := range []string{"route", "backtrace"} {
		t.Run(moduleID, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), []string{
				"plan", "--lang", "en", "--profile", "full", "--only", moduleID, "--exposure", "any",
			}, &stdout, &stderr)
			if status != 0 || stderr.Len() != 0 {
				t.Fatalf("Windows %s plan status=%d stderr=%q", moduleID, status, stderr.String())
			}
			var plan struct {
				Modules       []struct{ ID string } `json:"modules"`
				RequiredTools []string              `json:"required_tools"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if len(plan.Modules) != 1 || plan.Modules[0].ID != moduleID {
				t.Fatalf("Windows %s plan modules = %+v", moduleID, plan.Modules)
			}
			if !reflect.DeepEqual(plan.RequiredTools, []string{"nexttrace-tiny"}) {
				t.Fatalf("Windows %s plan required_tools = %v, want [nexttrace-tiny]", moduleID, plan.RequiredTools)
			}
		})
	}
}
