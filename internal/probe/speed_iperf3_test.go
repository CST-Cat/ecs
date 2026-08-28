package probe

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestSpeedMissingToolPreservesRawLookPathError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, expectedErr := exec.LookPath("iperf3")
	if expectedErr == nil {
		t.Fatal("fixture PATH unexpectedly contains iperf3")
	}

	direct := (speedProbe{}).Run(context.Background(), Environment{})
	if len(direct.Failures) != 1 {
		t.Fatalf("missing-tool failures = %#v", direct.Failures)
	}
	failure := direct.Failures[0]
	if failure.Category != model.FailureToolMissing || failure.Stage != "tool_lookup" || failure.Target != "iperf3" || failure.Message != expectedErr.Error() {
		t.Fatalf("missing-tool failure lost typed lookup error: %#v, want %q", failure, expectedErr.Error())
	}

	if len(direct.SummaryMessages) != 1 || direct.SummaryMessages[0].Key != "probe.speed.summary.tool_missing" {
		t.Fatalf("missing-tool summary = %#v", direct.SummaryMessages)
	}
	if len(direct.Notes) != 5 || direct.Notes[0] != "probe.speed.note.active_traffic" {
		t.Fatalf("missing-tool notes = %#v", direct.Notes)
	}
}

func TestIPerfJSONParsersAndDirectionDiagnostics(t *testing.T) {
	forward := parseIPerfTCPJSON([]byte(`{
		"start":{"connected":[{"local_host":"local","remote_host":"remote"}],"test_start":{"protocol":"TCP","reverse":0}},
		"intervals":[{"sum":{"bits_per_second":900000000}},{"sum":{"bits_per_second":1100000000}}],
		"end":{"sum_sent":{"bytes":100,"bits_per_second":1000000000,"retransmits":3,"seconds":1},"sum_received":{"bytes":90,"bits_per_second":800000000,"seconds":1}}
	}`), 5200, false)
	if forward.Error != "" || forward.Mbps != 1000 || forward.Bytes != 100 || forward.Retransmits != 3 || len(forward.IntervalMbps) != 2 || forward.IntervalMin != 900 || forward.IntervalMedian != 1000 || forward.IntervalCV != 10 || forward.LocalHost != "local" || forward.RemoteHost != "remote" {
		t.Fatalf("iperf3 forward = %+v", forward)
	}
	reverse := parseIPerfTCPJSON([]byte(`{"start":{"test_start":{"protocol":"TCP","reverse":1}},"end":{"sum_sent":{"bytes":70,"bits_per_second":700000000,"retransmits":2,"seconds":1},"sum_received":{"bytes":200,"bits_per_second":2000000000,"seconds":2}}}`), 5201, true)
	if reverse.Error != "" || reverse.Mbps != 2000 || reverse.Bytes != 200 || reverse.Seconds != 2 || reverse.Retransmits != 2 {
		t.Fatalf("iperf3 reverse = %+v", reverse)
	}

	udp := parseIPerfUDPJSON([]byte(`{"start":{"test_start":{"protocol":"UDP"}},"end":{"sum_received":{"bits_per_second":50000000,"jitter_ms":1.25,"lost_packets":2,"packets":100,"lost_percent":2}}}`), nil, nil)
	if !udp.Available || udp.Mbps != 50 || udp.JitterMS != 1.25 || udp.LostPercent != 2 || udp.Packets != 100 {
		t.Fatalf("iperf3 UDP = %+v", udp)
	}

	tcpFailures := []struct {
		name, raw, marker string
		port              int
		reverse           bool
	}{
		{name: "upstream error", raw: `{"error":"server busy"}`, marker: "server busy"},
		{name: "wrong protocol", raw: `{"start":{"test_start":{"protocol":"UDP"}}}`, marker: "不是 TCP"},
		{name: "direction mismatch", raw: `{"start":{"test_start":{"protocol":"TCP","reverse":1}}}`, marker: "方向与请求不一致", reverse: false},
		{name: "invalid statistics", raw: `{"start":{"test_start":{"protocol":"TCP","reverse":0}},"end":{"sum_sent":{"bytes":10,"bits_per_second":-1,"seconds":1}}}`, marker: "有效吞吐"},
		{name: "malformed", raw: `{`, marker: "解析 JSON"},
	}
	for _, test := range tcpFailures {
		got := parseIPerfTCPJSON([]byte(test.raw), test.port, test.reverse)
		if !strings.Contains(got.Error, test.marker) {
			t.Errorf("%s error = %q", test.name, got.Error)
		}
	}
	if got := parseIPerfTCPJSON(bytes.Repeat([]byte("x"), 4<<20+1), 5200, false); !strings.Contains(got.Error, "超过 4 MiB") {
		t.Fatalf("oversized TCP JSON error = %q", got.Error)
	}
	udpFailures := []struct {
		name, raw, marker, stderrMarker string
		commandErr                      error
		stderr                          []byte
	}{
		{name: "malformed stderr", raw: `{`, marker: "fixture stderr", stderr: []byte("fixture stderr \x1b[31m")},
		{name: "output error", raw: `{"error":"server busy"}`, marker: "server busy"},
		{name: "command error", raw: `{"start":{"test_start":{"protocol":"UDP"}}}`, marker: "command failed", stderrMarker: "command stderr", commandErr: errors.New("command failed"), stderr: []byte("command stderr")},
		{name: "wrong protocol", raw: `{"start":{"test_start":{"protocol":"TCP"}}}`, marker: "不是 UDP"},
		{name: "missing packets", raw: `{"start":{"test_start":{"protocol":"UDP"}},"end":{"sum_received":{"packets":0}}}`, marker: "未返回 UDP 包统计"},
		{name: "invalid statistics", raw: `{"start":{"test_start":{"protocol":"UDP"}},"end":{"sum_received":{"packets":1,"bits_per_second":-1,"jitter_ms":1,"lost_percent":101}}}`, marker: "无效的吞吐"},
	}
	for _, test := range udpFailures {
		got := parseIPerfUDPJSON([]byte(test.raw), test.commandErr, test.stderr)
		if !strings.Contains(got.Err, test.marker) {
			t.Errorf("%s error = %q", test.name, got.Err)
		}
		if test.stderrMarker != "" && !strings.Contains(got.Err, test.stderrMarker) {
			t.Errorf("%s omitted stderr detail: %q", test.name, got.Err)
		}
		if test.name == "malformed stderr" && strings.Contains(got.Err, "\x1b") {
			t.Errorf("%s leaked terminal escape: %q", test.name, got.Err)
		}
	}
	if got := parseIPerfUDPJSON(bytes.Repeat([]byte("x"), 4<<20+1), nil, nil); !strings.Contains(got.Err, "超过 4 MiB") {
		t.Fatalf("oversized UDP JSON error = %q", got.Err)
	}
}

