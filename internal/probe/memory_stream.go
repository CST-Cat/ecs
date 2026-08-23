package probe

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

// STREAM is intentionally treated as an external benchmark.  The probe only
// controls the official binary's OpenMP thread count and parses its reported
// Best Rate values; it never implements the Copy/Scale/Add/Triad loops itself.
const streamRunTimeout = 90 * time.Second

var (
	// The official STREAM table has this header.  The unit belongs to the
	// column, not to each kernel row, so the parser records it and applies the
	// corresponding unit conversion to every row.
	streamRateHeaderPattern   = regexp.MustCompile(`(?mi)^\s*Function\s+Best\s+Rate\s+([KMGT]?i?B/s)\s+Avg\s+time\s+Min\s+time\s+Max\s+time\s*$`)
	streamKernelLinePattern   = regexp.MustCompile(`^\s*(Copy|Scale|Add|Triad):\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*$`)
	streamKernelPrefixPattern = regexp.MustCompile(`^\s*(Copy|Scale|Add|Triad):`)
	streamThreadsPattern      = regexp.MustCompile(`(?mi)^\s*Number of Threads requested\s*=\s*([0-9]+)\s*$`)
	streamVersionPattern      = regexp.MustCompile(`(?mi)^\s*(STREAM\s+version\s+.+?)\s*$`)
)

var streamKernels = []string{"Copy", "Scale", "Add", "Triad"}

// streamSample is one row from the official STREAM result table.  RateMiBS
// is normalized for the machine-readable measurement schema while RawRate and
// Unit preserve what the benchmark printed.
type streamSample struct {
	RateMiBS float64
	RawRate  float64
	Unit     string
	AvgTime  float64
	MinTime  float64
	MaxTime  float64
}

type streamParsedOutput struct {
	Unit             string
	Samples          map[string]streamSample
	RequestedThreads int
}

type streamMemoryRun struct {
	Context string
	Threads int
	Reused  bool
	Sample  streamParsedOutput
	Output  string
	Args    []string
	Err     error
}

// streamOfficialMarkers 是官方 STREAM 可执行文件里稳定出现的只读数据串。
// run.sh 用 strings(1) 检查同一组标记；两边改动必须同步。
var streamOfficialMarkers = []string{
	"STREAM version",
	"Number of Threads requested",
	"Best Rate",
	"Function",
}

// maxStreamBinaryBytes 限制读入内存的候选文件大小。官方 STREAM 只有几十 KB；
// PATH 上恰好同名的巨大文件不应该把整个进程读爆。
const maxStreamBinaryBytes = 64 << 20

// IsOfficialStreamBinary identifies the upstream STREAM executable without
// invoking it.  ImageMagick also installs a command named "stream"; running
// that command as a benchmark can block or produce an unrelated transcript.
// doctor 与 memory 探针共用这一份判断，避免两处各写一份标记表。
func IsOfficialStreamBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxStreamBinaryBytes {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	for _, marker := range streamOfficialMarkers {
		if !strings.Contains(text, marker) {
			return false
		}
	}
	return true
}

