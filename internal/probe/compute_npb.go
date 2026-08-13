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

const (
	npbMethodVersion   = "npb-omp-3.4.4-class-a-v1"
	npbExpectedVersion = "3.4.4"
	npbExpectedClass   = "A"
	npbCompileFlags    = "-O3 -fopenmp -static"
	npbRandomGenerator = "randi8"
	npbRunTimeout      = 10 * time.Minute
)

var npbBannerPattern = regexp.MustCompile(`(?m)^\s*NAS Parallel Benchmarks \(NPB3\.4-OMP\) - (EP|FT) Benchmark\s*$`)

type npbBenchmarkSpec struct {
	Name             string
	Binary           string
	ExpectedSize     string
	ExpectedIters    int
	ExpectedOp       string
	Description      string
	MeasurementLabel string
}

var npbBenchmarkSpecs = []npbBenchmarkSpec{
	{
		Name: "EP", Binary: "npb-ep", ExpectedSize: "536870912", ExpectedIters: 0,
		ExpectedOp: "Random numbers generated", Description: "浮点随机数与高斯对计算",
		MeasurementLabel: "EP 浮点计算吞吐",
	},
	{
		Name: "FT", Binary: "npb-ft", ExpectedSize: "256x256x128", ExpectedIters: 6,
		ExpectedOp: "floating point", Description: "3D FFT、浮点与 cache/memory access 综合负载",
		MeasurementLabel: "FT FFT/浮点吞吐",
	},
}

type npbBenchmarkSample struct {
	Benchmark     string
	Class         string
	Size          string
	Iterations    int
	Seconds       float64
	Threads       int
	Available     int
	MOPS          float64
	MOPSPerThread float64
	Operation     string
	Version       string
	Compiler      string
	Linker        string
	CompileFlags  string
	LinkFlags     string
	Random        string
	Output        string
	Environment   []string
	Binary        string
	BinarySHA256  string
}

type npbProbe struct{}

func (npbProbe) ID() string         { return "npb" }
func (npbProbe) Title() string      { return "NPB EP + FT 计算性能" }
func (npbProbe) NeedsNetwork() bool { return false }

func (npbProbe) Run(ctx context.Context, env Environment) model.Result {
	return runNPBBenchmarks(ctx, env, npbBenchmarkSpecs)
}

func npbMethodology() model.Methodology {
	return model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "NAS Parallel Benchmarks OpenMP",
		Profile:         "NPB 3.4.4 · EP + FT · Class A · 1T/NT",
		ComparisonScope: "相同 NPB 版本、EP/FT Class A、OpenMP 编译参数、线程环境、binary SHA-256 与 method version",
	}
}

func runNPBBenchmarks(ctx context.Context, env Environment, specs []npbBenchmarkSpec) model.Result {
	return runNPBBenchmarksWithAllowance(ctx, env, specs, detectCPUAllowance())
}

