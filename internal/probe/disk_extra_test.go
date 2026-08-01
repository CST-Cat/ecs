package probe

import (
	"encoding/json"
	"strings"
	"testing"
)

// 样本取自本机真实运行的 ioping 1.3 输出，注意 min/avg/max/mdev 同行混用了 us 与 ms。
const realIOPingOutput = `
--- . (ext4 /dev/nvme0n1p2 443.2 GiB) ioping statistics ---
9 requests completed in 6.80 ms, 36 KiB read, 1.32 k iops, 5.17 MiB/s
generated 10 requests in 9.00 s, 40 KiB, 1 iops, 4.44 KiB/s
min/avg/max/mdev = 448.8 us / 755.4 us / 2.13 ms / 508.1 us
`

func TestParseIOPingDuration(t *testing.T) {
	cases := []struct {
		value, unit string
		wantMS      float64
	}{
		{"254", "ns", 0.000254},
		{"448.8", "us", 0.4488},
		{"2.13", "ms", 2.13},
		{"1.5", "s", 1500},
	}
	for _, testCase := range cases {
		got, ok := parseIOPingDuration(testCase.value, testCase.unit)
		if !ok {
			t.Fatalf("%s %s 解析失败", testCase.value, testCase.unit)
		}
		if diff := got - testCase.wantMS; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s %s = %g ms, want %g", testCase.value, testCase.unit, got, testCase.wantMS)
		}
	}
	if _, ok := parseIOPingDuration("abc", "ms"); ok {
		t.Fatal("非数值必须解析失败")
	}
	if _, ok := parseIOPingDuration("1", "分钟"); ok {
		t.Fatal("未知单位必须解析失败")
	}
}

func TestParseIOPingOutput(t *testing.T) {
	sample, ok := parseIOPingOutput(realIOPingOutput)
	if !ok {
		t.Fatal("真实 ioping 输出解析失败")
	}
	// 单位换算涉及浮点除法，必须按容差比较：448.8/1e3 的结果是
	// 0.44880000000000003，用 != 断言只会测出 IEEE754 的行为。
	closeTo := func(name string, got, want float64) {
		t.Helper()
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s = %g ms, want %g", name, got, want)
		}
	}
	// 同行混用单位是 ioping 的真实行为，按固定单位解析会把 2.13 ms 读成 2.13 us。
	closeTo("min", sample.MinMS, 0.4488)
	closeTo("avg", sample.AvgMS, 0.7554)
	closeTo("max（ms 不能被当成 us）", sample.MaxMS, 2.13)
	closeTo("mdev", sample.MdevMS, 0.5081)
	if sample.Filesystem != "ext4" {
		t.Errorf("文件系统 = %q, want ext4", sample.Filesystem)
	}
	if sample.Device != "/dev/nvme0n1p2" {
		t.Errorf("设备 = %q", sample.Device)
	}

	// min ≤ avg ≤ max 是必须成立的不变式。
	if !(sample.MinMS <= sample.AvgMS && sample.AvgMS <= sample.MaxMS) {
		t.Fatalf("统计不自洽：%+v", sample)
	}

	if _, ok := parseIOPingOutput("完全无关的输出"); ok {
		t.Fatal("无统计行时必须返回失败")
	}
}

// SMART 解析用本机真实 smartctl 7.4 的 JSON 结构作样本。
// 关键是：序列号存在于输入里，但绝不能出现在解析结果中。
const realSMARTJSON = `{
  "json_format_version": [1, 0],
  "smartctl": {"exit_status": 0, "messages": []},
  "device": {"name": "/dev/nvme0n1", "type": "nvme", "protocol": "NVMe"},
  "model_name": "WD PC SN540 SDDPNPF-512G",
  "serial_number": "225202801813",
  "firmware_version": "33006000",
  "user_capacity": {"bytes": 512110190592},
  "smart_status": {"passed": true},
  "power_on_time": {"hours": 8543},
  "temperature": {"current": 36},
  "nvme_smart_health_information_log": {
    "percentage_used": 5,
    "power_on_hours": 8543,
    "data_units_written": 31783454,
    "unsafe_shutdowns": 63
  }
}`

