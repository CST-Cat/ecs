package report

import (
	"math"
	"strconv"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

// measurements 渲染指标，并对同单位的一组画组内相对柱。
//
// 只在同单位、同方向且不止一项时画柱：把 ms 和 MiB/s 放在同一个刻度上比较
// 毫无意义，单独一项也没有"相对"可言。
func (r *textRenderer) measurements(items []model.Measurement) {
	groups := groupComparable(items)
	labelLimit := minInt(30, maxInt(6, r.width*3/10))
	valueLimit := minInt(26, maxInt(6, r.width/4))
	labelWidth, valueWidth := 0, 0
	for _, rawItem := range items {
		item := displayMeasurement(rawItem)
		display := displayValue(item.Display)
		labelWidth = maxInt(labelWidth, textwidth.Width(item.Label))
		valueWidth = maxInt(valueWidth, textwidth.Width(display))
	}
	labelWidth = minInt(labelWidth, labelLimit)
	valueWidth = minInt(valueWidth, valueLimit)
	for _, rawItem := range items {
		item := displayMeasurement(rawItem)
		display := displayValue(item.Display)
		label := textwidth.Pad(textwidth.Truncate(item.Label, labelLimit), labelWidth) + i18n.T("punct.colon")
		valueLines := wrapText(display, valueWidth)
		if len(valueLines) == 0 {
			valueLines = []string{""}
		}
		base := "  " + r.palette.Label(label) + "  " + r.styledValue(textwidth.PadLeft(valueLines[0], valueWidth), item.Display)
		rating := ""
		if item.Rating != "" {
			rating = "  " + textwidth.Truncate(item.Rating, 20)
		}
		separateRating := false
		bar := ""
		if group, ok := groups[item.Key]; ok {
			available := r.width - textwidth.Width(base) - 2
			barSize := minInt(adaptiveBarWidth(r.width, barWidth), maxInt(0, available-textwidth.Width(rating)))
			if rating != "" && barSize < 4 {
				separateRating = true
				rating = ""
				barSize = minInt(adaptiveBarWidth(r.width, barWidth), maxInt(0, available))
			}
			if barSize >= 2 {
				bar = "  " + r.palette.BarRelativeRange(comparableValue(item, group), group.min, group.max, barSize)
			}
		} else if textwidth.Width(base)+textwidth.Width(rating) > r.width {
			separateRating = rating != ""
			rating = ""
		}
		base += bar + rating
		for index, valueLine := range valueLines {
			if index == 0 {
				r.line(base)
			} else {
				r.linef("      %s", r.styledValue(valueLine, item.Display))
			}
		}
		if separateRating {
			r.indentedStyled(item.Rating, nil)
		}
		// Workload identifiers are part of the evidence contract. The compact
		// terminal view hides them, while file txt retains every method so it can
		// be audited against the structured JSON artifact.
		if !r.compact && item.Method != "" {
			prefix := "      " + i18n.T("report.method") + i18n.T("punct.colon")
			available := maxInt(1, r.width-textwidth.Width(prefix))
			lines := wrapText(item.Method, available)
			for index, methodLine := range lines {
				if index == 0 {
					r.linef("%s%s", r.palette.Dim(prefix), r.palette.Dim(methodLine))
				} else {
					r.linef("%s%s", strings.Repeat(" ", textwidth.Width(prefix)), r.palette.Dim(methodLine))
				}
			}
		}
	}
	r.blank()
}

// comparableGroup 是一组可以互相比较的指标。
type comparableGroup struct {
	max     float64
	min     float64
	inverse bool
}

// riskScoreMeasurement identifies the 0–100 risk scores whose magnitude is
// useful to show directly.  A high risk score is worse, but a longer bar still
// communicates a higher observed risk; only latency/utilization-style metrics
// should invert their values to visualize "higher is better".
func riskScoreMeasurement(item model.Measurement) bool {
	if strings.TrimSpace(item.Unit) != "/100" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(item.Key))
	return strings.HasSuffix(key, "_risk_score")
}

