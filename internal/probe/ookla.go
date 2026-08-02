package probe

import (
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

// ooklaProbe is an adapter around a user-installed official Ookla CLI.
// Keeping it as an explicit module matters: the client has its own licence,
// privacy terms and measurement service, unlike ecs's local-only report.
type ooklaProbe struct{}

func (ooklaProbe) ID() string         { return "ookla" }
func (ooklaProbe) Title() string      { return "Ookla Speedtest（外部服务）" }
func (ooklaProbe) NeedsNetwork() bool { return true }

type ooklaResult struct {
	Ping struct {
		Jitter  float64 `json:"jitter"`
		Latency float64 `json:"latency"`
	} `json:"ping"`
	Download struct {
		Bandwidth float64 `json:"bandwidth"`
	} `json:"download"`
	Upload struct {
		Bandwidth float64 `json:"bandwidth"`
	} `json:"upload"`
	PacketLoss *float64 `json:"packetLoss"`
	ISP        string   `json:"isp"`
	Interface  struct {
		ExternalIP string `json:"externalIp"`
	} `json:"interface"`
	Server struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Location string `json:"location"`
		Country  string `json:"country"`
	} `json:"server"`
	Result struct {
		Persisted bool `json:"persisted"`
	} `json:"result"`
}

func (ooklaProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("ookla", "Ookla Speedtest（外部服务）")
	result.Description = "调用本机已安装的官方 Ookla Speedtest CLI，记录一次外部服务测速结果"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "official Ookla Speedtest CLI",
		Profile:         "JSON, one server selected by Ookla",
		ComparisonScope: "同一 Ookla 客户端、服务器选择与协议族；外部服务数据不等同 ecs 本地报告",
	}

	if !env.Config.OoklaConsent {
		result.Skip("未确认 Ookla 许可与隐私条款")
		result.Notes = append(result.Notes,
			"Ookla 不是 ecs 的零上传探针；如需运行，请显式使用 --only ookla --accept-ookla-terms，或使用 --ookla --accept-ookla-terms。",
		)
		result.Finish(start)
		return result
	}
	path, err := exec.LookPath("speedtest")
	if err != nil {
		result.Skip("未找到官方 speedtest 客户端")
		result.Notes = append(result.Notes, "ecs 不会自动下载或安装 Ookla 客户端；请从 Ookla 官方渠道安装后重跑。")
		result.Finish(start)
		return result
	}

	args := []string{"--accept-license", "--accept-gdpr", "--format=json", "--progress=no"}
	if env.Config.IPVersion == config.IPVersion4 || env.Config.IPVersion == config.IPVersion6 {
		args = append(args, "--ip-version", env.Config.IPVersion)
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "引擎", Value: "official speedtest"},
		{Key: "binary", Label: "客户端路径", Value: path},
		{Key: "binary_sha256", Label: "客户端 SHA-256", Value: fallback(binarySHA256(path), "unavailable")},
		{Key: "arguments", Label: "命令参数", Value: strings.Join(args, " ")},
		{Key: "external_service", Label: "外部测量服务", Value: "Ookla"},
	}
	servers := append([]config.OoklaServer(nil), env.Config.OoklaServers...)
	if len(servers) == 0 {
		servers = []config.OoklaServer{{Carrier: "自动", ID: 0}}
		result.Fields = append(result.Fields, model.Field{Key: "server_selection", Label: "服务器选择", Value: "Ookla 自动选择（未配置三网服务器 ID）"})
	} else {
		var selections []string
		for _, server := range servers {
			selections = append(selections, fmt.Sprintf("%s=%d", server.Carrier, server.ID))
		}
		result.Fields = append(result.Fields, model.Field{Key: "server_selection", Label: "服务器选择", Value: strings.Join(selections, ", ")})
	}

	table := model.Table{
		Title:   "Ookla 测速结果",
		Columns: []string{"运营商标签", "服务器", "延迟", "下载", "上传", "丢包", "状态"},
	}
	successes := 0
	var summaries []string
	for _, target := range servers {
		targetArgs := append([]string(nil), args...)
		if target.ID > 0 {
			targetArgs = append(targetArgs, "--server-id", strconv.Itoa(target.ID))
		}
		parsed, runErr, parseErr, timedOut := runOfficialOokla(ctx, path, targetArgs)
		label := target.Carrier
		if parseErr != nil {
			result.Status = model.StatusWarning
			table.Rows = append(table.Rows, []string{label, formatOoklaServer(parsed), "—", "—", "—", "—", "未解析"})
			result.Fields = append(result.Fields, model.Field{Key: "error_" + ooklaCarrierKey(label), Label: label + " 失败原因", Value: compactError(parseErr)})
			if timedOut {
				result.Notes = append(result.Notes, label+" Ookla 测速超时；外部服务可能仍在处理或网络路径不可用。")
			} else if runErr != nil {
				result.Notes = append(result.Notes, label+" Ookla 客户端返回非零状态；ecs 不保留原始输出，避免把客户端可能包含的本地标识写入报告。")
			}
			continue
		}

		prefix := ""
		if len(servers) > 1 {
			prefix = ooklaCarrierKey(label) + "_"
		}
		appendOoklaMeasurementsFor(&result, parsed, prefix, label)
		serverName := formatOoklaServer(parsed)
		download := ooklaBandwidthMbps(parsed.Download.Bandwidth)
		upload := ooklaBandwidthMbps(parsed.Upload.Bandwidth)
		loss := "—"
		if parsed.PacketLoss != nil {
			loss = fmt.Sprintf("%.2f %%", *parsed.PacketLoss)
		}
		status := "完成"
		if runErr != nil {
			status = "部分完成"
			result.Status = model.StatusWarning
			result.Notes = append(result.Notes, label+" Ookla 客户端返回非零状态，但仍解析到了部分 JSON；结果按部分完成处理。")
		}
		table.Rows = append(table.Rows, []string{label, serverName, fmt.Sprintf("%.2f ms", parsed.Ping.Latency), fmt.Sprintf("%.2f Mbps", download), fmt.Sprintf("%.2f Mbps", upload), loss, status})
		successes++
		summaries = append(summaries, fmt.Sprintf("%s 下载 %.2f Mbps / 上传 %.2f Mbps", label, download, upload))
		if parsed.ISP != "" {
			result.Fields = append(result.Fields, model.Field{Key: "isp_" + ooklaCarrierKey(label), Label: label + " ISP", Value: parsed.ISP})
		}
		if parsed.Interface.ExternalIP != "" {
			result.Fields = append(result.Fields, model.Field{Key: "external_ip_" + ooklaCarrierKey(label), Label: label + " Ookla 出口 IP", Value: parsed.Interface.ExternalIP, Sensitive: true})
			if expected := env.Config.IPVersion; expected == config.IPVersion4 || expected == config.IPVersion6 {
				if ip := net.ParseIP(parsed.Interface.ExternalIP); ip != nil {
					actual := config.IPVersion4
					if ip.To4() == nil {
						actual = config.IPVersion6
					}
					if actual != expected {
						result.Status = model.StatusWarning
						result.Notes = append(result.Notes, fmt.Sprintf("%s 请求 IPv%s，但 Ookla 返回了 IPv%s 出口地址。", label, expected, actual))
					}
				}
			}
		}
	}
	result.Tables = []model.Table{table}
	if successes == 0 {
		result.Status = model.StatusWarning
		result.Summary = "Ookla 没有返回可用测速结果"
	} else if len(result.Measurements) == 0 {
		result.Status = model.StatusWarning
		result.Summary = "Ookla 返回 JSON，但没有可用测速指标"
	} else {
		result.Summary = strings.Join(summaries, " · ")
	}
	result.Sources = []model.Source{{
		Name:    "Ookla Speedtest",
		URL:     "https://www.speedtest.net/",
		Purpose: "外部测速引擎与服务器选择",
	}}
	result.Notes = append(result.Notes,
		"这是显式启用的外部服务调用；Ookla 可能接收测量所需的网络与客户端元数据，ecs 不把它描述成零上传。",
		"ecs 只提取速度、延迟、服务器和 ISP 等字段，不写入 Ookla 原始 JSON、MAC、内网地址或分享 URL。",
		"Ookla 测速会产生实际上下行流量；结果只适合与相同客户端和测试条件比较。",
	)
	result.Finish(start)
	return result
}

