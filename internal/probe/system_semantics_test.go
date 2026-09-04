package probe

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func systemFixtureSnapshot() systemSnapshot {
	return systemSnapshot{
		Hostname: "fixture-host", OS: "fixture-linux", Kernel: "fixture-kernel", Arch: "amd64",
		CPUModel: "fixture-cpu", LogicalCPUs: 8, PhysicalCores: 4,
		CPUFrequency: "3000 MHz", CPUCache: "8 MiB", AES: "available", Nested: "VT-x (vmx)",
		Virtualization: "kvm", MemoryTotal: 8 << 30, MemoryUsed: 2 << 30, MemoryFree: 6 << 30,
		MemoryUsage: 25, SwapTotal: 1 << 30, DiskTotal: 100 << 30, DiskUsed: 20 << 30,
		DiskFree: 80 << 30, DiskUsage: 20, DiskDevice: "/dev/vda", DiskMount: "/",
		UptimeSeconds: 12345, UptimeKnown: true, Load: "0.10 / 0.20 / 0.30",
		Congestion: "bbr", QDisc: "fq",
		Allowance: cpuAllowance{Visible: 8, Quota: 2, Threads: 2, Source: "fixture-quota"},
		Hardware: hardwareInventory{
			SystemVendor: "fixture-vendor", ProductName: "fixture-product", ProductVersion: "1",
			BoardVendor: "fixture-board-vendor", BoardName: "fixture-board", BoardVersion: "1",
			BIOSVendor: "fixture-bios-vendor", BIOSVersion: "fixture-bios", BIOSDate: "2026-01-01",
			GPUs: []string{"fixture-gpu"}, NICs: []string{"fixture-nic"}, BlockDevices: []string{"fixture-disk"},
		},
		BalloonReclaim: memoryFacility{Available: true, Evidence: "fixture-balloon"},
		KSM:            memoryFacility{Available: false, Evidence: "fixture-ksm"},
		StealPercent:   1.25, StealKnown: true,
	}
}

func systemFixtureResources() EnvironmentSnapshot {
	return EnvironmentSnapshot{
		Limits: resourceLimits{
			CPU:    cpuAllowance{Visible: 8, Quota: 2, Threads: 2, Source: "fixture-quota"},
			CPUSet: "0-1", CPUSetCount: 2, CPUSetSource: "fixture-cpuset",
			MemoryLimit: 4 << 30, MemoryLimitVia: "fixture-memory.max",
			MemoryCurrent: 1 << 30, MemoryCurrentVia: "fixture-memory.current",
			MemorySwapLimit: 2 << 30, MemorySwapVia: "fixture-memory.swap.max",
		},
		PSI: map[string]psiResource{
			"cpu":    {Some: psiValues{Avg10: 1.25, Present: true}, Full: psiValues{Avg10: 0.25, Present: true}, Source: "fixture-psi"},
			"memory": {Some: psiValues{Avg10: 2.5, Present: true}, Source: "fixture-psi"},
			"io":     {},
		},
		CPUStat: cgroupCPUStats{NrThrottled: 3, ThrottledUS: 2_000_000, Source: "fixture-cpu.stat", Present: true},
		Memory:  cgroupMemoryEvents{High: 1, Max: 2, OOM: 0, OOMKill: 0, Source: "fixture-memory.events", Present: true},
	}
}

func systemFixtureKernelFacts() map[string]string {
	return map[string]string{
		"tcp_congestion_control":    "bbr",
		"tcp_available_congestion":  "bbr cubic",
		"default_qdisc":             "fq",
		"rmem_max":                  "1000000",
		"disable_ipv6":              "0",
		"tcp_fastopen":              "3",
		"tcp_syncookies":            "1",
		"tcp_mtu_probing":           "0",
		"tcp_slow_start_after_idle": "1",
		"somaxconn":                 "4096",
		"nf_conntrack_max":          "262144",
		"swappiness":                "60",
		"tcp_rmem":                  "4096 131072 6291456",
		"tcp_wmem":                  "4096 16384 4194304",
		"wmem_max":                  "212992",
	}
}

