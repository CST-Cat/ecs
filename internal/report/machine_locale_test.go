package report

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

const machineLocaleRawProviderValue = "provider/telecom-original"

func machineLocaleReportFixture() model.Report {
	start := time.Date(2026, 8, 28, 8, 9, 10, 0, time.UTC)
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run: model.RunInfo{
			ID: "machine-locale", Profile: "full", StartedAt: start, CompletedAt: start.Add(time.Second),
			DurationMS: 1000, Exposure: "local", Redacted: true,
			Requested: []string{"latency", "nat", "speed"}, OutputFormats: []string{"json", "md", "html"},
		},
		Summary: model.Summary{
			Status: model.StatusOK, OK: 1,
			Messages: []model.Message{model.NewMessage("message.summary.allOK", 1)},
		},
		Results: []model.Result{{
			ID: "latency", Title: "module.latency.title", Description: "probe.latency.description", Status: model.StatusOK,
			SummaryMessages: []model.Message{model.NewMessage("message.summary.allOK", 1)},
			Fields: []model.Field{
				{Key: "endpoint_kind", Label: "probe.latency.column.region", Value: model.KeyValue("probe.latency.endpoint_kind.mainland_china")},
				{Key: "carrier", Label: "probe.cnspeed.column.carrier", Value: model.KeyValue("probe.cnspeed.carrier.telecom")},
				{Key: "status", Label: "probe.cnspeed.column.status", Value: model.KeyValue("probe.network.status.ok")},
				{Key: "provider", Label: "probe.cnspeed.column.node", Value: model.RawValue(machineLocaleRawProviderValue)},
			},
			Measurements: []model.Measurement{{
				Key: "tcp_p50_ms", Label: "probe.latency.column.tcp_p50", Value: 42.5, Unit: "ms",
				Display: model.RawValue("42.50 ms"), Rating: "probe.network.status.ok", Method: "tcp-connect-v1",
				HigherIsBetter: model.BoolPtr(false),
			}},
			Tables: []model.Table{
				{
					Key: "network.nat.stun_pool", Title: "probe.nat.table.stun_pool",
					Columns: []model.TableColumn{
						{Key: "server_name", Label: "probe.nat.column.server_name"},
						{Key: "server_address", Label: "probe.nat.column.server_address"},
						{Key: "kind", Label: "probe.nat.column.kind"},
					},
					Rows: [][]model.Value{{
						model.RawValue("stun-a.example"),
						model.RawValue("stun-a.example:3478"),
						model.KeyValue("probe.nat.stun_kind.dual_address"),
					}},
				},
				{
					Key: "network.iperf3.stability", Title: "probe.speed.table.stability",
					Columns: []model.TableColumn{
						{Key: "provider", Label: "probe.speed.column.provider"},
						{Key: "protocol", Label: "probe.speed.column.protocol"},
						{Key: "direction", Label: "probe.speed.column.direction"},
						{Key: "minimum_mbps", Label: "probe.speed.column.minimum", Numeric: true, HigherIsBetter: true},
						{Key: "p50_mbps", Label: "probe.speed.column.p50", Numeric: true, HigherIsBetter: true},
						{Key: "coefficient_of_variation_percent", Label: "probe.speed.column.cv", Numeric: true},
						{Key: "retransmits", Label: "probe.speed.column.retransmits", Numeric: true},
						{Key: "interval", Label: "probe.speed.column.interval"},
					},
					Rows: [][]model.Value{
						{
							model.RawValue(machineLocaleRawProviderValue), model.RawValue("tcp"),
							model.KeyValue("probe.speed.direction.upload"), model.RawValue("100.25"), model.RawValue("99.50"),
							model.RawValue("0.5"), model.RawValue("1"), model.RawValue("1s"),
						},
						{
							model.RawValue(machineLocaleRawProviderValue), model.RawValue("tcp"),
							model.KeyValue("probe.speed.direction.download"), model.RawValue("200.75"), model.RawValue("199.50"),
							model.RawValue("0.4"), model.RawValue("2"), model.RawValue("1s"),
						},
					},
				},
			},
			Evidence: model.NewEvidence(2, 3, "sample"),
		}},
	}
}

