package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type fioOutput struct {
	Version string   `json:"fio version"`
	Jobs    []fioJob `json:"jobs"`
}

type fioJob struct {
	Name string `json:"jobname"`
	// JobStart 是 fio 记录的作业起始时间（毫秒 epoch）。53 个作业 stonewall
	// 串行，靠后的档位可能已经耗尽云盘突发额度；保留相对偏移让读者能判断
	// 某个拐点是介质特性还是额度耗尽的伪影。
	JobStart int64        `json:"job_start"`
	Error    int          `json:"error"`
	Read     fioDirection `json:"read"`
	Write    fioDirection `json:"write"`
}

type fioDirection struct {
	IOBytes uint64  `json:"io_bytes"`
	BW      float64 `json:"bw"`
	BWBytes float64 `json:"bw_bytes"`
	IOPS    float64 `json:"iops"`
	ClatNS  fioClat `json:"clat_ns"`
	ClatUS  fioClat `json:"clat_us"`
	ClatMS  fioClat `json:"clat_ms"`
}

type fioClat struct {
	Mean       float64            `json:"mean"`
	Max        float64            `json:"max"`
	Percentile map[string]float64 `json:"percentile"`
}

const (
	fioQD1LatencyJobName = "latency_qd1"
	fioQD1LatencyMethod  = "fio-direct-4KiB-randread-qd1-latency-v1"
	fioQD1LatencyAvgKey  = "fio_random_read_4k_qd1_latency_avg_ms"
	fioQD1LatencyP95Key  = "fio_random_read_4k_qd1_latency_p95_ms"
	fioQD1LatencyP99Key  = "fio_random_read_4k_qd1_latency_p99_ms"
	fioQD1LatencyMaxKey  = "fio_random_read_4k_qd1_latency_max_ms"
)

// 作业计时窗口。
//
// 53 个作业全部 stonewall 串行，因此总时长是各窗口之和。给每一档统一 10 秒
// 会让磁盘模块跑满 9 分钟，而逐秒采样显示矩阵各档在 2–3 秒内就进入吞吐平台：
// 多出来的时间只是在重复采样同一个稳定值，却持续消耗云盘的突发额度，让靠后
// 的档位测到的是额度耗尽后的性能——矩阵内部因此前后不可比。
//
// 所以窗口按组划分：基础与混合保持 10 秒（它们是磁盘评分的口径），矩阵按各自
// 的收敛速度取更短的窗口。
const (
	// fioBaseRuntime 是基础与混合作业的计时窗口，也是磁盘评分的口径。
	fioBaseRuntime = 10 * time.Second

	// fioCrystalRuntime：RND4K/Q32 读约 3 秒进入平台，SEQ1M 各项约 2 秒。
	// 5 秒覆盖稳定段并留出余量；Crystal 四档共用同一窗口以保持组内可比。
	fioCrystalRuntime = 5 * time.Second

	// fioATTORuntime：512B–16M 各档 2–3 秒进入平台，3 秒足以还原曲线形状。
	fioATTORuntime = 3 * time.Second

	// fioATTOLargeRuntime 用于 3 秒窗口下样本不足或恰好卡在转折点的块大小：
	// 32M/64M 单次 I/O 时间长，实测出现 32768/65601 KiB/s 的交替跳变；
	// 64K 的吞吐平台恰好在第 3 秒才出现，3 秒窗口会把突发段计入结果。
	fioATTOLargeRuntime = 5 * time.Second
)

// fioLatencyStats 是同一个 fio clat 单位下的延迟统计，统一换算为毫秒。
// 每个字段都带有存在标志：fio 可能返回部分 clat 统计，缺失值不能被当作 0。
type fioLatencyStats struct {
	AvgMS float64
	AvgOK bool
	P95MS float64
	P95OK bool
	P99MS float64
	P99OK bool
	MaxMS float64
	MaxOK bool
}

// fioEngine 描述实际使用的 ioengine 及其队列深度能力。
type fioEngine struct {
	Name string
	// AsyncQueue 表示该引擎支持真实的队列深度。psync 是同步引擎，
	// iodepth 对它完全无效，此时报告必须按 QD1 标注而不是照抄参数。
	AsyncQueue bool
	Detected   bool
}

// EffectiveDepth 返回该引擎下实际生效的队列深度。
func (e fioEngine) EffectiveDepth(requested int) int {
	if e.AsyncQueue {
		return requested
	}
	return 1
}

// detectFIOEngine 探测 fio 实际可用的 ioengine。
//
// 精简发行版的 fio 常常没有编入 libaio 支持，硬选 libaio 会让整个磁盘模块以
// "engine libaio not loadable" 失败。这里用 fio --enghelp 读取真实可用列表，
// 按 io_uring -> libaio -> psync 的顺序回退。
func detectFIOEngine(ctx context.Context, fioPath string) fioEngine {
	helpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(helpCtx, fioPath, "--enghelp")
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil && len(output) == 0 {
		// 探测本身失败时退到最保守、必然存在的同步引擎。
		return fioEngine{Name: "psync", AsyncQueue: false}
	}
	available := make(map[string]bool)
	for _, line := range strings.Split(sanitizeCommandOutput(output), "\n") {
		available[strings.TrimSpace(line)] = true
	}
	for _, candidate := range []struct {
		name  string
		async bool
	}{
		{"io_uring", true},
		{"libaio", true},
		{"psync", false},
	} {
		if available[candidate.name] {
			return fioEngine{Name: candidate.name, AsyncQueue: candidate.async, Detected: true}
		}
	}
	return fioEngine{Name: "psync", AsyncQueue: false}
}

