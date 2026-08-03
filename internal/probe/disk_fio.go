package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

type fioOutput struct {
	Version string   `json:"fio version"`
	Jobs    []fioJob `json:"jobs"`
}

type fioJob struct {
	Name  string       `json:"jobname"`
	Error int          `json:"error"`
	Read  fioDirection `json:"read"`
	Write fioDirection `json:"write"`
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
	Percentile map[string]float64 `json:"percentile"`
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
	start := time.Now()
	result = model.NewResult("disk", "磁盘性能")
	result.Description = "fio Direct I/O 的基础口径、Crystal 矩阵、ATTO 矩阵与 YABS 兼容补充矩阵"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "fio",
		Profile:         "Direct I/O legacy + Crystal RND4K/SEQ1M + ATTO 512B–64M + YABS mixed",
		ComparisonScope: "相同 fio/ecs 版本、文件系统、文件大小、ioengine、块大小、队列深度与时长",
	}

	diskPath, actualBytes, disk, err := prepareFIODiskPath(ctx, env)
	if err != nil {
		result.Fail(err)
		result.Finish(start)
		return result
	}

	file, err := os.CreateTemp(diskPath, ".ecs-fio-*")
	if err != nil {
		result.Fail(fmt.Errorf("创建 fio 临时文件: %w", err))
		result.Finish(start)
		return result
	}
	tempName := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(tempName)
		result.Fail(fmt.Errorf("关闭 fio 临时文件: %w", err))
		result.Finish(start)
		return result
	}
	defer func() {
		if err := os.Remove(tempName); err != nil && !os.IsNotExist(err) {
			result.Notes = append(result.Notes, "fio 临时文件清理失败: "+err.Error())
			if result.Status == model.StatusOK {
				result.Status = model.StatusWarning
			}
		}
	}()

	duration := fioJobDuration(env.Config.Profile)
	engine := detectFIOEngine(ctx, fioPath)
	plan := fioJobPlan(env.Config.Profile)
	args := fioArguments(tempName, actualBytes, duration, engine, plan)
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
		result.Fail(fmt.Errorf("fio 执行失败: %w", runErr))
		result.Finish(start)
		return result
	}
	if stdout.Len() > 4*1024*1024 {
		result.Fail(fmt.Errorf("fio JSON 超过 4 MiB 安全上限"))
		result.Finish(start)
		return result
	}

	var output fioOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		result.Fail(fmt.Errorf("解析 fio JSON: %w", err))
		result.Finish(start)
		return result
	}
	jobs := make(map[string]fioJob, len(output.Jobs))
	for _, job := range output.Jobs {
		jobs[job.Name] = job
		if job.Error != 0 {
			result.Status = model.StatusWarning
			result.Notes = append(result.Notes, fmt.Sprintf("fio 作业 %s 返回错误码 %d", job.Name, job.Error))
		}
	}

	seqWrite := fioBandwidthMiB(jobs["seqwrite"].Write)
	seqRead := fioBandwidthMiB(jobs["seqread"].Read)
	randomRead := jobs["randread"].Read.IOPS
	randomWrite := jobs["randwrite"].Write.IOPS
	if seqWrite <= 0 && seqRead <= 0 && randomRead <= 0 && randomWrite <= 0 {
		result.Fail(fmt.Errorf("fio JSON 未包含可用的磁盘统计"))
		result.Finish(start)
		return result
	}
	randDepth := engine.EffectiveDepth(32)
	result.Measurements = append(result.Measurements,
		model.Measurement{
			Key: "fio_sequential_write_mib_s", Label: "fio 顺序写入",
			Value: seqWrite, Unit: "MiB/s", Display: model.FormatRate(seqWrite, "MiB/s"),
			Method: "fio-direct-1MiB-write-qd1-v1", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: "fio_sequential_read_mib_s", Label: "fio 顺序读取",
			Value: seqRead, Unit: "MiB/s", Display: model.FormatRate(seqRead, "MiB/s"),
			Method: "fio-direct-1MiB-read-qd1-v1", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: "fio_random_read_4k_iops", Label: fmt.Sprintf("fio 4K 随机读 QD%d", randDepth),
			Value: randomRead, Unit: "IOPS", Display: model.FormatRate(randomRead, "IOPS"),
			Method: fmt.Sprintf("fio-direct-4KiB-randread-qd%d-v1", randDepth), HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: "fio_random_write_4k_iops", Label: fmt.Sprintf("fio 4K 随机写 QD%d", randDepth),
			Value: randomWrite, Unit: "IOPS", Display: model.FormatRate(randomWrite, "IOPS"),
			Method: fmt.Sprintf("fio-direct-4KiB-randwrite-qd%d-v1", randDepth), HigherIsBetter: model.BoolPtr(true),
		},
	)
	if p95 := fioP95Milliseconds(jobs["randread"].Read); p95 > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "fio_random_read_p95_ms", Label: "fio 4K 随机读延迟 P95",
			Value: p95, Unit: "ms", Display: fmt.Sprintf("%.3f ms", p95),
			Method: "fio-clat-p95-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if p95 := fioP95Milliseconds(jobs["randwrite"].Write); p95 > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "fio_random_write_p95_ms", Label: "fio 4K 随机写延迟 P95",
			Value: p95, Unit: "ms", Display: fmt.Sprintf("%.3f ms", p95),
			Method: "fio-clat-p95-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}

	mixDepth := engine.EffectiveDepth(64)
	mixTable := model.Table{
		Title:   fmt.Sprintf("50/50 混合随机读写 QD%d × 2 作业（YABS 兼容口径）", mixDepth),
		Columns: []string{"块大小", "读", "读 IOPS", "写", "写 IOPS", "合计"},
	}
	for _, job := range plan {
		if !job.Mixed() {
			continue
		}
		sample, ok := jobs[job.Name]
		if !ok {
			continue
		}
		readMiB := fioBandwidthMiB(sample.Read)
		writeMiB := fioBandwidthMiB(sample.Write)
		if readMiB <= 0 && writeMiB <= 0 {
			continue
		}
		mixTable.Rows = append(mixTable.Rows, []string{
			job.BlockSize,
			model.FormatRate(readMiB, "MiB/s"),
			model.FormatRate(sample.Read.IOPS, "IOPS"),
			model.FormatRate(writeMiB, "MiB/s"),
			model.FormatRate(sample.Write.IOPS, "IOPS"),
			model.FormatRate(readMiB+writeMiB, "MiB/s"),
		})
		method := fmt.Sprintf("fio-direct-%s-randrw50-qd%d-n2-v1", job.BlockSize, mixDepth)
		result.Measurements = append(result.Measurements,
			model.Measurement{
				Key:   fmt.Sprintf("fio_mixed_%s_read_mib_s", job.BlockSize),
				Label: fmt.Sprintf("混合 %s 读", job.BlockSize),
				Value: readMiB, Unit: "MiB/s", Display: model.FormatRate(readMiB, "MiB/s"),
				Method: method, HigherIsBetter: model.BoolPtr(true),
			},
			model.Measurement{
				Key:   fmt.Sprintf("fio_mixed_%s_write_mib_s", job.BlockSize),
				Label: fmt.Sprintf("混合 %s 写", job.BlockSize),
				Value: writeMiB, Unit: "MiB/s", Display: model.FormatRate(writeMiB, "MiB/s"),
				Method: method, HigherIsBetter: model.BoolPtr(true),
			},
		)
	}
	if len(mixTable.Rows) > 0 {
		result.Tables = append(result.Tables, mixTable)
	}
	if matrixJobsEnabled(env.Config.Profile) {
		appendCrystalMatrix(&result, jobs, engine)
		appendATTOMatrix(&result, jobs, engine)
	}

	if seqWrite <= 0 || seqRead <= 0 || randomRead <= 0 || randomWrite <= 0 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "一个或多个 fio 作业没有返回有效吞吐；请检查文件系统的 Direct I/O 与 ioengine 支持。")
	}
	requestedBytes := int64(env.Config.DiskMiB) * 1024 * 1024
	if actualBytes < requestedBytes {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, fmt.Sprintf(
			"为保留至少 80%% 空闲空间，fio 文件已从 %s 自动缩小为 %s。",
			model.FormatBytes(uint64(requestedBytes)),
			model.FormatBytes(uint64(actualBytes)),
		))
	}
	if matrixJobsEnabled(env.Config.Profile) && actualBytes > requestedBytes {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"为安全容纳 ATTO 64 MiB 作业，fio 文件从配置的 %s 对齐/扩展为 %s（至少两个 64 MiB 窗口）。",
			model.FormatBytes(uint64(requestedBytes)), model.FormatBytes(uint64(actualBytes)),
		))
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "引擎", Value: "fio"},
		{Key: "version", Label: "fio 版本", Value: fallback(output.Version, "unknown")},
		{Key: "binary_sha256", Label: "fio SHA-256", Value: fallback(binarySHA256(fioPath), "unavailable")},
		{Key: "disk_device", Label: "测试设备", Value: fallback(disk.DiskDevice, "unavailable")},
		{Key: "path", Label: "测试目录", Value: diskPath, Sensitive: true},
		{Key: "mount", Label: "挂载点", Value: fallback(disk.DiskMount, diskPath)},
		{Key: "disk_total", Label: "磁盘总量", Value: model.FormatBytes(disk.DiskTotal)},
		{Key: "disk_used", Label: "磁盘已用", Value: model.FormatBytes(disk.DiskUsed)},
		{Key: "disk_available", Label: "磁盘可用", Value: model.FormatBytes(disk.DiskFree)},
		{Key: "disk_usage_percent", Label: "磁盘使用率", Value: fmt.Sprintf("%.1f %%", disk.DiskUsage)},
		{Key: "file_size", Label: "临时文件", Value: model.FormatBytes(uint64(actualBytes))},
		{Key: "free_before", Label: "测试前可用", Value: model.FormatBytes(disk.DiskFree)},
		{Key: "direct_io", Label: "Direct I/O", Value: "1"},
		{Key: "ioengine", Label: "ioengine", Value: describeFIOEngine(engine)},
		{Key: "jobs", Label: "作业数", Value: strconv.Itoa(len(plan))},
		{Key: "job_duration", Label: "每项计时", Value: duration.String()},
		{Key: "arguments", Label: "命令参数", Value: strings.Join(fioArguments("<tempfile>", actualBytes, duration, engine, plan), " ")},
	}
	result.Sources = []model.Source{
		{Name: "fio", URL: "https://github.com/axboe/fio", Purpose: "Direct I/O 磁盘工作负载与 JSON 统计"},
		{Name: "YABS", URL: "https://github.com/masonr/yet-another-bench-script", Purpose: "50/50 混合随机读写矩阵的块大小与队列深度口径"},
	}
	result.Notes = append(result.Notes,
		"fio 可由用户预先安装，也可由 run.sh 从系统包管理器临时准备；ecs 不下载未经校验的裸二进制。",
		fmt.Sprintf("%d 项作业使用 stonewall 串行执行，避免顺序与随机负载相互干扰。", len(plan)),
		"仅比较相同 fio/ecs 版本、文件大小、ioengine、块大小、队列深度与计时时长的结果。",
	)
	if matrixJobsEnabled(env.Config.Profile) {
		result.Notes = append(result.Notes,
			"混合、Crystal 与 ATTO 的吞吐和 IOPS 先按各自矩阵组内平均，再以等权子组参与磁盘分：legacy、混合、Crystal、ATTO 各占四分之一；缺失单元不补零。",
			"ATTO 使用完整块大小清单 512B、1K、2K、4K、8K、16K、32K、64K、128K、256K、512K、1M、2M、4M、8M、16M、32M、64M；不包含未请求的 5M。",
		)
	}
	if !engine.AsyncQueue {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"当前 ioengine 为 %s（同步），队列深度对它无效；所有随机项按实际生效的 QD1 标注，不能与 libaio/io_uring 的高队列深度成绩比较。",
			engine.Name,
		))
	}
	if !engine.Detected {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "未能通过 fio --enghelp 确认可用 ioengine，已退回 psync；成绩仍然有效但队列深度受限。")
	}
	result.Summary = fmt.Sprintf("fio 写 %s · 读 %s · 4K 读/写 %s/%s",
		model.FormatRate(seqWrite, "MiB/s"),
		model.FormatRate(seqRead, "MiB/s"),
		model.FormatRate(randomRead, "IOPS"),
		model.FormatRate(randomWrite, "IOPS"),
	)
	result.Finish(start)
	return result
}

