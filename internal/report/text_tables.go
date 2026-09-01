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
	labelLimit := min(30, max(6, r.width*3/10))
	valueLimit := min(26, max(6, r.width/4))
	labelWidth, valueWidth := 0, 0
	for _, rawItem := range items {
		item := displayMeasurement(rawItem)
		display := displayValue(item.Display)
		labelWidth = max(labelWidth, textwidth.Width(item.Label))
		valueWidth = max(valueWidth, textwidth.Width(display))
	}
	labelWidth = min(labelWidth, labelLimit)
	valueWidth = min(valueWidth, valueLimit)
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
			barSize := min(adaptiveBarWidth(r.width, barWidth), max(0, available-textwidth.Width(rating)))
			if rating != "" && barSize < 4 {
				separateRating = true
				rating = ""
				barSize = min(adaptiveBarWidth(r.width, barWidth), max(0, available))
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
			available := max(1, r.width-textwidth.Width(prefix))
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
// communicates a higher observed risk; the renderer only inverts the explicit
// lower-is-better latency families below.
func riskScoreMeasurement(item model.Measurement) bool {
	return strings.TrimSpace(item.Unit) == "/100" && knownRiskMeasurementKey(item.Key)
}

// comparisonSemantic prevents unrelated measurements that happen to share a
// unit from borrowing one another's scale (for example memory usage and packet
// loss are both percentages, but neither is comparable to the other).
func comparisonSemantic(item model.Measurement) string {
	if riskScoreMeasurement(item) {
		return "risk"
	}
	key := strings.ToLower(strings.TrimSpace(item.Key))
	// Percentile and interval qualifiers are separate metrics even when their
	// units and quality direction match. Keeping them in distinct buckets makes
	// every bar compare like with like across targets instead of, for example,
	// scaling a DNS P50 against a DNS P95 or an iperf interval minimum against
	// the final whole-run throughput.
	switch strings.TrimSpace(item.Unit) {
	case "ms":
		switch key {
		case "best_dns_median_ms":
			return "dns-p50"
		case "best_tcp_median_ms":
			return "tcp-p50"
		}
		if semantic := dnsMeasurementSemantic(key); semantic != "" {
			return semantic
		}
		if semantic := tcpMeasurementSemantic(key); semantic != "" {
			return semantic
		}
	case "Mbps":
		return iperfMeasurementSemantic(key)
	case "MiB/s", "IOPS":
		return matrixMeasurementSemantic(key)
	}
	return ""
}

func dnsMeasurementSemantic(key string) string {
	parts := strings.Split(key, "_")
	if len(parts) != 5 || parts[0] != "dns" || parts[1] != "resolver" ||
		!canonicalTargetIndex(parts[2]) || parts[4] != "ms" {
		return ""
	}
	switch parts[3] {
	case "p50":
		return "dns-p50"
	case "p95":
		return "dns-p95"
	default:
		return ""
	}
}

func tcpMeasurementSemantic(key string) string {
	parts := strings.Split(key, "_")
	if len(parts) != 6 || parts[0] != "tcp" || parts[1] != "target" ||
		!canonicalTargetIndex(parts[2]) || (parts[3] != "ipv4" && parts[3] != "ipv6") || parts[5] != "ms" {
		return ""
	}
	switch parts[4] {
	case "p50":
		return "tcp-p50"
	case "p95":
		return "tcp-p95"
	default:
		return ""
	}
}

func iperfMeasurementSemantic(key string) string {
	parts := strings.Split(key, "_")
	if len(parts) == 6 && parts[0] == "iperf3" && parts[1] == "target" &&
		canonicalTargetIndex(parts[2]) && (parts[3] == "ipv4" || parts[3] == "ipv6") &&
		(parts[4] == "upload" || parts[4] == "download") && parts[5] == "mbps" {
		return "iperf-throughput"
	}
	if len(parts) == 8 && parts[0] == "iperf3" && parts[1] == "target" &&
		canonicalTargetIndex(parts[2]) && (parts[3] == "ipv4" || parts[3] == "ipv6") &&
		(parts[4] == "upload" || parts[4] == "download") && parts[5] == "interval" &&
		(parts[6] == "min" || parts[6] == "p50") && parts[7] == "mbps" {
		if parts[6] == "min" {
			return "iperf-interval-min"
		}
		return "iperf-interval-p50"
	}
	return ""
}

func canonicalTargetIndex(value string) bool {
	if len(value) < 2 || (len(value) > 2 && value[0] == '0') || value == "00" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
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
	return ""
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
		if semantic == "" {
			continue
		}
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

func normalizeMatrixTable(table model.Table) model.Table {
	kind := matrixKindForTable(table.Key)
	switch kind {
	case matrixCrystal:
		table.Title = "Crystal"
	case matrixATTO:
		table.Title = "ATTO"
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
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "_")
	switch {
	case knownCrystalMatrixMeasurement(parts):
		return matrixCrystal
	case knownATTOMatrixMeasurement(parts):
		return matrixATTO
	case knownMixedMatrixMeasurement(parts):
		return matrixMixed
	default:
		return ""
	}
}

func knownCrystalMatrixMeasurement(parts []string) bool {
	if (len(parts) != 5 && len(parts) != 6) || parts[0] != "crystal" ||
		!knownCrystalWorkload(parts[1], parts[2]) || !knownMatrixDirection(parts[3]) {
		return false
	}
	return knownMatrixMetric(parts[4:])
}

func knownATTOMatrixMeasurement(parts []string) bool {
	if (len(parts) != 4 && len(parts) != 5) || parts[0] != "atto" ||
		!knownATTOBlock(parts[1]) || !knownMatrixDirection(parts[2]) {
		return false
	}
	return knownMatrixMetric(parts[3:])
}

func knownMixedMatrixMeasurement(parts []string) bool {
	if (len(parts) != 5 && len(parts) != 6) || parts[0] != "fio" || parts[1] != "mixed" ||
		!knownMixedBlock(parts[2]) || !knownMatrixDirection(parts[3]) {
		return false
	}
	return knownMatrixMetric(parts[4:])
}

func knownCrystalWorkload(workload, queueDepth string) bool {
	switch workload + "_" + queueDepth {
	case "rnd4k_q1", "rnd4k_q32", "seq1m_q1", "seq1m_q8":
		return true
	default:
		return false
	}
}

func knownMatrixDirection(direction string) bool {
	return direction == "read" || direction == "write"
}

func knownMatrixMetric(parts []string) bool {
	switch len(parts) {
	case 1:
		return parts[0] == "iops"
	case 2:
		return parts[0] == "mib" && parts[1] == "s"
	default:
		return false
	}
}

func knownATTOBlock(block string) bool {
	switch block {
	case "512b", "1k", "2k", "4k", "8k", "16k", "32k", "64k", "128k", "256k", "512k", "1m", "2m", "4m", "8m", "16m", "32m", "64m":
		return true
	default:
		return false
	}
}

func knownMixedBlock(block string) bool {
	switch block {
	case "4k", "64k", "512k", "1m":
		return true
	default:
		return false
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
		semantic := tableBarSemantic(table, column)
		if semantic == "" {
			continue
		}
		higher := table.Columns[column].HigherIsBetter
		if riskNumericColumn(table, column) {
			// Risk magnitude is intentionally drawn directly: a larger
			// 0–100 score gets a longer warning bar even though the quality
			// direction metadata is lower-is-better.
			higher = true
		}
		semantics[column] = semantic
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
			valueWidths[column] = max(valueWidths[column], textwidth.Width(row[column]))
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
		cellBarWidth = max(0, requestedBarWidth[0])
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

// tableBarSemantic recognizes only explicit table-family and column-key pairs.
// It reads stable machine keys, never localized labels, so arbitrary numeric
// columns cannot borrow a metric's relative scale.
func tableBarSemantic(table model.Table, column int) string {
	if column < 0 || column >= len(table.Columns) {
		return ""
	}
	tableKey := strings.ToLower(strings.TrimSpace(table.Key))
	columnKey := strings.ToLower(strings.TrimSpace(table.Columns[column].Key))
	switch tableKey {
	case "network.dns.resolvers":
		switch columnKey {
		case "p50_ms":
			return "dns-p50"
		case "p95_ms":
			return "dns-p95"
		}
	case "network.latency.tcp_icmp":
		switch columnKey {
		case "tcp_p50_ms":
			return "tcp-p50"
		case "tcp_p95_ms":
			return "tcp-p95"
		}
	case "network.iperf3.results":
		if columnKey == "upload_mbps" || columnKey == "download_mbps" {
			return "iperf-throughput"
		}
	case "network.iperf3.stability":
		switch columnKey {
		case "minimum_mbps":
			return "iperf-interval-min"
		case "p50_mbps":
			return "iperf-interval-p50"
		}
	case "disk.fio.crystal", "disk.fio.atto", "disk.fio.mixed":
		switch columnKey {
		case "read_mib_s", "write_mib_s", "total_mib_s":
			return "fio-throughput"
		case "read_iops", "write_iops":
			return "fio-iops"
		}
	case "network.ipquality.ipv4.scores", "network.ipquality.ipv6.scores":
		if columnKey == "risk_score" || columnKey == "raw_or_equivalent_value" {
			return "risk"
		}
	}
	return ""
}

func riskNumericColumn(table model.Table, column int) bool {
	if column < 0 || column >= len(table.Columns) {
		return false
	}
	tableKey := strings.ToLower(strings.TrimSpace(table.Key))
	if tableKey != "network.ipquality.ipv4.scores" && tableKey != "network.ipquality.ipv6.scores" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(table.Columns[column].Key))
	return key == "risk_score" || key == "raw_or_equivalent_value"
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
				widths[index] = max(widths[index], textwidth.Width(cell))
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
	r.line(r.palette.Dim("  " + strings.Repeat("─", max(0, min(total-2, r.width-2)))))

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
	valueWidth := max(1, r.width-4)
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			r.line(r.palette.Dim("  " + strings.Repeat("·", max(1, min(8, r.width-2)))))
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
		minimum = max(1, budget/len(widths))
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
