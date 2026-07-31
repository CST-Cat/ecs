package probe

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
	// Linux 用 mdev、BSD/macOS 用 stddev，统计行前缀也不同，统一按四段数值匹配。
	pingRTTPattern = regexp.MustCompile(`=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+)\s*ms`)
)

// icmpStats 是一次 ICMP 探测的统计结果。
type icmpStats struct {
	Available   bool
	LossPercent float64
	MinMS       float64
	AvgMS       float64
	MaxMS       float64
	StdDevMS    float64
	Err         error
}

// icmpAvailable 报告系统是否提供 ping。
func icmpAvailable() bool {
	_, err := exec.LookPath(pingCommandName())
	return err == nil
}

func pingCommandName() string {
	if runtime.GOOS == "windows" {
		return "ping.exe"
	}
	return "ping"
}

// pingArguments 给出跨平台的 ping 参数。
//
// -n/-q 让输出保持数字化且只打印统计，避免逐包刷屏；超时参数在 Linux 是秒、
// 在 macOS/BSD 是毫秒、在 Windows 是毫秒，必须分别处理。
func pingArguments(host string, count int, timeout time.Duration) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"-n", strconv.Itoa(count), "-w", strconv.Itoa(int(timeout.Milliseconds())), host}
	case "darwin", "freebsd", "netbsd", "openbsd":
		return []string{"-n", "-q", "-c", strconv.Itoa(count), "-W", strconv.Itoa(int(timeout.Milliseconds())), host}
	default:
		seconds := int(timeout.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		return []string{"-n", "-q", "-c", strconv.Itoa(count), "-W", strconv.Itoa(seconds), host}
	}
}

// runICMPPing 对目标执行一次 ICMP 探测。
func runICMPPing(ctx context.Context, host string, count int, timeout time.Duration) icmpStats {
	stats := icmpStats{}
	path, err := exec.LookPath(pingCommandName())
	if err != nil {
		stats.Err = err
		return stats
	}
	// 给足够余量：count 个包各等 timeout，再加上进程启动与统计输出。
	budget := time.Duration(count)*timeout + 5*time.Second
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	command := exec.CommandContext(runCtx, path, pingArguments(host, count, timeout)...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, runErr := command.CombinedOutput()
	text := sanitizeCommandOutput(output)

	// ping 在有丢包时返回非零退出码，但统计行依然有效，所以先解析再判错。
	if match := pingLossPattern.FindStringSubmatch(text); len(match) == 2 {
		if loss, parseErr := strconv.ParseFloat(match[1], 64); parseErr == nil {
			stats.LossPercent = loss
			stats.Available = true
		}
	}
	if match := pingRTTPattern.FindStringSubmatch(text); len(match) == 5 {
		stats.MinMS = parseFloatDefault(match[1], 0)
		stats.AvgMS = parseFloatDefault(match[2], 0)
		stats.MaxMS = parseFloatDefault(match[3], 0)
		stats.StdDevMS = parseFloatDefault(match[4], 0)
		stats.Available = true
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

func parseFloatDefault(value string, defaultValue float64) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}
	return parsed
}
