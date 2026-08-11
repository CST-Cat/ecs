package probe

import (
	"context"
	"fmt"
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
	zstdMethodVersion            = "zstd-silesia-l3-v1"
	zstdExpectedVersion          = "1.5.7"
	zstdCompressionLevel         = 3
	zstdEvaluationSeconds        = 5
	zstdCorpusName               = "ecs-silesia-v1.corpus"
	zstdCorpusSHA256             = "8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0"
	zstdCorpusSourceSHA256       = "0626e25f45c0ffb5dc801f13b7c82a3b75743ba07e3a71835a41e3d9f63c77af"
	zstdCorpusBytes        int64 = 211938580
	zstdRunTimeout               = 3 * time.Minute
)

var (
	zstdVersionPattern = regexp.MustCompile(`(?i)\bv([0-9]+\.[0-9]+\.[0-9]+)\b`)
	zstdBenchHeader    = regexp.MustCompile(`(?m)^\s*bench\s+([0-9]+\.[0-9]+\.[0-9]+)\s*:\s*input\s+([0-9]+)\s+bytes,\s*([0-9]+)\s+seconds,\s*([0-9]+)\s+KB\s+blocks\s*$`)
	zstdBenchResult    = regexp.MustCompile(`(?m)^\s*-([0-9]+)\s+([0-9]+)\s+\(([0-9]+(?:\.[0-9]+)?)\)\s+([0-9]+(?:\.[0-9]+)?)\s+MB/s\s+([0-9]+(?:\.[0-9]+)?)\s+MB/s(?:\s+\S+)?\s*$`)
)

type zstdBenchmarkContract struct {
	Version          string
	Level            int
	Seconds          int
	CorpusName       string
	CorpusSHA256     string
	CorpusSourceHash string
	CorpusBytes      int64
}

