//go:build integration && freebsd

package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func TestIntegrationPingLoopback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("FreeBSD ping integration must run as the ordinary non-root user")
	}
	path, err := icmpPingPath()
	if err != nil {
		t.Fatalf("FreeBSD base ping is required for integration tests: %v", err)
	}
	if path != "/sbin/ping" {
		t.Fatalf("FreeBSD integration selected %q, want /sbin/ping", path)
	}

	for _, test := range []struct {
		name, host, family string
	}{
		{name: "IPv4 loopback", host: "127.0.0.1", family: "4"},
		{name: "IPv6 loopback", host: "::1", family: "6"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			stats := runICMPPingFamily(ctx, test.host, 2, time.Second, test.family)
			if ctx.Err() != nil {
				t.Fatalf("FreeBSD ping loopback context expired: %v", ctx.Err())
			}
			if stats.Err != nil || !stats.Available {
				t.Fatalf("FreeBSD ping loopback unavailable: %+v", stats)
			}
			if !stats.LossKnown || stats.LossPercent != 0 {
				t.Errorf("FreeBSD ping loss = %f (known=%t), want zero", stats.LossPercent, stats.LossKnown)
			}
			if !stats.RTTKnown || !stats.StdDevKnown {
				t.Fatalf("FreeBSD ping did not return min/avg/max/stddev statistics: %+v", stats)
			}
			if stats.MinMS < 0 || stats.AvgMS < stats.MinMS || stats.MaxMS < stats.AvgMS || stats.StdDevMS < 0 {
				t.Errorf("FreeBSD ping min/avg/max/stddev ordering is invalid: %.3f/%.3f/%.3f/%.3f", stats.MinMS, stats.AvgMS, stats.MaxMS, stats.StdDevMS)
			}
		})
	}
}

