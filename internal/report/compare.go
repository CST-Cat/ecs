package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	comparison "ecs/internal/compare"
	"ecs/internal/i18n"
)

// WriteComparisonFiles writes one comparison in JSON, Markdown and HTML as a
// normal ecs report.  Paths are always resolved below an explicit output
// directory and each artifact is installed atomically.
func WriteComparisonFiles(data comparison.Report, directory, baseName string, formats []string) (map[string]string, error) {
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
	contents := make(map[string][]byte, len(formats))
	orderedFormats := make([]string, 0, len(formats))
	for _, format := range formats {
		if _, seen := contents[format]; seen {
			continue
		}
		var content []byte
		switch format {
		case "json":
			content, err = ComparisonJSON(data)
		case "md":
			content = []byte(ComparisonMarkdown(data))
		case "html":
			content, err = ComparisonHTML(data)
		default:
			err = i18n.Errorf("err.reportUnknownFormat", format)
		}
		if err != nil {
			return nil, i18n.Errorf("err.reportGenerate", format, err)
		}
		contents[format] = content
		orderedFormats = append(orderedFormats, format)
	}
	written := make(map[string]string, len(orderedFormats))
	for _, format := range orderedFormats {
		content := contents[format]
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

// localizeComparisonNotice is deliberately kept in the report package: the
// comparison package only carries stable machine message semantics, while
// report formats are the user-facing localization boundary.
func localizeComparisonNotice(notice comparison.Notice) string {
	if notice.Key == "" || !i18n.Has(i18n.LangZH, notice.Key) || !i18n.Has(i18n.LangEN, notice.Key) {
		return notice.Key
	}
	values := make([]any, len(notice.Args))
	for index, arg := range notice.Args {
		values[index] = arg
	}
	return fmt.Sprintf(i18n.T(notice.Key), values...)
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

// comparisonDifferenceLabel 把签名分量名翻译成人话。
//
// 参数分量形如 parameter:threads——参数名来自各基准工具的自有口径，不可能有
// 完整译名，因此只翻译"参数"二字，键名原样保留。
func comparisonDifferenceLabel(field string) string {
	if name, ok := strings.CutPrefix(field, "parameter:"); ok {
		return i18n.T("compare.field.parameter") + " " + name
	}
	key := "compare.field." + field
	translated := i18n.T(key)
	if translated == key {
		return field
	}
	return translated
}

// comparisonVersionedLabel 是报告标签加上产出它的 ecs 版本。
//
// 差异明细正是要回答"哪个版本用的哪个 method"，把版本贴在标签上是最短的答法；
// 没记录版本时退回纯标签，不显示一个空括号。
func comparisonVersionedLabel(data comparison.Report, index int) string {
	input := comparisonInput(data, index)
	version := strings.TrimSpace(input.ToolVersion)
	if version == "" {
		return input.Label
	}
	return input.Label + " (" + version + ")"
}

// comparisonDifferenceGroup 是一组差异完全相同的指标。
//
// 分组是必要的：scope_revision、threads 这类参数属于整个模块，口径一变，模块里
// 每个指标都会报同一组差异。逐指标重复渲染会让一个 7 指标的模块把同样两行刷七遍，
// 真正只影响单个指标的那一项（比如某个 method 单独升了版）反而被淹掉。
type comparisonDifferenceGroup struct {
	Metrics     []string
	Differences []comparison.Difference
}

// comparisonDifferenceGroups 把带差异的 issue 按"差异集合是否完全相同"归并，
// 保持原有出现顺序。三种格式共用这一份，避免各自实现出不同的归并结果。
func comparisonDifferenceGroups(issues []comparison.MetricIssue) []comparisonDifferenceGroup {
	groups := make([]comparisonDifferenceGroup, 0)
	index := make(map[string]int)
	for _, issue := range issues {
		if len(issue.Differences) == 0 {
			continue
		}
		key := comparisonDifferenceKey(issue.Differences)
		metric := fallbackReport(issue.Label, issue.Key)
		if at, ok := index[key]; ok {
			groups[at].Metrics = append(groups[at].Metrics, metric)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, comparisonDifferenceGroup{
			Metrics: []string{metric}, Differences: issue.Differences,
		})
	}
	return groups
}

func comparisonDifferenceKey(differences []comparison.Difference) string {
	var key strings.Builder
	for _, difference := range differences {
		key.WriteString(difference.Field)
		key.WriteByte(0x1f)
		for _, value := range difference.Values {
			fmt.Fprintf(&key, "%d=%s\x1e", value.Report, value.Value)
		}
		key.WriteByte(0x1d)
	}
	return key.String()
}

// comparisonDifferenceLine 是某个分量在各报告上的取值，压成一行。
//
// 两份报告是绝大多数情况，一行比一张两行小表读起来快得多，也不必为每一项重复
// 一遍表头。报告与取值之间用 " = " 而不是冒号：字段名后面已经有一个冒号了，
// 两层用同一个符号会让人分辨不出层级。
func comparisonDifferenceLine(data comparison.Report, difference comparison.Difference) string {
	parts := make([]string, 0, len(difference.Values))
	for _, value := range difference.Values {
		parts = append(parts, comparisonVersionedLabel(data, value.Report)+
			" = "+comparisonDifferenceValue(difference.Field, value))
	}
	return strings.Join(parts, " · ")
}

// comparisonDifferenceValue 是某份报告在某个分量上的取值。
//
// 空值意味着这份报告根本没有这一项，那本身就是差异的一部分。direction 的取值是
// higher/lower 这种内部标记，对用户没有意义，按当前语言翻译；其余分量（method、
// 单位、参数值）是机器口径的一部分，必须原样呈现——翻译它们等于篡改证据。
func comparisonDifferenceValue(field string, value comparison.DifferenceValue) string {
	raw := strings.TrimSpace(value.Value)
	if raw == "" {
		return i18n.T("compare.field.absent")
	}
	if field == "direction" {
		key := "compare.field." + raw
		if translated := i18n.T(key); translated != key {
			return translated
		}
	}
	return raw
}

func comparisonOutcomeLabel(outcome comparison.Outcome) string {
	key := "compare.outcome." + string(outcome)
	translated := i18n.T(key)
	if translated == key {
		return string(outcome)
	}
	return translated
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
