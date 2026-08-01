package probe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

// mbw 内存带宽适配器。
//
// 为什么在 sysbench 之外还要 mbw：两者测的不是一回事。sysbench memory 是按块
// 反复读写同一片缓冲区的微基准，mbw 测的是 memcpy 在两个大数组之间搬运数据的
// 带宽——后者更接近"内存带宽"这个词的通行含义，也是 oneclickvirt 默认采用的口径。
// 两个数字并列保留，不合并、不取代。
//
// mbw 是 Debian 官方软件包（源自 ahorvath/mbw），与 sysbench、fio 一样只作为
// 可关闭的外部适配器调用，记录参数与程序 SHA-256。

var (
	// AVG	Method: MEMCPY	Elapsed: 0.00541	MiB: 64.00000	Copy: 11833.590 MiB/s
	mbwAveragePattern = regexp.MustCompile(`(?m)^AVG\s+Method:\s*(\S+)\s+Elapsed:\s*([0-9.]+)\s+MiB:\s*([0-9.]+)\s+Copy:\s*([0-9.]+)\s+MiB/s`)
)

// mbwSample 是 mbw 一种搬运方法的平均结果。
type mbwSample struct {
	Method  string
	RateMiB float64
}

// mbwArraySizeMiB 决定 mbw 使用的数组大小。
//
// mbw 会同时分配两个该尺寸的数组，因此峰值占用是两倍。在小内存 VPS 上贸然要
// 几百 MiB 会触发 OOM 或把机器压进 swap，测出来的也不再是内存带宽。这里按
// 可用内存的 1/8 取，并夹在 16–256 MiB 之间。
func mbwArraySizeMiB(availableBytes uint64) int {
	const (
		minimum = 16
		maximum = 256
	)
	if availableBytes == 0 {
		return minimum
	}
	size := int(availableBytes / 8 / (1024 * 1024))
	if size < minimum {
		return minimum
	}
	if size > maximum {
		return maximum
	}
	return size
}

// parseMBWOutput 提取每种搬运方法的平均带宽。
func parseMBWOutput(output string) []mbwSample {
	matches := mbwAveragePattern.FindAllStringSubmatch(output, -1)
	samples := make([]mbwSample, 0, len(matches))
	for _, match := range matches {
		if len(match) != 5 {
			continue
		}
		rate, err := strconv.ParseFloat(match[4], 64)
		if err != nil || rate <= 0 {
			continue
		}
		samples = append(samples, mbwSample{Method: match[1], RateMiB: rate})
	}
	return samples
}

// mbwMethodLabel 把 mbw 的方法名翻成可读说明。
func mbwMethodLabel(method string) string {
	switch strings.ToUpper(method) {
	case "MEMCPY":
		return "memcpy"
	case "DUMB":
		return "逐元素赋值"
	case "MCBLOCK":
		return "分块 memcpy"
	default:
		return method
	}
}

// appendMBWMemory 在已有内存结果上追加 mbw 的带宽指标。
//
// mbw 不存在时什么都不做——它是补充口径，不是必需项，缺席不应让整个模块降级。
func appendMBWMemory(ctx context.Context, result *model.Result, availableBytes uint64) {
	path, err := exec.LookPath("mbw")
	if err != nil {
		result.Notes = append(result.Notes,
			"未安装 mbw，内存带宽（memcpy 口径）未测量；sysbench 的顺序读写成绩不受影响。")
		return
	}
	sizeMiB := mbwArraySizeMiB(availableBytes)
	const runs = 5
	args := []string{"-q", "-n", strconv.Itoa(runs), strconv.Itoa(sizeMiB)}

	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, runErr := command.CombinedOutput()
	text := sanitizeCommandOutput(output)
	if runCtx.Err() != nil {
		result.Notes = append(result.Notes, "mbw 内存带宽测试超时，本项未产出。")
		return
	}
	if runErr != nil {
		result.Notes = append(result.Notes, "mbw 内存带宽测试失败："+tailText(text, 200))
		return
	}
	samples := parseMBWOutput(text)
	if len(samples) == 0 {
		result.Notes = append(result.Notes, "mbw 输出未解析到平均带宽，本项未产出。")
		return
	}

	for _, sample := range samples {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key:            "mbw_" + strings.ToLower(sample.Method) + "_mib_s",
			Label:          "内存带宽 " + mbwMethodLabel(sample.Method),
			Value:          sample.RateMiB,
			Unit:           "MiB/s",
			Display:        model.FormatRate(sample.RateMiB, "MiB/s"),
			Method:         fmt.Sprintf("mbw-%s-%dMiB-n%d-v1", strings.ToLower(sample.Method), sizeMiB, runs),
			HigherIsBetter: model.BoolPtr(true),
		})
	}
	result.Fields = append(result.Fields,
		model.Field{Key: "mbw_binary_sha256", Label: "mbw SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		model.Field{Key: "mbw_arguments", Label: "mbw 参数", Value: "mbw " + strings.Join(args, " ")},
	)
	result.TextBlocks = append(result.TextBlocks, model.TextBlock{
		Title: "mbw 原始输出", Language: "text", Content: text,
	})
	result.Sources = append(result.Sources, model.Source{
		Name: "mbw", URL: "http://ahorvath.web.cern.ch/ahorvath/mbw/", Purpose: "memcpy 口径的内存带宽测量",
	})
	result.Notes = append(result.Notes,
		fmt.Sprintf("mbw 用两个 %d MiB 数组做 memcpy，测的是搬运带宽；sysbench memory 反复读写同一缓冲区，"+
			"两者口径不同，数值不可互相替代，也不能合并成一个分数。", sizeMiB),
	)
}
