package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

func TestNetworkSummaryPrintsDetailedTables(t *testing.T) {
	var output bytes.Buffer
	terminal := New(&output, true)
	terminal.Summary(model.Report{
		Summary: model.Summary{Headline: "完成"},
		Results: []model.Result{{
			ID:     "network",
			Title:  "网络与 IP 质量",
			Status: model.StatusOK,
			Fields: []model.Field{{
				Key:   "ipv4_ip_type",
				Label: "IPv4 IP 类型",
				Value: "原生 IP",
			}},
			Tables: []model.Table{{
				Title:   "IPv4 · 风险评分",
				Columns: []string{"数据库", "风险值"},
				Rows:    [][]string{{"IPQS", "87/100"}},
			}},
		}},
	}, nil)
	text := output.String()
	for _, expected := range []string{"IP 质量明细", "原生 IP", "IPv4 · 风险评分", "IPQS", "87/100"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("terminal output missing %q:\n%s", expected, text)
		}
	}
}

func TestDisplayWidthCountsCJKAsTwoColumns(t *testing.T) {
	if got := displayWidth("IP 风险"); got != 7 {
		t.Fatalf("displayWidth = %d, want 7", got)
	}
	if got := displayWidth("IPQS"); got != 4 {
		t.Fatalf("ASCII displayWidth = %d, want 4", got)
	}
}

func TestFullReportPrintsCompleteTextAndPaths(t *testing.T) {
	var output bytes.Buffer
	terminal := NewWithColor(&output, termcolor.LevelNone)
	terminal.FullReport(model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{Profile: "quick", StartedAt: time.Unix(0, 0).UTC(), Redacted: true},
		Summary:       model.Summary{Status: model.StatusOK, Headline: "完成"},
		Results: []model.Result{{
			ID: "full", Title: "完整结果", Status: model.StatusOK, Summary: "全部完成",
			Methodology: model.Methodology{Kind: "inventory", Label: "事实采集", Engine: "engine"},
			Measurements: []model.Measurement{
				{Key: "one", Label: "指标一", Display: "1 unit"},
				{Key: "two", Label: "指标二", Display: "2 unit"},
				{Key: "three", Label: "指标三", Display: "3 unit"},
			},
			Fields:     []model.Field{{Key: "field", Label: "字段", Value: "字段值"}},
			Tables:     []model.Table{{Title: "表格", Columns: []string{"列"}, Rows: [][]string{{"表格值"}}}},
			TextBlocks: []model.TextBlock{{Title: "文本", Content: "文本块内容"}},
			Notes:      []string{"备注"},
		}},
	}, map[string]string{"json": "/tmp/report.json", "txt": "/tmp/report.txt"}, &score.Report{Total: 800, Covered: 1, Possible: 1}, termcolor.LevelNone)
	text := output.String()
	for _, want := range []string{
		"指标一", "指标二", "指标三", "字段值", "表格值", "文本块内容", "备注",
		"完整结果", "事实采集", "综合评分", "/tmp/report.json", "/tmp/report.txt", "未上传任何报告",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("full report output missing %q:\n%s", want, text)
		}
	}
}
