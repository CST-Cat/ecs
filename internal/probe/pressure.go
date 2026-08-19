package probe

// Linux resource-pressure and cgroup diagnostics.
//
// PSI totals are sampled around a benchmark so no additional workload or
// network traffic is generated. cgroup counters are monotonic; deltas describe
// the benchmark window, while the system module also exposes cumulative facts.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

// stealInterferenceThreshold 是触发自动复测的 CPU steal 门槛（百分比）。
// 与 system 模块的累计 steal 告警同阈值，见 AssessBenchmarkInterference 的说明。
const stealInterferenceThreshold = 5.0

type psiValues struct {
	Avg10, Avg60, Avg300 float64
	TotalUS              uint64
	Present              bool
}

type psiResource struct {
	Some, Full psiValues
	Source     string
}

type cgroupCPUStats struct {
	UsageUS, NrPeriods, NrThrottled, ThrottledUS uint64
	Source                                       string
	Present                                      bool
}

type cgroupMemoryEvents struct {
	Low, High, Max, OOM, OOMKill, OOMGroupKill, FailCount uint64
	Source                                                string
	Present                                               bool
}

type resourceLimits struct {
	CPU              cpuAllowance
	CPUSet           string
	CPUSetCount      int
	CPUSetSource     string
	MemoryLimit      uint64
	MemoryLimitVia   string
	MemoryCurrent    uint64
	MemoryCurrentVia string
	MemorySwapLimit  uint64
	MemorySwapVia    string
	MemorySwapMax    bool
}

// EnvironmentSnapshot is intentionally opaque outside probe. Runner only
// captures and passes it back to AssessBenchmarkInterference.
type EnvironmentSnapshot struct {
	CapturedAt time.Time
	Load1      float64
	LoadKnown  bool
	CPUTimes   cpuTimeSample
	CPUTracked bool
	CPUStat    cgroupCPUStats
	Memory     cgroupMemoryEvents
	PSI        map[string]psiResource
	Limits     resourceLimits
}

// CaptureEnvironmentSnapshot performs read-only local sampling.
func CaptureEnvironmentSnapshot() EnvironmentSnapshot {
	snapshot := EnvironmentSnapshot{
		CapturedAt: time.Now(),
		PSI:        make(map[string]psiResource, 3),
		Limits:     collectResourceLimits(),
	}
	snapshot.Load1, snapshot.LoadKnown = readLoadAverage1()
	snapshot.CPUTimes, snapshot.CPUTracked = readCPUTimes()
	snapshot.CPUStat = readCgroupCPUStats()
	snapshot.Memory = readCgroupMemoryEvents()
	for _, resource := range []string{"cpu", "memory", "io"} {
		snapshot.PSI[resource] = readPressure(resource)
	}
	return snapshot
}

func collectResourceLimits() resourceLimits {
	limits := resourceLimits{CPU: detectCPUAllowance()}
	limits.CPUSet, limits.CPUSetCount, limits.CPUSetSource = readCPUSet()
	limits.MemoryLimit, limits.MemoryLimitVia, _ = cgroupMemoryLimit()
	limits.MemoryCurrent, limits.MemoryCurrentVia, _ = cgroupMemoryCurrent()
	if value, source, unlimited, ok := readCgroupLimit("memory.swap.max", "memory.memsw.limit_in_bytes"); ok {
		limits.MemorySwapLimit, limits.MemorySwapVia, limits.MemorySwapMax = value, source, unlimited
	}
	return limits
}

func currentCgroupPaths(file string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, relative := range selfCgroupPaths() {
		if relative == "" || relative == "/" {
			continue
		}
		path := filepath.Join(cgroupV2Root, relative, file)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	root := filepath.Join(cgroupV2Root, file)
	if !seen[root] {
		paths = append(paths, root)
	}
	return paths
}

func parsePSI(data string) psiResource {
	var result psiResource
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		values := psiValues{}
		for _, field := range fields[1:] {
			key, raw, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "avg10":
				values.Avg10, _ = strconv.ParseFloat(raw, 64)
			case "avg60":
				values.Avg60, _ = strconv.ParseFloat(raw, 64)
			case "avg300":
				values.Avg300, _ = strconv.ParseFloat(raw, 64)
			case "total":
				values.TotalUS, _ = strconv.ParseUint(raw, 10, 64)
			}
		}
		values.Present = true
		switch fields[0] {
		case "some":
			result.Some = values
		case "full":
			result.Full = values
		}
	}
	return result
}

