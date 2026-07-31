package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func runFIODisk(ctx context.Context, env Environment, fioPath string) (result model.Result) {
	start := time.Now()
	result = model.NewResult("disk", "磁盘性能")
	result.Description = "fio Direct I/O 的顺序吞吐与 4K 随机 QD32"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "fio",
		Profile:         "Direct I/O seq 1 MiB QD1 + random 4 KiB QD32",
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
	ioEngine := "psync"
	if runtime.GOOS == "linux" {
		ioEngine = "libaio"
	}
	args := fioArguments(tempName, actualBytes, duration, ioEngine)
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
			Key: "fio_random_read_4k_iops", Label: "fio 4K 随机读 QD32",
			Value: randomRead, Unit: "IOPS", Display: model.FormatRate(randomRead, "IOPS"),
			Method: "fio-direct-4KiB-randread-qd32-v1", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: "fio_random_write_4k_iops", Label: "fio 4K 随机写 QD32",
			Value: randomWrite, Unit: "IOPS", Display: model.FormatRate(randomWrite, "IOPS"),
			Method: "fio-direct-4KiB-randwrite-qd32-v1", HigherIsBetter: model.BoolPtr(true),
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
	result.Fields = []model.Field{
		{Key: "engine", Label: "引擎", Value: "fio"},
		{Key: "version", Label: "fio 版本", Value: fallback(output.Version, "unknown")},
		{Key: "binary_sha256", Label: "fio SHA-256", Value: fallback(binarySHA256(fioPath), "unavailable")},
		{Key: "path", Label: "测试目录", Value: diskPath},
		{Key: "mount", Label: "挂载点", Value: fallback(disk.DiskMount, diskPath)},
		{Key: "file_size", Label: "临时文件", Value: model.FormatBytes(uint64(actualBytes))},
		{Key: "free_before", Label: "测试前可用", Value: model.FormatBytes(disk.DiskFree)},
		{Key: "direct_io", Label: "Direct I/O", Value: "1"},
		{Key: "ioengine", Label: "ioengine", Value: ioEngine},
		{Key: "job_duration", Label: "每项计时", Value: duration.String()},
		{Key: "arguments", Label: "命令参数", Value: strings.Join(fioArguments("<tempfile>", actualBytes, duration, ioEngine), " ")},
	}
	result.Sources = []model.Source{
		{Name: "fio", URL: "https://github.com/axboe/fio", Purpose: "Direct I/O 磁盘工作负载与 JSON 统计"},
	}
	result.Notes = append(result.Notes,
		"fio 必须由用户预先安装；ecs 不会调用包管理器或下载二进制。",
		"四项作业使用 stonewall 串行执行，避免顺序与随机负载相互干扰。",
		"仅比较相同 fio/ecs 版本、文件大小、ioengine、块大小、队列深度与计时时长的结果。",
	)
	result.Summary = fmt.Sprintf("fio 写 %s · 读 %s · 4K 读/写 %s/%s",
		model.FormatRate(seqWrite, "MiB/s"),
		model.FormatRate(seqRead, "MiB/s"),
		model.FormatRate(randomRead, "IOPS"),
		model.FormatRate(randomWrite, "IOPS"),
	)
	result.Finish(start)
	return result
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
	actualBytes := int64(env.Config.DiskMiB) * 1024 * 1024
	if disk.DiskFree > 0 {
		safeLimit := int64(disk.DiskFree / 5)
		if actualBytes > safeLimit {
			actualBytes = safeLimit
		}
	}
	const alignment = int64(4 * 1024 * 1024)
	actualBytes = actualBytes / alignment * alignment
	if actualBytes < 16*1024*1024 {
		return "", 0, disk, fmt.Errorf("测试盘安全余量不足 16 MiB")
	}
	return diskPath, actualBytes, disk, nil
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

func fioArguments(filename string, size int64, duration time.Duration, ioEngine string) []string {
	common := []string{
		"--filename=" + filename,
		"--size=" + strconv.FormatInt(size, 10),
		"--direct=1",
		"--ioengine=" + ioEngine,
		"--thread=1",
		"--numjobs=1",
		"--runtime=" + strconv.Itoa(int(duration/time.Second)),
		"--time_based=1",
		"--randrepeat=1",
		"--invalidate=1",
		"--group_reporting=1",
	}
	args := []string{"--output-format=json", "--eta=never"}
	addJob := func(name, rw, blockSize string, depth int, stonewall bool, extra ...string) {
		args = append(args, "--name="+name)
		if stonewall {
			args = append(args, "--stonewall=1")
		}
		args = append(args, common...)
		args = append(args,
			"--rw="+rw,
			"--bs="+blockSize,
			"--iodepth="+strconv.Itoa(depth),
		)
		args = append(args, extra...)
	}
	addJob("seqwrite", "write", "1m", 1, false, "--end_fsync=1")
	addJob("seqread", "read", "1m", 1, true)
	addJob("randread", "randread", "4k", 32, true)
	addJob("randwrite", "randwrite", "4k", 32, true, "--end_fsync=1")
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
