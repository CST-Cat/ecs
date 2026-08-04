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
	for _, want := range []string{"全部模块", "本次选择", "CPU 性能", "流媒体与 AI 服务", "三网回程"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("navigation missing %q:\n%s", want, plain)
		}
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
		BaselineSource: "builtinSingleHost", BaselineSample: 1,
		Dimensions: []score.DimensionScore{
			{Key: "cpu", Score: 800, Ratio: 0.8, Metrics: []score.MetricScore{
				{Key: "cpu_single", Label: "单线程", Value: 800, Unit: "events/s", Baseline: 1000, Ratio: 0.8, Score: 800},
			}},
			{Key: "disk", Missing: true, MissingReason: "moduleNotRun"},
		},
	}
	out := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone, Score: scored})
	for _, want := range []string{"2/4", "未跑满全部维度", "只有 1 个样本", "内置单机快照"} {
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
	for _, heading := range []string{"一、结果一", "二、结果二", "三、结果三", "四、结果四", "五、结果五"} {
		if !strings.Contains(plain, heading) {
			t.Fatalf("missing numbered result %q:\n%s", heading, plain)
		}
	}
	if strings.Contains(plain, "六、") {
		t.Fatal("没有评分时不应出现额外章节编号")
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
	for _, heading := range []string{"1. 结果一", "2. 结果二", "3. 结果三", "4. 结果四", "5. 结果五", "6. Composite score"} {
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
		"第二张表", "第三张表值", "原始文本", "第三个文本块内容", "第三条备注",
		"需留意", "custom-kind", "自定义方法", "custom-engine", "custom-profile", "custom-scope",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("完整文本报告缺少 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "third-method") {
		t.Fatalf("纯文本不应显示 Measurement.Method: %s", out)
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
