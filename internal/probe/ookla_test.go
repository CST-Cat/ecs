package probe

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func TestParseOoklaJSONExtractsSafeMeasurementFields(t *testing.T) {
	output := `noise before {"ping":{"jitter":1.25,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0.5,"isp":"Example ISP","interface":{"externalIp":"203.0.113.10","macAddr":"should-not-be-copied"},"server":{"id":42,"name":"Example","location":"Test City","country":"CN"},"result":{"url":"https://example.invalid/result","persisted":true}} trailing`
	parsed, err := parseOoklaJSON([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	if got := ooklaBandwidthMbps(parsed.Download.Bandwidth); got != 1000 {
		t.Fatalf("download Mbps = %v", got)
	}
	if parsed.Interface.ExternalIP != "203.0.113.10" || parsed.Server.ID != 42 {
		t.Fatalf("parsed = %+v", parsed)
	}
	if !parsed.presence.PingJitter || !parsed.presence.PingLatency ||
		!parsed.presence.DownloadBandwidth || !parsed.presence.UploadBandwidth {
		t.Fatalf("measurement field presence = %+v", parsed.presence)
	}
	var result strings.Builder
	appendOoklaMeasurementsToString(&result, parsed)
	if strings.Contains(result.String(), "macAddr") || strings.Contains(result.String(), "example.invalid") {
		t.Fatalf("unsafe raw fields leaked: %q", result.String())
	}
}

// appendOoklaMeasurementsToString mirrors the report's selected fields; it
// keeps this parser test independent from report rendering details.
func appendOoklaMeasurementsToString(builder *strings.Builder, parsed ooklaResult) {
	builder.WriteString(parsed.ISP)
	builder.WriteString(" ")
	builder.WriteString(parsed.Interface.ExternalIP)
	builder.WriteString(" ")
	builder.WriteString(parsed.Server.Name)
}

func TestOoklaBandwidthRejectsNonPositiveValues(t *testing.T) {
	if got := ooklaBandwidthMbps(0); got != 0 {
		t.Fatalf("zero bandwidth = %v", got)
	}
	if got := ooklaBandwidthMbps(-1); got != 0 {
		t.Fatalf("negative bandwidth = %v", got)
	}
}

func TestOoklaRejectsNonFiniteValues(t *testing.T) {
	packetLoss := math.NaN()
	parsed := ooklaResult{}
	parsed.Ping.Latency = math.Inf(1)
	parsed.Ping.Jitter = math.NaN()
	parsed.Download.Bandwidth = math.Inf(1)
	parsed.PacketLoss = &packetLoss

	if ooklaHasValidMetric(parsed) || ooklaMeasurementsComplete(parsed) {
		t.Fatalf("non-finite Ookla values were accepted: %+v", parsed)
	}
	if loss, ok := ooklaPacketLoss(parsed); ok || loss != 0 {
		t.Fatalf("NaN packet loss = %v, %v; want unavailable", loss, ok)
	}
	if got := ooklaLatencyDisplay(parsed.Ping.Latency); got != "—" {
		t.Fatalf("infinite latency display = %q", got)
	}
	if got := ooklaBandwidthDisplay(parsed.Download.Bandwidth); got != "—" {
		t.Fatalf("infinite bandwidth display = %q", got)
	}
}

func TestOoklaRunAcceptsZeroJitterAndPacketLoss(t *testing.T) {
	result := runOoklaFixture(t, `{"ping":{"jitter":0,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0,"server":{"id":42,"name":"Example"}}`)
	if result.Status != model.StatusOK {
		t.Fatalf("status = %s, want ok: %+v", result.Status, result)
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 1 {
		t.Fatalf("Ookla table = %+v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	if row[5] != "0.00 %" {
		t.Fatalf("packet loss cell = %q, want zero measurement", row[5])
	}
	for _, cell := range row[2:6] {
		if cell == "—" {
			t.Fatalf("complete row has unavailable metric: %v", row)
		}
	}
	keys := measurementKeys(result)
	for _, key := range []string{"ookla_ping_ms", "ookla_jitter_ms", "ookla_download_mbps", "ookla_upload_mbps", "ookla_packet_loss_percent"} {
		if !keys[key] {
			t.Fatalf("complete JSON missing measurement %q: %+v", key, result.Measurements)
		}
	}
}

func TestOoklaRunMissingCoreFieldsDoesNotInventSuccess(t *testing.T) {
	tests := []struct {
		name             string
		output           string
		wantMeasurements []string
		wantCells        map[int]string
	}{
		{
			name:   "empty JSON",
			output: `{}`,
			wantCells: map[int]string{
				2: "—", 3: "—", 4: "—", 5: "—",
			},
		},
		{
			name:   "metadata only",
			output: `{"isp":"Example ISP","server":{"id":42},"result":{"persisted":true}}`,
			wantCells: map[int]string{
				2: "—", 3: "—", 4: "—", 5: "—",
			},
		},
		{
			name:   "jitter and loss only",
			output: `{"ping":{"jitter":0},"packetLoss":0}`,
			wantMeasurements: []string{
				"ookla_jitter_ms", "ookla_packet_loss_percent",
			},
			wantCells: map[int]string{
				2: "—", 3: "—", 4: "—", 5: "0.00 %",
			},
		},
		{
			name:   "missing download and upload",
			output: `{"ping":{"jitter":1.25,"latency":8.5},"packetLoss":0}`,
			wantMeasurements: []string{
				"ookla_ping_ms", "ookla_jitter_ms", "ookla_packet_loss_percent",
			},
			wantCells: map[int]string{
				2: "8.50 ms", 3: "—", 4: "—", 5: "0.00 %",
			},
		},
		{
			name:   "missing latency",
			output: `{"ping":{"jitter":1.25},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0}`,
			wantMeasurements: []string{
				"ookla_jitter_ms", "ookla_download_mbps", "ookla_upload_mbps", "ookla_packet_loss_percent",
			},
			wantCells: map[int]string{
				2: "—", 3: "1000.00 Mbps", 4: "200.00 Mbps", 5: "0.00 %",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runOoklaFixture(t, test.output)
			if result.Status == model.StatusOK {
				t.Fatalf("missing core fields reported ok: %+v", result)
			}
			if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 1 {
				t.Fatalf("Ookla table = %+v", result.Tables)
			}
			row := result.Tables[0].Rows[0]
			if row[6] == "完成" {
				t.Fatalf("missing core fields reported complete: %v", row)
			}
			for index, want := range test.wantCells {
				if row[index] != want {
					t.Fatalf("cell[%d] = %q, want %q; row=%v", index, row[index], want, row)
				}
			}
			if row[2] == "0.00 ms" || row[3] == "0.00 Mbps" || row[4] == "0.00 Mbps" {
				t.Fatalf("missing value was rendered as zero: %v", row)
			}
			keys := measurementKeys(result)
			if len(keys) != len(test.wantMeasurements) {
				t.Fatalf("measurement keys = %v, want %v", keys, test.wantMeasurements)
			}
			for _, key := range test.wantMeasurements {
				if !keys[key] {
					t.Fatalf("missing valid measurement %q: %v", key, keys)
				}
			}
			if (test.name == "empty JSON" || test.name == "metadata only" || test.name == "jitter and loss only") && result.Summary != "Ookla 没有返回可用测速结果" {
				t.Fatalf("empty JSON summary = %q, want no usable result", result.Summary)
			}
		})
	}
}

func measurementKeys(result model.Result) map[string]bool {
	keys := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		keys[measurement.Key] = true
	}
	return keys
}

func runOoklaFixture(t *testing.T, output string) model.Result {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "speedtest")
	script := []byte("#!/bin/sh\ncase \"$1\" in\n  --version|-V) printf '%s\\n' 'Speedtest by Ookla 1.2.3' ;;\n  *) printf '%s' \"$OOKLA_TEST_OUTPUT\" ;;\nesac\n")
	if err := os.WriteFile(path, script, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("OOKLA_TEST_OUTPUT", output)
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Exposure = config.ExposureThirdParty
	return (ooklaProbe{}).Run(context.Background(), Environment{Config: cfg})
}

func TestOoklaSkipIncludesReasonAndNextStepInText(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Exposure = config.ExposureThirdParty
	t.Setenv("PATH", t.TempDir())
	result := (ooklaProbe{}).Run(context.Background(), Environment{Config: cfg})
	if result.Status != model.StatusSkipped {
		t.Fatalf("status = %s, want skipped", result.Status)
	}
	if len(result.Fields) < 2 {
		t.Fatalf("skip details missing: %+v", result.Fields)
	}
	out := report.Text(model.Report{
		Tool:    model.ToolInfo{Name: "ecs", Version: "test"},
		Summary: model.Summary{Headline: "1 项跳过"},
		Results: []model.Result{result},
	}, report.TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{"跳过原因", "未找到官方 speedtest 客户端", "下一步", "run.sh 按需准备"} {
		if !strings.Contains(out, want) {
			t.Fatalf("skip text missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "命令参数") || strings.Contains(out, "原始输出") {
		t.Fatalf("skip text should not expose command/raw output:\n%s", out)
	}
}