func runFIODisk(ctx context.Context, env Environment, fioPath string) (result model.Result) {
	result = newDiskResult()
	expectedJobs := len(fioJobPlan())
	result.Evidence = model.NewEvidence(0, expectedJobs, "job")

	diskPath, actualBytes, disk, err := prepareFIODiskPath(ctx, env)
	if err != nil {
		result.Status = model.StatusError
		addFailure(&result, "prepare", env.Config.DiskPath, err)
		return result
	}

	file, err := os.CreateTemp(diskPath, ".ecs-fio-*")
	if err != nil {
		result.Status = model.StatusError
		addFailure(&result, "create_temp_file", diskPath, fmt.Errorf("创建 fio 临时文件: %w", err))
		return result
	}
	tempName := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(tempName)
		result.Status = model.StatusError
		addFailure(&result, "close_temp_file", tempName, fmt.Errorf("关闭 fio 临时文件: %w", err))
		return result
	}
	defer func() {
		if err := os.Remove(tempName); err != nil && !os.IsNotExist(err) {
			addFailure(&result, "cleanup", tempName, err)
			if result.Status == model.StatusOK {
				result.Status = model.StatusWarning
			}
		}
	}()

	engine := detectFIOEngine(ctx, fioPath)
	plan := fioJobPlan()
	matrixMode := env.Config.DiskMatrixMode
	if matrixMode == "" {
		matrixMode = config.DiskMatrixTime
	}
	args := fioArgumentsForMode(tempName, actualBytes, engine, plan, matrixMode)
	command := exec.CommandContext(ctx, fioPath, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 500 {
			detail = detail[:500] + "…"
		}
		if ctx.Err() != nil {
			runErr = ctx.Err()
		}
		if detail != "" {
			runErr = fmt.Errorf("%w: %s", runErr, detail)
		}
		result.Status = model.StatusError
		addFailure(&result, "benchmark_run", "fio", fmt.Errorf("fio 执行失败: %w", runErr))
		return result
	}
	if stdout.Len() > 4*1024*1024 {
		result.Status = model.StatusError
		addFailure(&result, "parse", "fio", fmt.Errorf("fio JSON 超过 4 MiB 安全上限"))
		return result
	}

	var output fioOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		result.Status = model.StatusError
		addFailure(&result, "parse", "fio", fmt.Errorf("解析 fio JSON: %w", err))
		return result
	}
	jobs := make(map[string]fioJob, len(output.Jobs))
	for _, job := range output.Jobs {
		jobs[job.Name] = job
		if job.Error != 0 {
			result.Status = model.StatusWarning
			result.AddFailure(model.Failure{
				Category: model.FailureUnknown, Stage: "benchmark_run", Target: job.Name,
				Count: 1, Message: fmt.Sprintf("fio job returned error code %d", job.Error),
			})
		}
	}
	offsets := fioModuleOffsets(jobs)
	validJobs := 0
	for _, spec := range plan {
		if job, ok := jobs[spec.Name]; ok && fioJobHasEvidence(spec, job) {
			validJobs++
		}
	}
	result.Evidence = model.NewEvidence(validJobs, expectedJobs, "job")

	seqWrite := fioBandwidthMiB(jobs["seqwrite"].Write)
	seqRead := fioBandwidthMiB(jobs["seqread"].Read)
	randomRead := jobs["randread"].Read.IOPS
	randomWrite := jobs["randwrite"].Write.IOPS
	appendFIOQD1LatencyMeasurements(&result, jobs)
	if !isPositiveFinite(seqWrite) && !isPositiveFinite(seqRead) && !isPositiveFinite(randomRead) && !isPositiveFinite(randomWrite) {
		result.Status = model.StatusError
		addFailure(&result, "validate", "fio", fmt.Errorf("fio JSON 未包含可用的磁盘统计"))
		return result
	}
	randDepth := engine.EffectiveDepth(32)
	missingBase := make([]string, 0, 4)
	if !appendFIOBaseMeasurement(&result, "fio_sequential_write_mib_s", seqWrite, "MiB/s", "fio-direct-1MiB-write-qd1-v1") {
		missingBase = append(missingBase, "顺序写")
	}
	if !appendFIOBaseMeasurement(&result, "fio_sequential_read_mib_s", seqRead, "MiB/s", "fio-direct-1MiB-read-qd1-v1") {
		missingBase = append(missingBase, "顺序读")
	}
	if !appendFIOBaseMeasurement(&result, "fio_random_read_4k_iops", randomRead, "IOPS", fmt.Sprintf("fio-direct-4KiB-randread-qd%d-v1", randDepth)) {
		missingBase = append(missingBase, "4K 随机读")
	}
	if !appendFIOBaseMeasurement(&result, "fio_random_write_4k_iops", randomWrite, "IOPS", fmt.Sprintf("fio-direct-4KiB-randwrite-qd%d-v1", randDepth)) {
		missingBase = append(missingBase, "4K 随机写")
	}
	if len(missingBase) > 0 {
		result.Status = model.StatusWarning
	}
	if p95 := fioP95Milliseconds(jobs["randread"].Read); isPositiveFinite(p95) {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "fio_random_read_p95_ms", Label: diskMeasurementLabel("fio_random_read_p95_ms"),
			Value: p95, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.3f ms", p95)),
			Method: "fio-clat-p95-v1", HigherIsBetter: model.BoolPtr(false),
		})
	} else {
		result.Status = model.StatusWarning
	}
	if p95 := fioP95Milliseconds(jobs["randwrite"].Write); isPositiveFinite(p95) {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "fio_random_write_p95_ms", Label: diskMeasurementLabel("fio_random_write_p95_ms"),
			Value: p95, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.3f ms", p95)),
			Method: "fio-clat-p95-v1", HigherIsBetter: model.BoolPtr(false),
		})
	} else {
		result.Status = model.StatusWarning
	}

	mixDepth := engine.EffectiveDepth(64)
	appendFIOMixedResults(&result, plan, jobs, mixDepth)
	appendCrystalMatrix(&result, jobs, engine, offsets)
	appendATTOMatrix(&result, jobs, engine, offsets)

	if !isPositiveFinite(seqWrite) || !isPositiveFinite(seqRead) || !isPositiveFinite(randomRead) || !isPositiveFinite(randomWrite) {
		result.Status = model.StatusWarning
	}
	requestedBytes := int64(env.Config.DiskMiB) * 1024 * 1024
	if actualBytes < requestedBytes {
		result.Status = model.StatusWarning
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: diskFieldLabel("engine"), Value: model.RawValue("fio")},
		{Key: "version", Label: diskFieldLabel("version"), Value: model.RawValue(fallback(output.Version, "unknown"))},
		{Key: "binary_sha256", Label: diskFieldLabel("binary_sha256"), Value: model.RawValue(fallback(binarySHA256(fioPath), "unavailable"))},
		{Key: "disk_device", Label: diskFieldLabel("disk_device"), Value: model.RawValue(fallback(disk.DiskDevice, "unavailable"))},
		{Key: "path", Label: diskFieldLabel("path"), Value: model.RawValue(diskPath)},
		{Key: "mount", Label: diskFieldLabel("mount"), Value: model.RawValue(fallback(disk.DiskMount, diskPath))},
		{Key: "disk_total", Label: diskFieldLabel("disk_total"), Value: model.RawValue(model.FormatBytes(disk.DiskTotal))},
		{Key: "disk_used", Label: diskFieldLabel("disk_used"), Value: model.RawValue(model.FormatBytes(disk.DiskUsed))},
		{Key: "disk_available", Label: diskFieldLabel("disk_available"), Value: model.RawValue(model.FormatBytes(disk.DiskFree))},
		{Key: "disk_usage_percent", Label: diskFieldLabel("disk_usage_percent"), Value: model.RawValue(fmt.Sprintf("%.1f %%", disk.DiskUsage))},
		{Key: "file_size", Label: diskFieldLabel("file_size"), Value: model.RawValue(model.FormatBytes(uint64(actualBytes)))},
		{Key: "free_before", Label: diskFieldLabel("free_before"), Value: model.RawValue(model.FormatBytes(disk.DiskFree))},
		{Key: "direct_io", Label: diskFieldLabel("direct_io"), Value: model.RawValue("1")},
		{Key: "ioengine", Label: diskFieldLabel("ioengine"), Value: model.RawValue(describeFIOEngine(engine))},
		{Key: "jobs", Label: diskFieldLabel("jobs"), Value: model.RawValue(strconv.Itoa(len(plan)))},
		{Key: "job_duration_base", Label: diskFieldLabel("job_duration_base"), Value: model.RawValue(fioBaseRuntime.String())},
		{Key: "job_duration_crystal", Label: diskFieldLabel("job_duration_crystal"), Value: model.RawValue(fioCrystalRuntime.String())},
		{Key: "job_duration_atto", Label: diskFieldLabel("job_duration_atto"), Value: model.RawValue(fmt.Sprintf("%s（64K/32M/64M 为 %s）", fioATTORuntime, fioATTOLargeRuntime))},
		{Key: "plan_duration", Label: diskFieldLabel("plan_duration"), Value: model.RawValue(fioPlanDuration(plan).String())},
		{Key: "matrix_mode", Label: diskFieldLabel("matrix_mode"), Value: model.RawValue(matrixMode)},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", fallback(output.Version, "unknown"))
	addComparisonParameter(result.Methodology.Parameters, "tool_sha256", fallback(binarySHA256(fioPath), "unavailable"))
	addComparisonParameter(result.Methodology.Parameters, "actual_file_size", model.FormatBytes(uint64(actualBytes)))
	addComparisonParameter(result.Methodology.Parameters, "direct_io", "1")
	addComparisonParameter(result.Methodology.Parameters, "ioengine", engine.Name)
	addComparisonParameter(result.Methodology.Parameters, "jobs", strconv.Itoa(len(plan)))
	addComparisonParameter(result.Methodology.Parameters, "job_duration", fioPlanDuration(plan).String())
	result.Sources = []model.Source{
		{Name: "fio", URL: "https://github.com/axboe/fio", Purpose: "probe.disk.source.fio"},
		{Name: "YABS", URL: "https://github.com/masonr/yet-another-bench-script", Purpose: "probe.disk.source.yabs"},
	}
	if !engine.Detected {
		result.Status = model.StatusWarning
	}
	return result
}

