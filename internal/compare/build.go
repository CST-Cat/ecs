package compare

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ecs/internal/i18n"
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
		return Report{}, i18n.Errorf("compare.help.inputs")
	}
	reference := options.Reference
	if reference < 0 || reference >= len(reports) {
		return Report{}, i18n.Errorf("compare.help.referenceRange", len(reports))
	}
	out := Report{
		SchemaVersion: SchemaVersion, Tool: options.Tool, GeneratedAt: time.Now().UTC(), Reference: reference,
		Notices: []string{
			i18n.T("compare.notice.scope"),
			i18n.T("compare.notice.relative"),
			i18n.T("compare.notice.observation"),
		},
	}
	labels := uniqueLabels(options.Labels, len(reports))
	for index, report := range reports {
		out.Inputs = append(out.Inputs, Input{
			Index: index, Label: labels[index], ReportID: report.Run.ID, ToolVersion: report.Tool.Version,
			Profile: report.Run.Profile, StartedAt: report.Run.StartedAt, IPVersion: report.Run.IPVersion,
			Redacted: report.Run.Redacted,
		})
	}

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
	return out, nil
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
			issues = append(issues, MetricIssue{Key: key, Label: refs[0].measurement.Label, Reason: reason, Reports: inputsForRefs(refs)})
		}
	}
	return metrics, issues
}

func metricSignature(ref metricRef) string {
	direction := "lower"
	if ref.measurement.HigherIsBetter != nil && *ref.measurement.HigherIsBetter {
		direction = "higher"
	}
	parts := []string{
		ref.measurement.Unit, ref.measurement.Method, direction,
		ref.result.Methodology.Kind,
		canonicalParameters(ref.result.Methodology.Parameters),
	}
	return strings.Join(parts, "\x1f")
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

func canonicalParameters(parameters map[string]string) string {
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+parameters[key])
	}
	return strings.Join(parts, "\x1e")
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
	tables := make(map[string]map[int]model.Table)
	order := make([]string, 0)
	seen := make(map[string]bool)
	for input, result := range results {
		if result == nil {
			continue
		}
		counts := make(map[string]int)
		for _, table := range result.Tables {
			counts[tableSchemaKey(table)]++
		}
		for _, table := range result.Tables {
			key := tableSchemaKey(table)
			if len(table.Columns) < 2 || counts[key] != 1 {
				continue
			}
			if tables[key] == nil {
				tables[key] = make(map[int]model.Table)
			}
			tables[key][input] = table
			if !seen[key] {
				seen[key] = true
				order = append(order, key)
			}
		}
	}
	for _, schema := range order {
		group := tables[schema]
		if len(group) < 2 {
			continue
		}
		identity := uniqueIdentityColumn(group)
		if identity < 0 {
			continue
		}
		for input := range results {
			table, exists := group[input]
			if !exists {
				continue
			}
			for _, row := range table.Rows {
				if identity >= len(row) {
					continue
				}
				rowKey := row[identity]
				for column, cell := range row {
					if column == identity || column >= len(table.Columns) {
						continue
					}
					key := "table:" + schema + ":" + rowKey + ":" + strconv.Itoa(column)
					label := table.Title + " · " + rowKey + " · " + table.Columns[column]
					add(input, key, label, "table", cell)
				}
			}
		}
	}
}

func tableSchemaKey(table model.Table) string {
	return table.Title + "\x1f" + strings.Join(table.Columns, "\x1e")
}

func uniqueIdentityColumn(tables map[int]model.Table) int {
	columnCount := -1
	for _, table := range tables {
		if columnCount < 0 || len(table.Columns) < columnCount {
			columnCount = len(table.Columns)
		}
	}
	for column := 0; column < columnCount; column++ {
		valid := true
		for _, table := range tables {
			seen := make(map[string]bool)
			for _, row := range table.Rows {
				if column >= len(row) || strings.TrimSpace(row[column]) == "" || seen[row[column]] {
					valid = false
					break
				}
				seen[row[column]] = true
			}
			if !valid {
				break
			}
		}
		if valid {
			return column
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