type matrixCell struct {
	ReadMiB, ReadIOPS   float64
	WriteMiB, WriteIOPS float64
	ReadMethod          string
	WriteMethod         string
}

func appendCrystalMatrix(result *model.Result, jobs map[string]fioJob, engine fioEngine) {
	specs := crystalJobSpecs()
	cells := make(map[string]matrixCell, 4)
	missing := 0
	for _, spec := range specs {
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
		if throughput <= 0 && direction.IOPS <= 0 {
			missing++
			continue
		}
		actualDepth := engine.EffectiveDepth(spec.IODepth)
		workloadID := strings.ToLower(strings.ReplaceAll(spec.Workload, "/", "-"))
		method := fmt.Sprintf("fio-direct-crystal-%s-%s-qd%d-v1", workloadID, spec.Direction, actualDepth)
		cell := cells[spec.Workload]
		if spec.Direction == "read" {
			cell.ReadMiB, cell.ReadIOPS, cell.ReadMethod = throughput, direction.IOPS, method
			appendFioMatrixMeasurements(result, "crystal", crystalMetricStem(spec.Workload), "read", throughput, direction.IOPS, method)
		} else {
			cell.WriteMiB, cell.WriteIOPS, cell.WriteMethod = throughput, direction.IOPS, method
			appendFioMatrixMeasurements(result, "crystal", crystalMetricStem(spec.Workload), "write", throughput, direction.IOPS, method)
		}
		cells[spec.Workload] = cell
	}

	table := model.Table{
		Title:                 "Crystal",
		Columns:               []string{"工作负载", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
		NumericColumns:        []int{1, 2, 3, 4},
		NumericHigherIsBetter: []bool{true, true, true, true},
	}
	for _, workload := range []string{"RND4K/Q1", "RND4K/Q32", "SEQ1M/Q1", "SEQ1M/Q8"} {
		cell := cells[workload]
		status := "完成"
		if cell.ReadMiB <= 0 && cell.ReadIOPS <= 0 || cell.WriteMiB <= 0 && cell.WriteIOPS <= 0 {
			status = "未返回"
		}
		table.Rows = append(table.Rows, []string{
			workload,
			formatMatrixRate(cell.ReadMiB, "MiB/s"), formatMatrixRate(cell.ReadIOPS, "IOPS"),
			formatMatrixRate(cell.WriteMiB, "MiB/s"), formatMatrixRate(cell.WriteIOPS, "IOPS"), status,
		})
	}
	result.Tables = append(result.Tables, table)
	if missing > 0 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, fmt.Sprintf("Crystal 矩阵有 %d 个读写单元未返回有效吞吐或 IOPS；缺失项保留为未返回。", missing))
	}
}

