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
	sysbenchEventsRatePattern = regexp.MustCompile(`(?m)^\s*events per second:\s*([0-9]+(?:\.[0-9]+)?)\s*$`)
	sysbenchEventsPattern     = regexp.MustCompile(`(?m)^\s*total number of events:\s*([0-9]+)\s*$`)
	sysbenchP95Pattern        = regexp.MustCompile(`(?m)^\s*95th percentile:\s*([0-9]+(?:\.[0-9]+)?)\s*$`)
)

// stealNoticeThreshold 是在报告里标注 steal 的门槛（百分比）。
//
// 它刻意比 stealInterferenceThreshold 低：标注只是多一行说明，代价为零，
// 因此值得对轻微争抢也如实提示；而自动复测要把整个基准重跑一遍，代价远高，
// 需要 steal 大到确实超过测量噪声才划算。两者不是同一个判断。
const stealNoticeThreshold = 1.0

type sysbenchCPUResult struct {
	Rate   float64
	Events uint64
	P95MS  float64
	Output string
	Args   []string
}

type cpuProbe struct{}

func (cpuProbe) ID() string         { return "cpu" }
func (cpuProbe) Title() string      { return "CPU 性能" }
func (cpuProbe) NeedsNetwork() bool { return false }

func (cpuProbe) Run(ctx context.Context, env Environment) model.Result {
	if path, err := exec.LookPath("sysbench"); err == nil {
		return runSysbenchCPU(ctx, env, path)
	}
	start := time.Now()
	result := model.NewResult("cpu", "CPU 性能")
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "sysbench",
		Profile:         "cpu prime=20000",
		ComparisonScope: "相同 sysbench 版本、prime、线程数与时长",
	}
	result.Status = model.StatusWarning
	result.Summary = "未找到 sysbench，标准 CPU 基准未运行"
	result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: "sysbench", Count: 1, Message: result.Summary})
	result.Evidence = model.NewEvidence(0, 2, "run")
	result.Notes = append(result.Notes, "可用 run.sh 从当前架构的已校验 ecs-tools 包临时提供 sysbench，或运行 install.sh --with-benchmarks 持久安装。ecs 不提供自研替代分数。")
	result.Finish(start)
	return result
}