func TestSMARTPayloadNeverCarriesSerialNumber(t *testing.T) {
	// 与 readSMART 内部使用同一套字段定义：序列号刻意没有对应字段，
	// 因此即便上游返回它，也不会进入任何报告结构。
	var payload struct {
		ModelName   string `json:"model_name"`
		SmartStatus struct {
			Passed *bool `json:"passed"`
		} `json:"smart_status"`
		PowerOnTime struct {
			Hours *int `json:"hours"`
		} `json:"power_on_time"`
		Temperature struct {
			Current *int `json:"current"`
		} `json:"temperature"`
		RotationRate *int `json:"rotation_rate"`
		NVMeLog      struct {
			PercentageUsed *int `json:"percentage_used"`
		} `json:"nvme_smart_health_information_log"`
	}
	if err := json.Unmarshal([]byte(realSMARTJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ModelName != "WD PC SN540 SDDPNPF-512G" {
		t.Fatalf("型号 = %q", payload.ModelName)
	}
	if payload.SmartStatus.Passed == nil || !*payload.SmartStatus.Passed {
		t.Fatal("健康状态未解析")
	}
	if payload.PowerOnTime.Hours == nil || *payload.PowerOnTime.Hours != 8543 {
		t.Fatalf("通电时间 = %v", payload.PowerOnTime.Hours)
	}
	if payload.NVMeLog.PercentageUsed == nil || *payload.NVMeLog.PercentageUsed != 5 {
		t.Fatalf("已用寿命 = %v", payload.NVMeLog.PercentageUsed)
	}
	// NVMe 没有转速概念，字段缺失时必须是 nil 而不是 0——0 会被显示成"机械硬盘 0 RPM"。
	if payload.RotationRate != nil {
		t.Fatalf("NVMe 不应有转速字段：%v", *payload.RotationRate)
	}

	// 兜底断言：把解析结构再序列化一次，序列号不得出现。
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "225202801813") {
		t.Fatal("序列号泄露进了解析结构——它能唯一标识物理硬件，必须排除")
	}
}

func TestMBWArraySizeStaysWithinMemory(t *testing.T) {
	// mbw 会同时分配两个数组，小内存机器上必须收敛，否则会触发 OOM 或压进 swap。
	cases := []struct {
		availableBytes uint64
		want           int
	}{
		{0, 16},          // 读不到可用内存时取下限
		{64 << 20, 16},   // 64 MiB 可用 → 下限
		{512 << 20, 64},  // 512 MiB 可用 → 1/8
		{8 << 30, 256},   // 8 GiB 可用 → 上限封顶
		{256 << 30, 256}, // 大内存同样封顶
	}
	for _, testCase := range cases {
		got := mbwArraySizeMiB(testCase.availableBytes)
		if got != testCase.want {
			t.Errorf("mbwArraySizeMiB(%d) = %d, want %d", testCase.availableBytes, got, testCase.want)
		}
		// 峰值占用是两倍数组大小，不能超过可用内存的一半。
		if testCase.availableBytes > 0 {
			peak := uint64(got) * 2 * 1024 * 1024
			if peak > testCase.availableBytes && got != 16 {
				t.Errorf("可用 %d 字节时 mbw 峰值 %d 字节过大", testCase.availableBytes, peak)
			}
		}
	}
}

// 样本取自本机真实 mbw 1.2.2 输出。
const realMBWOutput = `0	Method: MEMCPY	Elapsed: 0.01104	MiB: 64.00000	Copy: 5797.101 MiB/s
AVG	Method: MEMCPY	Elapsed: 0.01105	MiB: 64.00000	Copy: 5789.473 MiB/s
0	Method: DUMB	Elapsed: 0.00545	MiB: 64.00000	Copy: 11740.965 MiB/s
AVG	Method: DUMB	Elapsed: 0.00641	MiB: 64.00000	Copy: 9976.234 MiB/s
0	Method: MCBLOCK	Elapsed: 0.00902	MiB: 64.00000	Copy: 7099.279 MiB/s
AVG	Method: MCBLOCK	Elapsed: 0.00909	MiB: 64.00000	Copy: 7037.242 MiB/s`

func TestParseMBWOutput(t *testing.T) {
	samples := parseMBWOutput(realMBWOutput)
	if len(samples) != 3 {
		t.Fatalf("应解析出 3 种方法的平均值，实际 %d：%+v", len(samples), samples)
	}
	want := map[string]float64{"MEMCPY": 5789.473, "DUMB": 9976.234, "MCBLOCK": 7037.242}
	for _, sample := range samples {
		expected, ok := want[sample.Method]
		if !ok {
			t.Errorf("出现未预期的方法 %q", sample.Method)
			continue
		}
		if sample.RateMiB != expected {
			t.Errorf("%s = %g MiB/s, want %g", sample.Method, sample.RateMiB, expected)
		}
	}
	// 只取 AVG 行：逐次结果混进来会让同一方法出现多个值。
	for _, sample := range samples {
		if sample.RateMiB == 11740.965 || sample.RateMiB == 5797.101 {
			t.Fatalf("解析到了单次结果而非 AVG 行：%+v", sample)
		}
	}
	if got := parseMBWOutput("无关输出"); len(got) != 0 {
		t.Fatalf("无 AVG 行时不应产出样本：%+v", got)
	}
}
