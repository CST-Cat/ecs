package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
	"ecs/internal/termcolor"
)

// ComparisonOptions controls rendering of a multi-report comparison.
// Structured JSON, Markdown and HTML never contain terminal escape sequences;
// TextColor only applies to the txt artifact.
type ComparisonOptions struct {
	TextColor termcolor.Level
}

// WriteComparisonFiles writes one comparison in the same four formats as a
// normal ecs report.  Paths are always resolved below an explicit output
// directory and each artifact is installed atomically.
func WriteComparisonFiles(data comparison.Report, directory, baseName string, formats []string, options ComparisonOptions) (map[string]string, error) {
	if directory == "" {
		directory = "./reports"
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, i18n.Errorf("err.reportOutputDir", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, i18n.Errorf("err.reportCreateDir", err)
	}
	if baseName == "" {
		baseName = "ecs-compare-" + data.GeneratedAt.Format("20060102-150405")
	}
	baseName = sanitizeBaseName(baseName)
	written := make(map[string]string)
	for _, format := range formats {
		var content []byte
		switch format {
		case "json":
			content, err = ComparisonJSON(data)
		case "txt":
			content = []byte(ComparisonText(data, options.TextColor))
		case "md":
			content = []byte(ComparisonMarkdown(data))
		case "html":
			content, err = ComparisonHTML(data)
		default:
			err = i18n.Errorf("err.reportUnknownFormat", format)
		}
		if err != nil {
			return written, i18n.Errorf("err.reportGenerate", format, err)
		}
		path := filepath.Join(absolute, baseName+"."+format)
		if err := atomicWrite(path, content, 0o600); err != nil {
			return written, i18n.Errorf("err.reportWrite", format, err)
		}
		written[format] = path
	}
	return written, nil
}

func ComparisonJSON(data comparison.Report) ([]byte, error) {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func comparisonInput(data comparison.Report, index int) comparison.Input {
	if index >= 0 && index < len(data.Inputs) {
		return data.Inputs[index]
	}
	return comparison.Input{Index: index, Label: i18n.T("compare.missing")}
}

func comparisonInputLabel(data comparison.Report, index int) string {
	input := comparisonInput(data, index)
	label := input.Label
	if index == data.Reference {
		label += " (" + i18n.T("compare.referenceMark") + ")"
	}
	return label
}

func comparisonModuleTitle(module comparison.Module) string {
	key := "module." + module.ID + ".title"
	if i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	if module.Title != "" {
		return module.Title
	}
	return module.ID
}

func comparisonLabel(key string) string {
	translated := i18n.T("compare." + key)
	if translated == "compare."+key {
		return key
	}
	return translated
}

func comparisonIssueLabel(reason string) string {
	key := "compare.issue." + reason
	translated := i18n.T(key)
	if translated == key {
		return reason
	}
	return translated
}

func comparisonOutcomeLabel(outcome comparison.Outcome) string {
	key := "compare.outcome." + string(outcome)
	translated := i18n.T(key)
	if translated == key {
		return string(outcome)
	}
	return translated
}

func comparisonValueDisplay(value comparison.MetricValue) string {
	if !value.Available {
		return "—"
	}
	if value.Display != "" {
		return value.Display
	}
	return formatComparisonNumber(value.Value)
}

func comparisonMetricDisplay(metric comparison.Metric, value comparison.MetricValue) string {
	if !value.Available {
		return "—"
	}
	if value.Display != "" {
		return value.Display
	}
	display := formatComparisonNumber(value.Value)
	if metric.Unit != "" {
		display += " " + metric.Unit
	}
	return display
}

func formatComparisonNumber(value float64) string {
	return formatAdaptiveFloat(value)
}

func comparisonChange(value comparison.MetricValue, reference bool) string {
	if !value.Available {
		return "—"
	}
	if reference {
		return i18n.T("compare.referenceMark")
	}
	if value.PerformanceChangePercent == nil {
		return comparisonOutcomeLabel(value.Outcome)
	}
	change := *value.PerformanceChangePercent
	prefix := ""
	if change > 0 {
		prefix = "+"
	}
	return prefix + formatAdaptiveFloat(change) + "% " + comparisonOutcomeLabel(value.Outcome)
}

func formatAdaptiveFloat(value float64) string {
	absolute := value
	if absolute < 0 {
		absolute = -absolute
	}
	switch {
	case absolute >= 1000:
		return trimFloat(value, 0)
	case absolute >= 100:
		return trimFloat(value, 1)
	case absolute >= 1:
		return trimFloat(value, 2)
	default:
		return trimFloat(value, 3)
	}
}

func trimFloat(value float64, digits int) string {
	formatted := strconv.FormatFloat(value, 'f', digits, 64)
	if digits == 0 {
		return formatted
	}
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	if formatted == "-0" || formatted == "" {
		return "0"
	}
	return formatted
}

type comparisonLayout int

const (
	comparisonPair comparisonLayout = iota
	comparisonMatrix
	comparisonMany
)

func comparisonLayoutFor(reportCount int) comparisonLayout {
	switch {
	case reportCount <= 2:
		return comparisonPair
	case reportCount <= 5:
		return comparisonMatrix
	default:
		return comparisonMany
	}
}
