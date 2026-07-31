package probe

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPingArgumentsPerPlatform(t *testing.T) {
	args := strings.Join(pingArguments("1.1.1.1", 4, 2*time.Second), " ")
	if !strings.Contains(args, "1.1.1.1") {
		t.Fatalf("ping args lost the host: %s", args)
	}
	switch runtime.GOOS {
	case "windows":
		// Windows 用 -n 表示次数、-w 表示毫秒超时。
		if !strings.Contains(args, "-n 4") || !strings.Contains(args, "-w 2000") {
			t.Fatalf("windows ping args = %s", args)
		}
	case "darwin", "freebsd", "netbsd", "openbsd":
		// BSD 系的 -W 是毫秒。
		if !strings.Contains(args, "-c 4") || !strings.Contains(args, "-W 2000") {
			t.Fatalf("bsd ping args = %s", args)
		}
	default:
		// Linux 的 -W 是秒。
		if !strings.Contains(args, "-c 4") || !strings.Contains(args, "-W 2") {
			t.Fatalf("linux ping args = %s", args)
		}
	}
}

func TestPingStatisticsParsing(t *testing.T) {
	// Linux iputils 的输出格式。
	linux := `--- 1.1.1.1 ping statistics ---
5 packets transmitted, 5 received, 0% packet loss, time 4005ms
rtt min/avg/max/mdev = 10.100/12.300/15.200/1.800 ms`
	if match := pingLossPattern.FindStringSubmatch(linux); len(match) != 2 || match[1] != "0" {
		t.Fatalf("linux loss = %v", match)
	}
	rtt := pingRTTPattern.FindStringSubmatch(linux)
	if len(rtt) != 5 || rtt[2] != "12.300" {
		t.Fatalf("linux rtt = %v", rtt)
	}

	// macOS/BSD 用 stddev 且丢包带小数。
	bsd := `--- 1.1.1.1 ping statistics ---
5 packets transmitted, 4 packets received, 20.0% packet loss
round-trip min/avg/max/stddev = 10.1/12.3/15.2/1.8 ms`
	if match := pingLossPattern.FindStringSubmatch(bsd); len(match) != 2 || match[1] != "20.0" {
		t.Fatalf("bsd loss = %v", match)
	}
	if rtt := pingRTTPattern.FindStringSubmatch(bsd); len(rtt) != 5 || rtt[3] != "15.2" {
		t.Fatalf("bsd rtt = %v", rtt)
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
