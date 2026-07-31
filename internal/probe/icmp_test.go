package probe

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPingArguments(t *testing.T) {
	args := strings.Join(pingArguments("1.1.1.1", 4, 2*time.Second), " ")
	if !strings.Contains(args, "1.1.1.1") {
		t.Fatalf("ping args lost the host: %s", args)
	}
	// iputils 与 busybox 的 -W 都以秒计，不能传毫秒值。
	for _, expected := range []string{"-n", "-q", "-c 4", "-W 2"} {
		if !strings.Contains(args, expected) {
			t.Fatalf("ping args missing %q: %s", expected, args)
		}
	}
	// 不足一秒必须进位：-W 0 会被 ping 当成"不等待"。
	sub := strings.Join(pingArguments("1.1.1.1", 1, 300*time.Millisecond), " ")
	if !strings.Contains(sub, "-W 1") {
		t.Fatalf("sub-second timeout must round up to 1s: %s", sub)
	}
}

func TestPingStatisticsParsing(t *testing.T) {
	// iputils 的输出格式：min/avg/max/mdev 四段。
	iputils := `--- 1.1.1.1 ping statistics ---
5 packets transmitted, 5 received, 0% packet loss, time 4005ms
rtt min/avg/max/mdev = 10.100/12.300/15.200/1.800 ms`
	if match := pingLossPattern.FindStringSubmatch(iputils); len(match) != 2 || match[1] != "0" {
		t.Fatalf("iputils loss = %v", match)
	}
	rtt := pingRTTPattern.FindStringSubmatch(iputils)
	if len(rtt) != 5 || rtt[2] != "12.300" {
		t.Fatalf("iputils rtt = %v", rtt)
	}

	// busybox ping（Alpine 等精简镜像的默认实现）只有三段，且丢包带小数。
	busybox := `--- 1.1.1.1 ping statistics ---
5 packets transmitted, 4 packets received, 20.0% packet loss
round-trip min/avg/max = 10.1/12.3/15.2 ms`
	if match := pingLossPattern.FindStringSubmatch(busybox); len(match) != 2 || match[1] != "20.0" {
		t.Fatalf("busybox loss = %v", match)
	}
	if rtt := pingRTTPattern.FindStringSubmatch(busybox); rtt != nil {
		t.Fatalf("four-field pattern must not match a three-field line: %v", rtt)
	}
	rtt = pingRTTThreePattern.FindStringSubmatch(busybox)
	if len(rtt) != 4 || rtt[3] != "15.2" {
		t.Fatalf("busybox rtt = %v", rtt)
	}
	// 三段回退不能误吞四段统计行的后三个数。
	if match := pingRTTThreePattern.FindStringSubmatch(iputils); match != nil {
		t.Fatalf("three-field fallback must not match an iputils line: %v", match)
	}

	// 完全不可达时没有 rtt 行，但丢包率仍可解析。
	unreachable := `--- 10.255.255.1 ping statistics ---
3 packets transmitted, 0 received, 100% packet loss, time 2043ms`
	if match := pingLossPattern.FindStringSubmatch(unreachable); len(match) != 2 || match[1] != "100" {
		t.Fatalf("unreachable loss = %v", match)
	}
	if rtt := pingRTTPattern.FindStringSubmatch(unreachable); rtt != nil {
		t.Fatalf("unreachable output must not yield rtt: %v", rtt)
	}
	if rtt := pingRTTThreePattern.FindStringSubmatch(unreachable); rtt != nil {
		t.Fatalf("unreachable output must not yield a three-field rtt: %v", rtt)
	}
}

func TestIPerfUDPJSONFields(t *testing.T) {
	raw := []byte(`{"end":{"sum_received":{
		"bytes":31250000,"bits_per_second":50000000,"seconds":5,
		"jitter_ms":0.482,"lost_packets":12,"packets":21500,"lost_percent":0.0558
	}}}`)
	var output iperfJSONOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	sum := output.End.SumReceived
	if sum.JitterMS != 0.482 || sum.LostPackets != 12 || sum.Packets != 21500 {
		t.Fatalf("udp sum = %+v", sum)
	}
	if sum.LostPercent != 0.0558 {
		t.Fatalf("lost percent = %f", sum.LostPercent)
	}
}