// comparisonSemantic prevents unrelated measurements that happen to share a
// unit from borrowing one another's scale (for example memory usage and packet
// loss are both percentages, but neither is comparable to the other).
func comparisonSemantic(item model.Measurement) string {
	if riskScoreMeasurement(item) {
		return "risk"
	}
	// The unit alone is insufficient: Crystal, ATTO, mixed and baseline fio
	// throughput all use MiB/s, but their workloads are not one comparison set.
	if semantic := matrixMeasurementSemantic(item.Key); semantic != "" {
		return semantic
	}
	key := strings.ToLower(strings.TrimSpace(item.Key))
	// Percentile and interval qualifiers are separate metrics even when their
	// units and quality direction match. Keeping them in distinct buckets makes
	// every bar compare like with like across targets instead of, for example,
	// scaling a DNS P50 against a DNS P95 or an iperf interval minimum against
	// the final whole-run throughput.
	switch {
	case strings.Contains(key, "iperf3_") && strings.Contains(key, "_interval_min_"):
		return "iperf-interval-min"
	case strings.Contains(key, "iperf3_") && strings.Contains(key, "_interval_p50_"):
		return "iperf-interval-p50"
	case strings.Contains(key, "iperf3_") && strings.HasSuffix(key, "_mbps"):
		return "iperf-throughput"
	case strings.HasPrefix(key, "dns_resolver_") && strings.Contains(key, "_p50_"):
		return "dns-p50"
	case key == "best_dns_median_ms":
		return "dns-p50"
	case strings.HasPrefix(key, "dns_resolver_") && strings.Contains(key, "_p95_"):
		return "dns-p95"
	case strings.HasPrefix(key, "tcp_target_") && strings.Contains(key, "_p50_"):
		return "tcp-p50"
	case key == "best_tcp_median_ms":
		return "tcp-p50"
	case strings.HasPrefix(key, "tcp_target_") && strings.Contains(key, "_p95_"):
		return "tcp-p95"
	case strings.HasPrefix(key, "route_target_") && strings.HasSuffix(key, "_hop_slots"):
		return "route-hop-slots"
	case strings.HasPrefix(key, "route_target_") && strings.HasSuffix(key, "_visible_hops"):
		return "route-visible-hops"
	case strings.HasPrefix(key, "route_target_") && strings.HasSuffix(key, "_timeout_hops"):
		return "route-timeout-hops"
	}
	switch strings.TrimSpace(item.Unit) {
	case "%":
		switch {
		case strings.Contains(key, "loss"):
			return "loss"
		case strings.Contains(key, "steal"):
			return "cpu-steal"
		case strings.Contains(key, "usage"), strings.Contains(key, "utilization"):
			return "usage"
		case strings.Contains(key, "percentage_used"), strings.Contains(key, "percent_used"):
			return "device-life"
		default:
			return "percent"
		}
	case "ms":
		if strings.Contains(key, "jitter") {
			return "jitter"
		}
		return "latency"
	case "项":
		switch {
		case strings.Contains(key, "blacklist"), strings.Contains(key, "listed"):
			return "blacklist"
		case strings.Contains(key, "bgp"), strings.Contains(key, "observed"):
			return "coverage"
		case strings.Contains(key, "reachable"):
			return "reachability"
		}
	case "bytes":
		switch {
		case strings.Contains(key, "memory"):
			return "memory-capacity"
		case strings.Contains(key, "disk"):
			return "disk-capacity"
		case strings.Contains(key, "swap"):
			return "swap-capacity"
		}
	}
	return ""
}

func matrixMeasurementSemantic(key string) string {
	switch matrixKindForMeasurement(key) {
	case matrixCrystal:
		return "matrix-crystal"
	case matrixATTO:
		return "matrix-atto"
	case matrixMixed:
		return "matrix-mixed"
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.HasPrefix(lower, "fio_mount_"):
		return "disk-mount"
	case strings.HasPrefix(lower, "fio_"):
		return "disk-baseline"
	case strings.HasPrefix(lower, "sysbench_cpu_"):
		return "cpu"
	default:
		return ""
	}
}

