package report

import (
	"fmt"

	comparison "ecs/internal/compare"
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

// statusText owns the shared status glyph and localized label. Renderers keep
// their own surrounding markup and tone, but do not re-decide this semantic
// pair independently.
func statusText(status model.Status) string {
	return statusIcon(status) + " " + statusLabel(status)
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

// displayFailureCount preserves the report contract that a recorded failure
// always has at least one occurrence in human-readable output.
func displayFailureCount(count int) int {
	if count < 1 {
		return 1
	}
	return count
}

// displayComparisonStatus resolves availability and the shared status text;
// colors and markup remain renderer-owned.
func displayComparisonStatus(value comparison.StatusValue) string {
	if !value.Available {
		return "—"
	}
	return statusText(value.Status)
}

// displayComparisonEvidence resolves the shared count/grade text. The
// separator is supplied by each renderer because punctuation belongs to its
// layout, while availability, counters and grade localization do not.
func displayComparisonEvidence(value comparison.EvidenceValue, separator string) string {
	if !value.Available {
		return "—"
	}
	return fmt.Sprintf("%d/%d%s%s", value.Valid, value.Expected, separator, comparisonEvidenceGrade(derivedComparisonEvidenceGrade(value)))
}

func displayComparisonObservation(value comparison.ObservationValue) string {
	if !value.Available {
		return "—"
	}
	return value.Value
}

// sortComparisonValues keeps available values ordered by rank and missing
// values in their input order. Every comparison renderer uses the same order
// when it switches to the ranked many-report layout.
func sortComparisonValues(values []comparison.MetricValue) {
	for index := 1; index < len(values); index++ {
		current := values[index]
		position := index
		for position > 0 && comparisonValueBefore(current, values[position-1]) {
			values[position] = values[position-1]
			position--
		}
		values[position] = current
	}
}

func comparisonValueBefore(left, right comparison.MetricValue) bool {
	if left.Available != right.Available {
		return left.Available
	}
	if !left.Available {
		return false
	}
	return left.Rank < right.Rank
}

func derivedComparisonEvidenceGrade(evidence comparison.EvidenceValue) model.EvidenceGrade {
	return evidence.DerivedGrade()
}

func comparisonEvidenceGrade(grade model.EvidenceGrade) string {
	key := "evidence." + string(grade)
	if grade == model.EvidenceNotPlanned {
		key = "evidence.notPlanned"
	}
	translated := i18n.T(key)
	if translated == key {
		return string(grade)
	}
	return translated
}
