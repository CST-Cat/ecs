package report

import (
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// Localize 按当前语言翻译整份报告的可见文本。
//
// 翻译放在渲染层而不是探针里：探针照常产出中文，这里统一按原文查表。
// 好处是 probe 包一行不用改；代价是原文改字会丢译文，因此有测试遍历真实运行
// 产出的文本核对覆盖率。
//
// 返回新的副本，不修改入参。选定的语言适用于全部输出格式，JSON 也一样：
// 用户选了英文就该拿到英文报告。机器标识符不在翻译范围内——模块 id、
// measurement.key/method/unit、status、methodology.kind 都保持原样，
// 因此下游按这些字段解析不受语言影响。
func Localize(data model.Report) model.Report {
	if i18n.Current() == i18n.LangZH {
		return data
	}
	out := data
	out.Notices = i18n.TextSlice(data.Notices)
	out.Summary.Headline = i18n.Text(data.Summary.Headline)
	out.Results = make([]model.Result, len(data.Results))
	for index, result := range data.Results {
		out.Results[index] = localizeResult(result)
	}
	return out
}

func localizeResult(result model.Result) model.Result {
	out := result
	out.Title = i18n.Text(result.Title)
	out.Description = i18n.Text(result.Description)
	out.Summary = i18n.Text(result.Summary)
	out.Error = i18n.Text(result.Error)
	out.Methodology.Label = i18n.Text(result.Methodology.Label)
	// Engine and profile are human-facing methodology text. Stable machine
	// identifiers remain untouched below in measurement.key/method/unit.
	out.Methodology.Engine = i18n.Text(result.Methodology.Engine)
	out.Methodology.Profile = i18n.Text(result.Methodology.Profile)
	out.Methodology.ComparisonScope = i18n.Text(result.Methodology.ComparisonScope)
	out.Notes = i18n.TextSlice(result.Notes)

	out.Fields = make([]model.Field, len(result.Fields))
	for index, field := range result.Fields {
		field.Label = i18n.Text(field.Label)
		field.Value = i18n.Text(field.Value)
		out.Fields[index] = field
	}
	out.Measurements = make([]model.Measurement, len(result.Measurements))
	for index, measurement := range result.Measurements {
		measurement.Label = i18n.Text(measurement.Label)
		measurement.Display = i18n.Text(measurement.Display)
		measurement.Rating = i18n.Text(measurement.Rating)
		// unit is part of the machine-readable measurement contract, even when
		// its spelling happens to be human-readable (for example "线程").  Keep
		// both unit and method byte-for-byte stable across language exports.
		out.Measurements[index] = measurement
	}
	out.Tables = make([]model.Table, len(result.Tables))
	for index, table := range result.Tables {
		localized := table
		localized.Title = i18n.Text(table.Title)
		localized.Columns = i18n.TextSlice(table.Columns)
		localized.Rows = make([][]string, len(table.Rows))
		for rowIndex, row := range table.Rows {
			localized.Rows[rowIndex] = i18n.TextSlice(row)
		}
		out.Tables[index] = localized
	}
	out.TextBlocks = make([]model.TextBlock, len(result.TextBlocks))
	for index, block := range result.TextBlocks {
		// 只翻标题：正文是外部工具的原始输出，翻译它就等于篡改证据。
		block.Title = i18n.Text(block.Title)
		out.TextBlocks[index] = block
	}
	out.Sources = make([]model.Source, len(result.Sources))
	for index, source := range result.Sources {
		source.Purpose = i18n.Text(source.Purpose)
		out.Sources[index] = source
	}
	return out
}