func appendATTOMatrix(result *model.Result, jobs map[string]fioJob, engine fioEngine) {
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
			if throughput <= 0 && direction.IOPS <= 0 {
				missing++
				continue
			}
			method := fmt.Sprintf("fio-direct-atto-%s-%s-qd%d-v1", block.FIO, directionName, engine.EffectiveDepth(1))
			cell := cells[block.Label]
			if directionName == "read" {
				cell.ReadMiB, cell.ReadIOPS, cell.ReadMethod = throughput, direction.IOPS, method
				appendFioMatrixMeasurements(result, "atto", "atto_"+block.FIO, "read", throughput, direction.IOPS, method)
			} else {
				cell.WriteMiB, cell.WriteIOPS, cell.WriteMethod = throughput, direction.IOPS, method
				appendFioMatrixMeasurements(result, "atto", "atto_"+block.FIO, "write", throughput, direction.IOPS, method)
			}
			cells[block.Label] = cell
		}
	}

	table := model.Table{
		Title:                 "ATTO",
		Columns:               []string{"块大小", "读吞吐", "读 IOPS", "写吞吐", "写 IOPS", "状态"},
		NumericColumns:        []int{1, 2, 3, 4},
		NumericHigherIsBetter: []bool{true, true, true, true},
	}
	for _, block := range attoBlockSizes {
		cell := cells[block.Label]
		status := "完成"
		if cell.ReadMiB <= 0 && cell.ReadIOPS <= 0 || cell.WriteMiB <= 0 && cell.WriteIOPS <= 0 {
			status = "未返回"
		}
		table.Rows = append(table.Rows, []string{
			block.Label,
			formatMatrixRate(cell.ReadMiB, "MiB/s"), formatMatrixRate(cell.ReadIOPS, "IOPS"),
			formatMatrixRate(cell.WriteMiB, "MiB/s"), formatMatrixRate(cell.WriteIOPS, "IOPS"), status,
		})
	}
	result.Tables = append(result.Tables, table)
	if missing > 0 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, fmt.Sprintf("ATTO 矩阵有 %d 个读写单元未返回有效吞吐或 IOPS；缺失项保留为未返回。", missing))
	}
}

