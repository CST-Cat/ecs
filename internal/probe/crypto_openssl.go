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

func (cryptoProbe) ID() string { return "crypto" }

func newCryptoResult() model.Result {
	result := model.NewResult("crypto", "module.crypto.title")
	result.Description = "probe.crypto.description"
	result.Methodology = cryptoMethodology()
	result.Methodology.Parameters = newComparisonParameters()
	return result
}

func (cryptoProbe) Run(ctx context.Context, env Environment) model.Result {
	path, err := exec.LookPath("openssl")
	if err != nil {
		return missingOpenSSLResult(err)
	}
	return runOpenSSLSpeed(ctx, env, path, openSSLAlgorithmSpecs)
}

func cryptoMethodology() model.Methodology {
	return model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "methodology.standard-benchmark",
		Engine:          "OpenSSL speed",
		Profile:         "probe.crypto.profile",
		ComparisonScope: "probe.crypto.comparison_scope",
	}
}

func missingOpenSSLResult(err error) model.Result {
	start := time.Now()
	allowance := detectCPUAllowance()
	result := newCryptoResult()
	result.Status = model.StatusWarning
	message := ""
	if err != nil {
		message = err.Error()
	}
	result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: "openssl", Count: 1, Message: message})
	result.Evidence = model.NewEvidence(0, len(openSSLAlgorithmSpecs)*len(distinctBenchmarkThreadCounts(allowance.Threads)), "run")
	result.Notes = cryptoNotes(result, allowance)
	result.SummaryMessages = []model.Message{model.NewMessage("probe.crypto.summary.none")}
	result.Finish(start)
	return result
}

func runOpenSSLSpeed(ctx context.Context, env Environment, path string, specs []openSSLAlgorithmSpec) model.Result {
	return runOpenSSLSpeedWithAllowance(ctx, env, path, specs, detectCPUAllowance())
}