func TestIntegrationFreeBSDTracerouteCanonicalRoute(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("FreeBSD traceroute integration must run as the ordinary non-root user")
	}
	backend := detectTraceBackend(context.Background())
	if backend.Name != "freebsd-traceroute" || backend.Adapter != traceFreeBSDTracerouteAdapter ||
		!traceBackendAvailableForFamily(backend, "4") || !traceBackendAvailableForFamily(backend, "6") {
		t.Fatalf("FreeBSD traceroute backend = %#v", backend)
	}
	for _, test := range []struct {
		name, target, family string
		maxHops              int
	}{
		{name: "IPv4 loopback", target: "127.0.0.1", family: "4", maxHops: 12},
		{name: "IPv6 loopback", target: "::1", family: "6", maxHops: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			run := runTraceCommandForFamily(ctx, backend, test.target, test.maxHops, test.family)
			if run.Err != nil || run.ParseErr != nil || !run.Parsed {
				t.Fatalf("FreeBSD traceroute run = parsed:%t err:%v parse:%v stdout:%q stderr:%q", run.Parsed, run.Err, run.ParseErr, run.Stdout, run.Stderr)
			}
			if ctx.Err() != nil {
				t.Fatalf("FreeBSD traceroute context expired: %v", ctx.Err())
			}
			if len(run.Stdout) == 0 || len(run.Stderr) == 0 || !strings.Contains(string(run.Stderr), "traceroute") {
				t.Fatalf("FreeBSD traceroute raw streams = stdout:%q stderr:%q", run.Stdout, run.Stderr)
			}
			if len(run.Trace.Hops) == 0 || run.Trace.Engine != "freebsd-traceroute" || run.Trace.Adapter != traceFreeBSDTracerouteAdapter || run.Trace.Target != test.target || run.Trace.Family != traceFamilyName(test.family) {
				t.Fatalf("FreeBSD canonical trace = %#v", run.Trace)
			}
			if slots, visible, timeouts, ok := traceHopSummary(run.Trace); !ok || slots != len(run.Trace.Hops) || visible == 0 || timeouts != slots-visible {
				t.Fatalf("FreeBSD canonical summary = %d/%d/%d/%v", slots, visible, timeouts, ok)
			}
			normalized, err := run.Trace.canonicalJSON()
			if err != nil || !json.Valid(normalized) || !strings.Contains(string(normalized), `"adapter":"freebsd-traceroute-text-v1"`) {
				t.Fatalf("FreeBSD canonical JSON = %q err:%v", normalized, err)
			}

			route := (routeProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
				IPVersion:    test.family,
				RouteTargets: []config.Endpoint{{Name: test.name, Address: test.target, Kind: config.RouteTargetKindGlobal, Family: test.family}},
			}})
			if route.Status != model.StatusOK || route.Evidence == nil || route.Evidence.Valid != 1 || len(route.Measurements) != 4 {
				t.Fatalf("FreeBSD route result = status:%s evidence:%+v measurements:%d failures:%+v", route.Status, route.Evidence, len(route.Measurements), route.Failures)
			}
			if len(route.Fields) < 3 || route.Fields[0].Value.Text() != "freebsd-traceroute" || !strings.Contains(route.Fields[1].Value.Text(), "FreeBSD") || !strings.Contains(route.Fields[2].Value.Text(), "-m "+strconv.Itoa(test.maxHops)) {
				t.Fatalf("FreeBSD route fields = %#v", route.Fields)
			}
			if route.Methodology.Parameters["adapter"] != traceFreeBSDTracerouteAdapter {
				t.Fatalf("FreeBSD route adapter provenance = %q", route.Methodology.Parameters["adapter"])
			}
			if route.Methodology.Parameters["max_hops"] != strconv.Itoa(test.maxHops) {
				t.Fatalf("FreeBSD route max_hops provenance = %q, want %d", route.Methodology.Parameters["max_hops"], test.maxHops)
			}
			if len(route.Fields) < 3 || route.Methodology.Parameters["arguments"] != route.Fields[2].Value.Text() {
				t.Fatalf("FreeBSD route comparison arguments = %q, field arguments = %q", route.Methodology.Parameters["arguments"], route.Fields[2].Value.Text())
			}
			var raw, canonical bool
			for _, block := range route.TextBlocks {
				switch block.Title {
				case traceRawOutputTitle, traceRawStderrTitle:
					raw = raw || strings.Contains(block.Content, "traceroute")
				case traceNormalizedJSONTitle:
					canonical = canonical || block.Language == "json" && json.Valid([]byte(block.Content)) && strings.Contains(block.Content, `"engine":"freebsd-traceroute"`)
				}
			}
			if !raw || !canonical {
				t.Fatalf("FreeBSD route raw/canonical blocks = %#v", route.TextBlocks)
			}
		})
	}

	mixed := (routeProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
		IPVersion: config.IPVersionAuto,
		RouteTargets: []config.Endpoint{
			{Name: "IPv4 loopback", Address: "127.0.0.1", Kind: config.RouteTargetKindGlobal, Family: config.IPVersion4},
			{Name: "IPv6 loopback", Address: "::1", Kind: config.RouteTargetKindGlobal, Family: config.IPVersion6},
		},
	}})
	if mixed.Status != model.StatusOK || mixed.Evidence == nil || mixed.Evidence.Valid != 2 || len(mixed.Measurements) != 8 {
		t.Fatalf("FreeBSD mixed-family route = status:%s evidence:%+v measurements:%d failures:%+v", mixed.Status, mixed.Evidence, len(mixed.Measurements), mixed.Failures)
	}
	var mixedArguments string
	for _, field := range mixed.Fields {
		if field.Key == "arguments" {
			mixedArguments = field.Value.Text()
		}
	}
	if mixedArguments == "" || mixed.Methodology.Parameters["arguments"] != mixedArguments {
		t.Fatalf("FreeBSD mixed-family comparison arguments = %q, field arguments = %q", mixed.Methodology.Parameters["arguments"], mixedArguments)
	}
	var mixedVariants []traceArgumentVariant
	if err := json.Unmarshal([]byte(mixedArguments), &mixedVariants); err != nil {
		t.Fatalf("FreeBSD mixed-family arguments are not canonical JSON: %q: %v", mixedArguments, err)
	}
	if len(mixedVariants) != 2 || mixedVariants[0].Family != "ipv4" || mixedVariants[1].Family != "ipv6" {
		t.Fatalf("FreeBSD mixed-family variants = %#v, want ordered ipv4/ipv6", mixedVariants)
	}
	if !strings.Contains(strings.Join(mixedVariants[0].Args, " "), "-m 12") || !strings.Contains(strings.Join(mixedVariants[1].Args, " "), "-m 20") {
		t.Fatalf("FreeBSD mixed-family max-hop arguments = %#v, want -m 12 and -m 20", mixedVariants)
	}
	if strings.Contains(mixedArguments, freeBSDTracerouteIPv4Path) || strings.Contains(mixedArguments, freeBSDTracerouteIPv6Path) {
		t.Fatalf("FreeBSD mixed-family arguments leaked executable path: %q", mixedArguments)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	run := runTraceCommandForFamily(cancelled, backend, "127.0.0.1", 12, "4")
	if !errors.Is(run.Err, context.Canceled) {
		t.Fatalf("cancelled FreeBSD traceroute err = %v", run.Err)
	}
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	deadlineRun := runTraceCommandForFamily(deadlineCtx, backend, "192.0.2.1", 12, "4")
	deadlineCancel()
	if !errors.Is(deadlineRun.Err, context.DeadlineExceeded) || deadlineRun.Parsed || len(deadlineRun.Stdout) == 0 || len(deadlineRun.Stderr) == 0 {
		t.Fatalf("timed-out FreeBSD traceroute = parsed:%t err:%v stdout:%q stderr:%q", deadlineRun.Parsed, deadlineRun.Err, deadlineRun.Stdout, deadlineRun.Stderr)
	}

	invalid := runTraceCommandForFamily(context.Background(), backend, "invalid.invalid", 12, "4")
	if invalid.Err == nil || len(invalid.Stderr) == 0 {
		t.Fatalf("FreeBSD nonzero traceroute = err:%v stdout:%q stderr:%q", invalid.Err, invalid.Stdout, invalid.Stderr)
	}
	failedRoute := (routeProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
		IPVersion:    config.IPVersion4,
		RouteTargets: []config.Endpoint{{Name: "invalid", Address: "invalid.invalid", Kind: config.RouteTargetKindGlobal, Family: config.IPVersion4}},
	}})
	var stderrPreserved bool
	for _, block := range failedRoute.TextBlocks {
		if block.Title == traceRawStderrTitle && strings.TrimSpace(block.Content) != "" {
			stderrPreserved = true
		}
	}
	if !stderrPreserved {
		t.Fatalf("FreeBSD failed route did not preserve stderr: %#v", failedRoute.TextBlocks)
	}
}

