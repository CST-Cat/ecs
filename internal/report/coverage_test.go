package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

var hanPattern = regexp.MustCompile(`[\p{Han}]`)

// 翻译覆盖率检查：把真实运行产出的报告过一遍 Localize，
// 任何仍含中文的可见文本都说明译文表漏了一句。
//
// 用当前报告结构而不是孤立源码字面量：源码里有大量条件分支从不触发，
// 而运行产出的才是用户真正会看到的。CI 可以通过 ECS_I18N_SAMPLES 提供
// 当前 JSON 样本；没有样本时使用当前 renderer contract，不重新引入已删除样本。
func TestLocalizeCoversRealReports(t *testing.T) {
	dir := os.Getenv("ECS_I18N_SAMPLES")
	if dir == "" {
		t.Skip("未设置 ECS_I18N_SAMPLES，跳过外部报告样本检查")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("读取样本目录 %s 失败：%v", dir, err)
	}
	i18n.Set(i18n.LangEN)
	defer i18n.Set(i18n.LangZH)

	missing := map[string]bool{}
	reports := make([]model.Report, 0, len(files)+1)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var data model.Report
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		reports = append(reports, data)
	}
	if len(reports) == 0 {
		reports = append(reports, rendererContractReport())
	}
	for _, data := range reports {
		collectUntranslated(Localize(data), missing)
	}
	if len(missing) == 0 {
		return
	}
	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	t.Errorf("英文报告里仍有 %d 条中文未翻译：", len(keys))
	for _, key := range keys {
		t.Errorf("  %s", key)
	}
}

func collectUntranslated(data model.Report, missing map[string]bool) {
	check := func(value string) {
		if value != "" && hanPattern.MatchString(value) {
			missing[value] = true
		}
	}
	for _, notice := range data.Notices {
		check(notice)
	}
	check(data.Summary.Headline)
	for _, result := range data.Results {
		check(result.Title)
		check(result.Description)
		check(result.Summary)
		// 刻意不检查 Error：它承载外部工具与系统调用的原文（fio 的报错、
		// 系统的 permission denied），翻译它等于篡改诊断信息，
		// 与 TextBlock 正文同理。
		check(result.Methodology.Label)
		check(result.Methodology.Engine)
		check(result.Methodology.Profile)
		check(result.Methodology.ComparisonScope)
		for _, note := range result.Notes {
			check(note)
		}
		for _, field := range result.Fields {
			check(field.Label)
			check(field.Value)
		}
		for _, measurement := range result.Measurements {
			check(measurement.Label)
			check(measurement.Display)
			check(measurement.Rating)
			// Unit and Method are stable machine-readable fields. They are
			// intentionally preserved byte-for-byte by Localize.
		}
		for _, table := range result.Tables {
			check(table.Title)
			for _, column := range table.Columns {
				check(column)
			}
			for _, row := range table.Rows {
				for _, cell := range row {
					check(cell)
				}
			}
		}
		for _, block := range result.TextBlocks {
			// 正文是外部工具原始输出，本就不翻译。
			check(block.Title)
		}
		for _, source := range result.Sources {
			check(source.Purpose)
		}
	}
}
