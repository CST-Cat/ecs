package probe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/module"
)

func writeThroughputExecutable(t *testing.T, name, body string) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv(ToolBinEnv, directory)
	return path
}

const fakeIPerfExecutable = `#!/bin/sh
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
elif [ "$FAKE_IPERF_PARTIAL" = "1" ] && [ "$reverse" = "1" ]; then
  printf '%s\n' '{"error":"fixture reverse unavailable"}'
elif [ "$reverse" = "1" ]; then
  printf '%s\n' '{"start":{"connected":[{"local_host":"local","remote_host":"remote"}],"test_start":{"protocol":"TCP","reverse":1}},"intervals":[{"sum":{"bits_per_second":90000000}},{"sum":{"bits_per_second":110000000}}],"end":{"sum_sent":{"bytes":90,"bits_per_second":90000000,"retransmits":1,"seconds":1},"sum_received":{"bytes":100,"bits_per_second":100000000,"seconds":1}}}'
else
  printf '%s\n' '{"start":{"connected":[{"local_host":"local","remote_host":"remote"}],"test_start":{"protocol":"TCP","reverse":0}},"intervals":[{"sum":{"bits_per_second":90000000}},{"sum":{"bits_per_second":110000000}}],"end":{"sum_sent":{"bytes":100,"bits_per_second":100000000,"retransmits":1,"seconds":1},"sum_received":{"bytes":90,"bits_per_second":90000000,"seconds":1}}}'
fi
`

