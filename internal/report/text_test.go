package report

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

func textSampleReport() model.Report {
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run: model.RunInfo{
			ID: "abc", Profile: "quick", Exposure: "local", Offline: true,
			StartedAt: time.Unix(0, 0).UTC(), Redacted: true, Requested: []string{"cpu"},
		},
		Summary: model.Summary{Status: model.StatusOK, OK: 1, Headline: "1 项完成"},
		Results: []model.Result{{
			ID: "cpu", Title: "CPU 性能", Status: model.StatusOK, Summary: "单线程 780",
			Methodology: model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "sysbench"},
			Measurements: []model.Measurement{
				{Key: "a", Label: "单线程事件率", Value: 780, Unit: "events/s", Display: "780 events/s", HigherIsBetter: model.BoolPtr(true)},
				{Key: "b", Label: "多线程事件率", Value: 6100, Unit: "events/s", Display: "6100 events/s", HigherIsBetter: model.BoolPtr(true)},
			},
			Fields: []model.Field{{Key: "engine", Label: "引擎", Value: "sysbench"}},
			Tables: []model.Table{{
				Title: "明细", Columns: []string{"项目", "数值"},
				Rows: [][]string{{"单线程", "780"}, {"多线程", "6100"}},
			}},
			Notes: []string{"这是一条足够长的说明文字，用来检验按显示宽度折行的行为是否正确处理中英混排的情况。"},
		}},
		Notices: []string{"报告只写入本地。"},
	}
}

// 无色档不能残留任何转义序列：txt 会被 diff、贴进不解析 ANSI 的地方。
func TestTextPlainHasNoEscapes(t *testing.T) {
	out := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone})
	if strings.Contains(out, "\x1b") {
		t.Fatal("无色档输出了转义序列")
	}
	if !strings.Contains(out, "CPU 性能") {
		t.Fatal("缺少模块标题")
	}
}

func TestTextColoredEmitsEscapes(t *testing.T) {
	out := Text(textSampleReport(), TextOptions{Color: termcolor.LevelTrueColor})
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Fatal("真彩色档应输出 RGB 序列")
	}
}

func TestTextSemanticColorsAndWidth(t *testing.T) {
	data := textSampleReport()
	data.Results = []model.Result{
		{
			ID: "cpu", Title: "CPU 性能", Status: model.StatusOK,
			Fields: []model.Field{{Label: "可用状态", Value: "已启用 / available"}},
			Tables: []model.Table{{Title: "状态表", Columns: []string{"状态", "结果"}, Rows: [][]string{{"完成", "正常"}}}},
		},
		{
			ID: "media", Title: "流媒体", Status: model.StatusWarning,
			Summary: "部分平台 / medium risk", Fields: []model.Field{{Label: "风险", Value: "中风险，需留意"}},
		},
		{
			ID: "route", Title: "回程", Status: model.StatusError,
			Summary: "失败", Error: "不可用 (not available)", Fields: []model.Field{{Label: "状态", Value: "不可用 (not available)"}},
		},
	}
	plain := Text(data, TextOptions{Color: termcolor.LevelNone})
	if strings.Contains(plain, "\x1b") {
		t.Fatal("无色报告不应输出 ANSI")
	}
	for _, level := range []termcolor.Level{termcolor.LevelTrueColor, termcolor.LevelBasic} {
		colored := Text(data, TextOptions{Color: level})
		p := termcolor.Palette{Level: level}
		for _, styled := range []string{
			p.Success("已启用 / available"), p.Warning("中风险，需留意"), p.Error("不可用 (not available)"),
		} {
			if !strings.Contains(colored, styled) {
				t.Fatalf("%v 档语义状态未按预期着色 %q:\n%s", level, styled, colored)
			}
		}
		if !strings.Contains(colored, "\x1b[1;") {
			t.Fatalf("%v 档标题/表头应使用带强调色的粗体 ANSI", level)
		}
		if level == termcolor.LevelTrueColor {
			for _, separator := range []string{
				p.Dim(strings.Repeat("#", textWidth)),
				p.Dim(strings.Repeat("*", textWidth)),
			} {
				if !strings.Contains(colored, separator) {
					t.Fatalf("横幅分隔线应保持 dim: %q", separator)
				}
			}
			if !strings.Contains(colored, p.AccentBold("CPU 性能")) {
				t.Fatal("模块标题应使用 accent+bold")
			}
			if !strings.Contains(colored, p.LabelBold("  状态  结果")) {
				t.Fatal("表头应使用层次色")
			}
		}
		for lineNumber, line := range strings.Split(colored, "\n") {
			if width := textwidth.Width(line); width > textWidth {
				t.Fatalf("%v 档彩色报告第 %d 行超出 %d 列：%d", level, lineNumber+1, textWidth, width)
			}
		}
	}
}