var defaultZstdContract = zstdBenchmarkContract{
	Version:          zstdExpectedVersion,
	Level:            zstdCompressionLevel,
	Seconds:          zstdEvaluationSeconds,
	CorpusName:       zstdCorpusName,
	CorpusSHA256:     zstdCorpusSHA256,
	CorpusSourceHash: zstdCorpusSourceSHA256,
	CorpusBytes:      zstdCorpusBytes,
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

func (zstdProbe) ID() string         { return "zstd" }
func (zstdProbe) Title() string      { return "zstd 压缩性能" }
func (zstdProbe) NeedsNetwork() bool { return false }

func (zstdProbe) Run(ctx context.Context, env Environment) model.Result {
	path, err := exec.LookPath("zstd")
	if err != nil {
		return missingZstdResult("未找到固定版本 zstd，压缩基准未运行", "zstd")
	}
	corpus, err := findZstdCorpus(path, defaultZstdContract)
	if err != nil {
		result := missingZstdResult("未找到或无法验证固定 Silesia corpus，zstd 基准未运行", zstdCorpusName)
		result.Notes = append(result.Notes, err.Error())
		return result
	}
	return runZstdBenchmark(ctx, env, path, corpus, defaultZstdContract)
}

func missingZstdResult(summary, target string) model.Result {
	start := time.Now()
	result := model.NewResult("zstd", "zstd 压缩性能")
	result.Description = "固定 Silesia corpus 上的 zstd 压缩与解压吞吐，分别运行 1 worker 与当前 CPU allowance 的全 worker"
	result.Methodology = zstdMethodology(defaultZstdContract)
	result.Status = model.StatusWarning
	result.Summary = summary
	result.AddFailure(model.Failure{
		Category: model.FailureToolMissing, Stage: "tool_lookup", Target: target,
		Count: 1, Message: summary,
	})
	result.Evidence = model.NewEvidence(0, 2, "run")
	result.Notes = append(result.Notes,
		"请使用 run.sh；它会从已校验的 ecs-tools 包提供固定 zstd binary，并从独立 Release 资产准备固定 corpus。ecs 不生成自定义压缩综合分。",
	)
	result.Finish(start)
	return result
}

func zstdMethodology(contract zstdBenchmarkContract) model.Methodology {
	return model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "zstd",
		Profile:         fmt.Sprintf("Silesia v1 · level=%d · -i%d · -T1/-TN", contract.Level, contract.Seconds),
		ComparisonScope: "相同 zstd 版本、corpus SHA-256、压缩等级、评估时长、线程数与 method version",
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
	actual := binarySHA256(path)
	if !strings.EqualFold(actual, contract.CorpusSHA256) {
		return fmt.Errorf("corpus SHA-256 为 %s，期望 %s: %s", fallback(actual, "unavailable"), contract.CorpusSHA256, path)
	}
	return nil
}

func runZstdBenchmark(ctx context.Context, env Environment, path, corpus string, contract zstdBenchmarkContract) model.Result {
	start := time.Now()
	result := model.NewResult("zstd", "zstd 压缩性能")
	result.Description = "固定 Silesia corpus 上的 zstd 压缩与解压吞吐，分别运行 1 worker 与当前 CPU allowance 的全 worker"
	result.Methodology = zstdMethodology(contract)

	versionLine := commandVersion(ctx, path)
	version, ok := parseZstdVersion(versionLine)
	if !ok || version != contract.Version {
		result.Status = model.StatusWarning
		result.Summary = fmt.Sprintf("zstd 版本不符合固定口径：检测到 %s，要求 %s", fallback(version, "unknown"), contract.Version)
		result.AddFailure(model.Failure{
			Category: model.FailureUnsupported, Stage: "version_check", Target: path,
			Count: 1, Message: result.Summary,
		})
		result.Fields = []model.Field{
			{Key: "engine", Label: "标准工具", Value: "zstd"},
			{Key: "version", Label: "zstd 版本", Value: versionLine},
			{Key: "binary_sha256", Label: "zstd SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
			{Key: "required_version", Label: "固定版本", Value: contract.Version},
			{Key: "corpus_sha256", Label: "corpus SHA-256", Value: contract.CorpusSHA256},
		}
		result.Evidence = model.NewEvidence(0, 2, "run")
		result.Notes = append(result.Notes, "请使用 run.sh 提供版本与哈希均经过 ecs-tools manifest 校验的 zstd binary。")
		result.Finish(start)
		return result
	}
	if err := verifyZstdCorpus(corpus, contract); err != nil {
		result.Status = model.StatusWarning
		result.Summary = "固定 Silesia corpus 校验失败，zstd 基准未运行"
		result.AddFailure(model.Failure{
			Category: model.FailureUnsupported, Stage: "corpus_verify", Target: contract.CorpusName,
			Count: 1, Message: err.Error(),
		})
		result.Evidence = model.NewEvidence(0, 2, "run")
		result.Notes = append(result.Notes, err.Error())
		result.Finish(start)
		return result
	}

	allowance := detectCPUAllowance()
	workers := allowance.Threads
	runs := make([]zstdBenchmarkSample, 2)
	requested := []int{1, workers}
	validRuns := 0
	for index, threads := range requested {
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
		result.Notes = append(result.Notes, fmt.Sprintf("zstd %s 运行失败：%s", contextName, err.Error()))
		result.AddFailure(model.Failure{
			Category: model.FailureParse, Stage: "benchmark_run", Target: contextName,
			Count: 1, Message: err.Error(),
		})
	}

	appendZstdMeasurements(&result, runs, workers)
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "zstd"},
		{Key: "version", Label: "zstd 版本", Value: versionLine},
		{Key: "binary_sha256", Label: "zstd SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "method_version", Label: "method version", Value: zstdMethodVersion},
		{Key: "compression_level", Label: "压缩等级", Value: strconv.Itoa(contract.Level)},
		{Key: "threads", Label: "测试 worker", Value: fmt.Sprintf("1 / %d", workers)},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: describeCPUAllowance(allowance)},
		{Key: "duration", Label: "每阶段最短评估时长", Value: fmt.Sprintf("%ds", contract.Seconds)},
		{Key: "corpus", Label: "固定 corpus", Value: contract.CorpusName},
		{Key: "corpus_bytes", Label: "corpus 大小", Value: strconv.FormatInt(contract.CorpusBytes, 10) + " bytes"},
		{Key: "corpus_sha256", Label: "corpus SHA-256", Value: contract.CorpusSHA256},
		{Key: "corpus_source_sha256", Label: "Silesia ZIP SHA-256", Value: contract.CorpusSourceHash},
		{Key: "corpus_construction", Label: "corpus 构造", Value: "dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml（原字节顺序拼接）"},
		{Key: "arguments_1t", Label: "完整参数（1 worker）", Value: strings.Join(runs[0].Args, " ")},
		{Key: "arguments_nt", Label: "完整参数（全 worker）", Value: strings.Join(runs[1].Args, " ")},
	}
	for index, sample := range runs {
		if sample.Output == "" {
			continue
		}
		title := "zstd 1 worker 原始输出"
		if index == 1 {
			title = fmt.Sprintf("zstd 全 worker（%d）原始输出", workers)
		}
		result.TextBlocks = append(result.TextBlocks, model.TextBlock{Title: title, Language: "text", Content: sample.Output})
	}
	result.Tables = append(result.Tables, zstdThroughputTable(runs, workers))
	result.Sources = []model.Source{
		{Name: "Zstandard", URL: "https://github.com/facebook/zstd", Purpose: "官方 zstd CLI 内存 benchmark 模式"},
		{Name: "Silesia Corpus", URL: "https://data-compression.info/Corpora/SilesiaCorpus/", Purpose: "固定的多类型无损压缩测试数据"},
	}
	result.Notes = append(result.Notes,
		fmt.Sprintf("zstd 固定为 v%s、level %d、每阶段至少 %d 秒；1 worker 与 %d worker 是两次独立运行。", contract.Version, contract.Level, contract.Seconds, workers),
		"corpus 由 Silesia ZIP 中 12 个文件按固定顺序逐字节拼接；运行前同时校验长度与 SHA-256，数据不同不会产生成绩。",
		"zstd benchmark 在内存中重复压缩/解压整个输入；结构化值保留 CLI 报告的十进制 MB/s，不换算为 MiB/s。",
		"zstd 的 -T 参数控制压缩 worker；解压阶段本身不做多线程并行，因此解压扩展倍率与每 worker 效率仅用于揭示两轮波动，不代表并行解压能力。",
		"本模块只输出原始吞吐、扩展倍率和每 worker 效率，不进入 CPU 综合分，也不生成自定义综合分。",
	)
	if allowance.Limited() {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"检测到 CPU 配额 %.2f 核（%s），全 worker 轮按 allowance 使用 %d worker。",
			allowance.Quota, allowance.Source, workers,
		))
	}
	result.Evidence = model.NewEvidence(validRuns, 2, "run")
	if validRuns == 0 {
		result.Summary = "zstd 未产出有效吞吐"
	} else if runs[0].CompressMBPS > 0 && runs[1].CompressMBPS > 0 {
		result.Summary = fmt.Sprintf("zstd 压缩 1T %s · %dT %s · 扩展 %.2f×",
			model.FormatRate(runs[0].CompressMBPS, "MB/s"), workers,
			model.FormatRate(runs[1].CompressMBPS, "MB/s"), runs[1].CompressMBPS/runs[0].CompressMBPS)
	} else if runs[0].CompressMBPS > 0 {
		result.Summary = "zstd 压缩 1T " + model.FormatRate(runs[0].CompressMBPS, "MB/s")
	} else {
		result.Summary = fmt.Sprintf("zstd 压缩 %dT %s", workers, model.FormatRate(runs[1].CompressMBPS, "MB/s"))
	}
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
	labels := []string{"1 worker", fmt.Sprintf("%d worker", workers)}
	for index, sample := range runs {
		if sample.CompressMBPS > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "zstd_compress_" + contexts[index] + "_mb_s", Label: "zstd " + labels[index] + " 压缩吞吐",
				Value: sample.CompressMBPS, Unit: "MB/s", Display: model.FormatRate(sample.CompressMBPS, "MB/s"),
				Method: zstdMethodVersion + "-compress-" + contexts[index], HigherIsBetter: model.BoolPtr(true),
			})
		}
		if sample.DecompressMBPS > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "zstd_decompress_" + contexts[index] + "_mb_s", Label: "zstd " + labels[index] + " 解压吞吐",
				Value: sample.DecompressMBPS, Unit: "MB/s", Display: model.FormatRate(sample.DecompressMBPS, "MB/s"),
				Method: zstdMethodVersion + "-decompress-" + contexts[index], HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
	if runs[0].CompressMBPS > 0 && runs[1].CompressMBPS > 0 {
		appendZstdScalingMeasurements(result, "compress", "压缩", runs[1].CompressMBPS/runs[0].CompressMBPS, workers)
	}
	if runs[0].DecompressMBPS > 0 && runs[1].DecompressMBPS > 0 {
		appendZstdScalingMeasurements(result, "decompress", "解压", runs[1].DecompressMBPS/runs[0].DecompressMBPS, workers)
	}
}

