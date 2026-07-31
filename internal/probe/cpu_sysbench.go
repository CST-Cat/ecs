package probe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
	result.Notes = append(result.Notes, "运行 install.sh --with-benchmarks 或通过系统包管理器安装 sysbench。ecs 不提供自研替代分数。")
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
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 256 {
		workers = 256
	}

	single, err := executeSysbenchCPU(ctx, path, 1, seconds)
	if err != nil {
		result.Fail(fmt.Errorf("sysbench 单线程 CPU 基准: %w", err))
		result.Finish(start)
		return result
	}
	multi, err := executeSysbenchCPU(ctx, path, workers, seconds)
	if err != nil {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "sysbench 多线程 CPU 基准失败: "+err.Error())
	}

	result.Measurements = []model.Measurement{
		{
			Key: "sysbench_cpu_single_events_s", Label: "单线程事件率",
			Value: single.Rate, Unit: "events/s", Display: model.FormatRate(single.Rate, "events/s"),
			Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	if multi.Rate > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "sysbench_cpu_multi_events_s", Label: fmt.Sprintf("%d 线程事件率", workers),
			Value: multi.Rate, Unit: "events/s", Display: model.FormatRate(multi.Rate, "events/s"),
			Method: "sysbench-cpu-prime20000-v1", HigherIsBetter: model.BoolPtr(true),
		})
	}
	version := commandVersion(ctx, path)
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "sysbench"},
		{Key: "version", Label: "工具版本", Value: version},
		{Key: "binary_sha256", Label: "程序 SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "threads", Label: "测试线程", Value: fmt.Sprintf("1 / %d", workers)},
		{Key: "duration", Label: "每轮时长", Value: fmt.Sprintf("%ds", seconds)},
		{Key: "prime", Label: "最大素数", Value: "20000"},
		{Key: "single_events", Label: "单线程总事件", Value: strconv.FormatUint(single.Events, 10)},
		{Key: "multi_events", Label: "多线程总事件", Value: strconv.FormatUint(multi.Events, 10)},
		{Key: "arguments", Label: "参数模板", Value: "sysbench --threads=N --time=S --events=0 --percentile=95 cpu --cpu-max-prime=20000 run"},
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
	if multi.Rate > 0 {
		result.Summary = fmt.Sprintf("sysbench 单线程 %s · %d 线程 %s",
			model.FormatRate(single.Rate, "events/s"), workers, model.FormatRate(multi.Rate, "events/s"))
	} else {
		result.Summary = "sysbench 单线程 " + model.FormatRate(single.Rate, "events/s")
	}
	result.Finish(start)
	return result
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
	p95, _ := parseFirstFloat(sysbenchP95Pattern, text)
	return sysbenchCPUResult{Rate: rate, Events: events, P95MS: p95, Output: text, Args: args}, nil
}

func commandVersion(ctx context.Context, path string) string {
	versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, path, "--version").CombinedOutput()
	if err != nil {
		return "unknown"
	}
	return fallback(strings.TrimSpace(sanitizeCommandOutput(output)), "unknown")
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