func loadMachineLocaleReportFixture(t *testing.T) (model.Report, []byte) {
	t.Helper()
	data := machineLocaleReportFixture()
	canonical, err := JSON(data)
	if err != nil {
		t.Fatalf("canonical machine JSON: %v", err)
	}
	for _, forbidden := range []string{
		`"grade"`, `"offline"`, `"headers"`, `"column_keys"`, "中国大陆", "电信", "双 IP", "上传", "下载",
	} {
		if bytes.Contains(canonical, []byte(forbidden)) {
			t.Fatalf("canonical machine JSON contains forbidden derived/localized fact %q:\n%s", forbidden, canonical)
		}
	}
	if !bytes.Contains(canonical, []byte(`"columns"`)) || !bytes.Contains(canonical, []byte(`"numeric": true`)) {
		t.Fatalf("canonical machine JSON did not preserve typed table columns:\n%s", canonical)
	}

	path := filepath.Join(t.TempDir(), "current-report.json")
	writeReportFile(t, path, canonical)
	loaded, err := LoadJSON(path)
	if err != nil {
		t.Fatalf("LoadJSON current fixture: %v", err)
	}
	roundTrip, err := JSON(loaded)
	if err != nil {
		t.Fatalf("JSON after LoadJSON: %v", err)
	}
	if !bytes.Equal(roundTrip, canonical) {
		t.Fatalf("LoadJSON changed canonical machine facts:\nbefore=%s\nafter=%s", canonical, roundTrip)
	}
	return loaded, canonical
}

func TestCurrentMachineContractFixtureRoundTripsThroughLoadJSON(t *testing.T) {
	data, _ := loadMachineLocaleReportFixture(t)
	assertMachineLocaleFixtureFacts(t, data)
}

func assertMachineLocaleFixtureFacts(t *testing.T, data model.Report) {
	t.Helper()
	if data.SchemaVersion != "ecs.report/v1" || len(data.Results) != 1 {
		t.Fatalf("fixture schema/results = %q/%d", data.SchemaVersion, len(data.Results))
	}
	result := data.Results[0]
	if len(result.Fields) != 4 || len(result.Measurements) != 1 || len(result.Tables) != 2 {
		t.Fatalf("fixture shape = fields=%d measurements=%d tables=%d", len(result.Fields), len(result.Measurements), len(result.Tables))
	}
	assertMachineLocaleKey(t, result.Fields[0].Value, "probe.latency.endpoint_kind.mainland_china")
	assertMachineLocaleKey(t, result.Fields[1].Value, "probe.cnspeed.carrier.telecom")
	assertMachineLocaleKey(t, result.Fields[2].Value, "probe.network.status.ok")
	if raw, ok := result.Fields[3].Value.Raw(); !ok || raw != machineLocaleRawProviderValue {
		t.Fatalf("provider field = %#v, want raw %q", result.Fields[3].Value, machineLocaleRawProviderValue)
	}
	if raw, ok := result.Measurements[0].Display.Raw(); !ok || raw != "42.50 ms" {
		t.Fatalf("measurement display = %#v, want raw %q", result.Measurements[0].Display, "42.50 ms")
	}
	if result.Measurements[0].Value != 42.5 || result.Measurements[0].HigherIsBetter == nil || *result.Measurements[0].HigherIsBetter {
		t.Fatalf("measurement typed fields = %+v", result.Measurements[0])
	}
	if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 3 || result.Evidence.DerivedGrade() != model.EvidencePartial {
		t.Fatalf("evidence facts = %+v", result.Evidence)
	}

	natTable := result.Tables[0]
	if natTable.Key != "network.nat.stun_pool" || len(natTable.Columns) != 3 || len(natTable.Rows) != 1 {
		t.Fatalf("NAT table shape = %+v", natTable)
	}
	assertMachineLocaleKey(t, natTable.Rows[0][2], "probe.nat.stun_kind.dual_address")
	if raw, ok := natTable.Rows[0][0].Raw(); !ok || raw != "stun-a.example" {
		t.Fatalf("NAT server name = %#v", natTable.Rows[0][0])
	}

	stabilityTable := result.Tables[1]
	if stabilityTable.Key != "network.iperf3.stability" || len(stabilityTable.Columns) != 8 || len(stabilityTable.Rows) != 2 {
		t.Fatalf("stability table shape = %+v", stabilityTable)
	}
	for index, column := range stabilityTable.Columns {
		if column.Key == "minimum_mbps" || column.Key == "p50_mbps" {
			if !column.Numeric || !column.HigherIsBetter {
				t.Fatalf("typed numeric column %q = %+v", column.Key, column)
			}
		} else if index == 5 || index == 6 {
			if !column.Numeric {
				t.Fatalf("typed numeric column %q lost Numeric=true", column.Key)
			}
		}
	}
	assertMachineLocaleKey(t, stabilityTable.Rows[0][2], "probe.speed.direction.upload")
	assertMachineLocaleKey(t, stabilityTable.Rows[1][2], "probe.speed.direction.download")
	for rowIndex, row := range stabilityTable.Rows {
		if raw, ok := row[0].Raw(); !ok || raw != machineLocaleRawProviderValue {
			t.Fatalf("stability provider row %d = %#v", rowIndex, row[0])
		}
	}
}