func appendZstdScalingMeasurements(result *model.Result, key, label string, scaling float64, workers int) {
	efficiency := scaling / float64(workers) * 100
	result.Measurements = append(result.Measurements,
		model.Measurement{
			Key: "zstd_" + key + "_scaling_ratio", Label: "zstd " + label + "多线程扩展倍率",
			Value: scaling, Unit: "x", Display: fmt.Sprintf("%.2f×", scaling),
			Method: zstdMethodVersion + "-" + key + "-scaling", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: "zstd_" + key + "_per_worker_efficiency_percent", Label: "zstd " + label + "每 worker 效率",
			Value: efficiency, Unit: "%", Display: fmt.Sprintf("%.1f %%", efficiency),
			Method: zstdMethodVersion + "-" + key + "-scaling", HigherIsBetter: model.BoolPtr(true),
		},
	)
}

func zstdThroughputTable(runs []zstdBenchmarkSample, workers int) model.Table {
	table := model.Table{
		Title:                 "zstd 压缩与解压吞吐",
		Columns:               []string{"线程上下文", "压缩吞吐", "解压吞吐", "压缩扩展", "解压扩展", "压缩每 worker 效率", "解压每 worker 效率"},
		NumericColumns:        []int{1, 2, 3, 4, 5, 6},
		NumericHigherIsBetter: []bool{true, true, true, true, true, true},
	}
	for index, sample := range runs {
		contextName := "1 worker"
		if index == 1 {
			contextName = fmt.Sprintf("全 worker（%d）", workers)
		}
		compress, decompress := "—", "—"
		if sample.CompressMBPS > 0 {
			compress = model.FormatRate(sample.CompressMBPS, "MB/s")
		}
		if sample.DecompressMBPS > 0 {
			decompress = model.FormatRate(sample.DecompressMBPS, "MB/s")
		}
		compressScale, decompressScale, compressEfficiency, decompressEfficiency := "—", "—", "—", "—"
		if index == 0 && sample.CompressMBPS > 0 {
			compressScale, compressEfficiency = "1.00 x", "100.0 %"
		}
		if index == 0 && sample.DecompressMBPS > 0 {
			decompressScale, decompressEfficiency = "1.00 x", "100.0 %"
		}
		if index == 1 && len(runs) > 1 && runs[0].CompressMBPS > 0 && sample.CompressMBPS > 0 {
			ratio := sample.CompressMBPS / runs[0].CompressMBPS
			compressScale = fmt.Sprintf("%.2f x", ratio)
			compressEfficiency = fmt.Sprintf("%.1f %%", ratio/float64(workers)*100)
		}
		if index == 1 && len(runs) > 1 && runs[0].DecompressMBPS > 0 && sample.DecompressMBPS > 0 {
			ratio := sample.DecompressMBPS / runs[0].DecompressMBPS
			decompressScale = fmt.Sprintf("%.2f x", ratio)
			decompressEfficiency = fmt.Sprintf("%.1f %%", ratio/float64(workers)*100)
		}
		table.Rows = append(table.Rows, []string{
			contextName, compress, decompress, compressScale, decompressScale, compressEfficiency, decompressEfficiency,
		})
	}
	return table
}
