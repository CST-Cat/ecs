package probe

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestStabilizeSpeedResultUsesMetricPresenceNotLegacyStatusText(t *testing.T) {
	result := model.Result{
		ID:     "speed",
		Status: model.StatusOK,
		Fields: []model.Field{{Key: "threads", Label: "legacy", Value: "4"}},
		Measurements: []model.Measurement{{
			Key: "iperf3_target_01_4_upload_mbps", Value: 10, Display: "10 Mbps",
		}},
		Tables: []model.Table{{
			Key:     "network.iperf3.results",
			Columns: []string{"old"},
			Rows:    [][]string{{"target", "location", "IPv4", "10 Mbps", "20 Mbps", "0%", "1 ms", "5201", "legacy-status-text"}},
		}},
	}

	stabilizeSpeedResult(&result)
	if result.Title != "module.speed.title" || result.Fields[0].Label != "probe.speed.field.threads" {
		t.Fatalf("unstable speed metadata: %#v", result)
	}
	if result.Measurements[0].Label != "probe.speed.metric.upload" {
		t.Fatalf("measurement label = %q", result.Measurements[0].Label)
	}
	if got := result.Tables[0].Rows[0][8]; got != "probe.speed.status.complete" {
		t.Fatalf("status = %q", got)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.speed.summary.values" {
		t.Fatalf("summary messages = %#v", result.SummaryMessages)
	}
}

func TestStabilizeCNSpeedResultUsesDownloadEvidenceForStatus(t *testing.T) {
	result := model.Result{
		ID:     "cnspeed",
		Status: model.StatusWarning,
		Fields: []model.Field{{Key: "node_list", Label: "legacy", Value: "legacy-source"}},
		Measurements: []model.Measurement{{
			Key: "cnspeed_telecom_download_mbps", Value: 20, Display: "20 Mbps",
		}},
		Tables: []model.Table{{
			Key:     "network.cnspeed.nodes",
			Columns: []string{"old"},
			Rows:    [][]string{{"电信", "node", "上海", "3 ms", "—", "—", "arbitrary-external-error"}},
		}},
	}

	stabilizeCNSpeedResult(&result)
	row := result.Tables[0].Rows[0]
	if row[0] != "probe.cnspeed.carrier.telecom" || row[len(row)-1] != "probe.cnspeed.status.failed" {
		t.Fatalf("normalized row = %#v", row)
	}
	if result.Fields[0].Value != "speedtest.cn-CN-ID@audited-commit" {
		t.Fatalf("node list value = %q", result.Fields[0].Value)
	}
	if result.SummaryMessages[0].Key != "probe.cnspeed.summary.values" {
		t.Fatalf("summary messages = %#v", result.SummaryMessages)
	}
}

func TestStabilizeOoklaResultUsesConfigFailuresAndMetrics(t *testing.T) {
	runtime := config.Runtime{Exposure: config.ExposureThirdParty}
	result := model.Result{
		ID:     "ookla",
		Status: model.StatusWarning,
		Fields: []model.Field{
			{Key: "server_selection", Label: "legacy", Value: "arbitrary-display-text"},
			{Key: "isp_auto", Label: "legacy", Value: "fixture"},
		},
		Measurements: []model.Measurement{{
			Key: "auto_download_mbps", Value: 100, Display: "100 Mbps",
		}},
		Failures: []model.Failure{{Stage: "execute", Target: "自动", Category: model.FailureUnknown}},
		Tables: []model.Table{{
			Key:     "network.ookla.results",
			Columns: []string{"old"},
			Rows:    [][]string{{"自动", "fixture", "5 ms", "100 Mbps", "20 Mbps", "0%", "arbitrary-status-text"}},
		}},
	}

	stabilizeOoklaResult(&result, runtime)
	if result.Fields[0].Value != "automatic" || result.Fields[1].Label != "probe.ookla.field.isp" {
		t.Fatalf("fields = %#v", result.Fields)
	}
	if got := result.Tables[0].Rows[0][6]; got != "probe.ookla.status.partial" {
		t.Fatalf("status = %q", got)
	}
	if result.SummaryMessages[0].Key != "probe.ookla.summary.values" {
		t.Fatalf("summary messages = %#v", result.SummaryMessages)
	}
}

func TestStabilizeOoklaSkipFieldsUseRuntimeAndFailureFacts(t *testing.T) {
	result := model.Result{
		ID:     "ookla",
		Status: model.StatusSkipped,
		Fields: []model.Field{
			{Key: "skip_reason", Value: "arbitrary-display-text"},
			{Key: "next_step", Value: "arbitrary-display-text"},
		},
	}
	stabilizeOoklaResult(&result, config.Runtime{Exposure: config.ExposureLocal})
	if result.Fields[0].Value != "exposure_denied" || result.Fields[1].Value != "rerun_with_more_exposure" {
		t.Fatalf("skip fields = %#v", result.Fields)
	}
}
