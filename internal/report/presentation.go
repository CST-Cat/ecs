package report

import (
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// displayReportText is the only string-level lookup used by report views. A
// value is translated only when it is a registered stable key; arbitrary
// provider data, command output, identifiers, and diagnostics pass through
// unchanged.
func displayReportText(value string) string {
	if value == "" {
		return ""
	}
	if i18n.Has(i18n.Current(), value) {
		return i18n.T(value)
	}
	return value
}

func reportHeadline(summary model.Summary) string {
	if len(summary.Messages) > 0 {
		return renderMessages(summary.Messages)
	}
	return displayReportText(summary.Headline)
}

func resultSummary(result model.Result) string {
	if len(result.SummaryMessages) > 0 {
		return renderMessages(result.SummaryMessages)
	}
	return displayReportText(result.Summary)
}

func displayMeasurement(measurement model.Measurement) model.Measurement {
	measurement.Label = displayReportText(measurement.Label)
	measurement.Display = displayReportText(measurement.Display)
	measurement.Rating = displayReportText(measurement.Rating)
	return measurement
}

func displayField(field model.Field) model.Field {
	field.Label = displayReportText(field.Label)
	field.Value = displayReportText(field.Value)
	return field
}

func displayTable(table model.Table) model.Table {
	table.Title = displayReportText(table.Title)
	if table.Columns != nil {
		columns := table.Columns
		table.Columns = make([]string, len(columns))
		for index, column := range columns {
			table.Columns[index] = displayReportText(column)
		}
	}
	if table.Rows != nil {
		rows := table.Rows
		table.Rows = make([][]string, len(rows))
		for rowIndex, row := range rows {
			table.Rows[rowIndex] = make([]string, len(row))
			for columnIndex, value := range row {
				table.Rows[rowIndex][columnIndex] = displayReportText(value)
			}
		}
	}
	return table
}

func displayMethodology(methodology model.Methodology) model.Methodology {
	methodology.Label = displayReportText(methodology.Label)
	methodology.Engine = displayReportText(methodology.Engine)
	methodology.Profile = displayReportText(methodology.Profile)
	methodology.ComparisonScope = displayReportText(methodology.ComparisonScope)
	return methodology
}