func TestIntegrationFreeBSDRoutePresentationBilingual(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("FreeBSD route presentation integration must run as the ordinary non-root user")
	}
	result := (routeProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
		IPVersion:    config.IPVersion4,
		RouteTargets: []config.Endpoint{{Name: "IPv4 loopback", Address: "127.0.0.1", Kind: config.RouteTargetKindGlobal, Family: config.IPVersion4}},
	}})
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 1 || len(result.TextBlocks) == 0 {
		t.Fatalf("FreeBSD route presentation fixture = status:%s evidence:%+v blocks:%d failures:%+v", result.Status, result.Evidence, len(result.TextBlocks), result.Failures)
	}
	data := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: buildinfo.Name, Version: "test", Commit: "freebsd-route"},
		Run:           model.RunInfo{ID: "freebsd-route-presentation", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), CompletedAt: time.Unix(1, 0).UTC(), DurationMS: 1, Exposure: "local", Requested: []string{"route"}, OutputFormats: []string{"json", "md", "html"}},
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
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			textOutput := report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 110})
			markdownOutput := report.Markdown(data, nil)
			htmlBytes, err := report.HTML(data, nil)
			if err != nil {
				t.Fatal(err)
			}
			markers := []string{
				i18n.T(traceRawStderrTitle),
				i18n.T(traceNormalizedJSONTitle),
				i18n.T(traceFreeBSDSourceName),
				i18n.T(traceFreeBSDSourcePurpose),
			}
			for format, output := range map[string]string{"text": textOutput, "markdown": markdownOutput, "html": string(htmlBytes)} {
				lower := strings.ToLower(output)
				if strings.Contains(output, "probe.route.") || strings.Contains(lower, "nexttrace") || strings.Contains(lower, "pure json") || strings.Contains(lower, "pure-json") || strings.Contains(lower, "native json") || strings.Contains(output, "%!") {
					t.Fatalf("FreeBSD route %s output leaked platform-inaccurate wording:\n%s", format, output)
				}
				for _, marker := range markers {
					if format == "markdown" {
						marker = strings.NewReplacer("(", `\(`, ")", `\)`).Replace(marker)
					}
					if !strings.Contains(output, marker) {
						t.Fatalf("FreeBSD route %s output missing %q:\n%s", format, marker, output)
					}
				}
			}
			canonicalAfter, err := report.JSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonicalBefore, canonicalAfter) {
				t.Fatalf("FreeBSD route render mutated canonical JSON")
			}
		})
	}
}