func TestSpeedProducerBuildsStableSuccessDirectly(t *testing.T) {
	writeThroughputExecutable(t, "iperf3", fakeIPerfExecutable)
	env := Environment{
		Config: config.Runtime{
			IPVersion:     config.IPVersion4,
			IPerfDuration: time.Second,
			SpeedThreads:  2,
			IPerfTargets: []config.IPerfEndpoint{{
				Name: "fixture", Host: "fixture.invalid", Networks: "IPv4", PortStart: 5200, PortEnd: 5200,
			}},
		},
		Network: NetworkCapabilities{IPv4Usable: true},
	}
	result := (speedProbe{}).Run(context.Background(), env)
	if result.Title != "module.speed.title" || result.Description != "probe.speed.description" || result.Status != model.StatusOK {
		t.Fatalf("speed result metadata/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.speed.profile" {
		t.Fatalf("speed methodology = %+v", result.Methodology)
	}
	if len(result.Tables) != 2 || result.Tables[0].Title != "probe.speed.table.results" || result.Tables[1].Title != "probe.speed.table.stability" {
		t.Fatalf("speed tables = %+v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	if len(row) != len(result.Tables[0].Columns) || row[0].Text() != "fixture" || row[3].Text() == "失败" {
		t.Fatalf("speed result row = %#v", row)
	}
	if status, ok := row[8].Key(); !ok || status != "probe.speed.status.complete" {
		t.Fatalf("speed status = %#v", row[8])
	}
	seenDirections := map[string]bool{}
	for _, stabilityRow := range result.Tables[1].Rows {
		if direction, ok := stabilityRow[2].Key(); ok {
			seenDirections[direction] = true
		}
	}
	if !seenDirections["probe.speed.direction.upload"] || !seenDirections["probe.speed.direction.download"] {
		t.Fatalf("speed stability directions = %#v", result.Tables[1].Rows)
	}
	if result.Tables[0].Columns[3].Label != "probe.speed.column.upload" || result.Tables[1].Columns[3].Label != "probe.speed.column.minimum" {
		t.Fatalf("speed column labels = %#v/%#v", result.Tables[0].Columns, result.Tables[1].Columns)
	}
	if len(result.Measurements) == 0 || result.Measurements[0].Label != "probe.speed.metric.upload" {
		t.Fatalf("speed measurements = %#v", result.Measurements)
	}
	if raw, ok := result.Measurements[0].Display.Raw(); !ok || raw == "" {
		t.Fatalf("speed measurement display = %#v", result.Measurements[0].Display)
	}
	if len(result.Fields) != 7 || result.Fields[0].Label != "probe.speed.field.engine" {
		t.Fatalf("speed fields = %#v", result.Fields)
	}
	if len(result.Sources) != 2 || result.Sources[0].Purpose != "probe.speed.source.iperf3" {
		t.Fatalf("speed sources = %#v", result.Sources)
	}
	if len(result.Notes) != 5 || result.SummaryMessages[0].Key != "probe.speed.summary.values" {
		t.Fatalf("speed notes/summary = %#v/%#v", result.Notes, result.SummaryMessages)
	}
	if result.Evidence == nil || result.Evidence.Valid != 3 || result.Evidence.Expected != 3 {
		t.Fatalf("speed evidence = %+v", result.Evidence)
	}
	assertProducerParameterScope(t, result,
		"ip_version", "configured_duration", "configured_threads", "targets",
		"tool_version", "threads", "duration",
	)
	parameters := result.Methodology.Parameters
	if parameters["ip_version"] != config.IPVersion4 || parameters["configured_duration"] != "1s" || parameters["configured_threads"] != "2" || parameters["targets"] != comparisonParameterJSON(env.Config.IPerfTargets) || parameters["tool_version"] != "iperf 3.16" || parameters["threads"] != "2" || parameters["duration"] != "1s" {
		t.Fatalf("speed comparison parameters = %v", parameters)
	}
}

func TestSpeedProducerBuildsStablePartialStatusDirectly(t *testing.T) {
	path := writeThroughputExecutable(t, "iperf3", fakeIPerfExecutable)
	t.Setenv("FAKE_IPERF_PARTIAL", "1")
	env := Environment{
		Config: config.Runtime{
			IPVersion:     config.IPVersion4,
			IPerfDuration: time.Second,
			SpeedThreads:  2,
			IPerfTargets: []config.IPerfEndpoint{{
				Name: "fixture", Host: "fixture.invalid", Networks: "IPv4", PortStart: 5200, PortEnd: 5200,
			}},
		},
		Network: NetworkCapabilities{IPv4Usable: true},
	}
	result := runIPerfSpeed(context.Background(), env, path)
	if result.Status != model.StatusWarning || len(result.Failures) == 0 {
		t.Fatalf("speed partial status/failures = %s/%+v", result.Status, result.Failures)
	}
	row := result.Tables[0].Rows[0]
	if status, ok := row[8].Key(); !ok || status != "probe.speed.status.partial" {
		t.Fatalf("speed partial status = %#v", row[8])
	}
	if status, ok := row[4].Key(); !ok || status != "probe.speed.status.failed" {
		t.Fatalf("speed partial failed direction = %#v", row[4])
	}
	if len(result.Measurements) == 0 || result.SummaryMessages[0].Key != "probe.speed.summary.values" {
		t.Fatalf("speed partial measurements/summary = %#v/%#v", result.Measurements, result.SummaryMessages)
	}
	assertProducerParameterScope(t, result,
		"ip_version", "configured_duration", "configured_threads", "targets",
		"tool_version", "threads", "duration",
	)
}

func TestCNSpeedProducerBuildsStableSuccessDirectly(t *testing.T) {
	oldURL := cnNodeListURLForTest
	oldFactory := cnspeedHTTPClientFactory
	t.Cleanup(func() {
		cnNodeListURLForTest = oldURL
		cnspeedHTTPClientFactory = oldFactory
	})
	cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
	csvBody := "id,operator,province,city,host,pingUrl,downloadUrl,active\n" +
		"1,电信,上海,上海,cn1,https://8.8.8.8/ping,https://8.8.8.8/file,1\n" +
		"2,联通,北京,北京,cn2,https://8.8.4.4/ping,https://8.8.4.4/file,1\n" +
		"3,移动,广州,广州,cn3,https://1.1.1.1/ping,https://1.1.1.1/file,1\n"
	cnspeedHTTPClientFactory = func(time.Duration, string, cnIPResolver, cnDialContextFunc) *http.Client {
		return &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
			body := csvBody
			if request.URL.Path == "/ping" {
				body = "pong"
			} else if request.URL.Path == "/file" {
				body = "abcdefgh"
			}
			return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(body))), nil
		})}
	}
	result := (cnSpeedProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4, HTTPTimeout: time.Second}})
	if result.Title != "module.cnspeed.title" || result.Description != "probe.cnspeed.description" || result.Status != model.StatusOK {
		t.Fatalf("cnspeed result metadata/status = %+v", result)
	}
	if result.Evidence == nil || result.Evidence.Valid != 3 || result.Evidence.Expected != 3 {
		t.Fatalf("cnspeed evidence = %+v", result.Evidence)
	}
	if len(result.Fields) != 3 || result.Fields[0].Label != "probe.cnspeed.field.node_list" || result.Fields[0].Value.Text() != "speedtest.cn-CN-ID@audited-commit" {
		t.Fatalf("cnspeed fields = %#v", result.Fields)
	}
	if raw, ok := result.Fields[0].Value.Raw(); !ok || raw != "speedtest.cn-CN-ID@audited-commit" {
		t.Fatalf("cnspeed node list variant = %#v", result.Fields[0].Value)
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.cnspeed.table.nodes" || len(result.Tables[0].Rows) != 3 {
		t.Fatalf("cnspeed table = %#v", result.Tables)
	}
	for _, row := range result.Tables[0].Rows {
		if carrier, ok := row[0].Key(); !ok || !strings.HasPrefix(carrier, "probe.cnspeed.carrier.") {
			t.Fatalf("cnspeed carrier = %#v", row[0])
		}
		if status, ok := row[6].Key(); !ok || status != "probe.cnspeed.status.complete" {
			t.Fatalf("cnspeed status = %#v", row[6])
		}
	}
	if len(result.Measurements) != 3 || result.Measurements[0].Label != "probe.cnspeed.metric.download" || result.Sources[0].Purpose != "probe.cnspeed.source.nodes" {
		t.Fatalf("cnspeed measurements/sources = %#v/%#v", result.Measurements, result.Sources)
	}
	if len(result.Notes) != 5 || result.SummaryMessages[0].Key != "probe.cnspeed.summary.values" {
		t.Fatalf("cnspeed notes/summary = %#v/%#v", result.Notes, result.SummaryMessages)
	}
	assertProducerParameterScope(t, result, "ip_version", "download_budget", "selected_nodes")
	parameters := result.Methodology.Parameters
	if parameters["ip_version"] != config.IPVersion4 || parameters["download_budget"] != "8s 或 100 MiB" || parameters["selected_nodes"] != selectedValueJSON(result.Tables[0], 0, 1, 2) {
		t.Fatalf("cnspeed comparison parameters = %v", parameters)
	}
}

