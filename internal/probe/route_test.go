package probe

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func TestNextTraceCancellationPrecedesExecuteAndParseClassification(t *testing.T) {
	path := writeRouteFixtureBinary(t)
	engine := routeEngine{Name: routeEngineTiny, Path: path}
	cause := errors.New("fixture NextTrace cancellation cause")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	output, err := runRouteCommandForFamily(ctx, engine, "complete", routeSnapshotHops, config.IPVersionAuto)
	if !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled NextTrace command = output:%q err:%v", output, err)
	}

	result := (routeProbe{}).Run(ctx, routeTestEnvironment([]config.Endpoint{{Name: "Cancelled", Address: "complete"}}, config.IPVersionAuto))
	if len(result.Failures) != 1 || result.Failures[0].Category != model.FailureCanceled || result.Failures[0].Stage != "trace" {
		t.Fatalf("cancelled NextTrace result failures = %+v", result.Failures)
	}
	for _, failure := range result.Failures {
		if failure.Stage == "parse" {
			t.Fatalf("cancelled NextTrace was classified as parse failure: %+v", result.Failures)
		}
	}
}

const (
	routeCompleteFixtureOutput = `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}]],"provider":"原始汉字"}`
	routePartialFixtureOutput  = `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}]]}`
)

func TestRouteSummaryArgumentsAndFailures(t *testing.T) {
	output := `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}],[[]]]}`
	slots, visible, timeouts, ok := routeHopSummary(routeEngineTiny, output)
	if !ok || slots != 2 || visible != 1 || timeouts != 1 {
		t.Fatalf("NextTrace summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
	}
	if _, _, _, ok := routeHopSummary("other", output); ok {
		t.Fatal("unsupported route engine parsed successfully")
	}
	if _, _, _, ok := routeHopSummary(routeEngineTiny, "{}"); ok {
		t.Fatal("empty route output parsed successfully")
	}
	if args := routeCommandArgsForFamily(routeEngine{Name: routeEngineTiny}, "203.0.113.1", routeSnapshotHops, config.IPVersion4); len(args) == 0 || args[0] != "-4" || args[len(args)-1] != "203.0.113.1" {
		t.Fatalf("IPv4 route args = %v", args)
	}
	if args := routeCommandArgsForFamily(routeEngine{Name: routeEngineTiny}, "2001:db8::1", routeSnapshotHops, config.IPVersion6); len(args) == 0 || args[0] != "-6" || args[len(args)-1] != "2001:db8::1" {
		t.Fatalf("IPv6 route args = %v", args)
	}
	if routeCommandArgsForFamily(routeEngine{Name: "other"}, "target", 12, config.IPVersion4) != nil {
		t.Fatal("unsupported route engine produced arguments")
	}
	if clean := sanitizeCommandOutput([]byte("\x1b[31mhop\x1b[0m\x00")); clean != "hop" || strings.ContainsRune(clean, '\x1b') {
		t.Fatalf("sanitized route output = %q", clean)
	}
}

func TestRouteProducerUsesMachineSemanticsAndCounters(t *testing.T) {
	writeRouteFixtureBinary(t)
	targets := []config.Endpoint{
		{Name: "Complete", Address: "complete", Kind: config.RouteTargetKindGlobal},
		{Name: "NoResponse", Address: "zero", Kind: config.RouteTargetKindMainlandChina},
		{Name: "Parse", Address: "parse", Kind: "custom-kind"},
		{Name: "ExecFailure", Address: "partial", Kind: "custom-kind"},
	}
	result := (routeProbe{}).Run(context.Background(), routeTestEnvironment(targets, config.IPVersionAuto))

	if result.Title != "module.route.title" || result.Description != "probe.route.description" {
		t.Fatalf("route presentation fields = %#v", result)
	}
	if result.Methodology.Kind != "protocol-measurement" || result.Methodology.Label != "methodology.protocol-measurement" ||
		result.Methodology.Engine != "probe.route.methodology.engine" || result.Methodology.Profile != "probe.route.profile" ||
		result.Methodology.ComparisonScope != "probe.route.comparison_scope" {
		t.Fatalf("route methodology = %#v", result.Methodology)
	}
	assertProducerParameterScope(t, result, "ip_version", "targets", "max_hops", "tool_version", "arguments")
	parameters := result.Methodology.Parameters
	if parameters["ip_version"] != config.IPVersionAuto || parameters["targets"] != comparisonParameterJSON(targets) || parameters["max_hops"] != strconv.Itoa(routeSnapshotHops) || parameters["tool_version"] != "fixture-nexttrace" {
		t.Fatalf("route comparison parameters = %v", parameters)
	}
	arguments := routeTestFieldValue(result, "arguments")
	if arguments == "" || parameters["arguments"] != arguments {
		t.Fatalf("route argument scope = %q, field arguments = %q", parameters["arguments"], arguments)
	}
	wantFieldLabels := map[string]string{
		"engine":    "probe.route.field.engine",
		"version":   "probe.route.field.version",
		"arguments": "probe.route.field.arguments",
	}
	if len(result.Fields) != len(wantFieldLabels) {
		t.Fatalf("route fields = %#v", result.Fields)
	}
	for _, field := range result.Fields {
		if field.Label != wantFieldLabels[field.Key] {
			t.Fatalf("route field label = %#v", field)
		}
	}
	if result.Status != model.StatusWarning {
		t.Fatalf("route status = %q, want warning", result.Status)
	}
	if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 4 {
		t.Fatalf("route evidence = %#v, want valid=2 expected=4", result.Evidence)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.route.summary.values" ||
		!routeTestSlicesEqual(result.SummaryMessages[0].Args, []string{"2", "4"}) {
		t.Fatalf("route summary messages = %#v", result.SummaryMessages)
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 4 {
		t.Fatalf("route table = %#v", result.Tables)
	}
	wantColumns := []string{"probe.route.column.target", "probe.route.column.target_type", "probe.route.column.status", "probe.route.column.probed_hops", "probe.route.column.visible_hops", "probe.route.column.timeout_hops", "probe.route.column.duration"}
	if result.Tables[0].Title != "probe.route.table.summary" || !routeTestSlicesEqual(routeTestColumnLabels(result.Tables[0].Columns), wantColumns) || result.Tables[0].RowIdentity != "" {
		t.Fatalf("route table shape = %#v", result.Tables[0])
	}
	if len(result.Sources) != 1 || result.Sources[0].Name != "probe.route.source.nexttrace.name" || result.Sources[0].Purpose != "probe.route.source.nexttrace" {
		t.Fatalf("route source shape = %#v", result.Sources)
	}
	wantNotes := []string{"probe.route.note.forward_path", "probe.route.note.execution", "probe.route.note.json", "probe.route.note.parse_failed"}
	if !routeTestSlicesEqual(result.Notes, wantNotes) {
		t.Fatalf("route notes = %#v", result.Notes)
	}
	wantStatuses := []string{routeStatusComplete, routeStatusNoResponse, routeStatusParseFailed, routeStatusFailed}
	wantKinds := []string{"probe.route.target_type.global", "probe.route.target_type.mainland_china", "custom-kind", "custom-kind"}
	for index, row := range result.Tables[0].Rows {
		if len(row) < 3 || row[2].Text() != wantStatuses[index] || row[1].Text() != wantKinds[index] {
			t.Fatalf("route row %d = %#v, want status=%q kind=%q", index, row, wantStatuses[index], wantKinds[index])
		}
		if strings.ContainsAny(row[2].Text(), "完成失败无响应解析") {
			t.Fatalf("route row %d contains display status: %#v", index, row)
		}
		if _, ok := row[2].Key(); !ok {
			t.Fatalf("route status is not a tagged key: %#v", row[2])
		}
	}
	if len(result.Measurements) != 12 {
		t.Fatalf("parsed traces measurements = %d, want 12", len(result.Measurements))
	}
	for _, measurement := range result.Measurements {
		if !strings.HasPrefix(measurement.Label, "probe.route.metric.") || strings.ContainsAny(measurement.Label, "完成失败无响应探测可见超时追踪") {
			t.Fatalf("non-machine route measurement label = %#v", measurement)
		}
	}
	wantBlocks := []string{routeCompleteFixtureOutput, `{"Hops":[[]]}`, `{"not_route":true}`, routePartialFixtureOutput}
	if len(result.TextBlocks) != len(wantBlocks) {
		t.Fatalf("route raw blocks = %#v", result.TextBlocks)
	}
	for index, block := range result.TextBlocks {
		if block.Title != "probe.route.raw_output" || block.Language != "json" || block.Content != wantBlocks[index] {
			t.Fatalf("route raw block %d = %#v, want %q", index, block, wantBlocks[index])
		}
	}
	if got := routeTestFieldValue(result, "arguments"); strings.Contains(got, "按目标协议族") || strings.ContainsAny(got, "按目标参数命令") {
		t.Fatalf("localized route arguments = %q", got)
	}
	parseFailure := routeTestFailure(result, model.FailureParse, "parse")
	if parseFailure.Message != "" || parseFailure.Stage != "parse" || parseFailure.Target != "parse" {
		t.Fatalf("parse failure = %#v", parseFailure)
	}
	engine := detectRouteEngine(context.Background())
	_, expectedError := runRouteCommandForFamily(context.Background(), engine, "partial", routeSnapshotHops, config.IPVersionAuto)
	if expectedError == nil {
		t.Fatal("fixture exec failure unexpectedly succeeded")
	}
	execFailure := routeTestFailure(result, model.FailureUnknown, "partial")
	if execFailure.Stage != "trace" || execFailure.Message != expectedError.Error() {
		t.Fatalf("exec failure = %#v, expected original %q", execFailure, expectedError.Error())
	}
	complete := (routeProbe{}).Run(context.Background(), routeTestEnvironment([]config.Endpoint{{Name: "Complete", Address: "complete", Kind: config.RouteTargetKindGlobal}}, config.IPVersionAuto))
	if complete.Status != model.StatusOK || complete.Evidence == nil || complete.Evidence.Valid != 1 || complete.Evidence.Expected != 1 ||
		len(complete.SummaryMessages) != 1 || !routeTestSlicesEqual(complete.SummaryMessages[0].Args, []string{"1", "1"}) {
		t.Fatalf("all-complete route = %#v", complete)
	}
	noResponse := (routeProbe{}).Run(context.Background(), routeTestEnvironment([]config.Endpoint{{Name: "NoResponse", Address: "zero", Kind: config.RouteTargetKindMainlandChina}}, config.IPVersionAuto))
	if noResponse.Status != model.StatusWarning || noResponse.Evidence == nil || noResponse.Evidence.Valid != 1 || noResponse.Evidence.Expected != 1 ||
		len(noResponse.SummaryMessages) != 1 || !routeTestSlicesEqual(noResponse.SummaryMessages[0].Args, []string{"1", "1"}) {
		t.Fatalf("no-response-only route = %#v", noResponse)
	}
}

func TestRouteDefaultsUseMachineTargetKinds(t *testing.T) {
	runtime, err := config.Defaults(testCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.RouteTargets) != 3 || runtime.RouteTargets[0].Kind != config.RouteTargetKindGlobal || runtime.RouteTargets[1].Kind != config.RouteTargetKindGlobal || runtime.RouteTargets[2].Kind != config.RouteTargetKindMainlandChina {
		t.Fatalf("default route target kinds = %#v", runtime.RouteTargets)
	}
}

func TestRouteProducerSkipReasonsAreStructured(t *testing.T) {
	t.Run("tool missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		targets := []config.Endpoint{{Name: "One", Address: "203.0.113.1"}, {Name: "Two", Address: "198.51.100.1"}}
		result := (routeProbe{}).Run(context.Background(), routeTestEnvironment(targets, config.IPVersionAuto))
		if result.Status != model.StatusSkipped || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.route.summary.tool_missing" {
			t.Fatalf("tool-missing route = %#v", result)
		}
		if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 2 {
			t.Fatalf("tool-missing evidence = %#v", result.Evidence)
		}
		failure := routeTestFailure(result, model.FailureToolMissing, routeEngineTiny)
		if failure.Message != "" || failure.Stage != "tool_lookup" {
			t.Fatalf("tool-missing failure = %#v", failure)
		}
	})

	t.Run("no matching family", func(t *testing.T) {
		writeRouteFixtureBinary(t)
		targets := []config.Endpoint{{Name: "IPv4", Address: "203.0.113.1"}}
		result := (routeProbe{}).Run(context.Background(), routeTestEnvironment(targets, config.IPVersion6))
		if result.Status != model.StatusSkipped || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.route.summary.no_targets" {
			t.Fatalf("no-target route = %#v", result)
		}
		if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 0 || len(result.Failures) != 0 {
			t.Fatalf("no-target evidence/failures = %#v/%#v", result.Evidence, result.Failures)
		}
	})
}