func runOfficialOokla(ctx context.Context, path string, args []string) (ooklaResult, error, error, bool) {
	var parsed ooklaResult
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	output, runErr := command.CombinedOutput()
	if len(output) > 512*1024 {
		output = output[:512*1024]
	}
	parsed, parseErr := parseOoklaJSON(output)
	return parsed, runErr, parseErr, runCtx.Err() != nil
}

func formatOoklaServer(parsed ooklaResult) string {
	server := parsed.Server.Name
	if server == "" && parsed.Server.ID > 0 {
		server = "ID " + strconv.Itoa(parsed.Server.ID)
	}
	if parsed.Server.Location != "" {
		server += " · " + parsed.Server.Location
	}
	if parsed.Server.Country != "" {
		server += " · " + parsed.Server.Country
	}
	if server == "" {
		return "自动/未知"
	}
	return server
}

func ooklaCarrierKey(carrier string) string {
	switch carrier {
	case "电信":
		return "telecom"
	case "联通":
		return "unicom"
	case "移动":
		return "mobile"
	default:
		return "auto"
	}
}

func parseOoklaJSON(output []byte) (ooklaResult, error) {
	var parsed ooklaResult
	text := strings.TrimSpace(string(output))
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return parsed, fmt.Errorf("未找到 JSON 对象")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err != nil {
		return parsed, fmt.Errorf("解析 JSON 失败")
	}
	return parsed, nil
}

