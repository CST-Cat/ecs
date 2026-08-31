package probe

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

const (
	zstdMethodVersion           = "zstd-silesia-l3-v1"
	zstdExpectedVersion         = "1.5.7"
	zstdCompressionLevel        = 3
	zstdEvaluationSeconds       = 5
	zstdCorpusName              = "ecs-silesia-v1.corpus"
	zstdCorpusSHA256            = "8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0"
	zstdCorpusBytes       int64 = 211938580
	zstdRunTimeout              = 3 * time.Minute
)

var (
	zstdVersionPattern = regexp.MustCompile(`(?i)\bv([0-9]+\.[0-9]+\.[0-9]+)\b`)
	zstdBenchHeader    = regexp.MustCompile(`(?m)^\s*bench\s+([0-9]+\.[0-9]+\.[0-9]+)\s*:\s*input\s+([0-9]+)\s+bytes,\s*([0-9]+)\s+seconds,\s*([0-9]+)\s+KB\s+blocks\s*$`)
	zstdBenchResult    = regexp.MustCompile(`(?m)^\s*-([0-9]+)\s+([0-9]+)\s+\(([0-9]+(?:\.[0-9]+)?)\)\s+([0-9]+(?:\.[0-9]+)?)\s+MB/s\s+([0-9]+(?:\.[0-9]+)?)\s+MB/s(?:\s+\S+)?\s*$`)
)

type zstdBenchmarkContract struct {
	Version      string
	Level        int
	Seconds      int
	CorpusName   string
	CorpusSHA256 string
	CorpusBytes  int64
}

var defaultZstdContract = zstdBenchmarkContract{
	Version:      zstdExpectedVersion,
	Level:        zstdCompressionLevel,
	Seconds:      zstdEvaluationSeconds,
	CorpusName:   zstdCorpusName,
	CorpusSHA256: zstdCorpusSHA256,
	CorpusBytes:  zstdCorpusBytes,
}

type zstdBenchmarkSample struct {
	Version        string
	Threads        int
	InputBytes     int64
	CompressedSize int64
	Ratio          float64
	CompressMBPS   float64
	DecompressMBPS float64
	Output         string
	Args           []string
}

type zstdProbe struct{}

func (zstdProbe) ID() string { return "zstd" }

func (zstdProbe) Run(ctx context.Context, env Environment) model.Result {
	path, err := exec.LookPath("zstd")
	if err != nil {
		return missingZstdResult("zstd", err)
	}
	corpus, err := findZstdCorpus(path, defaultZstdContract)
	if err != nil {
		return missingZstdResult(zstdCorpusName, err)
	}
	return runZstdBenchmark(ctx, env, path, corpus, defaultZstdContract)
}

func missingZstdResult(target string, err error) model.Result {
	start := time.Now()
	allowance := detectCPUAllowance()
	result := model.NewResult("zstd", "module.zstd.title")
	result.Description = "probe.zstd.description"
	result.Methodology = zstdMethodology(defaultZstdContract)
	result.Methodology.Parameters = newComparisonParameters()
	result.Status = model.StatusWarning
	message := ""
	if err != nil {
		message = err.Error()
	}
	result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: target, Count: 1, Message: message})
	result.Evidence = model.NewEvidence(0, len(distinctBenchmarkThreadCounts(allowance.Threads)), "run")
	finalizeZstdResult(&result, allowance)
	result.Finish(start)
	return result
}

func zstdMethodology(contract zstdBenchmarkContract) model.Methodology {
	return model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "zstd",
		Profile:         "probe.zstd.profile",
		ComparisonScope: "probe.zstd.comparison_scope",
	}
}

func findZstdCorpus(zstdPath string, contract zstdBenchmarkContract) (string, error) {
	candidates := make([]string, 0, 4)
	if configured := strings.TrimSpace(os.Getenv("ECS_ZSTD_CORPUS")); configured != "" {
		candidates = append(candidates, configured)
	}
	candidates = append(candidates,
		filepath.Join("/usr/local/share/ecs/corpus", contract.CorpusName),
		filepath.Join("/usr/share/ecs/corpus", contract.CorpusName),
	)
	seen := make(map[string]bool, len(candidates))
	var diagnostics []string
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if err := verifyZstdCorpus(candidate, contract); err == nil {
			return candidate, nil
		} else if _, statErr := os.Stat(candidate); statErr == nil {
			diagnostics = append(diagnostics, err.Error())
		}
	}
	if len(diagnostics) > 0 {
		return "", fmt.Errorf("固定 corpus 校验失败: %s", strings.Join(diagnostics, "; "))
	}
	return "", fmt.Errorf("固定 corpus %s 不在 ECS_ZSTD_CORPUS 或系统 corpus 目录中", contract.CorpusName)
}

