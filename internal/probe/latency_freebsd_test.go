//go:build freebsd

package probe

import (
	"testing"

	"ecs/internal/model"
)

func assertLatencySuccessPlatform(t *testing.T, result model.Result) {
	t.Helper()
	if len(result.Notes) != 3 || result.Notes[2] != "probe.latency.note.icmp" {
		t.Fatalf("FreeBSD latency success notes = %v", result.Notes)
	}
	wantMeasurementKeys := []string{
		"tcp_target_01_ipv4_success_percent",
		"tcp_target_01_ipv4_p50_ms",
		"tcp_target_01_ipv4_p95_ms",
		"tcp_target_01_ipv4_jitter_ms",
		"icmp_min_ms_fixture_ipv4",
		"icmp_avg_ms_fixture_ipv4",
		"icmp_max_ms_fixture_ipv4",
		"icmp_mdev_ms_fixture_ipv4",
		"icmp_loss_percent_fixture_ipv4",
		"best_tcp_median_ms",
	}
	if len(result.Measurements) != len(wantMeasurementKeys) {
		t.Fatalf("FreeBSD latency success measurements = %d, want %d: %+v", len(result.Measurements), len(wantMeasurementKeys), result.Measurements)
	}
	for index, want := range wantMeasurementKeys {
		measurement := result.Measurements[index]
		if measurement.Key != want {
			t.Fatalf("FreeBSD latency measurement %d key = %q, want %q", index, measurement.Key, want)
		}
		if index >= 4 && index <= 8 {
			if measurement.Label != "probe.latency.metric.icmp" || measurement.Method != "icmp-echo-v1" {
				t.Fatalf("FreeBSD ICMP measurement %d provenance = label:%q method:%q", index, measurement.Label, measurement.Method)
			}
		}
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 1 || len(result.Tables[0].Rows[0]) < 12 {
		t.Fatalf("FreeBSD latency success table shape = %+v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	for index := 7; index <= 10; index++ {
		if got := row[index].Text(); got == "n/a" || got == "" {
			t.Fatalf("FreeBSD ICMP table fact cell %d = %q, want measured value", index, got)
		}
	}
	if got := row[11].Text(); got != "0 %" {
		t.Fatalf("FreeBSD ICMP table loss = %q, want 0 %%", got)
	}
}