func appendFioMatrixMeasurements(result *model.Result, matrix, stem, direction string, throughput, iops float64, method string) {
	if throughput > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: stem + "_" + direction + "_mib_s", Label: matrix + " " + stem + " " + direction + " 吞吐",
			Value: throughput, Unit: "MiB/s", Display: model.FormatRate(throughput, "MiB/s"), Method: method,
			HigherIsBetter: model.BoolPtr(true),
		})
	}
	if iops > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: stem + "_" + direction + "_iops", Label: matrix + " " + stem + " " + direction + " IOPS",
			Value: iops, Unit: "IOPS", Display: model.FormatRate(iops, "IOPS"), Method: method,
			HigherIsBetter: model.BoolPtr(true),
		})
	}
}

func crystalMetricStem(workload string) string {
	return "crystal_" + strings.ToLower(strings.ReplaceAll(workload, "/", "_"))
}

func formatMatrixRate(value float64, unit string) string {
	if value <= 0 {
		return "—"
	}
	return model.FormatRate(value, unit)
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
	actualBytes, err := fioDiskSize(uint64(env.Config.DiskMiB)*1024*1024, disk.DiskFree, matrixJobsEnabled(env.Config.Profile))
	if err != nil {
		return "", 0, disk, err
	}
	return diskPath, actualBytes, disk, nil
}

