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

var sysbenchMemoryRatePattern = regexp.MustCompile(`(?mi)\(([0-9]+(?:\.[0-9]+)?)\s+([KMGT]?i?B)/sec\)`)

type sysbenchMemoryResult struct {
	RateMiBS float64
	Output   string
	Args     []string
}

type memoryProbe struct{}

func (memoryProbe) ID() string         { return "memory" }
func (memoryProbe) Title() string      { return "内存性能" }
func (memoryProbe) NeedsNetwork() bool { return false }

func (memoryProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	// /proc/meminfo 的可用内存决定 mbw 的数组大小，小内存机器上不能贸然分配。
	available := parseMemInfo("/proc/meminfo")["MemAvailable"] * 1024

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
		result.Notes = append(result.Notes, "运行 install.sh --with-benchmarks 或通过系统包管理器安装 sysbench。ecs 不提供自研替代分数。")
	}

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

	writeSingle, err := executeSysbenchMemory(ctx, path, "write", 1, seconds)
	if err != nil {
		result.Fail(fmt.Errorf("sysbench 单线程内存写入: %w", err))
		result.Finish(start)
		return result
	}
	writeMulti, writeMultiErr := executeSysbenchMemory(ctx, path, "write", workers, seconds)
	readSingle, readSingleErr := executeSysbenchMemory(ctx, path, "read", 1, seconds)
	readMulti, readMultiErr := executeSysbenchMemory(ctx, path, "read", workers, seconds)
	for _, failure := range []struct {
		label string
		err   error
	}{
		{"多线程写入", writeMultiErr},
		{"单线程读取", readSingleErr},
		{"多线程读取", readMultiErr},
	} {
		if failure.err != nil {
			result.Status = model.StatusWarning
			result.Notes = append(result.Notes, "sysbench "+failure.label+"失败: "+failure.err.Error())
		}
	}

	appendMetric := func(key, label string, sample sysbenchMemoryResult, threads int, operation string) {
		if sample.RateMiBS <= 0 {
			return
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: key, Label: label, Value: sample.RateMiBS, Unit: "MiB/s",
			Display:        model.FormatRate(sample.RateMiBS, "MiB/s"),
			Method:         fmt.Sprintf("sysbench-memory-seq-1MiB-%s-%dt-v1", operation, threads),
			HigherIsBetter: model.BoolPtr(true),
		})
	}
	appendMetric("sysbench_memory_write_single_mib_s", "单线程顺序写", writeSingle, 1, "write")
	appendMetric("sysbench_memory_write_multi_mib_s", fmt.Sprintf("%d 线程顺序写", workers), writeMulti, workers, "write")
	appendMetric("sysbench_memory_read_single_mib_s", "单线程顺序读", readSingle, 1, "read")
	appendMetric("sysbench_memory_read_multi_mib_s", fmt.Sprintf("%d 线程顺序读", workers), readMulti, workers, "read")

	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "sysbench"},
		{Key: "version", Label: "工具版本", Value: commandVersion(ctx, path)},
		{Key: "binary_sha256", Label: "程序 SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
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
		{"单线程写入", writeSingle},
		{"多线程写入", writeMulti},
		{"单线程读取", readSingle},
		{"多线程读取", readMulti},
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
	summary := []string{"写 " + model.FormatRate(writeSingle.RateMiBS, "MiB/s")}
	if writeMulti.RateMiBS > 0 {
		summary = append(summary, fmt.Sprintf("%dT %s", workers, model.FormatRate(writeMulti.RateMiBS, "MiB/s")))
	}
	if readSingle.RateMiBS > 0 {
		summary = append(summary, "读 "+model.FormatRate(readSingle.RateMiBS, "MiB/s"))
	}
	result.Summary = "sysbench " + strings.Join(summary, " · ")
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
	return sysbenchMemoryResult{RateMiBS: rate, Output: text, Args: args}, nil
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
