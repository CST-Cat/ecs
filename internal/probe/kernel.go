package probe

import (
	"strconv"
	"strings"

	"ecs/internal/model"
)

// Kernel network parameters are read directly from /proc/sys. Values stay as
// the kernel exposed them; presentation labels and rationale are stable keys.
type kernelParam struct {
	Key  string
	Path string
}

func kernelParams() []kernelParam {
	return []kernelParam{
		{Key: "tcp_congestion_control", Path: "net/ipv4/tcp_congestion_control"},
		{Key: "tcp_available_congestion", Path: "net/ipv4/tcp_available_congestion_control"},
		{Key: "default_qdisc", Path: "net/core/default_qdisc"},
		{Key: "tcp_fastopen", Path: "net/ipv4/tcp_fastopen"},
		{Key: "rmem_max", Path: "net/core/rmem_max"},
		{Key: "wmem_max", Path: "net/core/wmem_max"},
		{Key: "tcp_rmem", Path: "net/ipv4/tcp_rmem"},
		{Key: "tcp_wmem", Path: "net/ipv4/tcp_wmem"},
		{Key: "ip_forward", Path: "net/ipv4/ip_forward"},
		{Key: "tcp_syncookies", Path: "net/ipv4/tcp_syncookies"},
		{Key: "tcp_mtu_probing", Path: "net/ipv4/tcp_mtu_probing"},
		{Key: "tcp_slow_start_after_idle", Path: "net/ipv4/tcp_slow_start_after_idle"},
		{Key: "disable_ipv6", Path: "net/ipv6/conf/all/disable_ipv6"},
		{Key: "somaxconn", Path: "net/core/somaxconn"},
		{Key: "nf_conntrack_max", Path: "net/netfilter/nf_conntrack_max"},
		{Key: "swappiness", Path: "vm/swappiness"},
	}
}

func kernelParamLabelKey(key string) string {
	return "probe.kernel.param." + key + ".label"
}

func kernelParamWhyKey(key string) string {
	return "probe.kernel.param." + key + ".why"
}

func bdpThroughputMbps(bufferBytes int, rttMS float64) float64 {
	if bufferBytes <= 0 || rttMS <= 0 {
		return 0
	}
	return float64(bufferBytes) * 8 / (rttMS / 1000) / 1e6
}

func bbrMachineStatus(current, available string) string {
	if current == "bbr" {
		return "enabled"
	}
	if strings.Contains(" "+available+" ", " bbr ") {
		return "available_not_enabled"
	}
	return "unavailable"
}

func appendKernelNetworkParams(result *model.Result) {
	values := make(map[string]string)
	for _, param := range kernelParams() {
		value := readTrimmed("/proc/sys/"+param.Path, "")
		if value == "" {
			continue
		}
		values[param.Key] = strings.Join(strings.Fields(value), " ")
	}
	appendKernelNetworkFacts(result, values)
}

func appendKernelNetworkFacts(result *model.Result, values map[string]string) {
	table := model.Table{
		Key:         "system.kernel.network_parameters",
		Title:       "probe.kernel.table.title",
		Columns:     []string{"probe.kernel.column.parameter", "probe.kernel.column.current_value", "probe.kernel.column.rationale"},
		ColumnKeys:  []string{"parameter", "current_value", "rationale"},
		RowIdentity: "parameter",
	}
	for _, param := range kernelParams() {
		value := strings.TrimSpace(values[param.Key])
		if value == "" {
			continue
		}
		table.Rows = append(table.Rows, []string{kernelParamLabelKey(param.Key), value, kernelParamWhyKey(param.Key)})
	}
	if len(table.Rows) == 0 {
		return
	}
	result.Tables = append(result.Tables, table)

	current := values["tcp_congestion_control"]
	available := values["tcp_available_congestion"]
	bbrStatus := bbrMachineStatus(current, available)
	result.Fields = append(result.Fields,
		model.Field{Key: "bbr_status", Label: "probe.kernel.field.bbr_status", Value: bbrStatus},
		model.Field{Key: "tcp_congestion_control", Label: "probe.kernel.field.current_congestion_control", Value: fallback(current, "unknown")},
		model.Field{Key: "tcp_available_congestion", Label: "probe.kernel.field.available_congestion_controls", Value: fallback(available, "unknown")},
	)
	if bbrStatus == "unavailable" {
		result.Notes = append(result.Notes, "probe.kernel.note.bbr_unavailable")
	}

	if raw, ok := values["rmem_max"]; ok {
		if bufferBytes, err := strconv.Atoi(raw); err == nil && bufferBytes > 0 {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "tcp_rmem_max_bytes", Label: "probe.kernel.metric.rmem_max",
				Value: float64(bufferBytes), Unit: "bytes", Display: model.FormatBytes(uint64(bufferBytes)),
				Method: "proc-sys-net-core-rmem-max-v1", HigherIsBetter: model.BoolPtr(true),
			})
			const referenceRTTMS = 150.0
			if limit := bdpThroughputMbps(bufferBytes, referenceRTTMS); limit > 0 {
				result.Measurements = append(result.Measurements, model.Measurement{
					Key: "tcp_single_flow_window_limit_150ms_mbps", Label: "probe.kernel.metric.single_flow_window_limit_150ms",
					Value: limit, Unit: "Mbps", Display: model.FormatRate(limit, "Mbps"),
					Method: "tcp-window-bdp-limit-150ms-v1", HigherIsBetter: model.BoolPtr(true),
				})
				if limit < 500 {
					result.Notes = append(result.Notes, "probe.kernel.note.rmem_bdp_limit")
				}
			}
		}
	}
	if values["disable_ipv6"] == "1" {
		result.Notes = append(result.Notes, "probe.kernel.note.ipv6_disabled")
	}
}
