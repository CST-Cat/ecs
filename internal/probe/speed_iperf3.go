package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type iperfJSONOutput struct {
	Error string `json:"error"`
	Start struct {
		Connected []struct {
			LocalHost  string `json:"local_host"`
			RemoteHost string `json:"remote_host"`
		} `json:"connected"`
	} `json:"start"`
	End struct {
		SumSent     iperfJSONSum `json:"sum_sent"`
		SumReceived iperfJSONSum `json:"sum_received"`
	} `json:"end"`
}

type iperfJSONSum struct {
	Bytes         int64   `json:"bytes"`
	BitsPerSecond float64 `json:"bits_per_second"`
	Retransmits   int64   `json:"retransmits"`
	Seconds       float64 `json:"seconds"`
	// UDP 专有统计。
	JitterMS    float64 `json:"jitter_ms"`
	LostPackets int64   `json:"lost_packets"`
	Packets     int64   `json:"packets"`
	LostPercent float64 `json:"lost_percent"`
}

// udpResult 是一次 UDP 测试的丢包与抖动统计。
type udpResult struct {
	Available   bool
	JitterMS    float64
	LostPercent float64
	Packets     int64
	Mbps        float64
	Err         string
}

// runIPerfUDP 用固定码率的 UDP 流测量丢包与抖动。
//
// TCP 吞吐说明链路能跑多快，UDP 丢包和抖动说明链路稳不稳；实时音视频、游戏和
// VPN 的体验取决于后者。这里用适中的固定码率而不是压满带宽：压满时的丢包只能
// 说明拥塞，不能反映常态质量。
func runIPerfUDP(ctx context.Context, path, host string, port int, family string, bitrate string, seconds int) udpResult {
	args := []string{
		"-" + strings.TrimPrefix(family, "IPv"),
		"-c", host,
		"-p", strconv.Itoa(port),
		"-u",
		"-b", bitrate,
		"-t", strconv.Itoa(seconds),
		"-J",
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds+12)*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	result := udpResult{}
	if runCtx.Err() != nil {
		result.Err = runCtx.Err().Error()
		return result
	}
	if stdout.Len() > 4*1024*1024 {
		result.Err = "iperf3 UDP JSON 超过 4 MiB 安全上限"
		return result
	}
	var output iperfJSONOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		detail := tailText(sanitizeCommandOutput(stderr.Bytes()), 200)
		result.Err = fmt.Sprintf("解析 UDP JSON: %v: %s", decodeErr, detail)
		return result
	}
	if output.Error != "" {
		result.Err = output.Error
		return result
	}
	if err != nil && output.End.SumReceived.Packets == 0 {
		result.Err = fmt.Sprintf("%v: %s", err, tailText(sanitizeCommandOutput(stderr.Bytes()), 200))
		return result
	}
	// UDP 模式下服务端回报的接收统计才带 jitter 与丢包。
	sum := output.End.SumReceived
	if sum.Packets == 0 {
		sum = output.End.SumSent
	}
	if sum.Packets == 0 {
		result.Err = "iperf3 未返回 UDP 包统计"
		return result
	}
	result.Available = true
	result.JitterMS = sum.JitterMS
	result.LostPercent = sum.LostPercent
	result.Packets = sum.Packets
	result.Mbps = sum.BitsPerSecond / 1_000_000
	return result
}

type iperfDirectionResult struct {
	Mbps        float64
	Bytes       int64
	Seconds     float64
	Retransmits int64
	Port        int
	LocalHost   string
	RemoteHost  string
	Error       string
}

type iperfRow struct {
	Target   config.IPerfEndpoint
	Family   string
	Upload   iperfDirectionResult
	Download iperfDirectionResult
	UDP      udpResult
}

type speedProbe struct{}

func (speedProbe) ID() string         { return "speed" }
func (speedProbe) Title() string      { return "网络吞吐" }
func (speedProbe) NeedsNetwork() bool { return true }

func (speedProbe) Run(ctx context.Context, env Environment) model.Result {
	if path, err := exec.LookPath("iperf3"); err == nil {
		return runIPerfSpeed(ctx, env, path)
	}
	start := time.Now()
	result := model.NewResult("speed", "网络吞吐")
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "iperf3",
		Profile:         "TCP multi-stream forward/reverse",
		ComparisonScope: "相同 iperf3 版本、节点、方向、并发流与时长",
	}
	result.Status = model.StatusWarning
	result.Summary = "未找到 iperf3，标准网络吞吐基准未运行"
	result.Notes = append(result.Notes, "运行 install.sh --with-benchmarks 或通过系统包管理器安装 iperf3。ecs 不提供 HTTP 或自研替代分数。")
	result.Finish(start)
	return result
}

