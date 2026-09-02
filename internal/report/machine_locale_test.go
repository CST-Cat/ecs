package report

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/probe"
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
		`"grade"`, `"offline"`, `"headers"`, `"column_keys"`, "中国大陆", "电信", "双 IP", "上传", "下载", "命令行指定",
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

const machineLocaleFakeIPerfExecutable = `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'iperf 3.16'
  exit 0
fi
udp=0
reverse=0
for arg do
  case "$arg" in
    -u) udp=1 ;;
    -R) reverse=1 ;;
  esac
done
if [ "$udp" = "1" ]; then
  printf '%s\n' '{"start":{"test_start":{"protocol":"UDP"}},"end":{"sum_received":{"bits_per_second":50000000,"jitter_ms":1.25,"packets":10,"lost_percent":2}}}'
elif [ "$reverse" = "1" ]; then
  printf '%s\n' '{"start":{"connected":[{"local_host":"local","remote_host":"remote"}],"test_start":{"protocol":"TCP","reverse":1}},"intervals":[{"sum":{"bits_per_second":90000000}},{"sum":{"bits_per_second":110000000}}],"end":{"sum_sent":{"bytes":90,"bits_per_second":90000000,"retransmits":1,"seconds":1},"sum_received":{"bytes":100,"bits_per_second":100000000,"seconds":1}}}'
else
  printf '%s\n' '{"start":{"connected":[{"local_host":"local","remote_host":"remote"}],"test_start":{"protocol":"TCP","reverse":0}},"intervals":[{"sum":{"bits_per_second":90000000}},{"sum":{"bits_per_second":110000000}}],"end":{"sum_sent":{"bytes":100,"bits_per_second":100000000,"retransmits":1,"seconds":1},"sum_received":{"bytes":90,"bits_per_second":90000000,"seconds":1}}}'
fi
`

func machineLocaleCustomIPerfResult(t *testing.T) model.Result {
	t.Helper()
	targets, err := config.ParseIPerfTargetList("fixture-provider=fixture.invalid:5200")
	if err != nil {
		t.Fatalf("ParseIPerfTargetList custom target: %v", err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "iperf3")
	if err := os.WriteFile(path, []byte(machineLocaleFakeIPerfExecutable), 0o755); err != nil {
		t.Fatalf("write iperf3 fixture: %v", err)
	}
	t.Setenv("PATH", directory)
	env := probe.Environment{
		Config: config.Runtime{
			IPVersion:     config.IPVersion4,
			IPerfDuration: time.Second,
			SpeedThreads:  2,
			IPerfTargets:  targets,
		},
		Network: probe.NetworkCapabilities{IPv4Usable: true},
	}
	for _, definition := range probe.BuiltinDefinitions() {
		if definition.Probe.ID() == "speed" {
			return definition.Probe.Run(context.Background(), env)
		}
	}
	t.Fatal("speed probe is not defined")
	return model.Result{}
}

func TestMachineLocaleCustomIPerfTargetUsesHostFactAcrossCanonicalAndEnglishRenderers(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	data := machineLocaleReportFixture()
	data.Results = append(data.Results, machineLocaleCustomIPerfResult(t))
	data.Summary.OK = len(data.Results)
	data.Summary.Messages = []model.Message{model.NewMessage("message.summary.allOK", data.Summary.OK)}

	speedResult := data.Results[len(data.Results)-1]
	if len(speedResult.Tables) == 0 || speedResult.Tables[0].Key != "network.iperf3.results" || len(speedResult.Tables[0].Rows) != 1 {
		t.Fatalf("custom target speed table = %#v", speedResult.Tables)
	}
	row := speedResult.Tables[0].Rows[0]
	if raw, ok := row[0].Raw(); !ok || raw != "fixture-provider" {
		t.Fatalf("custom target provider cell = %#v", row[0])
	}
	if raw, ok := row[1].Raw(); !ok || raw != "fixture.invalid" {
		t.Fatalf("custom target location cell = %#v, want raw host", row[1])
	}

	canonical, err := JSON(data)
	if err != nil {
		t.Fatalf("canonical custom target JSON: %v", err)
	}
	canonicalText := string(canonical)
	if strings.Contains(canonicalText, "命令行指定") {
		t.Fatalf("canonical custom target JSON contains ECS-localized location:\n%s", canonicalText)
	}
	if !strings.Contains(canonicalText, `"raw": "fixture.invalid"`) || !strings.Contains(canonicalText, machineLocaleRawProviderValue) {
		t.Fatalf("canonical custom target JSON lost raw host/provider facts:\n%s", canonicalText)
	}

	outputs := map[string]string{
		"text":     Text(data, TextOptions{Color: termcolor.LevelNone, Width: 120}),
		"markdown": Markdown(data, nil),
	}
	htmlBytes, err := HTML(data, nil)
	if err != nil {
		t.Fatalf("custom target HTML: %v", err)
	}
	outputs["html"] = string(htmlBytes)
	for format, output := range outputs {
		if strings.Contains(output, "命令行指定") {
			t.Errorf("English %s contains ECS-localized custom location:\n%s", format, output)
		}
		if !strings.Contains(output, "fixture.invalid") || !strings.Contains(output, machineLocaleRawProviderValue) {
			t.Errorf("English %s lost raw host/provider facts:\n%s", format, output)
		}
	}
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