func readPressure(resource string) psiResource {
	for _, path := range currentCgroupPaths(resource + ".pressure") {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parsed := parsePSI(string(data))
		if parsed.Some.Present || parsed.Full.Present {
			parsed.Source = path
			return parsed
		}
	}
	path := filepath.Join("/proc/pressure", resource)
	data, err := os.ReadFile(path)
	if err != nil {
		return psiResource{}
	}
	parsed := parsePSI(string(data))
	parsed.Source = path
	return parsed
}

func parseKeyValueCounters(data string) map[string]uint64 {
	values := make(map[string]uint64)
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			values[fields[0]] = value
		}
	}
	return values
}

func readCgroupCPUStats() cgroupCPUStats {
	for _, path := range append(currentCgroupPaths("cpu.stat"), filepath.Join(cgroupV1CPU, "cpu.stat")) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		values := parseKeyValueCounters(string(data))
		result := cgroupCPUStats{
			UsageUS: values["usage_usec"], NrPeriods: values["nr_periods"],
			NrThrottled: values["nr_throttled"], ThrottledUS: values["throttled_usec"],
			Source: path, Present: true,
		}
		if result.UsageUS == 0 && values["usage_ns"] > 0 {
			result.UsageUS = values["usage_ns"] / 1000
		}
		if result.ThrottledUS == 0 && values["throttled_time"] > 0 {
			result.ThrottledUS = values["throttled_time"] / 1000
		}
		return result
	}
	return cgroupCPUStats{}
}

func readCgroupMemoryEvents() cgroupMemoryEvents {
	for _, path := range currentCgroupPaths("memory.events") {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		values := parseKeyValueCounters(string(data))
		return cgroupMemoryEvents{
			Low: values["low"], High: values["high"], Max: values["max"],
			OOM: values["oom"], OOMKill: values["oom_kill"], OOMGroupKill: values["oom_group_kill"],
			Source: path, Present: true,
		}
	}
	for _, path := range append(cgroupCandidatePaths(cgroupV1Mem, "memory.failcnt"), cgroupCandidatePaths(cgroupV2Root, "memory.failcnt")...) {
		value, err := strconv.ParseUint(strings.TrimSpace(readTrimmed(path, "")), 10, 64)
		if err == nil {
			return cgroupMemoryEvents{FailCount: value, Source: path, Present: true}
		}
	}
	return cgroupMemoryEvents{}
}

func readCPUSet() (string, int, string) {
	for _, file := range []string{"cpuset.cpus.effective", "cpuset.cpus"} {
		for _, path := range currentCgroupPaths(file) {
			value := strings.TrimSpace(readTrimmed(path, ""))
			if value == "" {
				continue
			}
			return value, cpuSetCount(value), path
		}
	}
	return "", 0, ""
}

func cpuSetCount(value string) int {
	total := 0
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		startText, endText, ranged := strings.Cut(part, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 0 {
			continue
		}
		end := start
		if ranged {
			end, err = strconv.Atoi(endText)
			if err != nil || end < start {
				continue
			}
		}
		total += end - start + 1
	}
	return total
}

