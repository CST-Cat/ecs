package probe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

var (
	sysbenchMemoryRatePattern       = regexp.MustCompile(`(?mi)\(([0-9]+(?:\.[0-9]+)?)\s+([KMGT]?i?B)/sec\)`)
	sysbenchMemoryTotalTimePattern  = regexp.MustCompile(`(?mi)^\s*total time:\s*([0-9]+(?:\.[0-9]+)?)s\s*$`)
	sysbenchMemoryEventsPattern     = regexp.MustCompile(`(?mi)^\s*total number of events:\s*([0-9]+)\s*$`)
	sysbenchMemoryAvgLatencyPattern = regexp.MustCompile(`(?mi)^\s*avg:\s*([0-9]+(?:\.[0-9]+)?)(?:\s*ms)?\s*$`)
)

type sysbenchMemoryResult struct {
	RateMiBS       float64
	LatencyMS      float64
	LatencyP95MS   float64
	LatencyDerived bool
	Output         string
	Args           []string
}

type sysbenchMemoryRun struct {
	name      string
	operation string
	threads   int
	sample    sysbenchMemoryResult
	err       error
}

type memoryProbe struct{}

func (memoryProbe) ID() string         { return "memory" }
func (memoryProbe) Title() string      { return "内存性能" }
func (memoryProbe) NeedsNetwork() bool { return false }

func (memoryProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	// /proc/meminfo 与 cgroup 的有效视图决定 mbw 的数组大小，小内存机器上
	// 不能贸然分配。缺少 MemAvailable 时 helper 会保留旧的安全回退口径。
	mem := parseMemInfo("/proc/meminfo")
	limit, _, _ := cgroupMemoryLimit()
	memory := memoryUsageFromMemInfo(mem, limit)
	if memory.LimitApplied {
		if current, _, ok := cgroupMemoryCurrent(); ok && current <= memory.EffectiveTotalBytes {
			memory.EffectiveUsedBytes = current
			memory.EffectiveAvailableBytes = memory.EffectiveTotalBytes - current
			memory.EffectiveUsagePercent = float64(current) / float64(memory.EffectiveTotalBytes) * 100
			memory.EffectiveCurrentKnown = true
		}
	}
	available := memory.EffectiveAvailableBytes

	var result model.Result
	if path, err := exec.LookPath("sysbench"); err == nil {
		result = runSysbenchMemory(ctx, env, path)
	} else {
		result = model.NewResult("memory", "内存性能")
		result.Methodology = model.Methodology{
			Kind:            "standard-benchmark",
			Label:           "标准基准",
			Engine:          "sysbench",
			Profile:         "memory seq 1 MiB",
			ComparisonScope: "相同 sysbench 版本、操作、块大小、线程数与时长",
		}
		result.Status = model.StatusWarning
		result.Summary = "未找到 sysbench，标准内存基准未运行"
		result.Notes = append(result.Notes, "可用 run.sh 自动临时准备 sysbench，或运行 install.sh --with-benchmarks 持久安装。ecs 不提供自研替代分数。")
	}

	appendMemoryInventory(&result, memory, detectBalloonReclaim("/sys", "/proc/vmstat"), detectKSM("/sys"))
	// mbw 是补充口径，缺席不降级整个模块。
	appendMBWMemory(ctx, &result, available)
	result.Finish(start)
	return result
}

