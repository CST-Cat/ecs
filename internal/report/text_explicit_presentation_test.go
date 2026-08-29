package report

import (
	"strings"
	"testing"

	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func TestTextUnknownMeasurementFamiliesStayLiteral(t *testing.T) {
	items := []model.Measurement{
		{Key: "network_packet_loss_budget_percent", Label: "packet loss budget", Value: 10, Unit: "%", Display: model.RawValue("10 %"), HigherIsBetter: model.BoolPtr(false)},
		{Key: "network_packet_loss_budget_percent_other", Label: "packet loss budget other", Value: 20, Unit: "%", Display: model.RawValue("20 %"), HigherIsBetter: model.BoolPtr(false)},
		{Key: "risk_policy_version", Label: "risk policy version", Value: 1, Unit: "/100", Display: model.RawValue("1 /100"), HigherIsBetter: model.BoolPtr(false)},
		{Key: "risk_policy_version_other", Label: "risk policy version other", Value: 2, Unit: "/100", Display: model.RawValue("2 /100"), HigherIsBetter: model.BoolPtr(false)},
		{Key: "custom_processing_ms", Label: "custom processing", Value: 10, Unit: "ms", Display: model.RawValue("10 ms"), HigherIsBetter: model.BoolPtr(false)},
		{Key: "custom_processing_ms_other", Label: "custom processing other", Value: 20, Unit: "ms", Display: model.RawValue("20 ms"), HigherIsBetter: model.BoolPtr(false)},
	}
	if groups := groupComparable(items); len(groups) != 0 {
		t.Fatalf("unknown measurements borrowed relative groups: %#v", groups)
	}

	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID: "network", Title: "Network", Status: model.StatusOK, Measurements: items,
		}},
	}
	output := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120})
	if strings.ContainsAny(output, "█▓▒░") {
		t.Fatalf("unknown measurements received relative bars:\n%s", output)
	}
	for _, display := range []string{"10 %", "20 %", "1 /100", "2 /100", "10 ms", "20 ms"} {
		if !strings.Contains(output, display) {
			t.Fatalf("unknown measurement display %q was lost:\n%s", display, output)
		}
	}
}

func TestTextUnknownKeysUseCanonicalDefaultGroups(t *testing.T) {
	system := model.Result{
		ID: "system", Title: "System", Status: model.StatusOK,
		Fields: []model.Field{{Key: "custom_ipv6_forward_risk", Label: "custom", Value: model.RawValue("system")}},
		Tables: []model.Table{{Key: "system.custom_forward_risk", Title: "custom", Columns: []model.TableColumn{{Key: "value", Label: "Value"}}, Rows: [][]model.Value{{model.RawValue("system table")}}}},
	}
	systemGroups := textGroups(system)
	if len(systemGroups) != 1 || systemGroups[0].key != "system.hardware" {
		t.Fatalf("unknown system keys = %#v, want canonical hardware group", systemGroups)
	}

	network := model.Result{
		ID: "network", Title: "Network", Status: model.StatusOK,
		Fields:       []model.Field{{Key: "risk_policy_version", Label: "policy", Value: model.RawValue("network")}},
		Measurements: []model.Measurement{{Key: "network_packet_loss_budget_percent", Label: "budget", Display: model.RawValue("10 %")}},
		Tables:       []model.Table{{Key: "network.custom_risk_scores", Title: "custom", Columns: []model.TableColumn{{Key: "value", Label: "Value"}}, Rows: [][]model.Value{{model.RawValue("network table")}}}},
	}
	networkGroups := textGroups(network)
	if len(networkGroups) != 1 || networkGroups[0].key != "network.ip" {
		t.Fatalf("unknown network keys = %#v, want canonical IP group", networkGroups)
	}
}