func TestTextSemanticValueDoesNotNestColoredBars(t *testing.T) {
	p := termcolor.Palette{Level: termcolor.LevelTrueColor}
	renderer := textRenderer{palette: p}
	bar := p.BarRelative(5, 10, 8)
	cell := "5 MiB/s " + bar
	if got := renderer.semanticValue(cell); got != cell {
		t.Fatalf("already colored table bar was wrapped again:\nwant %q\ngot  %q", cell, got)
	}
}

func TestSemanticToneNegativeCompositeWins(t *testing.T) {
	for _, value := range []string{"not available", "not enabled", "not ready", "not ok", "unhealthy"} {
		if got, ok := semanticTone(value); !ok || got != termcolor.ToneError {
			t.Fatalf("negative composite %q should be error tone, got %v (ok=%v)", value, got, ok)
		}
	}
}

func TestTextNavigationAndWidthBudget(t *testing.T) {
	data := textSampleReport()
	data.Run.Requested = []string{"cpu", "media", "backtrace"}
	data.Results = append(data.Results, model.Result{
		ID: "media", Title: "流媒体与 AI 服务", Status: model.StatusWarning,
		Summary:      "部分平台响应很长但仍需保持版面稳定",
		Fields:       []model.Field{{Label: "平台状态说明", Value: strings.Repeat("中文和 English 混排的字段值 ", 8)}},
		Measurements: []model.Measurement{{Label: "超长指标名称", Display: strings.Repeat("123 ", 10), Value: 123}},
		Notes:        []string{strings.Repeat("长说明文字 ", 20)},
	})
	scored := &score.Report{Total: 720, Ratio: 0.72, Covered: 2, Possible: 3, BaselineSource: "builtinSingleHost", BaselineSample: 1}
	for _, level := range []termcolor.Level{termcolor.LevelNone, termcolor.LevelTrueColor} {
		out := Text(data, TextOptions{Color: level, Score: scored})
		for lineNumber, line := range strings.Split(out, "\n") {
			if width := textwidth.Width(line); width > textWidth {
				t.Fatalf("line %d exceeds %d columns at color level %v: %d\n%s", lineNumber+1, textWidth, level, width, line)
			}
		}
	}
	plain := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{"全部", "本次选择", "CPU 性能", "流媒体与 AI 服务", "三网回程"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("navigation missing %q:\n%s", want, plain)
		}
	}
}

func TestTextUsesTemplateBannersAndNestedGroups(t *testing.T) {
	data := textSampleReport()
	data.Results = append(data.Results, model.Result{
		ID: "network", Title: "网络与 IP 质量", Status: model.StatusOK,
		Fields: []model.Field{{Key: "ipv4", Label: "IPv4 出口", Value: "203.0.113.x"}},
		Tables: []model.Table{{Title: "IPv4 · 风险评分", Columns: []string{"数据库", "风险"}, Rows: [][]string{{"ipapi", "低"}}}},
	}, model.Result{
		ID: "mystery", Title: "神秘模块", Status: model.StatusWarning,
		Fields: []model.Field{{Key: "answer", Label: "答案", Value: "保留"}},
		Tables: []model.Table{{Title: "未知明细", Columns: []string{"值"}, Rows: [][]string{{"仍然可见"}}}},
	})
	out := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{
		strings.Repeat("#", textWidth), strings.Repeat("*", textWidth),
		"bash <(curl -sL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh)",
		"https://github.com/CST-Cat/ecs", "一、CPU 测评", "一、IP 信息", "一、模块详情",
		"引擎：", "单线程事件率：", "答案：", "仍然可见",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("模板文本缺少 %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"关键指标", "测试口径", "原始文本", "third-method"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("模板文本不应包含通用/原始前缀 %q:\n%s", forbidden, out)
		}
	}
}

