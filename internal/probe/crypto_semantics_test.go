package probe

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestStabilizeCryptoResultUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("crypto", "服务器密码学性能")
	result.Description = "独立的服务器密码学吞吐"
	result.Methodology = cryptoMethodology()
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "OpenSSL speed"},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: "legacy"},
		{Key: "arguments_aes_256_gcm_1w", Label: "完整参数（AES）", Value: "openssl speed"},
	}
	result.Measurements = []model.Measurement{{Key: "openssl_aes_256_gcm_1w_mb_s", Label: "legacy", Value: 100, Display: "100 MB/s"}}
	result.Tables = []model.Table{{Key: "benchmark.openssl.results", Title: "legacy", Columns: []string{"a", "b"}, Rows: [][]string{{"AES", "old", "", "", "", ""}, {"AES", "old", "", "", "", ""}}}}
	result.TextBlocks = []model.TextBlock{{Title: "legacy", Content: "OpenSSL raw"}}
	result.Sources = []model.Source{{Name: "OpenSSL speed", Purpose: "legacy"}}
	result.Notes = []string{"legacy"}
	result.Summary = "legacy summary"
	stabilizeCryptoResult(&result, cpuAllowance{Threads: 2, Visible: 2})
	if result.Title != "module.crypto.title" || result.Description != "probe.crypto.description" || result.Summary != "" {
		t.Fatalf("unstable crypto header: %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.crypto.summary.values" {
		t.Fatalf("crypto summary message = %+v", result.SummaryMessages)
	}
	if result.Fields[0].Label != "probe.crypto.field.engine" || result.Fields[1].Value == "legacy" || result.Fields[2].Label != "probe.crypto.field.arguments" {
		t.Fatalf("crypto fields = %+v", result.Fields)
	}
	if result.Measurements[0].Label != "probe.crypto.metric.aes_256_gcm.1w" {
		t.Fatalf("crypto measurement = %+v", result.Measurements[0])
	}
	if result.Tables[0].Title != "probe.crypto.table.title" || result.Tables[0].Columns[0] != "probe.crypto.column.algorithm" || result.Tables[0].Rows[1][1] != "NW(2W)" {
		t.Fatalf("crypto table = %+v", result.Tables[0])
	}
	if result.TextBlocks[0].Title != "probe.crypto.raw_output" || result.Sources[0].Purpose != "probe.crypto.source.openssl" {
		t.Fatalf("crypto evidence metadata = %+v/%+v", result.TextBlocks, result.Sources)
	}
	for _, note := range result.Notes {
		if !strings.HasPrefix(note, "probe.crypto.") {
			t.Fatalf("crypto note is not a stable key: %q", note)
		}
	}
}