// groupComparable 找出可以画组内相对柱的指标。
func groupComparable(items []model.Measurement) map[string]comparableGroup {
	type bucket struct {
		keys    []string
		values  []float64
		inverse bool
	}
	buckets := make(map[string]*bucket)
	for _, item := range items {
		if item.Unit == "" || item.Value < 0 || item.HigherIsBetter == nil ||
			math.IsNaN(item.Value) || math.IsInf(item.Value, 0) {
			continue
		}
		semantic := comparisonSemantic(item)
		direction := boolKey(*item.HigherIsBetter)
		if semantic == "risk" {
			// Keep risk scores in their own magnitude-based bucket instead of
			// mixing them with any other /100 metric that is lower-is-better.
			direction = "risk"
		}
		name := item.Unit + "|" + direction + "|" + semantic
		entry, ok := buckets[name]
		if !ok {
			entry = &bucket{inverse: !*item.HigherIsBetter && semantic != "risk"}
			buckets[name] = entry
		}
		entry.keys = append(entry.keys, item.Key)
		entry.values = append(entry.values, item.Value)
	}
	out := make(map[string]comparableGroup)
	for _, entry := range buckets {
		if len(entry.keys) < 2 {
			continue
		}
		var maximum float64
		minimum := 0.0
		for _, value := range entry.values {
			converted := value
			if entry.inverse && value > 0 {
				converted = 1 / value
			}
			if converted > maximum {
				maximum = converted
			}
			if converted > 0 && (minimum == 0 || converted < minimum) {
				minimum = converted
			}
		}
		if maximum <= 0 {
			// Keep a real all-zero group visible.  It renders as an empty
			// magnitude bar (or a full quality bar for lower-is-better
			// metrics) instead of looking like missing data.
			maximum = 1
		}
		for _, key := range entry.keys {
			out[key] = comparableGroup{max: maximum, min: minimum, inverse: entry.inverse}
		}
	}
	return out
}

// comparableValue 把"越小越好"的指标翻转，使柱长始终表示"越长越好"。
func comparableValue(item model.Measurement, group comparableGroup) float64 {
	if group.inverse {
		if item.Value <= 0 {
			// Zero latency/loss is the best attainable value; avoid 1/0 while
			// retaining a visible full-quality bar.
			return group.max
		}
		return 1 / item.Value
	}
	return item.Value
}

func boolKey(value bool) string {
	if value {
		return "up"
	}
	return "down"
}

// resultTable 渲染带边框的表格。
func (r *textRenderer) resultTable(table model.Table) {
	table = displayTable(table)
	table = normalizeMatrixTable(table)
	table = visibleTableColumns(table)
	if table.Title != "" {
		title := table.Title
		if kind := matrixKindForTable(table.Key); kind == matrixCrystal || kind == matrixATTO {
			title += i18n.T("punct.colon")
		}
		r.indentedStyled(title, r.palette.AccentBold)
	}
	cellBarWidth := adaptiveBarWidth(r.width, 8)
	if tableNeedsStackedLayout(r.width, len(table.Columns)) {
		// A bar inside every stacked value adds noise and makes ANSI-aware
		// wrapping harder. Preserve the exact numeric text in this layout.
		cellBarWidth = 0
	}
	r.tableWithStyles(displayTableLabels(table.Columns), tableRowsWithBars(table, r.palette, cellBarWidth), nil, r.tableValueStyles(table))
	r.blank()
}