func readCgroupLimit(v2File, v1File string) (uint64, string, bool, bool) {
	for _, path := range currentCgroupPaths(v2File) {
		text := strings.TrimSpace(readTrimmed(path, ""))
		if text == "max" {
			return 0, path, true, true
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err == nil && value < cgroupV1Unlimited {
			return value, path, false, true
		}
	}
	for _, path := range cgroupCandidatePaths(cgroupV1Mem, v1File) {
		value, err := strconv.ParseUint(strings.TrimSpace(readTrimmed(path, "")), 10, 64)
		if err == nil {
			return value, path, value >= cgroupV1Unlimited, true
		}
	}
	return 0, "", false, false
}

func counterDelta(before, after uint64) (uint64, bool) {
	if after < before {
		return 0, false
	}
	return after - before, true
}

func pressurePercent(before, after psiValues, elapsed time.Duration) (float64, bool) {
	elapsedUS := elapsed.Microseconds()
	if !before.Present || !after.Present || elapsedUS <= 0 {
		return 0, false
	}
	delta, ok := counterDelta(before.TotalUS, after.TotalUS)
	if !ok {
		return 0, false
	}
	percent := float64(delta) / float64(elapsedUS) * 100
	return math.Max(0, math.Min(100, percent)), true
}

func environmentMeasurement(key, label string, value float64, unit, display, method string) model.Measurement {
	return model.Measurement{
		Key: key, Label: label, Value: value, Unit: unit, Display: display,
		Method: method, HigherIsBetter: model.BoolPtr(false),
	}
}

// AssessBenchmarkInterference compares read-only snapshots around one run.
// Thresholds are deliberately conservative: PSI generated by the workload
// itself is reported but only pre-existing PSI contributes to automatic retry.
func AssessBenchmarkInterference(module string, before, after EnvironmentSnapshot) model.Interference {
	elapsed := after.CapturedAt.Sub(before.CapturedAt)
	assessment := model.Interference{}
	add := func(key, label string, value float64, unit, display, method string) {
		assessment.Measurements = append(assessment.Measurements, environmentMeasurement(key, label, value, unit, display, method))
	}
	reason := func(weight int, text string) {
		assessment.Detected = true
		assessment.Score += weight
		assessment.Reasons = append(assessment.Reasons, text)
	}
	workers := before.Limits.CPU.Threads
	if workers < 1 {
		workers = 1
	}
	if before.LoadKnown {
		add("pretest_load_1m", "测试前 1 分钟负载", before.Load1, "load", fmt.Sprintf("%.2f", before.Load1), "proc-loadavg-v1")
		if before.Load1 > float64(workers)*1.5 {
			reason(2, fmt.Sprintf("测试前 load %.2f 高于 %d CPU allowance 的 1.5 倍", before.Load1, workers))
		}
	}
	if before.CPUTracked && after.CPUTracked {
		if steal, ok := stealPercent(before.CPUTimes, after.CPUTimes); ok {
			add("cpu_steal_percent_window", "测试窗口 CPU steal", steal, "%", fmt.Sprintf("%.2f %%", steal), "proc-stat-steal-window-v1")
			// 阈值与 system 模块的累计 steal 告警保持一致。
			//
			// 取 5% 而不是 1%：同一台机器连续三轮的实测波动就有 ±5–8%
			// （randread 29.12 / 26.90 / 27.01 MiB/s），1% steal 造成的影响
			// 小于测量本身的噪声。而超卖 VPS 上 1% steal 是常态，按它复测
			// 等于把每个重型基准都跑两遍，收益却被噪声淹没。
			if steal >= stealInterferenceThreshold {
				reason(3, fmt.Sprintf("测试窗口 CPU steal %.2f%%", steal))
			}
		}
	}
	if before.CPUStat.Present && after.CPUStat.Present && before.CPUStat.Source == after.CPUStat.Source {
		if events, ok := counterDelta(before.CPUStat.NrThrottled, after.CPUStat.NrThrottled); ok {
			add("cgroup_cpu_throttled_events_window", "cgroup CPU throttle 次数", float64(events), "events", strconv.FormatUint(events, 10), "cgroup-cpu-stat-window-v1")
			throttledUS, timeOK := counterDelta(before.CPUStat.ThrottledUS, after.CPUStat.ThrottledUS)
			if timeOK && elapsed.Microseconds() > 0 {
				percent := float64(throttledUS) / float64(elapsed.Microseconds()) * 100
				add("cgroup_cpu_throttled_time_percent_window", "cgroup CPU throttle 时间占比", percent, "%", fmt.Sprintf("%.2f %%", percent), "cgroup-cpu-stat-window-v1")
				if events > 0 && percent >= 1 {
					reason(3, fmt.Sprintf("cgroup CPU throttle %d 次 / %.2f%% 窗口时间", events, percent))
				}
			}
		}
	}
	for _, resource := range []string{"cpu", "memory", "io"} {
		pre := before.PSI[resource]
		post := after.PSI[resource]
		if pre.Some.Present {
			add(resource+"_psi_some_avg10_pretest", strings.ToUpper(resource)+" PSI some avg10（测试前）", pre.Some.Avg10, "%", fmt.Sprintf("%.2f %%", pre.Some.Avg10), "linux-psi-avg10-v1")
		}
		if percent, ok := pressurePercent(pre.Some, post.Some, elapsed); ok {
			add(resource+"_psi_some_percent_window", strings.ToUpper(resource)+" PSI some（测试窗口）", percent, "%", fmt.Sprintf("%.2f %%", percent), "linux-psi-total-window-v1")
		}
		if percent, ok := pressurePercent(pre.Full, post.Full, elapsed); ok {
			add(resource+"_psi_full_percent_window", strings.ToUpper(resource)+" PSI full（测试窗口）", percent, "%", fmt.Sprintf("%.2f %%", percent), "linux-psi-total-window-v1")
		}
	}
	preCPU, preMemory, preIO := before.PSI["cpu"].Some.Avg10, before.PSI["memory"].Some.Avg10, before.PSI["io"].Some.Avg10
	if before.PSI["cpu"].Some.Present && preCPU >= 20 {
		reason(2, fmt.Sprintf("测试前 CPU PSI some avg10 %.2f%%", preCPU))
	}
	if before.PSI["memory"].Some.Present && preMemory >= 2 && (module == "memory" || module == "disk") {
		reason(2, fmt.Sprintf("测试前 memory PSI some avg10 %.2f%%", preMemory))
	}
	if before.PSI["io"].Some.Present && preIO >= 5 && module == "disk" {
		reason(2, fmt.Sprintf("测试前 I/O PSI some avg10 %.2f%%", preIO))
	}
	if before.Memory.Present && after.Memory.Present && before.Memory.Source == after.Memory.Source {
		for _, event := range []struct {
			key, label    string
			before, after uint64
		}{
			{"cgroup_memory_high_events_window", "cgroup memory.high 事件", before.Memory.High, after.Memory.High},
			{"cgroup_memory_max_events_window", "cgroup memory.max 事件", before.Memory.Max, after.Memory.Max},
			{"cgroup_oom_events_window", "cgroup OOM 事件", before.Memory.OOM, after.Memory.OOM},
			{"cgroup_oom_kill_events_window", "cgroup OOM kill 事件", before.Memory.OOMKill, after.Memory.OOMKill},
		} {
			if delta, ok := counterDelta(event.before, event.after); ok {
				add(event.key, event.label, float64(delta), "events", strconv.FormatUint(delta, 10), "cgroup-memory-events-window-v1")
				if delta > 0 && (event.key == "cgroup_oom_events_window" || event.key == "cgroup_oom_kill_events_window") {
					reason(5, fmt.Sprintf("%s增加 %d", event.label, delta))
				}
			}
		}
	}
	return assessment
}

// AppendSystemResourceDiagnostics adds current limits and cumulative pressure
// counters to the system module in a compact table plus machine measurements.
func AppendSystemResourceDiagnostics(result *model.Result, snapshot EnvironmentSnapshot) {
	if result == nil {
		return
	}
	limits := snapshot.Limits
	result.Fields = append(result.Fields,
		model.Field{Key: "cgroup_cpu_quota", Label: "cgroup CPU 真实配额", Value: describeCPUAllowance(limits.CPU)},
		model.Field{Key: "cgroup_cpuset", Label: "cgroup CPU 集合", Value: fallback(limits.CPUSet, "unavailable")},
		model.Field{Key: "cgroup_memory_limit", Label: "cgroup 内存上限", Value: formatOptionalLimit(limits.MemoryLimit, limits.MemoryLimitVia)},
		model.Field{Key: "cgroup_memory_current", Label: "cgroup 当前内存", Value: formatOptionalLimit(limits.MemoryCurrent, limits.MemoryCurrentVia)},
		model.Field{Key: "cgroup_memory_swap_limit", Label: "cgroup Swap 上限", Value: formatSwapLimit(limits)},
	)
	if limits.CPU.Quota > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "cgroup_cpu_quota_cores", Label: "cgroup CPU 配额", Value: limits.CPU.Quota,
			Unit: "cores", Display: fmt.Sprintf("%.2f cores", limits.CPU.Quota), Method: "cgroup-cpu-quota-v1",
		})
	}
	if limits.CPUSetCount > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "cgroup_cpuset_cpus", Label: "cgroup CPU 集合数量", Value: float64(limits.CPUSetCount),
			Unit: "cores", Display: strconv.Itoa(limits.CPUSetCount), Method: "cgroup-cpuset-effective-v1",
		})
	}
	for _, item := range []struct {
		key, label string
		value      uint64
	}{
		{"cgroup_memory_limit_bytes", "cgroup 内存上限", limits.MemoryLimit},
		{"cgroup_memory_current_bytes", "cgroup 当前内存", limits.MemoryCurrent},
	} {
		if item.value > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: item.key, Label: item.label, Value: float64(item.value), Unit: "bytes",
				Display: model.FormatBytes(item.value), Method: "cgroup-memory-limit-v1",
			})
		}
	}
	table := model.Table{
		Key:                   "system.pressure.cgroup",
		Title:                 "cgroup 与 PSI 压力诊断",
		Columns:               []string{"资源", "some avg10", "full avg10", "累计事件", "来源"},
		ColumnKeys:            []string{"resource", "psi_some_avg10", "psi_full_avg10", "cumulative_events", "source"},
		RowIdentity:           "resource",
		NumericColumns:        []int{1, 2},
		NumericHigherIsBetter: []bool{false, false},
	}
	for _, resource := range []string{"cpu", "memory", "io"} {
		pressure := snapshot.PSI[resource]
		some, full := "—", "—"
		if pressure.Some.Present {
			some = fmt.Sprintf("%.2f %%", pressure.Some.Avg10)
			result.Measurements = append(result.Measurements, environmentMeasurement(resource+"_psi_some_avg10", strings.ToUpper(resource)+" PSI some avg10", pressure.Some.Avg10, "%", some, "linux-psi-avg10-v1"))
		}
		if pressure.Full.Present {
			full = fmt.Sprintf("%.2f %%", pressure.Full.Avg10)
			result.Measurements = append(result.Measurements, environmentMeasurement(resource+"_psi_full_avg10", strings.ToUpper(resource)+" PSI full avg10", pressure.Full.Avg10, "%", full, "linux-psi-avg10-v1"))
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
		table.Rows = append(table.Rows, []string{strings.ToUpper(resource), some, full, events, fallback(pressure.Source, "unavailable")})
	}
	result.Tables = append(result.Tables, table)
	if snapshot.CPUStat.Present {
		result.Measurements = append(result.Measurements,
			environmentMeasurement("cgroup_cpu_nr_throttled", "cgroup CPU throttle 累计次数", float64(snapshot.CPUStat.NrThrottled), "events", strconv.FormatUint(snapshot.CPUStat.NrThrottled, 10), "cgroup-cpu-stat-cumulative-v1"),
			environmentMeasurement("cgroup_cpu_throttled_seconds", "cgroup CPU throttle 累计时间", float64(snapshot.CPUStat.ThrottledUS)/1e6, "s", fmt.Sprintf("%.3f s", float64(snapshot.CPUStat.ThrottledUS)/1e6), "cgroup-cpu-stat-cumulative-v1"),
		)
	}
	if snapshot.Memory.Present {
		result.Measurements = append(result.Measurements,
			environmentMeasurement("cgroup_oom_events", "cgroup OOM 累计事件", float64(snapshot.Memory.OOM), "events", strconv.FormatUint(snapshot.Memory.OOM, 10), "cgroup-memory-events-cumulative-v1"),
			environmentMeasurement("cgroup_oom_kill_events", "cgroup OOM kill 累计事件", float64(snapshot.Memory.OOMKill), "events", strconv.FormatUint(snapshot.Memory.OOMKill, 10), "cgroup-memory-events-cumulative-v1"),
		)
	}
}