func runOpenSSLSpeedWithAllowance(ctx context.Context, env Environment, path string, specs []openSSLAlgorithmSpec, allowance cpuAllowance) model.Result {
	start := time.Now()
	result := newCryptoResult()
	threadCounts := distinctBenchmarkThreadCounts(allowance.Threads)

	versionOutput, version, versionErr := queryOpenSSLVersion(ctx, path)
	if versionErr != nil || version != openSSLExpectedVersion {
		result.Status = model.StatusWarning
		message := versionOutput
		if versionErr != nil {
			message = versionErr.Error()
		}
		result.AddFailure(model.Failure{
			Category: model.FailureUnsupported, Stage: "version_check", Target: path,
			Count: 1, Message: message,
		})
		result.Fields = []model.Field{
			{Key: "engine", Label: "probe.crypto.field.engine", Value: model.RawValue("OpenSSL speed")},
			{Key: "version", Label: "probe.crypto.field.version", Value: model.RawValue(fallback(versionOutput, "unknown"))},
			{Key: "required_version", Label: "probe.crypto.field.required_version", Value: model.RawValue(openSSLExpectedVersion)},
			{Key: "binary_sha256", Label: "probe.crypto.field.binary_sha256", Value: model.RawValue(fallback(binarySHA256(path), "unavailable"))},
		}
		addComparisonParameter(result.Methodology.Parameters, "tool_version", fallback(versionOutput, "unknown"))
		addComparisonParameter(result.Methodology.Parameters, "tool_sha256", fallback(binarySHA256(path), "unavailable"))
		result.Evidence = model.NewEvidence(0, len(specs)*len(threadCounts), "run")
		result.Notes = cryptoNotes(result, allowance)
		result.SummaryMessages = []model.Message{model.NewMessage("probe.crypto.summary.version_mismatch", fallback(versionOutput, "unknown"), openSSLExpectedVersion)}
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
		{Key: "engine", Label: "probe.crypto.field.engine", Value: model.RawValue("OpenSSL speed")},
		{Key: "version", Label: "probe.crypto.field.version", Value: model.RawValue(versionOutput)},
		{Key: "binary_sha256", Label: "probe.crypto.field.binary_sha256", Value: model.RawValue(fallback(binarySHA256(path), "unavailable"))},
		{Key: "method_version", Label: "probe.crypto.field.method_version", Value: model.RawValue(openSSLMethodVersion)},
		{Key: "algorithms", Label: "probe.crypto.field.algorithms", Value: model.RawValue("AES-256-GCM / ChaCha20-Poly1305 / SHA-256")},
		{Key: "block_size", Label: "probe.crypto.field.block_size", Value: model.RawValue(strconv.Itoa(openSSLBlockBytes) + " bytes")},
		{Key: "duration", Label: "probe.crypto.field.duration", Value: model.RawValue(strconv.Itoa(openSSLDurationSeconds) + "s")},
		{Key: "workers", Label: "probe.crypto.field.workers", Value: model.RawValue(benchmarkThreadField(workers))},
		{Key: "cpu_allowance", Label: "probe.crypto.field.cpu_allowance", Value: model.RawValue(cpuAllowanceMachineValue(allowance))},
		{Key: "timing", Label: "probe.crypto.field.timing", Value: model.RawValue("-elapsed (wall clock)")},
		{Key: "machine_output", Label: "probe.crypto.field.machine_output", Value: model.RawValue("-mr")},
		{Key: "configuration", Label: "probe.crypto.field.configuration", Value: model.RawValue("OPENSSL_CONF=/dev/null；空 modules/engines 目录；CPU capability 自动探测")},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", versionOutput)
	addComparisonParameter(result.Methodology.Parameters, "tool_sha256", fallback(binarySHA256(path), "unavailable"))
	addComparisonParameter(result.Methodology.Parameters, "method_version", openSSLMethodVersion)
	addComparisonParameter(result.Methodology.Parameters, "algorithms", "AES-256-GCM / ChaCha20-Poly1305 / SHA-256")
	addComparisonParameter(result.Methodology.Parameters, "block_size", strconv.Itoa(openSSLBlockBytes)+" bytes")
	addComparisonParameter(result.Methodology.Parameters, "duration", strconv.Itoa(openSSLDurationSeconds)+"s")
	addComparisonParameter(result.Methodology.Parameters, "workers", benchmarkThreadField(workers))
	addComparisonParameter(result.Methodology.Parameters, "timing", "-elapsed (wall clock)")
	addComparisonParameter(result.Methodology.Parameters, "machine_output", "-mr")
	for _, spec := range specs {
		for index, sample := range runs[spec.Key] {
			contextKey := "1w"
			if index == 1 {
				contextKey = "nw"
			}
			result.Fields = append(result.Fields, model.Field{
				Key:   "arguments_" + spec.Key + "_" + contextKey,
				Label: "probe.crypto.field.arguments",
				Value: model.RawValue(strings.Join(sample.Args, " ")),
			})
			if len(sample.Args) > 0 {
				addComparisonParameter(result.Methodology.Parameters, "arguments_"+spec.Key+"_"+contextKey+"_sha256", comparisonParameterHash(strings.Join(sample.Args, " ")))
			}
			if sample.Output != "" && !(singleCore && index > 0) {
				result.TextBlocks = append(result.TextBlocks, model.TextBlock{
					Title:    "probe.crypto.raw_output",
					Language: "text", Content: sample.Output,
				})
			}
		}
	}
	result.Tables = append(result.Tables, openSSLResultsTable(specs, runs, workers))
	result.Sources = []model.Source{
		{Name: "OpenSSL speed", URL: "https://docs.openssl.org/3.5/man1/openssl-speed/", Purpose: "probe.crypto.source.openssl"},
		{Name: "OpenSSL 3.5.7", URL: "https://github.com/openssl/openssl/tree/openssl-3.5.7", Purpose: "probe.crypto.source.version"},
	}
	result.Notes = cryptoNotes(result, allowance)
	expectedRuns := len(specs) * len(threadCounts)
	result.Evidence = model.NewEvidence(validRuns, expectedRuns, "run")
	if validRuns < expectedRuns {
		result.Status = model.StatusWarning
	}
	if summary := cryptoSummary(runs, workers); summary != "" {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.crypto.summary.values", summary)}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.crypto.summary.none")}
	}
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
			contextKey := "1w"
			if index == 1 {
				contextKey = "nw"
			}
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "openssl_" + spec.Key + "_" + contextKey + "_mb_s",
				Label: openSSLMeasurementLabel("openssl_" + spec.Key + "_" + contextKey + "_mb_s"),
				Value: sample.ThroughputMBPS, Unit: "MB/s", Display: model.RawValue(model.FormatRate(sample.ThroughputMBPS, "MB/s")),
				Method:         openSSLMethodVersion + "-" + spec.Key + "-" + contextKey,
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		if workers > 1 && len(samples) >= 2 && samples[0].ThroughputBPS > 0 && samples[1].ThroughputBPS > 0 {
			scaling := samples[1].ThroughputBPS / samples[0].ThroughputBPS
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "openssl_" + spec.Key + "_scaling_ratio",
				Label: openSSLMeasurementLabel("openssl_" + spec.Key + "_scaling_ratio"),
				Value: scaling, Unit: "x", Display: model.RawValue(fmt.Sprintf("%.2f×", scaling)),
				Method:         openSSLMethodVersion + "-" + spec.Key + "-scaling",
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
}