func TestParseUptimeSecondsIsMachineFactParser(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  uint64
		valid bool
	}{
		{name: "fractional", input: "12345.75 0.00\n", want: 12345, valid: true},
		{name: "empty", input: "", valid: false},
		{name: "malformed", input: "not-a-number", valid: false},
		{name: "negative", input: "-1.0 0.0", valid: false},
		{name: "nan", input: "NaN 0.0", valid: false},
		{name: "positive infinity", input: "+Inf 0.0", valid: false},
		{name: "negative infinity", input: "-Inf 0.0", valid: false},
		{name: "uint64 overflow", input: "18446744073709551616.0 0.0", valid: false},
		{name: "max uint64 fractional", input: "18446744073709551615.9 0.0", want: ^uint64(0), valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid := parseUptimeSeconds([]byte(test.input))
			if got != test.want || valid != test.valid {
				t.Fatalf("parseUptimeSeconds(%q) = %d/%v, want %d/%v", test.input, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestSystemDirectBuilderUsesSingleStableShape(t *testing.T) {
	snapshot := systemFixtureSnapshot()
	resources := systemFixtureResources()
	result := buildSystemResult(time.Unix(100, 0), snapshot, resources, cloudIdentity{Provider: "fixture-cloud", Region: "fixture-region"})
	appendKernelNetworkFacts(&result, systemFixtureKernelFacts())
	finalizeSystemResult(&result, snapshot)

	if result.Title != "module.system.title" || result.Description != "probe.system.description" ||
		result.Methodology.Kind != "inventory" || result.Methodology.Label != "methodology.inventory" ||
		result.Methodology.Engine != "probe.system.methodology.engine" || result.Methodology.Profile != "probe.system.profile" ||
		result.Methodology.ComparisonScope != "probe.system.comparison_scope" {
		t.Fatalf("system identity = %+v", result)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.system.summary" {
		t.Fatalf("system summary = %+v", result.SummaryMessages)
	}
	for _, key := range []string{
		result.Title, result.Description, result.Methodology.Label, result.Methodology.Engine,
		result.Methodology.Profile, result.Methodology.ComparisonScope, result.SummaryMessages[0].Key,
	} {
		if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
			t.Fatalf("system stable key is not bilingual: %q", key)
		}
	}
	if got := result.SummaryMessages[0].Args; len(got) != 4 || got[0] != "8" || got[1] != "8.00 GiB" || got[2] != "80.00 GiB" || got[3] != "kvm" {
		t.Fatalf("system summary args = %v", result.SummaryMessages[0].Args)
	}
	values := make(map[string]string, len(result.Fields))
	fieldKeys := make(map[string]bool, len(result.Fields))
	for _, field := range result.Fields {
		if fieldKeys[field.Key] {
			t.Fatalf("duplicate system field %q", field.Key)
		}
		fieldKeys[field.Key] = true
		values[field.Key] = field.Value.Text()
		if (!strings.HasPrefix(field.Label, "probe.system.field.") && !strings.HasPrefix(field.Label, "probe.kernel.field.")) || !i18n.Has(i18n.LangZH, field.Label) || !i18n.Has(i18n.LangEN, field.Label) {
			t.Fatalf("field is not a bilingual stable key: %+v", field)
		}
	}
	for _, key := range []string{"memory", "disk", "uptime"} {
		if fieldKeys[key] {
			t.Fatalf("legacy compound system field %q remains", key)
		}
	}
	if values["cloud_provider"] != "fixture-cloud" || values["cloud_region"] != "fixture-region" || values["uptime_seconds"] != "12345" {
		t.Fatalf("direct cloud/uptime facts = %v", values)
	}
	if values["cpu_topology"] != "logical=8;physical=4" || values["cpu_allowance"] != "visible=8;quota=2.00;threads=2;source=fixture-quota" {
		t.Fatalf("direct CPU facts = %v", values)
	}
	measurementKeys := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurementKeys[measurement.Key] {
			t.Fatalf("duplicate system measurement %q", measurement.Key)
		}
		measurementKeys[measurement.Key] = true
		if (!strings.HasPrefix(measurement.Label, "probe.system.metric.") && !strings.HasPrefix(measurement.Label, "probe.kernel.metric.")) || !i18n.Has(i18n.LangZH, measurement.Label) || !i18n.Has(i18n.LangEN, measurement.Label) {
			t.Fatalf("measurement is not a bilingual stable key: %+v", measurement)
		}
	}
	if !measurementKeys["cgroup_cpu_quota_cores"] || !measurementKeys["cpu_psi_some_avg10"] || !measurementKeys["cgroup_oom_events"] {
		t.Fatalf("resource measurements missing: %v", measurementKeys)
	}
	tableKeys := make(map[string]bool, len(result.Tables))
	for _, table := range result.Tables {
		if tableKeys[table.Key] {
			t.Fatalf("duplicate system table %q", table.Key)
		}
		tableKeys[table.Key] = true
		if table.Title == "" || !i18n.Has(i18n.LangZH, table.Title) || !i18n.Has(i18n.LangEN, table.Title) {
			t.Fatalf("table title is not a bilingual stable key: %+v", table)
		}
		for _, column := range table.Columns {
			if !i18n.Has(i18n.LangZH, column.Label) || !i18n.Has(i18n.LangEN, column.Label) {
				t.Fatalf("table column is not a bilingual stable key: %+v", table)
			}
		}
	}
	if !fieldKeys["bbr_status"] || !measurementKeys["tcp_rmem_max_bytes"] || !tableKeys["system.kernel.network_parameters"] {
		t.Fatalf("kernel facts missing: fields=%v measurements=%v tables=%v", fieldKeys, measurementKeys, tableKeys)
	}
	if !tableKeys["system.pressure.cgroup"] || len(result.Tables[0].Rows) != 3 {
		t.Fatalf("pressure table missing or incomplete: %+v", result.Tables)
	}
	for _, note := range result.Notes {
		if (!strings.HasPrefix(note, "probe.system.") && !strings.HasPrefix(note, "probe.kernel.")) || !i18n.Has(i18n.LangZH, note) || !i18n.Has(i18n.LangEN, note) {
			t.Fatalf("note is not a bilingual stable key: %q", note)
		}
	}
	warningSnapshot := snapshot
	warningSnapshot.StealPercent = systemStealWarningThreshold
	warning := buildSystemResult(time.Unix(100, 0), warningSnapshot, resources, cloudIdentity{})
	finalizeSystemResult(&warning, warningSnapshot)
	if warning.Status != model.StatusWarning {
		t.Fatalf("high-steal system status = %s", warning.Status)
	}
	missingSnapshot := snapshot
	missingSnapshot.Hostname, missingSnapshot.OS, missingSnapshot.Kernel, missingSnapshot.Arch = "", "", "", ""
	missing := buildSystemResult(time.Unix(100, 0), missingSnapshot, resources, cloudIdentity{})
	finalizeSystemResult(&missing, missingSnapshot)
	if missing.Status != model.StatusWarning || missing.Evidence == nil || missing.Evidence.Valid >= missing.Evidence.Expected {
		t.Fatalf("missing-inventory system status/evidence = %s/%+v", missing.Status, missing.Evidence)
	}
}

func TestSystemDirectResultRendersBilingualWithoutMutation(t *testing.T) {
	snapshot := systemFixtureSnapshot()
	resources := systemFixtureResources()
	result := buildSystemResult(time.Unix(100, 0), snapshot, resources, cloudIdentity{Provider: "fixture-cloud", Region: "fixture-region"})
	appendKernelNetworkFacts(&result, systemFixtureKernelFacts())
	finalizeSystemResult(&result, snapshot)
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "system-fixture", Profile: "standard", Exposure: "local", StartedAt: time.Unix(100, 0).UTC()},
		Summary:       model.Summary{Status: result.Status, Messages: []model.Message{model.NewMessage("message.summary.allOK", "1")}},
		Results:       []model.Result{result},
	}
	before, err := report.JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		text := report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 120})
		markdown := report.Markdown(data, nil)
		html, err := report.HTML(data, nil)
		if err != nil {
			t.Fatalf("HTML %s: %v", language, err)
		}
		for format, output := range []string{text, markdown, string(html)} {
			if strings.Contains(output, "probe.system.") || strings.Contains(output, "probe.kernel.") || strings.Contains(output, "%!") {
				t.Fatalf("%s format %d leaked stable key/format diagnostic:\n%s", language, format, output)
			}
			if language == i18n.LangEN {
				for _, runeValue := range output {
					if runeValue >= '\u3400' && runeValue <= '\u9fff' {
						t.Fatalf("English format %d contains ECS Han text %q:\n%s", format, runeValue, output)
					}
				}
			}
		}
		after, err := report.JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("%s rendering mutated canonical system report", language)
		}
	}
}