func runSysbenchMemory(ctx context.Context, env Environment, path string) model.Result {
	start := time.Now()
	result := model.NewResult("memory", "内存性能")
	result.Description = "sysbench memory 顺序读写的单线程与多线程标准化工作负载"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "sysbench",
		Profile:         "memory seq 1 MiB",
		ComparisonScope: "相同 sysbench 版本、读写操作、1 MiB 块、线程数与时长",
	}

	seconds := int((env.Config.CPUTime + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	allowance := detectCPUAllowance()
	workers := allowance.Threads

	runs := []sysbenchMemoryRun{
		{name: "单线程写入", operation: "write", threads: 1},
		{name: "多线程写入", operation: "write", threads: workers},
		{name: "单线程读取", operation: "read", threads: 1},
		{name: "多线程读取", operation: "read", threads: workers},
	}
	for index := range runs {
		runs[index].sample, runs[index].err = executeSysbenchMemory(ctx, path, runs[index].operation, runs[index].threads, seconds)
		if runs[index].err != nil {
			result.Status = model.StatusWarning
			result.Notes = append(result.Notes, "sysbench "+runs[index].name+"失败: "+runs[index].err.Error())
		}
	}

	appendMetric := func(key, label string, sample sysbenchMemoryResult, threads int, operation, contextName string) {
		if sample.RateMiBS <= 0 {
			// Keep throughput and latency evidence independent: a valid timing
			// statistic may still be useful when the rate parser rejects a run.
		} else {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: key, Label: label, Value: sample.RateMiBS, Unit: "MiB/s",
				Display:        model.FormatRate(sample.RateMiBS, "MiB/s"),
				Method:         fmt.Sprintf("sysbench-memory-seq-1MiB-%s-%dt-v1", operation, threads),
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		if sample.LatencyMS <= 0 {
			return
		}
		method := fmt.Sprintf("sysbench-memory-latency-avg-1MiB-%s-%dt-v1", operation, threads)
		if sample.LatencyDerived {
			method = fmt.Sprintf("sysbench-memory-latency-total-time-div-events-1MiB-%s-%dt-v1", operation, threads)
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "sysbench_memory_" + operation + "_" + contextName + "_latency_ms",
			Label: label + "平均时延" + func() string {
				if sample.LatencyDerived {
					return "（派生）"
				}
				return ""
			}(),
			Value: sample.LatencyMS, Unit: "ms", Display: fmt.Sprintf("%.3f ms", sample.LatencyMS),
			Method: method, HigherIsBetter: model.BoolPtr(false),
		})
	}
	appendMetric("sysbench_memory_write_single_mib_s", "单线程顺序写", runs[0].sample, 1, "write", "single")
	appendMetric("sysbench_memory_write_multi_mib_s", fmt.Sprintf("%d 线程顺序写", workers), runs[1].sample, workers, "write", "multi")
	appendMetric("sysbench_memory_read_single_mib_s", "单线程顺序读", runs[2].sample, 1, "read", "single")
	appendMetric("sysbench_memory_read_multi_mib_s", fmt.Sprintf("%d 线程顺序读", workers), runs[3].sample, workers, "read", "multi")

	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "sysbench"},
		{Key: "version", Label: "sysbench 版本", Value: commandVersion(ctx, path)},
		{Key: "binary_sha256", Label: "sysbench SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "threads", Label: "测试线程", Value: fmt.Sprintf("1 / %d", workers)},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: describeCPUAllowance(allowance)},
		{Key: "duration", Label: "每轮时长", Value: fmt.Sprintf("%ds", seconds)},
		{Key: "block_size", Label: "块大小", Value: "1 MiB"},
		{Key: "access_mode", Label: "访问模式", Value: "sequential / global"},
		{Key: "logical_transfer_cap", Label: "逻辑传输上限", Value: "1 TiB/轮（到时先停止）"},
		{Key: "arguments", Label: "参数模板", Value: "sysbench --threads=N --time=S --events=0 memory --memory-block-size=1M --memory-total-size=1T --memory-oper=read|write --memory-access-mode=seq run"},
	}
	for _, raw := range []struct {
		title  string
		sample sysbenchMemoryResult
	}{
		{"单线程写入", runs[0].sample},
		{"多线程写入", runs[1].sample},
		{"单线程读取", runs[2].sample},
		{"多线程读取", runs[3].sample},
	} {
		if raw.sample.Output != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title: "sysbench " + raw.title + "原始输出", Language: "text", Content: raw.sample.Output,
			})
		}
	}
	result.Sources = []model.Source{
		{Name: "sysbench", URL: "https://github.com/akopytov/sysbench", Purpose: "内存访问标准化工作负载与统计"},
	}
	result.Notes = append(result.Notes,
		"成绩不与不同 sysbench 版本、操作方向、块大小、访问模式、线程数或时长混算。",
		"sysbench memory 是微基准，不等同于 STREAM；报告同时保留读/写与单/多线程口径。",
	)
	if runs[0].sample.LatencyDerived || runs[1].sample.LatencyDerived || runs[2].sample.LatencyDerived || runs[3].sample.LatencyDerived {
		result.Notes = append(result.Notes, "sysbench 未返回原生 Latency (ms) 时，时延才会按 total time ÷ total number of events 派生；该公式表示每个 1 MiB 事件的平均耗时，不冒充硬件 DRAM 单次访问延迟。")
	}
	result.Tables = append(result.Tables, sysbenchMemoryTable(runs))
	summary := []string{}
	if runs[0].sample.RateMiBS > 0 {
		summary = append(summary, "写 "+model.FormatRate(runs[0].sample.RateMiBS, "MiB/s"))
	}
	if runs[1].sample.RateMiBS > 0 {
		summary = append(summary, fmt.Sprintf("%dT %s", workers, model.FormatRate(runs[1].sample.RateMiBS, "MiB/s")))
	}
	if runs[2].sample.RateMiBS > 0 {
		summary = append(summary, "读 "+model.FormatRate(runs[2].sample.RateMiBS, "MiB/s"))
	}
	if len(summary) == 0 {
		result.Summary = "sysbench 未产出有效内存吞吐"
	} else {
		result.Summary = "sysbench " + strings.Join(summary, " · ")
	}
	result.Finish(start)
	return result
}