func runNPBBenchmarksWithAllowance(ctx context.Context, env Environment, specs []npbBenchmarkSpec, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := model.NewResult("npb", "NPB EP + FT 计算性能")
	result.Description = "NASA NPB-OMP Class A：EP 覆盖浮点与多核吞吐，FT 覆盖 3D FFT、浮点和 cache/memory access"
	result.Methodology = npbMethodology()

	workers := allowance.Threads
	threadCounts := distinctBenchmarkThreadCounts(workers)
	singleCore := len(threadCounts) == 1
	runs := make(map[string][]npbBenchmarkSample, len(specs))
	paths := make(map[string]string, len(specs))
	validRuns := 0
	expectedRuns := len(specs) * len(threadCounts)
	for _, spec := range specs {
		path, err := exec.LookPath(spec.Binary)
		if err != nil {
			message := fmt.Sprintf("未找到固定 NPB %s Class A binary，%s 未运行", spec.Name, spec.Name)
			result.Status = model.StatusWarning
			result.Notes = append(result.Notes, message)
			result.AddFailure(model.Failure{
				Category: model.FailureToolMissing, Stage: "tool_lookup", Target: spec.Binary,
				Count: len(threadCounts), Message: message,
			})
			continue
		}
		paths[spec.Name] = path
		for _, threads := range threadCounts {
			sample, runErr := executeNPBBenchmark(ctx, path, spec, threads)
			runs[spec.Name] = append(runs[spec.Name], sample)
			if runErr == nil {
				validRuns++
				continue
			}
			result.Status = model.StatusWarning
			contextName := fmt.Sprintf("%s %dT", spec.Name, threads)
			result.Notes = append(result.Notes, fmt.Sprintf("NPB %s 运行失败：%s", contextName, runErr.Error()))
			result.AddFailure(model.Failure{
				Category: benchmarkFailureCategory(ctx, runErr), Stage: "benchmark_run", Target: contextName,
				Count: 1, Message: runErr.Error(),
			})
		}
		if singleCore && len(runs[spec.Name]) == 1 {
			clone := runs[spec.Name][0]
			runs[spec.Name] = append(runs[spec.Name], clone)
		}
	}

	appendNPBMeasurements(&result, specs, runs, workers)
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "NASA NPB-OMP"},
		{Key: "version", Label: "NPB 版本", Value: npbExpectedVersion},
		{Key: "method_version", Label: "method version", Value: npbMethodVersion},
		{Key: "benchmarks", Label: "固定 benchmark", Value: "EP / FT"},
		{Key: "problem_class", Label: "problem class", Value: npbExpectedClass},
		{Key: "threads", Label: "测试线程", Value: benchmarkThreadField(workers)},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: describeCPUAllowance(allowance)},
		{Key: "implementation", Label: "实现", Value: "NPB3.4-OMP"},
		{Key: "compiler_flags", Label: "固定编译参数", Value: npbCompileFlags},
		{Key: "random_generator", Label: "随机数实现", Value: npbRandomGenerator},
		{Key: "arguments", Label: "程序参数", Value: "(none)"},
		{Key: "environment_1t", Label: "完整环境（1T）", Value: strings.Join(npbEnvironmentParameters(1), " ")},
		{Key: "environment_nt", Label: "完整环境（全线程）", Value: strings.Join(npbEnvironmentParameters(workers), " ")},
	}
	for _, spec := range specs {
		path := paths[spec.Name]
		result.Fields = append(result.Fields,
			model.Field{Key: "binary_" + strings.ToLower(spec.Name), Label: spec.Name + " binary", Value: fallback(path, "unavailable")},
			model.Field{Key: "binary_" + strings.ToLower(spec.Name) + "_sha256", Label: spec.Name + " SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		)
	}
	for _, spec := range specs {
		for index, sample := range runs[spec.Name] {
			if singleCore && index > 0 {
				continue
			}
			if sample.Output == "" {
				continue
			}
			contextName := "1T"
			if index == 1 {
				contextName = fmt.Sprintf("全线程（%dT）", workers)
			}
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    fmt.Sprintf("NPB %s Class A %s 原始输出", spec.Name, contextName),
				Language: "text", Content: sample.Output,
			})
		}
	}
	result.Tables = append(result.Tables, npbResultsTable(specs, runs, workers))
	result.Sources = []model.Source{
		{Name: "NASA NAS Parallel Benchmarks", URL: "https://www.nas.nasa.gov/software/npb.html", Purpose: "NPB 3.4.4 官方源码与 benchmark 定义"},
	}
	if singleCore {
		result.Notes = append(result.Notes, "仅运行 NPB-OMP 的 EP 与 FT；单核 allowance 下 1T 与 NT 逻辑指标复用各 benchmark 的同一次 1T 实测，未执行第二次相同命令，扩展倍率不适用。")
	} else {
		result.Notes = append(result.Notes, "仅运行 NPB-OMP 的 EP 与 FT；固定 NPB 3.4.4、Class A、1T/当前 CPU allowance 全线程与相同 OpenMP 环境。")
	}
	result.Notes = append(result.Notes,
		"EP 输出随机数/高斯对浮点计算 Mop/s；FT 输出 3D FFT、浮点与 cache/memory access 综合 Mop/s。两者 workload 不同，不互相混算。",
		"每轮必须报告 Class A、正确 size/iteration、实际线程数、NPB 3.4.4、固定编译参数并通过 Verification；否则不采纳 Mop/s。",
		"本模块只保留各 benchmark 原始 Mop/s、Mop/s/thread 和多线程扩展倍率，不进入 CPU 综合分，也不生成自定义综合分。",
	)
	if allowance.Limited() && !singleCore {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"检测到 CPU 配额 %.2f 核（%s），全线程轮按 allowance 使用 %d 线程。",
			allowance.Quota, allowance.Source, workers,
		))
	}
	result.Evidence = model.NewEvidence(validRuns, expectedRuns, "run")
	if validRuns < expectedRuns {
		result.Status = model.StatusWarning
	}
	result.Summary = npbSummary(specs, runs, workers)
	result.Finish(start)
	return result
}

