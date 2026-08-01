package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

// 磁盘的补充口径：I/O 延迟（ioping）与介质健康（smartctl）。
//
// fio 测的是吞吐与队列深度下的 IOPS，回答"能跑多快"；ioping 用单个请求测
// 空载延迟，回答"一次 I/O 要等多久"——存储是否被邻居拖累、是否走网络存储，
// 延迟比吞吐更敏感。smartctl 则回答"这块盘用了多久、还剩多少寿命"，
// 二手盘与高磨损盘只有这里看得出来。
//
// 两者都是 Debian 官方软件包，与 fio 一样只作为可关闭的外部适配器调用。

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

// smartInfo 是从 smartctl JSON 里取出的、可安全公开的磁盘健康信息。
//
// 刻意不收录序列号：它能唯一标识一块物理硬件，属于分享报告时的敏感信息，
// 而对"这块盘还能用多久"没有任何帮助。
type smartInfo struct {
	Device      string
	ModelName   string
	Passed      *bool
	PowerOnHrs  *int
	Temperature *int
	PercentUsed *int
	RotationRPM *int
	CapacityB   *uint64
	Message     string
}

// resolveSMARTDevice 由测试路径反查底层整盘设备。
//
// df 给出的是分区（/dev/nvme0n1p2），SMART 属于整盘（/dev/nvme0n1），
// 用 lsblk 的 PKNAME 取父设备；本身就是整盘时 PKNAME 为空。
func resolveSMARTDevice(ctx context.Context, diskPath string) string {
	source := commandOutput(ctx, "df", "--output=source", diskPath)
	lines := strings.Split(strings.TrimSpace(source), "\n")
	if len(lines) < 2 {
		return ""
	}
	device := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(device, "/dev/") {
		return ""
	}
	if parent := commandOutput(ctx, "lsblk", "-no", "PKNAME", device); parent != "" {
		if name := strings.TrimSpace(strings.Split(parent, "\n")[0]); name != "" {
			return "/dev/" + name
		}
	}
	return device
}