func verifyZstdCorpus(path string, contract zstdBenchmarkContract) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("corpus 不是普通文件: %s", path)
	}
	if info.Size() != contract.CorpusBytes {
		return fmt.Errorf("corpus 大小为 %d bytes，期望 %d: %s", info.Size(), contract.CorpusBytes, path)
	}
	actual := zstdCorpusDigest(path)
	if !strings.EqualFold(actual, contract.CorpusSHA256) {
		return fmt.Errorf("corpus SHA-256 为 %s，期望 %s: %s", fallback(actual, "unavailable"), contract.CorpusSHA256, path)
	}
	return nil
}

func zstdCorpusDigest(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func runZstdBenchmark(ctx context.Context, env Environment, path, corpus string, contract zstdBenchmarkContract) model.Result {
	return runZstdBenchmarkWithAllowance(ctx, env, path, corpus, contract, detectCPUAllowance())
}

func runZstdBenchmarkWithAllowance(ctx context.Context, env Environment, path, corpus string, contract zstdBenchmarkContract, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := model.NewResult("zstd", "module.zstd.title")
	result.Description = "probe.zstd.description"
	result.Methodology = zstdMethodology(contract)
	result.Methodology.Parameters = newComparisonParameters()
	threadCounts := distinctBenchmarkThreadCounts(allowance.Threads)

	versionLine := commandVersion(ctx, path)
	version, ok := parseZstdVersion(versionLine)
	if !ok || version != contract.Version {
		result.Status = model.StatusWarning
		result.AddFailure(model.Failure{
			Category: model.FailureUnsupported, Stage: "version_check", Target: path,
			Count: 1, Message: versionLine,
		})
		result.Fields = []model.Field{
			{Key: "engine", Label: "probe.zstd.field.engine", Value: model.RawValue("zstd")},
			{Key: "version", Label: "probe.zstd.field.version", Value: model.RawValue(versionLine)},
			{Key: "required_version", Label: "probe.zstd.field.required_version", Value: model.RawValue(contract.Version)},
		}
		addComparisonParameter(result.Methodology.Parameters, "tool_version", versionLine)
		result.Evidence = model.NewEvidence(0, len(threadCounts), "run")
		finalizeZstdResult(&result, allowance)
		result.Finish(start)
		return result
	}
	if err := verifyZstdCorpus(corpus, contract); err != nil {
		result.Status = model.StatusWarning
		result.AddFailure(model.Failure{
			Category: model.FailureUnsupported, Stage: "corpus_verify", Target: contract.CorpusName,
			Count: 1, Message: err.Error(),
		})
		result.Evidence = model.NewEvidence(0, len(threadCounts), "run")
		finalizeZstdResult(&result, allowance)
		result.Finish(start)
		return result
	}

	workers := allowance.Threads
	singleCore := len(threadCounts) == 1
	runs := make([]zstdBenchmarkSample, 2)
	validRuns := 0
	for index, threads := range threadCounts {
		sample, err := executeZstdBenchmark(ctx, path, corpus, threads, contract)
		runs[index] = sample
		if err == nil {
			validRuns++
			continue
		}
		result.Status = model.StatusWarning
		contextName := "1 worker"
		if index == 1 {
			contextName = fmt.Sprintf("全 worker（%d）", workers)
		}
		result.AddFailure(model.Failure{
			Category: model.FailureParse, Stage: "benchmark_run", Target: contextName,
			Count: 1, Message: err.Error(),
		})
	}
	if singleCore {
		runs[1] = runs[0]
		runs[1].Threads = 1
	}

	appendZstdMeasurements(&result, runs, workers)
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.zstd.field.engine", Value: model.RawValue("zstd")},
		{Key: "version", Label: "probe.zstd.field.version", Value: model.RawValue(versionLine)},
		{Key: "method_version", Label: "probe.zstd.field.method_version", Value: model.RawValue(zstdMethodVersion)},
		{Key: "compression_level", Label: "probe.zstd.field.compression_level", Value: model.RawValue(strconv.Itoa(contract.Level))},
		{Key: "threads", Label: "probe.zstd.field.threads", Value: model.RawValue(benchmarkThreadField(workers))},
		{Key: "cpu_allowance", Label: "probe.zstd.field.cpu_allowance", Value: model.RawValue(cpuAllowanceMachineValue(allowance))},
		{Key: "duration", Label: "probe.zstd.field.duration", Value: model.RawValue(fmt.Sprintf("%ds", contract.Seconds))},
		{Key: "corpus", Label: "probe.zstd.field.corpus", Value: model.RawValue(contract.CorpusName)},
		{Key: "corpus_bytes", Label: "probe.zstd.field.corpus_bytes", Value: model.RawValue(strconv.FormatInt(contract.CorpusBytes, 10) + " bytes")},
		{Key: "corpus_construction", Label: "probe.zstd.field.corpus_construction", Value: model.RawValue("dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml")},
		{Key: "arguments_1t", Label: "probe.zstd.field.arguments_1t", Value: model.RawValue(strings.Join(runs[0].Args, " "))},
		{Key: "arguments_nt", Label: "probe.zstd.field.arguments_nt", Value: model.RawValue(strings.Join(runs[1].Args, " "))},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", versionLine)
	addComparisonParameter(result.Methodology.Parameters, "method_version", zstdMethodVersion)
	addComparisonParameter(result.Methodology.Parameters, "compression_level", strconv.Itoa(contract.Level))
	addComparisonParameter(result.Methodology.Parameters, "threads", benchmarkThreadField(workers))
	addComparisonParameter(result.Methodology.Parameters, "duration", fmt.Sprintf("%ds", contract.Seconds))
	addComparisonParameter(result.Methodology.Parameters, "corpus_bytes", strconv.FormatInt(contract.CorpusBytes, 10)+" bytes")
	if len(runs[0].Args) > 0 {
		addComparisonParameter(result.Methodology.Parameters, "arguments_1t", strings.Join(zstdComparisonArguments(runs[0].Args), " "))
	}
	if len(runs[1].Args) > 0 {
		addComparisonParameter(result.Methodology.Parameters, "arguments_nt", strings.Join(zstdComparisonArguments(runs[1].Args), " "))
	}
	for index, sample := range runs {
		if singleCore && index > 0 {
			continue
		}
		if sample.Output == "" {
			continue
		}
		result.TextBlocks = append(result.TextBlocks, model.TextBlock{Title: "probe.zstd.raw_output", Language: "text", Content: sample.Output})
	}
	result.Tables = append(result.Tables, zstdThroughputTable(runs, workers))
	result.Sources = []model.Source{
		{Name: "Zstandard", URL: "https://github.com/facebook/zstd", Purpose: "probe.zstd.source.zstandard"},
		{Name: "Silesia Corpus", URL: "https://data-compression.info/Corpora/SilesiaCorpus/", Purpose: "probe.zstd.source.silesia"},
	}
	finalizeZstdResult(&result, allowance)
	result.Evidence = model.NewEvidence(validRuns, len(threadCounts), "run")
	result.Finish(start)
	return result
}

func parseZstdVersion(output string) (string, bool) {
	match := zstdVersionPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

// zstdComparisonArguments removes the per-run corpus path from the argument
// fingerprint.  The command keeps the stable benchmark contract in its first
// four tokens; the fifth token is a temporary path selected by the producer.
func zstdComparisonArguments(args []string) []string {
	parts := strings.Fields(strings.Join(args, " "))
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return parts
}

func executeZstdBenchmark(ctx context.Context, path, corpus string, threads int, contract zstdBenchmarkContract) (zstdBenchmarkSample, error) {
	sample := zstdBenchmarkSample{Threads: threads}
	if threads < 1 {
		return sample, fmt.Errorf("zstd worker 数必须为正数")
	}
	args := []string{
		"-q",
		"-b" + strconv.Itoa(contract.Level),
		"-i" + strconv.Itoa(contract.Seconds),
		"-T" + strconv.Itoa(threads),
		corpus,
	}
	sample.Args = append([]string(nil), args...)
	runCtx, cancel := context.WithTimeout(ctx, zstdRunTimeout)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = benchmarkEnvironment(nil)
	output, err := command.CombinedOutput()
	sample.Output = normalizeCarriageReturnOutput(output)
	if runCtx.Err() != nil {
		return sample, runCtx.Err()
	}
	if err != nil {
		return sample, fmt.Errorf("zstd 执行失败: %w: %s", err, tailText(sample.Output, 400))
	}
	parsed, err := parseZstdBenchmarkOutput(sample.Output, contract)
	parsed.Args = sample.Args
	parsed.Threads = threads
	parsed.Output = sample.Output
	if err != nil {
		return parsed, err
	}
	return parsed, nil
}

func parseZstdBenchmarkOutput(output string, contract zstdBenchmarkContract) (zstdBenchmarkSample, error) {
	headers := zstdBenchHeader.FindAllStringSubmatch(output, -1)
	if len(headers) != 1 || len(headers[0]) != 5 {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd 输出缺少唯一 benchmark header")
	}
	if headers[0][1] != contract.Version {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd benchmark header 版本为 %s，期望 %s", headers[0][1], contract.Version)
	}
	inputBytes, err := strconv.ParseInt(headers[0][2], 10, 64)
	if err != nil || inputBytes != contract.CorpusBytes {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd benchmark input bytes 为 %s，期望 %d", headers[0][2], contract.CorpusBytes)
	}
	seconds, err := strconv.Atoi(headers[0][3])
	if err != nil || seconds != contract.Seconds {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd benchmark duration 为 %s，期望 %d", headers[0][3], contract.Seconds)
	}
	rows := zstdBenchResult.FindAllStringSubmatch(output, -1)
	if len(rows) != 1 || len(rows[0]) != 6 {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd 输出缺少唯一完整吞吐结果")
	}
	level, err := strconv.Atoi(rows[0][1])
	if err != nil || level != contract.Level {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd benchmark level 为 %s，期望 %d", rows[0][1], contract.Level)
	}
	compressedSize, err := strconv.ParseInt(rows[0][2], 10, 64)
	if err != nil || compressedSize <= 0 || compressedSize >= inputBytes {
		return zstdBenchmarkSample{}, fmt.Errorf("zstd compressed size 无效: %s", rows[0][2])
	}
	ratio, ratioErr := strconv.ParseFloat(rows[0][3], 64)
	compress, compressErr := strconv.ParseFloat(rows[0][4], 64)
	decompress, decompressErr := strconv.ParseFloat(rows[0][5], 64)
	for name, value := range map[string]struct {
		value float64
		err   error
	}{
		"ratio": {ratio, ratioErr}, "compression": {compress, compressErr}, "decompression": {decompress, decompressErr},
	} {
		if value.err != nil || value.value <= 0 || math.IsNaN(value.value) || math.IsInf(value.value, 0) {
			return zstdBenchmarkSample{}, fmt.Errorf("zstd %s 数值无效", name)
		}
	}
	return zstdBenchmarkSample{
		Version: headers[0][1], InputBytes: inputBytes, CompressedSize: compressedSize,
		Ratio: ratio, CompressMBPS: compress, DecompressMBPS: decompress,
	}, nil
}

func normalizeCarriageReturnOutput(output []byte) string {
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return sanitizeCommandOutput([]byte(text))
}

func benchmarkEnvironment(overrides map[string]string) []string {
	blocked := map[string]bool{
		"LC_ALL": true, "LANG": true,
		"LD_PRELOAD": true, "LD_LIBRARY_PATH": true,
		"OMP_NUM_THREADS": true, "OMP_DYNAMIC": true, "OMP_PROC_BIND": true,
		"OMP_PLACES": true, "OMP_SCHEDULE": true, "OMP_DISPLAY_ENV": true,
		"OMP_THREAD_LIMIT": true, "OMP_STACKSIZE": true, "GOMP_CPU_AFFINITY": true,
		"NPB_TIMER_FLAG": true,
		"OPENSSL_CONF":   true, "OPENSSL_CONF_INCLUDE": true, "OPENSSL_MODULES": true,
		"OPENSSL_ENGINES": true, "OPENSSL_ia32cap": true, "OPENSSL_armcap": true,
		"OPENSSL_ppccap": true, "OPENSSL_s390xcap": true, "OPENSSL_riscvcap": true,
	}
	for key := range overrides {
		blocked[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(overrides)+2)
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && blocked[key] {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "LC_ALL=C", "LANG=C")
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func appendZstdMeasurements(result *model.Result, runs []zstdBenchmarkSample, workers int) {
	if len(runs) < 2 {
		return
	}
	contexts := []string{"1t", "nt"}
	for index, sample := range runs {
		if sample.CompressMBPS > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "zstd_compress_" + contexts[index] + "_mb_s", Label: "probe.zstd.metric.zstd_compress_" + contexts[index] + "_mb_s",
				Value: sample.CompressMBPS, Unit: "MB/s", Display: model.RawValue(model.FormatRate(sample.CompressMBPS, "MB/s")),
				Method: zstdMethodVersion + "-compress-" + contexts[index], HigherIsBetter: model.BoolPtr(true),
			})
		}
		if sample.DecompressMBPS > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "zstd_decompress_" + contexts[index] + "_mb_s", Label: "probe.zstd.metric.zstd_decompress_" + contexts[index] + "_mb_s",
				Value: sample.DecompressMBPS, Unit: "MB/s", Display: model.RawValue(model.FormatRate(sample.DecompressMBPS, "MB/s")),
				Method: zstdMethodVersion + "-decompress-" + contexts[index], HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
	if workers > 1 && runs[0].CompressMBPS > 0 && runs[1].CompressMBPS > 0 {
		appendZstdScalingMeasurements(result, "compress", runs[1].CompressMBPS/runs[0].CompressMBPS, workers)
	}
	if workers > 1 && runs[0].DecompressMBPS > 0 && runs[1].DecompressMBPS > 0 {
		appendZstdScalingMeasurements(result, "decompress", runs[1].DecompressMBPS/runs[0].DecompressMBPS, workers)
	}
}

func appendZstdScalingMeasurements(result *model.Result, key string, scaling float64, workers int) {
	efficiency := scaling / float64(workers) * 100
	result.Measurements = append(result.Measurements,
		model.Measurement{
			Key: "zstd_" + key + "_scaling_ratio", Label: "probe.zstd.metric.zstd_" + key + "_scaling_ratio",
			Value: scaling, Unit: "x", Display: model.RawValue(fmt.Sprintf("%.2f×", scaling)),
			Method: zstdMethodVersion + "-" + key + "-scaling", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: "zstd_" + key + "_per_worker_efficiency_percent", Label: "probe.zstd.metric.zstd_" + key + "_per_worker_efficiency_percent",
			Value: efficiency, Unit: "%", Display: model.RawValue(fmt.Sprintf("%.1f %%", efficiency)),
			Method: zstdMethodVersion + "-" + key + "-scaling", HigherIsBetter: model.BoolPtr(true),
		},
	)
}

func zstdThroughputTable(runs []zstdBenchmarkSample, workers int) model.Table {
	table := model.Table{
		Key:   "benchmark.zstd.throughput",
		Title: "probe.zstd.table.title",
		Columns: []model.TableColumn{
			{Key: "worker_context", Label: "probe.zstd.column.context"},
			{Key: "compress_mbps", Label: "probe.zstd.column.compress", Numeric: true, HigherIsBetter: true},
			{Key: "decompress_mbps", Label: "probe.zstd.column.decompress", Numeric: true, HigherIsBetter: true},
			{Key: "compress_scaling_ratio", Label: "probe.zstd.column.compress_scaling", Numeric: true, HigherIsBetter: true},
			{Key: "decompress_scaling_ratio", Label: "probe.zstd.column.decompress_scaling", Numeric: true, HigherIsBetter: true},
			{Key: "compress_efficiency_percent", Label: "probe.zstd.column.compress_efficiency", Numeric: true, HigherIsBetter: true},
			{Key: "decompress_efficiency_percent", Label: "probe.zstd.column.decompress_efficiency", Numeric: true, HigherIsBetter: true},
		},
	}
	for index, sample := range runs {
		contextName := "1T"
		if index == 1 {
			if workers <= 1 {
				contextName = "NT(1T-reused)"
			} else {
				contextName = fmt.Sprintf("NT(%dT)", workers)
			}
		}
		compress, decompress := "—", "—"
		if sample.CompressMBPS > 0 {
			compress = model.FormatRate(sample.CompressMBPS, "MB/s")
		}
		if sample.DecompressMBPS > 0 {
			decompress = model.FormatRate(sample.DecompressMBPS, "MB/s")
		}
		compressScale, decompressScale, compressEfficiency, decompressEfficiency := "—", "—", "—", "—"
		if workers > 1 && index == 0 && sample.CompressMBPS > 0 {
			compressScale, compressEfficiency = "1.00 x", "100.0 %"
		}
		if workers > 1 && index == 0 && sample.DecompressMBPS > 0 {
			decompressScale, decompressEfficiency = "1.00 x", "100.0 %"
		}
		if workers > 1 && index == 1 && len(runs) > 1 && runs[0].CompressMBPS > 0 && sample.CompressMBPS > 0 {
			ratio := sample.CompressMBPS / runs[0].CompressMBPS
			compressScale = fmt.Sprintf("%.2f x", ratio)
			compressEfficiency = fmt.Sprintf("%.1f %%", ratio/float64(workers)*100)
		}
		if workers > 1 && index == 1 && len(runs) > 1 && runs[0].DecompressMBPS > 0 && sample.DecompressMBPS > 0 {
			ratio := sample.DecompressMBPS / runs[0].DecompressMBPS
			decompressScale = fmt.Sprintf("%.2f x", ratio)
			decompressEfficiency = fmt.Sprintf("%.1f %%", ratio/float64(workers)*100)
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue(contextName), model.RawValue(compress), model.RawValue(decompress), model.RawValue(compressScale), model.RawValue(decompressScale), model.RawValue(compressEfficiency), model.RawValue(decompressEfficiency),
		})
		if workers <= 1 {
			row := table.Rows[len(table.Rows)-1]
			for column := 3; column < len(row); column++ {
				row[column] = model.RawValue("na")
			}
		}
	}
	return table
}

func finalizeZstdResult(result *model.Result, allowance cpuAllowance) {
	if result == nil {
		return
	}
	result.Notes = zstdNotes(*result, allowance)
	result.SummaryMessages = []model.Message{zstdSummaryMessage(*result, allowance.Threads)}
}

func zstdNotes(result model.Result, allowance cpuAllowance) []string {
	notes := []string{
		"probe.zstd.note.contract",
		"probe.zstd.note.corpus",
		"probe.zstd.note.units",
		"probe.zstd.note.decompression",
		"probe.zstd.note.no_composite_score",
	}
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.zstd.note.single_core")
	} else {
		notes = append(notes, "probe.zstd.note.separate_runs")
	}
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.zstd.note.quota_limited")
	}
	for _, failure := range result.Failures {
		switch failure.Stage {
		case "tool_lookup":
			notes = append(notes, "probe.zstd.note.tool_missing")
		case "version_check":
			notes = append(notes, "probe.zstd.note.version_mismatch")
		case "corpus_verify":
			notes = append(notes, "probe.zstd.note.corpus_invalid")
		case "benchmark_run":
			notes = append(notes, "probe.zstd.note.run_failure")
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

func zstdSummaryMessage(result model.Result, workers int) model.Message {
	if summary := zstdMachineSummary(result, workers); summary != "" {
		return model.NewMessage("probe.zstd.summary.values", summary)
	}
	return model.NewMessage("probe.zstd.summary.none")
}

func zstdMachineSummary(result model.Result, workers int) string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display.Text()
		}
	}
	parts := make([]string, 0, 3)
	if value := values["zstd_compress_1t_mb_s"]; value != "" {
		parts = append(parts, "compress:1T="+value)
	}
	if workers > 1 {
		if value := values["zstd_compress_nt_mb_s"]; value != "" {
			parts = append(parts, fmt.Sprintf("compress:NT(%dT)=%s", workers, value))
		}
		if value := values["zstd_compress_scaling_ratio"]; value != "" {
			parts = append(parts, "compress:scaling="+value)
		}
	}
	return strings.Join(parts, ";")
}