func TestSystemEvidenceExcludesUnavailablePlaceholders(t *testing.T) {
	result := model.NewResult("system", "system")
	result.Fields = []model.Field{
		systemField("available", "fixture"),
		systemField("unavailable", "unavailable"),
		systemField("known_unlimited", "unlimited"),
		systemField("unlimited_or_unavailable", "unlimited_or_unavailable"),
		systemField("not_applicable", "n/a"),
		systemField("unknown", "unknown"),
	}
	finalizeSystemResult(&result, systemSnapshot{})
	if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 6 || result.Status != model.StatusWarning {
		t.Fatalf("placeholder evidence/status = %+v/%s", result.Evidence, result.Status)
	}
}

func TestSystemBuiltinUsesDirectProbeAndLiveResultHasNoDuplicateFacts(t *testing.T) {
	systemCount := 0
	for _, definition := range BuiltinDefinitions() {
		if _, ok := definition.Probe.(systemProbe); ok {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Fatalf("systemProbe builtin count = %d", systemCount)
	}
	result := (systemProbe{}).Run(context.Background(), Environment{})
	if result.Title != "module.system.title" || len(result.SummaryMessages) != 1 {
		t.Fatalf("live direct system result = %+v", result)
	}
	fields := make(map[string]bool, len(result.Fields))
	for _, field := range result.Fields {
		if fields[field.Key] {
			t.Fatalf("live duplicate field %q", field.Key)
		}
		fields[field.Key] = true
		if strings.HasPrefix(field.Label, "probe.kernel.") && (!i18n.Has(i18n.LangZH, field.Label) || !i18n.Has(i18n.LangEN, field.Label)) {
			t.Fatalf("live kernel field label is not bilingual: %+v", field)
		}
	}
	if fields["memory"] || fields["disk"] || fields["uptime"] || !fields["uptime_seconds"] {
		t.Fatalf("live legacy/uptime fields = %v", fields)
	}
	measurements := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurements[measurement.Key] {
			t.Fatalf("live duplicate measurement %q", measurement.Key)
		}
		measurements[measurement.Key] = true
		if strings.HasPrefix(measurement.Label, "probe.kernel.") && (!i18n.Has(i18n.LangZH, measurement.Label) || !i18n.Has(i18n.LangEN, measurement.Label)) {
			t.Fatalf("live kernel measurement label is not bilingual: %+v", measurement)
		}
	}
	tables := make(map[string]bool, len(result.Tables))
	for _, table := range result.Tables {
		if tables[table.Key] {
			t.Fatalf("live duplicate table %q", table.Key)
		}
		tables[table.Key] = true
		if strings.HasPrefix(table.Key, "system.kernel.") {
			if !i18n.Has(i18n.LangZH, table.Title) || !i18n.Has(i18n.LangEN, table.Title) {
				t.Fatalf("live kernel table title is not bilingual: %+v", table)
			}
			for _, column := range table.Columns {
				if !i18n.Has(i18n.LangZH, column.Label) || !i18n.Has(i18n.LangEN, column.Label) {
					t.Fatalf("live kernel table column is not bilingual: %+v", table)
				}
			}
		}
	}
}

