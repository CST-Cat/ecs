//go:build linux

package app

import (
	"reflect"
	"testing"
)

// wantSelectedModuleTools is the frozen tool set the wrapper must stage on
// linux for the route/backtrace/ookla/zstd/cpu/latency selection used by
// TestBuildExecutionPlanDerivesRequiredToolsAndExternalServices.
func wantSelectedModuleTools() []string {
	return []string{"nexttrace-tiny", "speedtest", "zstd", "sysbench", "ping"}
}

// TestResolveRequiredToolsStagesEveryDeclaredToolOnLinux pins that the Linux
// platform boundary is a pure pass-through: no declared tool may be silently
// dropped, because the frozen bundle is the only source for all of them.
func TestResolveRequiredToolsStagesEveryDeclaredToolOnLinux(t *testing.T) {
	application := newApplication()
	for _, test := range []struct {
		module string
		want   []string
	}{
		{module: "latency", want: []string{"ping"}},
		{module: "route", want: []string{"nexttrace-tiny"}},
		{module: "backtrace", want: []string{"nexttrace-tiny"}},
		{module: "cpu", want: []string{"sysbench"}},
		{module: "zstd", want: []string{"zstd"}},
		{module: "npb", want: []string{"npb-ep", "npb-ft"}},
		{module: "memory", want: []string{"stream"}},
		{module: "crypto", want: []string{"openssl"}},
		{module: "disk", want: []string{"fio"}},
		{module: "speed", want: []string{"iperf3"}},
		{module: "ookla", want: []string{"speedtest"}},
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

// TestResolveRequiredToolsDoesNotAliasCallerStorage guards the immutability
// contract of module.Descriptor: the resolver must return storage the caller
// owns, so a later write cannot reach back into the descriptor it read.
func TestResolveRequiredToolsDoesNotAliasCallerStorage(t *testing.T) {
	declared := []string{"ping", "nexttrace-tiny"}
	resolved := resolveRequiredTools(declared)
	if !reflect.DeepEqual(resolved, declared) {
		t.Fatalf("resolveRequiredTools(%v) = %v", declared, resolved)
	}
	resolved[0] = "mutated"
	if declared[0] != "ping" {
		t.Fatalf("resolver output aliases its input: declared = %v", declared)
	}
}
