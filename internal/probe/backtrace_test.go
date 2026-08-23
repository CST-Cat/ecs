package probe

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
)

func TestNextTraceParsesAndClassifiesRepresentativeHop(t *testing.T) {
	output := `{"Hops":[[{"Success":true,"Address":{"IP":"59.43.130.22"},"RTT":3500000,"Geo":{"asnumber":"4809","owner":"China Telecom","country":"CN","prov":"Shanghai"}}],[[]]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 2 {
		t.Fatalf("nexttrace details = %+v, ok=%v", details, ok)
	}
	if details[0].IP != "59.43.130.22" || details[0].ASN != "AS4809" || details[0].Latency != "3.5 ms" || details[0].Network != "China Telecom" || details[0].Location != "CN / Shanghai" || details[0].Status != "probe.backtrace.hop.responded" || details[1].Status != "probe.backtrace.hop.no_response" || details[1].IP != "" {
		t.Fatalf("parsed hops = %+v", details)
	}

	hits := matchRouteSignatures(extractTraceHops(routeEngineTiny, output))
	best, ok := bestBacktraceHit(hits, config.BacktraceCarrierTelecom)
	if !ok || best.Signature.Code != "CN2" {
		t.Fatalf("classified route = %+v, ok=%v", best, ok)
	}
	annotateBacktraceDetails(details)
	if details[0].Network != "China Telecom" {
		t.Fatalf("raw provider network was overwritten: %+v", details[0])
	}
}

func TestNextTraceParserRejectsEmptyOutput(t *testing.T) {
	details, ok := extractNextTraceDetails(`{"Hops":[]}`)
	if ok || len(details) != 0 {
		t.Fatalf("empty nexttrace output = %+v, ok=%v", details, ok)
	}
}

func TestBacktraceChoosesBestCarrierAndFiniteLineReasons(t *testing.T) {
	hits := matchRouteSignatures([]string{"202.97.0.1", "59.43.0.1", "219.158.0.1"})
	best, ok := bestBacktraceHit(hits, config.BacktraceCarrierTelecom)
	if !ok || best.Signature.Code != "CN2" || best.Hop != 2 || backtraceLineKey(best, hits) != backtraceLineTelecomCN2GT {
		t.Fatalf("best backtrace hit = %+v, line=%q", best, backtraceLineKey(best, hits))
	}
	if _, ok := bestBacktraceHit(hits, config.BacktraceCarrierMobile); ok {
		t.Fatal("foreign route was accepted for an unrelated carrier")
	}

	tests := []struct {
		name string
		row  backtraceRow
		want string
	}{
		{name: "foreign", row: backtraceRow{Target: config.Endpoint{Kind: config.BacktraceCarrierTelecom}, Hits: []backtraceHit{{Signature: routeSignature{Carrier: config.BacktraceCarrierMobile}}}, Hops: []string{"223.120.0.1"}}, want: backtraceReasonForeignOnly},
		{name: "unknown", row: backtraceRow{Target: config.Endpoint{Kind: config.BacktraceCarrierTelecom}, Hops: []string{"198.51.100.1"}}, want: backtraceReasonNoKnownSignature},
		{name: "limited", row: backtraceRow{Target: config.Endpoint{Kind: config.BacktraceCarrierTelecom}, Hops: []string{"198.51.100.1", "", ""}}, want: backtraceReasonLimitedOrFiltered},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := backtraceUnidentifiedReason(test.row); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBacktraceLineKeysCoverKnownSignatures(t *testing.T) {
	wants := map[string]string{
		"59.43.0.1":    backtraceLineTelecomCN2,
		"202.97.0.1":   backtraceLineTelecom163,
		"218.105.0.1":  backtraceLineUnicomCUII,
		"219.158.0.1":  backtraceLineUnicom169,
		"223.120.0.1":  backtraceLineMobileCMI,
		"221.183.0.1":  backtraceLineMobileCMNET,
		"240e::1":      backtraceLineTelecomIPv6,
		"2408:8120::1": backtraceLineUnicomCUIIIPv6,
		"2408:8000::1": backtraceLineUnicom169IPv6,
		"2409::1":      backtraceLineMobileCMNETIPv6,
	}
	for address, want := range wants {
		hits := matchRouteSignatures([]string{address})
		if len(hits) != 1 || hits[0].Signature.LineKey != want {
			t.Fatalf("%s hit = %+v, want stable key %q", address, hits, want)
		}
	}

	giaHits := matchRouteSignatures([]string{"59.43.0.1"})
	gia, ok := bestBacktraceHit(giaHits, config.BacktraceCarrierTelecom)
	if !ok || backtraceLineKey(gia, giaHits) != backtraceLineTelecomCN2GIA {
		t.Fatalf("CN2 GIA = %+v, %q", gia, backtraceLineKey(gia, giaHits))
	}
}

func TestBacktraceParserKeepsExternalFactsAndUsesEmptyCanonicalMissing(t *testing.T) {
	output := `{"Hops":[[{"Address":{"IP":"198.51.100.2"},"RTT":"12.5 ms","ASN":"64500","Network":"原始网络","Location":"原始位置"}],[{}]]}`
	details, ok := extractNextTraceDetails(output)
	if !ok || len(details) != 2 {
		t.Fatalf("details = %+v, ok=%v", details, ok)
	}
	want := backtraceHop{Hop: 1, IP: "198.51.100.2", Latency: "12.5 ms", ASN: "AS64500", Network: "原始网络", Location: "原始位置", Status: "probe.backtrace.hop.responded"}
	if !reflect.DeepEqual(details[0], want) || details[1].IP != "" || details[1].Latency != "" || details[1].Network != "" || details[1].Status != "probe.backtrace.hop.no_response" {
		t.Fatalf("external/missing facts = %+v, want first %+v", details, want)
	}
	if got := backtraceCellValue(details[1].Network); got != backtraceMissingValue {
		t.Fatalf("missing table value = %q, want %q", got, backtraceMissingValue)
	}
}

func writeBacktraceFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, routeEngineTiny)
	script := `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-V" ]; then
  echo "backtrace-fixture 1"
  exit 0
fi
target=""
for arg do target="$arg"; done
case "$target" in
identified)
	  printf '%s' '{"Hops":[[{"Address":{"IP":"59.43.130.22"},"RTT":3500000,"ASN":"4809","Geo":{"owner":"外部网","country":"外部国","prov":"外部城"}}],[{"Address":{"IP":"198.51.100.1"}}]]}'
  ;;
