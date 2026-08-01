package probe

import (
	"context"
	"os/exec"
	"time"

	"ecs/internal/model"
)

type diskProbe struct{}

func (diskProbe) ID() string         { return "disk" }
func (diskProbe) Title() string      { return "磁盘性能" }
func (diskProbe) NeedsNetwork() bool { return false }

func (diskProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	var result model.Result
	if path, err := exec.LookPath("fio"); err == nil {
		result = runFIODisk(ctx, env, path)
	} else {
		result = model.NewResult("disk", "磁盘性能")
		result.Methodology = model.Methodology{
			Kind:            "standard-benchmark",
			Label:           "标准基准",
			Engine:          "fio",
			Profile:         "Direct I/O seq 1 MiB QD1 + random 4 KiB QD32",
			ComparisonScope: "相同 fio/ecs 版本、文件系统、文件大小、ioengine、块大小、队列深度与时长",
		}
		result.Status = model.StatusWarning
		result.Summary = "未找到 fio，标准磁盘基准未运行"
		result.Notes = append(result.Notes, "可用 run.sh 自动临时准备 fio，或运行 install.sh --with-benchmarks 持久安装。ecs 不提供缓存 I/O 或自研替代分数。")
	}

	// 多挂载盘默认关闭：多跑一块盘就多一份写入与时间，应由用户显式要求。
	if env.Config.DiskMulti {
		if fioPath, err := exec.LookPath("fio"); err == nil {
			appendMultiDiskResults(ctx, &result, env, fioPath)
		}
	}
	// 延迟与介质健康是补充口径，缺席不降级整个模块。
	appendIOPingLatency(ctx, &result, env.Config.DiskPath)
	appendSMARTHealth(ctx, &result, env.Config.DiskPath)
	result.Finish(start)
	return result
}
