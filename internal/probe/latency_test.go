package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestLatencyResolutionFamiliesInterceptionAndICMP(t *testing.T) {
	for _, test := range []struct {
		name, address, family, want, marker string
	}{
		{name: "literal IPv4", address: "192.0.2.1:443", family: config.IPVersion4, want: "192.0.2.1:443"},
		{name: "literal IPv6", address: "[2001:db8::1]:443", family: config.IPVersion6, want: "[2001:db8::1]:443"},
		{name: "family mismatch", address: "192.0.2.1:443", family: config.IPVersion6, marker: "目标不是 IPv6"},
		{name: "IPv6 family mismatch", address: "[2001:db8::1]:443", family: config.IPVersion4, marker: "目标不是 IPv4"},
		{name: "malformed", address: "not-an-address", family: config.IPVersionAuto, marker: "missing port"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := resolveEndpoint(context.Background(), test.address, test.family)
			if test.want != "" {
				if err != nil || got != test.want {
					t.Fatalf("resolved endpoint = %q/%v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("resolve error = %v, want %q", err, test.marker)
			}
		})
	}
	for _, test := range []struct {
		name, address, mode, endpointFamily string
		v4, v6                              bool
		want                                string
	}{
		{name: "auto dual", address: "example.test:443", mode: config.IPVersionAuto, v4: true, v6: true, want: "4,6"},
		{name: "auto IPv4 only", address: "example.test:443", mode: config.IPVersionAuto, v4: true, want: "4"},
		{name: "explicit IPv6", address: "example.test:443", mode: config.IPVersion6, v4: true, v6: true, want: "6"},
		{name: "explicit unavailable", address: "example.test:443", mode: config.IPVersion6, v4: true, want: ""},
		{name: "literal IPv4", address: "192.0.2.1:443", mode: config.IPVersionAuto, v4: true, v6: true, want: "4"},
		{name: "literal IPv6", address: "[2001:db8::1]:443", mode: config.IPVersionAuto, v4: true, v6: true, want: "6"},
		{name: "no capability", address: "example.test:443", mode: config.IPVersionAuto, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := latencyFamiliesWithCapability(test.address, test.mode, test.v4, test.v6)
			if strings.Join(got, ",") != test.want {
				t.Fatalf("latency families = %v, want %q", got, test.want)
			}
		})
	}
	if got := latencyFamiliesForEndpoint(config.Endpoint{Address: "example.test:443", Family: config.IPVersion6}, config.IPVersionAuto, true, true); strings.Join(got, ",") != "6" {
		t.Fatalf("endpoint family filter = %v", got)
	}

	for _, test := range []struct {
		name string
		tcp  time.Duration
		icmp icmpStats
		want bool
	}{
		{name: "strong mismatch", tcp: 10 * time.Millisecond, icmp: icmpStats{Available: true, AvgMS: 100}, want: true},
		{name: "normal ratio", tcp: 30 * time.Millisecond, icmp: icmpStats{Available: true, AvgMS: 100}},
		{name: "no ICMP", tcp: 10 * time.Millisecond},
		{name: "all loss", tcp: 10 * time.Millisecond, icmp: icmpStats{Available: true, AvgMS: 100, LossPercent: 100}},
		{name: "local RTT", tcp: 10 * time.Millisecond, icmp: icmpStats{Available: true, AvgMS: 1}},
		{name: "invalid TCP", tcp: 0, icmp: icmpStats{Available: true, AvgMS: 100}},
	} {
		if got := tcpLikelyIntercepted(test.tcp, test.icmp); got != test.want {
			t.Errorf("%s interception = %v, want %v", test.name, got, test.want)
		}
	}

	full := icmpStats{Available: true, LossKnown: true, LossPercent: 0, RTTKnown: true, MinMS: 10, AvgMS: 12, MaxMS: 15, StdDevMS: 1.8, StdDevKnown: true}
	result := model.NewResult("latency", "latency")
	appendICMPMeasurementsForFamily(&result, "fixture", "4", full)
	for _, key := range []string{"icmp_min_ms_fixture_ipv4", "icmp_avg_ms_fixture_ipv4", "icmp_max_ms_fixture_ipv4", "icmp_mdev_ms_fixture_ipv4", "icmp_loss_percent_fixture_ipv4"} {
		if !hasMeasurement(result, key) {
			t.Fatalf("missing ICMP measurement %q", key)
		}
	}
	if result.Measurements[0].HigherIsBetter == nil || *result.Measurements[0].HigherIsBetter {
		t.Fatal("ICMP latency must be lower-is-better")
	}
	busybox := full
	busybox.StdDevKnown = false
	appendICMPMeasurementsForFamily(&result, "fixture", "6", busybox)
	if hasMeasurement(result, "icmp_mdev_ms_fixture_ipv6") || !hasMeasurement(result, "icmp_avg_ms_fixture_ipv6") {
		t.Fatal("ICMP without standard deviation emitted mdev")
	}
	before := len(result.Measurements)
	appendICMPMeasurementsForFamily(&result, "fixture", "4", icmpStats{})
	if len(result.Measurements) != before || formatICMPMilliseconds(-1) != "n/a" || formatICMPMilliseconds(1.5) != "1.50 ms" {
		t.Fatal("ICMP unavailable/format contract failed")
	}
}