func runCNSpeedComparisonFixture(t *testing.T, csvBody, downloadBody string, downloadStatus int) model.Result {
	t.Helper()
	oldURL := cnNodeListURLForTest
	oldFactory := cnspeedHTTPClientFactory
	defer func() {
		cnNodeListURLForTest = oldURL
		cnspeedHTTPClientFactory = oldFactory
	}()
	cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
	cnspeedHTTPClientFactory = func(time.Duration, string, cnIPResolver, cnDialContextFunc) *http.Client {
		return &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/ping" {
				return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader("pong"))), nil
			}
			if request.URL.Path == "/file" {
				return fixtureResponse(downloadStatus, io.NopCloser(strings.NewReader(downloadBody))), nil
			}
			return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(csvBody))), nil
		})}
	}
	return (cnSpeedProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
		IPVersion: config.IPVersion4, HTTPTimeout: time.Second,
	}})
}

func TestCNSpeedProducerSelectedNodeComparisonScope(t *testing.T) {
	csvBody := "id,operator,province,city,host,pingUrl,downloadUrl,active\n" +
		"1,电信,上海,上海,cn1,https://8.8.8.8/ping,https://8.8.8.8/file,1\n" +
		"2,联通,北京,北京,cn2,https://8.8.4.4/ping,https://8.8.4.4/file,1\n" +
		"3,移动,广州,广州,cn3,https://1.1.1.1/ping,https://1.1.1.1/file,1\n"
	base := runCNSpeedComparisonFixture(t, csvBody, "abcdefgh", http.StatusOK)
	if len(base.Tables) != 1 || base.Methodology.Parameters["selected_nodes"] == "" {
		t.Fatalf("cnspeed producer omitted selected-node scope: %+v", base.Methodology.Parameters)
	}
	if got, want := base.Methodology.Parameters["selected_nodes"], selectedValueJSON(base.Tables[0], 0, 1, 2); got != want {
		t.Fatalf("cnspeed selected-node JSON = %q, want %q", got, want)
	}

	differentDownload := runCNSpeedComparisonFixture(t, csvBody, strings.Repeat("x", 4096), http.StatusOK)
	if base.Methodology.Parameters["selected_nodes"] != differentDownload.Methodology.Parameters["selected_nodes"] {
		t.Fatal("cnspeed download result changed selected-node comparison scope")
	}
	differentStatus := runCNSpeedComparisonFixture(t, csvBody, "download failed", http.StatusServiceUnavailable)
	if base.Methodology.Parameters["selected_nodes"] != differentStatus.Methodology.Parameters["selected_nodes"] {
		t.Fatal("cnspeed status change changed selected-node comparison scope")
	}

	changedCSV := strings.Replace(csvBody, "1,电信,上海,上海,cn1", "1b,电信,浙江,杭州,cn1b", 1)
	changedNode := runCNSpeedComparisonFixture(t, changedCSV, "abcdefgh", http.StatusOK)
	if base.Methodology.Parameters["selected_nodes"] == changedNode.Methodology.Parameters["selected_nodes"] {
		t.Fatal("cnspeed selected node identity change was omitted from comparison scope")
	}

	selected := selectedValueRows(base.Tables[0].Rows, 0, 1, 2)
	rawCarrier := make([][]model.Value, len(selected))
	for index, row := range selected {
		rawCarrier[index] = append([]model.Value(nil), row...)
	}
	rawCarrier[0][0] = model.RawValue(selected[0][0].Text())
	if comparisonParameterJSON(selected) == comparisonParameterJSON(rawCarrier) {
		t.Fatal("cnspeed selected-node JSON ignored a raw/key Value tag change")
	}
}