func TestTextModuleHeadersKeepProjectNamesOnly(t *testing.T) {
	data := textSampleReport()
	data.Results = []model.Result{
		{
			ID: "backtrace", Title: "三网回程", Status: model.StatusOK,
			StartedAt: time.Date(2026, 8, 4, 3, 4, 5, 0, time.UTC),
			Sources:   []model.Source{{Name: "backtrace", URL: "https://github.com/zhanghanyun/backtrace"}},
		},
		{
			ID: "disk", Title: "磁盘性能", Status: model.StatusOK,
			Sources: []model.Source{
				{Name: "fio", URL: "https://github.com/axboe/fio"},
				{Name: "YABS", URL: "https://github.com/masonr/yet-another-bench-script"},
				{Name: "fio", URL: "https://duplicate.example/fio"},
				{Name: "  ", URL: "https://unnamed.example"},
			},
		},
		{ID: "bare", Title: "无来源模块", Status: model.StatusOK},
	}
	plain := Text(data, TextOptions{Color: termcolor.LevelNone})
	segments := textModuleBanners(plain)
	if len(segments) != len(data.Results) {
		t.Fatalf("模块头数量 = %d，期望 %d:\n%s", len(segments), len(data.Results), plain)
	}
	for _, segment := range segments {
		for _, forbidden := range []string{
			"https://", "http://", "bash <(curl", "2026-08-04", "03:04:05", "test",
		} {
			if strings.Contains(segment, forbidden) {
				t.Fatalf("模块头不应包含冗余元数据 %q:\n%s", forbidden, segment)
			}
		}
	}
	if !strings.Contains(segments[0], "三网回程") || !strings.Contains(segments[0], "backtrace") {
		t.Fatalf("模块头应保留标题和项目名:\n%s", segments[0])
	}
	if !strings.Contains(segments[1], i18n.T("report.diskBenchmark")) || !strings.Contains(segments[1], "fio · YABS") {
		t.Fatalf("模块头应保留去重后的全部项目名:\n%s", segments[1])
	}
	if strings.Count(segments[1], "fio") != 1 || strings.Count(segments[1], "YABS") != 1 {
		t.Fatalf("模块头项目名不应重复:\n%s", segments[1])
	}
	if !strings.Contains(segments[2], "无来源模块") {
		t.Fatalf("无来源模块头缺少标题:\n%s", segments[2])
	}
	if lines := strings.Split(segments[2], "\n"); len(lines) != 3 {
		t.Fatalf("无来源模块不应因空项目产生额外空行：%d 行\n%s", len(lines), segments[2])
	}
}

func textModuleBanners(output string) []string {
	startMarker := strings.Repeat("*", textWidth)
	endMarker := strings.Repeat("-", textWidth)
	segments := []string{}
	for offset := 0; offset < len(output); {
		start := strings.Index(output[offset:], startMarker)
		if start < 0 {
			break
		}
		start += offset
		end := strings.Index(output[start+len(startMarker):], endMarker)
		if end < 0 {
			break
		}
		end += start + len(startMarker)
		segments = append(segments, output[start:end+len(endMarker)])
		offset = end + len(endMarker)
	}
	return segments
}