func TestIPerfDirectionPortSelectionAndUDPFallback(t *testing.T) {
	target := config.IPerfEndpoint{Host: "fixture.invalid", PortStart: 5200, PortEnd: 5202}
	ports := iperfPortCandidates(target)
	if len(ports) != 3 || ports[0] != 5200 || ports[2] != 5202 {
		t.Fatalf("iperf port candidates = %v", ports)
	}
	if ordered := orderedIPerfPorts(ports, 5201); len(ordered) != 3 || ordered[0] != 5201 {
		t.Fatalf("preferred iperf ports = %v", ordered)
	}

	var attempts []int
	var reverse bool
	runner := func(_ context.Context, _ string, _ string, port int, _ string, requestedReverse bool, _ int, _ int) iperfDirectionResult {
		attempts = append(attempts, port)
		reverse = requestedReverse
		if len(attempts) == 1 {
			return iperfDirectionResult{Port: port, Error: "fixture first port failed"}
		}
		return iperfDirectionResult{Port: port, Mbps: 100, Bytes: 10}
	}
	got := runIPerfDirectionWithPreferred(context.Background(), "unused", target, "IPv4", true, 2, 1, 5201, runner)
	if got.Mbps != 100 || got.Port != 5200 || !reverse || len(attempts) != 2 || attempts[0] != 5201 || attempts[1] != 5200 {
		t.Fatalf("iperf preferred retry = %+v attempts=%v", got, attempts)
	}

	failResult := runIPerfDirectionWith(context.Background(), "unused", target, "IPv4", false, 1, 1, func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		return iperfDirectionResult{Port: port, Error: "fixture unavailable"}
	})
	if failResult.Error != "fixture unavailable" || failResult.Port != 5202 {
		t.Fatalf("iperf all ports failure = %+v", failResult)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledResult := runIPerfDirectionWith(canceled, "unused", target, "IPv4", false, 1, 1, func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		t.Fatal("canceled iperf direction invoked runner")
		return iperfDirectionResult{Port: port, Mbps: 1}
	})
	if !strings.Contains(canceledResult.Error, "context canceled") {
		t.Fatalf("canceled iperf direction = %+v", canceledResult)
	}

	if got := iperfUDPPort(target, iperfDirectionResult{Port: 5201, Mbps: 10}, iperfDirectionResult{Port: 5202, Mbps: 20}); got != 5201 {
		t.Fatalf("UDP upload port = %d", got)
	}
	if got := iperfUDPPort(target, iperfDirectionResult{}, iperfDirectionResult{Port: 5202, Mbps: 20}); got != 5202 {
		t.Fatalf("UDP download fallback port = %d", got)
	}
}