func TestCNSpeedProducerBuildsStableSkipDirectly(t *testing.T) {
	oldURL := cnNodeListURLForTest
	oldFactory := cnspeedHTTPClientFactory
	t.Cleanup(func() {
		cnNodeListURLForTest = oldURL
		cnspeedHTTPClientFactory = oldFactory
	})
	cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
	cnspeedHTTPClientFactory = func(time.Duration, string, cnIPResolver, cnDialContextFunc) *http.Client {
		return &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("fixture registry unavailable")
		})}
	}
	result := (cnSpeedProbe{}).Run(context.Background(), Environment{})
	if result.Status != model.StatusSkipped || len(result.Failures) != 1 || result.Failures[0].Stage != "node_list" {
		t.Fatalf("cnspeed skip = %s/%+v", result.Status, result.Failures)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.cnspeed.summary.skipped" || len(result.Notes) != 5 {
		t.Fatalf("cnspeed skip notes/summary = %#v/%#v", result.Notes, result.SummaryMessages)
	}
}

func TestCNSpeedProducerBuildsStableFailureRowsDirectly(t *testing.T) {
	oldURL := cnNodeListURLForTest
	oldFactory := cnspeedHTTPClientFactory
	t.Cleanup(func() {
		cnNodeListURLForTest = oldURL
		cnspeedHTTPClientFactory = oldFactory
	})
	cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
	csvBody := "id,operator,province,city,host,pingUrl,downloadUrl,active\n" +
		"1,电信,上海,上海,cn1,https://8.8.8.8/ping,https://8.8.8.8/file,1\n" +
		"2,联通,北京,北京,cn2,https://8.8.4.4/ping,https://8.8.4.4/file,1\n" +
		"3,移动,广州,广州,cn3,https://1.1.1.1/ping,https://1.1.1.1/file,1\n"
	cnspeedHTTPClientFactory = func(time.Duration, string, cnIPResolver, cnDialContextFunc) *http.Client {
		return &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/file" {
				return fixtureResponse(http.StatusServiceUnavailable, io.NopCloser(strings.NewReader("fixture download failure"))), nil
			}
			body := csvBody
			if request.URL.Path == "/ping" {
				body = "pong"
			}
			return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(body))), nil
		})}
	}
	result := (cnSpeedProbe{}).Run(context.Background(), Environment{})
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 3 {
		t.Fatalf("cnspeed failure status/evidence = %s/%+v", result.Status, result.Evidence)
	}
	if len(result.Measurements) != 0 || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.cnspeed.summary.none" {
		t.Fatalf("cnspeed failure measurements/summary = %#v/%#v", result.Measurements, result.SummaryMessages)
	}
	for _, row := range result.Tables[0].Rows {
		if status, ok := row[6].Key(); !ok || status != "probe.cnspeed.status.failed" {
			t.Fatalf("cnspeed failure row status = %#v", row[6])
		}
	}
}

