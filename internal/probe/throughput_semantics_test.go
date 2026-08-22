package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestStabilizeSpeedResultUsesMachinePresentationKeys(t *testing.T) {
	result := model.Result{
		ID:     "speed",
		Status: model.StatusOK,
		Fields: []model.Field{{Key: "threads", Label: "并发流", Value: "4"}},
		Measurements: []model.Measurement{{
			Key: "iperf3_target_01_4_upload_mbps", Value: 10, Display: "10 Mbps",
		}},
		Tables: []model.Table{{
			Key:     "network.iperf3.results",
			Columns: []string{"旧"},
			Rows:    [][]string{{"target", "完成"}},
		}},
	}

	stabilizeSpeedResult(&result)
	if result.Title != "module.speed.title" || result.Fields[0].Label != "probe.speed.field.threads" {
		t.Fatalf("unstable speed metadata: %#v", result)
	}
	if result.Measurements[0].Label != "probe.speed.metric.upload" {
		t.Fatalf("measurement label = %q", result.Measurements[0].Label)
	}
	if got := result.Tables[0].Rows[0][1]; got != "probe.speed.status.complete" {
		t.Fatalf("status = %q", got)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.speed.summary.values" {
		t.Fatalf("summary messages = %#v", result.SummaryMessages)
	}
}

func TestStabilizeCNSpeedResultNormalizesCarrierAndFailureStatus(t *testing.T) {
	result := model.Result{
		ID:     "cnspeed",
		Status: model.StatusWarning,
		Fields: []model.Field{{Key: "node_list", Label: "节点清单来源", Value: "中文来源"}},
		Measurements: []model.Measurement{{
			Key: "cnspeed_telecom_download_mbps", Value: 20, Display: "20 Mbps",
		}},
		Tables: []model.Table{{
			Key:     "network.cnspeed.nodes",
			Columns: []string{"旧"},
			Rows:    [][]string{{"电信", "node", "上海", "3 ms", "20 Mbps", "1 MiB", "下载失败：fixture"}},
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

func TestStabilizeOoklaResultNormalizesDynamicFields(t *testing.T) {
	result := model.Result{
		ID:     "ookla",
		Status: model.StatusOK,
		Fields: []model.Field{
			{Key: "server_selection", Label: "服务器选择", Value: "Ookla 自动选择（未配置三网服务器 ID）"},
			{Key: "isp_auto", Label: "自动 ISP", Value: "fixture"},
		},
		Measurements: []model.Measurement{{
			Key: "auto_download_mbps", Value: 100, Display: "100 Mbps",
		}},
		Tables: []model.Table{{
			Key:     "network.ookla.results",
			Columns: []string{"旧"},
			Rows:    [][]string{{"自动", "fixture", "5 ms", "100 Mbps", "20 Mbps", "0%", "部分完成"}},
		}},
	}

	stabilizeOoklaResult(&result)
	if result.Fields[0].Value != "automatic" || result.Fields[1].Label != "probe.ookla.field.isp" {
		t.Fatalf("fields = %#v", result.Fields)
	}
	if got := result.Tables[0].Rows[0][len(result.Tables[0].Rows[0])-1]; got != "probe.ookla.status.partial" {
		t.Fatalf("status = %q", got)
	}
	if result.SummaryMessages[0].Key != "probe.ookla.summary.values" {
		t.Fatalf("summary messages = %#v", result.SummaryMessages)
	}
}