func TestSystemProductionHasSingleCollectionOwner(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	probeDir := filepath.Dir(testFile)
	files, err := filepath.Glob(filepath.Join(probeDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{
		"collectSystem":              true,
		"CaptureEnvironmentSnapshot": true,
		"discoverLocalCloudIdentity": true,
		"appendKernelNetworkParams":  true,
	}
	forbidden := map[string]bool{
		"systemSemanticProbe":   true,
		"stabilizeSystemResult": true,
		"systemUptimeSeconds":   true,
	}
	productionCalls := make(map[string][]string, len(targets))
	systemRunCalls := make(map[string]int, len(targets))
	forbiddenUses := make(map[string][]string, len(forbidden))
	systemRunCount := 0
	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.CallExpr:
				var name string
				switch function := value.Fun.(type) {
				case *ast.Ident:
					name = function.Name
				case *ast.SelectorExpr:
					name = function.Sel.Name
				}
				if targets[name] {
					productionCalls[name] = append(productionCalls[name], filepath.Base(filename))
				}
			case *ast.Ident:
				if forbidden[value.Name] {
					forbiddenUses[value.Name] = append(forbiddenUses[value.Name], filepath.Base(filename))
				}
			}
			return true
		})
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "Run" || function.Recv == nil || function.Body == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver, ok := function.Recv.List[0].Type.(*ast.Ident)
			if !ok || receiver.Name != "systemProbe" {
				continue
			}
			systemRunCount++
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				var name string
				switch function := call.Fun.(type) {
				case *ast.Ident:
					name = function.Name
				case *ast.SelectorExpr:
					name = function.Sel.Name
				}
				if targets[name] {
					systemRunCalls[name]++
				}
				return true
			})
		}
	}
	if systemRunCount != 1 {
		t.Fatalf("systemProbe.Run count = %d", systemRunCount)
	}
	for name := range targets {
		if len(productionCalls[name]) != 1 || systemRunCalls[name] != 1 {
			t.Fatalf("system production call sites for %s = %v, systemProbe.Run calls = %d", name, productionCalls[name], systemRunCalls[name])
		}
	}
	for name, uses := range forbiddenUses {
		if len(uses) != 0 {
			t.Fatalf("forbidden system bridge identifier %s remains in %v", name, uses)
		}
	}
}
