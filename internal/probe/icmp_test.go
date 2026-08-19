package probe

import "testing"

func TestPingOutputParsesLossAndLatency(t *testing.T) {
	output := `--- 1.1.1.1 ping statistics ---
5 packets transmitted, 5 received, 0% packet loss, time 4005ms
rtt min/avg/max/mdev = 10.100/12.300/15.200/1.800 ms`
	lossMatch := pingLossPattern.FindStringSubmatch(output)
	if len(lossMatch) != 2 {
		t.Fatalf("ping loss = %v", lossMatch)
	}
	loss, ok := parsePingFloat(lossMatch[1])
	if !ok || loss != 0 {
		t.Fatalf("ping loss = %v, %v", loss, ok)
	}
	rttMatch := pingRTTPattern.FindStringSubmatch(output)
	if len(rttMatch) != 5 {
		t.Fatalf("ping RTT = %v", rttMatch)
	}
	rtt, ok := parsePingFloats(rttMatch[1:])
	if !ok || rtt[1] != 12.3 {
		t.Fatalf("ping RTT values = %v, %v", rtt, ok)
	}
}