func formatOptionalLimit(value uint64, source string) string {
	if value == 0 {
		return "unlimited / unavailable"
	}
	if source == "" {
		return model.FormatBytes(value)
	}
	return model.FormatBytes(value) + "（" + source + "）"
}

func formatSwapLimit(limits resourceLimits) string {
	if limits.MemorySwapMax {
		return "unlimited（" + limits.MemorySwapVia + "）"
	}
	return formatOptionalLimit(limits.MemorySwapLimit, limits.MemorySwapVia)
}

// AppendInterferenceDiagnostics exposes one attempt's window in the ordinary
// report sections, so TXT/Markdown/HTML get the same facts as JSON.
func AppendInterferenceDiagnostics(result *model.Result, assessment model.Interference) {
	if result == nil {
		return
	}
	result.Measurements = append(result.Measurements, assessment.Measurements...)
	rows := make([][]string, 0, len(assessment.Measurements))
	for _, measurement := range assessment.Measurements {
		rows = append(rows, []string{measurement.Label, measurement.Display, measurement.Method})
	}
	if len(rows) > 0 {
		result.Tables = append(result.Tables, model.Table{
			Key: "system.pressure.interference", Title: "测试窗口资源干扰",
			Columns: []string{"指标", "数值", "方法"}, ColumnKeys: []string{"metric", "value", "method"}, Rows: rows,
		})
	}
	if assessment.Detected {
		result.Status = model.StatusWarning
		for _, reason := range assessment.Reasons {
			result.Notes = append(result.Notes, "检测到测试干扰："+reason+"。")
		}
	}
}