const fakeOoklaExecutable = `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'speedtest 1.2.3'
  exit 0
fi
printf '%s\n' '__PAYLOAD__'
exit "${FAKE_OOKLA_EXIT_CODE:-0}"
`

func runOoklaFixture(t *testing.T, payload string) model.Result {
	return runOoklaFixtureWithConfigAndExitCode(t, payload, config.Runtime{
		Exposure:     module.ExposureThirdParty,
		OoklaServers: []config.OoklaServer{{Carrier: config.OoklaCarrierTelecom, ID: 42}},
	}, "0")
}

func runOoklaFixtureWithConfig(t *testing.T, payload string, runtime config.Runtime) model.Result {
	return runOoklaFixtureWithConfigAndExitCode(t, payload, runtime, "0")
}

func runOoklaFixtureWithExitCode(t *testing.T, payload, exitCode string) model.Result {
	return runOoklaFixtureWithConfigAndExitCode(t, payload, config.Runtime{
		Exposure:     module.ExposureThirdParty,
		OoklaServers: []config.OoklaServer{{Carrier: config.OoklaCarrierTelecom, ID: 42}},
	}, exitCode)
}

func runOoklaFixtureWithConfigAndExitCode(t *testing.T, payload string, runtime config.Runtime, exitCode string) model.Result {
	t.Helper()
	writeThroughputExecutable(t, "speedtest", strings.Replace(fakeOoklaExecutable, "__PAYLOAD__", payload, 1))
	t.Setenv("FAKE_OOKLA_EXIT_CODE", exitCode)
	return (ooklaProbe{}).Run(context.Background(), Environment{Config: runtime, Catalog: testCatalog()})
}

func TestOoklaProducerBuildsStableSuccessDirectly(t *testing.T) {
	result := runOoklaFixture(t, `{"ping":{"jitter":1.5,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0,"isp":"Fixture ISP","interface":{"externalIp":"8.8.8.8"},"server":{"id":42,"name":"Example","location":"London","country":"GB"}}`)
	if result.Title != "module.ookla.title" || result.Description != "probe.ookla.description" || result.Status != model.StatusOK {
		t.Fatalf("Ookla result metadata/status = %+v", result)
	}
	if result.Methodology.Engine != "ookla-speedtest-cli" || result.Methodology.Profile != "probe.ookla.profile" {
		t.Fatalf("Ookla methodology = %+v", result.Methodology)
	}
	if len(result.Fields) < 8 || result.Fields[0].Label != "probe.ookla.field.engine" {
		t.Fatalf("Ookla fields = %#v", result.Fields)
	}
	if key, ok := result.Fields[5].Value.Key(); !ok || key != "probe.ookla.server_selection.configured" {
		t.Fatalf("Ookla server selection variant = %#v", result.Fields[5].Value)
	}
	if len(result.Measurements) != 5 || result.Measurements[0].Label != "probe.ookla.metric.latency" {
		t.Fatalf("Ookla measurements = %#v", result.Measurements)
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.ookla.table.results" {
		t.Fatalf("Ookla tables = %#v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	if carrier, ok := row[0].Key(); !ok || carrier != "probe.cnspeed.carrier.telecom" {
		t.Fatalf("Ookla carrier = %#v", row[0])
	}
	if status, ok := row[6].Key(); !ok || status != "probe.ookla.status.complete" {
		t.Fatalf("Ookla status = %#v", row[6])
	}
	if len(result.Sources) != 1 || result.Sources[0].Purpose != "probe.ookla.source.engine" || len(result.Notes) != 3 {
		t.Fatalf("Ookla sources/notes = %#v/%#v", result.Sources, result.Notes)
	}
	if len(result.Failures) != 0 || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.ookla.summary.values" {
		t.Fatalf("Ookla summary = %#v", result.SummaryMessages)
	}
}

