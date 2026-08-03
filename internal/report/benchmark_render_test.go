package report

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

func benchmarkRenderReport() model.Report {
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: "render-matrix", Profile: "full", StartedAt: time.Unix(0, 0).UTC(), Redacted: true},
		Summary:       model.Summary{Status: model.StatusOK, OK: 2, Headline: "2 项测试完成"},
		Results: []model.Result{
			{
				ID: "memory", Title: "内存性能", Status: model.StatusOK,
				Fields: []model.Field{
					{Key: "balloon_reclaim", Label: "Balloon reclaim", Value: "available"},
					{Key: "ksm_merging", Label: "KSM merging", Value: "unavailable"},
				},
				Tables: []model.Table{{
					Title:          "sysbench memory 读写与时延",
					Columns:        []string{"操作 / 线程", "吞吐", "平均时延", "P95 时延", "证据"},
					Rows:           [][]string{{"单线程读", "100 MiB/s", "2.000 ms", "3.000 ms", "原生"}, {"多线程读", "200 MiB/s", "1.000 ms", "1.500 ms", "派生"}},
					NumericColumns: []int{1, 2, 3}, NumericHigherIsBetter: []bool{true, false, false},
				}},
			},
			{
				ID: "disk", Title: "磁盘性能", Status: model.StatusOK,
				Tables: []model.Table{{
					Title:          "Crystal",
					Columns:        []string{"工作负载", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
					Rows:           [][]string{{"RND4K/Q1", "10 MiB/s", "100 IOPS", "20 MiB/s", "200 IOPS", "完成"}, {"SEQ1M/Q1", "20 MiB/s", "200 IOPS", "40 MiB/s", "400 IOPS", "完成"}},
					NumericColumns: []int{1, 2, 3, 4}, NumericHigherIsBetter: []bool{true, true, true, true},
				}, {
					Title: "ATTO", Columns: []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
					Rows:           [][]string{{"512B", "1 MiB/s", "10 IOPS", "2 MiB/s", "20 IOPS", "完成"}, {"64M", "8 MiB/s", "80 IOPS", "16 MiB/s", "160 IOPS", "完成"}},
					NumericColumns: []int{1, 2, 3, 4}, NumericHigherIsBetter: []bool{true, true, true, true},
				}},
			},
		},
	}
}

func TestBenchmarkTextRenderingHasSectionsBarsAndExplicitAvailability(t *testing.T) {
	plain := Text(benchmarkRenderReport(), TextOptions{Color: termcolor.LevelNone})
	if strings.Contains(plain, "\x1b") {
		t.Fatal("plain benchmark report contains ANSI")
	}
	for _, want := range []string{"内存测评", "硬盘测评", "Crystal", "ATTO", "available", "unavailable", "█", "▒"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain benchmark report missing %q:\n%s", want, plain)
		}
	}
	if strings.Count(plain, "████████") == 0 || strings.Count(plain, "▒▒▒▒") == 0 {
		t.Fatalf("numeric table bars are not proportional:\n%s", plain)
	}
}

func TestBenchmarkTextRenderingUsesPaletteLevels(t *testing.T) {
	for _, testCase := range []struct {
		level  termcolor.Level
		prefix string
	}{
		{termcolor.LevelTrueColor, "\x1b[38;2;"},
		{termcolor.LevelANSI256, "\x1b[38;5;"},
		{termcolor.LevelBasic, "\x1b["},
	} {
		out := Text(benchmarkRenderReport(), TextOptions{Color: testCase.level})
		if !strings.Contains(out, testCase.prefix) {
			t.Fatalf("color level %v did not color a benchmark cell/status", testCase.level)
		}
	}
}

func TestBenchmarkTitlesAreConsistentAcrossRenderers(t *testing.T) {
	data := benchmarkRenderReport()
	markdown := Markdown(data, nil)
	for _, want := range []string{"内存测评", "硬盘测评", "Crystal", "ATTO"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
	if !strings.Contains(markdown, "████") {
		t.Fatal("markdown numeric tables should retain density bars")
	}
	html, err := HTML(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"内存测评", "硬盘测评", "Crystal", "ATTO"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("html missing %q", want)
		}
	}
	if !strings.Contains(string(html), "████") || !strings.Contains(string(html), "cell-good") {
		t.Fatal("html numeric tables should retain density bars and status classes")
	}
}

func TestTableRowsWithBarsInvertsLowerIsBetterColumns(t *testing.T) {
	table := model.Table{
		Columns:        []string{"样本", "时延"},
		Rows:           [][]string{{"慢", "2 ms"}, {"快", "1 ms"}},
		NumericColumns: []int{1}, NumericHigherIsBetter: []bool{false},
	}
	rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone})
	if len(rows) != 2 || !strings.Contains(rows[0][1], "▒") || !strings.Contains(rows[1][1], "████████") {
		t.Fatalf("lower-is-better bars are not inverted: %+v", rows)
	}
}

func TestFioMixedScoreLabelIsLocalized(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	metric := score.MetricScore{Key: "fio_mixed_4k_read_mib_s", Label: "混合 4k 读"}

	i18n.Set(i18n.LangZH)
	if got := metricLabel(metric); !strings.Contains(got, "混合读写") {
		t.Fatalf("Chinese fio mixed label = %q", got)
	}
	i18n.Set(i18n.LangEN)
	if got := metricLabel(metric); !strings.Contains(got, "Mixed read/write") {
		t.Fatalf("English fio mixed label = %q", got)
	}
}