func TestTextFiltersImplementationFieldsAndExplanatoryColumns(t *testing.T) {
	data := textSampleReport()
	result := &data.Results[0]
	result.Fields = append(result.Fields,
		model.Field{Key: "arguments", Label: "参数模板", Value: "sysbench --threads=N"},
		model.Field{Key: "command_args", Label: "命令参数", Value: "--time=15s"},
		model.Field{Key: "mbw_args", Label: "mbw 参数", Value: "mbw -q -n 5"},
		model.Field{Key: "real", Label: "实际值", Value: "保留"},
	)
	result.Tables = append(result.Tables, model.Table{
		Title:   "风险表",
		Columns: []string{"事实", "为什么值得看", "指标口径", "分段规则", "备注", "来源"},
		Rows:    [][]string{{"低", "解释", "实现", "规则", "注释", "官方"}},
	})
	result.Sources = []model.Source{
		{Name: "primary", URL: "https://example.com/primary"},
		{Name: "secondary", URL: "https://example.com/secondary"},
	}
	out := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, forbidden := range []string{"参数模板", "命令参数", "mbw 参数", "sysbench --threads=N", "为什么值得看", "指标口径", "分段规则", "备注", "来源链接", "https://example.com/primary", "https://example.com/secondary"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("实现/解释性文本不应出现在 txt (%q):\n%s", forbidden, out)
		}
	}
	for _, want := range []string{"实际值：", "保留", "事实", "来源", "低", "primary", "secondary"} {
		if !strings.Contains(out, want) {
			t.Fatalf("纯文本缺少保留事实 %q:\n%s", want, out)
		}
	}
	if count := strings.Count(out, "primary"); count != 1 {
		t.Fatalf("banner 项目名应只显示一次，出现 %d 次", count)
	}
	if count := strings.Count(out, "secondary"); count != 1 {
		t.Fatalf("banner 项目名应只显示一次，出现 %d 次", count)
	}
}

func TestTextDiskMatricesRemainCompleteAndNotFlattened(t *testing.T) {
	data := textSampleReport()
	data.Run.Requested = []string{"disk"}
	data.Notices = []string{"内部 notice 不应出现在终端模板"}
	data.Results = []model.Result{{
		ID: "disk", Title: "磁盘性能", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "fio_sequential_read_mib_s", Label: "顺序读", Display: "100 MiB/s", Method: "fio-direct-legacy"},
			{Key: "crystal_rnd4k_q1_read_mib_s", Label: "Crystal crystal_rnd4k_q1 read 吞吐", Display: "10 MiB/s", Method: "fio-direct-crystal"},
			{Key: "atto_512b_read_mib_s", Label: "ATTO atto_512b read 吞吐", Display: "1 MiB/s", Method: "fio-direct-atto"},
			{Key: "fio_mixed_4k_read_mib_s", Label: "混合 fio_mixed_4k read 吞吐", Display: "15 MiB/s", Method: "fio-direct-mixed"},
		},
		Tables: []model.Table{
			{Title: "Crystal", Columns: []string{"工作负载", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"}, Rows: [][]string{
				{"RND4K/Q1", "10 MiB/s", "100 IOPS", "20 MiB/s", "200 IOPS", "完成"},
				{"SEQ1M/Q8", "30 MiB/s", "300 IOPS", "40 MiB/s", "400 IOPS", "完成"},
			}},
			{Title: "ATTO", Columns: []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"}, Rows: [][]string{
				{"512B", "1 MiB/s", "10 IOPS", "2 MiB/s", "20 IOPS", "完成"},
				{"64M", "8 MiB/s", "80 IOPS", "16 MiB/s", "160 IOPS", "完成"},
			}},
			{Title: "50/50 混合随机读写 QD64 × 2 作业（YABS 兼容口径）", Columns: []string{"块大小", "读", "读 IOPS", "写", "写 IOPS", "合计"}, Rows: [][]string{
				{"4k", "15 MiB/s", "150 IOPS", "25 MiB/s", "250 IOPS", "40 MiB/s"},
				{"1m", "35 MiB/s", "350 IOPS", "45 MiB/s", "450 IOPS", "80 MiB/s"},
			}},
		},
	}}
	out := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{"Crystal：", "ATTO：", "RND4K/Q1", "SEQ1M/Q8", "512B", "64M", "4k", "1m", "10 MiB/s", "100 IOPS", "20 MiB/s", "200 IOPS", "15 MiB/s", "150 IOPS", "25 MiB/s", "250 IOPS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("磁盘矩阵缺少 %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"crystal_", "atto_", "fio_mixed_", "内部 notice"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("txt 不应扁平化/输出冗余内容 %q:\n%s", forbidden, out)
		}
	}
	jsonBytes, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonBytes), "内部 notice") || !strings.Contains(string(jsonBytes), "crystal_rnd4k_q1_read_mib_s") {
		t.Fatal("隐藏 txt notice/flat metrics 不应修改 JSON 数据")
	}
}