func TestOoklaProducerSelectedServerComparisonScope(t *testing.T) {
	basePayload := `{"ping":{"jitter":1.5,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0,"server":{"id":42,"name":"Example","location":"London","country":"GB"}}`
	base := runOoklaFixture(t, basePayload)
	if len(base.Tables) != 1 || base.Methodology.Parameters["selected_servers"] == "" {
		t.Fatalf("Ookla producer omitted selected-server scope: %+v", base.Methodology.Parameters)
	}
	if got, want := base.Methodology.Parameters["selected_servers"], selectedValueJSON(base.Tables[0], 0, 1); got != want {
		t.Fatalf("Ookla selected-server JSON = %q, want %q", got, want)
	}
	assertProducerParameterScope(t, base, "server_configuration", "tool_version", "arguments", "selected_servers")
	if got, want := base.Methodology.Parameters["server_configuration"], comparisonParameterJSON([]config.OoklaServer{{Carrier: config.OoklaCarrierTelecom, ID: 42}}); got != want {
		t.Fatalf("Ookla server configuration scope = %q, want %q", got, want)
	}
	argumentsField := ""
	for _, field := range base.Fields {
		if field.Key == "arguments" {
			argumentsField = field.Value.Text()
			break
		}
	}
	if argumentsField == "" || base.Methodology.Parameters["arguments"] != argumentsField {
		t.Fatalf("Ookla argument scope = %q, field arguments = %q", base.Methodology.Parameters["arguments"], argumentsField)
	}

	changedMetrics := runOoklaFixture(t, `{"ping":{"jitter":9,"latency":99},"download":{"bandwidth":25000000},"upload":{"bandwidth":5000000},"packetLoss":10,"server":{"id":42,"name":"Example","location":"London","country":"GB"}}`)
	if base.Methodology.Parameters["selected_servers"] != changedMetrics.Methodology.Parameters["selected_servers"] {
		t.Fatal("Ookla metric/result columns changed selected-server comparison scope")
	}
	changedStatus := runOoklaFixture(t, `{"ping":{"latency":8.5},"server":{"id":42,"name":"Example","location":"London","country":"GB"}}`)
	if base.Methodology.Parameters["selected_servers"] != changedStatus.Methodology.Parameters["selected_servers"] {
		t.Fatal("Ookla status change changed selected-server comparison scope")
	}

	changedServer := runOoklaFixture(t, `{"ping":{"jitter":1.5,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0,"server":{"id":42,"name":"Other","location":"Paris","country":"FR"}}`)
	if base.Methodology.Parameters["selected_servers"] == changedServer.Methodology.Parameters["selected_servers"] {
		t.Fatal("Ookla selected server identity change was omitted from comparison scope")
	}

	selected := selectedValueRows(base.Tables[0].Rows, 0, 1)
	rawCarrier := make([][]model.Value, len(selected))
	for index, row := range selected {
		rawCarrier[index] = append([]model.Value(nil), row...)
	}
	rawCarrier[0][0] = model.RawValue(selected[0][0].Text())
	if comparisonParameterJSON(selected) == comparisonParameterJSON(rawCarrier) {
		t.Fatal("Ookla selected-server JSON ignored a raw/key Value tag change")
	}
}

