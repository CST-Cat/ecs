package probe

import (
	"context"
	"math"
	"os"
	"regexp"
	"strconv"
	"time"
)

// ICMP 延迟适配器。
//
// Go 标准库无法在不引入第三方包的情况下开非特权 ICMP socket，而本项目坚持核心
// 零第三方依赖，因此沿用既有做法：把 ECS_TOOL_BIN 私有 staging 目录中的 ping
// 当作可关闭的外部适配器调用，FreeBSD 则固定调用其由操作系统管理的
// /sbin/ping base-system 工具；参数以数组传入、不经过 shell，并记录实际使用的命令。
//
// ping 不可用、被容器裁掉或被防火墙拦截时一律返回不可用，由调用方降级到 TCP
// 建连延迟并在报告里说明原因，绝不用 TCP 数字冒充 ICMP。

var (
	pingLossPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%\s+packet loss`)
	// Linux iputils 和 FreeBSD base ping 的统计行都是 min/avg/max/stddev 四段；
	// busybox ping（Alpine 等精简镜像的默认实现）只有 min/avg/max 三段。四段优先，
	// 匹配不上再退到三段。
	pingRTTPattern      = regexp.MustCompile(`=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+)\s*ms`)
	pingRTTThreePattern = regexp.MustCompile(`=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)\s*ms`)
)

// icmpStats 是一次 ICMP 探测的统计结果。
type icmpStats struct {
	Available   bool
	LossKnown   bool
	LossPercent float64
	RTTKnown    bool
	MinMS       float64
	AvgMS       float64
	MaxMS       float64
	StdDevMS    float64
	// StdDevKnown 区分"标准差为 0"和"这个 ping 实现没有报告标准差"。
	StdDevKnown bool
	Err         error
}

func runICMPPingFamily(ctx context.Context, host string, count int, timeout time.Duration, family string) icmpStats {
	stats := icmpStats{}
	path, err := icmpPingPath()
	if err != nil {
		stats.Err = err
		return stats
	}
	// 给足够余量：count 个包各等 timeout，再加上进程启动与统计输出。
	budget := time.Duration(count)*timeout + 5*time.Second
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	command := newProbeCommand(runCtx, path, pingArgumentsForFamily(host, count, timeout, family)...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	run := command.RunCombined(probeCommandCombinedLimit)
	text := sanitizeCommandOutput(run.Combined)

	stats = parseICMPOutput(text)
	// ping 在有丢包时返回非零退出码，但统计行依然有效，所以先解析再判错。
	if !stats.Available {
		if run.Err != nil {
			stats.Err = run.Err
		}
	}
	return stats
}

func parseICMPOutput(text string) icmpStats {
	stats := icmpStats{}
	if match := pingLossPattern.FindStringSubmatch(text); len(match) == 2 {
		if loss, ok := parsePingFloat(match[1]); ok && loss <= 100 {
			stats.LossPercent = loss
			stats.LossKnown = true
			stats.Available = true
		}
	}
	if match := pingRTTPattern.FindStringSubmatch(text); len(match) == 5 {
		if values, ok := parsePingFloats(match[1:]); ok {
			stats.MinMS, stats.AvgMS, stats.MaxMS, stats.StdDevMS = values[0], values[1], values[2], values[3]
			stats.StdDevKnown = true
			stats.RTTKnown = true
			stats.Available = true
		}
	} else if match := pingRTTThreePattern.FindStringSubmatch(text); len(match) == 4 {
		// busybox ping 不报告标准差，此处只能留空而不是填 0 冒充测量值。
		if values, ok := parsePingFloats(match[1:]); ok {
			stats.MinMS, stats.AvgMS, stats.MaxMS = values[0], values[1], values[2]
			stats.RTTKnown = true
			stats.Available = true
		}
	}
	return stats
}

func parsePingFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func parsePingFloats(values []string) ([]float64, bool) {
	parsed := make([]float64, len(values))
	for index, value := range values {
		item, ok := parsePingFloat(value)
		if !ok {
			return nil, false
		}
		parsed[index] = item
	}
	return parsed, true
}