func TestTextFullTableKeepsGenericDescriptionColumns(t *testing.T) {
	keys := []string{"description", "comment", "note", "guidance", "definition", "why", "rationale"}
	columns := make([]model.TableColumn, 0, len(keys))
	row := make([]model.Value, 0, len(keys))
	for _, key := range keys {
		columns = append(columns, model.TableColumn{Key: key, Label: key})
		row = append(row, model.RawValue(key+" value"))
	}
	table := model.Table{Key: "custom.details", Title: "Custom details", Columns: columns, Rows: [][]model.Value{row}}

	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Summary:       model.Summary{Status: model.StatusOK},
		Results:       []model.Result{{ID: "custom", Title: "Custom", Status: model.StatusOK, Tables: []model.Table{table}}},
	}
	output := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 160})
	for _, key := range keys {
		if !strings.Contains(output, key) || !strings.Contains(output, key+" value") {
			t.Fatalf("full Text omitted canonical column %q:\n%s", key, output)
		}
	}
}

func TestTextRendererPreservesDuplicateMachineKeys(t *testing.T) {
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Summary:       model.Summary{Status: model.StatusOK},
		Results: []model.Result{{
			ID: "custom", Title: "Custom", Status: model.StatusOK,
			Fields: []model.Field{
				{Key: "duplicate", Label: "field one", Value: model.RawValue("field-first")},
				{Key: "duplicate", Label: "field two", Value: model.RawValue("field-second")},
			},
			Measurements: []model.Measurement{
				{Key: "duplicate", Label: "measurement one", Display: model.RawValue("measurement-first")},
				{Key: "duplicate", Label: "measurement two", Display: model.RawValue("measurement-second")},
			},
		}},
	}
	groups := textGroups(data.Results[0])
	if len(groups) != 1 || len(groups[0].fields) != 2 || len(groups[0].measurements) != 2 {
		t.Fatalf("duplicate machine keys were changed before rendering: %#v", groups)
	}
	output := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120})
	for _, value := range []string{"field-first", "field-second", "measurement-first", "measurement-second"} {
		if got := strings.Count(output, value); got != 1 {
			t.Fatalf("duplicate-key value %q rendered %d times, want once:\n%s", value, got, output)
		}
	}
}

