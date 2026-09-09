//go:build freebsd

package probe

import (
	"reflect"
	"testing"
	"time"
)

func TestFreeBSDPingArgumentsUseBaseSystemMillisecondWait(t *testing.T) {
	for _, test := range []struct {
		name    string
		family  string
		timeout time.Duration
		want    []string
	}{
		{name: "IPv4", family: "4", timeout: 500 * time.Millisecond, want: []string{"-n", "-q", "-c", "3", "-W", "500", "-4", "host"}},
		{name: "IPv6", family: "6", timeout: 1500 * time.Millisecond, want: []string{"-n", "-q", "-c", "3", "-W", "1500", "-6", "host"}},
		{name: "auto", timeout: 0, want: []string{"-n", "-q", "-c", "3", "-W", "1", "host"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pingArgumentsForFamily("host", 3, test.timeout, test.family); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("FreeBSD ping args = %#v, want %#v", got, test.want)
			}
		})
	}
	if pingCommand != "/sbin/ping" {
		t.Fatalf("FreeBSD ping command = %q, want /sbin/ping", pingCommand)
	}
}

func TestFreeBSDICMPOutputFixtures(t *testing.T) {
	// Captured from the real FreeBSD 15.1-RELEASE-p3 arm64 /sbin/ping in the
	// disposable QEMU VM used for this phase. The partial-loss capture used a
	// temporary PF rule in that VM only; it did not modify the repository or
	// the host.
	for _, test := range []struct {
		name                          string
		text                          string
		loss, min, avg, max, stddev   float64
		wantLoss, wantRTT, wantStddev bool
	}{
		{
			name: "IPv4 zero loss",
			text: "PING 127.0.0.1 (127.0.0.1): 56 data bytes\n\n--- 127.0.0.1 ping statistics ---\n2 packets transmitted, 2 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 1.607/5.045/8.484/3.438 ms",
			loss: 0, min: 1.607, avg: 5.045, max: 8.484, stddev: 3.438, wantLoss: true, wantRTT: true, wantStddev: true,
		},
		{
			name: "IPv4 partial loss",
			text: "PING 10.0.2.2 (10.0.2.2): 56 data bytes\n\n--- 10.0.2.2 ping statistics ---\n10 packets transmitted, 4 packets received, 60.0% packet loss\nround-trip min/avg/max/stddev = 1.062/1.282/1.434/0.140 ms",
			loss: 60, min: 1.062, avg: 1.282, max: 1.434, stddev: 0.140, wantLoss: true, wantRTT: true, wantStddev: true,
		},
		{
			name: "IPv4 total loss",
			text: "PING 192.0.2.1 (192.0.2.1): 56 data bytes\n\n--- 192.0.2.1 ping statistics ---\n1 packets transmitted, 0 packets received, 100.0% packet loss",
			loss: 100, wantLoss: true,
		},
		{
			name: "IPv6 zero loss",
			text: "PING(56=40+8+8 bytes) ::1 --> ::1\n\n--- ::1 ping statistics ---\n2 packets transmitted, 2 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 1.164/6.352/11.541/5.189 ms",
			loss: 0, min: 1.164, avg: 6.352, max: 11.541, stddev: 5.189, wantLoss: true, wantRTT: true, wantStddev: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := parseICMPOutput(test.text)
			if !got.Available || got.LossKnown != test.wantLoss || got.RTTKnown != test.wantRTT || got.StdDevKnown != test.wantStddev {
				t.Fatalf("FreeBSD ICMP stats = %+v", got)
			}
			if test.wantLoss && got.LossPercent != test.loss {
				t.Errorf("loss = %v, want %v", got.LossPercent, test.loss)
			}
			if test.wantRTT && (got.MinMS != test.min || got.AvgMS != test.avg || got.MaxMS != test.max || got.StdDevMS != test.stddev) {
				t.Errorf("RTT = %.3f/%.3f/%.3f/%.3f, want %.3f/%.3f/%.3f/%.3f", got.MinMS, got.AvgMS, got.MaxMS, got.StdDevMS, test.min, test.avg, test.max, test.stddev)
			}
		})
	}
}

func TestFreeBSDICMPMissingStddevRemainsUnknown(t *testing.T) {
	// This is an explicit parser contract for a valid three-value summary:
	// absence of a standard deviation must remain unknown, never a measured 0.
	got := parseICMPOutput("3 packets transmitted, 2 packets received, 33.3% packet loss\nround-trip min/avg/max = 10.0/12.0/15.0 ms")
	if !got.Available || !got.LossKnown || !got.RTTKnown || got.StdDevKnown || got.StdDevMS != 0 {
		t.Fatalf("missing FreeBSD stddev = %+v", got)
	}
}