// readSMART 读取磁盘健康信息。
//
// smartctl 需要对块设备的读权限，非 root 通常拿不到；虚拟磁盘（VPS 上常见的
// virtio/vda）往往根本不提供 SMART。两种情况都不是错误，如实说明即可。
func readSMART(ctx context.Context, device string) (smartInfo, bool) {
	info := smartInfo{Device: device}
	path, err := exec.LookPath("smartctl")
	if err != nil {
		// 常见于非 root 会话：smartctl 装在 /usr/sbin，不在普通用户 PATH 里。
		if _, statErr := os.Stat("/usr/sbin/smartctl"); statErr != nil {
			return info, false
		}
		path = "/usr/sbin/smartctl"
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, path, "-j", "-i", "-A", "-H", device)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, _ := command.Output()
	if len(output) == 0 || len(output) > 4*1024*1024 {
		return info, false
	}

	var payload struct {
		ModelName    string `json:"model_name"`
		SerialHidden string `json:"-"`
		SmartStatus  struct {
			Passed *bool `json:"passed"`
		} `json:"smart_status"`
		PowerOnTime struct {
			Hours *int `json:"hours"`
		} `json:"power_on_time"`
		Temperature struct {
			Current *int `json:"current"`
		} `json:"temperature"`
		RotationRate *int `json:"rotation_rate"`
		UserCapacity struct {
			Bytes *uint64 `json:"bytes"`
		} `json:"user_capacity"`
		NVMeLog struct {
			PercentageUsed *int `json:"percentage_used"`
		} `json:"nvme_smart_health_information_log"`
		Smartctl struct {
			Messages []struct {
				String string `json:"string"`
			} `json:"messages"`
		} `json:"smartctl"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return info, false
	}
	for _, message := range payload.Smartctl.Messages {
		if strings.TrimSpace(message.String) != "" {
			info.Message = message.String
			break
		}
	}
	info.ModelName = payload.ModelName
	info.Passed = payload.SmartStatus.Passed
	info.PowerOnHrs = payload.PowerOnTime.Hours
	info.Temperature = payload.Temperature.Current
	info.PercentUsed = payload.NVMeLog.PercentageUsed
	info.RotationRPM = payload.RotationRate
	info.CapacityB = payload.UserCapacity.Bytes

	usable := info.ModelName != "" || info.Passed != nil || info.PowerOnHrs != nil
	return info, usable
}

// appendSMARTHealth 在磁盘结果上追加介质健康信息。
func appendSMARTHealth(ctx context.Context, result *model.Result, diskPath string) {
	device := resolveSMARTDevice(ctx, diskPath)
	if device == "" {
		return
	}
	info, ok := readSMART(ctx, device)
	if !ok {
		// 失败原因常含 smartctl 的英文原文，拼进句子会让整句无法翻译；
		// 说明句保持固定，原因单独成字段。
		result.Notes = append(result.Notes,
			"未能读取磁盘 SMART 信息：需要 root 权限，或该设备不提供 SMART。"+
				"VPS 的虚拟磁盘通常不透传 SMART，这不影响 fio 与 ioping 的成绩。")
		if info.Message != "" {
			result.Fields = append(result.Fields, model.Field{
				Key: "smart_error", Label: "SMART 失败原因", Value: compactError(fmt.Errorf("%s", info.Message)),
			})
		}
		return
	}

	fields := []model.Field{
		{Key: "smart_device", Label: "SMART 设备", Value: device, Sensitive: true},
	}
	if info.ModelName != "" {
		fields = append(fields, model.Field{Key: "disk_model", Label: "磁盘型号", Value: info.ModelName})
	}
	if info.RotationRPM != nil {
		kind := "SSD / 非旋转介质"
		if *info.RotationRPM > 0 {
			kind = fmt.Sprintf("机械硬盘 %d RPM", *info.RotationRPM)
		}
		fields = append(fields, model.Field{Key: "disk_media", Label: "介质类型", Value: kind})
	}
	if info.Passed != nil {
		status := "通过"
		if !*info.Passed {
			status = "未通过（磁盘自评为异常）"
		}
		fields = append(fields, model.Field{Key: "smart_health", Label: "SMART 自检", Value: status})
	}
	if info.PowerOnHrs != nil {
		hours := *info.PowerOnHrs
		fields = append(fields, model.Field{
			Key: "disk_power_on", Label: "通电时间",
			Value: fmt.Sprintf("%d 小时（约 %.1f 年）", hours, float64(hours)/24/365),
		})
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "disk_power_on_hours", Label: "磁盘通电时间",
			Value: float64(hours), Unit: "小时", Display: strconv.Itoa(hours),
			Method: "smartctl-power-on-time-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if info.Temperature != nil {
		fields = append(fields, model.Field{
			Key: "disk_temperature", Label: "磁盘温度", Value: fmt.Sprintf("%d °C", *info.Temperature)})
	}
	if info.PercentUsed != nil {
		fields = append(fields, model.Field{
			Key: "disk_wear", Label: "已用寿命", Value: fmt.Sprintf("%d %%", *info.PercentUsed)})
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "disk_percentage_used", Label: "磁盘已用寿命",
			Value: float64(*info.PercentUsed), Unit: "%", Display: fmt.Sprintf("%d %%", *info.PercentUsed),
			Method: "smartctl-nvme-percentage-used-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	result.Fields = append(result.Fields, fields...)
	result.Sources = append(result.Sources, model.Source{
		Name: "smartmontools", URL: "https://www.smartmontools.org/", Purpose: "磁盘 SMART 健康与通电时间",
	})

	notes := []string{
		"SMART 数据来自磁盘自身，反映的是宿主机上这块物理盘的累计使用情况，" +
			"不代表你独占它；通电时间长或已用寿命高的盘更值得留意。",
	}
	if info.PowerOnHrs != nil && *info.PowerOnHrs > 43800 { // 约 5 年
		result.Status = model.StatusWarning
		notes = append(notes, fmt.Sprintf(
			"该盘已通电 %d 小时（超过 5 年），属于高龄介质，建议结合 SMART 自检结果评估风险。", *info.PowerOnHrs))
	}
	if info.PercentUsed != nil && *info.PercentUsed >= 80 {
		result.Status = model.StatusWarning
		notes = append(notes, fmt.Sprintf("该 SSD 已用寿命 %d%%，接近厂商标称写入上限。", *info.PercentUsed))
	}
	if info.Passed != nil && !*info.Passed {
		result.Status = model.StatusWarning
		notes = append(notes, "磁盘 SMART 自检未通过，介质可能已经出现问题。")
	}
	result.Notes = append(result.Notes, notes...)
}
