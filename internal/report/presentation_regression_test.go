package report

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func TestTextEnglishStructuredMachineFactsStayEnglishAcrossLayoutsAndColors(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	data := machineLocaleReportFixture()
	for _, test := range []struct {
		name    string
		compact bool
		color   termcolor.Level
	}{
		{name: "full-plain", color: termcolor.LevelNone},
		{name: "full-color", color: termcolor.LevelBasic},
		{name: "compact-plain", compact: true, color: termcolor.LevelNone},
		{name: "compact-color", compact: true, color: termcolor.LevelBasic},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := Text(data, TextOptions{Color: test.color, Compact: test.compact, Width: 120})
			for _, character := range output {
				if unicode.Is(unicode.Han, character) {
					t.Fatalf("English Text leaked ECS-owned Chinese machine fact %q:\n%s", character, output)
				}
			}
			for _, stablePrefix := range []string{
				"probe.latency.endpoint_kind.", "probe.cnspeed.carrier.",
				"probe.nat.stun_kind.", "probe.speed.direction.",
			} {
				if strings.Contains(output, stablePrefix) {
					t.Fatalf("English Text leaked machine key prefix %q:\n%s", stablePrefix, output)
				}
			}
		})
	}
}

func TestTextRawStatusFragmentsStayNeutralInFullAndCompactLayouts(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	rawValues := []string{
		"failed-provider.example", "available-host", "blocked.example", "trueNAS",
		"provider-failed.example", "domain-available.example", "host-blocked.example",
		"failed", "available", "blocked",
	}
	fields := make([]model.Field, 0, len(rawValues))
	measurements := make([]model.Measurement, 0, len(rawValues))
	rows := make([][]model.Value, 0, len(rawValues))
	for index, raw := range rawValues {
		fields = append(fields, model.Field{
			Key: fmt.Sprintf("raw_field_%d", index), Label: fmt.Sprintf("Raw field %d", index), Value: model.RawValue(raw),
		})
		measurements = append(measurements, model.Measurement{
			Key: fmt.Sprintf("raw_measurement_%d", index), Label: fmt.Sprintf("Raw measurement %d", index), Display: model.RawValue(raw),
		})
		rows = append(rows, []model.Value{model.RawValue(raw)})
	}
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           machineLocaleReportFixture().Run,
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID: "raw-status-fragments", Title: "Raw status fragments", Status: model.StatusOK,
			Fields: fields, Measurements: measurements,
			Tables: []model.Table{{
				Key: "raw-status-fragments", Title: "Raw status fragments",
				Columns: []model.TableColumn{{Key: "value", Label: "Value"}}, Rows: rows,
			}},
		}},
	}

	for _, compact := range []bool{false, true} {
		compact := compact
		t.Run(map[bool]string{false: "full", true: "compact"}[compact], func(t *testing.T) {
			plain := Text(data, TextOptions{Color: termcolor.LevelNone, Compact: compact, Width: 120})
			colored := Text(data, TextOptions{Color: termcolor.LevelBasic, Compact: compact, Width: 120})
			if strings.Contains(plain, "\x1b") {
				t.Fatalf("plain %s Text contains ANSI escape", map[bool]string{false: "full", true: "compact"}[compact])
			}
			if !strings.Contains(colored, "\x1b[") {
				t.Fatalf("colored %s Text contains no ANSI styling", map[bool]string{false: "full", true: "compact"}[compact])
			}
			if got := stripANSI(colored); got != plain {
				t.Fatalf("color changed %s Text content beyond presentation:\nplain=%q\ncolored=%q", map[bool]string{false: "full", true: "compact"}[compact], plain, colored)
			}
			for _, raw := range rawValues {
				assertTextTokenStyle(t, plain, raw, false)
				assertTextTokenStyle(t, colored, raw, false)
			}
		})
	}
}

