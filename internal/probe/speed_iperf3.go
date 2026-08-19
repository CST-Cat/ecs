package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
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
		TestStart struct {
			Protocol string `json:"protocol"`
			Reverse  *int   `json:"reverse"`
		} `json:"test_start"`
	} `json:"start"`
	Intervals []struct {
		Sum struct {
			BitsPerSecond *float64 `json:"bits_per_second"`
		} `json:"sum"`
	} `json:"intervals"`
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
		"--connect-timeout", strconv.Itoa(int(iperfControlConnectTimeout / time.Millisecond)),
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
	if err != nil {
		result.Err = fmt.Sprintf("%v: %s", err, tailText(sanitizeCommandOutput(stderr.Bytes()), 200))
		return result
	}
	if !strings.EqualFold(output.Start.TestStart.Protocol, "UDP") {
		result.Err = "iperf3 返回的不是 UDP 结果"
		return result
	}
	// UDP 模式下服务端回报的接收统计才带 jitter 与丢包。
	sum := output.End.SumReceived
	if sum.Packets <= 0 {
		result.Err = "iperf3 未返回 UDP 包统计"
		return result
	}
	if !isPositiveFinite(sum.BitsPerSecond) || !nonNegativeFinite(sum.JitterMS) || !nonNegativeFinite(sum.LostPercent) || sum.LostPercent > 100 {
		result.Err = "iperf3 UDP 返回了无效的吞吐、抖动或丢包统计"
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
	Mbps           float64
	Bytes          int64
	Seconds        float64
	Retransmits    int64
	IntervalMbps   []float64
	IntervalMin    float64
	IntervalMedian float64
	IntervalCV     float64
	Port           int
	LocalHost      string
	RemoteHost     string
	Error          string
}

type iperfRow struct {
	Target   config.IPerfEndpoint
	Family   string
	Upload   iperfDirectionResult
	Download iperfDirectionResult
	UDP      udpResult
}

type speedProbe struct{}

// iperfPortProbeTimeout bounds the optional TCP reachability check used by the
// live node test.  Public nodes commonly leave part of their advertised port
// range filtered; a bounded check keeps that diagnostic test from waiting on
// every filtered port.  The check sends no application data.
const iperfPortProbeTimeout = 1500 * time.Millisecond

// iperfControlConnectTimeout limits only TCP control-connection setup.  The
// real iperf3 client can therefore move past a filtered or black-holed port
// without a separate connect/close preflight that might disturb a public
// iperf3 daemon still handling the previous test.
const iperfControlConnectTimeout = 1500 * time.Millisecond

// Do not let a user-supplied or malformed endpoint range turn the cheap scan
// into an unbounded wait.  The built-in longest range is 37 ports, so this
// leaves room to inspect every built-in endpoint while capping larger ranges.
const iperfPortScanBudget = 60 * time.Second

// iperfDirectionBudgetWindows is the number of full iperf3 command windows a
// direction may spend after the short port scan.  The configured ranges are
// still traversed in order; this wall-clock budget only protects callers from
// a pathological range whose every port silently drops packets.  A successful
// session always stops the scan immediately.
const iperfDirectionBudgetWindows = 2

type iperfDirectionRunner func(context.Context, string, string, int, string, bool, int, int) iperfDirectionResult

type iperfPortChecker func(context.Context, string, int, string) error

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
		Profile:         "TCP multi-stream forward/reverse + UDP 50M/5s",
		ComparisonScope: "相同 iperf3 版本、节点、方向、并发流与时长",
	}
	result.Status = model.StatusWarning
	result.Summary = "未找到 iperf3，标准网络吞吐基准未运行"
	result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: "iperf3", Count: 1, Message: result.Summary})
	result.Evidence = model.NewEvidence(0, len(env.Config.IPerfTargets)*3, "operation")
	result.Notes = append(result.Notes, "probe.speed.tool_missing")
	result.Finish(start)
	return result
}

