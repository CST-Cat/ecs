package probe

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestZstdStage9BoundaryUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("zstd", "zstd 压缩性能")
	result.Description = "旧中文说明"
	result.Methodology = model.Methodology{Label: "标准基准", Profile: "旧 profile", ComparisonScope: "旧 scope"}
	result.Fields = []model.Field{
		{Key: "cpu_allowance", Label: "可用 CPU", Value: "8 线程（cgroup 配额 2.00 核）"},
		{Key: "corpus_construction", Label: "corpus 构造", Value: "dickens,mozilla（原字节顺序拼接）"},
	}
	result.Measurements = []model.Measurement{
		{Key: "zstd_compress_1t_mb_s", Label: "旧压缩标签", Value: 40, Unit: "MB/s", Display: "40.00 MB/s"},
		{Key: "zstd_compress_nt_mb_s", Label: "旧多线程标签", Value: 80, Unit: "MB/s", Display: "80.00 MB/s"},
		{Key: "zstd_compress_scaling_ratio", Label: "旧扩展标签", Value: 2, Unit: "x", Display: "2.00×"},
	}
	result.Tables = []model.Table{{
		Key:     "benchmark.zstd.throughput",
		Title:   "旧中文表名",
		Columns: []string{"线程上下文", "压缩吞吐", "解压吞吐", "压缩扩展", "解压扩展", "压缩每 worker 效率", "解压每 worker 效率"},
		Rows: [][]string{
			{"1 worker", "40.00 MB/s", "20.00 MB/s", "1.00 x", "1.00 x", "100.0 %", "100.0 %"},
			{"全 worker（2）", "80.00 MB/s", "20.00 MB/s", "2.00 x", "1.00 x", "100.0 %", "50.0 %"},
		},
	}}
	result.TextBlocks = []model.TextBlock{{Title: "旧中文原始输出", Language: "text", Content: "official zstd output"}}
	result.Sources = []model.Source{{Name: "Zstandard", Purpose: "旧中文用途"}}
	result.Notes = []string{"旧中文 note"}

	allowance := cpuAllowance{Visible: 8, Quota: 2, Threads: 2, Source: "fixture-quota"}
	stabilizeZstdResult(&result, allowance)

	if result.Title != "module.zstd.title" || result.Description != "probe.zstd.description" || result.Methodology.Label != "methodology.standard-benchmark" {
		t.Fatalf("zstd presentation identity not stabilized: %+v", result)
	}
	if result.Summary != "" || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.zstd.summary.values" {
		t.Fatalf("zstd summary is not a stable message: summary=%q messages=%+v", result.Summary, result.SummaryMessages)
	}
	if result.Fields[0].Value != "visible=8;quota=2.00;threads=2;source=fixture-quota" || strings.Contains(result.Fields[1].Value, "拼接") {
		t.Fatalf("zstd machine fields not stabilized: %+v", result.Fields)
	}
	for _, field := range result.Fields {
		if !strings.HasPrefix(field.Label, "probe.zstd.field.") {
			t.Fatalf("source-language field label crossed boundary: %+v", field)
		}
	}
	for _, measurement := range result.Measurements {
		if !strings.HasPrefix(measurement.Label, "probe.zstd.metric.") {
			t.Fatalf("source-language measurement label crossed boundary: %+v", measurement)
		}
	}
	if result.Tables[0].Title != "probe.zstd.table.title" || result.Tables[0].Rows[0][0] != "1T" || result.Tables[0].Rows[1][0] != "NT(2T)" {
		t.Fatalf("zstd table not stabilized: %+v", result.Tables)
	}
	if result.TextBlocks[0].Title != "probe.zstd.raw_output" || result.TextBlocks[0].Content != "official zstd output" || result.Sources[0].Purpose != "probe.zstd.source.zstandard" {
		t.Fatalf("zstd raw evidence or source metadata incorrect: blocks=%+v sources=%+v", result.TextBlocks, result.Sources)
	}
	for _, note := range result.Notes {
		if note == "旧中文 note" {
			t.Fatalf("legacy source-language note crossed boundary: %v", result.Notes)
		}
	}
}
