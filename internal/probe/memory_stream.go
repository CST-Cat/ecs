package probe

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"slices"
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
// memory 探针统一使用这一份判断，避免重复维护标记表。
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

func runStreamMemoryWithAllowance(ctx context.Context, env Environment, path string, allowance cpuAllowance) model.Result {
	result := newMemoryResult()
	result.Methodology.Parameters = newComparisonParameters()

	workers := allowance.Threads
	threadCounts := distinctBenchmarkThreadCounts(workers)
	singleCore := len(threadCounts) == 1
	if singleCore {
		result.Description = "probe.memory.description.single_core"
		result.Methodology.Profile = "probe.memory.stream.profile.single_core"
	}
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
			stage := "stream_1t"
			target := "1T"
			if contextName == "nt" {
				stage = "stream_nt"
				target = "NT"
			}
			result.AddFailure(model.Failure{
				Category: benchmarkFailureCategory(ctx, err),
				Stage:    stage,
				Target:   target,
				Count:    1,
				Message:  err.Error(),
			})
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
			Display:        model.RawValue(model.FormatRate(sample.RateMiBS, "MiB/s")),
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
				Label:          streamScalingMeasurementLabel(kernel),
				Value:          ratio,
				Unit:           "x",
				Display:        model.RawValue(fmt.Sprintf("%.2f×", ratio)),
				Method:         "stream-official-" + strings.ToLower(kernel) + "-scaling-v1",
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}

	units := make([]string, 0, 2)
	for _, run := range runs {
		if run.Sample.Unit == "" || slices.Contains(units, run.Sample.Unit) {
			continue
		}
		units = append(units, run.Sample.Unit)
	}
	version := streamVersion(runs)
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.memory.stream.field.engine", Value: model.RawValue("STREAM")},
		{Key: "version", Label: "probe.memory.stream.field.version", Value: model.RawValue(version)},
		{Key: "threads", Label: "probe.memory.stream.field.threads", Value: model.RawValue(benchmarkThreadField(workers))},
		{Key: "cpu_allowance", Label: "probe.memory.stream.field.cpu_allowance", Value: model.RawValue(cpuAllowanceMachineValue(allowance))},
		{Key: "kernel_order", Label: "probe.memory.stream.field.kernel_order", Value: model.RawValue("Copy / Scale / Add / Triad")},
		{Key: "thread_control", Label: "probe.memory.stream.field.thread_control", Value: model.RawValue(streamThreadControlValue(workers))},
		{Key: "rate_unit", Label: "probe.memory.stream.field.rate_unit", Value: model.RawValue("MiB/s")},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", version)
	addComparisonParameter(result.Methodology.Parameters, "threads", benchmarkThreadField(workers))
	addComparisonParameter(result.Methodology.Parameters, "kernel_order", "Copy / Scale / Add / Triad")
	if len(units) > 0 {
		result.Fields = append(result.Fields, model.Field{Key: "source_rate_units", Label: "probe.memory.stream.field.source_rate_units", Value: model.RawValue(strings.Join(units, " / "))})
	}
	for _, run := range runs {
		if run.Output == "" || run.Reused {
			continue
		}
		result.TextBlocks = append(result.TextBlocks, model.TextBlock{
			Title: streamRawBlockTitle(run), Language: "text", Content: run.Output,
		})
	}
	result.Sources = []model.Source{
		{Name: "STREAM", URL: "https://www.cs.virginia.edu/stream/", Purpose: "probe.memory.stream.source.purpose"},
	}

	result.Tables = append(result.Tables, streamMemoryTable(runs), streamStabilityTable(runs))
	result.Evidence = model.NewEvidence(validRuns, len(threadCounts), "run")
	result.Notes = streamNotes(result, allowance)
	summary := streamSummaryTokens(result, workers)
	if len(summary) == 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.memory.stream.summary.none")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.memory.stream.summary.values", strings.Join(summary, " · "))}
	}
	return result
}

func streamThreadControlValue(workers int) string {
	if workers <= 1 {
		return "OMP_NUM_THREADS=1;contexts=1T,NT;measurement=reused"
	}
	return fmt.Sprintf("OMP_NUM_THREADS=1;OMP_NUM_THREADS=%d;measurement=separate", workers)
}

func streamStabilityTable(runs []streamMemoryRun) model.Table {
	table := model.Table{
		Key:   "memory.stream.stability",
		Title: "probe.memory.stream.table.stability",
		Columns: []model.TableColumn{
			{Key: "kernel_context", Label: "probe.memory.stream.column.kernel_context"},
			{Key: "average_seconds", Label: "probe.memory.stream.column.average_seconds", Numeric: true},
			{Key: "minimum_seconds", Label: "probe.memory.stream.column.minimum_seconds", Numeric: true},
			{Key: "maximum_seconds", Label: "probe.memory.stream.column.maximum_seconds", Numeric: true},
			{Key: "spread_percent", Label: "probe.memory.stream.column.spread_percent", Numeric: true},
		},
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
			table.Rows = append(table.Rows, []model.Value{
				model.RawValue(kernel + " / " + streamTableContextLabel(run)), model.RawValue(avg), model.RawValue(minValue), model.RawValue(maxValue), model.RawValue(spread),
			})
		}
	}
	return table
}