// parseStreamOutput parses the official STREAM summary table strictly.  A
// valid result must contain exactly one rate row for each of Copy, Scale, Add,
// and Triad, all four timing columns must be finite numbers, and the header
// must declare a supported rate unit.
func parseStreamOutput(output string) (streamParsedOutput, error) {
	result := streamParsedOutput{Samples: make(map[string]streamSample)}
	headers := streamRateHeaderPattern.FindAllStringSubmatch(output, -1)
	if len(headers) != 1 || len(headers[0]) != 2 {
		return streamParsedOutput{}, fmt.Errorf("STREAM 输出缺少唯一的 Best Rate 单位表头")
	}
	unit, factor, ok := streamRateUnit(headers[0][1])
	if !ok {
		return streamParsedOutput{}, fmt.Errorf("STREAM 不支持的速率单位 %q", headers[0][1])
	}
	result.Unit = unit

	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if !streamKernelPrefixPattern.MatchString(line) {
			continue
		}
		match := streamKernelLinePattern.FindStringSubmatch(line)
		if len(match) != 6 {
			return streamParsedOutput{}, fmt.Errorf("STREAM %q 行格式无效", strings.TrimSpace(line))
		}
		kernel := match[1]
		if _, exists := result.Samples[kernel]; exists {
			return streamParsedOutput{}, fmt.Errorf("STREAM 重复的 %s 行", kernel)
		}
		values := make([]float64, 4)
		for index := 0; index < len(values); index++ {
			value, err := strconv.ParseFloat(match[index+2], 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return streamParsedOutput{}, fmt.Errorf("STREAM %s 行包含无效数值 %q", kernel, match[index+2])
			}
			values[index] = value
		}
		if values[0] <= 0 {
			return streamParsedOutput{}, fmt.Errorf("STREAM %s Best Rate 必须为正数", kernel)
		}
		if values[1] <= 0 || values[2] <= 0 || values[3] <= 0 {
			return streamParsedOutput{}, fmt.Errorf("STREAM %s 时间统计必须为正数", kernel)
		}
		if values[2] > values[1] || values[1] > values[3] {
			return streamParsedOutput{}, fmt.Errorf("STREAM %s 时间统计顺序无效", kernel)
		}
		rateMiBS := values[0] * factor
		if math.IsNaN(rateMiBS) || math.IsInf(rateMiBS, 0) || rateMiBS <= 0 {
			return streamParsedOutput{}, fmt.Errorf("STREAM %s Best Rate 换算后无效", kernel)
		}
		result.Samples[kernel] = streamSample{
			RateMiBS: rateMiBS,
			RawRate:  values[0],
			Unit:     unit,
			AvgTime:  values[1],
			MinTime:  values[2],
			MaxTime:  values[3],
		}
	}
	for _, kernel := range streamKernels {
		if _, ok := result.Samples[kernel]; !ok {
			return streamParsedOutput{}, fmt.Errorf("STREAM 输出缺少 %s 行", kernel)
		}
	}

	threadMatches := streamThreadsPattern.FindAllStringSubmatch(output, -1)
	if len(threadMatches) > 1 {
		return streamParsedOutput{}, fmt.Errorf("STREAM 输出包含多个线程数声明")
	}
	if len(threadMatches) == 1 {
		threads, err := strconv.Atoi(threadMatches[0][1])
		if err != nil || threads < 1 {
			return streamParsedOutput{}, fmt.Errorf("STREAM 线程数声明无效 %q", threadMatches[0][1])
		}
		result.RequestedThreads = threads
	}
	return result, nil
}

// streamRateUnit returns a canonical source unit and its multiplier to MiB/s.
// STREAM normally prints MB/s, but accepting the binary spelling keeps the
// parser strict while handling official builds that change the display unit.
func streamRateUnit(raw string) (string, float64, bool) {
	switch strings.ToLower(raw) {
	case "b/s":
		return "B/s", 1.0 / (1024 * 1024), true
	case "kb/s":
		return "KB/s", 1000.0 / (1024 * 1024), true
	case "kib/s":
		return "KiB/s", 1.0 / 1024, true
	case "mb/s":
		return "MB/s", 1000.0 * 1000 / (1024 * 1024), true
	case "mib/s":
		return "MiB/s", 1, true
	case "gb/s":
		return "GB/s", 1000.0 * 1000 * 1000 / (1024 * 1024), true
	case "gib/s":
		return "GiB/s", 1024, true
	case "tb/s":
		return "TB/s", 1000.0 * 1000 * 1000 * 1000 / (1024 * 1024), true
	case "tib/s":
		return "TiB/s", 1024 * 1024, true
	default:
		return "", 0, false
	}
}