foreign)
  printf '%s' '{"Hops":[[{"Address":{"IP":"223.120.1.1"}}],[{"Address":{"IP":"198.51.100.2"}}]]}'
  ;;
nosignature)
  printf '%s' '{"Hops":[[{"Address":{"IP":"198.51.100.3"}}],[{"Address":{"IP":"198.51.100.4"}}]]}'
  ;;
limited)
  printf '%s' '{"Hops":[[],[],[{"Address":{"IP":"198.51.100.5"}}]]}'
  ;;
partial)
  printf '%s' '{"Hops":[[{"Address":{"IP":"59.43.130.23"}}]]}'
  exit 7
  ;;
partial_unknown)
  printf '%s' '{"Hops":[[{"Address":{"IP":"198.51.100.6"}}]]}'
  exit 8
  ;;
parse)
  printf '%s' '{"invalid":true}'
  ;;
error)
  printf '%s' 'fixture typed command error'
  exit 9
  ;;
no_response)
  printf '%s' '{"Hops":[[],[]]}'
  ;;
*)
  printf '%s' '{"Hops":[[]]}'
  ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func backtraceFixtureTargets() []config.Endpoint {
	targets := []config.Endpoint{
		{Name: "probe.backtrace.target.beijing.telecom.ipv4", Address: "identified", Kind: config.BacktraceCarrierTelecom},
		{Name: "probe.backtrace.target.guangzhou.unicom.ipv4", Address: "foreign", Kind: config.BacktraceCarrierUnicom},
		{Name: "probe.backtrace.target.shanghai.mobile.ipv6", Address: "nosignature", Kind: config.BacktraceCarrierMobile},
		{Name: "probe.backtrace.target.chengdu.telecom.ipv4", Address: "limited", Kind: config.BacktraceCarrierTelecom},
		{Name: "用户部分", Address: "partial", Kind: config.BacktraceCarrierTelecom},
		{Name: "用户未知", Address: "partial_unknown", Kind: config.BacktraceCarrierUnicom},
		{Name: "用户解析", Address: "parse", Kind: config.BacktraceCarrierMobile},
		{Name: "用户错误", Address: "error", Kind: config.BacktraceCarrierTelecom},
		{Name: "用户无响应", Address: "no_response", Kind: config.BacktraceCarrierMobile},
	}
	return targets
}

