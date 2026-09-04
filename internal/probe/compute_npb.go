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
}

type npbProbe struct{}

func (npbProbe) ID() string { return "npb" }

func (npbProbe) Run(ctx context.Context, env Environment) model.Result {
	return runNPBBenchmarks(ctx, env, npbBenchmarkSpecs)
}

func npbMethodology() model.Methodology {
	return model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "NAS Parallel Benchmarks OpenMP",
		Profile:         "probe.npb.profile",
		ComparisonScope: "probe.npb.comparison_scope",
	}
}

func runNPBBenchmarks(ctx context.Context, env Environment, specs []npbBenchmarkSpec) model.Result {
	return runNPBBenchmarksWithAllowance(ctx, env, specs, detectCPUAllowance())
}

func runNPBBenchmarksWithAllowance(ctx context.Context, env Environment, specs []npbBenchmarkSpec, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := model.NewResult("npb", "module.npb.title")
	result.Description = "probe.npb.description"
	result.Methodology = npbMethodology()
	result.Methodology.Parameters = newComparisonParameters()

	workers := allowance.Threads
	threadCounts := distinctBenchmarkThreadCounts(workers)
	singleCore := len(threadCounts) == 1
	runs := make(map[string][]npbBenchmarkSample, len(specs))
	paths := make(map[string]string, len(specs))
	validRuns := 0
	expectedRuns := len(specs) * len(threadCounts)
	for _, spec := range specs {
		path, err := LookupTool(spec.Binary)
		if err != nil {
			message := fmt.Sprintf("未找到固定 NPB %s Class A binary，%s 未运行", spec.Name, spec.Name)
			result.Status = model.StatusWarning
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
	environment1T := strings.Join(npbEnvironmentParameters(1), " ")
	environmentNT := strings.Join(npbEnvironmentParameters(workers), " ")
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.npb.field.engine", Value: model.RawValue("NASA NPB-OMP")},
		{Key: "version", Label: "probe.npb.field.version", Value: model.RawValue(npbExpectedVersion)},
		{Key: "method_version", Label: "probe.npb.field.method_version", Value: model.RawValue(npbMethodVersion)},
		{Key: "benchmarks", Label: "probe.npb.field.benchmarks", Value: model.RawValue("EP / FT")},
		{Key: "problem_class", Label: "probe.npb.field.problem_class", Value: model.RawValue(npbExpectedClass)},
		{Key: "threads", Label: "probe.npb.field.threads", Value: model.RawValue(benchmarkThreadField(workers))},
		{Key: "cpu_allowance", Label: "probe.npb.field.cpu_allowance", Value: model.RawValue(cpuAllowanceMachineValue(allowance))},
		{Key: "implementation", Label: "probe.npb.field.implementation", Value: model.RawValue("NPB3.4-OMP")},
		{Key: "compiler_flags", Label: "probe.npb.field.compiler_flags", Value: model.RawValue(npbCompileFlags)},
		{Key: "random_generator", Label: "probe.npb.field.random_generator", Value: model.RawValue(npbRandomGenerator)},
		{Key: "arguments", Label: "probe.npb.field.arguments", Value: model.RawValue("(none)")},
		{Key: "environment_1t", Label: "probe.npb.field.environment_1t", Value: model.RawValue(environment1T)},
		{Key: "environment_nt", Label: "probe.npb.field.environment_nt", Value: model.RawValue(environmentNT)},
	}
	for _, spec := range specs {
		path := paths[spec.Name]
		result.Fields = append(result.Fields, model.Field{Key: "binary_" + strings.ToLower(spec.Name), Label: "probe.npb.field.binary_" + strings.ToLower(spec.Name), Value: model.RawValue(fallback(path, "unavailable"))})
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", npbExpectedVersion)
	addComparisonParameter(result.Methodology.Parameters, "method_version", npbMethodVersion)
	addComparisonParameter(result.Methodology.Parameters, "problem_class", npbExpectedClass)
	addComparisonParameter(result.Methodology.Parameters, "threads", benchmarkThreadField(workers))
	addComparisonParameter(result.Methodology.Parameters, "implementation", "NPB3.4-OMP")
	addComparisonParameter(result.Methodology.Parameters, "compiler_flags", npbCompileFlags)
	addComparisonParameter(result.Methodology.Parameters, "random_generator", npbRandomGenerator)
	addComparisonParameter(result.Methodology.Parameters, "environment_1t", environment1T)
	addComparisonParameter(result.Methodology.Parameters, "environment_nt", environmentNT)
	for _, spec := range specs {
		for index, sample := range runs[spec.Name] {
			if singleCore && index > 0 {
				continue
			}
			if sample.Output == "" {
				continue
			}
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    "probe.npb.raw_output",
				Language: "text", Content: sample.Output,
			})
		}
	}
	result.Tables = append(result.Tables, npbResultsTable(specs, runs, workers))
	result.Sources = []model.Source{
		{Name: "NASA NAS Parallel Benchmarks", URL: "https://www.nas.nasa.gov/software/npb.html", Purpose: "probe.npb.source.purpose"},
	}
	result.Evidence = model.NewEvidence(validRuns, expectedRuns, "run")
	if validRuns < expectedRuns {
		result.Status = model.StatusWarning
	}
	finalizeNPBResult(&result, allowance)
	result.Finish(start)
	return result
}