func visibleTableColumns(table model.Table) model.Table {
	if len(table.Columns) == 0 {
		return table
	}
	keep := make([]int, 0, len(table.Columns))
	for index, column := range table.Columns {
		if !isExplanatoryColumnKey(column.Key) {
			keep = append(keep, index)
		}
	}
	if len(keep) == len(table.Columns) {
		return table
	}
	columnMap := make(map[int]int, len(keep))
	out := table
	out.Columns = make([]model.TableColumn, 0, len(keep))
	for index, original := range keep {
		columnMap[original] = index
		out.Columns = append(out.Columns, table.Columns[original])
	}
	if table.RowIdentity != "" {
		out.RowIdentity = ""
		for original, column := range table.Columns {
			if column.Key == table.RowIdentity {
				if _, ok := columnMap[original]; ok {
					out.RowIdentity = column.Key
				}
				break
			}
		}
	}
	out.Rows = make([][]model.Value, len(table.Rows))
	for rowIndex, row := range table.Rows {
		filtered := make([]model.Value, 0, len(keep))
		for _, original := range keep {
			if original < len(row) {
				filtered = append(filtered, row[original])
			} else {
				filtered = append(filtered, model.RawValue(""))
			}
		}
		out.Rows[rowIndex] = filtered
	}
	return out
}

func isExplanatoryColumnKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "why", "rationale", "definition", "segment", "note", "comment", "description", "guidance",
		"metric_definition", "bucket_rule":
		return true
	default:
		return false
	}
}

func normalizeMatrixTable(table model.Table) model.Table {
	kind := matrixKindForTable(table.Key)
	if kind == "" {
		return table
	}
	if kind == matrixCrystal {
		table.Title = "Crystal"
	}
	if kind == matrixATTO {
		table.Title = "ATTO"
	}
	table.Columns = append([]model.TableColumn(nil), table.Columns...)
	columns := []string{}
	switch kind {
	case matrixCrystal:
		columns = []string{
			"probe.disk.column.workload", "probe.disk.column.read", "probe.disk.column.read_iops",
			"probe.disk.column.write", "probe.disk.column.write_iops", "probe.disk.column.offset", "probe.disk.column.status",
		}
	case matrixATTO:
		columns = []string{
			"probe.disk.column.block_size", "probe.disk.column.read", "probe.disk.column.read_iops",
			"probe.disk.column.write", "probe.disk.column.write_iops", "probe.disk.column.runtime",
			"probe.disk.column.offset", "probe.disk.column.status",
		}
	case matrixMixed:
		columns = []string{
			"probe.disk.column.block_size", "probe.disk.column.read", "probe.disk.column.read_iops",
			"probe.disk.column.write", "probe.disk.column.write_iops", "probe.disk.column.total",
		}
	}
	for index := range table.Columns {
		if index < len(columns) {
			table.Columns[index].Label = displayKey(columns[index])
		}
	}
	return table
}

func matrixKindForTable(key string) diskMatrixKind {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "disk.fio.crystal":
		return matrixCrystal
	case "disk.fio.atto":
		return matrixATTO
	case "disk.fio.mixed":
		return matrixMixed
	default:
		return ""
	}
}

func visibleMeasurements(result model.Result) []model.Measurement {
	tables := make(map[diskMatrixKind]bool)
	for _, table := range result.Tables {
		if kind := matrixKindForTable(table.Key); kind != "" && len(table.Rows) > 0 {
			tables[kind] = true
		}
	}
	items := make([]model.Measurement, 0, len(result.Measurements))
	for _, item := range result.Measurements {
		kind := matrixKindForMeasurement(item.Key)
		if kind == "" {
			items = append(items, item)
			continue
		}
		if tables[kind] {
			continue
		}
		items = append(items, item)
	}
	return items
}

func matrixKindForMeasurement(key string) diskMatrixKind {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.HasPrefix(lower, "crystal_"):
		return matrixCrystal
	case strings.HasPrefix(lower, "atto_"):
		return matrixATTO
	case strings.HasPrefix(lower, "fio_mixed_"):
		return matrixMixed
	default:
		return ""
	}
}

