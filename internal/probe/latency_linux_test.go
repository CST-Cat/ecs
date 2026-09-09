//go:build linux

package probe

import (
	"testing"

	"ecs/internal/model"
)

func assertLatencySuccessPlatform(t *testing.T, result model.Result) {
	t.Helper()
	if len(result.Notes) != 3 || result.Notes[2] != "probe.latency.note.icmp_unavailable" {
		t.Fatalf("Linux latency success notes = %v", result.Notes)
	}
	wantMeasurementKeys := []string{
		"tcp_target_01_ipv4_success_percent",
		"tcp_target_01_ipv4_p50_ms",
		"tcp_target_01_ipv4_p95_ms",
		"tcp_target_01_ipv4_jitter_ms",
		"best_tcp_median_ms",
	}
	if len(result.Measurements) != len(wantMeasurementKeys) {
		t.Fatalf("Linux latency success measurements = %d, want %d: %+v", len(result.Measurements), len(wantMeasurementKeys), result.Measurements)
	}
	for index, want := range wantMeasurementKeys {
		if result.Measurements[index].Key != want {
			t.Fatalf("Linux latency measurement %d key = %q, want %q", index, result.Measurements[index].Key, want)
		}
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 1 || len(result.Tables[0].Rows[0]) < 12 {
		t.Fatalf("Linux latency success table shape = %+v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	for index := 7; index <= 11; index++ {
		if got := row[index].Text(); got != "n/a" {
			t.Fatalf("Linux unavailable ICMP table cell %d = %q, want n/a", index, got)
		}
	}
}