func TestTableRowsWithBarsKeepNumericBoundariesAcrossTables(t *testing.T) {
	tables := []model.Table{
		{
			Title: "Crystal", Columns: []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
			Rows: [][]string{
				{"RND4K/Q1", "1 MiB/s", "9 IOPS", "2 MiB/s", "20 IOPS", "完成"},
				{"SEQ1M/Q8", "1000 MiB/s", "1000 IOPS", "2000 MiB/s", "2000 IOPS", "完成"},
			}, NumericColumns: []int{1, 2, 3, 4},
		},
		{
			Title: "ATTO", Columns: []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
			Rows: [][]string{
				{"512B", "10 MiB/s", "9 IOPS", "11 MiB/s", "99 IOPS", "完成"},
				{"64M", "1000 MiB/s", "10000 IOPS", "2000 MiB/s", "20000 IOPS", "完成"},
			}, NumericColumns: []int{1, 2, 3, 4},
		},
		{
			Title: "普通数值表", Columns: []string{"节点", "时延", "IOPS", "状态"},
			Rows: [][]string{
				{"节点 A", "1 ms", "9 IOPS", "正常"},
				{"node-long", "1000 ms", "1000 IOPS", "正常"},
			}, NumericColumns: []int{1, 2},
		},
	}
	for _, table := range tables {
		rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone})
		for _, column := range table.NumericColumns {
			start := -1
			for rowIndex, row := range rows {
				if column >= len(row) {
					t.Fatalf("%s row %d missing numeric column %d", table.Title, rowIndex, column)
				}
				barStart := firstDensityStart(row[column])
				if barStart < 0 {
					t.Fatalf("%s row %d column %d missing bar: %q", table.Title, rowIndex, column, row[column])
				}
				if start < 0 {
					start = barStart
				} else if barStart != start {
					t.Fatalf("%s column %d bar boundary drift: row %d at %d, want %d", table.Title, column, rowIndex, barStart, start)
				}
			}
		}
	}

	data := textSampleReport()
	data.Results = []model.Result{{ID: "disk", Title: "磁盘", Status: model.StatusOK, Tables: tables}}
	for _, level := range []termcolor.Level{termcolor.LevelNone, termcolor.LevelTrueColor} {
		out := Text(data, TextOptions{Color: level})
		for lineNumber, line := range strings.Split(out, "\n") {
			if width := textwidth.Width(line); width > textWidth {
				t.Fatalf("%v 档第 %d 行超出 %d 列：%d", level, lineNumber+1, textWidth, width)
			}
		}
	}
}

func firstDensityStart(value string) int {
	for index, character := range value {
		if strings.ContainsRune("░▒▓█", character) {
			return textwidth.Width(value[:index])
		}
	}
	return -1
}

