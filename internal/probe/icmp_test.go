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

// TCP 与 ICMP 严重背离说明握手被本地代理代答，必须报警而不是把数字当真。
//
// 这个判定来自一次真实观测：同一个 IP 的 ICMP 往返 227 ms、TCP 握手 2.7 ms，
// 相差两个数量级，成因是网关上的透明代理代答 TCP 而不处理 ICMP。
func TestTCPLikelyIntercepted(t *testing.T) {
	realICMP := icmpStats{Available: true, AvgMS: 227, LossPercent: 0}
	if !tcpLikelyIntercepted(100*time.Microsecond, realICMP) {
		t.Fatal("0.1ms TCP 对 227ms ICMP 必须判为被截获")
	}
	if !tcpLikelyIntercepted(3*time.Millisecond, realICMP) {
		t.Fatal("3ms TCP 对 227ms ICMP 必须判为被截获")
	}
	// 同量级是正常的：TCP 握手与 ICMP echo 都只花一个往返。
	if tcpLikelyIntercepted(230*time.Millisecond, realICMP) {
		t.Fatal("同量级的 TCP 与 ICMP 不能判为被截获")
	}
	if tcpLikelyIntercepted(60*time.Millisecond, realICMP) {
		t.Fatal("5 倍以内的差异属于正常波动")
	}

	// 缺证据一律不判：没有 ICMP、ICMP 全丢、目标在本地网络都不足以下结论。
	for name, icmp := range map[string]icmpStats{
		"ICMP 不可用": {Available: false, AvgMS: 227},
		"ICMP 全丢":  {Available: true, AvgMS: 227, LossPercent: 100},
		"本地目标":     {Available: true, AvgMS: 0.5, LossPercent: 0},
	} {
		if tcpLikelyIntercepted(10*time.Microsecond, icmp) {
			t.Fatalf("%s 时不应下截获结论", name)
		}
	}
	// TCP 全部失败（中位数为 0）时没有可比对象。
	if tcpLikelyIntercepted(0, realICMP) {
		t.Fatal("没有成功的 TCP 样本时不能下结论")
	}
}