func executeNPBBenchmark(ctx context.Context, path string, spec npbBenchmarkSpec, threads int) (npbBenchmarkSample, error) {
	sample := npbBenchmarkSample{Benchmark: spec.Name, Threads: threads, Binary: path, BinarySHA256: binarySHA256(path)}
	if threads < 1 {
		return sample, fmt.Errorf("NPB 线程数必须为正数")
	}
	environment := npbEnvironmentParameters(threads)
	sample.Environment = append([]string(nil), environment...)
	overrides := make(map[string]string, len(environment))
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			overrides[key] = value
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, npbRunTimeout)
	defer cancel()
	workDirectory, err := os.MkdirTemp("", "ecs-npb-")
	if err != nil {
		return sample, fmt.Errorf("创建 NPB 私有工作目录: %w", err)
	}
	defer os.RemoveAll(workDirectory)
	command := exec.CommandContext(runCtx, path)
	command.Env = benchmarkEnvironment(overrides)
	command.Dir = workDirectory
	output, err := command.CombinedOutput()
	sample.Output = normalizeCarriageReturnOutput(output)
	if runCtx.Err() != nil {
		return sample, runCtx.Err()
	}
	if err != nil {
		return sample, fmt.Errorf("NPB %s 执行失败: %w: %s", spec.Name, err, tailText(sample.Output, 600))
	}
	parsed, err := parseNPBBenchmarkOutput(sample.Output, spec, threads)
	parsed.Output = sample.Output
	parsed.Environment = sample.Environment
	parsed.Binary = path
	parsed.BinarySHA256 = sample.BinarySHA256
	if err != nil {
		return parsed, err
	}
	return parsed, nil
}

func npbEnvironmentParameters(threads int) []string {
	return []string{
		"OMP_NUM_THREADS=" + strconv.Itoa(threads),
		"OMP_DYNAMIC=FALSE",
		"OMP_PROC_BIND=close",
		"OMP_PLACES=cores",
		"OMP_SCHEDULE=static",
		"OMP_DISPLAY_ENV=FALSE",
		"NPB_TIMER_FLAG=0",
	}
}

