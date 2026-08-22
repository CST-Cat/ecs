package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestStabilizeDiskResultUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("disk", "磁盘性能")
	result.Description = "legacy"
	result.Fields = []model.Field{{Key: "engine", Label: "引擎", Value: "fio"}}
	result.Measurements = []model.Measurement{{Key: "fio_sequential_write_mib_s", Label: "fio 顺序写入", Value: 12, Display: "12 MiB/s"}}
	result.Tables = []model.Table{{Key: "disk.fio.crystal", Title: "Crystal", Columns: []string{"old"}, Rows: [][]string{{"RND4K/Q1", "", "", "", "", "", "完成"}}}}
	result.TextBlocks = []model.TextBlock{{Title: "legacy", Content: "fio output"}}
	result.Sources = []model.Source{{Name: "fio", Purpose: "legacy"}}
	result.Summary = "legacy"
	result.Evidence = model.NewEvidence(1, 2, "job")
	stabilizeDiskResult(&result)
	if result.Title != "module.disk.title" || result.Description != "probe.disk.description" || result.Summary != "" {
		t.Fatalf("disk header = %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.disk.summary.values" {
		t.Fatalf("disk summary = %+v", result.SummaryMessages)
	}
	if result.Fields[0].Label != "probe.disk.field.engine" || result.Measurements[0].Label != "probe.disk.metric.fio_sequential_write_mib_s" {
		t.Fatalf("disk metadata = fields:%+v measurements:%+v", result.Fields, result.Measurements)
	}
	if result.Tables[0].Title != "probe.disk.table.crystal" || result.Tables[0].Columns[0] != "probe.disk.column.workload" || result.Tables[0].Rows[0][6] != "probe.disk.status.complete" {
		t.Fatalf("disk table = %+v", result.Tables[0])
	}
	if result.TextBlocks[0].Title != "probe.disk.raw_output" || result.Sources[0].Purpose != "probe.disk.source.fio" {
		t.Fatalf("disk evidence metadata = %+v/%+v", result.TextBlocks, result.Sources)
	}
}
