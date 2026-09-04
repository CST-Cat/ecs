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

func (cpuProbe) ID() string { return "cpu" }

func (cpuProbe) Run(ctx context.Context, env Environment) model.Result {
	if path, err := LookupTool("sysbench"); err == nil {
		return runSysbenchCPU(ctx, env, path)
	}
	start := time.Now()
	result := model.NewResult("cpu", "module.cpu.title")
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "sysbench",
		Profile:         "cpu prime=20000",
		ComparisonScope: "probe.cpu.comparison_scope.tool_missing",
	}
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "configured_duration", env.Config.CPUTime.String())
	result.Status = model.StatusWarning
	result.SummaryMessages = []model.Message{model.NewMessage("probe.cpu.summary.tool_missing")}
	result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: "sysbench", Count: 1, Message: "executable not found"})
	result.Evidence = model.NewEvidence(0, len(distinctBenchmarkThreadCounts(detectCPUAllowance().Threads)), "run")
	result.Notes = append(result.Notes, "probe.cpu.tool_missing")
	result.Finish(start)
	return result
}

func runSysbenchCPU(ctx context.Context, env Environment, path string) model.Result {
	return runSysbenchCPUWithAllowance(ctx, env, path, detectCPUAllowance())
}

func runSysbenchCPUWithAllowance(ctx context.Context, env Environment, path string, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := model.NewResult("cpu", "module.cpu.title")
	result.Description = "probe.cpu.description"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "sysbench",
		Profile:         "cpu prime=20000",
		ComparisonScope: "probe.cpu.comparison_scope",
	}
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "configured_duration", env.Config.CPUTime.String())

	seconds := int((env.Config.CPUTime + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	workers := allowance.Threads
	threadCounts := distinctBenchmarkThreadCounts(workers)
	singleCore := len(threadCounts) == 1
	load1, loadKnown := readLoadAverage1()

	// steal 只有在压满 CPU 的窗口内测才有意义：空闲时宿主机没有争抢对象，
	// 读到的比例会偏低。这里夹住两轮 sysbench 的整个执行区间。
	stealBefore, stealTracked := readCPUTimes()

	single, err := executeSysbenchCPU(ctx, path, 1, seconds)
	if err != nil {
		result.Status = model.StatusError
		result.SummaryMessages = []model.Message{model.NewMessage("message.result.failed")}
		addFailure(&result, "single_thread_run", "sysbench", err)
		result.Evidence = model.NewEvidence(0, len(threadCounts), "run")
		result.Finish(start)
		return result
	}
	multi := single
	validRuns := 1
	if !singleCore {
		multi, err = executeSysbenchCPU(ctx, path, workers, seconds)
		if err != nil {
			result.Status = model.StatusWarning
			result.AddFailure(model.Failure{Category: model.FailureUnknown, Stage: "multi_thread_run", Target: "sysbench", Count: 1, Message: err.Error()})
			result.Notes = append(result.Notes, "probe.cpu.note.multi_failed")
		} else {
			validRuns++
		}
	}

	steal, stealOK := 0.0, false
	if stealTracked {
		if after, ok := readCPUTimes(); ok {
			steal, stealOK = stealPercent(stealBefore, after)
		}
	}

	appendSysbenchCPUMeasurements(&result, single, multi, workers)
	if stealOK {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "cpu_steal_percent_during_test", Label: "probe.cpu.metric.steal",
			Value: steal, Unit: "%", Display: model.RawValue(fmt.Sprintf("%.2f %%", steal)),
			Method: "proc-stat-steal-delta-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	version := commandVersion(ctx, path)
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.cpu.field.engine", Value: model.RawValue("sysbench")},
		{Key: "version", Label: "probe.cpu.field.version", Value: model.RawValue(version)},
		{Key: "threads", Label: "probe.cpu.field.threads", Value: model.RawValue(benchmarkThreadField(workers))},
		{Key: "cpu_allowance", Label: "probe.cpu.field.cpu_allowance", Value: model.RawValue(cpuAllowanceMachineValue(allowance))},
		{Key: "duration", Label: "probe.cpu.field.duration", Value: model.RawValue(fmt.Sprintf("%ds", seconds))},
		{Key: "prime", Label: "probe.cpu.field.prime", Value: model.RawValue("20000")},
		{Key: "single_events", Label: "probe.cpu.field.single_events", Value: model.RawValue(formatSysbenchEvents(single))},
		{Key: "multi_events", Label: "probe.cpu.field.multi_events", Value: model.RawValue(formatSysbenchEvents(multi))},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", version)
	addComparisonParameter(result.Methodology.Parameters, "threads", benchmarkThreadField(workers))
	addComparisonParameter(result.Methodology.Parameters, "duration", fmt.Sprintf("%ds", seconds))
	addComparisonParameter(result.Methodology.Parameters, "prime", "20000")
	validity := "probe.cpu.validity.valid"
	if allowance.Limited() {
		validity = "probe.cpu.validity.quota"
	}
	if multi.Rate <= 0 {
		validity = "probe.cpu.validity.partial"
	}
	if (stealOK && steal >= stealNoticeThreshold) || (loadKnown && load1 > float64(workers)*1.5) {
		validity = "probe.cpu.validity.interfered"
	}
	result.Fields = append(result.Fields, model.Field{Key: "result_validity", Label: "probe.cpu.field.result_validity", Value: model.KeyValue(validity)})
	if loadKnown {
		result.Fields = append(result.Fields, model.Field{
			Key: "pretest_load_1m", Label: "probe.cpu.field.pretest_load_1m", Value: model.RawValue(fmt.Sprintf("%.2f", load1)),
		})
	}
	result.TextBlocks = []model.TextBlock{
		{Title: "probe.cpu.raw.single", Language: "text", Content: single.Output},
	}
	if !singleCore && multi.Output != "" {
		result.TextBlocks = append(result.TextBlocks, model.TextBlock{Title: "probe.cpu.raw.multi", Language: "text", Content: multi.Output})
	}
	result.Sources = []model.Source{
		{Name: "sysbench", URL: "https://github.com/akopytov/sysbench", Purpose: "probe.cpu.source.purpose"},
	}
	result.Notes = append(result.Notes,
		"probe.cpu.note.comparability",
		"probe.cpu.note.scope",
	)
	if singleCore {
		result.Notes = append(result.Notes, "probe.cpu.note.single_core")
	}
	if allowance.Limited() && !singleCore {
		result.Notes = append(result.Notes, "probe.cpu.note.quota_limited")
	}
	if stealOK && steal >= stealNoticeThreshold {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.cpu.note.steal")
	}
	if loadKnown && load1 > float64(workers)*1.5 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.cpu.note.load")
	}
	result.Evidence = model.NewEvidence(validRuns, len(threadCounts), "run")
	if singleCore && multi.Rate > 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.cpu.summary.single_core", model.FormatRate(single.Rate, "events/s"))}
	} else if multi.Rate > 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.cpu.summary.multi", model.FormatRate(single.Rate, "events/s"), workers, model.FormatRate(multi.Rate, "events/s"))}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.cpu.summary.single", model.FormatRate(single.Rate, "events/s"))}
	}
	result.Finish(start)
	return result
}

