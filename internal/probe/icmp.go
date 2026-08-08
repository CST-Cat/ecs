package probe

import (
	"context"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// ICMP 延迟适配器。
//
// Go 标准库无法在不引入第三方包的情况下开非特权 ICMP socket，而本项目坚持核心
// 零第三方依赖，因此沿用既有做法：把系统 ping 当作可关闭的外部适配器调用，
// 参数以数组传入、不经过 shell，并记录实际使用的命令。
//
// ping 不可用、被容器裁掉或被防火墙拦截时一律返回不可用，由调用方降级到 TCP
// 建连延迟并在报告里说明原因，绝不用 TCP 数字冒充 ICMP。

var (
	pingLossPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%\s+packet loss`)
	// iputils 的统计行是 min/avg/max/mdev 四段；busybox ping（Alpine 等精简镜像的
	// 默认实现）只有 min/avg/max 三段。四段优先，匹配不上再退到三段。
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

// icmpAvailable 报告系统是否提供 ping。
func icmpAvailable() bool {
	_, err := exec.LookPath(pingCommand)
	return err == nil
}

// pingCommand 是 Linux 上的 ICMP 探测程序。
const pingCommand = "ping"

// pingArguments 给出 ping 参数。
//
// -n 保持输出数字化、-q 只打印统计，避免逐包刷屏。iputils 与 busybox 的 -W 都以
// 秒为单位，且都不接受小数，因此不足一秒一律进位到 1 秒。
func pingArguments(host string, count int, timeout time.Duration) []string {
	return pingArgumentsForFamily(host, count, timeout, "")
}

func pingArgumentsForFamily(host string, count int, timeout time.Duration, family string) []string {
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	args := []string{"-n", "-q", "-c", strconv.Itoa(count), "-W", strconv.Itoa(seconds)}
	if family == "4" || family == "6" {
		args = append(args, "-"+family)
	}
	return append(args, host)
}

// runICMPPing 对目标执行一次 ICMP 探测。
func runICMPPing(ctx context.Context, host string, count int, timeout time.Duration) icmpStats {
	return runICMPPingFamily(ctx, host, count, timeout, "")
}

func runICMPPingFamily(ctx context.Context, host string, count int, timeout time.Duration, family string) icmpStats {
	stats := icmpStats{}
	path, err := exec.LookPath(pingCommand)
	if err != nil {
		stats.Err = err
		return stats
	}
	// 给足够余量：count 个包各等 timeout，再加上进程启动与统计输出。
	budget := time.Duration(count)*timeout + 5*time.Second
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	command := exec.CommandContext(runCtx, path, pingArgumentsForFamily(host, count, timeout, family)...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, runErr := command.CombinedOutput()
	text := sanitizeCommandOutput(output)

	// ping 在有丢包时返回非零退出码，但统计行依然有效，所以先解析再判错。
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
	if !stats.Available {
		if runCtx.Err() != nil {
			stats.Err = runCtx.Err()
		} else if runErr != nil {
			stats.Err = runErr
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