func TestMachineLocaleKeepsCanonicalFactsAndLocalizesStableKeys(t *testing.T) {
	data, canonical := loadMachineLocaleReportFixture(t)
	assertMachineLocaleFixtureFacts(t, data)

	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, test := range []struct {
		language i18n.Lang
		want     []string
		forbid   []string
	}{
		{
			language: i18n.LangZH,
			want:     []string{"中国大陆", "电信", "双 IP", "上传", "下载", machineLocaleRawProviderValue},
		},
		{
			language: i18n.LangEN,
			want:     []string{"Mainland China", "Telecom", "Dual address", "Upload", "Download", machineLocaleRawProviderValue},
			forbid:   []string{"中国大陆", "电信", "双 IP", "上传", "下载"},
		},
	} {
		t.Run(string(test.language), func(t *testing.T) {
			i18n.Set(test.language)
			localizedJSON, err := JSON(data)
			if err != nil {
				t.Fatalf("%s machine JSON: %v", test.language, err)
			}
			if !bytes.Equal(localizedJSON, canonical) {
				t.Fatalf("%s changed machine JSON:\ncanonical=%s\nlocalized=%s", test.language, canonical, localizedJSON)
			}

			textOutput := Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120})
			markdownOutput := Markdown(data, nil)
			htmlBytes, err := HTML(data, nil)
			if err != nil {
				t.Fatalf("%s HTML: %v", test.language, err)
			}
			outputs := map[string]string{
				"text":     textOutput,
				"markdown": markdownOutput,
				"html":     string(htmlBytes),
			}
			for format, output := range outputs {
				for _, marker := range test.want {
					if !strings.Contains(output, marker) {
						t.Errorf("%s %s missing localized/raw value %q:\n%s", test.language, format, marker, output)
					}
				}
				for _, forbidden := range test.forbid {
					if strings.Contains(output, forbidden) {
						t.Errorf("%s %s contains ECS-owned Chinese category %q:\n%s", test.language, format, forbidden, output)
					}
				}
				for _, stablePrefix := range []string{
					"probe.latency.endpoint_kind.", "probe.cnspeed.carrier.",
					"probe.nat.stun_kind.", "probe.speed.direction.",
				} {
					if strings.Contains(output, stablePrefix) {
						t.Errorf("%s %s leaked machine key prefix %q:\n%s", test.language, format, stablePrefix, output)
					}
				}
			}

			after, err := JSON(data)
			if err != nil {
				t.Fatalf("%s post-render machine JSON: %v", test.language, err)
			}
			if !bytes.Equal(after, canonical) {
				t.Fatalf("%s rendering mutated the original report:\ncanonical=%s\nafter=%s", test.language, canonical, after)
			}
		})
	}
}

func assertMachineLocaleKey(t *testing.T, value model.Value, want string) {
	t.Helper()
	key, ok := value.Key()
	if !ok || key != want {
		t.Fatalf("machine value = %#v, want KeyValue(%q)", value, want)
	}
}