// executeStreamMemory runs one complete official STREAM pass.  STREAM's
// standard binary owns the four kernels and its iteration count; the only
// per-run control needed here is OMP_NUM_THREADS.
func executeStreamMemory(ctx context.Context, path string, threads int) (streamMemoryRun, error) {
	run := streamMemoryRun{Threads: threads, Args: nil}
	if threads < 1 {
		return run, fmt.Errorf("STREAM 线程数必须为正数")
	}
	runCtx, cancel := context.WithTimeout(ctx, streamRunTimeout)
	defer cancel()
	command := exec.CommandContext(runCtx, path)
	command.Env = streamEnvironment(threads)
	output, err := command.CombinedOutput()
	run.Output = sanitizeCommandOutput(output)
	if runCtx.Err() != nil {
		return run, runCtx.Err()
	}
	if err != nil {
		return run, fmt.Errorf("STREAM 执行失败: %w: %s", err, tailText(run.Output, 400))
	}
	parsed, parseErr := parseStreamOutput(run.Output)
	if parseErr != nil {
		return run, parseErr
	}
	if parsed.RequestedThreads == 0 {
		return run, fmt.Errorf("STREAM 输出缺少 Number of Threads requested，无法验证 OMP_NUM_THREADS=%d 已生效", threads)
	}
	if parsed.RequestedThreads != threads {
		return run, fmt.Errorf("STREAM 声明线程数为 %d，期望 %d", parsed.RequestedThreads, threads)
	}
	run.Sample = parsed
	return run, nil
}

// streamEnvironment replaces, rather than duplicates, the variables that
// control OpenMP.  Duplicate environment entries are implementation-defined
// for getenv(), so keeping the requested 1T/NT value unique matters here.
func streamEnvironment(threads int) []string {
	const (
		ompThreads = "OMP_NUM_THREADS"
		ompDynamic = "OMP_DYNAMIC"
		lcAll      = "LC_ALL"
		lang       = "LANG"
	)
	env := make([]string, 0, len(os.Environ())+4)
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && (key == ompThreads || key == ompDynamic || key == lcAll || key == lang) {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		ompThreads+"="+strconv.Itoa(threads),
		ompDynamic+"=FALSE",
		lcAll+"=C",
		lang+"=C",
	)
	return env
}

// runStreamMemory executes each distinct physical thread context, then appends
// the eight logical kernel measurements in report order: Copy/Triad first for
// the headline, followed by complete Scale/Add data. On a one-core allowance,
// the NT logical context explicitly reuses the single physical 1T run.
func runStreamMemory(ctx context.Context, env Environment, path string) model.Result {
	return runStreamMemoryWithAllowance(ctx, env, path, detectCPUAllowance())
}