func openSSLResultsTable(specs []openSSLAlgorithmSpec, runs map[string][]openSSLSpeedSample, workers int) model.Table {
	table := model.Table{
		Key:   "benchmark.openssl.results",
		Title: "probe.crypto.table.title",
		Columns: []model.TableColumn{
			{Key: "algorithm", Label: "probe.crypto.column.algorithm"},
			{Key: "worker_context", Label: "probe.crypto.column.worker_context"},
			{Key: "block_bytes", Label: "probe.crypto.column.block", Numeric: true, HigherIsBetter: false},
			{Key: "raw_bytes_per_second", Label: "probe.crypto.column.raw_bytes_per_second", Numeric: true, HigherIsBetter: true},
			{Key: "throughput_mbps", Label: "probe.crypto.column.throughput", Numeric: true, HigherIsBetter: true},
			{Key: "scaling_ratio", Label: "probe.crypto.column.scaling", Numeric: true, HigherIsBetter: true},
		},
	}
	for _, spec := range specs {
		samples := runs[spec.Key]
		for index, sample := range samples {
			contextName := "1W"
			if index == 1 {
				if workers <= 1 {
					contextName = "NW(1W-reused)"
				} else {
					contextName = fmt.Sprintf("NW(%dW)", workers)
				}
			}
			raw, throughput, scaling := "—", "—", "—"
			if workers <= 1 {
				scaling = "na"
			}
			if sample.ThroughputBPS > 0 {
				raw = strconv.FormatFloat(sample.ThroughputBPS, 'f', 2, 64)
				throughput = model.FormatRate(sample.ThroughputMBPS, "MB/s")
				if workers > 1 && index == 0 {
					scaling = "1.00 x"
				} else if workers > 1 && len(samples) >= 2 && samples[0].ThroughputBPS > 0 {
					scaling = fmt.Sprintf("%.2f x", sample.ThroughputBPS/samples[0].ThroughputBPS)
				}
			}
			table.Rows = append(table.Rows, []model.Value{
				model.RawValue(spec.Label), model.RawValue(contextName), model.RawValue(strconv.Itoa(openSSLBlockBytes) + " bytes"),
				model.RawValue(raw), model.RawValue(throughput), model.RawValue(scaling),
			})
		}
	}
	return table
}

func openSSLMeasurementLabel(key string) string {
	const prefix = "openssl_"
	key = strings.TrimPrefix(key, prefix)
	for _, algorithm := range []string{"aes_256_gcm", "chacha20_poly1305", "sha_256"} {
		if strings.HasPrefix(key, algorithm+"_") {
			suffix := strings.TrimPrefix(key, algorithm+"_")
			switch suffix {
			case "1w_mb_s":
				return "probe.crypto.metric." + algorithm + ".1w"
			case "nw_mb_s":
				return "probe.crypto.metric." + algorithm + ".nw"
			case "scaling_ratio":
				return "probe.crypto.metric." + algorithm + ".scaling"
			}
		}
	}
	return "probe.crypto.metric.unknown"
}

func cryptoNotes(result model.Result, allowance cpuAllowance) []string {
	notes := []string{
		"probe.crypto.note.contract",
		"probe.crypto.note.algorithms",
		"probe.crypto.note.output",
		"probe.crypto.note.hardware_acceleration",
		"probe.crypto.note.no_composite_score",
	}
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.crypto.note.single_core")
	} else {
		notes = append(notes, "probe.crypto.note.separate_runs")
	}
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.crypto.note.quota_limited")
	}
	for _, failure := range result.Failures {
		switch failure.Stage {
		case "tool_lookup":
			notes = append(notes, "probe.crypto.note.tool_missing")
		case "version_check":
			notes = append(notes, "probe.crypto.note.version_mismatch")
		case "benchmark_run":
			notes = append(notes, "probe.crypto.note.run_failure")
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

func firstFailureAt(result *model.Result, stage string) *model.Failure {
	for index := range result.Failures {
		if result.Failures[index].Stage == stage {
			return &result.Failures[index]
		}
	}
	return nil
}

func cryptoSummary(runs map[string][]openSSLSpeedSample, workers int) string {
	parts := make([]string, 0, 3)
	for _, algorithm := range []string{"aes_256_gcm", "chacha20_poly1305", "sha_256"} {
		samples := runs[algorithm]
		if len(samples) > 0 && samples[0].ThroughputMBPS > 0 {
			parts = append(parts, algorithm+" 1W="+model.FormatRate(samples[0].ThroughputMBPS, "MB/s"))
		}
		if workers > 1 {
			if len(samples) > 1 && samples[1].ThroughputMBPS > 0 {
				parts = append(parts, fmt.Sprintf("%s NW(%dW)=%s", algorithm, workers, model.FormatRate(samples[1].ThroughputMBPS, "MB/s")))
			}
			if len(samples) > 1 && samples[0].ThroughputBPS > 0 && samples[1].ThroughputBPS > 0 {
				parts = append(parts, fmt.Sprintf("%s scaling=%.2f×", algorithm, samples[1].ThroughputBPS/samples[0].ThroughputBPS))
			}
		}
	}
	return strings.Join(parts, ";")
}
