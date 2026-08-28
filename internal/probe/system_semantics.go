package probe

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

func buildSystemResult(start time.Time, snapshot systemSnapshot, resources EnvironmentSnapshot, cloud cloudIdentity) model.Result {
	result := model.NewResult("system", "module.system.title")
	result.StartedAt = start.UTC()
	result.Description = "probe.system.description"
	result.Methodology = model.Methodology{
		Kind:            "inventory",
		Label:           "methodology.inventory",
		Engine:          "probe.system.methodology.engine",
		Profile:         "probe.system.profile",
		ComparisonScope: "probe.system.comparison_scope",
	}

	hardware := snapshot.Hardware
	result.Fields = []model.Field{
		systemField("hostname", snapshot.Hostname),
		systemField("os", snapshot.OS),
		systemField("kernel", snapshot.Kernel),
		systemField("arch", snapshot.Arch),
		systemField("virtualization", snapshot.Virtualization),
		systemField("cloud_provider", cloud.Provider),
		systemField("cloud_region", cloud.Region),
		systemField("cpu_model", snapshot.CPUModel),
		systemField("cpu_topology", fmt.Sprintf("logical=%d;physical=%d", snapshot.LogicalCPUs, snapshot.PhysicalCores)),
		systemField("cpu_allowance", cpuAllowanceMachineValue(snapshot.Allowance)),
		systemField("cpu_frequency", snapshot.CPUFrequency),
		systemField("cpu_steal", systemStealMachineValue(snapshot)),
		systemField("cpu_cache", snapshot.CPUCache),
		systemField("aes", snapshot.AES),
		systemField("virtualization_ext", snapshot.Nested),
		systemField("memory_total", model.FormatBytes(snapshot.MemoryTotal)),
		systemField("memory_used", model.FormatBytes(snapshot.MemoryUsed)),
		systemField("memory_available", model.FormatBytes(snapshot.MemoryFree)),
		systemField("memory_usage_percent", fmt.Sprintf("%.1f %%", snapshot.MemoryUsage)),
		systemField("balloon_reclaim", snapshot.BalloonReclaim.Status()),
		systemField("balloon_reclaim_available", strconv.FormatBool(snapshot.BalloonReclaim.Available)),
		systemField("balloon_reclaim_evidence", fallback(snapshot.BalloonReclaim.Evidence, "none found")),
		systemField("ksm_merging", snapshot.KSM.Status()),
		systemField("ksm_merging_available", strconv.FormatBool(snapshot.KSM.Available)),
		systemField("ksm_merging_evidence", fallback(snapshot.KSM.Evidence, "none found")),
		systemField("swap", model.FormatBytes(snapshot.SwapTotal)),
		systemField("disk_device", fallback(snapshot.DiskDevice, "unavailable")),
		systemField("disk_mount", fallback(snapshot.DiskMount, "unavailable")),
		systemField("disk_total", model.FormatBytes(snapshot.DiskTotal)),
		systemField("disk_used", model.FormatBytes(snapshot.DiskUsed)),
		systemField("disk_available", model.FormatBytes(snapshot.DiskFree)),
		systemField("disk_usage_percent", fmt.Sprintf("%.1f %%", snapshot.DiskUsage)),
		systemField("uptime_seconds", systemUptimeMachineValue(snapshot)),
		systemField("load", snapshot.Load),
		systemField("tcp_congestion", snapshot.Congestion),
		systemField("qdisc", snapshot.QDisc),
		systemField("system_vendor", hardware.SystemVendor),
		systemField("product_name", hardware.ProductName),
		systemField("product_version", hardware.ProductVersion),
		systemField("motherboard", joinHardwareValues(hardware.BoardVendor, hardware.BoardName, hardware.BoardVersion)),
		systemField("bios", joinHardwareValues(hardware.BIOSVendor, hardware.BIOSVersion, hardware.BIOSDate)),
		systemField("gpus", joinHardwareList(hardware.GPUs)),
		systemField("network_adapters", joinHardwareList(hardware.NICs)),
		systemField("block_devices", joinHardwareList(hardware.BlockDevices)),
	}
	appendSystemResourceFields(&result.Fields, resources.Limits)

	result.Measurements = stableSystemMeasurements(snapshot, resources)
	result.Tables = append(result.Tables, systemPressureTable(resources))

	result.Notes = stableSystemNotes(snapshot, hardware)
	result.SummaryMessages = []model.Message{model.NewMessage(
		"probe.system.summary",
		strconv.Itoa(snapshot.LogicalCPUs),
		model.FormatBytes(snapshot.MemoryTotal),
		model.FormatBytes(snapshot.DiskFree),
		snapshot.Virtualization,
	)}
	return result
}

