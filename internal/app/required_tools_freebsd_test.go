//go:build freebsd

package app

import (
	"reflect"
	"testing"

	"ecs/internal/tool"
)

// wantSelectedModuleTools is the tool set the wrapper plan must retain on
// FreeBSD for the route/backtrace/ookla/zstd/cpu/latency selection used by
// TestBuildExecutionPlanDerivesRequiredToolsAndExternalServices. route and
// backtrace fall back to base-system traceroute and latency to base-system
// ping, so neither download survives.
func wantSelectedModuleTools() []string {
	return []string{"speedtest", "zstd", "sysbench"}
}

func wantPlanJSONRequiredTools() []string {
	return []string{"sysbench", "zstd", "speedtest"}
}

func wantPlanJSONExternalServices() []string {
	return []string{"third-party-provider", "ookla"}
}

// TestResolveRequiredToolsDropsBaseSystemNetworkToolsOnFreeBSD pins the FreeBSD
// platform boundary. Only ping and nexttrace-tiny have a base-system
// substitute; other requirements remain in the plan, including speedtest's
// existing signed-package path.
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

func TestFreeBSDSpeedtestRemainsInPrivatePlanProjection(t *testing.T) {
	if got := tool.PlatformToolSource(tool.PlatformFreeBSD, "speedtest"); got != tool.ToolSourceBundle {
		t.Fatalf("FreeBSD speedtest source = %q, want bundle projection", got)
	}
	if got, want := resolveRequiredTools([]string{"speedtest"}), []string{"speedtest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FreeBSD speedtest required_tools projection = %v, want %v", got, want)
	}
	// This is a private plan/staging projection only. speedtest is absent from
	// the frozen tools/lock.json archive; run.sh uses its separately verified
	// signed package path and FreeBSD then fails closed without a client.
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
