package report

import (
	"strings"
	"testing"
	"time"

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
			StartedAt: time.Unix(0, 0).UTC(), Redacted: true,
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