func TestOoklaProducerBuildsStablePartialAndParseStatesDirectly(t *testing.T) {
	partial := runOoklaFixture(t, `{"ping":{"latency":8.5}}`)
	if partial.Status != model.StatusWarning || len(partial.Measurements) != 1 || partial.SummaryMessages[0].Key != "probe.ookla.summary.values" {
		t.Fatalf("Ookla partial = %+v", partial)
	}
	if len(partial.Failures) != 1 || partial.Failures[0].Category != model.FailureParse || partial.Failures[0].Stage != "validate" || partial.Failures[0].Target != config.OoklaCarrierTelecom || partial.Failures[0].Message != "fields_incomplete=download.bandwidth,upload.bandwidth" {
		t.Fatalf("Ookla partial structured failure = %+v", partial.Failures)
	}
	if len(partial.SummaryMessages) != 2 || partial.SummaryMessages[1].Key != "probe.ookla.summary.warn.incomplete" || len(partial.SummaryMessages[1].Args) != 2 || partial.SummaryMessages[1].Args[1] != "download.bandwidth,upload.bandwidth" {
		t.Fatalf("Ookla partial warning summary = %+v", partial.SummaryMessages)
	}
	if field := ooklaField(partial, "incomplete_fields_telecom"); field.Value.Text() != "download.bandwidth,upload.bandwidth" {
		t.Fatalf("Ookla partial incomplete field = %#v", field)
	}
	if status, ok := partial.Tables[0].Rows[0][6].Key(); !ok || status != "probe.ookla.status.partial" {
		t.Fatalf("Ookla partial row status = %#v", partial.Tables[0].Rows[0][6])
	}
	partialJSON, err := json.Marshal(partial)
	if err != nil || !strings.Contains(string(partialJSON), `"key":"probe.ookla.summary.warn.incomplete"`) || strings.Contains(string(partialJSON), "测速字段不完整") {
		t.Fatalf("Ookla partial canonical warning = %s, err=%v", partialJSON, err)
	}

	parsed := runOoklaFixture(t, "{not-json")
	if parsed.Status != model.StatusWarning || len(parsed.Failures) == 0 || parsed.Failures[0].Stage != "parse" {
		t.Fatalf("Ookla parse failure = %+v", parsed)
	}
	if status, ok := parsed.Tables[0].Rows[0][6].Key(); !ok || status != "probe.ookla.status.unparsed" {
		t.Fatalf("Ookla parse row status = %#v", parsed.Tables[0].Rows[0][6])
	}
	if parsed.SummaryMessages[0].Key != "probe.ookla.summary.no_metric" {
		t.Fatalf("Ookla parse summary = %#v", parsed.SummaryMessages)
	}
	if len(parsed.SummaryMessages) != 3 || parsed.SummaryMessages[1].Key != "probe.ookla.summary.warn.unparsed" || parsed.SummaryMessages[2].Key != "probe.ookla.summary.warn.no_result" {
		t.Fatalf("Ookla parse warning summary = %#v", parsed.SummaryMessages)
	}

	empty := runOoklaFixture(t, `{}`)
	if empty.Status != model.StatusWarning || len(empty.Measurements) != 0 || len(empty.Failures) != 1 || empty.Failures[0].Stage != "validate" {
		t.Fatalf("Ookla empty JSON warning = %+v", empty)
	}
	if len(empty.SummaryMessages) != 3 || empty.SummaryMessages[1].Key != "probe.ookla.summary.warn.incomplete" || empty.SummaryMessages[2].Key != "probe.ookla.summary.warn.no_result" {
		t.Fatalf("Ookla empty JSON warning summary = %#v", empty.SummaryMessages)
	}
}

func TestOoklaProducerReportsIPFamilyMismatchStructurally(t *testing.T) {
	result := runOoklaFixtureWithConfig(t, `{"ping":{"jitter":1.5,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0,"interface":{"externalIp":"2001:db8::9"},"server":{"id":42,"name":"Example"}}`, config.Runtime{
		Exposure:     module.ExposureThirdParty,
		IPVersion:    config.IPVersion4,
		OoklaServers: []config.OoklaServer{{Carrier: config.OoklaCarrierTelecom, ID: 42}},
	})
	if result.Status != model.StatusWarning || len(result.Measurements) != 5 || len(result.Failures) != 0 {
		t.Fatalf("Ookla IP family mismatch status/data = %+v", result)
	}
	if len(result.SummaryMessages) != 2 || result.SummaryMessages[1].Key != "probe.ookla.summary.warn.ip_family" || len(result.SummaryMessages[1].Args) != 3 || result.SummaryMessages[1].Args[1] != "4" || result.SummaryMessages[1].Args[2] != "6" {
		t.Fatalf("Ookla IP family mismatch summary = %#v", result.SummaryMessages)
	}
	if field := ooklaField(result, "ip_version_mismatch_telecom"); field.Value.Text() != "requested=4;returned=6" {
		t.Fatalf("Ookla IP family mismatch field = %#v", field)
	}
	if status, ok := result.Tables[0].Rows[0][6].Key(); !ok || status != "probe.ookla.status.ip_family" {
		t.Fatalf("Ookla IP family mismatch row status = %#v", result.Tables[0].Rows[0][6])
	}
}

