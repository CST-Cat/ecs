package report

import (
	"fmt"
	"math"
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
			ID: "abc", Profile: "standard", Exposure: "local", Offline: true,
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

func TestTextRendersEachToolVersionAsItsOwnColoredField(t *testing.T) {
	data := textSampleReport()
	data.Results[0].Fields = []model.Field{
		{Key: "sysbench_version", Label: "sysbench 版本", Value: "sysbench 1.0.20"},
		{Key: "sysbench_binary_sha256", Label: "sysbench SHA-256", Value: "sha-sysbench"},
		{Key: "stream_version", Label: "STREAM 版本", Value: "STREAM 5.10"},
		{Key: "stream_binary_sha256", Label: "STREAM SHA-256", Value: "sha-stream"},
		{Key: "fio_version", Label: "fio 版本", Value: "fio-3.42"},
		{Key: "fio_binary_sha256", Label: "fio SHA-256", Value: "sha-fio"},
		{Key: "iperf3_version", Label: "iperf3 版本", Value: "iperf 3.16"},
		{Key: "iperf3_binary_sha256", Label: "iperf3 SHA-256", Value: "sha-iperf3"},
		{Key: "speedtest_version", Label: "speedtest 版本", Value: "speedtest 1.2.3"},
		{Key: "speedtest_binary_sha256", Label: "speedtest SHA-256", Value: "sha-speedtest"},
		{Key: "nexttrace_version", Label: "NextTrace Tiny 版本", Value: "NextTrace Tiny v0.1.0"},
		{Key: "nexttrace_binary_sha256", Label: "NextTrace Tiny SHA-256", Value: "abc123"},
	}
	out := Text(data, TextOptions{Color: termcolor.LevelTrueColor})
	p := termcolor.Palette{Level: termcolor.LevelTrueColor}
	labels := []string{
		"sysbench 版本", "sysbench SHA-256", "STREAM 版本", "STREAM SHA-256",
		"fio 版本", "fio SHA-256", "iperf3 版本", "iperf3 SHA-256",
		"speedtest 版本", "speedtest SHA-256", "NextTrace Tiny 版本", "NextTrace Tiny SHA-256",
	}
	labelWidth := 0
	for _, label := range labels {
		labelWidth = maxInt(labelWidth, textwidth.Width(label))
	}
	for _, label := range labels {
		if count := strings.Count(out, label); count != 1 {
			t.Fatalf("版本标签 %q 应各自出现一次，实际 %d 次:\\n%s", label, count, out)
		}
		styledLabel := p.Label(textwidth.Pad(label, labelWidth) + i18n.T("punct.colon"))
		if !strings.Contains(out, styledLabel) {
			t.Fatalf("版本标签 %q 未复用字段标签颜色层次:\\n%s", label, out)
		}
	}
	for _, version := range []string{
		"sysbench 1.0.20", "sha-sysbench", "STREAM 5.10", "sha-stream",
		"fio-3.42", "sha-fio", "iperf 3.16", "sha-iperf3",
		"speedtest 1.2.3", "sha-speedtest", "NextTrace Tiny v0.1.0", "abc123",
	} {
		if !strings.Contains(out, version) {
			t.Fatalf("报告缺少工具版本值 %q:\\n%s", version, out)
		}
	}
	for _, pair := range [][2]string{
		{"sysbench 版本", "sysbench SHA-256"}, {"STREAM 版本", "STREAM SHA-256"},
		{"fio 版本", "fio SHA-256"}, {"iperf3 版本", "iperf3 SHA-256"},
		{"speedtest 版本", "speedtest SHA-256"}, {"NextTrace Tiny 版本", "NextTrace Tiny SHA-256"},
	} {
		versionIndex := strings.Index(out, pair[0])
		shaIndex := strings.Index(out, pair[1])
		if shaIndex < versionIndex {
			t.Fatalf("%s 应显示在 %s 之后:\\n%s", pair[1], pair[0], out)
		}
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
	scored := &score.Report{Total: 720, Ratio: 0.72, Covered: 2, Possible: 3, BaselineSource: "test-reference", BaselineSample: 2}
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

func TestTextAdaptsBarsAndLayoutAcrossTerminalWidthsAndColorLevels(t *testing.T) {
	data := textSampleReport()
	data.Results[0].Evidence = model.NewEvidence(3, 4, "sample")
	data.Results[0].Tables = []model.Table{{
		Title:          "多列性能表",
		Columns:        []string{"workload", "1 worker", "all workers", "scaling", "efficiency", "status"},
		Rows:           [][]string{{"compress", "100 MB/s", "740 MB/s", "7.40x", "92.5%", "completed"}},
		NumericColumns: []int{1, 2, 3, 4}, NumericHigherIsBetter: []bool{true, true, true, true},
	}}

	levels := []termcolor.Level{
		termcolor.LevelNone, termcolor.LevelBasic, termcolor.LevelANSI256, termcolor.LevelTrueColor,
	}
	for _, width := range []int{40, 64, 96, 140} {
		for _, level := range levels {
			out := Text(data, TextOptions{Color: level, Width: width, Compact: true})
			for lineNumber, line := range strings.Split(out, "\n") {
				if got := textwidth.Width(line); got > width {
					t.Fatalf("width=%d level=%v line %d uses %d columns: %q", width, level, lineNumber+1, got, line)
				}
			}
			if !strings.Contains(out, "多列性能表") || !strings.Contains(out, "wor") {
				t.Fatalf("width=%d level=%v lost table structure:\n%s", width, level, out)
			}
			if level == termcolor.LevelNone && strings.Contains(out, "\x1b") {
				t.Fatalf("width=%d plain output contains ANSI", width)
			}
			if level != termcolor.LevelNone && !strings.Contains(out, "\x1b[") {
				t.Fatalf("width=%d level=%v lost ANSI hierarchy", width, level)
			}
		}
	}

	measurementBarWidth := func(out string) int {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "单线程") {
				return barDensityCountForTest(line)
			}
		}
		return 0
	}
	narrow := measurementBarWidth(Text(data, TextOptions{Color: termcolor.LevelNone, Width: 40, Compact: true}))
	wide := measurementBarWidth(Text(data, TextOptions{Color: termcolor.LevelNone, Width: 140, Compact: true}))
	if narrow == 0 || wide <= narrow {
		t.Fatalf("measurement bar did not adapt to viewport: narrow=%d wide=%d", narrow, wide)
	}

	stacked := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 24, Compact: true})
	for _, required := range []string{"workload", "efficiency", "status", "completed"} {
		if !strings.Contains(stacked, required) {
			t.Fatalf("stacked narrow table lost %q:\n%s", required, stacked)
		}
	}
	for lineNumber, line := range strings.Split(stacked, "\n") {
		if got := textwidth.Width(line); got > 24 {
			t.Fatalf("stacked line %d uses %d columns: %q", lineNumber+1, got, line)
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
	if !strings.Contains(out, "测试口径") {
		t.Fatalf("完整 txt 应包含当前方法学说明:\n%s", out)
	}
	for _, forbidden := range []string{"原始文本", "third-method"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("模板文本不应包含通用/原始前缀 %q:\n%s", forbidden, out)
		}
	}
}