func parseNPBBenchmarkOutput(output string, spec npbBenchmarkSpec, requestedThreads int) (npbBenchmarkSample, error) {
	sample := npbBenchmarkSample{Benchmark: spec.Name, Threads: requestedThreads}
	banners := npbBannerPattern.FindAllStringSubmatch(output, -1)
	if len(banners) != 1 || len(banners[0]) != 2 || banners[0][1] != spec.Name {
		return sample, fmt.Errorf("NPB %s 输出缺少唯一官方 benchmark header", spec.Name)
	}
	field := func(label string) (string, error) { return uniqueNPBOutputField(output, label) }
	class, err := field("Class")
	if err != nil || class != npbExpectedClass {
		return sample, fmt.Errorf("NPB %s Class 为 %s，期望 %s", spec.Name, fallback(class, "unknown"), npbExpectedClass)
	}
	size, err := field("Size")
	if err != nil || strings.ReplaceAll(size, " ", "") != spec.ExpectedSize {
		return sample, fmt.Errorf("NPB %s Size 为 %s，期望 %s", spec.Name, fallback(size, "unknown"), spec.ExpectedSize)
	}
	iterations, err := npbIntegerField(field, "Iterations")
	if err != nil || iterations != spec.ExpectedIters {
		return sample, fmt.Errorf("NPB %s Iterations 无效或不符合 Class A: %d", spec.Name, iterations)
	}
	seconds, err := npbFloatField(field, "Time in seconds")
	if err != nil || seconds <= 0 {
		return sample, fmt.Errorf("NPB %s Time in seconds 无效", spec.Name)
	}
	threads, err := npbIntegerField(field, "Total threads")
	if err != nil || threads != requestedThreads {
		return sample, fmt.Errorf("NPB %s 实际线程为 %d，期望 %d", spec.Name, threads, requestedThreads)
	}
	available, err := npbIntegerField(field, "Avail threads")
	if err != nil || available != requestedThreads {
		return sample, fmt.Errorf("NPB %s available threads 为 %d，期望 %d", spec.Name, available, requestedThreads)
	}
	mops, err := npbFloatField(field, "Mop/s total")
	if err != nil || mops <= 0 {
		return sample, fmt.Errorf("NPB %s Mop/s total 无效", spec.Name)
	}
	perThread, err := npbFloatField(field, "Mop/s/thread")
	if err != nil || perThread <= 0 {
		return sample, fmt.Errorf("NPB %s Mop/s/thread 无效", spec.Name)
	}
	operation, err := field("Operation type")
	if err != nil || !strings.EqualFold(operation, spec.ExpectedOp) {
		return sample, fmt.Errorf("NPB %s Operation type 为 %s，期望 %s", spec.Name, fallback(operation, "unknown"), spec.ExpectedOp)
	}
	verification, err := field("Verification")
	if err != nil || verification != "SUCCESSFUL" {
		return sample, fmt.Errorf("NPB %s Verification 未成功: %s", spec.Name, fallback(verification, "missing"))
	}
	version, err := field("Version")
	if err != nil || version != npbExpectedVersion {
		return sample, fmt.Errorf("NPB %s Version 为 %s，期望 %s", spec.Name, fallback(version, "unknown"), npbExpectedVersion)
	}
	compiler, err := field("FC")
	if err != nil || !strings.HasSuffix(compiler, "gfortran") {
		return sample, fmt.Errorf("NPB %s FC 不是固定 GNU Fortran toolchain: %s", spec.Name, fallback(compiler, "unknown"))
	}
	linker, err := field("FLINK")
	if err != nil || linker != compiler {
		return sample, fmt.Errorf("NPB %s FLINK 与 FC 不一致", spec.Name)
	}
	compileFlags, err := field("FFLAGS")
	if err != nil || compileFlags != npbCompileFlags {
		return sample, fmt.Errorf("NPB %s FFLAGS 为 %s，期望 %s", spec.Name, fallback(compileFlags, "unknown"), npbCompileFlags)
	}
	linkFlags, err := field("FLINKFLAGS")
	if err != nil || linkFlags != npbCompileFlags {
		return sample, fmt.Errorf("NPB %s FLINKFLAGS 为 %s，期望 %s", spec.Name, fallback(linkFlags, "unknown"), npbCompileFlags)
	}
	random, err := field("RAND")
	if err != nil || random != npbRandomGenerator {
		return sample, fmt.Errorf("NPB %s RAND 为 %s，期望 %s", spec.Name, fallback(random, "unknown"), npbRandomGenerator)
	}
	if strings.Contains(output, "Warning: Threads used differ from threads available") {
		return sample, fmt.Errorf("NPB %s 报告线程使用与 available 不一致", spec.Name)
	}
	return npbBenchmarkSample{
		Benchmark: spec.Name, Class: class, Size: strings.ReplaceAll(size, " ", ""), Iterations: iterations,
		Seconds: seconds, Threads: threads, Available: available, MOPS: mops, MOPSPerThread: perThread,
		Operation: operation, Version: version, Compiler: compiler, Linker: linker,
		CompileFlags: compileFlags, LinkFlags: linkFlags, Random: random,
	}, nil
}

func uniqueNPBOutputField(output, label string) (string, error) {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(label) + `\s*=\s*(.*?)\s*$`)
	matches := pattern.FindAllStringSubmatch(output, -1)
	if len(matches) != 1 || len(matches[0]) != 2 {
		return "", fmt.Errorf("NPB output field %s count = %d", label, len(matches))
	}
	value := strings.TrimSpace(matches[0][1])
	if value == "" {
		return "", fmt.Errorf("NPB output field %s is empty", label)
	}
	return value, nil
}

func npbIntegerField(field func(string) (string, error), label string) (int, error) {
	value, err := field(label)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("NPB integer field %s is invalid", label)
	}
	return parsed, nil
}

func npbFloatField(field func(string) (string, error), label string) (float64, error) {
	value, err := field(label)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("NPB float field %s is invalid", label)
	}
	return parsed, nil
}

func benchmarkFailureCategory(ctx context.Context, err error) model.FailureCategory {
	if ctx.Err() == context.Canceled {
		return model.FailureCanceled
	}
	if ctx.Err() == context.DeadlineExceeded || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		return model.FailureTimeout
	}
	if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		return model.FailurePermissionDenied
	}
	if strings.Contains(err.Error(), "输出") || strings.Contains(err.Error(), "Verification") || strings.Contains(err.Error(), "field") {
		return model.FailureParse
	}
	return model.FailureUnknown
}

