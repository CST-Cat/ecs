//go:build freebsd

package app

import (
	"reflect"
	"testing"
)

// wantSelectedModuleTools is the frozen tool set the wrapper must stage on
// FreeBSD for the route/backtrace/ookla/zstd/cpu/latency selection used by
// TestBuildExecutionPlanDerivesRequiredToolsAndExternalServices. route and
// backtrace fall back to base-system traceroute and latency to base-system
// ping, so neither download survives.
func wantSelectedModuleTools() []string {
	return []string{"speedtest", "zstd", "sysbench"}
}

// TestResolveRequiredToolsDropsBaseSystemNetworkToolsOnFreeBSD pins the FreeBSD
// platform boundary. Only ping and nexttrace-tiny have a base-system
// substitute; every other declared tool is still downloaded and staged.
func TestResolveRequiredToolsDropsBaseSystemNetworkToolsOnFreeBSD(t *testing.T) {
	application := newApplication()
	for _, test := range []struct {
		module string
		want   []string
	}{
		{module: "latency", want: nil},
		{module: "route", want: nil},
		{module: "backtrace", want: nil},
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
	declared := []string{"fio", "ping", "nexttrace-tiny"}
	resolved := resolveRequiredTools(declared)
	if !reflect.DeepEqual(resolved, []string{"fio"}) {
		t.Fatalf("resolveRequiredTools(%v) = %v, want [fio]", declared, resolved)
	}
	resolved[0] = "mutated"
	if declared[0] != "fio" {
		t.Fatalf("resolver output aliases its input: declared = %v", declared)
	}
}
