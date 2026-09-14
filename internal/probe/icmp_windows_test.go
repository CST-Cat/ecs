//go:build windows

package probe

import (
	"context"
	"testing"
	"time"
)

func TestWindowsNativeICMPLoopback(t *testing.T) {
	for _, test := range []struct {
		name   string
		host   string
		family string
	}{
		{name: "IPv4", host: "127.0.0.1", family: "4"},
		{name: "IPv6", host: "::1", family: "6"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stats := runICMPPingFamily(ctx, test.host, 2, time.Second, test.family)
			if stats.Err != nil || !stats.Available || !stats.LossKnown {
				t.Fatalf("native loopback stats = %+v", stats)
			}
			if stats.LossPercent != 0 || !stats.RTTKnown || !stats.StdDevKnown || stats.AvgMS < 0 || stats.MinMS < 0 || stats.MaxMS < 0 {
				t.Fatalf("native loopback result = %+v", stats)
			}
		})
	}
}

func TestWindowsNativeICMPDeadlineDoesNotHang(t *testing.T) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	stats := runICMPPingFamily(ctx, "192.0.2.1", 4, 5*time.Second, "4")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("native ICMP deadline took %s, want under one second (stats=%+v)", elapsed, stats)
	}
}