func TestTextKnownMeasurementFamiliesKeepExplicitSemantics(t *testing.T) {
	tests := []struct {
		name string
		item model.Measurement
		want string
	}{
		{name: "dns p50", item: model.Measurement{Key: "dns_resolver_01_p50_ms", Unit: "ms"}, want: "dns-p50"},
		{name: "dns p95", item: model.Measurement{Key: "dns_resolver_02_p95_ms", Unit: "ms"}, want: "dns-p95"},
		{name: "best DNS median", item: model.Measurement{Key: "best_dns_median_ms", Unit: "ms"}, want: "dns-p50"},
		{name: "tcp p50", item: model.Measurement{Key: "tcp_target_01_ipv4_p50_ms", Unit: "ms"}, want: "tcp-p50"},
		{name: "tcp p95", item: model.Measurement{Key: "tcp_target_02_ipv6_p95_ms", Unit: "ms"}, want: "tcp-p95"},
		{name: "best TCP median", item: model.Measurement{Key: "best_tcp_median_ms", Unit: "ms"}, want: "tcp-p50"},
		{name: "iperf throughput", item: model.Measurement{Key: "iperf3_target_01_ipv4_upload_mbps", Unit: "Mbps"}, want: "iperf-throughput"},
		{name: "iperf interval minimum", item: model.Measurement{Key: "iperf3_target_01_ipv6_download_interval_min_mbps", Unit: "Mbps"}, want: "iperf-interval-min"},
		{name: "iperf interval p50", item: model.Measurement{Key: "iperf3_target_01_ipv6_download_interval_p50_mbps", Unit: "Mbps"}, want: "iperf-interval-p50"},
		{name: "crystal matrix", item: model.Measurement{Key: "crystal_rnd4k_q1_read_mib_s", Unit: "MiB/s"}, want: "matrix-crystal"},
		{name: "atto matrix", item: model.Measurement{Key: "atto_512b_write_iops", Unit: "IOPS"}, want: "matrix-atto"},
		{name: "mixed matrix", item: model.Measurement{Key: "fio_mixed_4k_read_mib_s", Unit: "MiB/s"}, want: "matrix-mixed"},
		{name: "risk score", item: model.Measurement{Key: "ipv4_ipapi_risk_score", Unit: "/100"}, want: "risk"},
		{name: "unknown percent", item: model.Measurement{Key: "network_packet_loss_budget_percent", Unit: "%"}},
		{name: "unknown milliseconds", item: model.Measurement{Key: "custom_processing_ms", Unit: "ms"}},
		{name: "unknown risk", item: model.Measurement{Key: "risk_policy_version", Unit: "/100"}},
		{name: "near miss matrix", item: model.Measurement{Key: "crystal_custom_read_mib_s", Unit: "MiB/s"}},
		{name: "canonical IP quality source", item: model.Measurement{Key: "ipv4_ipinfo_risk_score", Unit: "/100"}, want: "risk"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := comparisonSemantic(test.item); got != test.want {
				t.Fatalf("comparison semantic = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTextKnownTableFamiliesGetBarsOnlyForExplicitColumns(t *testing.T) {
	dns := model.Table{
		Key: "network.dns.resolvers",
		Columns: []model.TableColumn{
			{Key: "p50_ms", Label: "P50", Numeric: true, HigherIsBetter: false},
			{Key: "jitter_ms", Label: "Jitter", Numeric: true, HigherIsBetter: false},
		},
		Rows: [][]model.Value{{model.RawValue("10 ms"), model.RawValue("1 ms")}, {model.RawValue("20 ms"), model.RawValue("2 ms")}},
	}
	rows := tableRowsWithBars(dns, termcolor.Palette{Level: termcolor.LevelNone}, 8)
	if !strings.ContainsAny(rows[0][0], "█▓▒░") || rows[0][1] != "1 ms" {
		t.Fatalf("DNS table bars were not limited to explicit P50/P95 columns: %#v", rows)
	}
	for _, test := range []struct {
		name, tableKey, columnKey, first, second string
	}{
		{name: "TCP", tableKey: "network.latency.tcp_icmp", columnKey: "tcp_p50_ms", first: "10 ms", second: "20 ms"},
		{name: "iperf results", tableKey: "network.iperf3.results", columnKey: "upload_mbps", first: "10 Mbps", second: "20 Mbps"},
		{name: "iperf stability", tableKey: "network.iperf3.stability", columnKey: "minimum_mbps", first: "10 Mbps", second: "20 Mbps"},
		{name: "fio matrix", tableKey: "disk.fio.crystal", columnKey: "read_mib_s", first: "10 MiB/s", second: "20 MiB/s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			table := model.Table{
				Key:     test.tableKey,
				Columns: []model.TableColumn{{Key: test.columnKey, Label: test.columnKey, Numeric: true, HigherIsBetter: true}},
				Rows:    [][]model.Value{{model.RawValue(test.first)}, {model.RawValue(test.second)}},
			}
			rows := tableRowsWithBars(table, termcolor.Palette{Level: termcolor.LevelNone}, 8)
			if !strings.ContainsAny(rows[0][0], "█▓▒░") {
				t.Fatalf("%s table lost its explicit relative bar: %#v", test.name, rows)
			}
		})
	}

	unknown := model.Table{
		Key:     "network.custom.details",
		Columns: []model.TableColumn{{Key: "latency_ms", Label: "Latency", Numeric: true, HigherIsBetter: false}},
		Rows:    [][]model.Value{{model.RawValue("10 ms")}, {model.RawValue("20 ms")}},
	}
	rows = tableRowsWithBars(unknown, termcolor.Palette{Level: termcolor.LevelNone}, 8)
	if strings.ContainsAny(rows[0][0], "█▓▒░") || rows[0][0] != "10 ms" {
		t.Fatalf("unknown table column borrowed a relative bar: %#v", rows)
	}

	risk := model.Table{
		Key:     "network.ipquality.ipv4.scores",
		Columns: []model.TableColumn{{Key: "risk_score", Label: "Risk", Numeric: true, HigherIsBetter: false}},
		Rows:    [][]model.Value{{model.RawValue("10 /100")}, {model.RawValue("20 /100")}},
	}
	rows = tableRowsWithBars(risk, termcolor.Palette{Level: termcolor.LevelNone}, 8)
	if !strings.ContainsAny(rows[0][0], "█▓▒░") {
		t.Fatalf("known risk score column lost its explicit bar: %#v", rows)
	}
}
