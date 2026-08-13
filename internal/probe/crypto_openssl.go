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
	openSSLMethodVersion   = "openssl-speed-3.5.7-16k-v1"
	openSSLExpectedVersion = "3.5.7"
	openSSLBlockBytes      = 16384
	openSSLDurationSeconds = 5
	openSSLRunTimeout      = 2 * time.Minute
)

var (
	openSSLVersionPattern = regexp.MustCompile(`(?m)^OpenSSL\s+([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)
	openSSLDTLine         = regexp.MustCompile(`(?m)^\+DT:([^:\r\n]+):([0-9]+):([0-9]+)\s*$`)
	openSSLRLine          = regexp.MustCompile(`(?m)^\+R:([0-9]+):([^:\r\n]+):([0-9]+(?:\.[0-9]+)?)\s*$`)
	openSSLFLine          = regexp.MustCompile(`(?m)^\+F:([0-9]+):([^:\r\n]+):([0-9]+(?:\.[0-9]+)?)\s*$`)
)

type openSSLAlgorithmSpec struct {
	Key        string
	EVPName    string
	OutputName string
	Label      string
	AEAD       bool
}

var openSSLAlgorithmSpecs = []openSSLAlgorithmSpec{
	{Key: "aes_256_gcm", EVPName: "aes-256-gcm", OutputName: "AES-256-GCM", Label: "AES-256-GCM", AEAD: true},
	{Key: "chacha20_poly1305", EVPName: "chacha20-poly1305", OutputName: "ChaCha20-Poly1305", Label: "ChaCha20-Poly1305", AEAD: true},
	{Key: "sha_256", EVPName: "sha256", OutputName: "sha256", Label: "SHA-256"},
}

type openSSLSpeedSample struct {
	Algorithm      string
	Workers        int
	Duration       int
	BlockBytes     int
	ThroughputBPS  float64
	ThroughputMBPS float64
	Args           []string
	Environment    []string
	Output         string
}

type cryptoProbe struct{}

func (cryptoProbe) ID() string         { return "crypto" }
func (cryptoProbe) Title() string      { return "服务器密码学性能" }
func (cryptoProbe) NeedsNetwork() bool { return false }

func (cryptoProbe) Run(ctx context.Context, env Environment) model.Result {
	path, err := exec.LookPath("openssl")
	if err != nil {
		return missingOpenSSLResult("未找到固定版本 OpenSSL，密码学基准未运行")
	}
	return runOpenSSLSpeed(ctx, env, path, openSSLAlgorithmSpecs)
}

func cryptoMethodology() model.Methodology {
	return model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "OpenSSL speed",
		Profile:         "OpenSSL 3.5.7 · AES-256-GCM/ChaCha20-Poly1305/SHA-256 · 16 KiB · 5s · 1W/NW",
		ComparisonScope: "相同 OpenSSL 版本、EVP algorithm、block size、duration、worker 数、参数、binary SHA-256 与 method version",
	}
}

func missingOpenSSLResult(summary string) model.Result {
	start := time.Now()
	result := model.NewResult("crypto", "服务器密码学性能")
	result.Description = "独立的服务器密码学吞吐：AES-256-GCM、ChaCha20-Poly1305 与 SHA-256"
	result.Methodology = cryptoMethodology()
	result.Status = model.StatusWarning
	result.Summary = summary
	result.AddFailure(model.Failure{
		Category: model.FailureToolMissing, Stage: "tool_lookup", Target: "openssl",
		Count: 1, Message: summary,
	})
	result.Evidence = model.NewEvidence(0, len(openSSLAlgorithmSpecs)*len(distinctBenchmarkThreadCounts(detectCPUAllowance().Threads)), "run")
	result.Notes = append(result.Notes,
		"请使用 run.sh；它会提供版本与哈希经过 ecs-tools manifest 校验的固定 OpenSSL binary。crypto 不并入 CPU 综合结果。",
	)
	result.Finish(start)
	return result
}

func runOpenSSLSpeed(ctx context.Context, env Environment, path string, specs []openSSLAlgorithmSpec) model.Result {
	return runOpenSSLSpeedWithAllowance(ctx, env, path, specs, detectCPUAllowance())
}

func runOpenSSLSpeedWithAllowance(ctx context.Context, env Environment, path string, specs []openSSLAlgorithmSpec, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := model.NewResult("crypto", "服务器密码学性能")
	result.Description = "独立的服务器密码学吞吐：AES-256-GCM、ChaCha20-Poly1305 与 SHA-256"
	result.Methodology = cryptoMethodology()
	threadCounts := distinctBenchmarkThreadCounts(allowance.Threads)

	versionOutput, version, versionErr := queryOpenSSLVersion(ctx, path)
	if versionErr != nil || version != openSSLExpectedVersion {
		result.Status = model.StatusWarning
		result.Summary = fmt.Sprintf("OpenSSL 版本不符合固定口径：检测到 %s，要求 %s", fallback(version, "unknown"), openSSLExpectedVersion)
		result.AddFailure(model.Failure{
			Category: model.FailureUnsupported, Stage: "version_check", Target: path,
			Count: 1, Message: result.Summary,
		})
		result.Fields = []model.Field{
			{Key: "engine", Label: "标准工具", Value: "OpenSSL speed"},
			{Key: "version", Label: "OpenSSL 版本", Value: fallback(versionOutput, "unknown")},
			{Key: "required_version", Label: "固定版本", Value: openSSLExpectedVersion},
			{Key: "binary_sha256", Label: "OpenSSL SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		}
		result.Evidence = model.NewEvidence(0, len(specs)*len(threadCounts), "run")
		result.Notes = append(result.Notes, "请使用 run.sh 提供固定 OpenSSL 3.5.7 static binary；不接受系统中其他版本生成可比较成绩。")
		result.Finish(start)
		return result
	}

	workers := allowance.Threads
	singleCore := len(threadCounts) == 1
	runs := make(map[string][]openSSLSpeedSample, len(specs))
	validRuns := 0
	for _, spec := range specs {
		for _, workerCount := range threadCounts {
			sample, runErr := executeOpenSSLSpeed(ctx, path, spec, workerCount, openSSLDurationSeconds, openSSLBlockBytes)
			runs[spec.Key] = append(runs[spec.Key], sample)
			if runErr == nil {
				validRuns++
				continue
			}
			result.Status = model.StatusWarning
			contextName := fmt.Sprintf("%s %d worker", spec.Label, workerCount)
			result.Notes = append(result.Notes, fmt.Sprintf("OpenSSL speed %s 运行失败：%s", contextName, runErr.Error()))
			result.AddFailure(model.Failure{
				Category: benchmarkFailureCategory(ctx, runErr), Stage: "benchmark_run", Target: contextName,
				Count: 1, Message: runErr.Error(),
			})
		}
		if singleCore && len(runs[spec.Key]) == 1 {
			clone := runs[spec.Key][0]
			runs[spec.Key] = append(runs[spec.Key], clone)
		}
	}

	appendOpenSSLMeasurements(&result, specs, runs, workers)
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "OpenSSL speed"},
		{Key: "version", Label: "OpenSSL 版本", Value: versionOutput},
		{Key: "binary_sha256", Label: "OpenSSL SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "method_version", Label: "method version", Value: openSSLMethodVersion},
		{Key: "algorithms", Label: "固定算法", Value: "AES-256-GCM / ChaCha20-Poly1305 / SHA-256"},
		{Key: "block_size", Label: "固定 block size", Value: strconv.Itoa(openSSLBlockBytes) + " bytes"},
		{Key: "duration", Label: "每算法每轮时长", Value: strconv.Itoa(openSSLDurationSeconds) + "s"},
		{Key: "workers", Label: "测试 worker", Value: benchmarkThreadField(workers)},
		{Key: "cpu_allowance", Label: "可用 CPU", Value: describeCPUAllowance(allowance)},
		{Key: "timing", Label: "计时方式", Value: "-elapsed (wall clock)"},
		{Key: "machine_output", Label: "输出模式", Value: "-mr"},
		{Key: "configuration", Label: "配置隔离", Value: "OPENSSL_CONF=/dev/null；空 modules/engines 目录；CPU capability 自动探测"},
	}
	for _, spec := range specs {
		for index, sample := range runs[spec.Key] {
			contextKey := "1w"
			contextLabel := "1 worker"
			if index == 1 {
				contextKey = "nw"
				contextLabel = fmt.Sprintf("全 worker（%d）", workers)
			}
			result.Fields = append(result.Fields, model.Field{
				Key:   "arguments_" + spec.Key + "_" + contextKey,
				Label: "完整参数（" + spec.Label + " " + contextLabel + "）",
				Value: strings.Join(sample.Args, " "),
			})
			if sample.Output != "" && !(singleCore && index > 0) {
				result.TextBlocks = append(result.TextBlocks, model.TextBlock{
					Title:    "OpenSSL speed " + spec.Label + " " + contextLabel + " 原始输出",
					Language: "text", Content: sample.Output,
				})
			}
		}
	}
	result.Tables = append(result.Tables, openSSLResultsTable(specs, runs, workers))
	result.Sources = []model.Source{
		{Name: "OpenSSL speed", URL: "https://docs.openssl.org/3.5/man1/openssl-speed/", Purpose: "官方 speed 参数与 machine-readable 输出"},
		{Name: "OpenSSL 3.5.7", URL: "https://github.com/openssl/openssl/tree/openssl-3.5.7", Purpose: "固定源码版本"},
	}
	if singleCore {
		result.Notes = append(result.Notes, "固定 OpenSSL 3.5.7、16 KiB block、每算法每轮 5 秒；单核 allowance 下 1W/NW 逻辑指标复用每个算法的同一次 -multi 1 实测，未执行第二次相同命令，扩展倍率不适用。")
	} else {
		result.Notes = append(result.Notes, "固定 OpenSSL 3.5.7、16 KiB block、每算法每轮 5 秒，并用 -multi 显式运行 1 worker 与当前 CPU allowance 的全 worker。")
	}
	result.Notes = append(result.Notes,
		"AES-256-GCM 与 ChaCha20-Poly1305 使用 EVP AEAD 路径；SHA-256 使用 EVP digest 路径；-elapsed 使用 wall-clock 计时。",
		"结构化吞吐由 OpenSSL -mr 最终 +F 行的 bytes/s 除以 1,000,000 得到十进制 MB/s；原始 bytes/s 与完整输出同时保留。",
		"CPU capability 覆盖变量被清除，让固定 binary 自动探测服务器可用的硬件加速；因此结果体现实际密码学加速能力。",
		"crypto 是独立服务器密码学模块，不并入 CPU 综合结果，也不生成自定义综合分。",
	)
	if allowance.Limited() && !singleCore {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"检测到 CPU 配额 %.2f 核（%s），全 worker 轮按 allowance 使用 %d worker。",
			allowance.Quota, allowance.Source, workers,
		))
	}
	expectedRuns := len(specs) * len(threadCounts)
	result.Evidence = model.NewEvidence(validRuns, expectedRuns, "run")
	if validRuns < expectedRuns {
		result.Status = model.StatusWarning
	}
	result.Summary = openSSLSummary(specs, runs, workers)
	result.Finish(start)
	return result
}

func queryOpenSSLVersion(ctx context.Context, path string) (string, string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, path, "version").CombinedOutput()
	text := sanitizeCommandOutput(output)
	if versionCtx.Err() != nil {
		return text, "", versionCtx.Err()
	}
	if err != nil {
		return text, "", err
	}
	matches := openSSLVersionPattern.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 || len(matches[0]) != 2 {
		return text, "", fmt.Errorf("无法解析唯一 OpenSSL version")
	}
	return text, matches[0][1], nil
}

func executeOpenSSLSpeed(ctx context.Context, path string, spec openSSLAlgorithmSpec, workers, seconds, blockBytes int) (openSSLSpeedSample, error) {
	sample := openSSLSpeedSample{Algorithm: spec.Key, Workers: workers, Duration: seconds, BlockBytes: blockBytes}
	if workers < 1 || seconds < 1 || blockBytes < 1 {
		return sample, fmt.Errorf("OpenSSL speed workers/duration/block size 必须为正数")
	}
	args := []string{
		"speed", "-elapsed", "-seconds", strconv.Itoa(seconds),
		"-bytes", strconv.Itoa(blockBytes), "-mr", "-multi", strconv.Itoa(workers),
		"-evp", spec.EVPName,
	}
	if spec.AEAD {
		args = append(args, "-aead")
	}
	sample.Args = append([]string(nil), args...)
	workDirectory, err := os.MkdirTemp("", "ecs-openssl-")
	if err != nil {
		return sample, fmt.Errorf("创建 OpenSSL speed 私有工作目录: %w", err)
	}
	defer os.RemoveAll(workDirectory)
	modulesDirectory := filepath.Join(workDirectory, "modules")
	enginesDirectory := filepath.Join(workDirectory, "engines")
	if err := os.Mkdir(modulesDirectory, 0o700); err != nil {
		return sample, err
	}
	if err := os.Mkdir(enginesDirectory, 0o700); err != nil {
		return sample, err
	}
	environment := []string{
		"OPENSSL_CONF=/dev/null",
		"OPENSSL_MODULES=" + modulesDirectory,
		"OPENSSL_ENGINES=" + enginesDirectory,
	}
	sample.Environment = append([]string(nil), environment...)
	overrides := make(map[string]string, len(environment))
	for _, item := range environment {
		key, value, _ := strings.Cut(item, "=")
		overrides[key] = value
	}
	runCtx, cancel := context.WithTimeout(ctx, openSSLRunTimeout)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = benchmarkEnvironment(overrides)
	command.Dir = workDirectory
	output, err := command.CombinedOutput()
	sample.Output = normalizeCarriageReturnOutput(output)
	if runCtx.Err() != nil {
		return sample, runCtx.Err()
	}
	if err != nil {
		return sample, fmt.Errorf("OpenSSL speed %s 执行失败: %w: %s", spec.Label, err, tailText(sample.Output, 600))
	}
	parsed, err := parseOpenSSLSpeedOutput(sample.Output, spec, workers, seconds, blockBytes)
	parsed.Args = sample.Args
	parsed.Environment = sample.Environment
	parsed.Output = sample.Output
	if err != nil {
		return parsed, err
	}
	return parsed, nil
}

func parseOpenSSLSpeedOutput(output string, spec openSSLAlgorithmSpec, workers, seconds, blockBytes int) (openSSLSpeedSample, error) {
	sample := openSSLSpeedSample{Algorithm: spec.Key, Workers: workers, Duration: seconds, BlockBytes: blockBytes}
	dtLines := openSSLDTLine.FindAllStringSubmatch(output, -1)
	if len(dtLines) != workers {
		return sample, fmt.Errorf("OpenSSL speed %s +DT worker 记录数为 %d，期望 %d", spec.Label, len(dtLines), workers)
	}
	for _, line := range dtLines {
		duration, durationErr := strconv.Atoi(line[2])
		block, blockErr := strconv.Atoi(line[3])
		if !strings.EqualFold(line[1], spec.OutputName) || durationErr != nil || duration != seconds || blockErr != nil || block != blockBytes {
			return sample, fmt.Errorf("OpenSSL speed %s +DT 参数不符合固定口径", spec.Label)
		}
	}
	rLines := openSSLRLine.FindAllStringSubmatch(output, -1)
	if len(rLines) != workers {
		return sample, fmt.Errorf("OpenSSL speed %s +R worker 记录数为 %d，期望 %d", spec.Label, len(rLines), workers)
	}
	for _, line := range rLines {
		count, countErr := strconv.ParseUint(line[1], 10, 64)
		actualSeconds, secondsErr := strconv.ParseFloat(line[3], 64)
		if countErr != nil || count == 0 || !strings.EqualFold(line[2], spec.OutputName) || secondsErr != nil ||
			actualSeconds < float64(seconds)*0.9 || actualSeconds > float64(seconds)+1 || math.IsNaN(actualSeconds) || math.IsInf(actualSeconds, 0) {
			return sample, fmt.Errorf("OpenSSL speed %s +R 计数或时长无效", spec.Label)
		}
	}
	fLines := openSSLFLine.FindAllStringSubmatch(output, -1)
	if len(fLines) != 1 || len(fLines[0]) != 4 {
		return sample, fmt.Errorf("OpenSSL speed %s 缺少唯一聚合 +F 结果", spec.Label)
	}
	if !strings.EqualFold(fLines[0][2], spec.OutputName) {
		return sample, fmt.Errorf("OpenSSL speed +F algorithm 为 %s，期望 %s", fLines[0][2], spec.OutputName)
	}
	throughput, err := strconv.ParseFloat(fLines[0][3], 64)
	if err != nil || throughput <= 0 || math.IsNaN(throughput) || math.IsInf(throughput, 0) {
		return sample, fmt.Errorf("OpenSSL speed %s +F throughput 无效", spec.Label)
	}
	sample.ThroughputBPS = throughput
	sample.ThroughputMBPS = throughput / 1_000_000
	return sample, nil
}

func appendOpenSSLMeasurements(result *model.Result, specs []openSSLAlgorithmSpec, runs map[string][]openSSLSpeedSample, workers int) {
	for _, spec := range specs {
		samples := runs[spec.Key]
		for index, sample := range samples {
			if sample.ThroughputMBPS <= 0 {
				continue
			}
			contextKey, contextLabel := "1w", "1 worker"
			if index == 1 {
				contextKey, contextLabel = "nw", fmt.Sprintf("%d worker", workers)
			}
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "openssl_" + spec.Key + "_" + contextKey + "_mb_s",
				Label: spec.Label + " " + contextLabel + " 吞吐",
				Value: sample.ThroughputMBPS, Unit: "MB/s", Display: model.FormatRate(sample.ThroughputMBPS, "MB/s"),
				Method:         openSSLMethodVersion + "-" + spec.Key + "-" + contextKey,
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		if workers > 1 && len(samples) >= 2 && samples[0].ThroughputBPS > 0 && samples[1].ThroughputBPS > 0 {
			scaling := samples[1].ThroughputBPS / samples[0].ThroughputBPS
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "openssl_" + spec.Key + "_scaling_ratio",
				Label: spec.Label + " 多 worker 扩展倍率",
				Value: scaling, Unit: "x", Display: fmt.Sprintf("%.2f×", scaling),
				Method:         openSSLMethodVersion + "-" + spec.Key + "-scaling",
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
}

func openSSLResultsTable(specs []openSSLAlgorithmSpec, runs map[string][]openSSLSpeedSample, workers int) model.Table {
	table := model.Table{
		Title:          "OpenSSL speed 密码学吞吐",
		Columns:        []string{"算法", "worker 上下文", "block", "原始 bytes/s", "吞吐", "扩展倍率"},
		NumericColumns: []int{2, 3, 4, 5}, NumericHigherIsBetter: []bool{false, true, true, true},
	}
	for _, spec := range specs {
		samples := runs[spec.Key]
		for index, sample := range samples {
			contextName := "1 worker"
			if index == 1 {
				contextName = fmt.Sprintf("全 worker（%d）", workers)
			}
			raw, throughput, scaling := "—", "—", "—"
			if sample.ThroughputBPS > 0 {
				raw = strconv.FormatFloat(sample.ThroughputBPS, 'f', 2, 64)
				throughput = model.FormatRate(sample.ThroughputMBPS, "MB/s")
				if workers <= 1 {
					scaling = "不适用"
				} else if index == 0 {
					scaling = "1.00 x"
				} else if len(samples) >= 2 && samples[0].ThroughputBPS > 0 {
					scaling = fmt.Sprintf("%.2f x", sample.ThroughputBPS/samples[0].ThroughputBPS)
				}
			}
			table.Rows = append(table.Rows, []string{spec.Label, contextName, strconv.Itoa(openSSLBlockBytes) + " bytes", raw, throughput, scaling})
		}
	}
	return table
}

func openSSLSummary(specs []openSSLAlgorithmSpec, runs map[string][]openSSLSpeedSample, workers int) string {
	parts := make([]string, 0, len(specs))
	for _, spec := range specs {
		samples := runs[spec.Key]
		if workers <= 1 && len(samples) >= 1 && samples[0].ThroughputBPS > 0 {
			parts = append(parts, fmt.Sprintf("%s 1W/NW（同一次实测）%s · 扩展不适用",
				spec.Label, model.FormatRate(samples[0].ThroughputMBPS, "MB/s")))
		} else if len(samples) >= 2 && samples[0].ThroughputBPS > 0 && samples[1].ThroughputBPS > 0 {
			parts = append(parts, fmt.Sprintf("%s 1W %s · %dW %s · %.2f×",
				spec.Label, model.FormatRate(samples[0].ThroughputMBPS, "MB/s"), workers,
				model.FormatRate(samples[1].ThroughputMBPS, "MB/s"), samples[1].ThroughputBPS/samples[0].ThroughputBPS))
		}
	}
	if len(parts) == 0 {
		return "OpenSSL speed 未产出有效密码学吞吐"
	}
	return strings.Join(parts, " · ")
}