func fioJobHasEvidence(spec fioJobSpec, job fioJob) bool {
	if job.Error != 0 {
		return false
	}
	directionHasEvidence := func(direction fioDirection) bool {
		return direction.IOBytes > 0 &&
			(isPositiveFinite(direction.BW) || isPositiveFinite(direction.BWBytes) || isPositiveFinite(direction.IOPS))
	}
	switch spec.RW {
	case "read", "randread":
		return directionHasEvidence(job.Read)
	case "write", "randwrite":
		return directionHasEvidence(job.Write)
	case "randrw":
		return directionHasEvidence(job.Read) && directionHasEvidence(job.Write)
	default:
		return false
	}
}

func appendFIOBaseMeasurement(result *model.Result, key string, value float64, unit, method string) bool {
	if !isPositiveFinite(value) {
		return false
	}
	result.Measurements = append(result.Measurements, model.Measurement{
		Key: key, Label: diskMeasurementLabel(key), Value: value, Unit: unit, Display: model.RawValue(model.FormatRate(value, unit)),
		Method: method, HigherIsBetter: model.BoolPtr(true),
	})
	return true
}

// appendFIOMixedResults 将四档 YABS 混合作业完整呈现出来。
//
// fio JSON 缺少某个 job 或某个方向时，不能把该行直接丢掉，也不能用 0
// 假装跑出了结果。表格保留原计划的四行，缺失单元显示为“—”，并把结果标为
// warning；只有读写吞吐都存在时才计算“合计”，避免用一侧数据冒充总吞吐。
func appendFIOMixedResults(result *model.Result, plan []fioJobSpec, jobs map[string]fioJob, mixDepth int) {
	table := model.Table{
		Key:   "disk.fio.mixed",
		Title: "probe.disk.table.mixed",
		Columns: []model.TableColumn{
			{Key: "block_size", Label: "probe.disk.column.block_size"},
			{Key: "read_mib_s", Label: "probe.disk.column.read", Numeric: true, HigherIsBetter: true},
			{Key: "read_iops", Label: "probe.disk.column.read_iops", Numeric: true, HigherIsBetter: true},
			{Key: "write_mib_s", Label: "probe.disk.column.write", Numeric: true, HigherIsBetter: true},
			{Key: "write_iops", Label: "probe.disk.column.write_iops", Numeric: true, HigherIsBetter: true},
			{Key: "total_mib_s", Label: "probe.disk.column.total", Numeric: true, HigherIsBetter: true},
		},
		RowIdentity: "block_size",
	}
	incomplete := 0
	for _, job := range plan {
		if !job.Mixed() {
			continue
		}
		sample, ok := jobs[job.Name]
		readMiB, writeMiB := 0.0, 0.0
		readIOPS, writeIOPS := 0.0, 0.0
		if ok {
			readMiB = fioBandwidthMiB(sample.Read)
			writeMiB = fioBandwidthMiB(sample.Write)
			readIOPS, writeIOPS = sample.Read.IOPS, sample.Write.IOPS
		}
		if !isPositiveFinite(readMiB) || !isPositiveFinite(readIOPS) || !isPositiveFinite(writeMiB) || !isPositiveFinite(writeIOPS) {
			incomplete++
		}
		total := "—"
		if isPositiveFinite(readMiB) && isPositiveFinite(writeMiB) {
			total = formatMatrixRate(readMiB+writeMiB, "MiB/s")
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue(job.BlockSize),
			model.RawValue(formatMatrixRate(readMiB, "MiB/s")),
			model.RawValue(formatMatrixRate(readIOPS, "IOPS")),
			model.RawValue(formatMatrixRate(writeMiB, "MiB/s")),
			model.RawValue(formatMatrixRate(writeIOPS, "IOPS")),
			model.RawValue(total),
		})
		method := fmt.Sprintf("fio-direct-%s-randrw50-qd%d-n2-v1", job.BlockSize, mixDepth)
		if isPositiveFinite(readMiB) {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: fmt.Sprintf("fio_mixed_%s_read_mib_s", job.BlockSize), Label: diskMeasurementLabel(fmt.Sprintf("fio_mixed_%s_read_mib_s", job.BlockSize)),
				Value: readMiB, Unit: "MiB/s", Display: model.RawValue(model.FormatRate(readMiB, "MiB/s")),
				Method: method, HigherIsBetter: model.BoolPtr(true),
			})
		}
		if isPositiveFinite(writeMiB) {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: fmt.Sprintf("fio_mixed_%s_write_mib_s", job.BlockSize), Label: diskMeasurementLabel(fmt.Sprintf("fio_mixed_%s_write_mib_s", job.BlockSize)),
				Value: writeMiB, Unit: "MiB/s", Display: model.RawValue(model.FormatRate(writeMiB, "MiB/s")),
				Method: method, HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
	if len(table.Rows) > 0 {
		result.Tables = append(result.Tables, table)
	}
	if incomplete > 0 {
		if result.Status == model.StatusOK {
			result.Status = model.StatusWarning
		}
	}
}

