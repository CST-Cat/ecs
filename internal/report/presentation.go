package report

import (
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// displayKey resolves a field whose report contract identifies it as a
// presentation key. i18n.T deliberately returns an unknown key unchanged, so
// missing catalog entries remain visible without treating arbitrary content as
// a candidate for translation.
func displayKey(key string) string {
	if key == "" {
		return ""
	}
	return i18n.T(key)
}

// displayValue resolves only the explicit Value variant. Raw values are
// provider output or diagnostics and must remain literal; key values are the
// stable ECS keys that belong to the current presentation language.
func displayValue(value model.Value) string {
	if raw, ok := value.Raw(); ok {
		return raw
	}
	if key, ok := value.Key(); ok {
		return i18n.T(key)
	}
	return value.Text()
}

func reportSummaryText(summary model.Summary) string {
	return renderMessages(summary.Messages)
}

func resultSummary(result model.Result) string {
	return renderMessages(result.SummaryMessages)
}

func displayMeasurement(measurement model.Measurement) model.Measurement {
	measurement.Label = displayKey(measurement.Label)
	measurement.Rating = displayKey(measurement.Rating)
	return measurement
}

func displayField(field model.Field) model.Field {
	field.Label = displayKey(field.Label)
	return field
}

func displayTable(table model.Table) model.Table {
	table.Title = displayKey(table.Title)
	if table.Columns != nil {
		columns := table.Columns
		table.Columns = make([]model.TableColumn, len(columns))
		for index, column := range columns {
			table.Columns[index] = displayTableColumn(column)
		}
	}
	return table
}

func displayTableColumn(column model.TableColumn) model.TableColumn {
	column.Label = displayKey(column.Label)
	return column
}

func displayTableColumnLabel(column model.TableColumn) string {
	return displayTableColumn(column).Label
}

// columnLabels extracts labels that displayTable has already resolved. It does
// not translate; callers pass the result of displayTable.
func columnLabels(columns []model.TableColumn) []string {
	labels := make([]string, len(columns))
	for index, column := range columns {
		labels[index] = column.Label
	}
	return labels
}

func displayTableRows(table model.Table) [][]string {
	rows := make([][]string, len(table.Rows))
	for rowIndex, row := range table.Rows {
		rows[rowIndex] = make([]string, len(row))
		for columnIndex, value := range row {
			rows[rowIndex][columnIndex] = displayValue(value)
		}
	}
	return rows
}

func displayMethodology(methodology model.Methodology) model.Methodology {
	methodology.Label = displayKey(methodology.Label)
	methodology.Engine = displayKey(methodology.Engine)
	methodology.Profile = displayKey(methodology.Profile)
	methodology.ComparisonScope = displayKey(methodology.ComparisonScope)
	return methodology
}