func executeSysbenchMemory(ctx context.Context, path, operation string, threads, seconds int) (sysbenchMemoryResult, error) {
	args := []string{
		"--threads=" + strconv.Itoa(threads),
		"--time=" + strconv.Itoa(seconds),
		"--events=0",
		"memory",
		"--memory-block-size=1M",
		"--memory-total-size=1T",
		"--memory-oper=" + operation,
		"--memory-access-mode=seq",
		"run",
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds+10)*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := command.CombinedOutput()
	text := sanitizeCommandOutput(output)
	if runCtx.Err() != nil {
		return sysbenchMemoryResult{Output: text, Args: args}, runCtx.Err()
	}
	if err != nil {
		return sysbenchMemoryResult{Output: text, Args: args}, fmt.Errorf("%w: %s", err, tailText(text, 400))
	}
	match := sysbenchMemoryRatePattern.FindStringSubmatch(text)
	if len(match) != 3 {
		return sysbenchMemoryResult{Output: text, Args: args}, fmt.Errorf("未解析到 transferred rate")
	}
	value, parseErr := strconv.ParseFloat(match[1], 64)
	if parseErr != nil {
		return sysbenchMemoryResult{Output: text, Args: args}, fmt.Errorf("解析带宽: %w", parseErr)
	}
	rate := memoryRateToMiB(value, match[2])
	if rate <= 0 {
		return sysbenchMemoryResult{Output: text, Args: args}, fmt.Errorf("无效带宽单位 %q", match[2])
	}
	latencyMS, p95MS := parseSysbenchMemoryLatency(text)
	derived := false
	if latencyMS <= 0 {
		if secondsValue, events, ok := parseSysbenchMemoryTiming(text); ok && events > 0 {
			latencyMS = secondsValue * 1000 / float64(events)
			derived = latencyMS > 0
		}
	}
	return sysbenchMemoryResult{RateMiBS: rate, LatencyMS: latencyMS, LatencyP95MS: p95MS, LatencyDerived: derived, Output: text, Args: args}, nil
}

func parseSysbenchMemoryTiming(text string) (seconds float64, events uint64, ok bool) {
	timeMatch := sysbenchMemoryTotalTimePattern.FindStringSubmatch(text)
	eventMatch := sysbenchMemoryEventsPattern.FindStringSubmatch(text)
	if len(timeMatch) != 2 || len(eventMatch) != 2 {
		return 0, 0, false
	}
	seconds, timeErr := strconv.ParseFloat(timeMatch[1], 64)
	events, eventErr := strconv.ParseUint(eventMatch[1], 10, 64)
	if timeErr != nil || eventErr != nil || seconds <= 0 || events == 0 {
		return 0, 0, false
	}
	return seconds, events, true
}

func parseSysbenchMemoryLatency(text string) (avgMS, p95MS float64) {
	avgMatch := sysbenchMemoryAvgLatencyPattern.FindStringSubmatch(text)
	if len(avgMatch) == 2 {
		avgMS, _ = strconv.ParseFloat(avgMatch[1], 64)
	}
	const p95Label = "95th percentile:"
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(p95Label)) {
			continue
		}
		fields := strings.Fields(line)
		for index := len(fields) - 1; index >= 0; index-- {
			value, err := strconv.ParseFloat(fields[index], 64)
			if err == nil && value > 0 {
				p95MS = value
				break
			}
		}
		if p95MS > 0 {
			break
		}
	}
	if avgMS <= 0 {
		return 0, p95MS
	}
	return avgMS, p95MS
}