type matrixCell struct {
	ReadMiB, ReadIOPS   float64
	WriteMiB, WriteIOPS float64
	ReadMethod          string
	WriteMethod         string
}

func appendCrystalMatrix(result *model.Result, jobs map[string]fioJob, engine fioEngine, offsets map[string]float64) {
	specs := crystalJobSpecs()
	cells := make(map[string]matrixCell, 4)
	// 每个工作负载记录读方向的起始偏移：同一负载的读写相邻执行，
	// 用读的偏移标注该行足以定位它在模块内的位置。
	rowOffsets := make(map[string]string, 4)
	missing := 0
	for _, spec := range specs {
		if spec.Direction == "read" {
			rowOffsets[spec.Workload] = formatModuleOffset(offsets, spec.Name)
		}
		sample, ok := jobs[spec.Name]
		if !ok {
			missing++
			continue
		}
		direction := sample.Read
		if spec.Direction == "write" {
			direction = sample.Write
		}
		throughput := fioBandwidthMiB(direction)
		if !isPositiveFinite(throughput) || !isPositiveFinite(direction.IOPS) {
			missing++
		}
		if !isPositiveFinite(throughput) && !isPositiveFinite(direction.IOPS) {
			continue
		}
		actualDepth := engine.EffectiveDepth(spec.IODepth)
		workloadID := strings.ToLower(strings.ReplaceAll(spec.Workload, "/", "-"))
		method := fmt.Sprintf("fio-direct-crystal-%s-%s-qd%d-v1", workloadID, spec.Direction, actualDepth)
		cell := cells[spec.Workload]
		if spec.Direction == "read" {
			cell.ReadMiB, cell.ReadIOPS, cell.ReadMethod = throughput, direction.IOPS, method
			appendFioMatrixMeasurements(result, crystalMetricStem(spec.Workload), "read", throughput, direction.IOPS, method)
		} else {
			cell.WriteMiB, cell.WriteIOPS, cell.WriteMethod = throughput, direction.IOPS, method
			appendFioMatrixMeasurements(result, crystalMetricStem(spec.Workload), "write", throughput, direction.IOPS, method)
		}
		cells[spec.Workload] = cell
	}

	table := model.Table{
		Title: "probe.disk.table.crystal",
		Key:   "disk.fio.crystal",
		Columns: []model.TableColumn{
			{Key: "workload", Label: "probe.disk.column.workload"},
			{Key: "read_mib_s", Label: "probe.disk.column.read", Numeric: true, HigherIsBetter: true},
			{Key: "read_iops", Label: "probe.disk.column.read_iops", Numeric: true, HigherIsBetter: true},
			{Key: "write_mib_s", Label: "probe.disk.column.write", Numeric: true, HigherIsBetter: true},
			{Key: "write_iops", Label: "probe.disk.column.write_iops", Numeric: true, HigherIsBetter: true},
			{Key: "start_offset", Label: "probe.disk.column.offset"},
			{Key: "status", Label: "probe.disk.column.status"},
		},
		RowIdentity: "workload",
	}
	for _, workload := range []string{"RND4K/Q1", "RND4K/Q32", "SEQ1M/Q1", "SEQ1M/Q8"} {
		cell := cells[workload]
		complete := isPositiveFinite(cell.ReadMiB) && isPositiveFinite(cell.ReadIOPS) &&
			isPositiveFinite(cell.WriteMiB) && isPositiveFinite(cell.WriteIOPS)
		row := []model.Value{
			model.RawValue(workload),
			model.RawValue(formatMatrixRate(cell.ReadMiB, "MiB/s")), model.RawValue(formatMatrixRate(cell.ReadIOPS, "IOPS")),
			model.RawValue(formatMatrixRate(cell.WriteMiB, "MiB/s")), model.RawValue(formatMatrixRate(cell.WriteIOPS, "IOPS")),
			model.RawValue(fallback(rowOffsets[workload], "—")), model.KeyValue(diskTableStatusKey(table.Key, complete)),
		}
		table.Rows = append(table.Rows, row)
	}
	result.Tables = append(result.Tables, table)
	if missing > 0 {
		result.Status = model.StatusWarning
	}
}

