package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestStabilizeDNSResultUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("dns", "DNS 质量")
	result.Description = "legacy"
	result.Measurements = []model.Measurement{{Key: "best_dns_median_ms", Label: "最佳 DNS P50", Display: "10 ms", Value: 10}}
	result.Tables = []model.Table{{Key: "network.dns.resolvers", Title: "递归解析器", Columns: []string{"old"}, Rows: [][]string{{"r", "a", "1/1", "10", "11", "1", "正常"}}}}
	result.Evidence = model.NewEvidence(1, 1, "query")
	result.Summary = "legacy"
	stabilizeDNSResult(&result)
	if result.Title != "module.dns.title" || result.Description != "probe.dns.description" || result.Summary != "" {
		t.Fatalf("dns header = %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.dns.summary.values" {
		t.Fatalf("dns summary = %+v", result.SummaryMessages)
	}
	if result.Measurements[0].Label != "probe.dns.metric.best_median" || result.Tables[0].Rows[0][6] != "probe.dns.status.ok" {
		t.Fatalf("dns metadata = %+v/%+v", result.Measurements, result.Tables[0])
	}
}

func TestStabilizeLatencyResultUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("latency", "网络延迟")
	result.Description = "legacy"
	result.Measurements = []model.Measurement{{Key: "best_tcp_median_ms", Label: "最佳 TCP P50", Display: "20 ms", Value: 20}}
	result.Tables = []model.Table{{Key: "network.latency.tcp_icmp", Title: "TCP 建连与 ICMP 往返", Columns: []string{"old"}, Rows: [][]string{{"target", "4", "region", "1/1", "20", "21", "1", "n/a", "n/a", "n/a", "n/a", "n/a", "解析失败"}}}}
	result.Evidence = model.NewEvidence(1, 1, "sample")
	stabilizeLatencyResult(&result)
	if result.Title != "module.latency.title" || result.Description != "probe.latency.description" || result.Summary != "" {
		t.Fatalf("latency header = %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.latency.summary.values" {
		t.Fatalf("latency summary = %+v", result.SummaryMessages)
	}
	if result.Measurements[0].Label != "probe.latency.metric.best_median" || result.Tables[0].Rows[0][12] != "probe.latency.status.resolve_failed" {
		t.Fatalf("latency metadata = %+v/%+v", result.Measurements, result.Tables[0])
	}
}