func backtraceFixtureRawOutputs() []string {
	return []string{
		`{"Hops":[[{"Address":{"IP":"59.43.130.22"},"RTT":3500000,"ASN":"4809","Geo":{"owner":"外部网","country":"外部国","prov":"外部城"}}],[{"Address":{"IP":"198.51.100.1"}}]]}`,
		`{"Hops":[[{"Address":{"IP":"223.120.1.1"}}],[{"Address":{"IP":"198.51.100.2"}}]]}`,
		`{"Hops":[[{"Address":{"IP":"198.51.100.3"}}],[{"Address":{"IP":"198.51.100.4"}}]]}`,
		`{"Hops":[[],[],[{"Address":{"IP":"198.51.100.5"}}]]}`,
		`{"Hops":[[{"Address":{"IP":"59.43.130.23"}}]]}`,
		`{"Hops":[[{"Address":{"IP":"198.51.100.6"}}]]}`,
		`{"invalid":true}`,
		"fixture typed command error",
		`{"Hops":[[],[]]}`,
	}
}

func TestBacktraceProducerEmitsDirectMachineFactsAndPreservesErrors(t *testing.T) {
	fixturePath := writeBacktraceFixture(t)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", fixturePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	runtime, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = backtraceFixtureTargets()
	result := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if result.Title != "module.backtrace.title" || result.Description != "probe.backtrace.description" || result.Summary != "" || len(result.SummaryMessages) != 1 {
		t.Fatalf("direct result shape = %+v", result)
	}
	if result.SummaryMessages[0].Key != "probe.backtrace.summary.values" || !reflect.DeepEqual(result.SummaryMessages[0].Args, []string{"2", "9"}) {
		t.Fatalf("summary = %+v", result.SummaryMessages)
	}
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 6 || result.Evidence.Expected != 9 {
		t.Fatalf("status/evidence = %s/%+v", result.Status, result.Evidence)
	}
	if len(result.Failures) != 5 {
		t.Fatalf("failures = %+v", result.Failures)
	}
	var sawPartial, sawPartialUnknown, sawError, sawParse, sawNoResponse bool
	for _, failure := range result.Failures {
		switch {
		case failure.Target == "partial":
			sawPartial = failure.Message == "exit status 7"
		case failure.Target == "partial_unknown":
			sawPartialUnknown = failure.Message == "exit status 8"
		case failure.Target == "error":
			sawError = failure.Message == "exit status 9"
		case failure.Target == "parse":
			sawParse = failure.Category == model.FailureParse && failure.Message == ""
		case failure.Target == "no_response":
			sawNoResponse = failure.Category == model.FailureUnknown && failure.Stage == "trace" && failure.Message == ""
		}
	}
	if !sawPartial || !sawPartialUnknown || !sawError || !sawParse || !sawNoResponse {
		t.Fatalf("typed/parse failures = %+v", result.Failures)
	}
	if len(result.Tables) != 2 || len(result.Tables[0].Rows) != 9 || len(result.Tables[0].Columns) != 7 {
		t.Fatalf("tables = %+v", result.Tables)
	}
	rows := result.Tables[0].Rows
	if rows[0][0] != "probe.backtrace.carrier.telecom" || rows[0][1] != "probe.backtrace.target.beijing.telecom.ipv4" || rows[0][2] != backtraceLineTelecomCN2GIA || rows[0][5] != backtraceStatusIdentified || rows[0][6] != backtraceReasonSignatureMatch {
		t.Fatalf("identified row = %+v", rows[0])
	}
	if rows[1][0] != "probe.backtrace.carrier.unicom" || rows[1][5] != backtraceStatusUnidentified || rows[1][6] != backtraceReasonForeignOnly || rows[2][0] != "probe.backtrace.carrier.mobile" || rows[2][6] != backtraceReasonNoKnownSignature || rows[3][6] != backtraceReasonLimitedOrFiltered {
		t.Fatalf("unidentified rows = %v, %v, %v, %v", rows[1], rows[2], rows[3], rows[4])
	}
	if rows[4][2] != backtraceLineTelecomCN2GIA || rows[4][5] != backtraceStatusIdentified || rows[4][6] != backtraceReasonSignatureMatch || rows[5][5] != backtraceStatusUnidentified || rows[5][6] != backtraceReasonNoKnownSignature || rows[6][5] != backtraceStatusFailed || rows[6][6] != backtraceReasonParseFailed || rows[7][5] != backtraceStatusFailed || rows[7][6] != backtraceReasonTraceError || rows[8][5] != backtraceStatusFailed || rows[8][6] != backtraceReasonNoResponsiveHops {
		t.Fatalf("failure rows = %v, %v, %v", rows[4], rows[5], rows[6])
	}
	if len(result.Tables[1].Rows) != 15 {
		t.Fatalf("hop rows = %d, want partial/no-response details retained", len(result.Tables[1].Rows))
	}
	if result.Tables[1].Rows[13][8] != "probe.backtrace.hop.no_response" || result.Tables[1].Rows[14][8] != "probe.backtrace.hop.no_response" {
		t.Fatalf("no-response hop rows = %v, %v", result.Tables[1].Rows[13], result.Tables[1].Rows[14])
	}
	if len(result.TextBlocks) != 9 || !strings.Contains(result.TextBlocks[0].Content, "外部网") || !strings.Contains(result.TextBlocks[0].Content, "外部国") || !strings.Contains(result.TextBlocks[0].Content, "外部城") || !strings.Contains(result.TextBlocks[4].Content, "59.43.130.23") || result.TextBlocks[7].Content != "fixture typed command error" || result.TextBlocks[8].Content != `{"Hops":[[],[]]}` {
		t.Fatalf("raw blocks = %+v", result.TextBlocks)
	}
	for _, block := range result.TextBlocks {
		if block.Title != "probe.backtrace.raw_output" || block.Language != "text" || block.Content == "" {
			t.Fatalf("raw block metadata = %+v", block)
		}
	}
}

