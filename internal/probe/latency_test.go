package probe

import (
	"context"
	"net"
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

func TestLatencyProducerDirectResult(t *testing.T) {
	t.Run("skip without targets", func(t *testing.T) {
		result := (latencyProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		if result.Status != model.StatusSkipped || result.Title != "module.latency.title" || result.Description != "probe.latency.description" {
			t.Fatalf("latency skip result = %+v", result)
		}
		if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 0 || result.Evidence.Unit != "sample" {
			t.Fatalf("latency skip evidence = %+v", result.Evidence)
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.latency.summary.skipped" {
			t.Fatalf("latency skip summary = %+v", result.SummaryMessages)
		}
		if got := strings.Join(result.Notes, ","); got != "probe.latency.note.resolution,probe.latency.note.region,probe.latency.note.icmp_unavailable" {
			t.Fatalf("latency skip notes = %q", got)
		}
	})

	t.Run("success", func(t *testing.T) {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		accepted := make(chan int, 1)
		go func() {
			count := 0
			for count < 2 {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					break
				}
				count++
				_ = connection.Close()
			}
			accepted <- count
		}()

		t.Setenv("PATH", t.TempDir())
		result := (latencyProbe{}).Run(context.Background(), Environment{
			Config: config.Runtime{
				IPVersion:       config.IPVersion4,
				LatencyAttempts: 2,
				LatencyTargets:  []config.Endpoint{{Name: "fixture", Address: listener.Addr().String(), Kind: "local", Family: config.IPVersion4}},
			},
			Network: NetworkCapabilities{IPv4Usable: true},
		})
		if got := <-accepted; got != 2 {
			t.Fatalf("latency successful TCP connections = %d, want 2", got)
		}
		if result.Status != model.StatusOK || result.Title != "module.latency.title" || result.Description != "probe.latency.description" {
			t.Fatalf("latency success status/metadata = %s/%+v", result.Status, result)
		}
		if result.Methodology.Label != "methodology.protocol-measurement" || result.Methodology.Profile != "probe.latency.profile" || result.Methodology.ComparisonScope != "probe.latency.comparison_scope" {
			t.Fatalf("latency success methodology = %+v", result.Methodology)
		}
		if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 2 || result.Evidence.Unit != "sample" {
			t.Fatalf("latency success evidence = %+v", result.Evidence)
		}
		if result.StartedAt.IsZero() || len(result.Failures) != 0 {
			t.Fatalf("latency success completion/failures = %s/%+v", result.StartedAt, result.Failures)
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.latency.summary.values" {
			t.Fatalf("latency success summary = %+v", result.SummaryMessages)
		}
		if len(result.Notes) != 3 || result.Notes[0] != "probe.latency.note.resolution" || result.Notes[1] != "probe.latency.note.region" || result.Notes[2] != "probe.latency.note.icmp_unavailable" {
			t.Fatalf("latency success notes = %v", result.Notes)
		}
		if len(result.Measurements) != 5 || result.Measurements[0].Label != "probe.latency.metric.tcp" || result.Measurements[4].Label != "probe.latency.metric.best_median" {
			t.Fatalf("latency success measurements = %+v", result.Measurements)
		}
		for _, measurement := range result.Measurements {
			if _, ok := measurement.Display.Raw(); !ok {
				t.Fatalf("latency measurement display is not raw: %+v", measurement)
			}
		}
		if len(result.Tables) != 1 || result.Tables[0].Title != "probe.latency.table.tcp_icmp" || len(result.Tables[0].Columns) != 13 || len(result.Tables[0].Rows) != 1 {
			t.Fatalf("latency success table = %+v", result.Tables)
		}
		for index, column := range result.Tables[0].Columns {
			if column.Key == "" || column.Label == "" {
				t.Fatalf("latency success column %d = %+v", index, column)
			}
		}
		row := result.Tables[0].Rows[0]
		if len(row) != len(result.Tables[0].Columns) || row[0].Text() != "fixture" || row[1].Text() != "IPv4" || row[2].Text() != "local" {
			t.Fatalf("latency success raw cells = %+v", row)
		}
		for index, cell := range row {
			if index == len(row)-1 {
				continue
			}
			if _, ok := cell.Raw(); !ok {
				t.Fatalf("latency success cell %d is not raw: %+v", index, cell)
			}
		}
		if key, ok := row[12].Key(); !ok || key != "probe.latency.status.no_resolution" {
			t.Fatalf("latency success resolution status = %+v", row[12])
		}
	})

	t.Run("all failed resolution", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		result := (latencyProbe{}).Run(context.Background(), Environment{
			Config: config.Runtime{
				IPVersion:       config.IPVersion4,
				LatencyAttempts: 1,
				LatencyTargets:  []config.Endpoint{{Name: "bad", Address: "not-an-address", Family: config.IPVersion4}},
			},
			Network: NetworkCapabilities{IPv4Usable: true},
		})
		if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 1 {
			t.Fatalf("latency all-failed status/evidence = %s/%+v", result.Status, result.Evidence)
		}
		if len(result.Failures) != 1 || result.Failures[0].Stage != "resolve" || result.Failures[0].Message == "" {
			t.Fatalf("latency all-failed failure = %+v", result.Failures)
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.latency.summary.all_failed" {
			t.Fatalf("latency all-failed summary = %+v", result.SummaryMessages)
		}
		if key, ok := result.Tables[0].Rows[0][12].Key(); !ok || key != "probe.latency.status.resolve_failed" {
			t.Fatalf("latency resolution failure status = %+v", result.Tables[0].Rows[0][12])
		}
	})
}