func TestTextModuleHeadersIncludeSourceAndExecutionMetadata(t *testing.T) {
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
		for _, want := range []string{"https://", "bash <(curl", "test"} {
			if !strings.Contains(segment, want) {
				t.Fatalf("模块头缺少 URL/curl/时间或版本 %q:\n%s", want, segment)
			}
		}
	}
	if !strings.Contains(segments[0], "三网回程") || !strings.Contains(segments[0], "https://github.com/zhanghanyun/backtrace") ||
		!strings.Contains(segments[0], "bash <(curl -sL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh) --only backtrace") ||
		!strings.Contains(segments[0], "2026-08-04 03:04:05 UTC · test") {
		t.Fatalf("模块头应保留来源、命令和开始时间/版本:\n%s", segments[0])
	}
	if !strings.Contains(segments[1], i18n.T("report.diskBenchmark")) ||
		!strings.Contains(segments[1], "https://github.com/axboe/fio") ||
		!strings.Contains(segments[1], "--only disk") || !strings.Contains(segments[1], "test") {
		t.Fatalf("磁盘模块头应保留首个来源、命令和版本:\n%s", segments[1])
	}
	if !strings.Contains(segments[2], "无来源模块") {
		t.Fatalf("无来源模块头缺少标题:\n%s", segments[2])
	}
	for _, segment := range segments {
		if lines := strings.Split(segment, "\n"); len(lines) != 6 {
			t.Fatalf("模块头布局应为标题/来源/命令/时间版本/分隔线：%d 行\n%s", len(lines), segment)
		}
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
	for _, forbidden := range []string{"参数模板", "命令参数", "sysbench --threads=N", "为什么值得看", "指标口径", "分段规则", "备注", "来源链接"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("实现/解释性文本不应出现在 txt (%q):\n%s", forbidden, out)
		}
	}
	for _, want := range []string{"实际值：", "保留", "事实", "来源", "低", "https://example.com/primary", "https://example.com/secondary"} {
		if !strings.Contains(out, want) {
			t.Fatalf("纯文本缺少保留事实 %q:\n%s", want, out)
		}
	}
	if count := strings.Count(out, "https://example.com/primary"); count != 1 {
		t.Fatalf("banner 来源应只显示一次，出现 %d 次", count)
	}
	if count := strings.Count(out, "https://example.com/secondary"); count != 1 {
		t.Fatalf("正文来源应保留一次，出现 %d 次", count)
	}
}