func TestBacktraceSkipReasonsAreDistinctAndMachineOnly(t *testing.T) {
	oldPath := os.Getenv("PATH")
	empty := t.TempDir()
	if err := os.Setenv("PATH", empty); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	runtime, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = nil
	missing := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if missing.SummaryMessages[0].Key != "probe.backtrace.summary.tool_missing" || missing.Failures[0].Message != "" || missing.Notes[0] != "probe.backtrace.note.tool_missing" {
		t.Fatalf("tool missing = %+v", missing)
	}

	fixturePath := writeBacktraceFixture(t)
	if err := os.Setenv("PATH", fixturePath); err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = nil
	noTargets := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if noTargets.SummaryMessages[0].Key != "probe.backtrace.summary.no_targets" || len(noTargets.Failures) != 0 {
		t.Fatalf("no targets = %+v", noTargets)
	}
	runtime.BacktraceTargets = []config.Endpoint{{Name: "ipv4", Address: "1.1.1.1", Kind: config.BacktraceCarrierTelecom}}
	runtime.IPVersion = config.IPVersion6
	noFamily := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if noFamily.SummaryMessages[0].Key != "probe.backtrace.summary.no_family_targets" || len(noFamily.Failures) != 0 {
		t.Fatalf("no family targets = %+v", noFamily)
	}
}