func appendATTOMatrix(result *model.Result, jobs map[string]fioJob, engine fioEngine, offsets map[string]float64) {
	cells := make(map[string]matrixCell, len(attoBlockSizes))
	missing := 0
	for _, block := range attoBlockSizes {
		for _, directionName := range []string{"read", "write"} {
			name := "atto_" + directionName + "_" + block.FIO
			sample, ok := jobs[name]
			if !ok {
				missing++
				continue
			}
			direction := sample.Read
			if directionName == "write" {
				direction = sample.Write
			}
			throughput := fioBandwidthMiB(direction)
			if !isPositiveFinite(throughput) || !isPositiveFinite(direction.IOPS) {
				missing++
			}
			if !isPositiveFinite(throughput) && !isPositiveFinite(direction.IOPS) {
				continue
			}
			method := fmt.Sprintf("fio-direct-atto-%s-%s-qd%d-v1", block.FIO, directionName, engine.EffectiveDepth(1))
			cell := cells[block.Label]
			if directionName == "read" {
				cell.ReadMiB, cell.ReadIOPS, cell.ReadMethod = throughput, direction.IOPS, method
				appendFioMatrixMeasurements(result, "atto_"+block.FIO, "read", throughput, direction.IOPS, method)
			} else {
				cell.WriteMiB, cell.WriteIOPS, cell.WriteMethod = throughput, direction.IOPS, method
				appendFioMatrixMeasurements(result, "atto_"+block.FIO, "write", throughput, direction.IOPS, method)
			}
			cells[block.Label] = cell
		}
	}

	table := model.Table{
		Title: "probe.disk.table.atto",
		Key:   "disk.fio.atto",
		Columns: []model.TableColumn{
			{Key: "block_size", Label: "probe.disk.column.block_size"},
			{Key: "read_mib_s", Label: "probe.disk.column.read", Numeric: true, HigherIsBetter: true},
			{Key: "read_iops", Label: "probe.disk.column.read_iops", Numeric: true, HigherIsBetter: true},
			{Key: "write_mib_s", Label: "probe.disk.column.write", Numeric: true, HigherIsBetter: true},
			{Key: "write_iops", Label: "probe.disk.column.write_iops", Numeric: true, HigherIsBetter: true},
			{Key: "runtime", Label: "probe.disk.column.runtime"},
			{Key: "start_offset", Label: "probe.disk.column.offset"},
			{Key: "status", Label: "probe.disk.column.status"},
		},
		RowIdentity: "block_size",
	}
	for _, block := range attoBlockSizes {
		cell := cells[block.Label]
		complete := isPositiveFinite(cell.ReadMiB) && isPositiveFinite(cell.ReadIOPS) &&
			isPositiveFinite(cell.WriteMiB) && isPositiveFinite(cell.WriteIOPS)
		row := []model.Value{
			model.RawValue(block.Label),
			model.RawValue(formatMatrixRate(cell.ReadMiB, "MiB/s")), model.RawValue(formatMatrixRate(cell.ReadIOPS, "IOPS")),
			model.RawValue(formatMatrixRate(cell.WriteMiB, "MiB/s")), model.RawValue(formatMatrixRate(cell.WriteIOPS, "IOPS")),
			model.RawValue(block.Runtime.String()),
			model.RawValue(formatModuleOffset(offsets, "atto_read_"+block.FIO)),
			model.KeyValue(diskTableStatusKey(table.Key, complete)),
		}
		table.Rows = append(table.Rows, row)
	}
	result.Tables = append(result.Tables, table)
	if missing > 0 {
		result.Status = model.StatusWarning
	}
}

func appendFioMatrixMeasurements(result *model.Result, stem, direction string, throughput, iops float64, method string) {
	if isPositiveFinite(throughput) {
		key := stem + "_" + direction + "_mib_s"
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: key, Label: diskMeasurementLabel(key),
			Value: throughput, Unit: "MiB/s", Display: model.RawValue(model.FormatRate(throughput, "MiB/s")), Method: method,
			HigherIsBetter: model.BoolPtr(true),
		})
	}
	if isPositiveFinite(iops) {
		key := stem + "_" + direction + "_iops"
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: key, Label: diskMeasurementLabel(key),
			Value: iops, Unit: "IOPS", Display: model.RawValue(model.FormatRate(iops, "IOPS")), Method: method,
			HigherIsBetter: model.BoolPtr(true),
		})
	}
}

func crystalMetricStem(workload string) string {
	return "crystal_" + strings.ToLower(strings.ReplaceAll(workload, "/", "_"))
}

func formatMatrixRate(value float64, unit string) string {
	if !isPositiveFinite(value) {
		return "—"
	}
	return model.FormatRate(value, unit)
}