func finalizeSystemResult(result *model.Result, snapshot systemSnapshot) {
	if result == nil {
		return
	}
	missing := 0
	for _, field := range result.Fields {
		if field.Value.Text() == "" || field.Value.Text() == "unknown" {
			missing++
		}
	}
	result.Evidence = model.NewEvidence(len(result.Fields)-missing, len(result.Fields), "sample")
	if missing > 3 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.system.note.partial_inventory")
	}
	if snapshot.StealKnown && snapshot.StealPercent >= stealInterferenceThreshold {
		result.Status = model.StatusWarning
	}
}

func systemField(key, value string) model.Field {
	return model.Field{Key: key, Label: "probe.system.field." + key, Value: model.RawValue(value)}
}

func systemStealMachineValue(snapshot systemSnapshot) string {
	if !snapshot.StealKnown {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f %%", snapshot.StealPercent)
}

func systemUptimeMachineValue(snapshot systemSnapshot) string {
	if !snapshot.UptimeKnown {
		return "unavailable"
	}
	return strconv.FormatUint(snapshot.UptimeSeconds, 10)
}

func appendSystemResourceFields(fields *[]model.Field, limits resourceLimits) {
	*fields = append(*fields,
		systemField("cgroup_cpu_quota", cpuAllowanceMachineValue(limits.CPU)),
		systemField("cgroup_cpuset", fallback(limits.CPUSet, "unavailable")),
		systemField("cgroup_cpuset_source", fallback(limits.CPUSetSource, "unavailable")),
		systemField("cgroup_memory_limit_bytes", systemUintMachineValue(limits.MemoryLimit, "unlimited_or_unavailable")),
		systemField("cgroup_memory_limit_source", fallback(limits.MemoryLimitVia, "unavailable")),
		systemField("cgroup_memory_current_bytes", systemUintMachineValue(limits.MemoryCurrent, "unavailable")),
		systemField("cgroup_memory_current_source", fallback(limits.MemoryCurrentVia, "unavailable")),
		systemField("cgroup_memory_swap_limit_bytes", systemSwapLimitMachineValue(limits)),
		systemField("cgroup_memory_swap_limit_source", fallback(limits.MemorySwapVia, "unavailable")),
	)
}

func systemUintMachineValue(value uint64, zero string) string {
	if value == 0 {
		return zero
	}
	return strconv.FormatUint(value, 10)
}

func systemSwapLimitMachineValue(limits resourceLimits) string {
	if limits.MemorySwapMax {
		return "unlimited"
	}
	return systemUintMachineValue(limits.MemorySwapLimit, "unavailable")
}

func stableSystemMeasurements(snapshot systemSnapshot, resources EnvironmentSnapshot) []model.Measurement {
	measurements := []model.Measurement{
		systemMeasurement("logical_cpus", float64(snapshot.LogicalCPUs), "count", strconv.Itoa(snapshot.LogicalCPUs), "runtime-numcpu-v1", nil),
		systemMeasurement("usable_cpus", float64(snapshot.Allowance.Threads), "count", strconv.Itoa(snapshot.Allowance.Threads), "cpu-allowance-v1", model.BoolPtr(true)),
		systemMeasurement("memory_total_bytes", float64(snapshot.MemoryTotal), "bytes", model.FormatBytes(snapshot.MemoryTotal), "proc-meminfo-v1", nil),
		systemMeasurement("memory_used_bytes", float64(snapshot.MemoryUsed), "bytes", model.FormatBytes(snapshot.MemoryUsed), "proc-meminfo-v1", model.BoolPtr(false)),
		systemMeasurement("memory_available_bytes", float64(snapshot.MemoryFree), "bytes", model.FormatBytes(snapshot.MemoryFree), "proc-meminfo-v1", model.BoolPtr(true)),
		systemMeasurement("memory_usage_percent", snapshot.MemoryUsage, "%", fmt.Sprintf("%.1f %%", snapshot.MemoryUsage), "proc-meminfo-v1", model.BoolPtr(false)),
		systemMeasurement("disk_total_bytes", float64(snapshot.DiskTotal), "bytes", model.FormatBytes(snapshot.DiskTotal), "statfs-v1", nil),
		systemMeasurement("disk_used_bytes", float64(snapshot.DiskUsed), "bytes", model.FormatBytes(snapshot.DiskUsed), "statfs-v1", model.BoolPtr(false)),
		systemMeasurement("disk_free_bytes", float64(snapshot.DiskFree), "bytes", model.FormatBytes(snapshot.DiskFree), "statfs-v1", model.BoolPtr(true)),
		systemMeasurement("disk_usage_percent", snapshot.DiskUsage, "%", fmt.Sprintf("%.1f %%", snapshot.DiskUsage), "statfs-v1", model.BoolPtr(false)),
	}
	if snapshot.StealKnown {
		measurements = append(measurements, systemMeasurement("cpu_steal_percent_cumulative", snapshot.StealPercent, "%", fmt.Sprintf("%.2f %%", snapshot.StealPercent), "proc-stat-steal-cumulative-v1", model.BoolPtr(false)))
	}
	if snapshot.MemoryLimit > 0 {
		measurements = append(measurements, systemMeasurement("memory_limit_bytes", float64(snapshot.MemoryLimit), "bytes", model.FormatBytes(snapshot.MemoryLimit), "cgroup-memory-limit-v1", nil))
	}
	measurements = append(measurements, systemResourceMeasurements(resources)...)
	return measurements
}

func systemResourceMeasurements(resources EnvironmentSnapshot) []model.Measurement {
	measurements := make([]model.Measurement, 0, 12)
	limits := resources.Limits
	if limits.CPU.Quota > 0 {
		measurements = append(measurements, systemMeasurement("cgroup_cpu_quota_cores", limits.CPU.Quota, "cores", fmt.Sprintf("%.2f cores", limits.CPU.Quota), "cgroup-cpu-quota-v1", nil))
	}
	if limits.CPUSetCount > 0 {
		measurements = append(measurements, systemMeasurement("cgroup_cpuset_cpus", float64(limits.CPUSetCount), "count", strconv.Itoa(limits.CPUSetCount), "cgroup-cpuset-effective-v1", nil))
	}
	if limits.MemoryLimit > 0 {
		measurements = append(measurements, systemMeasurement("cgroup_memory_limit_bytes", float64(limits.MemoryLimit), "bytes", model.FormatBytes(limits.MemoryLimit), "cgroup-memory-limit-v1", nil))
	}
	if limits.MemoryCurrent > 0 {
		measurements = append(measurements, systemMeasurement("cgroup_memory_current_bytes", float64(limits.MemoryCurrent), "bytes", model.FormatBytes(limits.MemoryCurrent), "cgroup-memory-current-v1", model.BoolPtr(false)))
	}
	for _, resource := range []string{"cpu", "memory", "io"} {
		pressure := resources.PSI[resource]
		if pressure.Some.Present {
			key := resource + "_psi_some_avg10"
			measurements = append(measurements, systemMeasurement(key, pressure.Some.Avg10, "%", fmt.Sprintf("%.2f %%", pressure.Some.Avg10), "linux-psi-avg10-v1", model.BoolPtr(false)))
		}
		if pressure.Full.Present {
			key := resource + "_psi_full_avg10"
			measurements = append(measurements, systemMeasurement(key, pressure.Full.Avg10, "%", fmt.Sprintf("%.2f %%", pressure.Full.Avg10), "linux-psi-avg10-v1", model.BoolPtr(false)))
		}
	}
	if resources.CPUStat.Present {
		measurements = append(measurements,
			systemMeasurement("cgroup_cpu_nr_throttled", float64(resources.CPUStat.NrThrottled), "events", strconv.FormatUint(resources.CPUStat.NrThrottled, 10), "cgroup-cpu-stat-cumulative-v1", model.BoolPtr(false)),
			systemMeasurement("cgroup_cpu_throttled_seconds", float64(resources.CPUStat.ThrottledUS)/1e6, "s", fmt.Sprintf("%.3f s", float64(resources.CPUStat.ThrottledUS)/1e6), "cgroup-cpu-stat-cumulative-v1", model.BoolPtr(false)),
		)
	}
	if resources.Memory.Present {
		measurements = append(measurements,
			systemMeasurement("cgroup_oom_events", float64(resources.Memory.OOM), "events", strconv.FormatUint(resources.Memory.OOM, 10), "cgroup-memory-events-cumulative-v1", model.BoolPtr(false)),
			systemMeasurement("cgroup_oom_kill_events", float64(resources.Memory.OOMKill), "events", strconv.FormatUint(resources.Memory.OOMKill, 10), "cgroup-memory-events-cumulative-v1", model.BoolPtr(false)),
		)
	}
	return measurements
}

func systemMeasurement(key string, value float64, unit, display, method string, higher *bool) model.Measurement {
	return model.Measurement{
		Key: key, Label: "probe.system.metric." + key, Value: value, Unit: unit,
		Display: model.RawValue(display), Method: method, HigherIsBetter: higher,
	}
}

func systemPressureTable(snapshot EnvironmentSnapshot) model.Table {
	table := model.Table{
		Key:   "system.pressure.cgroup",
		Title: "probe.system.pressure.table.title",
		Columns: []model.TableColumn{
			{Key: "resource", Label: "probe.system.pressure.column.resource"},
			{Key: "psi_some_avg10", Label: "probe.system.pressure.column.some_avg10", Numeric: true},
			{Key: "psi_full_avg10", Label: "probe.system.pressure.column.full_avg10", Numeric: true},
			{Key: "cumulative_events", Label: "probe.system.pressure.column.cumulative_events"},
			{Key: "source", Label: "probe.system.pressure.column.source"},
		},
		RowIdentity: "resource",
	}
	for _, resource := range []string{"cpu", "memory", "io"} {
		pressure := snapshot.PSI[resource]
		some, full := "—", "—"
		if pressure.Some.Present {
			some = fmt.Sprintf("%.2f %%", pressure.Some.Avg10)
		}
		if pressure.Full.Present {
			full = fmt.Sprintf("%.2f %%", pressure.Full.Avg10)
		}
		events := "—"
		switch resource {
		case "cpu":
			if snapshot.CPUStat.Present {
				events = fmt.Sprintf("throttle %d · %.3f s", snapshot.CPUStat.NrThrottled, float64(snapshot.CPUStat.ThrottledUS)/1e6)
			}
		case "memory":
			if snapshot.Memory.Present {
				events = fmt.Sprintf("high %d · max %d · OOM %d · kill %d", snapshot.Memory.High, snapshot.Memory.Max, snapshot.Memory.OOM, snapshot.Memory.OOMKill)
			}
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue(strings.ToUpper(resource)), model.RawValue(some), model.RawValue(full), model.RawValue(events), model.RawValue(fallback(pressure.Source, "unavailable")),
		})
	}
	return table
}