func runBacktraceFixtureResult(t *testing.T) model.Result {
	t.Helper()
	t.Setenv("PATH", writeBacktraceFixture(t))
	runtime, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = backtraceFixtureTargets()
	return (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
}

func TestBacktraceProducerDirectShapeContract(t *testing.T) {
	result := runBacktraceFixtureResult(t)
	wantMethodology := model.Methodology{
		Kind: "heuristic", Label: "methodology.heuristic", Engine: "probe.backtrace.methodology.engine",
		Profile: "probe.backtrace.profile", ComparisonScope: "probe.backtrace.comparison_scope",
	}
	if !reflect.DeepEqual(result.Methodology, wantMethodology) {
		t.Fatalf("methodology = %#v, want %#v", result.Methodology, wantMethodology)
	}
	wantFields := []model.Field{
		{Key: "nexttrace_binary", Label: "probe.backtrace.field.nexttrace_binary", Value: routeEngineTiny},
		{Key: "nexttrace_version", Label: "probe.backtrace.field.nexttrace_version", Value: "backtrace-fixture 1"},
		{Key: "nexttrace_binary_sha256", Label: "probe.backtrace.field.nexttrace_binary_sha256"},
		{Key: "arguments", Label: "probe.backtrace.field.arguments", Value: strings.Join(routeCommandArgsForFamily(routeEngine{Name: routeEngineTiny}, "<target>", backtraceMaxHops, config.IPVersionAuto), " ")},
	}
	if len(result.Fields) != len(wantFields) {
		t.Fatalf("fields = %#v", result.Fields)
	}
	for index, want := range wantFields {
		got := result.Fields[index]
		if got.Key != want.Key || got.Label != want.Label || (want.Value != "" && got.Value != want.Value) {
			t.Fatalf("field %d = %#v, want key/label/value %#v", index, got, want)
		}
	}
	if len(result.Fields[2].Value) != 64 {
		t.Fatalf("binary SHA-256 field = %q, want a 64-character machine digest", result.Fields[2].Value)
	}
	measurement := result.Measurements[0]
	if measurement.Key != "backtrace_identified" || measurement.Label != "probe.backtrace.metric.identified" || measurement.Unit != "count" || measurement.Display != "2/9" || measurement.Method != "china-backbone-signature-v1" || measurement.Value != 2 {
		t.Fatalf("measurement = %#v", measurement)
	}
	wantSummaryColumns := []string{"probe.backtrace.column.provider", "probe.backtrace.column.target", "probe.backtrace.column.line", "probe.backtrace.column.hit_hop", "probe.backtrace.column.hit_ip", "probe.backtrace.column.status", "probe.backtrace.column.reason"}
	wantSummaryKeys := []string{"provider", "reference_target", "line", "hit_hop", "hit_ip", "status", "reason"}
	wantHopColumns := []string{"probe.backtrace.column.target", "probe.backtrace.column.provider", "probe.backtrace.column.hop", "probe.backtrace.column.latency", "probe.backtrace.column.ip", "probe.backtrace.column.asn", "probe.backtrace.column.network", "probe.backtrace.column.location", "probe.backtrace.column.status"}
	wantHopKeys := []string{"reference_target", "provider", "hop", "latency_ms", "ip", "asn", "network", "location", "status"}
	if len(result.Tables) != 2 || result.Tables[0].Title != "probe.backtrace.table.summary" || !reflect.DeepEqual(result.Tables[0].Columns, wantSummaryColumns) || !reflect.DeepEqual(result.Tables[0].ColumnKeys, wantSummaryKeys) || result.Tables[0].RowIdentity != "" || result.Tables[1].Title != "probe.backtrace.table.hops" || !reflect.DeepEqual(result.Tables[1].Columns, wantHopColumns) || !reflect.DeepEqual(result.Tables[1].ColumnKeys, wantHopKeys) || result.Tables[1].RowIdentity != "" {
		t.Fatalf("table shape = %#v", result.Tables)
	}
	if len(result.Sources) != 1 || result.Sources[0].Name != "probe.backtrace.source.method.name" || result.Sources[0].Purpose != "probe.backtrace.source.method" {
		t.Fatalf("sources = %#v", result.Sources)
	}
	wantNotes := []string{"probe.backtrace.note.active_path", "probe.backtrace.note.signature_scope", "probe.backtrace.note.cn2_variant_inference", "probe.backtrace.note.ipv6_targets", "probe.backtrace.note.unidentified", "probe.backtrace.note.parse_failed"}
	if !reflect.DeepEqual(result.Notes, wantNotes) || result.Summary != "" || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.backtrace.summary.values" || !reflect.DeepEqual(result.SummaryMessages[0].Args, []string{"2", "9"}) {
		t.Fatalf("notes/summary = %#v/%#v/%q", result.Notes, result.SummaryMessages, result.Summary)
	}
	wantRawOutputs := backtraceFixtureRawOutputs()
	if len(result.TextBlocks) != len(wantRawOutputs) {
		t.Fatalf("text block count = %d, want %d", len(result.TextBlocks), len(wantRawOutputs))
	}
	for index, block := range result.TextBlocks {
		if block.Title != "probe.backtrace.raw_output" || block.Language != "text" || block.Content != wantRawOutputs[index] {
			t.Fatalf("text block %d = %#v, want exact content %q", index, block, wantRawOutputs[index])
		}
	}
}

