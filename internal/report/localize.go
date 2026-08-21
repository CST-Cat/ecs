package report

import (
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// Localize 按当前语言翻译整份报告的可见文本，并返回独立的展示副本。
//
// 新迁移的 ECS 自生成动态文本从 model.Message 直接按当前语言渲染；尚未迁移的
// 探针字段仍暂时经过历史 source-text 兼容层。外部工具原始输出不翻译。
//
// 返回值与 data 不共享可变的 slice、map 或 pointer。JSON 的写入方继续使用原始
// machine report；Localize 只是迁移期间的人类展示边界，后续会在全部字段结构化后删除。
func Localize(data model.Report) model.Report {
	out := data
	out.Run.Requested = cloneStrings(data.Run.Requested)
	out.Run.OutputFormats = cloneStrings(data.Run.OutputFormats)
	out.Notices = cloneModelMessages(data.Notices)
	out.SensitiveIPs = cloneStrings(data.SensitiveIPs)
	out.Summary.Messages = cloneModelMessages(data.Summary.Messages)
	if len(data.Summary.Messages) > 0 {
		out.Summary.Headline = renderMessages(data.Summary.Messages)
	} else {
		out.Summary.Headline = i18n.Text(data.Summary.Headline)
	}
	if data.Results == nil {
		out.Results = nil
	} else {
		out.Results = make([]model.Result, len(data.Results))
		for index, result := range data.Results {
			out.Results[index] = localizeResult(result)
		}
	}
	return out
}

func localizeResult(result model.Result) model.Result {
	out := result
	out.Methodology.Parameters = make(map[string]string, len(result.Methodology.Parameters))
	if result.Methodology.Parameters == nil {
		out.Methodology.Parameters = nil
	} else {
		for key, value := range result.Methodology.Parameters {
			// Parameter keys and values form the machine comparison signature and
			// must remain byte-stable across output languages.
			out.Methodology.Parameters[key] = value
		}
	}
	out.Title = i18n.Text(result.Title)
	out.Description = i18n.Text(result.Description)
	out.SummaryMessages = cloneModelMessages(result.SummaryMessages)
	if len(result.SummaryMessages) > 0 {
		out.Summary = renderMessages(result.SummaryMessages)
	} else {
		out.Summary = i18n.Text(result.Summary)
	}
	out.Error = i18n.Text(result.Error)
	out.Methodology.Label = i18n.Text(result.Methodology.Label)
	// Engine and profile are human-facing methodology text. Stable machine
	// identifiers remain untouched below in measurement.key/method/unit.
	out.Methodology.Engine = i18n.Text(result.Methodology.Engine)
	out.Methodology.Profile = i18n.Text(result.Methodology.Profile)
	out.Methodology.ComparisonScope = i18n.Text(result.Methodology.ComparisonScope)
	out.Notes = localizeStrings(result.Notes)

	if result.Fields == nil {
		out.Fields = nil
	} else {
		out.Fields = make([]model.Field, len(result.Fields))
		for index, field := range result.Fields {
			field.Label = i18n.Text(field.Label)
			field.Value = i18n.Text(field.Value)
			out.Fields[index] = field
		}
	}
	if result.Measurements == nil {
		out.Measurements = nil
	} else {
		out.Measurements = make([]model.Measurement, len(result.Measurements))
		for index, measurement := range result.Measurements {
			out.Measurements[index] = localizeMeasurement(measurement)
		}
	}
	if result.Tables == nil {
		out.Tables = nil
	} else {
		out.Tables = make([]model.Table, len(result.Tables))
		for index, table := range result.Tables {
			localized := table
			localized.Title = i18n.Text(table.Title)
			localized.Columns = localizeStrings(table.Columns)
			// ColumnKeys and RowIdentity are machine schema, never display text.
			localized.ColumnKeys = cloneStrings(table.ColumnKeys)
			localized.NumericColumns = cloneInts(table.NumericColumns)
			localized.NumericHigherIsBetter = cloneBools(table.NumericHigherIsBetter)
			localized.SensitiveColumns = cloneInts(table.SensitiveColumns)
			if table.Rows == nil {
				localized.Rows = nil
			} else {
				localized.Rows = make([][]string, len(table.Rows))
				for rowIndex, row := range table.Rows {
					localized.Rows[rowIndex] = localizeStrings(row)
				}
			}
			out.Tables[index] = localized
		}
	}
	if result.TextBlocks == nil {
		out.TextBlocks = nil
	} else {
		out.TextBlocks = make([]model.TextBlock, len(result.TextBlocks))
		for index, block := range result.TextBlocks {
			// 只翻标题：正文是外部工具的原始输出，翻译它就等于篡改证据。
			block.Title = i18n.Text(block.Title)
			out.TextBlocks[index] = block
		}
	}
	if result.Sources == nil {
		out.Sources = nil
	} else {
		out.Sources = make([]model.Source, len(result.Sources))
		for index, source := range result.Sources {
			source.Purpose = i18n.Text(source.Purpose)
			out.Sources[index] = source
		}
	}
	out.Failures = append([]model.Failure(nil), result.Failures...)
	if result.Evidence != nil {
		evidence := *result.Evidence
		out.Evidence = &evidence
	}
	if result.Retry != nil {
		retry := *result.Retry
		retry.SelectionRule = i18n.Text(result.Retry.SelectionRule)
		retry.TriggerReasons = localizeStrings(result.Retry.TriggerReasons)
		if result.Retry.Attempts == nil {
			retry.Attempts = nil
		} else {
			retry.Attempts = make([]model.RetryAttempt, len(result.Retry.Attempts))
			for attemptIndex, attempt := range result.Retry.Attempts {
				localized := attempt
				localized.Interference.Reasons = localizeStrings(attempt.Interference.Reasons)
				if attempt.Interference.Measurements == nil {
					localized.Interference.Measurements = nil
				} else {
					localized.Interference.Measurements = make([]model.Measurement, len(attempt.Interference.Measurements))
					for measurementIndex, measurement := range attempt.Interference.Measurements {
						localized.Interference.Measurements[measurementIndex] = localizeMeasurement(measurement)
					}
				}
				if attempt.Measurements == nil {
					localized.Measurements = nil
				} else {
					localized.Measurements = make([]model.Measurement, len(attempt.Measurements))
					for measurementIndex, measurement := range attempt.Measurements {
						localized.Measurements[measurementIndex] = localizeMeasurement(measurement)
					}
				}
				if attempt.Evidence != nil {
					evidence := *attempt.Evidence
					localized.Evidence = &evidence
				}
				retry.Attempts[attemptIndex] = localized
			}
		}
		out.Retry = &retry
	}
	return out
}

func localizeMeasurement(measurement model.Measurement) model.Measurement {
	out := measurement
	out.Label = i18n.Text(measurement.Label)
	out.Display = i18n.Text(measurement.Display)
	out.Rating = i18n.Text(measurement.Rating)
	if measurement.HigherIsBetter != nil {
		higherIsBetter := *measurement.HigherIsBetter
		out.HigherIsBetter = &higherIsBetter
	}
	// unit is part of the machine-readable measurement contract, even when
	// its spelling happens to be human-readable (for example "线程"). Keep
	// both unit and method byte-for-byte stable across language exports.
	return out
}

// localizeStrings always allocates for a non-nil slice and resolves each item
// through the temporary probe source-text compatibility path.
func localizeStrings(items []string) []string {
	if items == nil {
		return nil
	}
	out := make([]string, len(items))
	for index, item := range items {
		out[index] = i18n.Text(item)
	}
	return out
}

func cloneModelMessages(items []model.Message) []model.Message {
	if items == nil {
		return nil
	}
	out := make([]model.Message, len(items))
	for index, message := range items {
		out[index] = message
		out[index].Args = cloneStrings(message.Args)
	}
	return out
}

func cloneStrings(items []string) []string {
	if items == nil {
		return nil
	}
	return append([]string(nil), items...)
}

func cloneInts(items []int) []int {
	if items == nil {
		return nil
	}
	return append([]int(nil), items...)
}

func cloneBools(items []bool) []bool {
	if items == nil {
		return nil
	}
	return append([]bool(nil), items...)
}
