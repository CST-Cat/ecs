package probe

import (
	"fmt"
	"strconv"
	"strings"

	"ecs/internal/model"
)

// 内核网络参数采集。
//
// 这些值直接决定网络表现，却常被忽略：跨洋链路跑不满带宽，多半不是线路问题，
// 而是 socket 缓冲区上限卡住了 BDP；想开 BBR 却发现内核根本没编译进去。
// 全部读自 /proc/sys，纯本地、零依赖、不需要 root。

// kernelParam 是一个待采集的内核参数。
type kernelParam struct {
	Key   string
	Path  string
	Label string
	// Why 说明这个值为什么值得看。
	Why string
}

// kernelParams 是要采集的参数清单，全部于 2026-08-01 在本机确认可读。
func kernelParams() []kernelParam {
	return []kernelParam{
		{Key: "tcp_congestion_control", Path: "net/ipv4/tcp_congestion_control", Label: "拥塞控制算法", Why: "决定丢包链路上的吞吐表现"},
		{Key: "tcp_available_congestion", Path: "net/ipv4/tcp_available_congestion_control", Label: "可用拥塞算法", Why: "BBR 不在其中就说明内核没编译进来"},
		{Key: "default_qdisc", Path: "net/core/default_qdisc", Label: "默认队列规则", Why: "BBR 通常需要配合 fq"},
		{Key: "tcp_fastopen", Path: "net/ipv4/tcp_fastopen", Label: "TCP Fast Open", Why: "减少一次握手往返"},
		{Key: "rmem_max", Path: "net/core/rmem_max", Label: "接收缓冲上限", Why: "高延迟链路的吞吐上限由它与 RTT 共同决定"},
		{Key: "wmem_max", Path: "net/core/wmem_max", Label: "发送缓冲上限", Why: "同上，影响上行"},
		{Key: "tcp_rmem", Path: "net/ipv4/tcp_rmem", Label: "TCP 接收缓冲", Why: "最小/默认/最大三档自动调节范围"},
		{Key: "tcp_wmem", Path: "net/ipv4/tcp_wmem", Label: "TCP 发送缓冲", Why: "同上"},
		{Key: "ip_forward", Path: "net/ipv4/ip_forward", Label: "IPv4 转发", Why: "做软路由、VPN 出口必须开启"},
		{Key: "tcp_syncookies", Path: "net/ipv4/tcp_syncookies", Label: "SYN Cookies", Why: "SYN Flood 下维持可用性"},
		{Key: "tcp_mtu_probing", Path: "net/ipv4/tcp_mtu_probing", Label: "MTU 探测", Why: "隧道环境下避免黑洞"},
		{Key: "tcp_slow_start_after_idle", Path: "net/ipv4/tcp_slow_start_after_idle", Label: "空闲后慢启动", Why: "长连接复用时置 0 可避免反复降速"},
		{Key: "disable_ipv6", Path: "net/ipv6/conf/all/disable_ipv6", Label: "IPv6 已禁用", Why: "为 1 时系统层面关掉了 IPv6"},
		{Key: "somaxconn", Path: "net/core/somaxconn", Label: "监听队列上限", Why: "高并发服务的 accept 队列"},
		{Key: "nf_conntrack_max", Path: "net/netfilter/nf_conntrack_max", Label: "连接跟踪上限", Why: "NAT 与高并发下会成为瓶颈"},
		{Key: "swappiness", Path: "vm/swappiness", Label: "Swap 倾向", Why: "小内存机器上过高会拖慢一切"},
	}
}

// bdpThroughputMbps 由缓冲区上限与往返时延算出理论吞吐上限。
//
// 单条 TCP 连接的吞吐不会超过 窗口大小 / RTT。这就是为什么默认 208 KiB 的
// rmem_max 在 200 ms 的跨洋链路上把单流限死在 8 Mbps 左右——线路再快也没用。
func bdpThroughputMbps(bufferBytes int, rttMS float64) float64 {
	if bufferBytes <= 0 || rttMS <= 0 {
		return 0
	}
	return float64(bufferBytes) * 8 / (rttMS / 1000) / 1e6
}

// appendKernelNetworkParams 采集内核参数并给出解读。
func appendKernelNetworkParams(result *model.Result) {
	table := model.Table{
		Key:        "system.kernel.network_parameters",
		Title:      "内核网络参数",
		Columns:    []string{"参数", "当前值", "为什么值得看"},
		ColumnKeys: []string{"parameter", "current_value", "rationale"},
	}
	values := make(map[string]string)
	for _, param := range kernelParams() {
		value := readTrimmed("/proc/sys/"+param.Path, "")
		if value == "" {
			continue
		}
		value = strings.Join(strings.Fields(value), " ")
		values[param.Key] = value
		display := value
		if param.Key == "ip_forward" || param.Key == "tcp_syncookies" || param.Key == "disable_ipv6" {
			display = value + " (" + map[string]string{"0": "关", "1": "开"}[value] + ")"
		}
		table.Rows = append(table.Rows, []string{param.Label, display, param.Why})
	}
	if len(table.Rows) == 0 {
		return
	}
	result.Tables = append(result.Tables, table)

	// BBR 可用性是装完 VPS 最常问的一件事，单独给结论。
	available := values["tcp_available_congestion"]
	current := values["tcp_congestion_control"]
	bbrAvailable := strings.Contains(" "+available+" ", " bbr ")
	bbrStatus := "内核未提供 BBR（可用：" + fallback(available, "未知") + "）"
	switch {
	case current == "bbr":
		bbrStatus = "已启用 BBR"
	case bbrAvailable:
		bbrStatus = "内核支持 BBR，当前使用 " + current
	}
	result.Fields = append(result.Fields,
		model.Field{Key: "bbr_status", Label: "BBR 状态", Value: bbrStatus})

	if !bbrAvailable && current != "bbr" {
		result.Notes = append(result.Notes,
			"当前内核没有把 BBR 编译进来，"+
				"`sysctl -w net.ipv4.tcp_congestion_control=bbr` 会失败；需要换内核而不是改参数。")
	}

	// 缓冲区上限换算成典型 RTT 下的单流吞吐上限，这是"线路很快却跑不满"的常见原因。
	if raw, ok := values["rmem_max"]; ok {
		if bufferBytes, err := strconv.Atoi(raw); err == nil && bufferBytes > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "tcp_rmem_max_bytes", Label: "接收缓冲上限",
				Value: float64(bufferBytes), Unit: "bytes", Display: model.FormatBytes(uint64(bufferBytes)),
				Method: "proc-sys-net-core-rmem-max-v1", HigherIsBetter: model.BoolPtr(true),
			})
			// 150 ms 大致对应亚洲到北美/欧洲的常见往返。
			const referenceRTT = 150.0
			if limit := bdpThroughputMbps(bufferBytes, referenceRTT); limit > 0 && limit < 500 {
				result.Notes = append(result.Notes, fmt.Sprintf(
					"接收缓冲上限 %s，在 %.0f ms 往返的链路上单条 TCP 连接最高只能跑到约 %s——"+
						"跨洋链路跑不满带宽时先看这里，而不是怀疑线路。多线程下载或调大 "+
						"net.core.rmem_max 可以绕开这个限制。",
					model.FormatBytes(uint64(bufferBytes)), referenceRTT,
					model.FormatRate(limit, "Mbps")))
			}
		}
	}
	if values["disable_ipv6"] == "1" {
		result.Notes = append(result.Notes, "系统已在内核层面禁用 IPv6，相关模块的 IPv6 结果都会是不可用。")
	}
}