func appendOoklaMeasurements(result *model.Result, parsed ooklaResult) {
	appendOoklaMeasurementsFor(result, parsed, "", "Ookla")
}

func appendOoklaMeasurementsFor(result *model.Result, parsed ooklaResult, prefix, labelPrefix string) {
	if labelPrefix == "" {
		labelPrefix = "Ookla"
	}
	if parsed.Ping.Latency > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_ping_ms", Label: labelPrefix + " 延迟", Value: parsed.Ping.Latency, Unit: "ms", Display: fmt.Sprintf("%.2f ms", parsed.Ping.Latency),
			Method: "ookla-cli-json-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if parsed.Ping.Jitter > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_jitter_ms", Label: labelPrefix + " 抖动", Value: parsed.Ping.Jitter, Unit: "ms", Display: fmt.Sprintf("%.2f ms", parsed.Ping.Jitter),
			Method: "ookla-cli-json-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if speed := ooklaBandwidthMbps(parsed.Download.Bandwidth); speed > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_download_mbps", Label: labelPrefix + " 下载", Value: speed, Unit: "Mbps", Display: fmt.Sprintf("%.2f Mbps", speed),
			Method: "ookla-cli-json-v1-bandwidth-bytes-per-second", HigherIsBetter: model.BoolPtr(true),
		})
	}
	if speed := ooklaBandwidthMbps(parsed.Upload.Bandwidth); speed > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_upload_mbps", Label: labelPrefix + " 上传", Value: speed, Unit: "Mbps", Display: fmt.Sprintf("%.2f Mbps", speed),
			Method: "ookla-cli-json-v1-bandwidth-bytes-per-second", HigherIsBetter: model.BoolPtr(true),
		})
	}
	if parsed.PacketLoss != nil {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_packet_loss_percent", Label: labelPrefix + " 丢包", Value: *parsed.PacketLoss, Unit: "%", Display: fmt.Sprintf("%.2f %%", *parsed.PacketLoss),
			Method: "ookla-cli-json-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
}

func ooklaBandwidthMbps(bytesPerSecond float64) float64 {
	if bytesPerSecond <= 0 {
		return 0
	}
	return bytesPerSecond * 8 / 1e6
}

func ooklaSummary(parsed ooklaResult) string {
	download := ooklaBandwidthMbps(parsed.Download.Bandwidth)
	upload := ooklaBandwidthMbps(parsed.Upload.Bandwidth)
	return fmt.Sprintf("下载 %.2f Mbps · 上传 %.2f Mbps · 延迟 %.2f ms", download, upload, parsed.Ping.Latency)
}
