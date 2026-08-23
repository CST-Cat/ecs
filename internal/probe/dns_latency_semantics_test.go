package probe

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestStabilizeDNSResultUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("dns", "DNS 质量")
	result.Description = "legacy"
	result.Measurements = []model.Measurement{{Key: "best_dns_median_ms", Label: "legacy-label", Display: "10 ms", Value: 10}}
	result.Tables = []model.Table{{Key: "network.dns.resolvers", Title: "legacy-table", Columns: []string{"old"}, Rows: [][]string{{"r", "a", "1/1", "10", "11", "1", dnsStatusOK}}}}
	result.Evidence = model.NewEvidence(1, 1, "query")
	stabilizeDNSResult(&result)
	if result.Title != "module.dns.title" || result.Description != "probe.dns.description" {
		t.Fatalf("dns header = %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.dns.summary.values" {
		t.Fatalf("dns summary = %+v", result.SummaryMessages)
	}
	if result.Measurements[0].Label != "probe.dns.metric.best_median" || result.Tables[0].Rows[0][6] != "probe.dns.status.ok" {
		t.Fatalf("dns metadata = %+v/%+v", result.Measurements, result.Tables[0])
	}
}

func TestStabilizeLatencyResultUsesStructuredResolutionFailure(t *testing.T) {
	result := model.NewResult("latency", "legacy-title")
	result.Description = "legacy"
	result.Measurements = []model.Measurement{{Key: "best_tcp_median_ms", Label: "legacy-label", Display: "20 ms", Value: 20}}
	result.Tables = []model.Table{{Key: "network.latency.tcp_icmp", Title: "legacy-table", Columns: []string{"old"}, Rows: [][]string{{"target", "IPv4", "region", "1/1", "20", "21", "1", "n/a", "n/a", "n/a", "n/a", "n/a", "legacy-resolution-text"}}}}
	result.Failures = []model.Failure{{Stage: "resolve", Target: "example.com:443", Category: model.FailureDNS}}
	result.Evidence = model.NewEvidence(1, 1, "sample")
	targets := []config.Endpoint{{Name: "target", Address: "example.com:443"}}
	stabilizeLatencyResult(&result, targets)
	if result.Title != "module.latency.title" || result.Description != "probe.latency.description" {
		t.Fatalf("latency header = %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.latency.summary.values" {
		t.Fatalf("latency summary = %+v", result.SummaryMessages)
	}
	if result.Measurements[0].Label != "probe.latency.metric.best_median" || result.Tables[0].Rows[0][12] != "probe.latency.status.resolve_failed" {
		t.Fatalf("latency metadata = %+v/%+v", result.Measurements, result.Tables[0])
	}
}

func TestStabilizeLatencyResultRecognizesLiteralAddressWithoutDisplayText(t *testing.T) {
	result := model.NewResult("latency", "legacy-title")
	result.Tables = []model.Table{{Key: "network.latency.tcp_icmp", Rows: [][]string{{"literal", "IPv4", "region", "1/1", "20", "21", "1", "n/a", "n/a", "n/a", "n/a", "n/a", "legacy-resolution-text"}}}}
	stabilizeLatencyResult(&result, []config.Endpoint{{Name: "literal", Address: "192.0.2.1:443"}})
	if got := result.Tables[0].Rows[0][12]; got != "probe.latency.status.no_resolution" {
		t.Fatalf("literal resolution status = %q", got)
	}
}