func TestIntegrationFreeBSDBacktraceCanonical(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("FreeBSD backtrace integration must run as the ordinary non-root user")
	}
	target := config.Endpoint{Name: "FreeBSD loopback", Address: "127.0.0.1", Kind: config.BacktraceCarrierTelecom, Family: config.IPVersion4}
	backend := detectTraceBackend(context.Background())
	if backend.Name != "freebsd-traceroute" || backend.Adapter != traceFreeBSDTracerouteAdapter || !traceBackendAvailableForFamily(backend, "4") {
		t.Fatalf("FreeBSD backtrace backend = %#v", backend)
	}
	row := runBacktraceTarget(context.Background(), backend, target, "4")
	if row.Err != nil || row.ParseErr != nil || row.ParseFailed || len(row.Trace.Hops) == 0 || !row.Trace.Hops[0].Responded {
		t.Fatalf("FreeBSD backtrace canonical run = trace:%#v err:%v parse:%v", row.Trace, row.Err, row.ParseErr)
	}
	if len(row.Details) != len(row.Trace.Hops) || len(row.Hits) != 0 {
		// Loopback is intentionally not a carrier-signature hit. It must still
		// produce a successful canonical path and an unidentified classification.
		t.Fatalf("FreeBSD backtrace derived facts = details:%#v hits:%#v", row.Details, row.Hits)
	}
	for _, detail := range row.Details {
		if detail.ASN != "" || detail.Network != "" || detail.Location != "" {
			t.Fatalf("FreeBSD traceroute fabricated unavailable metadata: %+v", detail)
		}
	}
	if row.RawStdout == "" || row.RawStderr == "" || !strings.Contains(row.RawStderr, "traceroute") || row.NormalizedJSON == "" || !json.Valid([]byte(row.NormalizedJSON)) {
		t.Fatalf("FreeBSD backtrace raw/canonical evidence = stdout:%q stderr:%q normalized:%q", row.RawStdout, row.RawStderr, row.NormalizedJSON)
	}
	if strings.Contains(row.NormalizedJSON, `"Hops"`) || !strings.Contains(row.NormalizedJSON, `"engine":"freebsd-traceroute"`) || !strings.Contains(row.NormalizedJSON, `"adapter":"freebsd-traceroute-text-v1"`) {
		t.Fatalf("FreeBSD backtrace normalized provenance = %q", row.NormalizedJSON)
	}

	result := (backtraceProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
		IPVersion:        config.IPVersion4,
		BacktraceTargets: []config.Endpoint{target},
	}})
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 1 || len(result.Tables) != 2 || len(result.Tables[1].Rows) != len(row.Details) {
		t.Fatalf("FreeBSD backtrace result = status:%s evidence:%+v tables:%d/%d failures:%+v", result.Status, result.Evidence, len(result.Tables), len(result.Tables[1].Rows), result.Failures)
	}
	fieldValues := make(map[string]string, len(result.Fields))
	for _, field := range result.Fields {
		fieldValues[field.Key] = field.Value.Text()
	}
	if fieldValues["engine"] != backend.Name || fieldValues["adapter"] != backend.Adapter || !strings.Contains(fieldValues["arguments"], "-m 20") || result.Methodology.Parameters["max_hops"] != "20" || result.Methodology.Parameters["adapter"] != backend.Adapter || result.Methodology.Parameters["arguments"] != fieldValues["arguments"] {
		t.Fatalf("FreeBSD backtrace provenance = fields:%v parameters:%v", fieldValues, result.Methodology.Parameters)
	}
	var rawOutput, rawStderr, normalized bool
	for _, block := range result.TextBlocks {
		switch block.Title {
		case backtraceRawOutputTitle:
			rawOutput = rawOutput || block.Language == "text" && strings.Contains(block.Content, "127.0.0.1")
		case backtraceRawStderrTitle:
			rawStderr = rawStderr || block.Language == "text" && strings.Contains(block.Content, "traceroute")
		case backtraceNormalizedJSONTitle:
			normalized = normalized || block.Language == "json" && json.Valid([]byte(block.Content)) && strings.Contains(block.Content, `"engine":"freebsd-traceroute"`)
		}
	}
	if !rawOutput || !rawStderr || !normalized {
		t.Fatalf("FreeBSD backtrace result blocks = %#v", result.TextBlocks)
	}
	detail := result.Tables[1].Rows[0]
	for _, index := range []int{5, 6, 7} {
		if detail[index].Text() != backtraceMissingValue {
			t.Fatalf("FreeBSD missing detail column %d = %#v", index, detail)
		}
	}
	if len(result.Sources) != 2 || result.Sources[0].Name != traceFreeBSDSourceName || result.Sources[1].Name != "probe.backtrace.source.method.name" {
		t.Fatalf("FreeBSD backtrace sources = %#v", result.Sources)
	}

	data := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: buildinfo.Name, Version: "test", Commit: "freebsd-backtrace"},
		Run:           model.RunInfo{ID: "freebsd-backtrace", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), CompletedAt: time.Unix(1, 0).UTC(), DurationMS: 1, Exposure: "local", Requested: []string{"backtrace"}, OutputFormats: []string{"json", "md", "html"}},
		Summary:       model.Summary{Status: result.Status, OK: 1}, Results: []model.Result{result},
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
		for format, output := range map[string]string{"text": textOutput, "markdown": markdownOutput, "html": string(htmlBytes)} {
			lower := strings.ToLower(output)
			if strings.Contains(output, "probe.backtrace.") || strings.Contains(lower, "nexttrace") || strings.Contains(lower, "pure json") || strings.Contains(lower, "native json") || strings.Contains(output, "%!") {
				t.Fatalf("FreeBSD backtrace %s output leaked inaccurate provenance: %s", format, output)
			}
		}
		canonicalAfter, err := report.JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(canonicalBefore, canonicalAfter) {
			t.Fatalf("FreeBSD backtrace render mutated canonical JSON")
		}
	}
}