// appendFIOQD1LatencyMeasurements 从固定的 QD1 randread 作业追加延迟指标。
// 延迟来自该作业自己的 clat，而不是基础 QD32 作业或其他工具的结果。
func appendFIOQD1LatencyMeasurements(result *model.Result, jobs map[string]fioJob) {
	job, ok := jobs[fioQD1LatencyJobName]
	if !ok {
		markFIOQD1LatencyMissing(result, []string{"avg", "P95", "P99", "max"})
		return
	}

	stats, ok := fioLatencyStatsFor(job.Read)
	if !ok {
		markFIOQD1LatencyMissing(result, []string{"avg", "P95", "P99", "max"})
		return
	}

	missing := make([]string, 0, 4)
	appendMetric := func(key, label string, value float64, present bool) {
		if !present {
			missing = append(missing, label)
			return
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: key, Label: diskMeasurementLabel(key),
			Value: value, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.3f ms", value)),
			Method: fioQD1LatencyMethod, HigherIsBetter: model.BoolPtr(false),
		})
	}
	appendMetric(fioQD1LatencyAvgKey, "均值", stats.AvgMS, stats.AvgOK)
	appendMetric(fioQD1LatencyP95Key, "P95", stats.P95MS, stats.P95OK)
	appendMetric(fioQD1LatencyP99Key, "P99", stats.P99MS, stats.P99OK)
	appendMetric(fioQD1LatencyMaxKey, "最大", stats.MaxMS, stats.MaxOK)
	if len(missing) > 0 {
		markFIOQD1LatencyMissing(result, missing)
	}
}

func markFIOQD1LatencyMissing(result *model.Result, missing []string) {
	if len(missing) == 0 {
		return
	}
	if result.Status == model.StatusOK {
		result.Status = model.StatusWarning
	}
}

// parseFIOJobs 把 fio 的 JSON 输出按作业名索引。
func parseFIOJobs(output []byte) (map[string]fioJob, error) {
	var parsed fioOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, fmt.Errorf("解析 fio JSON: %w", err)
	}
	jobs := make(map[string]fioJob, len(parsed.Jobs))
	for _, job := range parsed.Jobs {
		jobs[job.Name] = job
	}
	return jobs, nil
}

func prepareFIODiskPath(ctx context.Context, env Environment) (string, int64, systemSnapshot, error) {
	var disk systemSnapshot
	diskPath, err := filepath.Abs(env.Config.DiskPath)
	if err != nil {
		return "", 0, disk, fmt.Errorf("解析测试路径: %w", err)
	}
	info, err := os.Stat(diskPath)
	if err != nil || !info.IsDir() {
		return "", 0, disk, fmt.Errorf("测试路径不可用: %s", diskPath)
	}
	collectDisk(ctx, diskPath, &disk)
	actualBytes, err := fioDiskSize(uint64(env.Config.DiskMiB)*1024*1024, disk.DiskFree)
	if err != nil {
		return "", 0, disk, err
	}
	return diskPath, actualBytes, disk, nil
}

// fioDiskSize 给出测试文件的实际大小。
//
// 作业计划恒定包含 Crystal 与 ATTO 矩阵，因此文件必须能容纳最大的 ATTO 块：
// 按 64 MiB 对齐并保留至少两个窗口，避免边界截断与单窗口缓存效应。
func fioDiskSize(requestedBytes, freeBytes uint64) (int64, error) {
	actualBytes := int64(requestedBytes)
	if freeBytes > 0 {
		safeLimit := int64(freeBytes / 5)
		if actualBytes > safeLimit {
			actualBytes = safeLimit
		}
	}
	// The largest ATTO job is 64 MiB.  Keep two aligned 64 MiB windows so fio
	// can place a full block without edge truncation or an accidental
	// single-window cache effect on small VPS disks.
	const alignment = int64(64 * 1024 * 1024)
	const minimum = int64(128 * 1024 * 1024)
	// A caller may configure a smaller baseline file size.  Expand it to the
	// matrix minimum when the 20% free-space safety limit permits; otherwise
	// refusing the run is safer than silently dropping ATTO cells.
	if actualBytes < minimum {
		if freeBytes > 0 && int64(freeBytes/5) < minimum {
			return 0, fmt.Errorf("测试盘安全余量不足 %s（ATTO 最大块为 64 MiB）", model.FormatBytes(uint64(minimum)))
		}
		actualBytes = minimum
	}
	actualBytes = actualBytes / alignment * alignment
	if actualBytes < minimum {
		return 0, fmt.Errorf("测试盘安全余量不足 %s（ATTO 最大块为 64 MiB）", model.FormatBytes(uint64(minimum)))
	}
	return actualBytes, nil
}

// describeFIOEngine 给出带队列能力说明的 ioengine 描述。
func describeFIOEngine(engine fioEngine) string {
	if engine.AsyncQueue {
		return engine.Name + "（异步，队列深度有效）"
	}
	return engine.Name + "（同步，队列深度恒为 1）"
}

// FIOPlanDuration 是磁盘模块一次完整作业计划的串行执行下限。
// probe.EstimateFor 直接读取它，避免把作业时长在两处各写一份。
func FIOPlanDuration() time.Duration {
	return fioPlanDuration(fioJobPlan())
}

// fioJobSpec 描述一个 fio 作业。
type fioJobSpec struct {
	Name      string
	RW        string
	BlockSize string
	IODepth   int
	NumJobs   int
	Matrix    string
	Workload  string
	Direction string
	// MixRead 只对 randrw 有效，表示读操作占比。
	MixRead  int
	EndFsync bool
	// Runtime 是这个作业自己的计时窗口；0 表示使用 fioBaseRuntime。
	// 见上方常量注释：矩阵作业用更短的窗口，避免 53 个作业串行成 9 分钟。
	Runtime time.Duration
}

// EffectiveRuntime 给出该作业实际使用的计时窗口。
func (s fioJobSpec) EffectiveRuntime() time.Duration {
	if s.Runtime > 0 {
		return s.Runtime
	}
	return fioBaseRuntime
}

// Mixed 表示这是一个 50/50 混合随机读写作业。
func (s fioJobSpec) Mixed() bool { return s.RW == "randrw" }