func appendNPBMeasurements(result *model.Result, specs []npbBenchmarkSpec, runs map[string][]npbBenchmarkSample, workers int) {
	for _, spec := range specs {
		samples := runs[spec.Name]
		contexts := []string{"1t", "nt"}
		for index, sample := range samples {
			if index >= len(contexts) || sample.MOPS <= 0 {
				continue
			}
			contextLabel := "1T"
			if index == 1 {
				contextLabel = fmt.Sprintf("%dT", workers)
			}
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "npb_" + strings.ToLower(spec.Name) + "_" + contexts[index] + "_mops",
				Label: "NPB " + spec.MeasurementLabel + " " + contextLabel,
				Value: sample.MOPS, Unit: "Mop/s", Display: model.FormatRate(sample.MOPS, "Mop/s"),
				Method:         npbMethodVersion + "-" + strings.ToLower(spec.Name) + "-" + contexts[index],
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		if workers > 1 && len(samples) >= 2 && samples[0].MOPS > 0 && samples[1].MOPS > 0 {
			scaling := samples[1].MOPS / samples[0].MOPS
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "npb_" + strings.ToLower(spec.Name) + "_scaling_ratio",
				Label: "NPB " + spec.Name + " 多线程扩展倍率",
				Value: scaling, Unit: "x", Display: fmt.Sprintf("%.2f×", scaling),
				Method:         npbMethodVersion + "-" + strings.ToLower(spec.Name) + "-scaling",
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
}

func npbResultsTable(specs []npbBenchmarkSpec, runs map[string][]npbBenchmarkSample, workers int) model.Table {
	table := model.Table{
		Title:          "NPB EP + FT Class A 原始结果",
		Columns:        []string{"Benchmark", "负载", "线程上下文", "Mop/s total", "Mop/s/thread", "耗时", "扩展倍率", "验证"},
		NumericColumns: []int{3, 4, 5, 6}, NumericHigherIsBetter: []bool{true, true, false, true},
	}
	for _, spec := range specs {
		samples := runs[spec.Name]
		for index, sample := range samples {
			contextName := "1T"
			if index == 1 {
				contextName = fmt.Sprintf("全线程（%dT）", workers)
			}
			mops, perThread, seconds, scaling, verification := "—", "—", "—", "—", "失败"
			if sample.MOPS > 0 {
				mops = model.FormatRate(sample.MOPS, "Mop/s")
				perThread = model.FormatRate(sample.MOPSPerThread, "Mop/s")
				seconds = fmt.Sprintf("%.2f s", sample.Seconds)
				verification = "SUCCESSFUL"
				if workers <= 1 {
					scaling = "不适用"
				} else if index == 0 {
					scaling = "1.00 x"
				} else if len(samples) >= 2 && samples[0].MOPS > 0 {
					scaling = fmt.Sprintf("%.2f x", sample.MOPS/samples[0].MOPS)
				}
			}
			table.Rows = append(table.Rows, []string{spec.Name, spec.Description, contextName, mops, perThread, seconds, scaling, verification})
		}
	}
	return table
}

func npbSummary(specs []npbBenchmarkSpec, runs map[string][]npbBenchmarkSample, workers int) string {
	parts := make([]string, 0, len(specs))
	for _, spec := range specs {
		samples := runs[spec.Name]
		if workers <= 1 && len(samples) >= 1 && samples[0].MOPS > 0 {
			parts = append(parts, fmt.Sprintf("%s 1T/NT（同一次实测）%s · 扩展不适用",
				spec.Name, model.FormatRate(samples[0].MOPS, "Mop/s")))
		} else if len(samples) >= 2 && samples[0].MOPS > 0 && samples[1].MOPS > 0 {
			parts = append(parts, fmt.Sprintf("%s 1T %s · %dT %s · %.2f×",
				spec.Name, model.FormatRate(samples[0].MOPS, "Mop/s"), workers,
				model.FormatRate(samples[1].MOPS, "Mop/s"), samples[1].MOPS/samples[0].MOPS))
		} else if len(samples) > 0 && samples[0].MOPS > 0 {
			parts = append(parts, spec.Name+" 1T "+model.FormatRate(samples[0].MOPS, "Mop/s"))
		}
	}
	if len(parts) == 0 {
		return "NPB EP/FT 未产出有效 Class A Mop/s"
	}
	return strings.Join(parts, " · ")
}