func appendSysbenchCPUMeasurements(result *model.Result, single, multi sysbenchCPUResult, workers int) {
	result.Measurements = append(result.Measurements, model.Measurement{
		Key: "sysbench_cpu_single_events_s", Label: "probe.cpu.metric.single_events_s",
		Value: single.Rate, Unit: "events/s", Display: model.RawValue(model.FormatRate(single.Rate, "events/s")),
		Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
	})
	if single.P95MS > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "sysbench_cpu_single_p95_ms", Label: "probe.cpu.metric.single_p95_ms",
			Value: single.P95MS, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.2f ms", single.P95MS)),
			Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if multi.Rate > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "sysbench_cpu_multi_events_s", Label: "probe.cpu.metric.multi_events_s",
			Value: multi.Rate, Unit: "events/s", Display: model.RawValue(model.FormatRate(multi.Rate, "events/s")),
			Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
		})
		if workers > 1 {
			scaling := multi.Rate / single.Rate
			efficiency := scaling / float64(workers) * 100
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "sysbench_cpu_scaling_ratio", Label: "probe.cpu.metric.scaling_ratio",
				Value: scaling, Unit: "x", Display: model.RawValue(fmt.Sprintf("%.2f×", scaling)),
				Method: "sysbench-cpu-scaling-v1", HigherIsBetter: model.BoolPtr(true),
			}, model.Measurement{
				Key: "sysbench_cpu_per_thread_efficiency_percent", Label: "probe.cpu.metric.per_thread_efficiency_percent",
				Value: efficiency, Unit: "%", Display: model.RawValue(fmt.Sprintf("%.1f %%", efficiency)),
				Method: "sysbench-cpu-scaling-v1", HigherIsBetter: model.BoolPtr(true),
			})
		}
		if multi.P95MS > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "sysbench_cpu_multi_p95_ms", Label: "probe.cpu.metric.multi_p95_ms",
				Value: multi.P95MS, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.2f ms", multi.P95MS)),
				Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(false),
			})
		}
	}
}

func benchmarkThreadField(workers int) string {
	if workers <= 1 {
		return "1 / 1"
	}
	return fmt.Sprintf("1 / %d", workers)
}

func cpuAllowanceMachineValue(allowance cpuAllowance) string {
	if !allowance.Limited() {
		return fmt.Sprintf("visible=%d;quota=unlimited", allowance.Visible)
	}
	return fmt.Sprintf("visible=%d;quota=%.2f;threads=%d;source=%s", allowance.Visible, allowance.Quota, allowance.Threads, allowance.Source)
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
		return sysbenchCPUResult{Output: text, Args: args}, fmt.Errorf("events per second not found")
	}
	events, _ := parseFirstUint(sysbenchEventsPattern, text)
	p95, ok := parseFirstFloat(sysbenchP95Pattern, text)
	if !ok || p95 <= 0 {
		return sysbenchCPUResult{Rate: rate, Events: events, Output: text, Args: args}, fmt.Errorf("valid 95th percentile latency not found")
	}
	return sysbenchCPUResult{Rate: rate, Events: events, P95MS: p95, Output: text, Args: args}, nil
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
