package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestLatencyProbeRunWithRealLoopbackTCP(t *testing.T) {
	const attempts = 3

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	acceptDone := make(chan struct{})
	accepted := make(chan struct{}, attempts)
	acceptErr := make(chan error, 1)
	go func() {
		defer close(acceptDone)
		for {
			connection, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					select {
					case acceptErr <- err:
					default:
					}
				}
				return
			}
			_ = connection.Close()
			select {
			case accepted <- struct{}{}:
			default:
			}
		}
	}()
	defer func() {
		_ = listener.Close()
		<-acceptDone
	}()

	closedListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	failedAddress := closedListener.Addr().String()
	if err := closedListener.Close(); err != nil {
		t.Fatal(err)
	}

	successAddress := listener.Addr().String()
	config := config.Runtime{
		IPVersion:       config.IPVersion4,
		LatencyAttempts: attempts,
		LatencyTargets: []config.Endpoint{
			{Name: "loopback-ok", Address: successAddress, Kind: "local"},
			{Name: "loopback-failed", Address: failedAddress, Kind: "local"},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := (latencyProbe{}).Run(ctx, Environment{Config: config})

	for index := 0; index < attempts; index++ {
		select {
		case <-accepted:
		case err := <-acceptErr:
			t.Fatalf("loopback listener failed after %d accepts: %v", index, err)
		case <-time.After(time.Second):
			t.Fatalf("loopback listener accepted %d/%d TCP connections", index, attempts)
		}
	}

	if len(result.Tables) != 1 {
		t.Fatalf("latency tables = %d, want one TCP table", len(result.Tables))
	}
	table := result.Tables[0]
	wantColumns := []string{"目标", "协议", "区域", "成功", "TCP P50", "TCP P95", "标准差", "ICMP 最小", "ICMP 平均", "ICMP 最大", "ICMP mdev", "ICMP 丢包", "DNS 解析"}
	if !reflect.DeepEqual(table.Columns, wantColumns) {
		t.Fatalf("latency table columns = %v, want %v", table.Columns, wantColumns)
	}
	rows := make(map[string][]string, len(table.Rows))
	for _, row := range table.Rows {
		if len(row) != len(table.Columns) {
			t.Fatalf("latency row has %d cells, want %d: %v", len(row), len(table.Columns), row)
		}
		rows[row[0]] = row
	}

	successRow, ok := rows["loopback-ok"]
	if !ok {
		t.Fatalf("missing successful loopback row: %v", rows)
	}
	if successRow[1] != "IPv4" || successRow[3] != fmt.Sprintf("%d/%d", attempts, attempts) {
		t.Fatalf("successful loopback row = %v, want IPv4 and %d/%d", successRow, attempts, attempts)
	}
	if successRow[4] == "n/a" || successRow[5] == "n/a" || !strings.HasSuffix(successRow[4], " ms") || !strings.HasSuffix(successRow[5], " ms") {
		t.Fatalf("successful loopback P50/P95 = %q/%q", successRow[4], successRow[5])
	}
	if successRow[12] != "无需解析" {
		t.Fatalf("literal loopback DNS time = %q, want no lookup", successRow[12])
	}

	failedRow, ok := rows["loopback-failed"]
	if !ok {
		t.Fatalf("missing failed loopback row: %v", rows)
	}
	if failedRow[1] != "IPv4" || failedRow[3] != fmt.Sprintf("0/%d", attempts) {
		t.Fatalf("failed loopback row = %v, want IPv4 and 0/%d", failedRow, attempts)
	}
	if failedRow[4] != "n/a" || failedRow[5] != "n/a" {
		t.Fatalf("failed loopback must not report TCP percentiles: %v", failedRow)
	}

	var bestFound bool
	for _, measurement := range result.Measurements {
		if measurement.Key != "best_tcp_median_ms" {
			continue
		}
		bestFound = true
		if measurement.Value <= 0 || measurement.Unit != "ms" || measurement.Method != "tcp-connect-resolved-v2" {
			t.Fatalf("best TCP measurement = %+v", measurement)
		}
	}
	if !bestFound {
		t.Fatalf("missing best_tcp_median_ms measurement: %+v", result.Measurements)
	}
	keys := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		keys[measurement.Key] = true
	}
	for _, key := range []string{
		"tcp_target_01_ipv4_success_percent", "tcp_target_01_ipv4_p50_ms",
		"tcp_target_01_ipv4_p95_ms", "tcp_target_01_ipv4_jitter_ms",
		"tcp_target_02_ipv4_success_percent",
	} {
		if !keys[key] {
			t.Errorf("latency result missing %q: %+v", key, result.Measurements)
		}
	}
	if result.Evidence == nil || result.Evidence.Valid != attempts || result.Evidence.Expected != attempts*2 {
		t.Fatalf("latency evidence = %+v, want %d/%d", result.Evidence, attempts, attempts*2)
	}
	if !strings.Contains(result.Summary, "loopback-ok") || !strings.Contains(result.Summary, "P50") {
		t.Fatalf("latency summary = %q", result.Summary)
	}
}

func TestAppendICMPMeasurementsRetainsCompleteStatistics(t *testing.T) {
	var result model.Result
	appendICMPMeasurements(&result, "loopback", icmpStats{
		Available: true, LossKnown: true, LossPercent: 0,
		RTTKnown: true, MinMS: 1.25, AvgMS: 2.5, MaxMS: 4.75,
		StdDevKnown: true, StdDevMS: 0.5,
	})

	want := map[string]float64{
		"icmp_min_ms_loopback":       1.25,
		"icmp_avg_ms_loopback":       2.5,
		"icmp_max_ms_loopback":       4.75,
		"icmp_mdev_ms_loopback":      0.5,
		"icmp_loss_percent_loopback": 0,
	}
	if len(result.Measurements) != len(want) {
		t.Fatalf("ICMP measurement count = %d, want %d: %+v", len(result.Measurements), len(want), result.Measurements)
	}
	for _, measurement := range result.Measurements {
		value, ok := want[measurement.Key]
		if !ok || measurement.Value != value || measurement.Method != "icmp-echo-v1" {
			t.Errorf("unexpected ICMP measurement: %+v", measurement)
		}
		delete(want, measurement.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing ICMP measurements: %v", want)
	}
}

func TestAppendICMPMeasurementsKeepsIPFamiliesDistinct(t *testing.T) {
	var result model.Result
	stats := icmpStats{Available: true, LossKnown: true, LossPercent: 0}
	appendICMPMeasurementsForFamily(&result, "Cloudflare", "4", stats)
	appendICMPMeasurementsForFamily(&result, "Cloudflare", "6", stats)
	if len(result.Measurements) != 2 ||
		result.Measurements[0].Key != "icmp_loss_percent_cloudflare_ipv4" ||
		result.Measurements[1].Key != "icmp_loss_percent_cloudflare_ipv6" {
		t.Fatalf("family-specific ICMP measurements = %+v", result.Measurements)
	}
}

func TestICMPLossOnlyDoesNotInventRTT(t *testing.T) {
	var result model.Result
	appendICMPMeasurements(&result, "unreachable", icmpStats{Available: true, LossKnown: true, LossPercent: 100})
	if len(result.Measurements) != 1 || result.Measurements[0].Key != "icmp_loss_percent_unreachable" {
		t.Fatalf("loss-only ICMP measurements = %+v", result.Measurements)
	}
}