func runSysbenchCPU(ctx context.Context, env Environment, path string) model.Result {
	start := time.Now()
	result := model.NewResult("cpu", "CPU 性能")
	result.Description = "sysbench CPU 素数计算的单线程与多线程标准化工作负载"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "sysbench",
		Profile:         "cpu prime=20000",
		ComparisonScope: "相同 sysbench 版本、prime=20000、线程数与时长",
	}

	seconds := int((env.Config.CPUTime + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	allowance := detectCPUAllowance()
	workers := allowance.Threads
	load1, loadKnown := readLoadAverage1()

	// steal 只有在压满 CPU 的窗口内测才有意义：空闲时宿主机没有争抢对象，
	// 读到的比例会偏低。这里夹住两轮 sysbench 的整个执行区间。
	stealBefore, stealTracked := readCPUTimes()

	single, err := executeSysbenchCPU(ctx, path, 1, seconds)
	if err != nil {
		result.Fail(fmt.Errorf("sysbench 单线程 CPU 基准: %w", err))
		result.Evidence = model.NewEvidence(0, 2, "run")
		result.Finish(start)
		return result
	}
	multi, err := executeSysbenchCPU(ctx, path, workers, seconds)
	if err != nil {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "sysbench 多线程 CPU 基准失败: "+err.Error())
	}

	steal, stealOK := 0.0, false
	if stealTracked {
		if after, ok := readCPUTimes(); ok {
			steal, stealOK = stealPercent(stealBefore, after)
		}
	}

	result.Measurements = []model.Measurement{
		{
			Key: "sysbench_cpu_single_events_s", Label: "单线程事件率",
			Value: single.Rate, Unit: "events/s", Display: model.FormatRate(single.Rate, "events/s"),
			Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	if single.P95MS > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "sysbench_cpu_single_p95_ms", Label: "单线程事件延迟 P95",
			Value: single.P95MS, Unit: "ms", Display: fmt.Sprintf("%.2f ms", single.P95MS),
			Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if multi.Rate > 0 {
		scaling := multi.Rate / single.Rate
		efficiency := scaling / float64(workers) * 100
		result.Measurements = append(result.Measurements,
			model.Measurement{
				Key: "sysbench_cpu_multi_events_s", Label: fmt.Sprintf("%d 线程事件率", workers),
				Value: multi.Rate, Unit: "events/s", Display: model.FormatRate(multi.Rate, "events/s"),
				Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
			},
			model.Measurement{
				Key: "sysbench_cpu_scaling_ratio", Label: "多线程扩展倍率",
				Value: scaling, Unit: "x", Display: fmt.Sprintf("%.2f×", scaling),
				Method: "sysbench-cpu-scaling-v1", HigherIsBetter: model.BoolPtr(true),
			},
			model.Measurement{
				Key: "sysbench_cpu_per_thread_efficiency_percent", Label: "每线程扩展效率",
				Value: efficiency, Unit: "%", Display: fmt.Sprintf("%.1f %%", efficiency),
				Method: "sysbench-cpu-scaling-v1", HigherIsBetter: model.BoolPtr(true),
			},
		)
		if multi.P95MS > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "sysbench_cpu_multi_p95_ms", Label: fmt.Sprintf("%d 线程事件延迟 P95", workers),
				Value: multi.P95MS, Unit: "ms", Display: fmt.Sprintf("%.2f ms", multi.P95MS),
				Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(false),
			})
		}
	}
	if stealOK {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "cpu_steal_percent_during_test", Label: "测试期间 CPU steal",
			Value: steal, Unit: "%", Display: fmt.Sprintf("%.2f %%", steal),
			Method: "proc-stat-steal-delta-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	version := commandVersion(ctx, path)
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "sysbench"},
		{Key: "version", Label: "sysbench 版本", Value: version},
		{Key: "binary_sha256", Label: "sysbench SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "threads", Label: "测试线程", Value: fmt.Sprintf("1 / %d", workers)},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: describeCPUAllowance(allowance)},
		{Key: "duration", Label: "每轮时长", Value: fmt.Sprintf("%ds", seconds)},
		{Key: "prime", Label: "最大素数", Value: "20000"},
		{Key: "single_events", Label: "单线程总事件", Value: formatSysbenchEvents(single)},
		{Key: "multi_events", Label: "多线程总事件", Value: formatSysbenchEvents(multi)},
	}
	validity := "有效"
	if allowance.Limited() {
		validity = "按 CPU 配额有效"
	}
	if multi.Rate <= 0 {
		validity = "部分有效"
	}
	if (stealOK && steal >= stealNoticeThreshold) || (loadKnown && load1 > float64(workers)*1.5) {
		validity = "受干扰，建议复测"
	}
	result.Fields = append(result.Fields, model.Field{Key: "result_validity", Label: "成绩有效性", Value: validity})
	if loadKnown {
		result.Fields = append(result.Fields, model.Field{
			Key: "pretest_load_1m", Label: "测试前 1 分钟负载", Value: fmt.Sprintf("%.2f", load1),
		})
	}
	result.TextBlocks = []model.TextBlock{
		{Title: "sysbench 单线程原始输出", Language: "text", Content: single.Output},
	}
	if multi.Output != "" {
		result.TextBlocks = append(result.TextBlocks, model.TextBlock{Title: "sysbench 多线程原始输出", Language: "text", Content: multi.Output})
	}
	result.Sources = []model.Source{
		{Name: "sysbench", URL: "https://github.com/akopytov/sysbench", Purpose: "CPU 标准化工作负载与统计"},
	}
	result.Notes = append(result.Notes,
		"成绩不与不同 sysbench 版本、prime 值、线程数或测试时长混算。",
		"sysbench CPU 是素数计算微基准，不等同于 Geekbench/SPEC 的综合应用负载。",
	)
	if allowance.Limited() {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"检测到 CPU 配额 %.2f 核（%s），低于可见的 %d 个逻辑核；多线程测试按配额使用 %d 线程，避免超开导致成绩失真。",
			allowance.Quota, allowance.Source, allowance.Visible, allowance.Threads,
		))
	}
	if stealOK && steal >= stealNoticeThreshold {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, fmt.Sprintf(
			"测试期间 CPU steal 约 %.2f%%，宿主机存在争抢；本轮成绩会被压低，建议错峰复测。", steal,
		))
	}
	if loadKnown && load1 > float64(workers)*1.5 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, fmt.Sprintf(
			"测试前 1 分钟负载为 %.2f，超过本轮 %d 线程 allowance 的 1.5 倍；后台任务可能压低成绩，建议空闲时复测。", load1, workers,
		))
	}
	validRuns := 1
	if multi.Rate > 0 {
		validRuns++
	}
	result.Evidence = model.NewEvidence(validRuns, 2, "run")
	if multi.Rate > 0 {
		result.Summary = fmt.Sprintf("sysbench 单线程 %s · %d 线程 %s",
			model.FormatRate(single.Rate, "events/s"), workers, model.FormatRate(multi.Rate, "events/s"))
	} else {
		result.Summary = "sysbench 单线程 " + model.FormatRate(single.Rate, "events/s")
	}
	result.Finish(start)
	return result
}