func runStreamMemoryWithAllowance(ctx context.Context, env Environment, path string, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := model.NewResult("memory", "内存性能")
	result.Description = "官方 STREAM 内存带宽的 Copy、Scale、Add、Triad kernel，分别运行 1T 与当前 CPU allowance 的 NT"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "STREAM",
		Profile:         "Copy/Scale/Add/Triad × 1T/NT",
		ComparisonScope: "相同 STREAM 实现、数组规模、编译选项、线程上下文与运行环境",
	}

	workers := allowance.Threads
	threadCounts := distinctBenchmarkThreadCounts(workers)
	singleCore := len(threadCounts) == 1
	runs := make([]streamMemoryRun, 0, 2)
	validRuns := 0
	for index, threads := range threadCounts {
		contextName := "1t"
		if index == 1 {
			contextName = "nt"
		}
		run, err := executeStreamMemory(ctx, path, threads)
		run.Context = contextName
		run.Threads = threads
		run.Err = err
		runs = append(runs, run)
		if err != nil {
			result.Status = model.StatusWarning
			logicalName := "1T"
			if contextName == "nt" {
				logicalName = "NT"
			}
			// Keep the note localizable; the exact command/parser diagnostic and
			// raw output remain in the run result for machine/debug inspection.
			result.Notes = append(result.Notes, fmt.Sprintf("STREAM %s 运行失败；请查看原始输出。", logicalName))
		} else {
			validRuns++
		}
	}
	if singleCore {
		clone := runs[0]
		clone.Context = "nt"
		clone.Threads = 1
		clone.Reused = true
		runs = append(runs, clone)
		result.Description = "官方 STREAM Copy、Scale、Add、Triad kernel；单核 allowance 下 1T/NT 逻辑指标复用一次实测"
		result.Methodology.Profile = "Copy/Scale/Add/Triad × 1T/NT logical · one physical 1T run"
	}

	measurementOrder := []struct {
		kernel string
		run    int
	}{
		{kernel: "Copy", run: 0},
		{kernel: "Copy", run: 1},
		{kernel: "Triad", run: 0},
		{kernel: "Triad", run: 1},
		{kernel: "Scale", run: 0},
		{kernel: "Scale", run: 1},
		{kernel: "Add", run: 0},
		{kernel: "Add", run: 1},
	}
	for _, item := range measurementOrder {
		run := runs[item.run]
		sample, ok := run.Sample.Samples[item.kernel]
		if !ok || sample.RateMiBS <= 0 {
			continue
		}
		contextName := run.Context
		result.Measurements = append(result.Measurements, model.Measurement{
			Key:            "stream_" + strings.ToLower(item.kernel) + "_" + contextName + "_mib_s",
			Label:          streamMeasurementLabel(item.kernel, run),
			Value:          sample.RateMiBS,
			Unit:           "MiB/s",
			Display:        model.FormatRate(sample.RateMiBS, "MiB/s"),
			Method:         "stream-official-" + strings.ToLower(item.kernel) + "-" + contextName + "-v1",
			HigherIsBetter: model.BoolPtr(true),
		})
	}
	if workers > 1 && len(runs) == 2 {
		for _, kernel := range streamKernels {
			single, singleOK := runs[0].Sample.Samples[kernel]
			multi, multiOK := runs[1].Sample.Samples[kernel]
			if !singleOK || !multiOK || single.RateMiBS <= 0 || multi.RateMiBS <= 0 {
				continue
			}
			ratio := multi.RateMiBS / single.RateMiBS
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:            "stream_" + strings.ToLower(kernel) + "_scaling_ratio",
				Label:          "STREAM " + kernel + " 多线程扩展倍率",
				Value:          ratio,
				Unit:           "x",
				Display:        fmt.Sprintf("%.2f×", ratio),
				Method:         "stream-official-" + strings.ToLower(kernel) + "-scaling-v1",
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}

	units := make([]string, 0, 2)
	for _, run := range runs {
		if run.Sample.Unit == "" || containsString(units, run.Sample.Unit) {
			continue
		}
		units = append(units, run.Sample.Unit)
	}
	version := streamVersion(runs)
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "STREAM"},
		{Key: "version", Label: "STREAM 版本", Value: version},
		{Key: "binary_sha256", Label: "STREAM SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "threads", Label: "测试线程", Value: benchmarkThreadField(workers)},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: describeCPUAllowance(allowance)},
		{Key: "kernel_order", Label: "STREAM kernel", Value: "Copy / Scale / Add / Triad"},
		{Key: "thread_control", Label: "线程控制", Value: streamThreadControlField(workers)},
		{Key: "rate_unit", Label: "速率单位", Value: "MiB/s（结构化值）"},
	}
	if len(units) > 0 {
		result.Fields = append(result.Fields, model.Field{Key: "source_rate_units", Label: "原始速率单位", Value: strings.Join(units, " / ")})
	}
	for _, run := range runs {
		if run.Output == "" || run.Reused {
			continue
		}
		result.TextBlocks = append(result.TextBlocks, model.TextBlock{
			Title: "STREAM " + streamContextLabel(run) + " 原始输出", Language: "text", Content: run.Output,
		})
	}
	result.Sources = []model.Source{
		{Name: "STREAM", URL: "https://www.cs.virginia.edu/stream/", Purpose: "官方 STREAM 内存带宽标准基准"},
	}
	if singleCore {
		result.Notes = append(result.Notes, "STREAM 使用官方 Copy、Scale、Add、Triad kernel；单核 allowance 下 1T 与 NT 逻辑指标复用同一次 OMP_NUM_THREADS=1 实测，未执行第二次相同命令，多线程扩展倍率不适用。")
	} else {
		result.Notes = append(result.Notes, fmt.Sprintf("STREAM 使用官方 Copy、Scale、Add、Triad kernel；1T 与 NT 是两次独立运行，分别通过 OMP_NUM_THREADS=1 与 OMP_NUM_THREADS=%d 控制。", workers))
	}
	result.Notes = append(result.Notes,
		"STREAM 输出的 Best Rate 原始单位保留在字段中；结构化指标统一换算为 MiB/s。",
	)
	if len(units) > 1 {
		result.Notes = append(result.Notes, "两次 STREAM 输出的原始速率单位不同；报告仍将结构化数值统一为 MiB/s。")
	}
	if allowance.Limited() && !singleCore {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"STREAM NT 使用 %d 个线程；检测到的 CPU allowance 为 %.2f 个核（%s）；1T 与 NT 仍是独立运行。",
			workers, allowance.Quota, allowance.Source,
		))
	}

	result.Tables = append(result.Tables, streamMemoryTable(runs), streamStabilityTable(runs))
	result.Evidence = model.NewEvidence(validRuns, len(threadCounts), "run")
	result.Finish(start)
	return result
}

