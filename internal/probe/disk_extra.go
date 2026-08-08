package probe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"ecs/internal/model"
)

// 磁盘的补充口径：I/O 延迟（ioping）。
//
// fio 测的是吞吐与队列深度下的 IOPS，回答"能跑多快"；ioping 用单个请求测
// 空载延迟，回答"一次 I/O 要等多久"——存储是否被邻居拖累、是否走网络存储，
// 延迟比吞吐更敏感，可以回答一次 I/O 要等多久。
//
// ioping 是 Debian 官方软件包，与 fio 一样只作为可关闭的外部适配器调用。

var (
	// min/avg/max/mdev = 448.8 us / 755.4 us / 2.13 ms / 508.1 us
	ioPingLatencyPattern = regexp.MustCompile(
		`min/avg/max/mdev\s*=\s*([0-9.]+)\s*(ns|us|ms|s)\s*/\s*([0-9.]+)\s*(ns|us|ms|s)\s*/\s*([0-9.]+)\s*(ns|us|ms|s)\s*/\s*([0-9.]+)\s*(ns|us|ms|s)`)
	// --- . (ext4 /dev/nvme0n1p2 443.2 GiB) ioping statistics ---
	ioPingTargetPattern = regexp.MustCompile(`---\s+.*?\(([a-z0-9]+)\s+(\S+)\s+[^)]*\)\s+ioping statistics`)
)

// parseIOPingDuration 把 ioping 的带单位数值折算成毫秒。
//
// ioping 会在同一行里混用单位（实测出现过 "448.8 us / ... / 2.13 ms"），
// 按固定单位解析必然出错。
func parseIOPingDuration(value, unit string) (float64, bool) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case "ns":
		return number / 1e6, true
	case "us":
		return number / 1e3, true
	case "ms":
		return number, true
	case "s":
		return number * 1e3, true
	default:
		return 0, false
	}
}

// ioPingResult 是一次 ioping 探测的延迟统计，单位统一为毫秒。
type ioPingResult struct {
	MinMS      float64
	AvgMS      float64
	MaxMS      float64
	MdevMS     float64
	Filesystem string
	Device     string
}

// parseIOPingOutput 解析 ioping 的统计段。
func parseIOPingOutput(text string) (ioPingResult, bool) {
	var result ioPingResult
	if match := ioPingTargetPattern.FindStringSubmatch(text); len(match) == 3 {
		result.Filesystem = match[1]
		result.Device = match[2]
	}
	match := ioPingLatencyPattern.FindStringSubmatch(text)
	if len(match) != 9 {
		return result, false
	}
	values := make([]float64, 0, 4)
	for i := 1; i < 9; i += 2 {
		value, ok := parseIOPingDuration(match[i], match[i+1])
		if !ok {
			return result, false
		}
		values = append(values, value)
	}
	result.MinMS, result.AvgMS, result.MaxMS, result.MdevMS = values[0], values[1], values[2], values[3]
	return result, true
}

// appendIOPingLatency 在磁盘结果上追加空载 I/O 延迟。
func appendIOPingLatency(ctx context.Context, result *model.Result, diskPath string) {
	path, err := exec.LookPath("ioping")
	if err != nil {
		result.Notes = append(result.Notes,
			"未安装 ioping，未测量空载 I/O 延迟；fio 的吞吐与 IOPS 不受影响。")
		return
	}
	appendToolVersion(ctx, result, "ioping_version", "ioping 版本", path)
	result.Fields = append(result.Fields, model.Field{
		Key: "ioping_binary_sha256", Label: "ioping SHA-256", Value: fallback(binarySHA256(path), "unavailable"),
	})
	// -D 走 Direct I/O，与 fio 的 direct=1 同口径，避免测到页缓存。
	const requests = 20
	args := []string{"-D", "-c", strconv.Itoa(requests), "-q", diskPath}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, runErr := command.CombinedOutput()
	text := sanitizeCommandOutput(output)
	if runCtx.Err() != nil {
		result.Notes = append(result.Notes, "ioping 延迟测试超时，本项未产出。")
		return
	}
	if runErr != nil {
		result.Notes = append(result.Notes, "ioping 延迟测试失败，详见下方失败原因。")
		result.Fields = append(result.Fields, model.Field{
			Key: "ioping_error", Label: "ioping 失败原因", Value: tailText(text, 200),
		})
		return
	}
	sample, ok := parseIOPingOutput(text)
	if !ok {
		result.Notes = append(result.Notes, "ioping 输出未解析到延迟统计，本项未产出。")
		return
	}

	result.Measurements = append(result.Measurements,
		model.Measurement{
			Key: "ioping_latency_avg_ms", Label: "I/O 延迟均值",
			Value: sample.AvgMS, Unit: "ms", Display: fmt.Sprintf("%.3f ms", sample.AvgMS),
			Method: fmt.Sprintf("ioping-direct-4KiB-c%d-v1", requests), HigherIsBetter: model.BoolPtr(false),
		},
		model.Measurement{
			Key: "ioping_latency_max_ms", Label: "I/O 延迟最大",
			Value: sample.MaxMS, Unit: "ms", Display: fmt.Sprintf("%.3f ms", sample.MaxMS),
			Method: fmt.Sprintf("ioping-direct-4KiB-c%d-v1", requests), HigherIsBetter: model.BoolPtr(false),
		},
		model.Measurement{
			Key: "ioping_latency_mdev_ms", Label: "I/O 延迟抖动",
			Value: sample.MdevMS, Unit: "ms", Display: fmt.Sprintf("%.3f ms", sample.MdevMS),
			Method: fmt.Sprintf("ioping-direct-4KiB-c%d-v1", requests), HigherIsBetter: model.BoolPtr(false),
		},
	)
	if sample.Filesystem != "" {
		result.Fields = append(result.Fields,
			model.Field{Key: "filesystem", Label: "文件系统", Value: sample.Filesystem})
	}
	result.Sources = append(result.Sources, model.Source{
		Name: "ioping", URL: "https://github.com/koct9i/ioping", Purpose: "单请求 Direct I/O 延迟测量",
	})
	result.Notes = append(result.Notes,
		"ioping 用单个 Direct I/O 请求测空载延迟，回答“一次 I/O 要等多久”；"+
			"fio 用队列深度压吞吐，两者不可互相替代。延迟抖动大通常说明存储被邻居争抢或走了网络存储。")
}