func TestBacktraceProductionCatalogKeysRegisteredInBothLanguages(t *testing.T) {
	keys := []string{
		"module.backtrace.title", "probe.backtrace.description", "probe.backtrace.methodology.engine", "probe.backtrace.profile", "probe.backtrace.comparison_scope",
		"probe.backtrace.summary.tool_missing", "probe.backtrace.summary.no_targets", "probe.backtrace.summary.no_family_targets", "probe.backtrace.summary.values",
		"probe.backtrace.field.nexttrace_binary", "probe.backtrace.field.nexttrace_version", "probe.backtrace.field.nexttrace_binary_sha256", "probe.backtrace.field.arguments", "probe.backtrace.metric.identified",
		"probe.backtrace.table.summary", "probe.backtrace.table.hops", "probe.backtrace.column.provider", "probe.backtrace.column.target", "probe.backtrace.column.line", "probe.backtrace.column.hit_hop", "probe.backtrace.column.hit_ip", "probe.backtrace.column.status", "probe.backtrace.column.reason", "probe.backtrace.column.hop", "probe.backtrace.column.latency", "probe.backtrace.column.ip", "probe.backtrace.column.asn", "probe.backtrace.column.network", "probe.backtrace.column.location",
		backtraceStatusFailed, backtraceStatusIdentified, backtraceStatusUnidentified, "probe.backtrace.hop.responded", "probe.backtrace.hop.no_response", "probe.backtrace.carrier.telecom", "probe.backtrace.carrier.unicom", "probe.backtrace.carrier.mobile", backtraceReasonSignatureMatch, backtraceReasonForeignOnly, backtraceReasonNoKnownSignature, backtraceReasonLimitedOrFiltered, backtraceReasonTraceError, backtraceReasonParseFailed, backtraceReasonNoResponsiveHops,
		backtraceLineTelecomCN2, backtraceLineTelecomCN2GIA, backtraceLineTelecomCN2GT, backtraceLineTelecom163, backtraceLineUnicomCUII, backtraceLineUnicom169, backtraceLineMobileCMI, backtraceLineMobileCMNET, backtraceLineTelecomIPv6, backtraceLineUnicomCUIIIPv6, backtraceLineUnicom169IPv6, backtraceLineMobileCMNETIPv6, backtraceMissingValue,
		"probe.backtrace.raw_output", "probe.backtrace.source.method.name", "probe.backtrace.source.method", "probe.backtrace.note.tool_missing", "probe.backtrace.note.no_targets", "probe.backtrace.note.no_family_targets", "probe.backtrace.note.parse_failed", "probe.backtrace.note.active_path", "probe.backtrace.note.signature_scope", "probe.backtrace.note.cn2_variant_inference", "probe.backtrace.note.ipv6_targets", "probe.backtrace.note.unidentified",
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		for _, key := range keys {
			if !i18n.Has(language, key) {
				t.Fatalf("%s missing production backtrace key %q", language, key)
			}
		}
	}
}

