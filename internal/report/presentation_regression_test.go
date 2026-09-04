package report

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/probe"
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

func TestTextFIOProducerMixedTableUsesCanonicalMetadataAndOrder(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	producerResult := runFIOProducerFixture(t)
	var mixed model.Table
	for _, table := range producerResult.Tables {
		if table.Key == "disk.fio.mixed" {
			mixed = table
			break
		}
	}
	if mixed.Key == "" {
		t.Fatalf("concrete disk producer did not emit disk.fio.mixed: %+v", producerResult.Tables)
	}
	wantColumns := map[string]model.TableColumn{
		"read_mib_s":  {Key: "read_mib_s", Label: "probe.disk.column.read", Numeric: true, HigherIsBetter: true},
		"read_iops":   {Key: "read_iops", Label: "probe.disk.column.read_iops", Numeric: true, HigherIsBetter: true},
		"write_mib_s": {Key: "write_mib_s", Label: "probe.disk.column.write", Numeric: true, HigherIsBetter: true},
		"write_iops":  {Key: "write_iops", Label: "probe.disk.column.write_iops", Numeric: true, HigherIsBetter: true},
		"total_mib_s": {Key: "total_mib_s", Label: "probe.disk.column.total", Numeric: true, HigherIsBetter: true},
	}
	seenColumns := make(map[string]bool, len(wantColumns))
	for _, column := range mixed.Columns {
		want, ok := wantColumns[column.Key]
		if !ok {
			continue
		}
		seenColumns[column.Key] = true
		if column != want {
			t.Fatalf("producer mixed column %q = %+v, want %+v", column.Key, column, want)
		}
	}
	for key := range wantColumns {
		if !seenColumns[key] {
			t.Fatalf("producer mixed table omitted canonical column %q: %+v", key, mixed.Columns)
		}
	}
	if len(mixed.Rows) == 0 || len(mixed.Rows[0]) != len(mixed.Columns) {
		t.Fatalf("producer mixed table row shape = columns:%d rows:%+v", len(mixed.Columns), mixed.Rows)
	}

	// Keep the table emitted by the concrete producer, while dropping unrelated
	// producer sections so any relative bars below can only come from this table.
	producerResult.Fields = nil
	producerResult.Measurements = nil
	producerResult.Evidence = nil
	producerResult.Methodology = model.Methodology{}
	producerResult.Notes = nil
	producerResult.Sources = nil
	producerResult.Tables = []model.Table{mixed}
	data := fioProducerReport(producerResult)
	output := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 160})
	if !strings.ContainsAny(output, "█▓▒░") {
		t.Fatalf("Text did not render numeric bars from concrete producer metadata:\n%s", output)
	}

	// Swap two complete, legal producer columns (including their row cells).
	// The display path must preserve each column's canonical key/label pairing.
	swappedResult := producerResult
	swappedResult.Tables = []model.Table{swapTableColumns(mixed, 1, 3)}
	swappedOutput := Text(fioProducerReport(swappedResult), TextOptions{Color: termcolor.LevelNone, Width: 160})
	header := fioMixedHeader(t, swappedOutput)
	writePosition := strings.Index(header, i18n.T("probe.disk.column.write"))
	readPosition := strings.Index(header, i18n.T("probe.disk.column.read"))
	if writePosition < 0 || readPosition < 0 || writePosition >= readPosition {
		t.Fatalf("swapped producer columns lost key/label order: header=%q\n%s", header, swappedOutput)
	}
}

func runFIOProducerFixture(t *testing.T) model.Result {
	t.Helper()
	fixtureDir := t.TempDir()
	fioPath := filepath.Join(fixtureDir, "fio")
	payload := fioProducerFixtureJSON()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--enghelp\" ]; then\n" +
		"  printf '%s\\n' 'psync'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' '" + payload + "'\n"
	if err := os.WriteFile(fioPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fio producer fixture: %v", err)
	}
	t.Setenv("PATH", fixtureDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(probe.ToolBinEnv, fixtureDir)

	var disk probe.Probe
	for _, definition := range probe.BuiltinDefinitions() {
		if definition.Probe.ID() == "disk" {
			disk = definition.Probe
			break
		}
	}
	if disk == nil {
		t.Fatal("probe.BuiltinDefinitions() did not expose the disk producer")
	}
	return disk.Run(context.Background(), probe.Environment{Config: config.Runtime{
		DiskPath: t.TempDir(), DiskMiB: 128,
	}})
}

func fioProducerFixtureJSON() string {
	jobs := []string{
		`{"jobname":"seqwrite","write":{"io_bytes":1,"bw_bytes":2097152}}`,
		`{"jobname":"seqread","read":{"io_bytes":1,"bw_bytes":2097152}}`,
		`{"jobname":"randread","read":{"io_bytes":1,"iops":10}}`,
		`{"jobname":"randwrite","write":{"io_bytes":1,"iops":20}}`,
		`{"jobname":"latency_qd1","read":{"io_bytes":1,"clat_ns":{"mean":1000000,"max":2000000,"percentile":{"95.00":1500000,"99.00":1800000}}}}`,
	}
	for _, job := range []struct {
		block                                string
		readBW, readIOPS, writeBW, writeIOPS int
	}{
		{block: "4k", readBW: 4194304, readIOPS: 10, writeBW: 8388608, writeIOPS: 20},
		{block: "64k", readBW: 8388608, readIOPS: 20, writeBW: 16777216, writeIOPS: 40},
		{block: "512k", readBW: 12582912, readIOPS: 30, writeBW: 25165824, writeIOPS: 60},
		{block: "1m", readBW: 16777216, readIOPS: 40, writeBW: 33554432, writeIOPS: 80},
	} {
		jobs = append(jobs, fmt.Sprintf(
			`{"jobname":"mix%s","read":{"io_bytes":1,"bw_bytes":%d,"iops":%d},"write":{"io_bytes":1,"bw_bytes":%d,"iops":%d}}`,
			job.block, job.readBW, job.readIOPS, job.writeBW, job.writeIOPS,
		))
	}
	return `{"fio version":"fio-fixture","jobs":[` + strings.Join(jobs, ",") + `]}`
}

func fioProducerReport(result model.Result) model.Report {
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "fio-producer", Profile: "standard", Requested: []string{"disk"}},
		Summary:       model.Summary{Status: result.Status},
		Results:       []model.Result{result},
	}
}

func swapTableColumns(table model.Table, left, right int) model.Table {
	table.Columns = append([]model.TableColumn(nil), table.Columns...)
	table.Columns[left], table.Columns[right] = table.Columns[right], table.Columns[left]
	rows := table.Rows
	table.Rows = make([][]model.Value, len(rows))
	for rowIndex, row := range rows {
		table.Rows[rowIndex] = append([]model.Value(nil), row...)
		table.Rows[rowIndex][left], table.Rows[rowIndex][right] = table.Rows[rowIndex][right], table.Rows[rowIndex][left]
	}
	return table
}

func fioMixedHeader(t *testing.T, output string) string {
	t.Helper()
	title := i18n.T("probe.disk.table.mixed")
	start := strings.Index(output, title)
	if start < 0 {
		t.Fatalf("mixed table title missing from output:\n%s", output)
	}
	for _, line := range strings.Split(output[start+len(title):], "\n") {
		if strings.Contains(line, i18n.T("probe.disk.column.block_size")) {
			return line
		}
	}
	t.Fatalf("mixed table header missing from output:\n%s", output)
	return ""
}