// 中英混排的表格必须列对齐：用字符数而不是显示宽度对齐会让整张表歪掉。
func TestTextTableAlignsCJK(t *testing.T) {
	data := textSampleReport()
	data.Results[0].Tables = []model.Table{{
		Columns: []string{"名称", "值"},
		Rows:    [][]string{{"中文很长的名称", "1"}, {"ab", "22"}, {"混合ab中文", "333"}},
	}}
	out := Text(data, TextOptions{Color: termcolor.LevelNone})

	// 取出这三行数据行，第二列的起始列必须一致。
	var starts []int
	for _, line := range strings.Split(out, "\n") {
		for _, prefix := range []string{"  中文很长的名称", "  ab ", "  混合ab中文"} {
			if strings.HasPrefix(line, prefix) {
				trimmed := strings.TrimRight(line, " ")
				// 第二列起点 = 整行宽度减去末列内容宽度
				fields := strings.Fields(trimmed)
				last := fields[len(fields)-1]
				starts = append(starts, textwidth.Width(trimmed)-textwidth.Width(last))
			}
		}
	}
	if len(starts) != 3 {
		t.Fatalf("没有找齐三行数据，找到 %d 行", len(starts))
	}
	for _, start := range starts[1:] {
		if start != starts[0] {
			t.Fatalf("列未对齐：各行第二列起点 %v", starts)
		}
	}
}

// 章节编号在中文下用中文数字，英文下用阿拉伯数字。
func TestChineseNumeral(t *testing.T) {
	cases := map[int]string{1: "一", 9: "九", 10: "十", 11: "十一", 20: "二十", 21: "二十一", 35: "三十五"}
	for value, want := range cases {
		if got := chineseNumeral(value); got != want {
			t.Errorf("chineseNumeral(%d) = %q，期望 %q", value, got, want)
		}
	}
}

// 折行必须按显示宽度，否则中文长句会在窄终端里溢出版面。
func TestWrapTextRespectsDisplayWidth(t *testing.T) {
	text := strings.Repeat("中文", 40)
	for _, line := range wrapText(text, 30) {
		if textwidth.Width(line) > 30 {
			t.Fatalf("折行后仍超宽：%d 列", textwidth.Width(line))
		}
	}
	// 短文本不该被折。
	if got := wrapText("短", 30); len(got) != 1 {
		t.Fatalf("短文本被折成了 %d 行", len(got))
	}
}

// 评分区必须同时呈现覆盖度与基线来源：只给一个数字无从判断它值多少。
func TestTextScoreSectionStatesCoverageAndBaseline(t *testing.T) {
	scored := &score.Report{
		Total: 800, Ratio: 0.8, Covered: 2, Possible: 4, Complete: false,
		BaselineSource: "builtinSingleHost", BaselineSample: 1,
		Dimensions: []score.DimensionScore{
			{Key: "cpu", Score: 800, Ratio: 0.8, Metrics: []score.MetricScore{
				{Key: "cpu_single", Label: "单线程", Value: 800, Unit: "events/s", Baseline: 1000, Ratio: 0.8, Score: 800},
			}},
			{Key: "disk", Missing: true, MissingReason: "moduleNotRun"},
		},
	}
	out := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone, Score: scored})
	for _, want := range []string{"2/4", "未覆盖全部维度", "样本 1 台", "内置单机快照"} {
		if !strings.Contains(out, want) {
			t.Errorf("评分区缺少 %q", want)
		}
	}
	// 缺失维度要显式说明，不能留白。
	if !strings.Contains(out, "未测（未计入）") {
		t.Error("缺失维度应显式标注")
	}
}

func TestTextWithoutScoreOmitsSection(t *testing.T) {
	out := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone})
	if strings.Contains(out, "综合评分") {
		t.Fatal("没有评分时不该出现评分区")
	}
}