func tableRowsWithBars(table model.Table, palette termcolor.Palette, requestedBarWidth ...int) [][]string {
	rows := displayTableRows(table)
	numericColumns := numericTableColumnIndexes(table)
	if len(numericColumns) == 0 || len(rows) == 0 {
		return rows
	}
	// A table column is not necessarily a metric group.  Crystal/ATTO, for
	// example, put read and write throughput in adjacent columns; calculating a
	// maximum independently for each column makes a 4.63 MiB/s read hit a full
	// bar when that column has no faster read, even though a 1000 MiB/s write in
	// the same table is orders of magnitude faster.  Group columns by metric and
	// unit first, then scale every member against the group's range.  The shared
	// helper keeps compact ranges linear and switches wide ranges to a
	// min-relative logarithmic scale, while the table itself remains the scope
	// so values from different matrices never borrow a scale.
	stats := make(map[tableBarGroup]tableBarStats, len(numericColumns))
	valueWidths := make(map[int]int, len(numericColumns))
	semantics := make(map[int]string, len(numericColumns))
	directions := make(map[int]bool, len(numericColumns))
	for _, column := range numericColumns {
		higher := table.Columns[column].HigherIsBetter
		if riskNumericColumn(table, column) {
			// Risk magnitude is intentionally drawn directly: a larger
			// 0–100 score gets a longer warning bar even though the quality
			// direction metadata is lower-is-better.
			higher = true
		}
		semantics[column] = tableBarSemantic(table, column)
		directions[column] = higher
		for _, row := range rows {
			if column >= len(row) {
				continue
			}
			value, unit, ok := numericCell(row[column])
			if !ok || value < 0 {
				continue
			}
			group := tableBarGroup{semantic: semantics[column], unit: unit, higher: higher}
			valueWidths[column] = maxInt(valueWidths[column], textwidth.Width(row[column]))
			if value == 0 {
				entry := stats[group]
				entry.zero = true
				stats[group] = entry
			}
			if !higher && value > 0 {
				value = 1 / value
			}
			if value > 0 {
				entry := stats[group]
				if value > entry.max {
					entry.max = value
				}
				if entry.min == 0 || value < entry.min {
					entry.min = value
				}
				stats[group] = entry
			}
		}
	}
	for group, entry := range stats {
		if entry.max <= 0 && entry.zero {
			// A real all-zero column still gets a visible semantic bar: empty
			// for magnitude metrics, full quality for lower-is-better metrics.
			entry.max = 1
		}
		stats[group] = entry
	}
	cellBarWidth := 8
	if len(requestedBarWidth) > 0 {
		cellBarWidth = maxInt(0, requestedBarWidth[0])
	}
	barRows := make([][]string, len(rows))
	for rowIndex, original := range rows {
		barRows[rowIndex] = append([]string(nil), original...)
		for _, column := range numericColumns {
			if column >= len(barRows[rowIndex]) {
				continue
			}
			value, unit, ok := numericCell(barRows[rowIndex][column])
			if !ok || value < 0 {
				continue
			}
			higher := directions[column]
			group := tableBarGroup{semantic: semantics[column], unit: unit, higher: higher}
			entry, ok := stats[group]
			if !ok || entry.max <= 0 {
				continue
			}
			if !higher && value == 0 {
				value = entry.max
			} else if !higher {
				value = 1 / value
			}
			// Reserve one stable value field before the bar.  Without this
			// padding, a 1 MiB/s row starts its bar earlier than a 1000 MiB/s
			// row and the visual column drifts with digit count or unit text.
			cell := textwidth.Pad(barRows[rowIndex][column], valueWidths[column])
			if cellBarWidth > 0 {
				barRows[rowIndex][column] = cell + " " + palette.BarRelativeRange(value, entry.min, entry.max, cellBarWidth)
			}
		}
	}
	return barRows
}

func numericTableColumnIndexes(table model.Table) []int {
	columns := make([]int, 0, len(table.Columns))
	for index, column := range table.Columns {
		if column.Numeric {
			columns = append(columns, index)
		}
	}
	return columns
}

// tableBarGroup is the scale identity for a numeric table cell.  Unit is
// taken from the cell rather than inferred solely from the heading, so a
// malformed/mixed-unit table cannot silently compare MiB/s with IOPS.  Higher
// is part of the identity because a lower-is-better latency column uses the
// reciprocal magnitude.
type tableBarGroup struct {
	semantic string
	unit     string
	higher   bool
}