func TestRouteProducerPreservesLongTypedCommandError(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("route-error-path-", 12))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, routeEngineTiny)
	if err := os.WriteFile(path, []byte("#!/route-interpreter-that-does-not-exist\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	engine := routeEngine{Name: routeEngineTiny, Path: path}
	_, expectedError := runRouteCommandForFamily(context.Background(), engine, "long", routeSnapshotHops, config.IPVersionAuto)
	if expectedError == nil || len(expectedError.Error()) <= 100 {
		t.Fatalf("fixture error = %v, want a long typed error", expectedError)
	}
	result := (routeProbe{}).Run(context.Background(), routeTestEnvironment([]config.Endpoint{{Name: "Long", Address: "long"}}, config.IPVersionAuto))
	failure := routeTestFailure(result, model.FailureUnknown, "long")
	if failure.Message != expectedError.Error() {
		t.Fatalf("long command error was changed: got %q want %q", failure.Message, expectedError.Error())
	}
}

func TestRouteReportRendersBilingualWithoutMutatingCanonicalJSON(t *testing.T) {
	writeRouteFixtureBinary(t)
	targets := []config.Endpoint{
		{Name: "Complete", Address: "complete", Kind: config.RouteTargetKindGlobal},
		{Name: "NoResponse", Address: "zero", Kind: config.RouteTargetKindMainlandChina},
		{Name: "Parse", Address: "parse", Kind: "custom-kind"},
		{Name: "ExecFailure", Address: "partial", Kind: "custom-kind"},
	}
	result := (routeProbe{}).Run(context.Background(), routeTestEnvironment(targets, config.IPVersionAuto))
	const rawSentinel = "原始汉字"
	data := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: buildinfo.Name, Version: "test", Commit: "route"},
		Run:           model.RunInfo{ID: "route-test", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), CompletedAt: time.Unix(1, 0).UTC(), DurationMS: 1, Exposure: "local", Requested: []string{"route"}, OutputFormats: []string{"json", "md", "html"}},
		Summary:       model.Summary{Status: result.Status, OK: 1},
		Results:       []model.Result{result},
	}
	canonicalBefore, err := report.JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		textOutput := report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 110})
		markdownOutput := report.Markdown(data, nil)
		htmlBytes, err := report.HTML(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		markers := []string{"完成", "无响应", "NextTrace 解析失败", "部分/失败", "全球", "中国大陆", "一个或多个路由目标的 NextTrace 输出无法解析。"}
		if language == i18n.LangEN {
			markers = []string{"Complete", "No response", "NextTrace parse failed", "Partial/failed", "Global", "Mainland China", "NextTrace output for one or more route targets could not be parsed."}
		}
		for format, output := range map[string]string{"text": textOutput, "markdown": markdownOutput, "html": string(htmlBytes)} {
			if !strings.Contains(output, rawSentinel) || strings.Contains(output, "probe.route.") || strings.Contains(output, "module.route.title") || strings.Contains(output, "%!") {
				t.Fatalf("route %s %s output leaked stable keys or raw data:\n%s", language, format, output)
			}
			for _, marker := range markers {
				if !strings.Contains(output, marker) {
					t.Fatalf("route %s %s output missing %q:\n%s", language, format, marker, output)
				}
			}
			if language == i18n.LangEN && routeTestHasHan(strings.ReplaceAll(output, rawSentinel, "")) {
				t.Fatalf("route English %s output contains ECS-owned Han characters:\n%s", format, output)
			}
		}
		canonicalAfter, err := report.JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(canonicalBefore, canonicalAfter) {
			t.Fatalf("route render mutated canonical JSON for %s", language)
		}
	}
}