func fioDiskSize(requestedBytes, freeBytes uint64, matrix bool) (int64, error) {
	actualBytes := int64(requestedBytes)
	if freeBytes > 0 {
		safeLimit := int64(freeBytes / 5)
		if actualBytes > safeLimit {
			actualBytes = safeLimit
		}
	}
	alignment := int64(4 * 1024 * 1024)
	minimum := int64(16 * 1024 * 1024)
	if matrix {
		// The largest ATTO job is 64 MiB.  Keep two aligned 64 MiB windows so
		// fio can place a full block without edge truncation or an accidental
		// single-window cache effect on small VPS disks.
		alignment = 64 * 1024 * 1024
		minimum = 128 * 1024 * 1024
		// A caller may configure a smaller legacy file size.  Expand it to the
		// matrix minimum when the 20% free-space safety limit permits; otherwise
		// refusing the run is safer than silently dropping ATTO cells.
		if actualBytes < minimum {
			if freeBytes > 0 && int64(freeBytes/5) < minimum {
				return 0, fmt.Errorf("测试盘安全余量不足 %s（ATTO 最大块为 64 MiB）", model.FormatBytes(uint64(minimum)))
			}
			actualBytes = minimum
		}
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

func fioJobDuration(profile string) time.Duration {
	switch profile {
	case "quick":
		return 2 * time.Second
	case "full":
		return 10 * time.Second
	default:
		return 5 * time.Second
	}
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
}

// Mixed 表示这是一个 50/50 混合随机读写作业。
func (s fioJobSpec) Mixed() bool { return s.RW == "randrw" }

// fioJobPlan 给出本次磁盘测试的作业集合。
//
// 前四项是 ecs 既有口径：1 MiB 顺序读写反映带宽上限，4 KiB 随机读写反映 IOPS。
// 后面是 YABS 兼容矩阵：4k/64k/512k/1m 四档 50/50 混合随机读写，iodepth=64、
// numjobs=2，这是社区里流传最广、样本量最大的磁盘口径，补上它才能和主流测评
// 贴的数字对得上。quick 档只跑首尾两档以控制时长。
func fioJobPlan(profile string) []fioJobSpec {
	plan := []fioJobSpec{
		{Name: "seqwrite", RW: "write", BlockSize: "1m", IODepth: 1, NumJobs: 1, EndFsync: true},
		{Name: "seqread", RW: "read", BlockSize: "1m", IODepth: 1, NumJobs: 1},
		{Name: "randread", RW: "randread", BlockSize: "4k", IODepth: 32, NumJobs: 1},
		{Name: "randwrite", RW: "randwrite", BlockSize: "4k", IODepth: 32, NumJobs: 1, EndFsync: true},
	}
	blocks := []string{"4k", "64k", "512k", "1m"}
	if profile == "quick" {
		blocks = []string{"4k", "1m"}
	}
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
	if matrixJobsEnabled(profile) {
		plan = append(plan, crystalJobSpecs()...)
		plan = append(plan, attoJobSpecs()...)
	}
	return plan
}

func matrixJobsEnabled(profile string) bool {
	// quick retains the established small workload and output shape.  The
	// standard and full profiles carry the complete reporting expansion; full
	// also supplies the stable 10-second fio duration.
	return profile != "quick"
}

func crystalJobSpecs() []fioJobSpec {
	return []fioJobSpec{
		{Name: "crystal_read_rnd4k_q1", RW: "randread", BlockSize: "4k", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q1", Direction: "read"},
		{Name: "crystal_write_rnd4k_q1", RW: "randwrite", BlockSize: "4k", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q1", Direction: "write", EndFsync: true},
		{Name: "crystal_read_rnd4k_q32", RW: "randread", BlockSize: "4k", IODepth: 32, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q32", Direction: "read"},
		{Name: "crystal_write_rnd4k_q32", RW: "randwrite", BlockSize: "4k", IODepth: 32, NumJobs: 1, Matrix: "crystal", Workload: "RND4K/Q32", Direction: "write", EndFsync: true},
		{Name: "crystal_read_seq1m_q1", RW: "read", BlockSize: "1m", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q1", Direction: "read"},
		{Name: "crystal_write_seq1m_q1", RW: "write", BlockSize: "1m", IODepth: 1, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q1", Direction: "write", EndFsync: true},
		{Name: "crystal_read_seq1m_q8", RW: "read", BlockSize: "1m", IODepth: 8, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q8", Direction: "read"},
		{Name: "crystal_write_seq1m_q8", RW: "write", BlockSize: "1m", IODepth: 8, NumJobs: 1, Matrix: "crystal", Workload: "SEQ1M/Q8", Direction: "write", EndFsync: true},
	}
}

var attoBlockSizes = []struct {
	FIO   string
	Label string
}{
	{"512b", "512B"}, {"1k", "1K"}, {"2k", "2K"}, {"4k", "4K"}, {"8k", "8K"},
	{"16k", "16K"}, {"32k", "32K"}, {"64k", "64K"}, {"128k", "128K"}, {"256k", "256K"},
	{"512k", "512K"}, {"1m", "1M"}, {"2m", "2M"}, {"4m", "4M"}, {"8m", "8M"},
	{"16m", "16M"}, {"32m", "32M"}, {"64m", "64M"},
}

func attoJobSpecs() []fioJobSpec {
	jobs := make([]fioJobSpec, 0, len(attoBlockSizes)*2)
	for _, block := range attoBlockSizes {
		jobs = append(jobs,
			fioJobSpec{Name: "atto_read_" + block.FIO, RW: "read", BlockSize: block.FIO, IODepth: 1, NumJobs: 1, Matrix: "atto", Workload: block.Label, Direction: "read"},
			fioJobSpec{Name: "atto_write_" + block.FIO, RW: "write", BlockSize: block.FIO, IODepth: 1, NumJobs: 1, Matrix: "atto", Workload: block.Label, Direction: "write", EndFsync: true},
		)
	}
	return jobs
}

func fioArguments(filename string, size int64, duration time.Duration, engine fioEngine, plan []fioJobSpec) []string {
	args := []string{"--output-format=json", "--eta=never"}
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
			"--runtime="+strconv.Itoa(int(duration/time.Second)),
			"--time_based=1",
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
	if direction.BWBytes > 0 {
		return direction.BWBytes / 1024 / 1024
	}
	if direction.BW > 0 {
		// fio documents bw as KiB/s in JSON output.
		return direction.BW / 1024
	}
	return 0
}

func fioP95Milliseconds(direction fioDirection) float64 {
	if value := fioPercentile(direction.ClatNS.Percentile, "95."); value > 0 {
		return value / 1_000_000
	}
	if value := fioPercentile(direction.ClatUS.Percentile, "95."); value > 0 {
		return value / 1_000
	}
	if value := fioPercentile(direction.ClatMS.Percentile, "95."); value > 0 {
		return value
	}
	return 0
}

func fioPercentile(values map[string]float64, prefix string) float64 {
	for key, value := range values {
		if strings.HasPrefix(key, prefix) {
			return value
		}
	}
	return 0
}
