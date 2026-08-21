package probe

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestSystemStage9BoundaryUsesMachineSemantics(t *testing.T) {
	result := model.NewResult("system", "系统与资源")
	result.Fields = []model.Field{
		{Key: "cloud_provider", Label: "云厂商", Value: "fixture-cloud"},
		{Key: "cloud_region", Label: "云区域", Value: "fixture-region"},
		{Key: "bbr_status", Label: "probe.kernel.field.bbr_status", Value: "enabled"},
	}
	result.Measurements = []model.Measurement{{Key: "tcp_rmem_max_bytes", Label: "probe.kernel.metric.rmem_max", Value: 1, Unit: "bytes", Display: "1 B"}}
	result.Notes = []string{"旧中文说明", "probe.kernel.note.rmem_bdp_limit"}
	result.Tables = []model.Table{{Key: "system.pressure.cgroup", Title: "旧中文表名", Columns: []string{"旧列"}}}

	snapshot := systemSnapshot{
		Hostname: "fixture", OS: "linux", Kernel: "6.0", Arch: "amd64",
		CPUModel: "fixture-cpu", LogicalCPUs: 8, PhysicalCores: 4,
		CPUFrequency: "3000 MHz", CPUCache: "8 MiB", AES: "yes", Nested: "yes",
		Virtualization: "kvm", MemoryTotal: 8 << 30, MemoryUsed: 2 << 30, MemoryFree: 6 << 30,
		MemoryUsage: 25, DiskTotal: 100 << 30, DiskUsed: 20 << 30, DiskFree: 80 << 30, DiskUsage: 20,
		DiskDevice: "/dev/vda", DiskMount: "/", Load: "0.10 0.20 0.30", Congestion: "bbr", QDisc: "fq",
		Allowance: cpuAllowance{Visible: 8, Quota: 2, Threads: 2, Source: "fixture-quota"},
		Hardware: hardwareInventory{SystemVendor: "fixture-vendor", ProductName: "fixture-product", BoardName: "fixture-board", BIOSVersion: "fixture-bios"},
	}
	resources := EnvironmentSnapshot{Limits: resourceLimits{CPU: snapshot.Allowance, CPUSet: "0-1", CPUSetCount: 2, CPUSetSource: "/fixture/cpuset"}}
	stabilizeSystemResult(&result, snapshot, resources)

	if result.Title != "module.system.title" || result.Description != "probe.system.description" || result.Methodology.Label != "methodology.inventory" {
		t.Fatalf("system presentation identity not stabilized: %+v", result)
	}
	if result.Summary != "" || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.system.summary" {
		t.Fatalf("system summary is not a stable message: summary=%q messages=%+v", result.Summary, result.SummaryMessages)
	}
	values := make(map[string]string)
	for _, field := range result.Fields {
		values[field.Key] = field.Value
		if strings.Contains(field.Label, "主机") || strings.Contains(field.Label, "内存") || strings.Contains(field.Label, "磁盘") {
			t.Fatalf("source-language field label crossed boundary: %+v", field)
		}
	}
	if values["cpu_topology"] != "logical=8;physical=4" || values["cpu_allowance"] != "visible=8;quota=2.00;threads=2;source=fixture-quota" {
		t.Fatalf("machine CPU facts = %v", values)
	}
	if _, ok := values["memory"]; ok {
		t.Fatal("legacy compound memory presentation field crossed boundary")
	}
	if _, ok := values["disk"]; ok {
		t.Fatal("legacy compound disk presentation field crossed boundary")
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.system.pressure.table.title" || len(result.Tables[0].Columns) != 5 {
		t.Fatalf("system pressure table not stabilized: %+v", result.Tables)
	}
	for _, note := range result.Notes {
		if note == "旧中文说明" {
			t.Fatalf("legacy source-text note crossed boundary: %v", result.Notes)
		}
	}
}