func TestTextSectionsNumberOnlyActualResultsAndScore(t *testing.T) {
	data := textSampleReport()
	data.Results = []model.Result{
		{ID: "one", Title: "结果一", Status: model.StatusOK},
		{ID: "two", Title: "结果二", Status: model.StatusOK},
		{ID: "three", Title: "结果三", Status: model.StatusWarning},
		{ID: "four", Title: "结果四", Status: model.StatusSkipped},
		{ID: "five", Title: "结果五", Status: model.StatusError},
	}
	plain := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, heading := range []string{"结果一", "结果二", "结果三", "结果四", "结果五", "一、模块详情"} {
		if !strings.Contains(plain, heading) {
			t.Fatalf("missing result/banner section %q:\n%s", heading, plain)
		}
	}
	if strings.Contains(plain, "六、综合评分") {
		t.Fatal("没有评分时不应出现评分章节")
	}

	scored := &score.Report{Total: 800, Ratio: 0.8, Covered: 1, Possible: 1}
	withScore := Text(data, TextOptions{Color: termcolor.LevelNone, Score: scored})
	if !strings.Contains(withScore, "六、综合评分") {
		t.Fatalf("评分章节未接在实际结果之后:\n%s", withScore)
	}

	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangEN)
	english := Text(data, TextOptions{Color: termcolor.LevelNone, Score: scored})
	for _, heading := range []string{"结果一", "结果二", "结果三", "结果四", "结果五", "1. Module details", "6. Composite score"} {
		if !strings.Contains(english, heading) {
			t.Fatalf("missing English heading %q:\n%s", heading, english)
		}
	}
}

func TestTextIncludesEveryResultDetail(t *testing.T) {
	data := textSampleReport()
	result := &data.Results[0]
	result.Measurements = append(result.Measurements,
		model.Measurement{Key: "third", Label: "第三个指标", Value: 3, Display: "3 units", Method: "third-method"})
	result.Fields = append(result.Fields, model.Field{Key: "third-field", Label: "第三个字段", Value: "第三个值"})
	result.Tables = append(result.Tables, model.Table{
		Title: "第二张表", Columns: []string{"列"}, Rows: [][]string{{"第三张表值"}},
	})
	result.TextBlocks = []model.TextBlock{{Title: "原始文本", Content: "第三个文本块内容"}}
	result.Notes = append(result.Notes, "第三条备注")
	result.Status = model.StatusWarning
	result.Methodology = model.Methodology{
		Kind: "custom-kind", Label: "自定义方法", Engine: "custom-engine",
		Profile: "custom-profile", ComparisonScope: "custom-scope",
	}
	out := Text(data, TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{
		"第三个指标", "3 units", "第三个字段", "第三个值",
		"第二张表", "第三张表值",
		"需留意",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("完整文本报告缺少 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "third-method") {
		t.Fatalf("纯文本不应显示 Measurement.Method: %s", out)
	}
	if strings.Contains(out, "第三个文本块内容") || strings.Contains(out, "第三条备注") {
		t.Fatal("纯文本不应恢复原始文本块或长注释")
	}
}

// 同单位同方向的一组指标才画组内相对柱：把 ms 和 MiB/s 放同一刻度没有意义。
func TestGroupComparableRequiresSameUnitAndDirection(t *testing.T) {
	items := []model.Measurement{
		{Key: "a", Unit: "ms", Value: 10, HigherIsBetter: model.BoolPtr(false)},
		{Key: "b", Unit: "ms", Value: 20, HigherIsBetter: model.BoolPtr(false)},
		{Key: "c", Unit: "MiB/s", Value: 500, HigherIsBetter: model.BoolPtr(true)},
	}
	groups := groupComparable(items)
	if _, ok := groups["a"]; !ok {
		t.Error("同单位同方向的两项应当成组")
	}
	if _, ok := groups["c"]; ok {
		t.Error("单独一项不该成组")
	}
	// 越小越好的组要翻转，使柱长始终表示"越长越好"。
	if group := groups["a"]; !group.inverse {
		t.Error("越小越好的组应标记为翻转")
	}
	if value := comparableValue(items[0], groups["a"]); value <= comparableValue(items[1], groups["b"]) {
		t.Error("翻转后更小的延迟应得到更大的比较值")
	}
}

func TestGroupComparableIgnoresUnknownDirection(t *testing.T) {
	items := []model.Measurement{
		{Key: "a", Unit: "u", Value: 1},
		{Key: "b", Unit: "u", Value: 2},
	}
	if len(groupComparable(items)) != 0 {
		t.Error("方向未知的指标不该画相对柱")
	}
}
