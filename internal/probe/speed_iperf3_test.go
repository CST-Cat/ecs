package probe

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/report"
)

func TestIPerfEndpointFamiliesHonorLiteralHost(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		networks string
		want     []string
	}{
		{name: "IPv4 literal without networks", host: "192.0.2.1", want: []string{"IPv4"}},
		{name: "IPv6 literal without networks", host: "2001:db8::1", want: []string{"IPv6"}},
		{name: "hostname without networks keeps default", host: "iperf.example.com", want: []string{"IPv4"}},
		{name: "hostname pinned IPv4", host: "iperf.example.com", networks: "IPv4", want: []string{"IPv4"}},
		{name: "hostname pinned IPv6", host: "iperf.example.com", networks: "IPv6", want: []string{"IPv6"}},
		{name: "hostname dual stack", host: "iperf.example.com", networks: "IPv4|IPv6", want: []string{"IPv4", "IPv6"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := iperfEndpointFamilies(config.IPerfEndpoint{Host: test.host, Networks: test.networks}, true, true, config.IPVersionAuto)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("iperfEndpointFamilies(%+v) = %v, want %v", test, got, test.want)
			}
		})
	}
}

func TestSpeedMissingToolPreservesStagedLookupError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(ToolBinEnv, "")
	_, expectedErr := LookupTool("iperf3")
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

func TestIPerfCustomTargetMachineFactFlowsThroughSpeedTableToCanonicalJSON(t *testing.T) {
	targets, err := config.ParseIPerfTargetList("fixture=fixture.invalid:5200")
	if err != nil {
		t.Fatalf("ParseIPerfTargetList custom target: %v", err)
	}
	if len(targets) != 1 || targets[0].Location != "" {
		t.Fatalf("custom target location = %#v, want empty machine fact", targets)
	}

	path := writeThroughputExecutable(t, "iperf3", fakeIPerfExecutable)
	result := runIPerfSpeed(context.Background(), Environment{
		Config: config.Runtime{
			IPVersion:     config.IPVersion4,
			IPerfDuration: time.Second,
			SpeedThreads:  2,
			IPerfTargets:  targets,
		},
		Network: NetworkCapabilities{IPv4Usable: true},
	}, path)
	if len(result.Tables) == 0 || result.Tables[0].Key != "network.iperf3.results" || len(result.Tables[0].Rows) != 1 {
		t.Fatalf("custom target speed table = %#v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	if raw, ok := row[0].Raw(); !ok || raw != targets[0].Name {
		t.Fatalf("custom target provider cell = %#v, want raw %q", row[0], targets[0].Name)
	}
	if raw, ok := row[1].Raw(); !ok || raw != targets[0].Host {
		t.Fatalf("custom target location cell = %#v, want raw host %q", row[1], targets[0].Host)
	}
	if strings.Contains(row[1].Text(), "命令行指定") {
		t.Fatalf("speed table retained ECS-localized custom location: %#v", row[1])
	}

	canonical, err := report.JSON(model.Report{
		SchemaVersion: "ecs.report/v1",
		Results:       []model.Result{result},
	})
	if err != nil {
		t.Fatalf("canonical custom target JSON: %v", err)
	}
	canonicalText := string(canonical)
	if strings.Contains(canonicalText, "命令行指定") {
		t.Fatalf("canonical custom target JSON contains ECS-localized location:\n%s", canonicalText)
	}
	if !strings.Contains(canonicalText, `"raw": "fixture.invalid"`) {
		t.Fatalf("canonical custom target JSON lost raw host fallback:\n%s", canonicalText)
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

	failedDirection := runIPerfDirectionWith(context.Background(), "unused", target, "IPv4", false, 1, 1, func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		return iperfDirectionResult{Port: port, Error: "fixture unavailable"}
	})
	if failedDirection.Error != "fixture unavailable" || failedDirection.Port != 5202 {
		t.Fatalf("iperf all ports failure = %+v", failedDirection)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledDirection := runIPerfDirectionWith(canceled, "unused", target, "IPv4", false, 1, 1, func(_ context.Context, _ string, _ string, port int, _ string, _ bool, _ int, _ int) iperfDirectionResult {
		t.Fatal("canceled iperf direction invoked runner")
		return iperfDirectionResult{Port: port, Mbps: 1}
	})
	if !strings.Contains(cancelledDirection.Error, "context canceled") {
		t.Fatalf("canceled iperf direction = %+v", cancelledDirection)
	}

	if got := iperfUDPPort(target, iperfDirectionResult{Port: 5201, Mbps: 10}, iperfDirectionResult{Port: 5202, Mbps: 20}); got != 5201 {
		t.Fatalf("UDP upload port = %d", got)
	}
	if got := iperfUDPPort(target, iperfDirectionResult{}, iperfDirectionResult{Port: 5202, Mbps: 20}); got != 5202 {
		t.Fatalf("UDP download fallback port = %d", got)
	}
}