func retryAttempt(number int, result model.Result, assessment model.Interference) model.RetryAttempt {
	var evidence *model.Evidence
	if result.Evidence != nil {
		copy := *result.Evidence
		evidence = &copy
	}
	return model.RetryAttempt{
		Number: number, Status: result.Status, DurationMS: result.DurationMS, Evidence: evidence,
		Interference: assessment, Measurements: append([]model.Measurement(nil), result.Measurements...),
	}
}

// FinalizeBenchmarkRetry chooses the less-interfered attempt, preserves the
// other attempt's raw blocks, and adds a compact attempt table.
func FinalizeBenchmarkRetry(first model.Result, firstInterference model.Interference, second model.Result, secondInterference model.Interference) model.Result {
	selected, selectedNumber := first, 1
	firstUsable, secondUsable := retryResultUsable(first), retryResultUsable(second)
	if (!firstUsable && secondUsable) || (firstUsable == secondUsable && secondInterference.Score < firstInterference.Score) {
		selected, selectedNumber = second, 2
	}
	other, otherNumber := second, 2
	if selectedNumber == 2 {
		other, otherNumber = first, 1
	}
	selected.StartedAt = first.StartedAt
	selected.DurationMS = first.DurationMS + second.DurationMS
	for _, block := range other.TextBlocks {
		block.Title = fmt.Sprintf("自动复测第 %d 轮 · %s", otherNumber, block.Title)
		selected.TextBlocks = append(selected.TextBlocks, block)
	}
	selected.Retry = &model.RetryInfo{
		Triggered: true, SelectedAttempt: selectedNumber,
		SelectionRule:  "先排除无有效证据的轮次，再选择干扰评分较低的一轮；同分保留首次结果，不按性能高低挑选",
		TriggerReasons: append([]string(nil), firstInterference.Reasons...),
		Attempts: []model.RetryAttempt{
			retryAttempt(1, first, firstInterference), retryAttempt(2, second, secondInterference),
		},
	}
	selected.Fields = append(selected.Fields,
		model.Field{Key: "automatic_retry", Label: "自动复测", Value: "已触发一次"},
		model.Field{Key: "retry_selected_attempt", Label: "采用轮次", Value: strconv.Itoa(selectedNumber)},
		model.Field{Key: "retry_selection_rule", Label: "采用规则", Value: selected.Retry.SelectionRule},
	)
	rows := [][]string{
		{"1", string(first.Status), retryEvidenceText(first.Evidence), strconv.Itoa(firstInterference.Score), strings.Join(firstInterference.Reasons, "；"), retrySelectionText(selectedNumber == 1)},
		{"2", string(second.Status), retryEvidenceText(second.Evidence), strconv.Itoa(secondInterference.Score), strings.Join(secondInterference.Reasons, "；"), retrySelectionText(selectedNumber == 2)},
	}
	selected.Tables = append(selected.Tables, model.Table{
		Key: "system.pressure.retry", Title: "自动复测判定",
		Columns:     []string{"轮次", "状态", "证据", "干扰评分", "检测依据", "采用"},
		ColumnKeys:  []string{"attempt", "status", "evidence", "interference_score", "reasons", "selected"},
		RowIdentity: "attempt", Rows: rows,
		NumericColumns: []int{3}, NumericHigherIsBetter: []bool{false},
	})
	selected.Notes = append(selected.Notes, "仅因检测到环境干扰而自动复测一次；先排除无有效证据的轮次，再按干扰评分选择，不按性能数字高低挑选。")
	return selected
}

func retryResultUsable(result model.Result) bool {
	return result.Status != model.StatusError && result.Status != model.StatusSkipped &&
		result.Evidence != nil && result.Evidence.Valid > 0
}

func retryEvidenceText(evidence *model.Evidence) string {
	if evidence == nil {
		return "—"
	}
	return fmt.Sprintf("%d/%d", evidence.Valid, evidence.Expected)
}

func retrySelectionText(selected bool) string {
	if selected {
		return "采用"
	}
	return "保留供复核"
}