func TestOoklaProducerReportsExecutableFailureStructurally(t *testing.T) {
	result := runOoklaFixtureWithExitCode(t, `{"ping":{"jitter":1.5,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0,"server":{"id":42,"name":"Example"}}`, "7")
	if result.Status != model.StatusWarning || len(result.Measurements) != 5 {
		t.Fatalf("Ookla executable failure status/data = %+v", result)
	}
	if len(result.Failures) != 1 || result.Failures[0].Stage != "execute" || result.Failures[0].Target != config.OoklaCarrierTelecom || result.Failures[0].Message == "" {
		t.Fatalf("Ookla executable failure = %+v", result.Failures)
	}
	if len(result.SummaryMessages) != 2 || result.SummaryMessages[1].Key != "probe.ookla.summary.warn.execution" {
		t.Fatalf("Ookla executable failure summary = %#v", result.SummaryMessages)
	}
	if status, ok := result.Tables[0].Rows[0][6].Key(); !ok || status != "probe.ookla.status.partial" {
		t.Fatalf("Ookla executable failure row status = %#v", result.Tables[0].Rows[0][6])
	}
}

func ooklaField(result model.Result, key string) model.Field {
	for _, field := range result.Fields {
		if field.Key == key {
			return field
		}
	}
	return model.Field{}
}

func TestOoklaStatusKeyUsesParsedResult(t *testing.T) {
	complete, err := parseOoklaJSON([]byte(`{"ping":{"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := ooklaStatusKey(complete, "fixture", nil); got != "probe.ookla.status.complete" {
		t.Fatalf("complete status = %q", got)
	}
	partial, err := parseOoklaJSON([]byte(`{"ping":{"latency":8.5}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := ooklaStatusKey(partial, "fixture", nil); got != "probe.ookla.status.partial" {
		t.Fatalf("partial status = %q", got)
	}
	if got := ooklaStatusKey(complete, "fixture", []model.Failure{{Stage: "parse", Target: "fixture"}}); got != "probe.ookla.status.unparsed" {
		t.Fatalf("parse failure status = %q", got)
	}
	if got := ooklaStatusKey(complete, "fixture", []model.Failure{{Stage: "execute", Target: "fixture"}}); got != "probe.ookla.status.partial" {
		t.Fatalf("execute failure status = %q", got)
	}
}

func TestOoklaProducerBuildsStableExposureAndToolStatesDirectly(t *testing.T) {
	t.Run("exposure denied", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		t.Setenv(ToolBinEnv, "")
		result := (ooklaProbe{}).Run(context.Background(), Environment{Config: config.Runtime{Exposure: module.ExposureLocal}, Catalog: testCatalog()})
		if result.Status != model.StatusSkipped || len(result.Fields) != 2 || result.SummaryMessages[0].Key != "probe.ookla.summary.skipped" {
			t.Fatalf("Ookla exposure skip = %+v", result)
		}
		reason, reasonOK := result.Fields[0].Value.Key()
		nextStep, nextStepOK := result.Fields[1].Value.Key()
		if result.Fields[0].Label != "probe.ookla.field.skip_reason" || !reasonOK || reason != "probe.ookla.skip_reason.exposure_denied" || !nextStepOK || nextStep != "probe.ookla.next_step.rerun_with_more_exposure" {
			t.Fatalf("Ookla exposure fields = %#v", result.Fields)
		}
	})
	t.Run("tool missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		t.Setenv(ToolBinEnv, "")
		result := (ooklaProbe{}).Run(context.Background(), Environment{Config: config.Runtime{Exposure: module.ExposureThirdParty}, Catalog: testCatalog()})
		if result.Status != model.StatusSkipped || len(result.Failures) != 1 || result.Failures[0].Stage != "tool_lookup" {
			t.Fatalf("Ookla tool skip = %+v", result)
		}
		reason, reasonOK := result.Fields[0].Value.Key()
		nextStep, nextStepOK := result.Fields[1].Value.Key()
		if len(result.Fields) != 2 || !reasonOK || reason != "probe.ookla.skip_reason.tool_unavailable" || !nextStepOK || nextStep != "probe.ookla.next_step.install_official_client" {
			t.Fatalf("Ookla tool fields = %#v", result.Fields)
		}
	})
}
