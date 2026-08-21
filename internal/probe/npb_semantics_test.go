package probe

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestNPBStage9BoundaryUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("npb", "NAS Parallel Benchmarks")
	result.Description = "旧中文说明"
	result.Methodology = model.Methodology{Label: "标准基准", Profile: "旧 profile", ComparisonScope: "旧 scope"}
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "NAS Parallel Benchmarks"},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: "8 线程（cgroup 配额 2.00 核）"},
	}
	result.Measurements = []model.Measurement{
		{Key: "npb_ep_1t_mops", Label: "旧 EP 标签", Value: 100, Unit: "Mop/s", Display: "100.00 Mop/s"},
		{Key: "npb_ep_nt_mops", Label: "旧 EP NT 标签", Value: 200, Unit: "Mop/s", Display: "200.00 Mop/s"},
		{Key: "npb_ep_scaling_ratio", Label: "旧扩展标签", Value: 2, Unit: "x", Display: "2.00×"},
	}
	result.Tables = []model.Table{{
		Key:        "benchmark.npb.results",
		Title:      "旧中文表名",
		Columns:    []string{"Benchmark", "负载", "线程上下文", "Mop/s total", "Mop/s/thread", "耗时", "扩展倍率", "验证"},
		ColumnKeys: []string{"benchmark", "load", "worker_context", "mops_total", "mops_per_thread", "elapsed_seconds", "scaling_ratio", "verification"},
		Rows: [][]string{
			{"EP", "随机数 embarrassingly parallel", "1T", "100.00 Mop/s", "100.00 Mop/s", "1.25 s", "1.00 x", "SUCCESSFUL"},
			{"EP", "随机数 embarrassingly parallel", "全线程（2T）", "200.00 Mop/s", "100.00 Mop/s", "0.75 s", "2.00 x", "SUCCESSFUL"},
		},
	}}
	result.TextBlocks = []model.TextBlock{{Title: "旧中文原始输出", Language: "text", Content: "official raw output"}}
	result.Sources = []model.Source{{Name: "NPB", URL: "https://www.nas.nasa.gov/software/npb.html", Purpose: "旧中文用途"}}
	result.Notes = []string{"旧中文 note"}

	allowance := cpuAllowance{Visible: 8, Quota: 2, Threads: 2, Source: "fixture-quota"}
	stabilizeNPBResult(&result, allowance)

	if result.Title != "module.npb.title" || result.Description != "probe.npb.description" || result.Methodology.Label != "methodology.standard-benchmark" {
		t.Fatalf("NPB presentation identity not stabilized: %+v", result)
	}
	if result.Summary != "" || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.npb.summary.values" {
		t.Fatalf("NPB summary is not a stable message: summary=%q messages=%+v", result.Summary, result.SummaryMessages)
	}
	if result.Fields[0].Label != "probe.npb.field.engine" || result.Fields[1].Label != "probe.npb.field.cpu_allowance" || result.Fields[1].Value != "visible=8;quota=2.00;threads=2;source=fixture-quota" {
		t.Fatalf("NPB fields not stabilized: %+v", result.Fields)
	}
	for _, measurement := range result.Measurements {
		if !strings.HasPrefix(measurement.Label, "probe.npb.metric.") {
			t.Fatalf("source-language measurement label crossed boundary: %+v", measurement)
		}
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.npb.table.title" || result.Tables[0].Rows[0][1] != "probe.npb.workload.ep" || result.Tables[0].Rows[1][2] != "NT(2T)" || result.Tables[0].Rows[1][7] != "probe.npb.verification.successful" {
		t.Fatalf("NPB table not stabilized: %+v", result.Tables)
	}
	if result.TextBlocks[0].Title != "probe.npb.raw_output" || result.TextBlocks[0].Content != "official raw output" {
		t.Fatalf("raw NPB evidence was changed incorrectly: %+v", result.TextBlocks)
	}
	if result.Sources[0].Purpose != "probe.npb.source.purpose" || len(result.Notes) == 0 || result.Notes[0] == "旧中文 note" {
		t.Fatalf("NPB metadata not stabilized: sources=%+v notes=%v", result.Sources, result.Notes)
	}
}
