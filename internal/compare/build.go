package compare

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

type metricRef struct {
	input       int
	measurement model.Measurement
	result      model.Result
}

// Build creates a comparison from two or more reports. The first input is the
// default reference; callers may select another valid index through Options.
func Build(reports []model.Report, options Options) (Report, error) {
	if len(reports) < 2 {
		return Report{}, newValidationError("compare.help.inputs")
	}
	reference := options.Reference
	if reference < 0 || reference >= len(reports) {
		return Report{}, newValidationError("compare.help.referenceRange", len(reports))
	}
	out := Report{
		SchemaVersion: SchemaVersion, Tool: options.Tool, GeneratedAt: time.Now().UTC(), Reference: reference,
		Notices: []string{
			canonicalNotice("compare.notice.scope"),
			canonicalNotice("compare.notice.relative"),
			canonicalNotice("compare.notice.observation"),
		},
	}
	labels := uniqueLabels(options.Labels, len(reports))
	for index, report := range reports {
		out.Inputs = append(out.Inputs, Input{
			Index: index, Label: labels[index], SchemaVersion: report.SchemaVersion,
			ReportID: report.Run.ID, ToolVersion: report.Tool.Version,
			Profile: report.Run.Profile, StartedAt: report.Run.StartedAt, IPVersion: report.Run.IPVersion,
			Redacted: report.Run.Redacted,
		})
	}
	schemaMixed := prependVersionNotices(&out)

	moduleOrder := unionModuleOrder(reports)
	for _, moduleID := range moduleOrder {
		module := buildModule(reports, moduleID, reference)
		out.Modules = append(out.Modules, module)
		out.Summary.ComparableMetrics += len(module.Metrics)
		out.Summary.MetricIssues += len(module.MetricIssues)
		out.Summary.ObservedChanges += len(module.Changes)
		out.Summary.MissingModuleValues += len(module.MissingReports)
		for _, status := range module.Statuses {
			if status.Report == reference || !status.Available {
				continue
			}
			base := module.Statuses[reference]
			if base.Available && base.Status != status.Status {
				out.Summary.StatusChanges++
			}
		}
		for _, evidence := range module.Evidence {
			if evidence.Report == reference || !evidence.Available {
				continue
			}
			base := module.Evidence[reference]
			if base.Available && (!nearlyEqual(base.Ratio, evidence.Ratio) || base.Grade != evidence.Grade || base.Expected != evidence.Expected) {
				out.Summary.EvidenceChanges++
			}
		}
		for _, metric := range module.Metrics {
			for _, value := range metric.Values {
				if value.Report == reference || !value.Available {
					continue
				}
				switch value.Outcome {
				case OutcomeImproved:
					out.Summary.Improved++
				case OutcomeRegressed:
					out.Summary.Regressed++
				case OutcomeUnchanged:
					out.Summary.Unchanged++
				}
			}
		}
	}
	out.Summary.Reports = len(reports)
	out.Summary.Modules = len(out.Modules)
	out.Summary.Comparability = overallComparability(out)
	if schemaMixed && out.Summary.Comparability == Comparable {
		// 跨 schema 版本时不给"完全可比"。指标本身由签名保护，但 status 枚举
		// 与 evidence 口径不在签名里，它们的语义理论上可以在升版时改变而没有
		// 任何信号。降一级是对这段未覆盖面的如实表达。
		out.Summary.Comparability = PartiallyComparable
	}
	return out, nil
}

// prependVersionNotices 把版本差异说明放到 Notices 最前面，并回答"schema 版本
// 是否不一致"。
//
// 两种差异的性质不同，因此提示也不同：
//
//	schema 版本不同   下方结论的可信范围缩小了，必须降级并说清楚。
//	ecs 版本不同      结论照常可信，但"某模块缺失""method 不一致"多半是版本
//	                  差异的正常结果而不是故障。缺了这句话，用户会把它当 bug。
func prependVersionNotices(out *Report) bool {
	schemas := distinctInputValues(out.Inputs, func(input Input) string { return input.SchemaVersion })
	tools := distinctInputValues(out.Inputs, func(input Input) string { return input.ToolVersion })

	var leading []string
	if len(schemas) > 1 {
		leading = append(leading,
			canonicalNotice("compare.notice.schemaMixed", strings.Join(schemas, ", ")))
	}
	if len(tools) > 1 {
		leading = append(leading,
			canonicalNotice("compare.notice.toolMixed", strings.Join(tools, ", ")))
	}
	out.Notices = append(leading, out.Notices...)
	return len(schemas) > 1
}