func TestTextDiskMatricesRemainCompleteAndNotFlattened(t *testing.T) {
	data := textSampleReport()
	data.Run.Requested = []string{"disk"}
	data.Notices = []string{"内部 notice 不应出现在终端模板"}
	data.Results = []model.Result{{
		ID: "disk", Title: "磁盘性能", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "fio_sequential_read_mib_s", Label: "顺序读", Display: "100 MiB/s", Method: "fio-direct-baseline"},
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
			{Title: "50/50 混合随机读写 QD64 × 2 作业（YABS 口径）", Columns: []string{"块大小", "读", "读 IOPS", "写", "写 IOPS", "合计"}, Rows: [][]string{
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
	for _, forbidden := range []string{"crystal_", "atto_", "fio_mixed_"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("txt 不应扁平化/输出冗余内容 %q:\n%s", forbidden, out)
		}
	}
	jsonBytes, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "内部 notice") || !strings.Contains(string(jsonBytes), "内部 notice") || !strings.Contains(string(jsonBytes), "crystal_rnd4k_q1_read_mib_s") {
		t.Fatal("隐藏 txt notice/flat metrics 不应修改 JSON 数据")
	}
}

func TestTextBacktraceRendersHopDetailsAndMasksIPs(t *testing.T) {
	data := textSampleReport()
	data.Results = []model.Result{{
		ID: "backtrace", Title: "三网回程", Status: model.StatusOK,
		Summary: "电信 CN2（AS4809）",
		Tables: []model.Table{
			{Title: "本机出口", Columns: []string{"项目", "IP"}, Rows: [][]string{{"IPv4", "203.0.113.44"}}, SensitiveColumns: []int{1}},
			{Title: "逐跳明细", Columns: []string{"参考目标", "运营商", "跳数", "延迟", "IP", "ASN", "网络/线路", "地理位置", "状态"}, Rows: [][]string{
				{"北京电信", "电信", "1", "0.512 ms", "10.0.0.1", "—", "—", "—", "已响应"},
				{"北京电信", "电信", "2", "—", "—", "—", "—", "—", "无响应"},
				{"北京电信", "电信", "3", "32.118 ms", "59.43.130.22", "AS4809", "China Telecom", "Shanghai", "已响应"},
			},
			},
		},
	}}
	redacted := model.RedactedCopy(data, false)
	plain := Text(redacted, TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{"本机出口", "逐跳明细", "北京电信", "0.512 ms", "32.118 ms", "AS4809", "China Telecom", "无响应", "203.0.x.x", "59.43.130.22"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("回程逐跳报告缺少 %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "203.0.113.44") {
		t.Fatalf("脱敏后的终端报告仍暴露本机完整 IP:\n%s", plain)
	}
	for _, level := range []termcolor.Level{termcolor.LevelNone, termcolor.LevelTrueColor} {
		out := Text(redacted, TextOptions{Color: level})
		for lineNumber, line := range strings.Split(out, "\n") {
			if width := textwidth.Width(line); width > textWidth {
				t.Fatalf("%v 档第 %d 行超出 %d 列：%d", level, lineNumber+1, textWidth, width)
			}
		}
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

func barDensityCountForTest(value string) int {
	return strings.Count(value, "░") + strings.Count(value, "▒") +
		strings.Count(value, "▓") + strings.Count(value, "█")
}

// ATTO's read and write throughput columns are one metric group.  Scaling each
// column independently makes the 4.63 MiB/s read maximum look as long as a
// 1000+ MiB/s write maximum; the complete block-size matrix must expose that
// difference instead.
func TestATTOMatrixBarsUseSharedThroughputScale(t *testing.T) {
	blocks := []string{"512B", "1K", "2K", "4K", "8K", "16K", "32K", "64K", "128K", "256K", "512K", "1M", "2M", "4M", "8M", "16M", "32M", "64M"}
	read := []float64{0.21, 0.42, 0.83, 1.12, 1.48, 1.91, 2.14, 2.49, 2.87, 3.12, 3.35, 3.61, 3.82, 4.01, 4.19, 4.36, 4.52, 4.63}
	write := []float64{12, 24, 41, 68, 107, 155, 211, 278, 344, 421, 503, 594, 677, 748, 819, 1000, 5000, 10000}
	table := model.Table{
		Title:                 "ATTO",
		Columns:               []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
		NumericColumns:        []int{1, 2, 3, 4},
		NumericHigherIsBetter: []bool{true, true, true, true},
	}
	for index, block := range blocks {
		table.Rows = append(table.Rows, []string{
			block,
			fmt.Sprintf("%.2f MiB/s", read[index]), "1 IOPS",
			fmt.Sprintf("%.0f MiB/s", write[index]), "1 IOPS", "完成",
		})
	}
	rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone})
	low := barDensityCountForTest(rows[len(rows)-1][1])
	thousand := barDensityCountForTest(rows[len(rows)-2][3])
	tenThousand := barDensityCountForTest(rows[len(rows)-1][3])
	if low >= thousand || thousand >= tenThousand || tenThousand != 8 {
		t.Fatalf("ATTO read/write throughput scale should separate 4.63/1000/10000: 4.63=%d 1000=%d 10000=%d\nread=%q\nwrite=%q", low, thousand, tenThousand, rows[len(rows)-1][1], rows[len(rows)-1][3])
	}
}

// Units and metric qualifiers remain scale boundaries.  A GiB/s column must
// not borrow the MiB/s maximum, and P50/P95 latency are distinct metrics even
// though both cells carry milliseconds.
func TestTableBarsDoNotMixUnitsOrMetricQualifiers(t *testing.T) {
	unitTable := model.Table{
		Columns: []string{"样本", "读吞吐", "写吞吐"},
		Rows: [][]string{
			{"A", "4.63 MiB/s", "1.00 GiB/s"},
			{"B", "100 MiB/s", "0.50 GiB/s"},
		},
		NumericColumns: []int{1, 2}, NumericHigherIsBetter: []bool{true, true},
	}
	unitRows := tableRowsWithBars(unitTable, termcolor.Palette{Level: termcolor.LevelNone})
	miBBar := barDensityCountForTest(unitRows[0][1])
	giBBar := barDensityCountForTest(unitRows[0][2])
	if miBBar >= giBBar {
		t.Fatalf("different units should not share a numeric scale: MiB/s=%d GiB/s=%d\n%q", miBBar, giBBar, unitRows[0])
	}

	metricTable := model.Table{
		Columns: []string{"样本", "P50 时延", "P95 时延"},
		Rows: [][]string{
			{"A", "1 ms", "100 ms"},
			{"B", "2 ms", "101 ms"},
		},
		NumericColumns: []int{1, 2}, NumericHigherIsBetter: []bool{true, true},
	}
	metricRows := tableRowsWithBars(metricTable, termcolor.Palette{Level: termcolor.LevelNone})
	p50Bar := barDensityCountForTest(metricRows[0][1])
	p95Bar := barDensityCountForTest(metricRows[0][2])
	if p50Bar == p95Bar {
		t.Fatalf("different metric qualifiers should keep separate scales: P50=%d P95=%d\n%q", p50Bar, p95Bar, metricRows[0])
	}
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
		BaselineSource: "test-reference", BaselineSample: 2,
		Dimensions: []score.DimensionScore{
			{Key: "cpu", Score: 800, Ratio: 0.8, Metrics: []score.MetricScore{
				{Key: "cpu_single", Label: "单线程", Value: 800, Unit: "events/s", Baseline: 1000, Ratio: 0.8, Score: 800},
			}},
			{Key: "disk", Missing: true, MissingReason: "moduleNotRun"},
		},
	}
	out := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone, Score: scored})
	for _, want := range []string{"2/4", "未覆盖全部维度", "样本 2 台", "test-reference"} {
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
	if !strings.Contains(out, "third-method") {
		t.Fatalf("完整文本报告应保留当前工作负载方法: %s", out)
	}
	if !strings.Contains(out, "第三个文本块内容") || !strings.Contains(out, "第三条备注") {
		t.Fatal("完整文本报告缺少当前原始输出或说明")
	}
}

func TestEvidenceCoverageUsesPaletteAndFitsEveryTerminalLevel(t *testing.T) {
	data := textSampleReport()
	data.Results[0].Evidence = model.NewEvidence(3, 4, "sample")
	for _, level := range []termcolor.Level{
		termcolor.LevelNone, termcolor.LevelBasic, termcolor.LevelANSI256, termcolor.LevelTrueColor,
	} {
		out := Text(data, TextOptions{Color: level})
		if !strings.Contains(out, i18n.T("report.evidence")) || !strings.Contains(out, "3/4") || !strings.Contains(out, "75%") {
			t.Fatalf("level %v lost evidence coverage:\n%s", level, out)
		}
		if !strings.Contains(out, "█") && !strings.Contains(out, "▓") && !strings.Contains(out, "▒") {
			t.Fatalf("level %v lost proportional evidence bar:\n%s", level, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if width := textwidth.Width(line); width > textWidth {
				t.Fatalf("level %v evidence report line is %d columns: %q", level, width, line)
			}
		}
	}
}

func TestEvidenceCoverageUsesEnglishPluralUnitsAndNoPlanState(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangEN)
	if got := evidenceText(*model.NewEvidence(3, 4, "query")); got != "3/4 queries · 75% · partial" {
		t.Fatalf("plural evidence text = %q", got)
	}
	if got := evidenceText(*model.NewEvidence(0, 0, "sample")); got != "0/0 samples · no samples planned" {
		t.Fatalf("unplanned evidence text = %q", got)
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

func TestStructuredPercentilesAndRouteCountsUseSeparateBarScales(t *testing.T) {
	items := []model.Measurement{
		{Key: "dns_resolver_01_p50_ms", Value: 10, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "dns_resolver_02_p50_ms", Value: 20, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "dns_resolver_01_p95_ms", Value: 100, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "dns_resolver_02_p95_ms", Value: 200, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "route_target_01_hop_slots", Value: 8, Unit: "hops", HigherIsBetter: model.BoolPtr(false)},
		{Key: "route_target_02_hop_slots", Value: 10, Unit: "hops", HigherIsBetter: model.BoolPtr(false)},
		{Key: "route_target_01_timeout_hops", Value: 1, Unit: "hops", HigherIsBetter: model.BoolPtr(false)},
		{Key: "route_target_02_timeout_hops", Value: 2, Unit: "hops", HigherIsBetter: model.BoolPtr(false)},
	}
	groups := groupComparable(items)
	if groups["dns_resolver_01_p50_ms"].min == groups["dns_resolver_01_p95_ms"].min ||
		groups["route_target_01_hop_slots"].min == groups["route_target_01_timeout_hops"].min {
		t.Fatalf("unlike structured metrics borrowed one bar scale: %+v", groups)
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

func TestMeasurementBarsKeepSemanticDirectionAndZeroValues(t *testing.T) {
	palette := termcolor.Palette{Level: termcolor.LevelNone}
	densityCount := func(value string) int {
		return strings.Count(value, "░") + strings.Count(value, "▒") + strings.Count(value, "▓") + strings.Count(value, "█")
	}

	risk := []model.Measurement{
		{Key: "ip_risk_score_low", Label: "IP risk score", Value: 3, Unit: "/100", HigherIsBetter: model.BoolPtr(false)},
		{Key: "ip_risk_score_high", Label: "IP risk score", Value: 90, Unit: "/100", HigherIsBetter: model.BoolPtr(false)},
		{Key: "ip_risk_score_zero", Label: "IP risk score", Value: 0, Unit: "/100", HigherIsBetter: model.BoolPtr(false)},
	}
	riskGroups := groupComparable(risk)
	if riskGroups["ip_risk_score_low"].inverse {
		t.Fatal("风险分应按原始幅度而不是质量方向绘制")
	}
	lowRiskBar := palette.BarRelative(comparableValue(risk[0], riskGroups["ip_risk_score_low"]), riskGroups["ip_risk_score_low"].max, 24)
	highRiskBar := palette.BarRelative(comparableValue(risk[1], riskGroups["ip_risk_score_high"]), riskGroups["ip_risk_score_high"].max, 24)
	zeroRiskBar := palette.BarRelative(comparableValue(risk[2], riskGroups["ip_risk_score_zero"]), riskGroups["ip_risk_score_zero"].max, 24)
	if densityCount(lowRiskBar) >= densityCount(highRiskBar) || densityCount(zeroRiskBar) != 0 || !strings.Contains(zeroRiskBar, "·") {
		t.Fatalf("风险柱未按 0/3/90 的幅度递增：0=%q 3=%q 90=%q", zeroRiskBar, lowRiskBar, highRiskBar)
	}

	latency := []model.Measurement{
		{Key: "latency_fast", Label: "延迟", Value: 10, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "latency_slow", Label: "延迟", Value: 20, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
	}
	latencyGroups := groupComparable(latency)
	if !latencyGroups["latency_fast"].inverse || comparableValue(latency[0], latencyGroups["latency_fast"]) <= comparableValue(latency[1], latencyGroups["latency_slow"]) {
		t.Fatal("低延迟应得到更长的质量柱")
	}

	throughput := []model.Measurement{
		{Key: "throughput_low", Label: "吞吐", Value: 100, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "throughput_high", Label: "吞吐", Value: 200, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
	}
	throughputGroups := groupComparable(throughput)
	if throughputGroups["throughput_high"].inverse || comparableValue(throughput[0], throughputGroups["throughput_low"]) >= comparableValue(throughput[1], throughputGroups["throughput_high"]) {
		t.Fatal("高吞吐应得到更长的柱")
	}

	usageAndLoss := []model.Measurement{
		{Key: "memory_usage_percent", Label: "内存使用率", Value: 10, Unit: "%", HigherIsBetter: model.BoolPtr(false)},
		{Key: "disk_usage_percent", Label: "磁盘使用率", Value: 20, Unit: "%", HigherIsBetter: model.BoolPtr(false)},
		{Key: "udp_loss_percent", Label: "UDP 丢包", Value: 1, Unit: "%", HigherIsBetter: model.BoolPtr(false)},
		{Key: "tcp_loss_percent", Label: "TCP 丢包", Value: 2, Unit: "%", HigherIsBetter: model.BoolPtr(false)},
	}
	semanticGroups := groupComparable(usageAndLoss)
	if _, ok := semanticGroups["memory_usage_percent"]; !ok {
		t.Fatal("使用率应形成独立可比较组")
	}
	if _, ok := semanticGroups["udp_loss_percent"]; !ok {
		t.Fatal("丢包应形成独立可比较组")
	}
	if semanticGroups["memory_usage_percent"].max == semanticGroups["udp_loss_percent"].max {
		t.Fatal("使用率和丢包不应跨语义共用刻度")
	}

	if groups := groupComparable([]model.Measurement{{Key: "single", Label: "单项", Value: 1, Unit: "u", HigherIsBetter: model.BoolPtr(true)}}); len(groups) != 0 {
		t.Fatalf("单项不应绘制相对柱：%v", groups)
	}

	resources := []model.Measurement{
		{Key: "memory_used_bytes", Label: "已用内存", Value: 100, Unit: "bytes", HigherIsBetter: model.BoolPtr(false)},
		{Key: "memory_cached_bytes", Label: "缓存内存", Value: 50, Unit: "bytes", HigherIsBetter: model.BoolPtr(false)},
		{Key: "memory_available_bytes", Label: "可用内存", Value: 200, Unit: "bytes", HigherIsBetter: model.BoolPtr(true)},
		{Key: "memory_total_bytes", Label: "总内存", Value: 300, Unit: "bytes", HigherIsBetter: model.BoolPtr(true)},
		{Key: "disk_used_bytes", Label: "已用磁盘", Value: 1000, Unit: "bytes", HigherIsBetter: model.BoolPtr(false)},
		{Key: "disk_reserved_bytes", Label: "预留磁盘", Value: 500, Unit: "bytes", HigherIsBetter: model.BoolPtr(false)},
		{Key: "disk_available_bytes", Label: "可用磁盘", Value: 2000, Unit: "bytes", HigherIsBetter: model.BoolPtr(true)},
		{Key: "disk_total_bytes", Label: "总磁盘", Value: 3000, Unit: "bytes", HigherIsBetter: model.BoolPtr(true)},
	}
	resourceGroups := groupComparable(resources)
	if resourceGroups["memory_used_bytes"].max == resourceGroups["disk_used_bytes"].max ||
		resourceGroups["memory_available_bytes"].max == resourceGroups["disk_available_bytes"].max {
		t.Fatal("内存与磁盘容量不应共用同一柱状图刻度")
	}
}

func TestMeasurementBarsUseAdaptiveScaleForWideRanges(t *testing.T) {
	items := []model.Measurement{
		{Key: "throughput_low", Label: "吞吐", Value: 4, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "throughput_mid", Label: "吞吐", Value: 1000, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "throughput_high", Label: "吞吐", Value: 10000, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
	}
	groups := groupComparable(items)
	for _, item := range items {
		if group, ok := groups[item.Key]; !ok {
			t.Fatalf("wide-range measurement %q did not form a comparison group", item.Key)
		} else if group.min != 4 {
			t.Fatalf("wide-range group minimum = %v, want 4", group.min)
		}
	}
	p := termcolor.Palette{Level: termcolor.LevelNone}
	bar := func(item model.Measurement, group comparableGroup) int {
		return barDensityCountForTest(p.BarRelativeRange(comparableValue(item, group), group.min, group.max, barWidth))
	}
	low := bar(items[0], groups[items[0].Key])
	mid := bar(items[1], groups[items[1].Key])
	high := bar(items[2], groups[items[2].Key])
	if !(low < mid && mid < high) {
		t.Fatalf("adaptive measurement bars should preserve 4<1000<10000: low=%d mid=%d high=%d", low, mid, high)
	}

	near := []model.Measurement{
		{Key: "near_low", Label: "吞吐", Value: 100, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "near_mid", Label: "吞吐", Value: 200, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "near_high", Label: "吞吐", Value: 300, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
	}
	nearGroups := groupComparable(near)
	nearLow := bar(near[0], nearGroups[near[0].Key])
	nearHigh := bar(near[2], nearGroups[near[2].Key])
	if nearLow >= nearHigh {
		t.Fatalf("compact-range bars should remain linear and ordered: low=%d high=%d", nearLow, nearHigh)
	}

	latency := []model.Measurement{
		{Key: "latency_best", Label: "延迟", Value: 1, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "latency_mid", Label: "延迟", Value: 1000, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
		{Key: "latency_worst", Label: "延迟", Value: 10000, Unit: "ms", HigherIsBetter: model.BoolPtr(false)},
	}
	latencyGroups := groupComparable(latency)
	best := bar(latency[0], latencyGroups[latency[0].Key])
	worst := bar(latency[2], latencyGroups[latency[2].Key])
	if best <= worst {
		t.Fatalf("lower-is-better adaptive bars should keep the best value longest: best=%d worst=%d", best, worst)
	}
}

func TestMeasurementBarsIgnoreNonFiniteValues(t *testing.T) {
	items := []model.Measurement{
		{Key: "nan", Label: "吞吐", Value: math.NaN(), Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "inf", Label: "吞吐", Value: math.Inf(1), Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
	}
	if groups := groupComparable(items); len(groups) != 0 {
		t.Fatalf("NaN/Inf measurements should not create scales: %v", groups)
	}
}

func TestMeasurementBarsKeepMatrixScopesSeparate(t *testing.T) {
	items := []model.Measurement{
		{Key: "crystal_rnd4k_q1_read_mib_s", Label: "Crystal 读吞吐", Value: 1, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "crystal_seq1m_q1_read_mib_s", Label: "Crystal 读吞吐", Value: 2, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "atto_512b_read_mib_s", Label: "ATTO 读吞吐", Value: 100, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
		{Key: "atto_64m_read_mib_s", Label: "ATTO 读吞吐", Value: 200, Unit: "MiB/s", HigherIsBetter: model.BoolPtr(true)},
	}
	groups := groupComparable(items)
	if groups[items[0].Key].max == groups[items[2].Key].max {
		t.Fatalf("Crystal and ATTO measurements should not share a MiB/s scale: %+v", groups)
	}
}