func runIPerfSpeed(ctx context.Context, env Environment, path string) model.Result {
	start := time.Now()
	result := model.NewResult("speed", "网络吞吐")
	result.Description = "iperf3 公共节点 TCP 多流上传与反向下载吞吐"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "iperf3",
		Profile:         "TCP multi-stream forward/reverse",
		ComparisonScope: "相同 iperf3 版本、节点、IP 协议、并发流与时长",
	}

	if len(env.Config.IPerfTargets) == 0 {
		result.Status = model.StatusWarning
		result.Summary = "未配置 iperf3 节点，标准网络吞吐基准未运行"
		result.Finish(start)
		return result
	}
	seconds := int(env.Config.IPerfDuration / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	threads := env.Config.SpeedThreads
	if threads < 1 {
		threads = 1
	}

	hasIPv6 := hostHasUsableIPv6()
	// UDP 丢包/抖动只在 full 档跑：它需要额外的往返时间，而 standard 档的重点
	// 是吞吐本身。
	udpEnabled := env.Config.Profile == "full"
	rows := make([]iperfRow, 0, len(env.Config.IPerfTargets)*2)
	for _, target := range env.Config.IPerfTargets {
		for _, family := range endpointFamilies(target.Networks, hasIPv6) {
			if ctx.Err() != nil {
				break
			}
			row := iperfRow{
				Target:   target,
				Family:   family,
				Upload:   runIPerfDirection(ctx, path, target, family, false, threads, seconds),
				Download: runIPerfDirection(ctx, path, target, family, true, threads, seconds),
			}
			if udpEnabled && ctx.Err() == nil && (row.Upload.Mbps > 0 || row.Download.Mbps > 0) {
				port := target.PortStart
				if row.Upload.Port > 0 {
					port = row.Upload.Port
				}
				row.UDP = runIPerfUDP(ctx, path, target.Host, port, family, "50M", 5)
			}
			rows = append(rows, row)
		}
	}

	table := model.Table{
		Title:   "iperf3 TCP 节点",
		Columns: []string{"服务商", "位置", "协议", "上传", "下载", "重传", "UDP 丢包", "UDP 抖动", "端口", "状态"},
	}
	var transferred int64
	failures := 0
	completedDirections := 0
	for rowIndex, row := range rows {
		status := "完成"
		if row.Upload.Mbps <= 0 && row.Download.Mbps <= 0 {
			status = "失败"
			failures++
		} else if row.Upload.Mbps <= 0 || row.Download.Mbps <= 0 {
			status = "部分"
			failures++
		}
		if row.Upload.Mbps > 0 {
			transferred += row.Upload.Bytes
			completedDirections++
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:            fmt.Sprintf("iperf3_target_%02d_%s_upload_mbps", rowIndex+1, strings.ToLower(row.Family)),
				Label:          fmt.Sprintf("%s %s 上传", row.Target.Name, row.Family),
				Value:          row.Upload.Mbps,
				Unit:           "Mbps",
				Display:        model.FormatRate(row.Upload.Mbps, "Mbps"),
				Method:         fmt.Sprintf("iperf3-tcp-forward-p%d-%ds-v1", threads, seconds),
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		if row.Download.Mbps > 0 {
			transferred += row.Download.Bytes
			completedDirections++
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:            fmt.Sprintf("iperf3_target_%02d_%s_download_mbps", rowIndex+1, strings.ToLower(row.Family)),
				Label:          fmt.Sprintf("%s %s 下载", row.Target.Name, row.Family),
				Value:          row.Download.Mbps,
				Unit:           "Mbps",
				Display:        model.FormatRate(row.Download.Mbps, "Mbps"),
				Method:         fmt.Sprintf("iperf3-tcp-reverse-p%d-%ds-v1", threads, seconds),
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		retransmits := row.Upload.Retransmits + row.Download.Retransmits
		ports := formatIPerfPorts(row.Upload.Port, row.Download.Port)
		udpLoss, udpJitter := "—", "—"
		if row.UDP.Available {
			udpLoss = fmt.Sprintf("%.2f %%", row.UDP.LostPercent)
			udpJitter = fmt.Sprintf("%.3f ms", row.UDP.JitterMS)
			result.Measurements = append(result.Measurements,
				model.Measurement{
					Key:   fmt.Sprintf("iperf3_target_%02d_%s_udp_loss_percent", rowIndex+1, strings.ToLower(row.Family)),
					Label: fmt.Sprintf("%s %s UDP 丢包", row.Target.Name, row.Family),
					Value: row.UDP.LostPercent, Unit: "%", Display: udpLoss,
					Method: "iperf3-udp-50M-5s-v1", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key:   fmt.Sprintf("iperf3_target_%02d_%s_udp_jitter_ms", rowIndex+1, strings.ToLower(row.Family)),
					Label: fmt.Sprintf("%s %s UDP 抖动", row.Target.Name, row.Family),
					Value: row.UDP.JitterMS, Unit: "ms", Display: udpJitter,
					Method: "iperf3-udp-50M-5s-v1", HigherIsBetter: model.BoolPtr(false),
				},
			)
		} else if row.UDP.Err != "" {
			result.Notes = append(result.Notes,
				fmt.Sprintf("%s %s UDP 测试失败: %s", row.Target.Name, row.Family, row.UDP.Err))
		}
		table.Rows = append(table.Rows, []string{
			row.Target.Name,
			fallback(row.Target.Location, row.Target.Host),
			row.Family,
			formatOptionalMbps(row.Upload.Mbps),
			formatOptionalMbps(row.Download.Mbps),
			strconv.FormatInt(retransmits, 10),
			udpLoss,
			udpJitter,
			ports,
			status,
		})
		for _, direction := range []struct {
			name   string
			sample iperfDirectionResult
		}{
			{"上传", row.Upload},
			{"下载", row.Download},
		} {
			if direction.sample.Error != "" {
				result.Notes = append(result.Notes,
					fmt.Sprintf("%s %s %s失败: %s", row.Target.Name, row.Family, direction.name, direction.sample.Error))
			}
		}
	}
	result.Tables = []model.Table{table}
	if completedDirections == 0 {
		result.Fail(fmt.Errorf("所有 iperf3 节点与方向均失败"))
		result.Finish(start)
		return result
	}
	if failures > 0 {
		result.Status = model.StatusWarning
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "标准工具", Value: "iperf3"},
		{Key: "version", Label: "工具版本", Value: commandVersion(ctx, path)},
		{Key: "binary_sha256", Label: "程序 SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "threads", Label: "并发流", Value: strconv.Itoa(threads)},
		{Key: "duration", Label: "每节点每方向", Value: fmt.Sprintf("%ds", seconds)},
		{Key: "targets", Label: "配置节点", Value: strconv.Itoa(len(env.Config.IPerfTargets))},
		{Key: "actual_traffic", Label: "已统计传输量", Value: model.FormatBytes(uint64(max64(transferred, 0)))},
		{Key: "arguments", Label: "参数模板", Value: "iperf3 -4|-6 -c HOST -p PORT -P N -t S -J [-R]"},
	}
	result.Sources = []model.Source{
		{Name: "iperf3", URL: "https://software.es.net/iperf/", Purpose: "TCP 网络吞吐标准测试工具"},
		{Name: "YABS public server registry", URL: "https://github.com/masonr/yet-another-bench-script", Purpose: "仅复用默认公共 iperf3 节点清单"},
	}
	result.Notes = append(result.Notes,
		"iperf3 是按时长尽力跑满链路的主动测试；实际流量随带宽变化，不能用 MiB 上限精确预估。",
		"公共节点可能繁忙、限速或更换端口；单节点失败不代表 VPS 网络故障。",
		"只比较相同节点、IP 协议、iperf3 版本、并发流和时长的结果。",
		"报告保留每个节点、方向的 iperf3 原值，不计算跨节点平均分、中位数或综合分。",
	)
	if udpEnabled {
		result.Notes = append(result.Notes,
			"UDP 列用 50 Mbps 固定码率跑 5 秒，测的是常态丢包与抖动而不是压满带宽后的拥塞表现；实时音视频与游戏体验主要取决于这两项。",
		)
	} else {
		result.Notes = append(result.Notes, "UDP 丢包与抖动仅在 full 档执行，可用 --profile full 获取。")
	}
	result.Summary = fmt.Sprintf(
		"iperf3 完成 %d/%d 个节点方向 · 实际传输 %s",
		completedDirections,
		len(rows)*2,
		model.FormatBytes(uint64(max64(transferred, 0))),
	)
	result.Finish(start)
	return result
}

func runIPerfDirection(ctx context.Context, path string, target config.IPerfEndpoint, family string, reverse bool, threads, seconds int) iperfDirectionResult {
	attempts := target.PortEnd - target.PortStart + 1
	if attempts > 3 {
		attempts = 3
	}
	if attempts < 1 {
		attempts = 1
	}
	var last iperfDirectionResult
	for attempt := 0; attempt < attempts; attempt++ {
		port := target.PortStart + attempt
		sample := executeIPerf(ctx, path, target.Host, port, family, reverse, threads, seconds)
		if sample.Mbps > 0 {
			return sample
		}
		last = sample
		if ctx.Err() != nil {
			break
		}
	}
	return last
}

func executeIPerf(ctx context.Context, path, host string, port int, family string, reverse bool, threads, seconds int) iperfDirectionResult {
	args := []string{
		"-" + strings.TrimPrefix(family, "IPv"),
		"-c", host,
		"-p", strconv.Itoa(port),
		"-P", strconv.Itoa(threads),
		"-t", strconv.Itoa(seconds),
		"-J",
	}
	if reverse {
		args = append(args, "-R")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds+12)*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	sample := iperfDirectionResult{Port: port}
	if runCtx.Err() != nil {
		sample.Error = runCtx.Err().Error()
		return sample
	}
	if stdout.Len() > 4*1024*1024 {
		sample.Error = "iperf3 JSON 超过 4 MiB 安全上限"
		return sample
	}
	var output iperfJSONOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		detail := tailText(sanitizeCommandOutput(stderr.Bytes()), 300)
		if detail == "" {
			detail = tailText(sanitizeCommandOutput(stdout.Bytes()), 300)
		}
		sample.Error = fmt.Sprintf("解析 JSON: %v: %s", decodeErr, detail)
		return sample
	}
	if output.Error != "" {
		sample.Error = output.Error
		return sample
	}
	if err != nil {
		sample.Error = fmt.Sprintf("%v: %s", err, tailText(sanitizeCommandOutput(stderr.Bytes()), 300))
		return sample
	}
	sum := output.End.SumReceived
	if sum.BitsPerSecond <= 0 {
		sum = output.End.SumSent
	}
	sample.Mbps = sum.BitsPerSecond / 1_000_000
	sample.Bytes = sum.Bytes
	sample.Seconds = sum.Seconds
	sample.Retransmits = output.End.SumSent.Retransmits
	if len(output.Start.Connected) > 0 {
		sample.LocalHost = output.Start.Connected[0].LocalHost
		sample.RemoteHost = output.Start.Connected[0].RemoteHost
	}
	if sample.Mbps <= 0 {
		sample.Error = "iperf3 未返回有效吞吐"
	}
	return sample
}

func endpointFamilies(networks string, hasIPv6 bool) []string {
	switch networks {
	case "IPv6":
		if hasIPv6 {
			return []string{"IPv6"}
		}
		return nil
	case "IPv4|IPv6":
		if hasIPv6 {
			return []string{"IPv4", "IPv6"}
		}
		return []string{"IPv4"}
	default:
		return []string{"IPv4"}
	}
}

// hasGlobalUnicastIPv6 判断地址列表里是否存在全球可路由的 IPv6 单播地址。
//
// 必须排除 ULA（fc00::/7）：Tailscale、Docker 和家用网络都会分配 ULA，
// 它有地址但到不了公网。注意 net.IP.IsGlobalUnicast() 对 ULA 同样返回 true，
// 不能用它做这个判断，得靠 IsPrivate()。
func hasGlobalUnicastIPv6(addresses []net.Addr) bool {
	for _, address := range addresses {
		raw := address.String()
		if slash := strings.IndexByte(raw, '/'); slash >= 0 {
			raw = raw[:slash]
		}
		ip := net.ParseIP(raw)
		if ip == nil || ip.To4() != nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || !ip.IsGlobalUnicast() {
			continue
		}
		return true
	}
	return false
}

// hostHasUsableIPv6 判断本机是否真的能用 IPv6 出网。
//
// 只看网卡上有没有 IPv6 地址是不够的。实测中一台只有 Tailscale ULA、没有 IPv6
// 默认路由的机器会被判为"支持 IPv6"，于是每个 iperf3 节点都白跑一轮 IPv6 测试
// 并全部失败，既浪费时间又在报告里留下一堆并非链路问题的"失败"行。
//
// 因此在地址检查之外再确认内核确实有到公网 IPv6 的路由：UDP dial 只做路由查找、
// 不发送任何数据包，没有路由会立即失败。
func hostHasUsableIPv6() bool {
	addresses, err := net.InterfaceAddrs()
	if err != nil || !hasGlobalUnicastIPv6(addresses) {
		return false
	}
	connection, err := net.DialTimeout("udp6", "[2001:4860:4860::8888]:53", 2*time.Second)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func formatOptionalMbps(value float64) string {
	if value <= 0 {
		return "失败"
	}
	return model.FormatRate(value, "Mbps")
}

func formatIPerfPorts(upload, download int) string {
	switch {
	case upload > 0 && upload == download:
		return strconv.Itoa(upload)
	case upload > 0 && download > 0:
		return fmt.Sprintf("%d/%d", upload, download)
	case upload > 0:
		return strconv.Itoa(upload)
	case download > 0:
		return strconv.Itoa(download)
	default:
		return "—"
	}
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