// distinctInputValues 按首次出现顺序收集互不相同的非空取值。
func distinctInputValues(inputs []Input, pick func(Input) string) []string {
	seen := make(map[string]bool, len(inputs))
	var values []string
	for _, input := range inputs {
		value := strings.TrimSpace(pick(input))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}

// SchemaVersions 返回各输入声明的互不相同的 schema 版本，按首次出现顺序。
// 长度大于 1 表示这是一次跨版本比较。
//
// 渲染器共用这一个判定，而不是各自比对 Inputs——三份各写一遍迟早会出现
// "文本报告说跨版本、HTML 说不跨"这种自相矛盾的输出。
func (r Report) SchemaVersions() []string {
	return distinctInputValues(r.Inputs, func(input Input) string { return input.SchemaVersion })
}

func uniqueLabels(labels []string, count int) []string {
	result := make([]string, count)
	used := make(map[string]bool)
	for index := 0; index < count; index++ {
		base := "report-" + strconv.Itoa(index+1)
		if index < len(labels) && strings.TrimSpace(labels[index]) != "" {
			base = strings.TrimSpace(labels[index])
		}
		label := base
		for suffix := 2; used[label]; suffix++ {
			label = base + " #" + strconv.Itoa(suffix)
		}
		used[label] = true
		result[index] = label
	}
	return result
}

func unionModuleOrder(reports []model.Report) []string {
	seen := make(map[string]bool)
	var order []string
	for _, report := range reports {
		for _, result := range report.Results {
			if result.ID == "" || seen[result.ID] {
				continue
			}
			seen[result.ID] = true
			order = append(order, result.ID)
		}
	}
	return order
}

func buildModule(reports []model.Report, id string, reference int) Module {
	module := Module{ID: id, Comparability: Comparable}
	refs := make([]*model.Result, len(reports))
	for inputIndex, report := range reports {
		matches := 0
		for resultIndex := range report.Results {
			if report.Results[resultIndex].ID == id {
				matches++
				copy := report.Results[resultIndex]
				refs[inputIndex] = &copy
				if module.Title == "" {
					module.Title = copy.Title
				}
			}
		}
		if matches > 1 {
			refs[inputIndex] = nil
			module.MetricIssues = append(module.MetricIssues, MetricIssue{
				Key: id, Label: module.Title, Reason: "duplicate_module_id", Reports: []int{inputIndex},
			})
		}
		status := StatusValue{Report: inputIndex}
		evidence := EvidenceValue{Report: inputIndex}
		if refs[inputIndex] == nil {
			module.MissingReports = append(module.MissingReports, inputIndex)
			module.Statuses = append(module.Statuses, status)
			module.Evidence = append(module.Evidence, evidence)
			continue
		}
		status.Available, status.Status = true, refs[inputIndex].Status
		if refs[inputIndex].Evidence != nil {
			e := *refs[inputIndex].Evidence
			e.Normalize()
			evidence = EvidenceValue{
				Report: inputIndex, Available: true, Valid: e.Valid, Expected: e.Expected,
				Unit: e.Unit, Grade: e.EffectiveGrade(), Ratio: e.EvidenceRatio(),
			}
		}
		module.Statuses = append(module.Statuses, status)
		module.Evidence = append(module.Evidence, evidence)
	}
	metrics, metricIssues := buildMetrics(refs, reference)
	module.Metrics = metrics
	module.MetricIssues = append(module.MetricIssues, metricIssues...)
	module.Changes = buildObservations(refs)
	switch {
	case len(module.Metrics) == 0:
		module.Comparability = NotComparable
	case len(module.MissingReports) > 0 || len(module.MetricIssues) > 0 || metricsMissingValues(module.Metrics):
		module.Comparability = PartiallyComparable
	default:
		module.Comparability = Comparable
	}
	return module
}

func buildMetrics(results []*model.Result, reference int) ([]Metric, []MetricIssue) {
	byKey := make(map[string][]metricRef)
	duplicateKeys := make(map[string]map[int]bool)
	keyOrder := make([]string, 0)
	seenKey := make(map[string]bool)
	for input, result := range results {
		if result == nil {
			continue
		}
		perInput := make(map[string]int)
		for _, measurement := range result.Measurements {
			perInput[measurement.Key]++
			if !seenKey[measurement.Key] {
				seenKey[measurement.Key] = true
				keyOrder = append(keyOrder, measurement.Key)
			}
			byKey[measurement.Key] = append(byKey[measurement.Key], metricRef{input: input, measurement: measurement, result: *result})
		}
		for key, count := range perInput {
			if count > 1 {
				if duplicateKeys[key] == nil {
					duplicateKeys[key] = make(map[int]bool)
				}
				duplicateKeys[key][input] = true
			}
		}
	}
	var metrics []Metric
	var issues []MetricIssue
	for _, key := range keyOrder {
		refs := byKey[key]
		if strings.TrimSpace(key) == "" {
			issues = append(issues, MetricIssue{Label: refs[0].measurement.Label, Reason: "missing_metric_key", Reports: inputsForRefs(refs)})
			continue
		}
		if len(refs) < 2 {
			if len(refs) == 1 {
				issues = append(issues, MetricIssue{
					Key: key, Label: refs[0].measurement.Label, Reason: "no_matching_metric", Reports: inputsForRefs(refs),
				})
			}
			continue
		}
		if duplicates := duplicateKeys[key]; len(duplicates) > 0 {
			issues = append(issues, MetricIssue{Key: key, Label: refs[0].measurement.Label, Reason: "duplicate_metric_key", Reports: sortedInputSet(duplicates)})
			continue
		}
		groups := make(map[string][]metricRef)
		groupOrder := make([]string, 0)
		// signed 只收录真正算出了签名的报告。取不到签名的（数值非有限、缺 method、
		// 参数口径损坏）由各自的 issue 负责说明，混进差异归因只会误导。
		signed := make([]metricRef, 0, len(refs))
		invalid := make(map[int]bool)
		invalidValues := make(map[int]bool)
		invalidParameters := make(map[int]bool)
		for _, ref := range refs {
			measurement := ref.measurement
			if math.IsNaN(measurement.Value) || math.IsInf(measurement.Value, 0) {
				invalidValues[ref.input] = true
				continue
			}
			if strings.TrimSpace(measurement.Method) == "" {
				invalid[ref.input] = true
				continue
			}
			if measurement.HigherIsBetter == nil {
				invalid[ref.input] = true
				continue
			}
			if !validParameters(ref.result.Methodology.Parameters) {
				invalidParameters[ref.input] = true
				continue
			}
			signature := metricSignature(ref)
			if _, exists := groups[signature]; !exists {
				groupOrder = append(groupOrder, signature)
			}
			groups[signature] = append(groups[signature], ref)
			signed = append(signed, ref)
		}
		comparableGroups := 0
		for _, signature := range groupOrder {
			group := groups[signature]
			if len(group) < 2 {
				continue
			}
			comparableGroups++
			metrics = append(metrics, buildMetric(group, len(results), reference))
		}
		if len(invalid) > 0 {
			reason := "missing_method_or_direction"
			issues = append(issues, MetricIssue{Key: key, Label: refs[0].measurement.Label, Reason: reason, Reports: sortedInputSet(invalid)})
		}
		if len(invalidValues) > 0 {
			issues = append(issues, MetricIssue{Key: key, Label: refs[0].measurement.Label, Reason: "non_finite_value", Reports: sortedInputSet(invalidValues)})
		}
		if len(invalidParameters) > 0 {
			issues = append(issues, MetricIssue{Key: key, Label: refs[0].measurement.Label, Reason: "missing_or_invalid_parameter_scope", Reports: sortedInputSet(invalidParameters)})
		}
		if len(groups) > 1 {
			reason := "method_or_parameters_mismatch"
			if comparableGroups > 0 {
				reason = "some_reports_use_different_method_or_parameters"
			}
			issues = append(issues, MetricIssue{
				Key: key, Label: refs[0].measurement.Label, Reason: reason,
				Reports: inputsForRefs(refs), Differences: signatureDifferences(signed),
			})
		}
	}
	return metrics, issues
}

func metricSignature(ref metricRef) string {
	components := signatureComponents(ref)
	parts := make([]string, 0, len(components))
	for _, component := range components {
		parts = append(parts, component.field+"\x1e"+component.value)
	}
	return strings.Join(parts, "\x1f")
}

// signatureComponent 是签名的一个具名分量。
type signatureComponent struct {
	field string
	value string
}

// signatureComponents 是签名的唯一来源：metricSignature 由它拼成，��异归因也读它。
//
// 两者共用同一组分量是刻意的。如果各写一份，迟早会出现"明细说没有差异、判定却
// 说不可比"这种自相矛盾的输出——那比现在只给一个原因码更糟。
//
// 参数逐键展开而不是并成一串：并成一串只能告诉用户"参数不一样"，逐键才能指出
// 是 threads 变了还是 scope_revision 变了。
func signatureComponents(ref metricRef) []signatureComponent {
	direction := "lower"
	if ref.measurement.HigherIsBetter != nil && *ref.measurement.HigherIsBetter {
		direction = "higher"
	}
	parameters := ref.result.Methodology.Parameters
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	components := make([]signatureComponent, 0, 4+len(keys))
	components = append(components,
		signatureComponent{field: "unit", value: ref.measurement.Unit},
		signatureComponent{field: "method", value: ref.measurement.Method},
		signatureComponent{field: "direction", value: direction},
		signatureComponent{field: "kind", value: ref.result.Methodology.Kind},
	)
	for _, key := range keys {
		components = append(components, signatureComponent{field: "parameter:" + key, value: parameters[key]})
	}
	return components
}

// signatureDifferences 找出这批报告在签名分量上真正不一致的项。
//
// 只收录确实不同的分量。相同的分量不是用户在找的东西，列出来只会把真正变了的
// 那一项淹掉。某报告缺某个参数键时取值留空——"缺这个键"本身就是一种差异。
func signatureDifferences(refs []metricRef) []Difference {
	if len(refs) < 2 {
		return nil
	}
	reports := make([]int, 0, len(refs))
	byField := make(map[string]map[int]string)
	fieldOrder := make([]string, 0)
	for _, ref := range refs {
		reports = append(reports, ref.input)
		for _, component := range signatureComponents(ref) {
			if byField[component.field] == nil {
				byField[component.field] = make(map[int]string)
				fieldOrder = append(fieldOrder, component.field)
			}
			byField[component.field][ref.input] = component.value
		}
	}
	sort.Ints(reports)

	differences := make([]Difference, 0)
	for _, field := range fieldOrder {
		values := byField[field]
		differs := false
		for index := 1; index < len(reports); index++ {
			if values[reports[index]] != values[reports[0]] {
				differs = true
				break
			}
		}
		if !differs {
			continue
		}
		difference := Difference{Field: field, Values: make([]DifferenceValue, 0, len(reports))}
		for _, report := range reports {
			difference.Values = append(difference.Values, DifferenceValue{Report: report, Value: values[report]})
		}
		differences = append(differences, difference)
	}
	if len(differences) == 0 {
		return nil
	}
	return differences
}

func buildMetric(refs []metricRef, reportCount, reference int) Metric {
	first := refs[0]
	metric := Metric{
		Key: first.measurement.Key, Label: first.measurement.Label, Unit: first.measurement.Unit,
		Method: first.measurement.Method, Parameters: cloneParameters(first.result.Methodology.Parameters),
		ParameterScope: displayParameterScope(first.result.Methodology.Profile, first.result.Methodology.Parameters),
		HigherIsBetter: *first.measurement.HigherIsBetter,
		Values:         make([]MetricValue, reportCount),
	}
	for index := range metric.Values {
		metric.Values[index].Report = index
		metric.Values[index].Outcome = OutcomeNoReference
	}
	for _, ref := range refs {
		metric.Values[ref.input] = MetricValue{
			Report: ref.input, Available: true, Value: ref.measurement.Value, Display: ref.measurement.Display,
			Outcome: OutcomeNoReference,
		}
	}
	decorateMetricValues(&metric, reference)
	return metric
}

func validParameters(parameters map[string]string) bool {
	if len(parameters) == 0 {
		return false
	}
	if strings.TrimSpace(parameters["scope_revision"]) == "" {
		return false
	}
	for key, value := range parameters {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func cloneParameters(parameters map[string]string) map[string]string {
	out := make(map[string]string, len(parameters))
	for key, value := range parameters {
		out[key] = value
	}
	return out
}

func displayParameterScope(profile string, parameters map[string]string) string {
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	if strings.TrimSpace(profile) != "" {
		parts = append(parts, strings.TrimSpace(profile))
	}
	for _, key := range keys {
		value := parameters[key]
		displayKey := key
		if strings.HasSuffix(key, "_sha256") && len(value) > 12 {
			value = value[:12]
			displayKey = strings.TrimSuffix(key, "_sha256") + "#"
		}
		if key == "scope_revision" {
			displayKey = "scope"
			value = "v" + value
		}
		parts = append(parts, displayKey+"="+value)
	}
	return strings.Join(parts, " · ")
}

func decorateMetricValues(metric *Metric, reference int) {
	var available []int
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for index, value := range metric.Values {
		if !value.Available {
			continue
		}
		available = append(available, index)
		minimum = math.Min(minimum, value.Value)
		maximum = math.Max(maximum, value.Value)
	}
	sort.SliceStable(available, func(i, j int) bool {
		left, right := metric.Values[available[i]].Value, metric.Values[available[j]].Value
		if metric.HigherIsBetter {
			return left > right
		}
		return left < right
	})
	rank := 0
	var previous float64
	for position, index := range available {
		value := &metric.Values[index]
		if position == 0 || !nearlyEqual(value.Value, previous) {
			rank = position + 1
		}
		value.Rank = rank
		value.Best = rank == 1
		previous = value.Value
	}
	worstValue := minimum
	if metric.HigherIsBetter {
		worstValue = minimum
	} else {
		worstValue = maximum
	}
	for _, index := range available {
		value := &metric.Values[index]
		value.Worst = nearlyEqual(value.Value, worstValue) && !value.Best
		if nearlyEqual(maximum, minimum) {
			value.QualityRatio = 1
		} else if metric.HigherIsBetter {
			value.QualityRatio = 0.15 + 0.85*(value.Value-minimum)/(maximum-minimum)
		} else {
			value.QualityRatio = 0.15 + 0.85*(maximum-value.Value)/(maximum-minimum)
		}
	}
	if reference < 0 || reference >= len(metric.Values) || !metric.Values[reference].Available {
		return
	}
	base := metric.Values[reference].Value
	for index := range metric.Values {
		value := &metric.Values[index]
		if !value.Available {
			continue
		}
		if nearlyEqual(value.Value, base) {
			value.Outcome = OutcomeUnchanged
		} else if (metric.HigherIsBetter && value.Value > base) || (!metric.HigherIsBetter && value.Value < base) {
			value.Outcome = OutcomeImproved
		} else {
			value.Outcome = OutcomeRegressed
		}
		if !nearlyEqual(base, 0) {
			change := (value.Value - base) / math.Abs(base) * 100
			if !metric.HigherIsBetter {
				change = -change
			}
			value.PerformanceChangePercent = &change
		}
	}
}

func buildObservations(results []*model.Result) []Observation {
	values := make(map[string][]ObservationValue)
	labels := make(map[string]string)
	sources := make(map[string]string)
	order := make([]string, 0)
	seen := make(map[string]bool)
	add := func(input int, key, label, source, value string) {
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
			values[key] = make([]ObservationValue, len(results))
			for index := range values[key] {
				values[key][index].Report = index
			}
			labels[key], sources[key] = label, source
		}
		values[key][input] = ObservationValue{Report: input, Available: true, Value: value}
	}
	for input, result := range results {
		if result == nil {
			continue
		}
		fieldCounts := make(map[string]int)
		for _, field := range result.Fields {
			fieldCounts[field.Key]++
		}
		for _, field := range result.Fields {
			if field.Key == "" || fieldCounts[field.Key] != 1 {
				continue
			}
			add(input, "field:"+field.Key, field.Label, "field", field.Value)
		}
	}
	appendTableObservations(results, add)
	var observations []Observation
	for _, key := range order {
		if observationChanged(values[key]) {
			observations = append(observations, Observation{Key: key, Label: labels[key], Source: sources[key], Values: values[key]})
		}
	}
	return observations
}

func appendTableObservations(results []*model.Result, add func(int, string, string, string, string)) {
	// A machine schema is the only safe way to match a table across reports.
	// Legacy tables are kept in a separate positional fallback path so display
	// labels can never become an accidental identity.
	tables := make(map[string]map[int][]tableRef)
	order := make([]string, 0)
	seen := make(map[string]bool)
	legacy := make(map[int]map[int]tableRef)
	legacyOrder := make([]int, 0)
	for input, result := range results {
		if result == nil {
			continue
		}
		for position, table := range result.Tables {
			ref := tableRef{input: input, position: position, table: table}
			schema := tableSchemaKey(table)
			if schema == "" {
				appendLegacyTableRef(legacy, &legacyOrder, ref)
				continue
			}
			if tables[schema] == nil {
				tables[schema] = make(map[int][]tableRef)
			}
			tables[schema][input] = append(tables[schema][input], ref)
			if !seen[schema] {
				seen[schema] = true
				order = append(order, schema)
			}
		}
	}
	for _, schema := range order {
		group := tables[schema]
		if hasDuplicateTableInput(group) {
			// Duplicate declarations are malformed. Reclassify their tables as
			// legacy and compare by position, never by an arbitrary duplicate.
			for _, refs := range group {
				for _, ref := range refs {
					appendLegacyTableRef(legacy, &legacyOrder, ref)
				}
			}
			continue
		}
		refs := make(map[int]tableRef, len(group))
		for input, values := range group {
			refs[input] = values[0]
		}
		if len(refs) < 2 {
			continue
		}
		appendMachineTableObservations(results, schema, refs, add)
	}
	for _, position := range legacyOrder {
		group := legacy[position]
		if len(group) < 2 {
			continue
		}
		appendWholeTableObservation(results, "legacy:"+strconv.Itoa(position), group, add)
	}
}

type tableRef struct {
	input    int
	position int
	table    model.Table
}

func appendLegacyTableRef(groups map[int]map[int]tableRef, order *[]int, ref tableRef) {
	if groups[ref.position] == nil {
		groups[ref.position] = make(map[int]tableRef)
		*order = append(*order, ref.position)
	}
	groups[ref.position][ref.input] = ref
}

func hasDuplicateTableInput(group map[int][]tableRef) bool {
	for _, refs := range group {
		if len(refs) != 1 {
			return true
		}
	}
	return false
}

func appendMachineTableObservations(results []*model.Result, schema string, group map[int]tableRef, add func(int, string, string, string, string)) {
	if keyedTableGroup(group) {
		appendKeyedTableObservations(results, schema, group, add)
		return
	}
	if tablesHaveSameShape(group) {
		appendPositionalTableObservations(results, schema, group, add)
		return
	}
	// A malformed row shape cannot be safely aligned even positionally. The
	// whole-table snapshot still exposes a change without inventing row keys.
	appendWholeTableObservation(results, schema+":whole", group, add)
}

func keyedTableGroup(group map[int]tableRef) bool {
	identity := ""
	for _, ref := range group {
		table := ref.table
		index := tableRowIdentityIndex(table)
		if index < 0 {
			return false
		}
		if identity == "" {
			identity = table.RowIdentity
		} else if identity != table.RowIdentity {
			return false
		}
		seen := make(map[string]bool, len(table.Rows))
		for _, row := range table.Rows {
			if index >= len(row) {
				return false
			}
			rowKey := strings.TrimSpace(row[index])
			if rowKey == "" || seen[rowKey] {
				return false
			}
			seen[rowKey] = true
		}
	}
	return true
}

func appendKeyedTableObservations(results []*model.Result, schema string, group map[int]tableRef, add func(int, string, string, string, string)) {
	for input := range results {
		ref, exists := group[input]
		if !exists {
			continue
		}
		table := ref.table
		identity := tableRowIdentityIndex(table)
		for _, row := range table.Rows {
			rowKey := row[identity]
			for column, cell := range row {
				if column == identity || column >= len(table.ColumnKeys) {
					continue
				}
				columnKey := table.ColumnKeys[column]
				key := tableObservationKey(schema, rowKey, columnKey)
				label := tableCellLabel(table, rowKey, column)
				add(input, key, label, "table", cell)
			}
		}
	}
}

func appendPositionalTableObservations(results []*model.Result, schema string, group map[int]tableRef, add func(int, string, string, string, string)) {
	for input := range results {
		ref, exists := group[input]
		if !exists {
			continue
		}
		table := ref.table
		for rowIndex, row := range table.Rows {
			for column, cell := range row {
				if column >= len(table.ColumnKeys) {
					continue
				}
				columnKey := table.ColumnKeys[column]
				key := "table:" + schema + ":row-index:" + strconv.Itoa(rowIndex) + ":column:" + columnKey
				label := tableCellLabel(table, "row "+strconv.Itoa(rowIndex+1), column)
				add(input, key, label, "table", cell)
			}
		}
	}
}

func appendWholeTableObservation(results []*model.Result, key string, group map[int]tableRef, add func(int, string, string, string, string)) {
	for input := range results {
		ref, exists := group[input]
		if !exists {
			continue
		}
		label := ref.table.Title
		if strings.TrimSpace(label) == "" {
			label = "table #" + strconv.Itoa(ref.position+1)
		}
		add(input, "table:"+key, label, "table", tableSnapshot(ref.table))
	}
}

func tableCellLabel(table model.Table, rowKey string, column int) string {
	columnLabel := "column " + strconv.Itoa(column+1)
	if column >= 0 && column < len(table.Columns) && strings.TrimSpace(table.Columns[column]) != "" {
		columnLabel = table.Columns[column]
	}
	return table.Title + " · " + rowKey + " · " + columnLabel
}

func tableObservationKey(schema, rowKey, columnKey string) string {
	return "table:" + schema + ":" + rowKey + ":" + columnKey
}

func tableSnapshot(table model.Table) string {
	rows := make([]string, len(table.Rows))
	for rowIndex, row := range table.Rows {
		cells := make([]string, len(row))
		for column, cell := range row {
			cells[column] = strconv.Quote(cell)
		}
		rows[rowIndex] = "[" + strings.Join(cells, ", ") + "]"
	}
	return "columns=" + strconv.Itoa(len(table.Columns)) + "; rows=" + strings.Join(rows, ", ")
}

func tablesHaveSameShape(group map[int]tableRef) bool {
	var first [][]string
	set := false
	for _, ref := range group {
		if !set {
			first, set = ref.table.Rows, true
			continue
		}
		if len(ref.table.Rows) != len(first) {
			return false
		}
		for rowIndex, row := range ref.table.Rows {
			if len(row) != len(first[rowIndex]) {
				return false
			}
		}
	}
	return true
}

func tableSchemaKey(table model.Table) string {
	if !validTableMachineSchema(table) {
		return ""
	}
	return table.Key + "\x1f" + strings.Join(table.ColumnKeys, "\x1e")
}

func validTableMachineSchema(table model.Table) bool {
	if strings.TrimSpace(table.Key) == "" || len(table.ColumnKeys) == 0 || len(table.ColumnKeys) != len(table.Columns) {
		return false
	}
	seen := make(map[string]bool, len(table.ColumnKeys))
	for _, columnKey := range table.ColumnKeys {
		columnKey = strings.TrimSpace(columnKey)
		if columnKey == "" || seen[columnKey] {
			return false
		}
		seen[columnKey] = true
	}
	return true
}

func tableRowIdentityIndex(table model.Table) int {
	if !validTableMachineSchema(table) || strings.TrimSpace(table.RowIdentity) == "" {
		return -1
	}
	for index, columnKey := range table.ColumnKeys {
		if columnKey == table.RowIdentity {
			return index
		}
	}
	return -1
}

func observationChanged(values []ObservationValue) bool {
	first := ""
	found := false
	available := 0
	for _, value := range values {
		if !value.Available {
			continue
		}
		available++
		if !found {
			first, found = value.Value, true
			continue
		}
		if value.Value != first {
			return true
		}
	}
	return available >= 2 && available < len(values)
}

func metricsMissingValues(metrics []Metric) bool {
	for _, metric := range metrics {
		for _, value := range metric.Values {
			if !value.Available {
				return true
			}
		}
	}
	return false
}

func overallComparability(report Report) Comparability {
	if report.Summary.ComparableMetrics == 0 {
		return NotComparable
	}
	if report.Summary.MetricIssues > 0 || report.Summary.MissingModuleValues > 0 {
		return PartiallyComparable
	}
	for _, module := range report.Modules {
		if module.Comparability != Comparable {
			return PartiallyComparable
		}
	}
	return Comparable
}

func nearlyEqual(left, right float64) bool {
	difference := math.Abs(left - right)
	scale := math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
	return difference <= scale*1e-9
}

func sortedInputSet(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func inputsForRefs(refs []metricRef) []int {
	seen := make(map[int]bool)
	for _, ref := range refs {
		seen[ref.input] = true
	}
	return sortedInputSet(seen)
}