func TestCompactTextGroupingUsesMachineKeysForFieldsAndTables(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		language := language
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			result := model.Result{
				ID: "network", Title: "module.network.title", Status: model.StatusOK,
				Fields: []model.Field{
					// The labels intentionally describe the other category. Only the
					// stable field keys may select the group.
					{Key: "provider", Label: i18n.T("probe.network.column.risk"), Value: model.RawValue("ip-field")},
					{Key: "fraud_record", Label: i18n.T("probe.network.column.source"), Value: model.RawValue("risk-field")},
				},
				Tables: []model.Table{
					// Titles intentionally carry translated text from the opposite
					// grouping category. Table keys must remain authoritative.
					{
						Key: "network.egress.overview", Title: i18n.T("probe.network.table.ipquality.scores"),
						Columns: []model.TableColumn{{Key: "value", Label: "Value"}},
						Rows:    [][]model.Value{{model.RawValue("egress-table")}},
					},
					{
						Key: "network.ipquality.ipv4.scores", Title: i18n.T("probe.network.table.overview"),
						Columns: []model.TableColumn{{Key: "value", Label: "Value"}},
						Rows:    [][]model.Value{{model.RawValue("risk-table")}},
					},
				},
			}
			groups := textGroups(result)
			wantKeys := []string{"network.egress", "network.ip", "network.risk"}
			if len(groups) != len(wantKeys) {
				t.Fatalf("groups = %#v, want %v", groups, wantKeys)
			}
			for index, want := range wantKeys {
				if groups[index].key != want {
					t.Fatalf("group %d key = %q, want %q (groups=%#v)", index, groups[index].key, want, groups)
				}
			}

			data := model.Report{
				SchemaVersion: "ecs.report/v1",
				Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
				Run:           machineLocaleReportFixture().Run,
				Summary:       model.Summary{Status: model.StatusOK},
				Results:       []model.Result{result},
			}
			output := Text(data, TextOptions{Color: termcolor.LevelNone, Compact: true, Width: 120})
			wantTitles := []string{
				i18n.T("report.group.network.egress"),
				i18n.T("report.group.network.ip"),
				i18n.T("report.group.network.risk"),
			}
			previous := -1
			for index, title := range wantTitles {
				heading := fmt.Sprintf("%d. %s", index+1, title)
				if language == i18n.LangZH {
					heading = fmt.Sprintf("%s、%s", chineseNumeral(index+1), title)
				}
				position := strings.Index(output, heading)
				if position < 0 || position <= previous {
					t.Fatalf("compact grouping heading %q not in stable order (position=%d, previous=%d):\n%s", heading, position, previous, output)
				}
				previous = position
			}
			for _, marker := range []string{"ip-field", "risk-field", "egress-table", "risk-table"} {
				if !strings.Contains(output, marker) {
					t.Fatalf("compact grouping output lost %q:\n%s", marker, output)
				}
			}
		})
	}
}

func TestTextMatrixColumnsStayLocalizedAndAligned(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	table := func(key string, columns []model.TableColumn) model.Table {
		row := make([]model.Value, len(columns))
		for index := range row {
			row[index] = model.RawValue("value")
		}
		return model.Table{Key: key, Title: "probe.disk.table." + strings.TrimPrefix(key, "disk.fio."), Columns: columns, Rows: [][]model.Value{row}}
	}
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "matrix-columns", Profile: "standard"},
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID: "disk", Title: "module.disk.title", Status: model.StatusOK,
			Tables: []model.Table{
				table("disk.fio.crystal", []model.TableColumn{
					{Key: "workload", Label: "probe.disk.column.workload"},
					{Key: "read_mib_s", Label: "probe.disk.column.read"},
					{Key: "read_iops", Label: "probe.disk.column.read_iops"},
					{Key: "write_mib_s", Label: "probe.disk.column.write"},
					{Key: "write_iops", Label: "probe.disk.column.write_iops"},
					{Key: "start_offset", Label: "probe.disk.column.offset"},
					{Key: "status", Label: "probe.disk.column.status"},
				}),
				table("disk.fio.atto", []model.TableColumn{
					{Key: "block_size", Label: "probe.disk.column.block_size"},
					{Key: "read_mib_s", Label: "probe.disk.column.read"},
					{Key: "read_iops", Label: "probe.disk.column.read_iops"},
					{Key: "write_mib_s", Label: "probe.disk.column.write"},
					{Key: "write_iops", Label: "probe.disk.column.write_iops"},
					{Key: "runtime", Label: "probe.disk.column.runtime"},
					{Key: "start_offset", Label: "probe.disk.column.offset"},
					{Key: "status", Label: "probe.disk.column.status"},
				}),
				table("disk.fio.mixed", []model.TableColumn{
					{Key: "block_size", Label: "probe.disk.column.block_size"},
					{Key: "read_mib_s", Label: "probe.disk.column.read"},
					{Key: "read_iops", Label: "probe.disk.column.read_iops"},
					{Key: "write_mib_s", Label: "probe.disk.column.write"},
					{Key: "write_iops", Label: "probe.disk.column.write_iops"},
					{Key: "total_mib_s", Label: "probe.disk.column.total"},
				}),
			},
		}},
	}

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		output := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120})
		if language == i18n.LangEN {
			for _, character := range output {
				if unicode.Is(unicode.Han, character) {
					t.Fatalf("English matrix output leaked Han character %q:\n%s", character, output)
				}
			}
			for _, marker := range []string{"Workload", "Block size", "Start offset", "Timing", "Total", "Status"} {
				if !strings.Contains(output, marker) {
					t.Fatalf("English matrix output missing %q:\n%s", marker, output)
				}
			}
		} else {
			for _, marker := range []string{"工作负载", "块大小", "起始偏移", "计时", "合计", "状态"} {
				if !strings.Contains(output, marker) {
					t.Fatalf("Chinese matrix output missing %q:\n%s", marker, output)
				}
			}
		}
	}
}
