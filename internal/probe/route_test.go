//go:build linux

package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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
	backend := traceBackend{Name: traceNextTraceEngineName, Path: path, Adapter: traceNextTraceAdapter}
	cause := errors.New("fixture NextTrace cancellation cause")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	run := runTraceCommandForFamily(ctx, backend, "complete", routeSnapshotHops, config.IPVersionAuto)
	if !errors.Is(run.Err, cause) || !errors.Is(run.Err, context.Canceled) {
		t.Fatalf("cancelled NextTrace command = stdout:%q stderr:%q err:%v", run.Stdout, run.Stderr, run.Err)
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

func TestNextTraceSeparatesAndBoundsCommandStreams(t *testing.T) {
	path := writeRouteFixtureBinary(t)
	backend := traceBackend{Name: traceNextTraceEngineName, Path: path, Adapter: traceNextTraceAdapter}

	run := runTraceCommandForFamily(context.Background(), backend, "stderr", routeSnapshotHops, config.IPVersionAuto)
	if run.Err != nil || string(run.Stdout) != routeCompleteFixtureOutput || string(run.Stderr) == "" || !run.Parsed {
		t.Fatalf("stderr fixture = stdout:%q stderr:%q parsed:%t err:%v", run.Stdout, run.Stderr, run.Parsed, run.Err)
	}
	if slots, visible, _, ok := traceHopSummary(run.Trace); !ok || slots != 1 || visible != 1 {
		t.Fatalf("stderr route parse = slots:%d visible:%d parsed:%v", slots, visible, ok)
	}
	row := runBacktraceTarget(context.Background(), backend, config.Endpoint{Name: "stderr", Address: "stderr"}, config.IPVersionAuto)
	if row.Err != nil || string(row.RawStdout) != routeCompleteFixtureOutput || string(row.RawStderr) == "" || len(row.Details) != 1 {
		t.Fatalf("stderr backtrace = stdout:%q stderr:%q details:%d err:%v", row.RawStdout, row.RawStderr, len(row.Details), row.Err)
	}
	run = runTraceCommandForFamily(context.Background(), backend, "stderr_failure", routeSnapshotHops, config.IPVersionAuto)
	var exitErr *exec.ExitError
	if !errors.As(run.Err, &exitErr) || !strings.Contains(string(run.Stderr), "route fixture diagnostic") || string(run.Stdout) != routeCompleteFixtureOutput || !run.Parsed {
		t.Fatalf("stderr failure = stdout:%q stderr:%q parsed:%t err:%v", run.Stdout, run.Stderr, run.Parsed, run.Err)
	}

	const overflowTimeout = 2 * time.Second
	runWithOverflowContext := func(run func(context.Context)) {
		overflowContext, overflowCancel := context.WithTimeout(context.Background(), overflowTimeout)
		defer overflowCancel()
		run(overflowContext)
	}
	runWithOverflowContext(func(overflowContext context.Context) {
		started := time.Now()
		run = runTraceCommandForFamily(overflowContext, backend, "oversized", routeSnapshotHops, config.IPVersionAuto)
		var overflowExitErr *exec.ExitError
		if !errors.Is(run.Err, errProbeCommandOutputLimit) || !errors.As(run.Err, &overflowExitErr) || errors.Is(run.Err, context.Canceled) || errors.Is(run.Err, context.DeadlineExceeded) || run.Stdout != nil || run.Parsed || time.Since(started) >= overflowTimeout {
			t.Fatalf("oversized stdout = output_nil:%v err:%v elapsed:%s", run.Stdout == nil, run.Err, time.Since(started))
		}
	})
	runWithOverflowContext(func(overflowContext context.Context) {
		row := runBacktraceTarget(overflowContext, backend, config.Endpoint{Name: "oversized", Address: "oversized"}, config.IPVersionAuto)
		var overflowExitErr *exec.ExitError
		if !errors.Is(row.Err, errProbeCommandOutputLimit) || !errors.As(row.Err, &overflowExitErr) || errors.Is(row.Err, context.Canceled) || errors.Is(row.Err, context.DeadlineExceeded) || row.RawStdout != "" || row.RawStderr != "" || len(row.Details) != 0 || len(row.Hops) != 0 || len(row.Hits) != 0 {
			t.Fatalf("oversized backtrace = stdout:%d stderr:%d details:%d hops:%d hits:%d err:%v", len(row.RawStdout), len(row.RawStderr), len(row.Details), len(row.Hops), len(row.Hits), row.Err)
		}
	})
	runWithOverflowContext(func(overflowContext context.Context) {
		result := (routeProbe{}).Run(overflowContext, routeTestEnvironment([]config.Endpoint{{Name: "Oversized", Address: "oversized"}}, config.IPVersionAuto))
		if len(result.Measurements) != 0 || result.Evidence == nil || result.Evidence.Valid != 0 || len(result.Failures) != 1 {
			t.Fatalf("oversized route result = measurements:%d evidence:%+v failures:%+v", len(result.Measurements), result.Evidence, result.Failures)
		}
	})
	runWithOverflowContext(func(overflowContext context.Context) {
		run = runTraceCommandForFamily(overflowContext, backend, "stderr_oversized", routeSnapshotHops, config.IPVersionAuto)
		var overflowExitErr *exec.ExitError
		if !errors.Is(run.Err, errProbeCommandOutputLimit) || !errors.As(run.Err, &overflowExitErr) || errors.Is(run.Err, context.Canceled) || errors.Is(run.Err, context.DeadlineExceeded) || run.Stdout != nil || run.Stderr == nil || len(run.Stderr) > probeCommandStderrLimit || run.Parsed {
			t.Fatalf("oversized stderr = stdout_nil:%v stderr:%d parsed:%v err:%v", run.Stdout == nil, len(run.Stderr), run.Parsed, run.Err)
		}
	})
	runWithOverflowContext(func(overflowContext context.Context) {
		row := runBacktraceTarget(overflowContext, backend, config.Endpoint{Name: "stderr_oversized", Address: "stderr_oversized"}, config.IPVersionAuto)
		var overflowExitErr *exec.ExitError
		if !errors.Is(row.Err, errProbeCommandOutputLimit) || !errors.As(row.Err, &overflowExitErr) || errors.Is(row.Err, context.Canceled) || errors.Is(row.Err, context.DeadlineExceeded) || row.RawStdout != "" || row.RawStderr != "" || len(row.Details) != 0 || len(row.Hops) != 0 || len(row.Hits) != 0 {
			t.Fatalf("oversized stderr backtrace = stdout:%d stderr:%d details:%d hops:%d hits:%d err:%v", len(row.RawStdout), len(row.RawStderr), len(row.Details), len(row.Hops), len(row.Hits), row.Err)
		}
	})
}

const (
	routeCompleteFixtureOutput = `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}]],"provider":"原始汉字"}`
	routePartialFixtureOutput  = `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}]]}`
)

func TestRouteSummaryArgumentsAndFailures(t *testing.T) {
	output := `{"Hops":[[{"Address":{"IP":"203.0.113.1"}}],[[]]]}`
	trace, err := parseNextTraceCanonical(output, config.IPVersionAuto, "203.0.113.1")
	if err != nil {
		t.Fatalf("NextTrace canonical parse = %v", err)
	}
	slots, visible, timeouts, ok := traceHopSummary(trace)
	if !ok || slots != 2 || visible != 1 || timeouts != 1 {
		t.Fatalf("NextTrace summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
	}
	if _, err := parseNextTraceCanonical("{}", config.IPVersionAuto, "203.0.113.1"); err == nil {
		t.Fatal("empty route output parsed successfully")
	}
	if args := nextTraceCommandArgsForFamily("203.0.113.1", routeSnapshotHops, config.IPVersion4); len(args) == 0 || args[0] != "-4" || args[len(args)-1] != "203.0.113.1" {
		t.Fatalf("IPv4 route args = %v", args)
	}
	if args := nextTraceCommandArgsForFamily("2001:db8::1", routeSnapshotHops, config.IPVersion6); len(args) == 0 || args[0] != "-6" || args[len(args)-1] != "2001:db8::1" {
		t.Fatalf("IPv6 route args = %v", args)
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
	assertProducerParameterScope(t, result, "ip_version", "targets", "max_hops", "tool_version", "adapter", "arguments")
	parameters := result.Methodology.Parameters
	if parameters["ip_version"] != config.IPVersionAuto || parameters["targets"] != comparisonParameterJSON(targets) || parameters["max_hops"] != strconv.Itoa(routeSnapshotHops) || parameters["tool_version"] != "fixture-nexttrace" || parameters["adapter"] != traceNextTraceAdapter {
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
		if measurement.Method != traceHopSummaryMethod {
			t.Fatalf("route measurement method = %q, want %q", measurement.Method, traceHopSummaryMethod)
		}
	}
	wantBlocks := []struct {
		title, language, content string
	}{
		{title: traceRawOutputTitle, language: "json", content: routeCompleteFixtureOutput},
		{title: traceNormalizedJSONTitle, language: "json", content: `{"engine":"nexttrace-tiny","family":"auto","target":"complete","hops":[{"hop":1,"responded":true,"ip":"203.0.113.1"}],"adapter":"nexttrace-json-v1"}`},
		{title: traceRawOutputTitle, language: "json", content: `{"Hops":[[]]}`},
		{title: traceNormalizedJSONTitle, language: "json", content: `{"engine":"nexttrace-tiny","family":"auto","target":"zero","hops":[{"hop":1,"responded":false}],"adapter":"nexttrace-json-v1"}`},
		{title: traceRawOutputTitle, language: "json", content: `{"not_route":true}`},
		{title: traceRawOutputTitle, language: "json", content: routePartialFixtureOutput},
		{title: traceNormalizedJSONTitle, language: "json", content: `{"engine":"nexttrace-tiny","family":"auto","target":"partial","hops":[{"hop":1,"responded":true,"ip":"203.0.113.1"}],"adapter":"nexttrace-json-v1"}`},
	}
	if len(result.TextBlocks) != len(wantBlocks) {
		t.Fatalf("route raw blocks = %#v", result.TextBlocks)
	}
	for index, block := range result.TextBlocks {
		want := wantBlocks[index]
		if block.Title != want.title || block.Language != want.language || block.Content != want.content {
			t.Fatalf("route raw block %d = %#v, want %#v", index, block, want)
		}
	}
	if got := routeTestFieldValue(result, "arguments"); strings.Contains(got, "按目标协议族") || strings.ContainsAny(got, "按目标参数命令") {
		t.Fatalf("localized route arguments = %q", got)
	}
	parseFailure := routeTestFailure(result, model.FailureParse, "parse")
	if parseFailure.Message != "NextTrace output contains no hop slots" || parseFailure.Stage != "parse" || parseFailure.Target != "parse" {
		t.Fatalf("parse failure = %#v", parseFailure)
	}
	backend := detectTraceBackend(context.Background())
	traceRun := runTraceCommandForFamily(context.Background(), backend, "partial", routeSnapshotHops, config.IPVersionAuto)
	if traceRun.Err == nil {
		t.Fatal("fixture exec failure unexpectedly succeeded")
	}
	execFailure := routeTestFailure(result, model.FailureUnknown, "partial")
	if execFailure.Stage != "trace" || execFailure.Message != traceRun.Err.Error() {
		t.Fatalf("exec failure = %#v, expected original %q", execFailure, traceRun.Err.Error())
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

func TestRouteMixedFamilyArgumentsPreserveEveryVariant(t *testing.T) {
	writeRouteFixtureBinary(t)
	targets := []config.Endpoint{
		{Name: "IPv4 first", Address: "complete", Family: config.IPVersion4},
		{Name: "IPv6 second", Address: "complete", Family: config.IPVersion6},
		{Name: "IPv4 duplicate", Address: "complete", Family: config.IPVersion4},
	}
	result := (routeProbe{}).Run(context.Background(), routeTestEnvironment(targets, config.IPVersionAuto))
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != len(targets) {
		t.Fatalf("mixed-family route = status:%s evidence:%+v failures:%+v", result.Status, result.Evidence, result.Failures)
	}
	arguments := routeTestFieldValue(result, "arguments")
	if arguments == "" || strings.Contains(arguments, "按目标") || strings.Contains(arguments, "NextTrace") {
		t.Fatalf("mixed-family arguments contain non-machine prose: %q", arguments)
	}
	var variants []traceArgumentVariant
	if err := json.Unmarshal([]byte(arguments), &variants); err != nil {
		t.Fatalf("mixed-family arguments are not canonical JSON: %q: %v", arguments, err)
	}
	if len(variants) != 2 || variants[0].Family != "ipv4" || variants[1].Family != "ipv6" {
		t.Fatalf("mixed-family argument variants = %#v, want ordered ipv4/ipv6", variants)
	}
	wantIPv4 := []string{"-4", "--no-color", "--json", "-M", "--max-hops", "12", "--queries", "1", "--parallel-requests", "1", "--timeout", "1000", "<target>"}
	wantIPv6 := []string{"-6", "--no-color", "--json", "-M", "--max-hops", "12", "--queries", "1", "--parallel-requests", "1", "--timeout", "1000", "<target>"}
	if !routeTestSlicesEqual(variants[0].Args, wantIPv4) || !routeTestSlicesEqual(variants[1].Args, wantIPv6) {
		t.Fatalf("mixed-family argument arrays = %#v, want v4=%#v v6=%#v", variants, wantIPv4, wantIPv6)
	}
	if result.Methodology.Parameters["arguments"] != arguments {
		t.Fatalf("mixed-family comparison arguments = %q, field arguments = %q", result.Methodology.Parameters["arguments"], arguments)
	}
	if strings.Count(arguments, `"family":"ipv4"`) != 1 || strings.Count(arguments, `"family":"ipv6"`) != 1 {
		t.Fatalf("mixed-family arguments duplicate or omit a family: %q", arguments)
	}

	single := (routeProbe{}).Run(context.Background(), routeTestEnvironment([]config.Endpoint{
		{Name: "IPv4 only", Address: "complete", Family: config.IPVersion4},
	}, config.IPVersionAuto))
	singleArguments := routeTestFieldValue(single, "arguments")
	if want := strings.Join(wantIPv4, " "); singleArguments != want || single.Methodology.Parameters["arguments"] != want {
		t.Fatalf("single-family arguments = field:%q parameter:%q, want %q", singleArguments, single.Methodology.Parameters["arguments"], want)
	}
}

func TestRoutePreservesParserDiagnosticWithCommandFailure(t *testing.T) {
	writeRouteFixtureBinary(t)
	result := (routeProbe{}).Run(context.Background(), routeTestEnvironment([]config.Endpoint{{Name: "ParseExec", Address: "parse_exec_failure"}}, config.IPVersionAuto))
	if len(result.Failures) != 1 || result.Failures[0].Category == model.FailureParse || result.Failures[0].Stage != "trace" {
		t.Fatalf("nonzero malformed route failure = %#v", result.Failures)
	}
	if !strings.Contains(result.Failures[0].Message, "exit status 8") || !strings.Contains(result.Failures[0].Message, "NextTrace output contains no hop slots") {
		t.Fatalf("nonzero malformed route diagnostic = %q", result.Failures[0].Message)
	}
	var stderrPreserved bool
	for _, block := range result.TextBlocks {
		if block.Title == traceRawStderrTitle && strings.Contains(block.Content, "route parser diagnostic") {
			stderrPreserved = true
		}
	}
	if !stderrPreserved {
		t.Fatalf("nonzero malformed route lost stderr: %#v", result.TextBlocks)
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
		t.Setenv(ToolBinEnv, "")
		targets := []config.Endpoint{{Name: "One", Address: "203.0.113.1"}, {Name: "Two", Address: "198.51.100.1"}}
		result := (routeProbe{}).Run(context.Background(), routeTestEnvironment(targets, config.IPVersionAuto))
		if result.Status != model.StatusSkipped || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.route.summary.tool_missing" {
			t.Fatalf("tool-missing route = %#v", result)
		}
		if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 2 {
			t.Fatalf("tool-missing evidence = %#v", result.Evidence)
		}
		failure := routeTestFailure(result, model.FailureToolMissing, traceNextTraceEngineName)
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
	path := filepath.Join(root, traceNextTraceEngineName)
	if err := os.WriteFile(path, []byte("#!/route-interpreter-that-does-not-exist\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv(ToolBinEnv, root)
	backend := traceBackend{Name: traceNextTraceEngineName, Path: path, Adapter: traceNextTraceAdapter}
	traceRun := runTraceCommandForFamily(context.Background(), backend, "long", routeSnapshotHops, config.IPVersionAuto)
	if traceRun.Err == nil || len(traceRun.Err.Error()) <= 100 {
		t.Fatalf("fixture error = %v, want a long typed error", traceRun.Err)
	}
	result := (routeProbe{}).Run(context.Background(), routeTestEnvironment([]config.Endpoint{{Name: "Long", Address: "long"}}, config.IPVersionAuto))
	failure := routeTestFailure(result, model.FailureUnknown, "long")
	wantMessage := traceRun.Err.Error() + "; parser diagnostic: NextTrace output contains no hop slots"
	if failure.Message != wantMessage {
		t.Fatalf("long command error was changed: got %q want %q", failure.Message, wantMessage)
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
		markers := []string{
			i18n.T("probe.route.status.complete"),
			i18n.T("probe.route.status.no_response"),
			i18n.T("probe.route.status.failed"),
			i18n.T("probe.route.target_type.global"),
			i18n.T("probe.route.target_type.mainland_china"),
			i18n.T("probe.route.note.parse_failed"),
			i18n.T("probe.route.source.nexttrace.name"),
			i18n.T("probe.route.normalized_trace_json"),
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
	path := filepath.Join(directory, traceNextTraceEngineName)
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
		"parse_exec_failure)\n" +
		"  printf '%s' '{\"not_route\":true}'\n" +
		"  printf '%s\\n' 'route parser diagnostic' >&2\n" +
		"  exit 8\n" +
		"  ;;\n" +
		"stderr)\n" +
		"  printf '%s' '" + routeCompleteFixtureOutput + "'\n" +
		"  printf '%s\\n' 'route fixture diagnostic' >&2\n" +
		"  ;;\n" +
		"stderr_failure)\n" +
		"  printf '%s' '" + routeCompleteFixtureOutput + "'\n" +
		"  printf '%s\\n' 'route fixture diagnostic' >&2\n" +
		"  exit 9\n" +
		"  ;;\n" +
		"oversized)\n" +
		"  printf '%s' '" + routeCompleteFixtureOutput + "'\n" +
		"  chunk=' '\n" +
		"  i=0\n" +
		"  while [ \"$i\" -lt 22 ]; do chunk=$chunk$chunk; i=$((i + 1)); done\n" +
		"  printf '%s' \"$chunk\"\n" +
		"  printf '%s' beyond-limit\n" +
		"  while :; do :; done\n" +
		"  ;;\n" +
		"stderr_oversized)\n" +
		"  printf '%s' '" + routeCompleteFixtureOutput + "'\n" +
		"  chunk=' '\n" +
		"  i=0\n" +
		"  while [ \"$i\" -lt 17 ]; do chunk=$chunk$chunk; i=$((i + 1)); done\n" +
		"  printf '%s' \"$chunk\" >&2\n" +
		"  printf '%s' beyond-limit >&2\n" +
		"  while :; do :; done\n" +
		"  ;;\n" +
		"*)\n" +
		"  printf '%s' '{\"not_route\":true}'\n" +
		"  ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv(ToolBinEnv, directory)
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