type tableBarStats struct {
	max  float64
	min  float64
	zero bool
}

// tableBarSemantic canonicalizes only the direction decoration shared by
// matrix columns. It reads the stable column key, never the localized label.
// More specific metric qualifiers (for example average vs P95 latency) stay in
// the key and therefore do not get a misleading common scale.
func tableBarSemantic(table model.Table, column int) string {
	if column < 0 || column >= len(table.Columns) {
		return ""
	}
	heading := strings.ToLower(strings.TrimSpace(table.Columns[column].Key))
	// Direction is not a metric: read/write and upload/download throughput
	// should share one scale, while qualifiers such as P50/P95 stay.
	if strings.Contains(heading, "iops") {
		return "iops"
	}
	if strings.Contains(heading, "mib_s") || strings.Contains(heading, "mbps") ||
		strings.Contains(heading, "throughput") || strings.Contains(heading, "bandwidth") {
		return "throughput"
	}
	for _, separator := range []string{"/", "_", "-", "(", ")", ":", "·"} {
		heading = strings.ReplaceAll(heading, separator, " ")
	}
	fields := strings.Fields(heading)
	kept := fields[:0]
	for _, field := range fields {
		// Do not remove substrings ("thread" contains "read").
		if field == "read" || field == "write" || field == "upload" || field == "download" ||
			field == "send" || field == "receive" || field == "sent" || field == "received" ||
			field == "inbound" || field == "outbound" || field == "tx" || field == "rx" ||
			field == "r" || field == "w" {
			continue
		}
		kept = append(kept, field)
	}
	if len(kept) == 0 {
		return ""
	}
	joined := strings.Join(kept, " ")
	switch {
	case strings.Contains(joined, "iops"):
		return "iops"
	case strings.Contains(joined, "throughput"), strings.Contains(joined, "bandwidth"):
		return "throughput"
	case joined == "total":
		// Mixed/ATTO tables may call the sum simply "合计".  Its unit keeps
		// it separate from any same-table non-throughput column.
		return ""
	default:
		return joined
	}
}

func riskNumericColumn(table model.Table, column int) bool {
	if column < 0 || column >= len(table.Columns) {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(table.Columns[column].Key))
	return key == "risk_score"
}

func numericCellValue(cell string) (float64, bool) {
	value, _, ok := numericCell(cell)
	return value, ok
}

// numericCell returns the leading number and the unit token that follows it.
// Keeping the unit with the parsed value lets table bars share a scale across
// read/write columns without ever comparing unlike units.
func numericCell(cell string) (float64, string, bool) {
	fields := strings.Fields(strings.TrimSpace(cell))
	if len(fields) == 0 || fields[0] == "—" || fields[0] == "-" {
		return 0, "", false
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, "", false
	}
	unit := ""
	if len(fields) > 1 {
		// Generated reports use one token (MiB/s, IOPS, ms, %, /100).  Only
		// the first suffix token is part of the unit; trailing prose should
		// not accidentally create a second scale for an otherwise equal cell.
		unit = strings.ToLower(strings.TrimSpace(fields[1]))
	}
	return value, unit, true
}

// table 渲染一张对齐的表格。
//
// 不画竖线边框：中英混排下竖线对齐一旦错位就特别刺眼，而列间距足够分隔数据。
// 只在表头下画一条横线。
func (r *textRenderer) table(columns []string, rows [][]string, rightAlign map[int]bool) {
	r.tableWithStyles(columns, rows, rightAlign, nil)
}

func (r *textRenderer) tableValueStyles(table model.Table) [][]func(string) string {
	styles := make([][]func(string) string, len(table.Rows))
	for rowIndex, row := range table.Rows {
		styles[rowIndex] = make([]func(string) string, len(row))
		for columnIndex, value := range row {
			styles[rowIndex][columnIndex] = r.valueStyle(value)
		}
	}
	return styles
}