// fioJobPlan 给出本次磁盘测试的作业集合。
//
// 前四项是 ecs 既有口径：1 MiB 顺序读写反映带宽上限，4 KiB 随机读写反映 IOPS。
// 后面是 YABS 口径矩阵：4k/64k/512k/1m 四档 50/50 混合随机读写，iodepth=64、
// numjobs=2，这是社区里流传最广、样本量最大的磁盘口径，补上它才能和主流测评
// 贴的数字对得上。所有被选中的 disk 模块都执行完整混合与 Crystal/ATTO 矩阵，
// 避免同一模块因为默认模块预设不同而产生不可比的作业集合。固定低延迟作业
// 单独使用 4 KiB randread、iodepth=1、numjobs=1，且与这些作业一起写入同一份 fio JSON。
func fioJobPlan() []fioJobSpec {
	plan := []fioJobSpec{
		{Name: "seqwrite", RW: "write", BlockSize: "1m", IODepth: 1, NumJobs: 1, EndFsync: true},
		{Name: "seqread", RW: "read", BlockSize: "1m", IODepth: 1, NumJobs: 1},
		{Name: "randread", RW: "randread", BlockSize: "4k", IODepth: 32, NumJobs: 1},
		{Name: "randwrite", RW: "randwrite", BlockSize: "4k", IODepth: 32, NumJobs: 1, EndFsync: true},
		{Name: fioQD1LatencyJobName, RW: "randread", BlockSize: "4k", IODepth: 1, NumJobs: 1, Matrix: "latency", Direction: "read"},
	}
	blocks := []string{"4k", "64k", "512k", "1m"}
	for _, blockSize := range blocks {
		plan = append(plan, fioJobSpec{
			Name:      "mix" + blockSize,
			RW:        "randrw",
			BlockSize: blockSize,
			IODepth:   64,
			NumJobs:   2,
			MixRead:   50,
		})
	}
	plan = append(plan, crystalJobSpecs()...)
	plan = append(plan, attoJobSpecs()...)
	return plan
}

// fioPlanDuration 是一份作业计划的串行执行下限，用于运行时长估算。
// 所有作业都带 stonewall，因此总时长就是各计时窗口之和。
func fioPlanDuration(plan []fioJobSpec) time.Duration {
	var total time.Duration
	for _, job := range plan {
		total += job.EffectiveRuntime()
	}
	return total
}

// fioModuleOffsets 给出每个作业相对本模块第一个作业的起始偏移（秒）。
//
// fio 的 job_start 是毫秒 epoch；取最小值作为模块零点即可，不需要额外计时。
// JSON 缺少该字段时返回 nil，表格按“—”呈现而不是编一个偏移。
func fioModuleOffsets(jobs map[string]fioJob) map[string]float64 {
	var base int64
	for _, job := range jobs {
		if job.JobStart > 0 && (base == 0 || job.JobStart < base) {
			base = job.JobStart
		}
	}
	if base == 0 {
		return nil
	}
	offsets := make(map[string]float64, len(jobs))
	for name, job := range jobs {
		if job.JobStart > 0 {
			offsets[name] = float64(job.JobStart-base) / 1000
		}
	}
	return offsets
}

// formatModuleOffset 呈现一个作业的起始偏移。
func formatModuleOffset(offsets map[string]float64, name string) string {
	if offsets == nil {
		return "—"
	}
	value, ok := offsets[name]
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.0f s", value)
}

