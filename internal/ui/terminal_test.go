package ui

import (
	"bytes"
	"strings"
	"testing"

	"ecs/internal/model"
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
