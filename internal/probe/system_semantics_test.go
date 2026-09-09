//go:build linux

package probe

import (
	"bytes"
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
		MemoryUsage: 25, MemoryTotalKnown: true, MemoryUsedKnown: true, MemoryAvailableKnown: true, PhysicalCoresKnown: true,
		SwapTotal: 1 << 30, SwapKnown: true, DiskTotal: 100 << 30, DiskUsed: 20 << 30,
		DiskFree: 80 << 30, DiskUsage: 20, DiskDevice: "/dev/vda", DiskMount: "/",
		DiskKnown:     true,
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
