package probe

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestIPerfPortCandidatesUsesCompleteConfiguredRange(t *testing.T) {
	target := config.IPerfEndpoint{PortStart: 5200, PortEnd: 5209}
	want := []int{5200, 5201, 5202, 5203, 5204, 5205, 5206, 5207, 5208, 5209}
	if got := iperfPortCandidates(target); !reflect.DeepEqual(got, want) {
		t.Fatalf("iperf port candidates = %v, want %v", got, want)
	}

	// Config validation normally rejects this, but a directly constructed
	// endpoint still has one concrete candidate to test.
	if got := iperfPortCandidates(config.IPerfEndpoint{PortStart: 5300, PortEnd: 5299}); !reflect.DeepEqual(got, []int{5300}) {
		t.Fatalf("invalid iperf port range candidates = %v, want [5300]", got)
	}
}

func TestRunIPerfDirectionTriesLaterPortThenStops(t *testing.T) {
	target := config.IPerfEndpoint{Host: "example.invalid", PortStart: 9204, PortEnd: 9231}
	var calls []int
	run := func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		calls = append(calls, port)
		if port == 9221 {
			return iperfDirectionResult{Port: port, Mbps: 123.4}
		}
		return iperfDirectionResult{Port: port, Error: "connection refused"}
	}

	got := runIPerfDirectionWith(context.Background(), "unused", target, "IPv4", false, 1, 1, run, nil)
	if got.Mbps != 123.4 || got.Port != 9221 {
		t.Fatalf("later-port success = %+v, want port 9221 at 123.4 Mbps", got)
	}
	wantCalls := make([]int, 0, 18)
	for port := 9204; port <= 9221; port++ {
		wantCalls = append(wantCalls, port)
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("ports tried before success = %v, want %v", calls, wantCalls)
	}
}

func TestRunIPerfDirectionAllPortsFail(t *testing.T) {
	target := config.IPerfEndpoint{Host: "example.invalid", PortStart: 5200, PortEnd: 5205}
	var calls []int
	run := func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		calls = append(calls, port)
		return iperfDirectionResult{Port: port, Error: "iperf3 server is busy"}
	}

	got := runIPerfDirectionWith(context.Background(), "unused", target, "IPv4", true, 2, 1, run, nil)
	if got.Mbps != 0 || got.Port != target.PortEnd || got.Error != "iperf3 server is busy" {
		t.Fatalf("all-port failure = %+v, want final port/error preserved", got)
	}
	wantCalls := []int{5200, 5201, 5202, 5203, 5204, 5205}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("all-port calls = %v, want %v", calls, wantCalls)
	}
}

func TestRunIPerfDirectionAllPortPrechecksFail(t *testing.T) {
	target := config.IPerfEndpoint{Host: "example.invalid", PortStart: 9204, PortEnd: 9208}
	var checked []int
	runCalls := 0
	check := func(_ context.Context, _ string, port int, _ string) error {
		checked = append(checked, port)
		return context.DeadlineExceeded
	}
	run := func(_ context.Context, _ string, _ string, _ int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		runCalls++
		return iperfDirectionResult{Mbps: 999}
	}

	got := runIPerfDirectionWith(context.Background(), "unused", target, "IPv4", false, 1, 1, run, check)
	if runCalls != 0 {
		t.Fatalf("runner calls after all prechecks failed = %d, want 0", runCalls)
	}
	if got.Port != target.PortEnd || !strings.Contains(got.Error, "端口预检失败") {
		t.Fatalf("all-precheck failure = %+v, want final port and precheck error", got)
	}
	want := []int{9204, 9205, 9206, 9207, 9208}
	if !reflect.DeepEqual(checked, want) {
		t.Fatalf("precheck ports = %v, want %v", checked, want)
	}
}

func TestRunIPerfDirectionContinuesAfterProtocolFailure(t *testing.T) {
	target := config.IPerfEndpoint{Host: "example.invalid", PortStart: 9204, PortEnd: 9208}
	var calls []int
	check := func(_ context.Context, _ string, _ int, _ string) error { return nil }
	run := func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		calls = append(calls, port)
		if port == 9208 {
			return iperfDirectionResult{Port: port, Mbps: 42}
		}
		return iperfDirectionResult{Port: port, Error: "iperf3 JSON 无效"}
	}

	got := runIPerfDirectionWith(context.Background(), "unused", target, "IPv4", false, 1, 1, run, check)
	if got.Mbps != 42 || got.Port != 9208 {
		t.Fatalf("protocol failure recovery = %+v, want final success on 9208", got)
	}
	want := []int{9204, 9205, 9206, 9207, 9208}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("protocol retry ports = %v, want %v", calls, want)
	}
}

func TestIPerfUDPPortUsesSuccessfulDirection(t *testing.T) {
	target := config.IPerfEndpoint{PortStart: 5200, PortEnd: 5210}
	if got := iperfUDPPort(target,
		iperfDirectionResult{Port: 5209, Error: "upload failed"},
		iperfDirectionResult{Port: 5221, Mbps: 10},
	); got != 5221 {
		t.Fatalf("UDP port with upload failure = %d, want download success port 5221", got)
	}
	if got := iperfUDPPort(target,
		iperfDirectionResult{Port: 5222, Mbps: 10},
		iperfDirectionResult{Port: 5221, Mbps: 9},
	); got != 5222 {
		t.Fatalf("UDP port with both directions successful = %d, want upload port 5222", got)
	}
	if got := iperfUDPPort(target,
		iperfDirectionResult{Port: 5209, Error: "upload failed"},
		iperfDirectionResult{Port: 5210, Error: "download failed"},
	); got != target.PortStart {
		t.Fatalf("UDP port with both directions failed = %d, want configured start %d", got, target.PortStart)
	}
}

func TestRunIPerfDirectionHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := config.IPerfEndpoint{Host: "example.invalid", PortStart: 5200, PortEnd: 5210}
	calls := 0
	run := func(_ context.Context, _ string, _ string, _ int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		calls++
		return iperfDirectionResult{Error: "must not run"}
	}

	got := runIPerfDirectionWith(ctx, "unused", target, "IPv4", false, 1, 1, run, nil)
	if calls != 0 {
		t.Fatalf("runner calls after cancellation = %d, want 0", calls)
	}
	if got.Port != target.PortStart || got.Error != context.Canceled.Error() {
		t.Fatalf("cancelled direction = %+v, want first port/context canceled", got)
	}

	ctx, cancel = context.WithCancel(context.Background())
	calls = 0
	run = func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		calls++
		cancel()
		return iperfDirectionResult{Port: port, Error: "timeout"}
	}
	got = runIPerfDirectionWith(ctx, "unused", target, "IPv4", false, 1, 1, run, nil)
	if calls != 1 || got.Port != target.PortStart || got.Error != "timeout" {
		t.Fatalf("mid-run cancellation = calls %d result %+v, want one final failed sample", calls, got)
	}
}