func stableSystemNotes(snapshot systemSnapshot, hardware hardwareInventory) []string {
	notes := make([]string, 0, 8)
	if snapshot.BalloonReclaim.Available {
		notes = append(notes, "probe.system.note.balloon_available")
	} else {
		notes = append(notes, "probe.system.note.balloon_unavailable")
	}
	if snapshot.KSM.Available {
		notes = append(notes, "probe.system.note.ksm_available")
	} else {
		notes = append(notes, "probe.system.note.ksm_unavailable")
	}
	if snapshot.Allowance.Limited() {
		notes = append(notes, "probe.system.note.cpu_quota_limited")
	}
	if snapshot.MemoryLimit > 0 && snapshot.MemoryTotal > 0 && snapshot.MemoryLimit < snapshot.MemoryTotal {
		notes = append(notes, "probe.system.note.memory_quota_limited")
	}
	if snapshot.StealKnown && snapshot.StealPercent >= stealInterferenceThreshold {
		notes = append(notes, "probe.system.note.steal_high")
	}
	if hardware.SystemVendor == "unknown" && hardware.ProductName == "unknown" && hardware.BoardName == "unknown" && hardware.BIOSVersion == "unknown" {
		notes = append(notes, "probe.system.note.dmi_unavailable")
	}
	notes = append(notes, "probe.system.note.hardware_privacy")
	return notes
}