func writeRouteFixtureBinary(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, routeEngineTiny)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  printf '%s\\n' 'fixture-nexttrace'\n" +
		"  exit 0\n" +
		"fi\n" +
		"target=\"\"\n" +
		"for arg do target=\"$arg\"; done\n" +
		"case \"$target\" in\n" +
		"complete|long)\n" +
		"  printf '%s' '" + routeCompleteFixtureOutput + "'\n" +
		"  ;;\n" +
		"zero)\n" +
		"  printf '%s' '{\"Hops\":[[]]}'\n" +
		"  ;;\n" +
		"parse)\n" +
		"  printf '%s' '{\"not_route\":true}'\n" +
		"  ;;\n" +
		"partial)\n" +
		"  printf '%s' '" + routePartialFixtureOutput + "'\n" +
		"  exit 7\n" +
		"  ;;\n" +
		"*)\n" +
		"  printf '%s' '{\"not_route\":true}'\n" +
		"  ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	return path
}

func routeTestEnvironment(targets []config.Endpoint, ipVersion string) Environment {
	return Environment{Config: config.Runtime{RouteTargets: targets, IPVersion: ipVersion}}
}

func routeTestFailure(result model.Result, category model.FailureCategory, target string) model.Failure {
	for _, failure := range result.Failures {
		if failure.Category == category && failure.Target == target {
			return failure
		}
	}
	return model.Failure{Category: "missing", Target: target}
}

func routeTestFieldValue(result model.Result, key string) string {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value.Text()
		}
	}
	return ""
}

func routeTestSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func routeTestColumnLabels(columns []model.TableColumn) []string {
	labels := make([]string, len(columns))
	for index, column := range columns {
		labels[index] = column.Label
	}
	return labels
}

func routeTestHasHan(value string) bool {
	for _, runeValue := range value {
		if unicode.Is(unicode.Han, runeValue) {
			return true
		}
	}
	return false
}