func (r *textRenderer) tableWithStyles(columns []string, rows [][]string, rightAlign map[int]bool, styles [][]func(string) string) {
	if len(columns) == 0 {
		return
	}
	if tableNeedsStackedLayout(r.width, len(columns)) {
		r.stackedTableWithStyles(columns, rows, styles)
		return
	}
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = textwidth.Width(column)
	}
	for _, row := range rows {
		for index, cell := range row {
			if index < len(widths) {
				widths[index] = maxInt(widths[index], textwidth.Width(cell))
			}
		}
	}
	// 总宽超出版面时按比例压缩最宽的列，而不是让表格折行。
	shrinkColumns(widths, r.width-2-2*(len(columns)-1))

	var head strings.Builder
	head.WriteString("  ")
	for index, column := range columns {
		if index > 0 {
			head.WriteString("  ")
		}
		head.WriteString(textwidth.Pad(textwidth.Truncate(column, widths[index]), widths[index]))
	}
	r.line(r.palette.LabelBold(strings.TrimRight(head.String(), " ")))

	total := 2
	for index, width := range widths {
		total += width
		if index > 0 {
			total += 2
		}
	}
	r.line(r.palette.Dim("  " + strings.Repeat("─", maxInt(0, minInt(total-2, r.width-2)))))

	for rowIndex, row := range rows {
		var line strings.Builder
		line.WriteString("  ")
		for index := range columns {
			cell := ""
			if index < len(row) {
				cell = row[index]
			}
			if index > 0 {
				line.WriteString("  ")
			}
			cell = textwidth.Truncate(cell, widths[index])
			var rendered string
			if rightAlign[index] {
				rendered = textwidth.PadLeft(cell, widths[index])
			} else {
				rendered = textwidth.Pad(cell, widths[index])
			}
			if style := tableStyleAt(styles, rowIndex, index); style != nil {
				rendered = style(rendered)
			}
			line.WriteString(rendered)
		}
		r.line(strings.TrimRight(line.String(), " "))
	}
}

func tableStyleAt(styles [][]func(string) string, row, column int) func(string) string {
	if row < 0 || row >= len(styles) || column < 0 || column >= len(styles[row]) {
		return nil
	}
	return styles[row][column]
}

func tableNeedsStackedLayout(width, columns int) bool {
	if columns < 2 {
		return false
	}
	if width <= 0 {
		width = textWidth
	}
	// Two spaces indent, four useful columns per cell, and two spaces between
	// cells. Below this point a horizontal row would collapse into ellipses.
	return width < 2+columns*4+(columns-1)*2
}

func (r *textRenderer) stackedTableWithStyles(columns []string, rows [][]string, styles [][]func(string) string) {
	valueWidth := maxInt(1, r.width-4)
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			r.line(r.palette.Dim("  " + strings.Repeat("·", maxInt(1, minInt(8, r.width-2)))))
		}
		for columnIndex, column := range columns {
			value := ""
			if columnIndex < len(row) {
				value = row[columnIndex]
			}
			r.indentedStyled(column+i18n.T("punct.colon"), r.palette.LabelBold)
			for _, line := range wrapText(value, valueWidth) {
				if style := tableStyleAt(styles, rowIndex, columnIndex); style != nil {
					line = style(line)
				}
				r.linef("    %s", line)
			}
		}
	}
}

// shrinkColumns 在总宽超限时从最宽的列开始收缩。
func shrinkColumns(widths []int, budget int) {
	if budget <= 0 {
		return
	}
	minimum := 4
	if budget < len(widths)*minimum && len(widths) > 0 {
		minimum = maxInt(1, budget/len(widths))
	}
	total := 0
	for _, width := range widths {
		total += width
	}
	for total > budget {
		widest, index := 0, -1
		for position, width := range widths {
			if width > widest {
				widest, index = width, position
			}
		}
		// 窄终端上允许列收缩到 4 格；极窄视口再均匀收缩，
		// 以保证整行不溢出。省略号会明确表示被压缩的单元格。
		if index < 0 || widths[index] <= minimum {
			return
		}
		widths[index]--
		total--
	}
}