func streamThreadControlField(workers int) string {
	if workers <= 1 {
		return "OMP_NUM_THREADS=1（1T/NT 同一次实测）"
	}
	return fmt.Sprintf("OMP_NUM_THREADS=1；OMP_NUM_THREADS=%d（独立运行）", workers)
}

func streamStabilityTable(runs []streamMemoryRun) model.Table {
	table := model.Table{
		Key:                   "memory.stream.stability",
		Title:                 "STREAM 运行稳定性",
		Columns:               []string{"内核 / 线程", "平均时间", "最短时间", "最长时间", "时间波动"},
		ColumnKeys:            []string{"kernel_context", "average_seconds", "minimum_seconds", "maximum_seconds", "spread_percent"},
		NumericColumns:        []int{1, 2, 3, 4},
		NumericHigherIsBetter: []bool{false, false, false, false},
	}
	for _, kernel := range []string{"Copy", "Triad", "Scale", "Add"} {
		for _, run := range runs {
			avg, minValue, maxValue, spread := "—", "—", "—", "—"
			if sample, ok := run.Sample.Samples[kernel]; ok {
				avg = fmt.Sprintf("%.6f s", sample.AvgTime)
				minValue = fmt.Sprintf("%.6f s", sample.MinTime)
				maxValue = fmt.Sprintf("%.6f s", sample.MaxTime)
				if sample.MinTime > 0 && sample.MaxTime >= sample.MinTime {
					spread = fmt.Sprintf("%.2f %%", (sample.MaxTime-sample.MinTime)/sample.MinTime*100)
				}
			}
			table.Rows = append(table.Rows, []string{
				kernel + " / " + streamContextLabel(run), avg, minValue, maxValue, spread,
			})
		}
	}
	return table
}

func streamMemoryTable(runs []streamMemoryRun) model.Table {
	table := model.Table{
		Key:                   "memory.stream.bandwidth",
		Title:                 "STREAM 四 kernel 带宽（Copy/Triad 主结果）",
		Columns:               []string{"内核 / 线程", "最佳速率", "原始单位", "方法", "证据"},
		ColumnKeys:            []string{"kernel_context", "best_rate_mibs", "raw_unit", "method", "evidence"},
		NumericColumns:        []int{1},
		NumericHigherIsBetter: []bool{true},
	}
	// Keep Copy/Triad at the top while retaining every Scale/Add cell.
	order := []string{"Copy", "Triad", "Scale", "Add"}
	for _, kernel := range order {
		for _, run := range runs {
			value, unit, evidence := "—", "—", "未返回"
			if sample, ok := run.Sample.Samples[kernel]; ok && sample.RateMiBS > 0 {
				value = model.FormatRate(sample.RateMiBS, "MiB/s")
				unit = sample.Unit
				evidence = "STREAM 最佳速率"
			}
			if run.Err != nil && evidence == "未返回" {
				evidence = "失败"
			}
			if run.Reused && evidence == "STREAM 最佳速率" {
				evidence = "复用同一次实测"
			}
			table.Rows = append(table.Rows, []string{
				kernel + " / " + streamContextLabel(run), value, unit,
				"stream-official-" + strings.ToLower(kernel) + "-" + run.Context + "-v1", evidence,
			})
		}
	}
	return table
}

func streamContextLabel(run streamMemoryRun) string {
	if run.Context == "nt" {
		return fmt.Sprintf("NT（%d 线程）", run.Threads)
	}
	return "1T"
}

func streamMeasurementLabel(kernel string, run streamMemoryRun) string {
	return "STREAM " + kernel + " " + streamContextLabel(run)
}

func streamVersion(runs []streamMemoryRun) string {
	for _, run := range runs {
		if match := streamVersionPattern.FindStringSubmatch(run.Output); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return "unknown"
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