func sysbenchMemoryTable(runs []sysbenchMemoryRun) model.Table {
	table := model.Table{
		Title:                 "sysbench memory 读写与时延",
		Columns:               []string{"操作 / 线程", "吞吐", "平均时延", "P95 时延", "证据"},
		NumericColumns:        []int{1, 2, 3},
		NumericHigherIsBetter: []bool{true, false, false},
	}
	for _, run := range runs {
		throughput, latency, p95, evidence := "—", "—", "—", "未返回"
		if run.sample.RateMiBS > 0 {
			throughput = model.FormatRate(run.sample.RateMiBS, "MiB/s")
		}
		if run.sample.LatencyMS > 0 {
			latency = fmt.Sprintf("%.3f ms", run.sample.LatencyMS)
			if run.sample.LatencyDerived {
				evidence = "派生：total time / events"
			} else {
				evidence = "sysbench Latency (ms)"
			}
		}
		if run.sample.LatencyP95MS > 0 {
			p95 = fmt.Sprintf("%.3f ms", run.sample.LatencyP95MS)
		}
		if run.err != nil && evidence == "未返回" {
			evidence = "失败"
		}
		table.Rows = append(table.Rows, []string{run.name, throughput, latency, p95, evidence})
	}
	return table
}

func appendMemoryInventory(result *model.Result, memory memoryUsageSnapshot, balloon, ksm memoryFacility) {
	result.Fields = append(result.Fields,
		model.Field{Key: "memory_total", Label: "内存总量", Value: model.FormatBytes(memory.EffectiveTotalBytes)},
		model.Field{Key: "memory_used", Label: "内存已用", Value: model.FormatBytes(memory.EffectiveUsedBytes)},
		model.Field{Key: "memory_available", Label: "内存可用", Value: model.FormatBytes(memory.EffectiveAvailableBytes)},
		model.Field{Key: "memory_usage_percent", Label: "内存使用率", Value: fmt.Sprintf("%.1f %%", memory.EffectiveUsagePercent)},
		model.Field{Key: "balloon_reclaim", Label: "Balloon reclaim", Value: balloon.Status()},
		model.Field{Key: "balloon_reclaim_available", Label: "Balloon reclaim 可用", Value: strconv.FormatBool(balloon.Available)},
		model.Field{Key: "balloon_reclaim_evidence", Label: "Balloon reclaim 证据", Value: fallback(balloon.Evidence, "none found")},
		model.Field{Key: "ksm_merging", Label: "KSM merging", Value: ksm.Status()},
		model.Field{Key: "ksm_merging_available", Label: "KSM merging 可用", Value: strconv.FormatBool(ksm.Available)},
		model.Field{Key: "ksm_merging_evidence", Label: "KSM merging 证据", Value: fallback(ksm.Evidence, "none found")},
	)
	if memory.LimitApplied {
		result.Notes = append(result.Notes, "内存总量与使用率按 cgroup 有效配额计算；/proc/meminfo 的宿主可见值仍由系统模块保留。")
		if memory.EffectiveCurrentKnown {
			result.Notes = append(result.Notes, "cgroup memory.current 提供了有效配额内的已用内存；可用值按配额减当前用量计算。")
		} else {
			result.Notes = append(result.Notes, "cgroup memory.current 不可读；有效配额内的已用/可用值按 MemAvailable 兼容回退计算。")
		}
	}
	if !memory.AvailableKnown {
		result.Notes = append(result.Notes, "MemAvailable unavailable：使用 MemFree + Buffers + Cached 的 Linux 兼容回退计算可用内存。")
	}
	if !balloon.Available {
		result.Notes = append(result.Notes, "Balloon reclaim unavailable：未找到可验证的 Linux sysfs/proc 证据。")
	}
	if !ksm.Available {
		result.Notes = append(result.Notes, "KSM merging unavailable：未找到可验证的 Linux sysfs run/pages_sharing 证据。")
	}
}

func memoryRateToMiB(value float64, unit string) float64 {
	switch strings.ToLower(unit) {
	case "kib", "kb":
		return value / 1024
	case "mib", "mb":
		return value
	case "gib", "gb":
		return value * 1024
	case "tib", "tb":
		return value * 1024 * 1024
	default:
		return 0
	}
}