func TestBacktraceProducerReportRendersBilingualWithoutMutation(t *testing.T) {
	result := runBacktraceFixtureResult(t)
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "backtrace-fixture", Commit: "backtrace"},
		Run: model.RunInfo{
			ID: "backtrace-test", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), CompletedAt: time.Unix(1, 0).UTC(), DurationMS: 1,
			Exposure: "local", Requested: []string{"backtrace"}, OutputFormats: []string{"json", "md", "html"},
		},
		Summary: model.Summary{Status: result.Status, OK: 1}, Results: []model.Result{result},
	}
	canonicalBefore, err := report.JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	rawSentinels := []string{"外部网", "外部国", "外部城", "用户部分", "用户未知", "用户解析", "用户错误", "用户无响应"}
	want := map[i18n.Lang][]string{
		i18n.LangZH: {"已识别", "未识别", "追踪失败", "命中线路", "仅命中其他", "未命中已知", "路径响应", "追踪命令", "路径结果", "已响应", "无响应", "没有响应", "中国电信", "中国联通", "中国移动", "北京电信", "电信 CN2", "推测", "判定原因", "Backtrace 线路特征表"},
		i18n.LangEN: {"Identified", "Unidentified", "Trace failed", "Signature match", "Foreign-carrier", "No known line", "Path responses", "Trace command", "Path result", "Responded", "No response", "No responsive", "China Telecom", "China Unicom", "China Mobile", "Beijing Telecom", "China Telecom CN2", "inferred", "Decision reason", "Backtrace line"},
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		textOutput := report.Text(data, report.TextOptions{Width: 120})
		markdownOutput := report.Markdown(data, nil)
		htmlBytes, err := report.HTML(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		for format, output := range map[string]string{"text": textOutput, "markdown": markdownOutput, "html": string(htmlBytes)} {
			for _, marker := range want[language] {
				if !strings.Contains(output, marker) {
					t.Fatalf("%s %s output missing %q:\n%s", language, format, marker, output)
				}
			}
			for _, sentinel := range rawSentinels {
				if !strings.Contains(output, sentinel) {
					t.Fatalf("%s %s output lost raw sentinel %q:\n%s", language, format, sentinel, output)
				}
			}
			if strings.Contains(output, "probe.backtrace.") || strings.Contains(output, "%!") {
				t.Fatalf("%s %s output leaked stable key or format diagnostic:\n%s", language, format, output)
			}
			if language == i18n.LangEN {
				// The text renderer may insert line breaks inside a long JSON/raw
				// sentinel. Remove formatting whitespace before excluding those
				// explicitly external values from the ECS-owned Han check.
				withoutRaw := strings.Map(func(r rune) rune {
					if unicode.IsSpace(r) {
						return -1
					}
					return r
				}, output)
				for _, sentinel := range rawSentinels {
					withoutRaw = strings.ReplaceAll(withoutRaw, sentinel, "")
				}
				if backtraceTestHasHan(withoutRaw) {
					t.Fatalf("English ECS-owned %s output contains Han characters:\n%s", format, output)
				}
			}
		}
		canonicalAfter, err := report.JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(canonicalBefore, canonicalAfter) {
			t.Fatalf("backtrace %s rendering mutated canonical JSON", language)
		}
	}
}

func backtraceTestHasHan(value string) bool {
	for _, runeValue := range value {
		if unicode.Is(unicode.Han, runeValue) {
			return true
		}
	}
	return false
}

func TestBacktraceProducerPreservesLongTypedCommandError(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("backtrace-error-path-", 12))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, routeEngineTiny)
	if err := os.WriteFile(path, []byte("#!/backtrace-interpreter-that-does-not-exist\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	engine := routeEngine{Name: routeEngineTiny, Path: path}
	_, expectedError := runRouteCommandForFamily(context.Background(), engine, "long", backtraceMaxHops, config.IPVersionAuto)
	if expectedError == nil || len(expectedError.Error()) <= 100 {
		t.Fatalf("fixture error = %v, want a long typed error", expectedError)
	}
	runtime, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.BacktraceTargets = []config.Endpoint{{Name: "long", Address: "long", Kind: config.BacktraceCarrierTelecom}}
	result := (backtraceProbe{}).Run(context.Background(), Environment{Config: runtime})
	if len(result.Failures) != 1 || result.Failures[0].Stage != "trace" || result.Failures[0].Target != "long" || result.Failures[0].Message != expectedError.Error() || result.Failures[0].Category == "" {
		t.Fatalf("long typed failure = %#v, expected original %q", result.Failures, expectedError.Error())
	}
}