func executeNPBBenchmark(ctx context.Context, path string, spec npbBenchmarkSpec, threads int) (npbBenchmarkSample, error) {
	sample := npbBenchmarkSample{Benchmark: spec.Name, Threads: threads, Binary: path}
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
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "npb_" + strings.ToLower(spec.Name) + "_" + contexts[index] + "_mops",
				Label: "probe.npb.metric.npb_" + strings.ToLower(spec.Name) + "_" + contexts[index] + "_mops",
				Value: sample.MOPS, Unit: "Mop/s", Display: model.RawValue(model.FormatRate(sample.MOPS, "Mop/s")),
				Method:         npbMethodVersion + "-" + strings.ToLower(spec.Name) + "-" + contexts[index],
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		if workers > 1 && len(samples) >= 2 && samples[0].MOPS > 0 && samples[1].MOPS > 0 {
			scaling := samples[1].MOPS / samples[0].MOPS
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "npb_" + strings.ToLower(spec.Name) + "_scaling_ratio",
				Label: "probe.npb.metric.npb_" + strings.ToLower(spec.Name) + "_scaling_ratio",
				Value: scaling, Unit: "x", Display: model.RawValue(fmt.Sprintf("%.2f×", scaling)),
				Method:         npbMethodVersion + "-" + strings.ToLower(spec.Name) + "-scaling",
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
}

func npbResultsTable(specs []npbBenchmarkSpec, runs map[string][]npbBenchmarkSample, workers int) model.Table {
	table := model.Table{
		Key:   "benchmark.npb.results",
		Title: "probe.npb.table.title",
		Columns: []model.TableColumn{
			{Key: "benchmark", Label: "probe.npb.column.benchmark"},
			{Key: "load", Label: "probe.npb.column.workload"},
			{Key: "worker_context", Label: "probe.npb.column.context"},
			{Key: "mops_total", Label: "probe.npb.column.mops_total", Numeric: true, HigherIsBetter: true},
			{Key: "mops_per_thread", Label: "probe.npb.column.mops_per_thread", Numeric: true, HigherIsBetter: true},
			{Key: "elapsed_seconds", Label: "probe.npb.column.elapsed", Numeric: true, HigherIsBetter: false},
			{Key: "scaling_ratio", Label: "probe.npb.column.scaling", Numeric: true, HigherIsBetter: true},
			{Key: "verification", Label: "probe.npb.column.verification"},
		},
	}
	seen := make(map[string]int, 2)
	for _, spec := range specs {
		samples := runs[spec.Name]
		for _, sample := range samples {
			contextName := "1T"
			benchmark := strings.ToUpper(strings.TrimSpace(spec.Name))
			seen[benchmark]++
			if seen[benchmark] > 1 {
				if workers <= 1 {
					contextName = "NT(1T-reused)"
				} else {
					contextName = fmt.Sprintf("NT(%dT)", workers)
				}
			}
			mops, perThread, seconds, scaling := "—", "—", "—", "—"
			verification := model.KeyValue("probe.npb.verification.failed")
			if sample.MOPS > 0 {
				mops = model.FormatRate(sample.MOPS, "Mop/s")
				perThread = model.FormatRate(sample.MOPSPerThread, "Mop/s")
				seconds = fmt.Sprintf("%.2f s", sample.Seconds)
				verification = model.KeyValue("probe.npb.verification.successful")
				if workers <= 1 {
					scaling = "na"
				} else if seen[benchmark] == 1 {
					scaling = "1.00 x"
				} else if len(samples) >= 2 && samples[0].MOPS > 0 {
					scaling = fmt.Sprintf("%.2f x", sample.MOPS/samples[0].MOPS)
				}
			} else if workers <= 1 {
				scaling = "na"
			} else {
				scaling = "unavailable"
			}
			table.Rows = append(table.Rows, []model.Value{
				model.RawValue(spec.Name), workloadValue(spec.Name), model.RawValue(contextName),
				model.RawValue(mops), model.RawValue(perThread), model.RawValue(seconds),
				model.RawValue(scaling), verification,
			})
		}
	}
	return table
}

func workloadValue(benchmark string) model.Value {
	switch strings.ToUpper(strings.TrimSpace(benchmark)) {
	case "EP":
		return model.KeyValue("probe.npb.workload.ep")
	case "FT":
		return model.KeyValue("probe.npb.workload.ft")
	default:
		return model.KeyValue("probe.npb.workload.unknown")
	}
}

func finalizeNPBResult(result *model.Result, allowance cpuAllowance) {
	if result == nil {
		return
	}
	result.Notes = npbNotes(*result, allowance)
	result.SummaryMessages = []model.Message{npbSummaryMessage(*result, allowance.Threads)}
}

func npbNotes(result model.Result, allowance cpuAllowance) []string {
	notes := make([]string, 0, 7)
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.npb.note.single_core")
	} else {
		notes = append(notes, "probe.npb.note.separate_runs")
	}
	notes = append(notes,
		"probe.npb.note.workloads",
		"probe.npb.note.verification",
		"probe.npb.note.no_composite_score",
	)
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.npb.note.quota_limited")
	}
	for _, failure := range result.Failures {
		switch failure.Stage {
		case "tool_lookup":
			notes = append(notes, "probe.npb.note.tool_missing")
		case "benchmark_run":
			notes = append(notes, "probe.npb.note.run_failure")
		}
	}
	seen := make(map[string]bool, len(notes))
	out := notes[:0]
	for _, note := range notes {
		if seen[note] {
			continue
		}
		seen[note] = true
		out = append(out, note)
	}
	return out
}

func npbSummaryMessage(result model.Result, workers int) model.Message {
	if summary := npbMachineSummary(result, workers); summary != "" {
		return model.NewMessage("probe.npb.summary.values", summary)
	}
	return model.NewMessage("probe.npb.summary.none")
}

func npbMachineSummary(result model.Result, workers int) string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display.Text()
		}
	}
	parts := make([]string, 0, 6)
	for _, benchmark := range []string{"ep", "ft"} {
		upper := strings.ToUpper(benchmark)
		if value := values["npb_"+benchmark+"_1t_mops"]; value != "" {
			parts = append(parts, upper+":1T="+value)
		}
		if workers > 1 {
			if value := values["npb_"+benchmark+"_nt_mops"]; value != "" {
				parts = append(parts, fmt.Sprintf("%s:NT(%dT)=%s", upper, workers, value))
			}
			if value := values["npb_"+benchmark+"_scaling_ratio"]; value != "" {
				parts = append(parts, upper+":scaling="+value)
			}
		}
	}
	return strings.Join(parts, ";")
}