func runIPerfSpeed(ctx context.Context, env Environment, path string) model.Result {
	start := time.Now()
	result := model.NewResult("speed", "网络吞吐")
	result.Description = "iperf3 公共节点 TCP 多流上传/反向下载与 UDP 质量"
	result.Methodology = model.Methodology{
		Kind:            "standard-benchmark",
		Label:           "标准基准",
		Engine:          "iperf3",
		Profile:         "TCP multi-stream forward/reverse + UDP 50M/5s",
		ComparisonScope: "相同 iperf3 版本、节点、IP 协议、并发流与时长",
	}

	if len(env.Config.IPerfTargets) == 0 {
		result.Status = model.StatusWarning
		result.Summary = "未配置 iperf3 节点，标准网络吞吐基准未运行"
		result.Evidence = model.NewEvidence(0, 0, "operation")
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

	rows := make([]iperfRow, 0, len(env.Config.IPerfTargets)*2)
	for _, target := range env.Config.IPerfTargets {
		for _, family := range endpointFamilies(target.Networks, env.Network.IPv4Usable, env.Network.IPv6Usable, env.Config.IPVersion) {
			if ctx.Err() != nil {
				break
			}
			upload := runIPerfDirection(ctx, path, target, family, false, threads, seconds)
			row := iperfRow{
				Target: target,
				Family: family,
				Upload: upload,
				// Public nodes often expose several ports, and a successful upload
				// identifies the port whose daemon is actually serving this family.
				// Try that port first for reverse mode, then retain the configured
				// range as a fallback when the daemon is busy or direction-filtered.
				Download: runIPerfDirectionPreferred(ctx, path, target, family, true, threads, seconds, upload.Port),
			}
			if ctx.Err() == nil && (isPositiveFinite(row.Upload.Mbps) || isPositiveFinite(row.Download.Mbps)) {
				port := iperfUDPPort(target, row.Upload, row.Download)
				row.UDP = runIPerfUDP(ctx, path, target.Host, port, family, "50M", int(config.IPerfUDPDuration/time.Second))
			}
			rows = append(rows, row)
		}
	}

	table := model.Table{
		Key:                   "network.iperf3.results",
		Title:                 "iperf3 TCP/UDP 节点",
		Columns:               []string{"服务商", "位置", "协议", "上传", "下载", "UDP 丢包", "UDP 抖动", "端口", "状态"},
		ColumnKeys:            []string{"provider", "location", "protocol", "upload_mbps", "download_mbps", "udp_loss_percent", "udp_jitter_ms", "port", "status"},
		NumericColumns:        []int{3, 4, 5, 6},
		NumericHigherIsBetter: []bool{true, true, false, false},
	}
	stabilityTable := model.Table{
		Key:                   "network.iperf3.stability",
		Title:                 "iperf3 TCP 分秒稳定性",
		Columns:               []string{"服务商", "协议", "方向", "最低", "P50", "变异系数", "重传", "区间"},
		ColumnKeys:            []string{"provider", "protocol", "direction", "minimum_mbps", "p50_mbps", "coefficient_of_variation_percent", "retransmits", "interval"},
		NumericColumns:        []int{3, 4, 5, 6},
		NumericHigherIsBetter: []bool{true, true, false, false},
	}
	var transferred int64
	failures := 0
	completedDirections := 0
	for rowIndex, row := range rows {
		status := "完成"
		if !isPositiveFinite(row.Upload.Mbps) && !isPositiveFinite(row.Download.Mbps) {
			status = "失败"
			failures++
		} else if !isPositiveFinite(row.Upload.Mbps) || !isPositiveFinite(row.Download.Mbps) {
			status = "部分"
			failures++
		}
		if isPositiveFinite(row.Upload.Mbps) {
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
			appendIPerfDirectionDiagnostics(&result, &stabilityTable, rowIndex, row, "upload", "上传", row.Upload, threads, seconds)
		}
		if isPositiveFinite(row.Download.Mbps) {
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
			appendIPerfDirectionDiagnostics(&result, &stabilityTable, rowIndex, row, "download", "下载", row.Download, threads, seconds)
		}
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
			addFailureMessage(&result, "udp", row.Target.Host+" "+row.Family, row.UDP.Err)
			result.Notes = append(result.Notes,
				fmt.Sprintf("%s %s UDP 测试失败: %s", row.Target.Name, row.Family, row.UDP.Err))
		}
		table.Rows = append(table.Rows, []string{
			row.Target.Name,
			fallback(row.Target.Location, row.Target.Host),
			row.Family,
			formatOptionalMbps(row.Upload.Mbps),
			formatOptionalMbps(row.Download.Mbps),
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
				addFailureMessage(&result, "tcp_"+map[string]string{"上传": "forward", "下载": "reverse"}[direction.name], row.Target.Host+" "+row.Family, direction.sample.Error)
				result.Notes = append(result.Notes,
					fmt.Sprintf("%s %s %s失败: %s", row.Target.Name, row.Family, direction.name, direction.sample.Error))
			}
		}
	}
	result.Tables = []model.Table{table}
	if len(stabilityTable.Rows) > 0 {
		result.Tables = append(result.Tables, stabilityTable)
	}
	validOperations := completedDirections
	for _, row := range rows {
		if row.UDP.Available {
			validOperations++
		}
	}
	result.Evidence = model.NewEvidence(validOperations, len(rows)*3, "operation")
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
		{Key: "version", Label: "iperf3 版本", Value: commandVersion(ctx, path)},
		{Key: "binary_sha256", Label: "iperf3 SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
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
	result.Notes = append(result.Notes,
		"UDP 列用 50 Mbps 固定码率跑 5 秒，测的是常态丢包与抖动而不是压满带宽后的拥塞表现；实时音视频与游戏体验主要取决于这两项。",
	)
	result.Summary = fmt.Sprintf(
		"iperf3 完成 %d/%d 个节点方向 · 实际传输 %s",
		completedDirections,
		len(rows)*2,
		model.FormatBytes(uint64(max64(transferred, 0))),
	)
	result.Finish(start)
	return result
}

func appendIPerfDirectionDiagnostics(
	result *model.Result,
	table *model.Table,
	rowIndex int,
	row iperfRow,
	directionKey, directionLabel string,
	sample iperfDirectionResult,
	threads, seconds int,
) {
	if result == nil || table == nil || !isPositiveFinite(sample.Mbps) {
		return
	}
	prefix := fmt.Sprintf("iperf3_target_%02d_%s_%s", rowIndex+1, strings.ToLower(row.Family), directionKey)
	directionMethod := "forward"
	if directionKey == "download" {
		directionMethod = "reverse"
	}
	method := fmt.Sprintf("iperf3-tcp-%s-p%d-%ds-v1", directionMethod, threads, seconds)
	result.Measurements = append(result.Measurements, model.Measurement{
		Key:            prefix + "_retransmits",
		Label:          fmt.Sprintf("%s %s %s重传", row.Target.Name, row.Family, directionLabel),
		Value:          float64(sample.Retransmits),
		Unit:           "retransmits",
		Display:        strconv.FormatInt(sample.Retransmits, 10),
		Method:         method,
		HigherIsBetter: model.BoolPtr(false),
	})
	if len(sample.IntervalMbps) == 0 {
		return
	}
	result.Measurements = append(result.Measurements,
		model.Measurement{
			Key: prefix + "_interval_min_mbps", Label: fmt.Sprintf("%s %s %s分秒最低", row.Target.Name, row.Family, directionLabel),
			Value: sample.IntervalMin, Unit: "Mbps", Display: model.FormatRate(sample.IntervalMin, "Mbps"),
			Method: method + "-intervals", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: prefix + "_interval_p50_mbps", Label: fmt.Sprintf("%s %s %s分秒 P50", row.Target.Name, row.Family, directionLabel),
			Value: sample.IntervalMedian, Unit: "Mbps", Display: model.FormatRate(sample.IntervalMedian, "Mbps"),
			Method: method + "-intervals", HigherIsBetter: model.BoolPtr(true),
		},
		model.Measurement{
			Key: prefix + "_interval_cv_percent", Label: fmt.Sprintf("%s %s %s分秒变异系数", row.Target.Name, row.Family, directionLabel),
			Value: sample.IntervalCV, Unit: "%", Display: fmt.Sprintf("%.2f %%", sample.IntervalCV),
			Method: method + "-intervals", HigherIsBetter: model.BoolPtr(false),
		},
	)
	table.Rows = append(table.Rows, []string{
		row.Target.Name, row.Family, directionLabel,
		model.FormatRate(sample.IntervalMin, "Mbps"),
		model.FormatRate(sample.IntervalMedian, "Mbps"),
		fmt.Sprintf("%.2f %%", sample.IntervalCV),
		strconv.FormatInt(sample.Retransmits, 10),
		strconv.Itoa(len(sample.IntervalMbps)),
	})
}

func runIPerfDirection(ctx context.Context, path string, target config.IPerfEndpoint, family string, reverse bool, threads, seconds int) iperfDirectionResult {
	// Do not make a separate TCP connect/close preflight here: on an iperf3
	// server that can leave an incomplete control session and reset the next
	// direction.  executeIPerf uses --connect-timeout for the same fast retry
	// behavior without that extra connection.
	return runIPerfDirectionWith(ctx, path, target, family, reverse, threads, seconds, executeIPerf, nil)
}

func runIPerfDirectionPreferred(ctx context.Context, path string, target config.IPerfEndpoint, family string, reverse bool, threads, seconds, preferredPort int) iperfDirectionResult {
	return runIPerfDirectionWithPreferred(ctx, path, target, family, reverse, threads, seconds, preferredPort, executeIPerf, nil)
}

// runIPerfDirectionWith is the retry policy behind runIPerfDirection.  Keeping
// the runner and port checker injectable makes the policy auditable without
// replacing the real iperf3 process in production: tests can exercise every
// port, protocol failure, success stop, and cancellation deterministically.
func runIPerfDirectionWith(
	ctx context.Context,
	path string,
	target config.IPerfEndpoint,
	family string,
	reverse bool,
	threads, seconds int,
	run iperfDirectionRunner,
	check iperfPortChecker,
) iperfDirectionResult {
	return runIPerfDirectionWithPreferred(ctx, path, target, family, reverse, threads, seconds, 0, run, check)
}

func runIPerfDirectionWithPreferred(
	ctx context.Context,
	path string,
	target config.IPerfEndpoint,
	family string,
	reverse bool,
	threads, seconds, preferredPort int,
	run iperfDirectionRunner,
	check iperfPortChecker,
) iperfDirectionResult {
	ports := iperfPortCandidates(target)
	if len(ports) == 0 {
		return iperfDirectionResult{
			Port:  target.PortStart,
			Error: "iperf3 未配置有效端口范围",
		}
	}

	// executeIPerf already bounds each child process.  This outer budget adds a
	// finite ceiling across a whole range of silently dropped ports while still
	// leaving room for the short scan and one successful transfer.
	runCtx, cancel := context.WithTimeout(ctx, iperfDirectionBudget(seconds, len(ports)))
	defer cancel()

	var last iperfDirectionResult
	for _, port := range orderedIPerfPorts(ports, preferredPort) {
		if err := runCtx.Err(); err != nil {
			// Keep the first configured port in the result when cancellation
			// happens before any attempt, matching executeIPerf's port record.
			if last.Port == 0 {
				last.Port = port
			}
			if last.Error == "" {
				last.Error = err.Error()
			}
			break
		}

		if check != nil {
			if err := check(runCtx, target.Host, port, family); err != nil {
				last = iperfDirectionResult{
					Port:  port,
					Error: fmt.Sprintf("iperf3 端口预检失败: %v", err),
				}
				if runCtx.Err() != nil {
					break
				}
				continue
			}
		}

		sample := run(runCtx, path, target.Host, port, family, reverse, threads, seconds)
		if sample.Port == 0 {
			sample.Port = port
		}
		if isPositiveFinite(sample.Mbps) {
			return sample
		}
		last = sample
		if runCtx.Err() != nil {
			break
		}
	}
	return last
}

func orderedIPerfPorts(ports []int, preferred int) []int {
	if preferred <= 0 {
		return ports
	}
	ordered := make([]int, 0, len(ports))
	for _, port := range ports {
		if port == preferred {
			ordered = append(ordered, port)
			break
		}
	}
	for _, port := range ports {
		if port != preferred {
			ordered = append(ordered, port)
		}
	}
	return ordered
}

// iperfPortCandidates returns the configured inclusive range in deterministic
// ascending order.  A malformed range still gets one attempt at PortStart,
// preserving the previous behavior for callers that bypass config validation.
func iperfPortCandidates(target config.IPerfEndpoint) []int {
	start, end := target.PortStart, target.PortEnd
	if end < start {
		return []int{start}
	}
	span := end - start + 1
	// Config validation limits ports to 1..65535.  Keep direct callers safe
	// from integer overflow or an accidental giant allocation as well.
	if span <= 0 || span > 65535 {
		return []int{start}
	}
	ports := make([]int, 0, span)
	for port := start; ; port++ {
		ports = append(ports, port)
		if port == end {
			break
		}
	}
	return ports
}

func iperfDirectionBudget(seconds, portCount int) time.Duration {
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 30 {
		// Config validation currently caps this at 30 seconds.  Keep the
		// retry budget finite for direct callers that bypass validation.
		seconds = 30
	}
	commandWindow := time.Duration(seconds+12) * time.Second
	scanWindow := iperfPortScanBudget
	if portCount >= 0 && portCount < int(iperfPortScanBudget/iperfPortProbeTimeout) {
		scanWindow = time.Duration(portCount) * iperfPortProbeTimeout
	}
	return iperfDirectionBudgetWindows*commandWindow + scanWindow + 2*iperfPortProbeTimeout
}

// iperfUDPPort chooses a port that actually produced TCP throughput.  Failed
// directions retain their last attempted port for diagnostics, but that port
// is not evidence of a listening iperf3 service and must not drive UDP.
func iperfUDPPort(target config.IPerfEndpoint, upload, download iperfDirectionResult) int {
	if isPositiveFinite(upload.Mbps) && upload.Port > 0 {
		return upload.Port
	}
	if isPositiveFinite(download.Mbps) && download.Port > 0 {
		return download.Port
	}
	return target.PortStart
}

func executeIPerf(ctx context.Context, path, host string, port int, family string, reverse bool, threads, seconds int) iperfDirectionResult {
	args := []string{
		"-" + strings.TrimPrefix(family, "IPv"),
		"-c", host,
		"-p", strconv.Itoa(port),
		"--connect-timeout", strconv.Itoa(int(iperfControlConnectTimeout / time.Millisecond)),
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
	if err != nil {
		sample.Error = fmt.Sprintf("%v: %s", err, tailText(sanitizeCommandOutput(stderr.Bytes()), 300))
		return sample
	}
	parsed := parseIPerfTCPJSON(stdout.Bytes(), port, reverse)
	if parsed.Error != "" && len(stderr.Bytes()) > 0 {
		parsed.Error += ": " + tailText(sanitizeCommandOutput(stderr.Bytes()), 300)
	}
	return parsed
}

func parseIPerfTCPJSON(raw []byte, port int, reverse bool) iperfDirectionResult {
	sample := iperfDirectionResult{Port: port}
	if len(raw) > 4*1024*1024 {
		sample.Error = "iperf3 JSON 超过 4 MiB 安全上限"
		return sample
	}
	var output iperfJSONOutput
	if decodeErr := json.Unmarshal(raw, &output); decodeErr != nil {
		sample.Error = fmt.Sprintf("解析 JSON: %v", decodeErr)
		return sample
	}
	if output.Error != "" {
		sample.Error = output.Error
		return sample
	}
	if !strings.EqualFold(output.Start.TestStart.Protocol, "TCP") {
		sample.Error = "iperf3 返回的不是 TCP 结果"
		return sample
	}
	if output.Start.TestStart.Reverse != nil {
		want := 0
		if reverse {
			want = 1
		}
		if *output.Start.TestStart.Reverse != want {
			sample.Error = "iperf3 返回的 TCP 方向与请求不一致"
			return sample
		}
	}
	// Forward mode measures bytes sent by this client; reverse mode measures
	// bytes received by this client.  Do not silently fall back to the opposite
	// summary: a non-empty opposite side can be a partial/error report.
	sum := output.End.SumSent
	if reverse {
		sum = output.End.SumReceived
	}
	if !isPositiveFinite(sum.BitsPerSecond) || sum.Bytes <= 0 || sum.Seconds <= 0 || !nonNegativeFinite(sum.Seconds) {
		sample.Error = "iperf3 未返回有效吞吐统计"
		return sample
	}
	sample.Mbps = sum.BitsPerSecond / 1_000_000
	sample.Bytes = sum.Bytes
	sample.Seconds = sum.Seconds
	sample.Retransmits = output.End.SumSent.Retransmits
	for _, interval := range output.Intervals {
		if interval.Sum.BitsPerSecond == nil || !nonNegativeFinite(*interval.Sum.BitsPerSecond) {
			continue
		}
		sample.IntervalMbps = append(sample.IntervalMbps, *interval.Sum.BitsPerSecond/1_000_000)
	}
	if len(sample.IntervalMbps) > 0 {
		sample.IntervalMin = sample.IntervalMbps[0]
		var total float64
		for _, value := range sample.IntervalMbps {
			if value < sample.IntervalMin {
				sample.IntervalMin = value
			}
			total += value
		}
		sample.IntervalMedian = medianFloat(sample.IntervalMbps)
		mean := total / float64(len(sample.IntervalMbps))
		if mean > 0 {
			sample.IntervalCV = stddevFloat(sample.IntervalMbps) / mean * 100
		}
	}
	if len(output.Start.Connected) > 0 {
		sample.LocalHost = output.Start.Connected[0].LocalHost
		sample.RemoteHost = output.Start.Connected[0].RemoteHost
	}
	return sample
}

func endpointFamilies(networks string, hasIPv4, hasIPv6 bool, mode string) []string {
	allow4 := config.AllowsIPVersion(mode, config.IPVersion4)
	allow6 := config.AllowsIPVersion(mode, config.IPVersion6)
	allow4 = allow4 && hasIPv4
	allow6 = allow6 && hasIPv6
	switch networks {
	case "IPv6":
		if hasIPv6 && allow6 {
			return []string{"IPv6"}
		}
		return nil
	case "IPv4|IPv6":
		families := make([]string, 0, 2)
		if allow4 {
			families = append(families, "IPv4")
		}
		if hasIPv6 && allow6 {
			families = append(families, "IPv6")
		}
		if len(families) == 0 {
			return nil
		}
		return families
	case "IPv4":
		if allow4 {
			return []string{"IPv4"}
		}
		return nil
	default:
		if allow4 {
			return []string{"IPv4"}
		}
		if allow6 {
			return []string{"IPv6"}
		}
		return nil
	}
}

func formatOptionalMbps(value float64) string {
	if !isPositiveFinite(value) {
		return "失败"
	}
	return model.FormatRate(value, "Mbps")
}

func nonNegativeFinite(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
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
