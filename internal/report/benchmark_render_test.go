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
		SchemaVersion: "ecs.report/v2",
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
					Title:          "STREAM 四 kernel 带宽（Copy/Triad 主结果）",
					Columns:        []string{"内核 / 线程", "最佳速率", "原始单位", "方法", "证据"},
					Rows:           [][]string{{"Copy / 1T", "100 MiB/s", "MB/s", "stream-official-copy-1t-v1", "STREAM 最佳速率"}, {"Triad / NT", "200 MiB/s", "MB/s", "stream-official-triad-nt-v1", "STREAM 最佳速率"}},
					NumericColumns: []int{1}, NumericHigherIsBetter: []bool{true},
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

func TestTextHidesMatrixMeasurementKeysAndKeepsMethods(t *testing.T) {
	data := benchmarkRenderReport()
	disk := &data.Results[1]
	disk.Measurements = []model.Measurement{
		{Key: "fio_sequential_read_mib_s", Label: "顺序读", Display: "100 MiB/s", Method: "fio-direct-baseline"},
		{Key: "crystal_rnd4k_q1_read_mib_s", Label: "Crystal RND4K/Q1 读吞吐", Display: "10 MiB/s", Method: "fio-direct-crystal"},
		{Key: "atto_512b_write_iops", Label: "ATTO 512B 写 IOPS", Display: "20 IOPS", Method: "fio-direct-atto"},
		{Key: "fio_mixed_4k_read_mib_s", Label: "混合 4K 读吞吐", Display: "30 MiB/s", Method: "fio-direct-mixed"},
	}
	disk.Tables[0].Rows = [][]string{
		{"RND4K/Q1", "10 MiB/s", "100 IOPS", "20 MiB/s", "200 IOPS", "完成"},
		{"RND4K/Q32", "11 MiB/s", "110 IOPS", "21 MiB/s", "210 IOPS", "完成"},
		{"SEQ1M/Q1", "20 MiB/s", "200 IOPS", "40 MiB/s", "400 IOPS", "完成"},
		{"SEQ1M/Q8", "21 MiB/s", "210 IOPS", "41 MiB/s", "410 IOPS", "完成"},
	}
	blocks := []string{"512B", "1K", "2K", "4K", "8K", "16K", "32K", "64K", "128K", "256K", "512K", "1M", "2M", "4M", "8M", "16M", "32M", "64M"}
	rows := make([][]string, 0, len(blocks))
	for index, block := range blocks {
		rows = append(rows, []string{block, "1 MiB/s", "10 IOPS", "2 MiB/s", "20 IOPS", "完成"})
		if index == len(blocks)-1 {
			disk.Tables[1].Rows = rows
		}
	}
	plain := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, forbidden := range []string{"crystal crystal_", "atto atto_", "fio_mixed_"} {
		if strings.Contains(plain, forbidden) {
			t.Fatalf("纯文本不应显示内部标识 %q:\n%s", forbidden, plain)
		}
	}
	for _, want := range append(blocks, "RND4K/Q1", "RND4K/Q32", "SEQ1M/Q1", "SEQ1M/Q8", "顺序读", "100 MiB/s", "块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS") {
		if !strings.Contains(plain, want) {
			t.Fatalf("矩阵/基线文本缺少 %q:\n%s", want, plain)
		}
	}
	for _, want := range []string{"fio-direct-baseline", "fio-direct-mixed"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("当前 txt 缺少工作负载方法 %q:\n%s", want, plain)
		}
	}
	scored := &score.Report{
		Total: 800, Covered: 1, Possible: 1,
		Dimensions: []score.DimensionScore{{
			Key: "disk", Score: 800, Ratio: 0.8,
			Metrics: []score.MetricScore{
				{Key: "fio_sequential_read_mib_s", Label: "磁盘顺序读", Value: 100, Unit: "MiB/s", Ratio: 1},
				{Key: "crystal_rnd4k_q1_read_mib_s", Label: "Crystal RND4K/Q1 读吞吐", Value: 10, Unit: "MiB/s", Ratio: 1},
				{Key: "atto_512b_write_iops", Label: "ATTO 512B 写 IOPS", Value: 20, Unit: "IOPS", Ratio: 1},
			},
			Groups: []score.GroupScore{
				{Key: "crystal", MetricCount: 16},
				{Key: "atto", MetricCount: 72},
			},
		}},
	}
	withScore := Text(data, TextOptions{Color: termcolor.LevelNone, Score: scored})
	for _, want := range []string{"Crystal 16 项", "ATTO 72 项", "磁盘顺序读"} {
		if !strings.Contains(withScore, want) {
			t.Fatalf("评分区缺少矩阵摘要/基线指标 %q:\n%s", want, withScore)
		}
	}
	for _, forbidden := range []string{"crystal crystal_", "atto atto_"} {
		if strings.Contains(withScore, forbidden) {
			t.Fatalf("评分区不应显示冗余标识 %q:\n%s", forbidden, withScore)
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

func TestTableRowsWithBarsKeepZeroDirectionSemantics(t *testing.T) {
	table := model.Table{
		Columns:        []string{"样本", "风险", "丢包"},
		Rows:           [][]string{{"零", "0 /100", "0 %"}, {"高", "90 /100", "2 %"}},
		NumericColumns: []int{1, 2}, NumericHigherIsBetter: []bool{false, false},
	}
	rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone})
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	densityCount := func(value string) int {
		return strings.Count(value, "░") + strings.Count(value, "▒") + strings.Count(value, "▓") + strings.Count(value, "█")
	}
	if densityCount(rows[0][1]) != 0 || !strings.Contains(rows[0][1], "·") {
		t.Fatalf("0/100 risk should remain an empty magnitude bar: %+v", rows)
	}
	if densityCount(rows[0][2]) == 0 {
		t.Fatalf("0%% packet loss should remain a visible quality bar: %+v", rows)
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
