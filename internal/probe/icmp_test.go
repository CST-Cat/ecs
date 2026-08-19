package probe

import (
	"strings"
	"testing"
	"time"
)

func TestICMPParserArgumentsAndMeasurements(t *testing.T) {
	cases := []struct {
		name                      string
		text                      string
		loss, min, avg, max, mdev float64
		lossKnown, rttKnown       bool
		stddev, available         bool
	}{
		{name: "iputils", text: "5 packets transmitted, 5 received, 0% packet loss\nrtt min/avg/max/mdev = 10.100/12.300/15.200/1.800 ms", loss: 0, min: 10.1, avg: 12.3, max: 15.2, mdev: 1.8, lossKnown: true, rttKnown: true, stddev: true, available: true},
		{name: "busybox", text: "3 packets transmitted, 2 packets received, 33.3% packet loss\nrtt min/avg/max = 10.0/12.0/15.0 ms", loss: 33.3, min: 10, avg: 12, max: 15, lossKnown: true, rttKnown: true, available: true},
		{name: "loss only", text: "5 packets transmitted, 0 received, 100% packet loss", loss: 100, lossKnown: true, available: true},
		{name: "unavailable", text: "ping output unavailable", available: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := parseICMPOutput(test.text)
			if got.Available != test.available || got.LossKnown != test.lossKnown || got.RTTKnown != test.rttKnown || (test.lossKnown && got.LossPercent != test.loss) || (test.rttKnown && (got.MinMS != test.min || got.AvgMS != test.avg || got.MaxMS != test.max || got.StdDevMS != test.mdev)) || got.StdDevKnown != test.stddev {
				t.Fatalf("ICMP stats = %+v", got)
			}
		})
	}
	if value, ok := parsePingFloat("12.3"); !ok || value != 12.3 {
		t.Fatalf("valid ping float = %v/%v", value, ok)
	}
	if _, ok := parsePingFloat("bad"); ok {
		t.Fatal("invalid ping float parsed")
	}
	if _, ok := parsePingFloats([]string{"1", "bad"}); ok {
		t.Fatal("partially invalid ping floats parsed")
	}
	for _, test := range []struct {
		family, want string
		timeout      time.Duration
	}{{"4", "-4", 500 * time.Millisecond}, {"6", "-6", time.Second}, {"", "-W 1", 0}} {
		args := strings.Join(pingArgumentsForFamily("host", 3, test.timeout, test.family), " ")
		if !strings.Contains(args, test.want) || !strings.HasSuffix(args, "host") || test.family == "" && (strings.Contains(args, " -4 ") || strings.Contains(args, " -6 ")) {
			t.Fatalf("ping args = %q, want %q", args, test.want)
		}
	}
}