func readLoadAverage1() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	return value, err == nil && value >= 0
}

func formatSysbenchEvents(sample sysbenchCPUResult) string {
	if sample.Rate <= 0 || sample.Events == 0 {
		return "unavailable"
	}
	return strconv.FormatUint(sample.Events, 10)
}

func executeSysbenchCPU(ctx context.Context, path string, threads, seconds int) (sysbenchCPUResult, error) {
	args := []string{
		"--threads=" + strconv.Itoa(threads),
		"--time=" + strconv.Itoa(seconds),
		"--events=0",
		"--percentile=95",
		"cpu",
		"--cpu-max-prime=20000",
		"run",
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds+10)*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := command.CombinedOutput()
	text := sanitizeCommandOutput(output)
	if runCtx.Err() != nil {
		return sysbenchCPUResult{Output: text, Args: args}, runCtx.Err()
	}
	if err != nil {
		return sysbenchCPUResult{Output: text, Args: args}, fmt.Errorf("%w: %s", err, tailText(text, 400))
	}
	rate, ok := parseFirstFloat(sysbenchEventsRatePattern, text)
	if !ok || rate <= 0 {
		return sysbenchCPUResult{Output: text, Args: args}, fmt.Errorf("未解析到 events per second")
	}
	events, _ := parseFirstUint(sysbenchEventsPattern, text)
	p95, ok := parseFirstFloat(sysbenchP95Pattern, text)
	if !ok || p95 <= 0 {
		return sysbenchCPUResult{Rate: rate, Events: events, Output: text, Args: args}, fmt.Errorf("未解析到有效的 95th percentile 延迟")
	}
	return sysbenchCPUResult{Rate: rate, Events: events, P95MS: p95, Output: text, Args: args}, nil
}

func commandVersion(ctx context.Context, path string) string {
	for _, argument := range []string{"--version", "-V"} {
		versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, err := exec.CommandContext(versionCtx, path, argument).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(sanitizeCommandOutput(output), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return "unknown"
}

func parseFirstFloat(pattern *regexp.Regexp, text string) (float64, bool) {
	match := pattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	return value, err == nil
}

func parseFirstUint(pattern *regexp.Regexp, text string) (uint64, bool) {
	match := pattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseUint(match[1], 10, 64)
	return value, err == nil
}

func tailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}