func crystalJobSpecs() []fioJobSpec {
	specs := []fioJobSpec{
		{Name: "crystal_read_rnd4k_q1", RW: "randread", BlockSize: "4k", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q1", Direction: "read"},
		{Name: "crystal_write_rnd4k_q1", RW: "randwrite", BlockSize: "4k", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q1", Direction: "write", EndFsync: true},
		{Name: "crystal_read_rnd4k_q32", RW: "randread", BlockSize: "4k", IODepth: 32, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q32", Direction: "read"},
		{Name: "crystal_write_rnd4k_q32", RW: "randwrite", BlockSize: "4k", IODepth: 32, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q32", Direction: "write", EndFsync: true},
		{Name: "crystal_read_seq1m_q1", RW: "read", BlockSize: "1m", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q1", Direction: "read"},
		{Name: "crystal_write_seq1m_q1", RW: "write", BlockSize: "1m", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q1", Direction: "write", EndFsync: true},
		{Name: "crystal_read_seq1m_q8", RW: "read", BlockSize: "1m", IODepth: 8, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q8", Direction: "read"},
		{Name: "crystal_write_seq1m_q8", RW: "write", BlockSize: "1m", IODepth: 8, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q8", Direction: "write", EndFsync: true},
	}
	// 八个单元共用同一窗口：Crystal 的四种工作负载要互相可比，给其中某一档
	// 单独放宽会在组内引入口径差。
	for index := range specs {
		specs[index].Runtime = fioCrystalRuntime
	}
	return specs
}

// attoBlockSizes 是 ATTO 的完整块大小谱系与各档的计时窗口。
//
// 大多数块在 2–3 秒内进入吞吐平台（逐秒采样：128K–16M 首秒 55–63 MB/s，
// 第二秒起稳定在 49–50 MB/s）。三个例外用更长的窗口，理由各不相同：
// 64K 的平台恰好在第 3 秒才出现，3 秒会把突发段计入结果；32M 与 64M 单次
// I/O 时间长，3 秒内样本过少（实测 32M 出现 32768/65601 KiB/s 交替）。
var attoBlockSizes = []struct {
	FIO     string
	Label   string
	Runtime time.Duration
}{
	{"512b", "512B", fioATTORuntime}, {"1k", "1K", fioATTORuntime},
	{"2k", "2K", fioATTORuntime}, {"4k", "4K", fioATTORuntime},
	{"8k", "8K", fioATTORuntime}, {"16k", "16K", fioATTORuntime},
	{"32k", "32K", fioATTORuntime}, {"64k", "64K", fioATTOLargeRuntime},
	{"128k", "128K", fioATTORuntime}, {"256k", "256K", fioATTORuntime},
	{"512k", "512K", fioATTORuntime}, {"1m", "1M", fioATTORuntime},
	{"2m", "2M", fioATTORuntime}, {"4m", "4M", fioATTORuntime},
	{"8m", "8M", fioATTORuntime}, {"16m", "16M", fioATTORuntime},
	{"32m", "32M", fioATTOLargeRuntime}, {"64m", "64M", fioATTOLargeRuntime},
}

func attoJobSpecs() []fioJobSpec {
	jobs := make([]fioJobSpec, 0, len(attoBlockSizes)*2)
	for _, block := range attoBlockSizes {
		jobs = append(jobs,
			fioJobSpec{Name: "atto_read_" + block.FIO, RW: "read", BlockSize: block.FIO, IODepth: 1, NumJobs: 1, Matrix: "atto", Workload: block.Label, Direction: "read", Runtime: block.Runtime},
			fioJobSpec{Name: "atto_write_" + block.FIO, RW: "write", BlockSize: block.FIO, IODepth: 1, NumJobs: 1, Matrix: "atto", Workload: block.Label, Direction: "write", EndFsync: true, Runtime: block.Runtime},
		)
	}
	return jobs
}

// fioFixedIOSize 是复核模式下每个 ATTO 单元的固定传输量。
//
// 固定传输量与按时长是两种测量口径，数值不可混比：小块档位需要海量 I/O 操作
// 才能凑够同样的字节数（实测 512B 读 237 秒、写 348 秒），整轮超过 20 分钟。
// 它的价值在于每档数据量相同，便于观察吞吐随传输量、突发额度与缓存的变化。
const fioFixedIOSize = "256m"

func fioArguments(filename string, size int64, engine fioEngine, plan []fioJobSpec) []string {
	return fioArgumentsForMode(filename, size, engine, plan, config.DiskMatrixTime)
}

func fioArgumentsForMode(filename string, size int64, engine fioEngine, plan []fioJobSpec, matrixMode string) []string {
	args := []string{"--output-format=json", "--eta=never", "--clat_percentiles=1"}
	for index, job := range plan {
		args = append(args, "--name="+job.Name)
		// stonewall 让每个作业串行执行，避免顺序与随机负载互相干扰。
		// 第一个作业不需要屏障。
		if index > 0 {
			args = append(args, "--stonewall=1")
		}
		args = append(args,
			"--filename="+filename,
			"--size="+strconv.FormatInt(size, 10),
			"--direct=1",
			"--ioengine="+engine.Name,
			"--thread=1",
		)
		// 复核模式只改 ATTO：Crystal 与基础作业保持按时长，否则整轮无法收敛。
		if matrixMode == config.DiskMatrixFixed && job.Matrix == "atto" {
			args = append(args, "--io_size="+fioFixedIOSize)
		} else {
			args = append(args,
				// 计时窗口按作业取：矩阵各档的收敛速度不同，统一 10 秒只是在
				// 重复采样同一个平台值，却让整个模块串行成 9 分钟。
				"--runtime="+strconv.Itoa(int(job.EffectiveRuntime()/time.Second)),
				"--time_based=1",
			)
		}
		args = append(args,
			"--randrepeat=1",
			"--invalidate=1",
			"--group_reporting=1",
			"--rw="+job.RW,
			"--bs="+job.BlockSize,
			// 同步引擎下 iodepth 无效，直接按实际生效值传参，
			// 避免 fio 输出误导性的队列深度警告。
			"--iodepth="+strconv.Itoa(engine.EffectiveDepth(job.IODepth)),
			"--numjobs="+strconv.Itoa(job.NumJobs),
		)
		if job.Mixed() {
			args = append(args, "--rwmixread="+strconv.Itoa(job.MixRead))
		}
		if job.EndFsync {
			args = append(args, "--end_fsync=1")
		}
	}
	return args
}

func fioBandwidthMiB(direction fioDirection) float64 {
	if isPositiveFinite(direction.BWBytes) {
		value := direction.BWBytes / 1024 / 1024
		if isPositiveFinite(value) {
			return value
		}
	}
	if isPositiveFinite(direction.BW) {
		// fio documents bw as KiB/s in JSON output.
		value := direction.BW / 1024
		if isPositiveFinite(value) {
			return value
		}
	}
	return 0
}

// fioLatencyStatsFor 兼容 fio JSON 可能使用的 clat_ns、clat_us、clat_ms。
// 一个方向通常只会有其中一个字段；按 fio 的精度从 ns 到 ms 选择第一个有数据的字段。
func fioLatencyStatsFor(direction fioDirection) (fioLatencyStats, bool) {
	candidates := []struct {
		clat   fioClat
		factor float64
	}{
		{direction.ClatNS, 1 / 1_000_000.0},
		{direction.ClatUS, 1 / 1_000.0},
		{direction.ClatMS, 1},
	}
	for _, candidate := range candidates {
		if !candidate.clat.hasData() {
			continue
		}
		stats := fioLatencyStats{}
		stats.AvgMS, stats.AvgOK = fioLatencyValue(candidate.clat.Mean, candidate.factor)
		stats.MaxMS, stats.MaxOK = fioLatencyValue(candidate.clat.Max, candidate.factor)
		if value, ok := fioPercentileValue(candidate.clat.Percentile, 95); ok {
			stats.P95MS, stats.P95OK = fioLatencyValue(value, candidate.factor)
		}
		if value, ok := fioPercentileValue(candidate.clat.Percentile, 99); ok {
			stats.P99MS, stats.P99OK = fioLatencyValue(value, candidate.factor)
		}
		return stats, true
	}
	return fioLatencyStats{}, false
}

func (c fioClat) hasData() bool {
	return c.Mean > 0 || c.Max > 0 || len(c.Percentile) > 0
}

func fioLatencyValue(value, factor float64) (float64, bool) {
	if !isPositiveFinite(value) || !isPositiveFinite(factor) {
		return 0, false
	}
	converted := value * factor
	if !isPositiveFinite(converted) {
		return 0, false
	}
	return converted, true
}

func fioP95Milliseconds(direction fioDirection) float64 {
	if stats, ok := fioLatencyStatsFor(direction); ok && stats.P95OK {
		return stats.P95MS
	}
	return 0
}

func fioPercentileValue(values map[string]float64, target float64) (float64, bool) {
	for key, value := range values {
		percentile, err := strconv.ParseFloat(strings.TrimSpace(key), 64)
		if err != nil || percentile != target || !isPositiveFinite(value) {
			continue
		}
		return value, true
	}
	return 0, false
}

func isPositiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