func streamMemoryTable(runs []streamMemoryRun) model.Table {
	table := model.Table{
		Key:   "memory.stream.bandwidth",
		Title: "probe.memory.stream.table.bandwidth",
		Columns: []model.TableColumn{
			{Key: "kernel_context", Label: "probe.memory.stream.column.kernel_context"},
			{Key: "best_rate_mibs", Label: "probe.memory.stream.column.best_rate", Numeric: true, HigherIsBetter: true},
			{Key: "raw_unit", Label: "probe.memory.stream.column.raw_unit"},
			{Key: "method", Label: "probe.memory.stream.column.method"},
			{Key: "evidence", Label: "probe.memory.stream.column.evidence"},
		},
	}
	// Keep Copy/Triad at the top while retaining every Scale/Add cell.
	order := []string{"Copy", "Triad", "Scale", "Add"}
	for _, kernel := range order {
		for _, run := range runs {
			value, unit := "—", "—"
			evidenceKey := "probe.memory.stream.evidence.failed"
			if sample, ok := run.Sample.Samples[kernel]; ok && sample.RateMiBS > 0 {
				value = model.FormatRate(sample.RateMiBS, "MiB/s")
				unit = sample.Unit
				evidenceKey = "probe.memory.stream.evidence.best_rate"
				if run.Reused {
					evidenceKey = "probe.memory.stream.evidence.reused"
				}
			}
			table.Rows = append(table.Rows, []model.Value{
				model.RawValue(kernel + " / " + streamTableContextLabel(run)), model.RawValue(value), model.RawValue(unit),
				model.RawValue("stream-official-" + strings.ToLower(kernel) + "-" + run.Context + "-v1"), model.KeyValue(evidenceKey),
			})
		}
	}
	return table
}

func streamTableContextLabel(run streamMemoryRun) string {
	if run.Context != "nt" {
		return "1T"
	}
	if run.Reused {
		return "NT(1T-reused)"
	}
	return fmt.Sprintf("NT(%dT)", run.Threads)
}

func streamMeasurementLabel(kernel string, run streamMemoryRun) string {
	return "probe.memory.stream.metric." + strings.ToLower(kernel) + "." + run.Context
}

func streamScalingMeasurementLabel(kernel string) string {
	return "probe.memory.stream.metric." + strings.ToLower(kernel) + ".scaling"
}

func streamRawBlockTitle(run streamMemoryRun) string {
	if run.Context == "nt" {
		return "probe.memory.stream.raw.nt"
	}
	return "probe.memory.stream.raw.1t"
}

func streamNotes(result model.Result, allowance cpuAllowance) []string {
	notes := make([]string, 0, 7)
	if !streamHasContextMeasurements(result, "1t") {
		notes = append(notes, "probe.memory.stream.note.run_failed.1t")
	}
	if allowance.Threads > 1 && !streamHasContextMeasurements(result, "nt") {
		notes = append(notes, "probe.memory.stream.note.run_failed.nt")
	}
	if allowance.Threads <= 1 {
		notes = append(notes, "probe.memory.stream.note.single_core")
	} else {
		notes = append(notes, "probe.memory.stream.note.separate_runs")
	}
	notes = append(notes, "probe.memory.stream.note.units_normalized")
	if streamSourceUnitsDiffer(result.Fields) {
		notes = append(notes, "probe.memory.stream.note.mixed_units")
	}
	if allowance.Limited() && allowance.Threads > 1 {
		notes = append(notes, "probe.memory.stream.note.quota_limited")
	}
	return notes
}

func streamHasContextMeasurements(result model.Result, contextName string) bool {
	suffix := "_" + contextName + "_mib_s"
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 && strings.HasPrefix(measurement.Key, "stream_") && strings.HasSuffix(measurement.Key, suffix) {
			return true
		}
	}
	return false
}

func streamSourceUnitsDiffer(fields []model.Field) bool {
	for _, field := range fields {
		if field.Key != "source_rate_units" {
			continue
		}
		parts := strings.Split(field.Value.Text(), " / ")
		if len(parts) < 2 {
			return false
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		return left != "" && right != "" && left != right
	}
	return false
}

func streamSummaryTokens(result model.Result, workers int) []string {
	values := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			values[measurement.Key] = measurement.Display.Text()
		}
	}
	if workers <= 1 {
		tokens := make([]string, 0, 2)
		if value := values["stream_copy_1t_mib_s"]; value != "" {
			tokens = append(tokens, "Copy 1T/NT "+value)
		}
		if value := values["stream_triad_1t_mib_s"]; value != "" {
			tokens = append(tokens, "Triad 1T/NT "+value)
		}
		return tokens
	}
	tokens := make([]string, 0, 4)
	for _, item := range []struct {
		key   string
		label string
	}{
		{key: "stream_copy_1t_mib_s", label: "Copy 1T"},
		{key: "stream_copy_nt_mib_s", label: fmt.Sprintf("Copy NT(%dT)", workers)},
		{key: "stream_triad_1t_mib_s", label: "Triad 1T"},
		{key: "stream_triad_nt_mib_s", label: fmt.Sprintf("Triad NT(%dT)", workers)},
	} {
		if value := values[item.key]; value != "" {
			tokens = append(tokens, item.label+" "+value)
		}
	}
	return tokens
}

func streamVersion(runs []streamMemoryRun) string {
	for _, run := range runs {
		if match := streamVersionPattern.FindStringSubmatch(run.Output); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return "unknown"
}
